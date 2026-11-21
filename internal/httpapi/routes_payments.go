package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/api"

	"github.com/ostendorf-cloud/solobill/internal/docissue"
	"github.com/ostendorf-cloud/solobill/internal/docpayload"
	"github.com/ostendorf-cloud/solobill/internal/paymentvat"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// registerPayments mounts the payments of a document. Owned by issue
// #71 (I5). See docs/api.md, "Payments", and docs/concepts/solobill.md,
// "Payments".
//
// A payment belongs to an invoice chain: {id} is any issued document of
// the chain, and the payment is stored against the chain root, the
// original invoice. Payments are append-only. A recording mistake is
// undone by a reversal, money paid back is a refund. The open amount is
// derived on every read and never stored.
//
// Decisions this file makes where docs/api.md leaves room:
//   - A draft or an offer answers 409 on GET as well as on POST: neither
//     belongs to a chain that can be paid.
//   - received_on is a calendar date from 2000-01-01 to today in
//     Europe/Berlin. A payment is recorded after the money moved, so a
//     future date is a typo, and it would put the entry into the wrong
//     EÜR year.
//   - An overpayment is accepted (docs/api.md) and shows as a negative
//     open_cents.
//   - A refund (negative, no reverses_payment_id) may pay back at most
//     the refund due: the negated open_cents. With nothing due it is
//     refused. A recording mistake is a reversal, not a refund.
//   - A reversal names a payment of the same chain that is no reversal
//     and not reversed yet, carries exactly its negated amount, and is
//     not dated before it. The table's trigger checks the same.
//
// Both writes and the reads they decide on run under a lock on the root
// document row, so two refunds or a refund and a correction cannot both
// see the same open amount.
func registerPayments(mux *http.ServeMux, route routeFunc, a *app) {
	mux.Handle("GET /api/v1/documents/{id}/payments", route(api.Handle(a.handleListPayments)))
	mux.Handle("POST /api/v1/documents/{id}/payments", route(api.Handle(a.handleCreatePayment)))
}

// maxPaymentAmountCents is 1,000,000,000.00 EUR, the bound of the
// payments table (payments_amount_bound).
const maxPaymentAmountCents int64 = 100_000_000_000

const (
	maxPaymentNoteRunes = 1000
	paymentEarliestYear = 2000
)

var paymentEarliestDate = time.Date(paymentEarliestYear, time.January, 1, 0, 0, 0, 0, time.UTC)

// paymentMethods is the value set of method, the same as the table's
// payments_method_check.
var paymentMethods = map[storage.PaymentMethod]bool{
	storage.PaymentMethodBankTransfer: true,
	storage.PaymentMethodCash:         true,
	storage.PaymentMethodCard:         true,
	storage.PaymentMethodPayPal:       true,
	storage.PaymentMethodOther:        true,
}

// paymentJSON is the payment JSON of docs/api.md.
type paymentJSON struct {
	ID                string  `json:"id"`
	DocumentID        string  `json:"document_id"`
	ReceivedOn        string  `json:"received_on"`
	AmountCents       int64   `json:"amount_cents"`
	Method            string  `json:"method"`
	Note              *string `json:"note"`
	ReversesPaymentID *string `json:"reverses_payment_id"`
	Imported          bool    `json:"imported"`
	CreatedAt         string  `json:"created_at"`
}

func toPaymentJSON(p storage.Payment) paymentJSON {
	out := paymentJSON{
		ID:          p.ID.String(),
		DocumentID:  p.DocumentID.String(),
		ReceivedOn:  p.ReceivedOn.UTC().Format(docpayload.DateLayout),
		AmountCents: p.AmountCents,
		Method:      string(p.Method),
		Note:        p.Note,
		Imported:    p.Imported,
		CreatedAt:   p.CreatedAt.UTC().Format(time.RFC3339),
	}
	if p.ReversesPaymentID != nil {
		s := p.ReversesPaymentID.String()
		out.ReversesPaymentID = &s
	}

	return out
}

// paymentRequest is the body of POST /api/v1/documents/{id}/payments.
// Every field is a pointer so a missing one is told apart from a zero.
// api.Decode refuses unknown fields: the tenant, the document, imported
// and the timestamps never come from the body.
type paymentRequest struct {
	ReceivedOn        *string `json:"received_on"`
	AmountCents       *int64  `json:"amount_cents"`
	Method            *string `json:"method"`
	Note              *string `json:"note"`
	ReversesPaymentID *string `json:"reverses_payment_id"`
}

