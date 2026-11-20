package docissue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/docchain"
	"github.com/ostendorf-cloud/solobill/internal/docpayload"
	"github.com/ostendorf-cloud/solobill/internal/numbering"
	"github.com/ostendorf-cloud/solobill/internal/sellerprofile"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// ValidationError is a payload that cannot be frozen. Details maps a
// field path to a reason: the seller validation of issue #68 (paths
// under "seller."), docpayload.Validate and the printable check, merged
// unchanged. The API answers it as 400 invalid_argument.
type ValidationError struct {
	Details map[string]string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("docissue: the document is incomplete: %v", docpayload.SortedPaths(e.Details))
}

// RegimeMismatchError is a draft whose VAT regime differs from the regime
// of the period that covers the issue date. The API answers it as 409
// vat_regime_mismatch with the period's regime in details.
type RegimeMismatchError struct {
	Regime storage.VATRegime
}

func (e *RegimeMismatchError) Error() string {
	return "docissue: the draft's VAT regime differs from the period's " + string(e.Regime)
}

// BaseChangedError is a correction draft whose base is no longer the
// chain's effective document. Seq is the sequence effective now. The API
// answers it as 409 correction_base_changed. The draft stays.
type BaseChangedError struct {
	Seq int
}

func (e *BaseChangedError) Error() string {
	return fmt.Sprintf("docissue: the chain's effective document is sequence %d now", e.Seq)
}

func (e *BaseChangedError) Unwrap() error { return docchain.ErrCorrectionBaseChanged }

var (
	// ErrRegimeMissing is an issue date no VAT regime period covers. The
	// API answers it as 400 vat_regime_missing.
	ErrRegimeMissing = errors.New("docissue: no VAT regime period covers the issue date")

	// ErrNotDraft is a document that is issued already. The API answers
	// it as 409 conflict.
	ErrNotDraft = errors.New("docissue: the document is issued already")

	// ErrNumberTaken is a display number or a correction sequence that
	// exists in the tenant already, for example an imported number. The
	// API answers it as 409 conflict.
	ErrNumberTaken = errors.New("docissue: the number exists already")
)

// PayloadChecker is an optional, deeper check of a payload before it is
// frozen. The renderer of issue #74 implements it with
// internal/pdf.CheckPrintable, which also measures every word against
// its cell and estimates the pages; that needs the font metrics, which
// only the renderer has. Its details are merged like Check's.
type PayloadChecker interface {
	CheckPayload(ctx context.Context, p docpayload.FrozenPayloadV1) (map[string]string, error)
}

// Issuer issues documents. Every repository must be set except the
// optional ones; every call must run inside a context
// storage.TenantScope.WithTenant produced, so the whole issue is one
// transaction.
type Issuer struct {
	Documents        storage.DocumentRepository
	DocumentNumbers  storage.DocumentNumberRepository
	VATRegimePeriods storage.VATRegimePeriodRepository
	Customers        storage.CustomerRepository

	// SellerProfileLogos and Files freeze the logo's SHA-256. Both nil
	// means no logo is printed.
	SellerProfileLogos storage.SellerProfileLogoRepository
	Files              storage.FileRepository

	// Checker is optional, see PayloadChecker.
	Checker PayloadChecker
}

// numberPlan is how the issued document gets its number.
type numberPlan struct {
	// template renders an invoice's or an offer's number from the
	// counter. Nil for a correction.
	template *numbering.Template
	year     int

	// The fields of a correction.
	display       string
	value         int64
	correctionSeq int
	corrects      *docpayload.DocumentReference
}