// payment checks the body on its own and returns the payment it
// describes, without tenant and document.
func (r paymentRequest) payment(today time.Time) (storage.Payment, error) {
	details := map[string]any{}
	var p storage.Payment

	switch {
	case r.ReceivedOn == nil || strings.TrimSpace(*r.ReceivedOn) == "":
		details["received_on"] = "required"
	default:
		d, err := time.Parse(docpayload.DateLayout, strings.TrimSpace(*r.ReceivedOn))
		switch {
		case err != nil:
			details["received_on"] = "must be a date such as 2026-12-03"
		case d.Before(paymentEarliestDate) || d.After(today):
			details["received_on"] = "must be between 2000-01-01 and today"
		default:
			p.ReceivedOn = d
		}
	}

	switch {
	case r.AmountCents == nil:
		details["amount_cents"] = "required"
	case *r.AmountCents == 0:
		details["amount_cents"] = "must not be 0"
	case *r.AmountCents > maxPaymentAmountCents || *r.AmountCents < -maxPaymentAmountCents:
		details["amount_cents"] = fmt.Sprintf("must be at most %d in absolute value", maxPaymentAmountCents)
	default:
		p.AmountCents = *r.AmountCents
	}

	switch {
	case r.Method == nil || *r.Method == "":
		details["method"] = "required"
	case !paymentMethods[storage.PaymentMethod(*r.Method)]:
		details["method"] = "must be bank_transfer, cash, card, paypal or other"
	default:
		p.Method = storage.PaymentMethod(*r.Method)
	}

	if r.Note != nil && *r.Note != "" {
		if utf8.RuneCountInString(*r.Note) > maxPaymentNoteRunes {
			details["note"] = fmt.Sprintf("must be at most %d characters", maxPaymentNoteRunes)
		} else {
			note := *r.Note
			p.Note = &note
		}
	}

	if r.ReversesPaymentID != nil && strings.TrimSpace(*r.ReversesPaymentID) != "" {
		id, err := uuid.Parse(strings.TrimSpace(*r.ReversesPaymentID))
		if err != nil {
			details["reverses_payment_id"] = "must be a payment id"
		} else {
			p.ReversesPaymentID = &id
			if r.AmountCents != nil && *r.AmountCents > 0 {
				details["amount_cents"] = "a reversal must be negative"
			}
		}
	}

	if len(details) > 0 {
		return storage.Payment{}, masterDataInvalid(details)
	}

	return p, nil
}

// paymentsWired reports whether every repository this file needs is
// wired. Until then the routes answer 501, as their stubs did.
func (a *app) paymentsWired() bool {
	return a.repos.Scope != nil && a.repos.Documents != nil && a.repos.Payments != nil
}

// paymentsCaller is requireSession plus the wiring check. owner adds the
// owner role (docs/api.md: POST is owner only, GET is any).
func (a *app) paymentsCaller(w http.ResponseWriter, r *http.Request, owner bool) (caller, bool, error) {
	c, err := a.requireSession(r)
	if err != nil {
		return caller{}, false, err
	}
	if !a.paymentsWired() {
		return caller{}, false, a.notImplemented(w, r)
	}
	if owner && c.role != storage.AccountRoleOwner {
		return caller{}, false, api.PermissionDenied("owner role required")
	}

	return c, true, nil
}

// paymentChain is one chain as the payment routes see it.
type paymentChain struct {
	rootID      uuid.UUID
	effectiveID uuid.UUID
	grandTotal  int64
	payments    []storage.Payment
}

func (pc paymentChain) paidCents() int64 {
	var paid int64
	for _, p := range pc.payments {
		paid += p.AmountCents
	}

	return paid
}

// openCents is the effective document's grand total minus the sum of the
// chain's payments. Negative means a refund is due.
func (pc paymentChain) openCents() int64 {
	return pc.grandTotal - pc.paidCents()
}

// errPaymentNotPayable is the 409 of docs/api.md for a draft or an offer.
// A new value per call: an *api.Error is not shared between requests.
func errPaymentNotPayable() error {
	return api.Conflict("payments belong to issued invoices and their corrections only")
}

// loadPaymentChain resolves id to its chain and reads the chain's
// payments. Another tenant's document is not found, the same as a
// missing one. lock locks the root row first, for a write.
func (a *app) loadPaymentChain(ctx context.Context, c caller, id uuid.UUID, lock bool) (paymentChain, error) {
	doc, err := a.repos.Documents.FindByID(ctx, id)
	if err != nil {
		return paymentChain{}, err
	}
	// Row level security already hides another tenant's row; the handler
	// does not rely on it alone.
	if doc.TenantID != c.tenantID {
		return paymentChain{}, storage.ErrNotFound
	}
	if doc.State != storage.DocumentStateIssued {
		return paymentChain{}, errPaymentNotPayable()
	}
	rootID, ok := docissue.RootOf(doc)
	if !ok {
		return paymentChain{}, errPaymentNotPayable()
	}

	if lock {
		root, err := a.repos.Documents.Lock(ctx, rootID)
		if err != nil {
			return paymentChain{}, err
		}
		if root.TenantID != c.tenantID || root.Type != storage.DocumentTypeInvoice || root.State != storage.DocumentStateIssued {
			return paymentChain{}, errPaymentNotPayable()
		}
	}

	members, err := a.repos.Documents.Chain(ctx, rootID)
	if err != nil {
		return paymentChain{}, err
	}
	chain, err := docissue.ChainOf(members)
	if err != nil {
		return paymentChain{}, err
	}
	effectiveID := chain.Effective().ID
	var effective *storage.Document
	for i := range members {
		if members[i].ID == effectiveID {
			effective = &members[i]
		}
	}
	if effective == nil {
		return paymentChain{}, fmt.Errorf("httpapi: payments: effective document %s missing from its chain", effectiveID)
	}
	figures, err := paymentvat.FiguresOf(*effective)
	if err != nil {
		return paymentChain{}, err
	}

	list, err := a.repos.Payments.ListByChain(ctx, rootID)
	if err != nil {
		return paymentChain{}, err
	}
	own := make([]storage.Payment, 0, len(list))
	for _, p := range list {
		if p.TenantID == c.tenantID && p.DocumentID == rootID {
			own = append(own, p)
		}
	}

	return paymentChain{rootID: rootID, effectiveID: effectiveID, grandTotal: figures.GrossCents, payments: own}, nil
}