// Issue issues the draft id at now. In order, all in the caller's
// transaction:
//
//  1. Lock the draft. A document that is not a draft is ErrNotDraft.
//  2. For a correction: lock the chain's root, load the chain, refuse a
//     draft whose base is no longer the effective document
//     (BaseChangedError), and take the next correction sequence.
//  3. Evaluate the VAT regime period on the issue date
//     (ErrRegimeMissing, RegimeMismatchError).
//  4. Read and lock the seller profile, validate it for the draft's
//     regime, build the payload and check it (ValidationError). An
//     invoice or an offer is built with a placeholder counter value here,
//     so a refused issue never touches the counter.
//  5. Allocate the counter value of an invoice or an offer, render the
//     display number, build and check the payload again.
//  6. Marshal, hash and write it in one statement.
//
// Any error leaves the caller to roll the transaction back, the counter
// allocation included.
func (i Issuer) Issue(ctx context.Context, id uuid.UUID, now time.Time) (storage.Document, error) {
	draft, err := i.Documents.Lock(ctx, id)
	if err != nil {
		return storage.Document{}, err
	}
	if draft.State != storage.DocumentStateDraft {
		return storage.Document{}, ErrNotDraft
	}

	issueDate := IssueDate(now)
	issuedAt := IssuedAt(now)

	var plan numberPlan
	switch draft.Type {
	case storage.DocumentTypeCorrection:
		plan, err = i.planCorrection(ctx, draft)
		if err != nil {
			return storage.Document{}, err
		}
	case storage.DocumentTypeInvoice, storage.DocumentTypeOffer:
		plan.year = issueDate.Year()
	default:
		return storage.Document{}, fmt.Errorf("docissue: unknown document type %q", draft.Type)
	}

	period, err := i.VATRegimePeriods.RegimeOn(ctx, issueDate)
	if errors.Is(err, storage.ErrNotFound) {
		return storage.Document{}, ErrRegimeMissing
	}
	if err != nil {
		return storage.Document{}, fmt.Errorf("docissue: regime on %s: %w", issueDate.Format(docpayload.DateLayout), err)
	}
	if period.Regime != draft.VATRegime {
		return storage.Document{}, &RegimeMismatchError{Regime: period.Regime}
	}

	profile, err := i.Documents.SellerProfileForIssue(ctx)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return storage.Document{}, fmt.Errorf("docissue: seller profile: %w", err)
	}
	problems := map[string]string{}
	for path, reason := range sellerprofile.Validate(profile, draft.VATRegime) {
		problems[path] = reason
	}

	if plan.template == nil && draft.Type != storage.DocumentTypeCorrection {
		formats := sellerprofile.WithDefaultFormats(profile.NumberFormats)
		tpl, err := numbering.Parse(formats[draft.Type])
		if err != nil {
			problems["seller.number_formats."+string(draft.Type)] = err.Error()
		} else {
			plan.template = &tpl
		}
	}

	customer, err := i.Customers.FindByID(ctx, draft.CustomerID)
	if err != nil {
		return storage.Document{}, fmt.Errorf("docissue: customer: %w", err)
	}

	logo, err := i.logoSHA256(ctx)
	if err != nil {
		return storage.Document{}, err
	}

	in := PayloadInput{
		Document:   draft,
		Seller:     profile,
		LogoSHA256: logo,
		Customer:   customer,
		IssueDate:  issueDate,
	}

	// A placeholder counter value of 1: the checks then run on a number
	// rendered exactly like the real one, only with other digits.
	if err := plan.apply(&in, 1); err != nil {
		problems["number"] = err.Error()
	}
	if err := i.check(ctx, BuildPayload(in), problems); err != nil {
		return storage.Document{}, err
	}

	if plan.template != nil {
		n, err := i.DocumentNumbers.AllocateNext(ctx, draft.Type, plan.year)
		if err != nil {
			return storage.Document{}, fmt.Errorf("docissue: allocate number: %w", err)
		}
		if err := plan.apply(&in, n); err != nil {
			return storage.Document{}, &ValidationError{Details: map[string]string{"number": err.Error()}}
		}
	}

	payload := BuildPayload(in)
	if err := i.check(ctx, payload, map[string]string{}); err != nil {
		return storage.Document{}, err
	}

	raw, err := docpayload.Marshal(payload)
	if err != nil {
		return storage.Document{}, fmt.Errorf("docissue: %w", err)
	}
	sum := sha256.Sum256(raw)

	issue := storage.DocumentIssue{
		Type:          draft.Type,
		Number:        in.NumberValue,
		NumberYear:    in.NumberYear,
		DisplayNumber: in.Number,
		IssuedAt:      issuedAt,
		Payload:       raw,
		PayloadSHA256: hex.EncodeToString(sum[:]),
	}
	if draft.Type == storage.DocumentTypeCorrection {
		seq := int32(plan.correctionSeq) //nolint:gosec // at most numbering.MaxCorrectionSeq
		issue.CorrectionSeq = &seq
	}

	issued, err := i.Documents.Issue(ctx, draft.ID, issue)
	if errors.Is(err, storage.ErrConflict) {
		return storage.Document{}, ErrNumberTaken
	}
	if err != nil {
		return storage.Document{}, fmt.Errorf("docissue: issue: %w", err)
	}

	return issued, nil
}

// apply sets the number fields of in: for an invoice or an offer from
// the counter value n, for a correction from the plan alone.
func (p numberPlan) apply(in *PayloadInput, n int64) error {
	if p.template == nil {
		if p.display == "" {
			// An invoice or an offer whose template does not parse: the
			// problem is reported on the template already.
			return nil
		}
		in.Number = p.display
		in.NumberValue = p.value
		in.NumberYear = p.year
		in.CorrectionSeq = p.correctionSeq
		in.Corrects = p.corrects

		return nil
	}

	display, err := p.template.Render(p.year, n)
	if err != nil {
		return err
	}
	in.Number = display
	in.NumberValue = n
	in.NumberYear = p.year

	return nil
}

// check merges Check, the optional PayloadChecker and problems, and
// returns a ValidationError when anything is left.
func (i Issuer) check(ctx context.Context, p docpayload.FrozenPayloadV1, problems map[string]string) error {
	merge := func(more map[string]string) {
		for path, reason := range more {
			if _, seen := problems[path]; !seen {
				problems[path] = reason
			}
		}
	}
	merge(Check(p))
	if i.Checker != nil {
		more, err := i.Checker.CheckPayload(ctx, p)
		if err != nil {
			return fmt.Errorf("docissue: check payload: %w", err)
		}
		merge(more)
	}
	if len(problems) > 0 {
		return &ValidationError{Details: problems}
	}

	return nil
}

// planCorrection locks the chain's root, loads the chain and checks the
// draft's base. The correction gets the next sequence of the chain and
// the root's number plus "-" and that sequence. corrects_document_id
// always names the root, also for a correction of a correction.
func (i Issuer) planCorrection(ctx context.Context, draft storage.Document) (numberPlan, error) {
	if draft.CorrectsDocumentID == nil {
		return numberPlan{}, errors.New("docissue: a correction without its original")
	}
	root, err := i.Documents.Lock(ctx, *draft.CorrectsDocumentID)
	if err != nil {
		return numberPlan{}, fmt.Errorf("docissue: lock the chain root: %w", err)
	}
	if root.Type != storage.DocumentTypeInvoice || root.State != storage.DocumentStateIssued ||
		root.DisplayNumber == nil || root.Number == nil || root.NumberYear == nil {
		return numberPlan{}, fmt.Errorf("docissue: the chain root %s is no issued invoice with a number", root.ID)
	}

	documents, err := i.Documents.Chain(ctx, root.ID)
	if err != nil {
		return numberPlan{}, fmt.Errorf("docissue: load the chain: %w", err)
	}
	chain, err := ChainOf(documents)
	if err != nil {
		return numberPlan{}, err
	}

	effective := chain.Effective()
	if draft.CorrectionBaseID == nil || draft.CorrectionBaseSeq == nil {
		return numberPlan{}, &BaseChangedError{Seq: effective.Seq}
	}
	base := docchain.Base{DocumentID: *draft.CorrectionBaseID, Seq: int(*draft.CorrectionBaseSeq)}
	if err := chain.CheckBase(base); err != nil {
		return numberPlan{}, &BaseChangedError{Seq: effective.Seq}
	}

	seq := chain.NextSeq()
	display, err := numbering.Correction(*root.DisplayNumber, seq)
	if err != nil {
		return numberPlan{}, &ValidationError{Details: map[string]string{"correction_seq": err.Error()}}
	}

	return numberPlan{
		year:          int(*root.NumberYear),
		display:       display,
		value:         *root.Number,
		correctionSeq: seq,
		corrects: &docpayload.DocumentReference{
			ID:        root.ID.String(),
			Number:    *root.DisplayNumber,
			IssueDate: issueDateOf(root),
		},
	}, nil
}

// issueDateOf is the issue date an issued document printed: the one in
// its frozen payload, or its issue time in Germany when the payload
// cannot be read.
func issueDateOf(d storage.Document) string {
	if p, err := docpayload.Unmarshal(d.FrozenPayload); err == nil && p.IssueDate != "" {
		return p.IssueDate
	}
	if d.IssuedAt != nil {
		return IssueDate(*d.IssuedAt).Format(docpayload.DateLayout)
	}

	return ""
}

// logoSHA256 is the hash of the profile's logo, "" without one.
func (i Issuer) logoSHA256(ctx context.Context) (string, error) {
	if i.SellerProfileLogos == nil {
		return "", nil
	}
	id, err := i.SellerProfileLogos.Logo(ctx)
	if errors.Is(err, storage.ErrNotFound) || (err == nil && id == nil) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("docissue: logo: %w", err)
	}
	if i.Files == nil {
		return "", errors.New("docissue: a logo is set but no file repository is wired")
	}
	file, err := i.Files.FindByID(ctx, *id)
	if err != nil {
		return "", fmt.Errorf("docissue: logo file: %w", err)
	}

	return file.SHA256, nil
}