// checkPaymentInChain applies the rules that need the chain: a reversal
// mirrors an unreversed payment of the chain, a refund pays back at most
// what is due.
func checkPaymentInChain(pc paymentChain, p storage.Payment) error {
	if p.ReversesPaymentID != nil {
		var original *storage.Payment
		for i := range pc.payments {
			q := pc.payments[i]
			if q.ID == *p.ReversesPaymentID {
				original = &pc.payments[i]
			}
			if q.ReversesPaymentID != nil && *q.ReversesPaymentID == *p.ReversesPaymentID {
				return api.Conflict("the payment is reversed already")
			}
		}
		if original == nil {
			return api.Conflict("reverses_payment_id names no payment of this chain")
		}
		if original.ReversesPaymentID != nil {
			return api.Conflict("a reversal cannot be reversed")
		}
		details := map[string]any{}
		if p.AmountCents != -original.AmountCents {
			details["amount_cents"] = fmt.Sprintf("must be %d, the negated amount of the reversed payment", -original.AmountCents)
		}
		if p.ReceivedOn.Before(original.ReceivedOn) {
			details["received_on"] = "must not be before the received_on of the reversed payment"
		}
		if len(details) > 0 {
			return masterDataInvalid(details)
		}

		return nil
	}

	if p.AmountCents < 0 {
		due := -pc.openCents()
		if due <= 0 {
			return masterDataInvalid(map[string]any{
				"amount_cents": "no refund is due; undo a recording mistake with reverses_payment_id",
			})
		}
		if -p.AmountCents > due {
			return masterDataInvalid(map[string]any{
				"amount_cents": fmt.Sprintf("a refund may pay back at most the refund due, %d cents", due),
			})
		}
	}

	return nil
}

// paymentsError maps an error from the tenant scope: an *api.Error passes
// through, a repository sentinel becomes 404 or 409, anything else 500.
func paymentsError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		return apiErr
	}

	return masterDataError(err, "the payment conflicts with the chain: the document is not payable, "+
		"or the reversed payment belongs to another chain or is reversed already")
}

func (a *app) handleListPayments(w http.ResponseWriter, r *http.Request) error {
	c, ok, err := a.paymentsCaller(w, r, false)
	if !ok {
		return err
	}
	id, err := masterDataID(r)
	if err != nil {
		return err
	}

	var pc paymentChain
	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		var err error
		pc, err = a.loadPaymentChain(ctx, c, id, false)

		return err
	})
	if err != nil {
		return paymentsError(err)
	}

	out := make([]paymentJSON, 0, len(pc.payments))
	for _, p := range pc.payments {
		out = append(out, toPaymentJSON(p))
	}

	return api.Write(w, http.StatusOK, map[string]any{
		"payments":              out,
		"paid_cents":            pc.paidCents(),
		"effective_document_id": pc.effectiveID.String(),
		"open_cents":            pc.openCents(),
	})
}

func (a *app) handleCreatePayment(w http.ResponseWriter, r *http.Request) error {
	c, ok, err := a.paymentsCaller(w, r, true)
	if !ok {
		return err
	}
	id, err := masterDataID(r)
	if err != nil {
		return err
	}

	var req paymentRequest
	if err := api.Decode(r, &req); err != nil {
		return err
	}
	// Today in Europe/Berlin, the calendar the owner books in.
	p, err := req.payment(docissue.IssueDate(time.Now()))
	if err != nil {
		return err
	}
	// The tenant comes from the session, never from the body.
	p.TenantID = c.tenantID

	var created storage.Payment
	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		pc, err := a.loadPaymentChain(ctx, c, id, true)
		if err != nil {
			return err
		}
		p.DocumentID = pc.rootID
		if err := checkPaymentInChain(pc, p); err != nil {
			return err
		}
		created, err = a.repos.Payments.Create(ctx, p)

		return err
	})
	if err != nil {
		return paymentsError(err)
	}

	return api.Write(w, http.StatusCreated, toPaymentJSON(created))
}
