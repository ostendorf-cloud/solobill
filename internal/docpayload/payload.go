// Package docpayload defines the frozen payload of an issued document:
// the complete, self contained snapshot stored in
// documents.frozen_payload, hashed into documents.frozen_payload_sha256
// and rendered into a PDF forever after (issue #74).
//
// The payload is versioned. FrozenPayloadV1 is version 1. A stored
// payload is never migrated: a later shape is a new type with a new
// version number, and the renderer keeps reading every version it ever
// wrote. That is what makes a PDF reproducible after years.
//
// Every figure is precomputed. The renderer formats, it never
// calculates: the totals come from internal/money at issue time.
//
// Pure data and validation, no database, no clock, no I/O.
package docpayload

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ostendorf-cloud/solobill/internal/money"
)

// Version1 is the value of FrozenPayloadV1.Version.
const Version1 = 1

// CurrencyEUR is the only currency of version 1.
const CurrencyEUR = "EUR"

// DateLayout is the only date format in a payload: ISO 8601 calendar
// date, no time and no zone, because every date on an invoice is a
// calendar date.
const DateLayout = "2006-01-02"

// Document types, regimes and languages as they appear in a payload.
// They equal the values of internal/storage, duplicated here so this
// package imports nothing but the pure packages internal/money and
// internal/numbering.
const (
	TypeInvoice    = "invoice"
	TypeCorrection = "correction"
	TypeOffer      = "offer"

	RegimeStandard         = "standard"
	RegimeKleinunternehmer = "kleinunternehmer"

	LanguageGerman  = "de"
	LanguageEnglish = "en"
)

// FrozenPayloadV1 is version 1 of the frozen payload. The JSON field
// names are the contract: they are never renamed.
//
// Field to law map for an invoice (section 14 Abs. 4 Satz 1 UStG):
//
//   - Nr. 1, names and addresses: Seller, Customer.
//   - Nr. 2, Steuernummer or USt-IdNr: Seller.TaxNumber, Seller.VATID.
//   - Nr. 3, Ausstellungsdatum: IssueDate.
//   - Nr. 4, Rechnungsnummer: Number.
//   - Nr. 5, Menge und Art: Lines[].Quantity, Unit, Description.
//   - Nr. 6, Zeitpunkt der Leistung: ServicePeriod.
//   - Nr. 7, Entgelt nach Steuersätzen and reductions: Totals.VAT[].NetCents,
//     Lines[].LineDiscount, DocumentDiscount, Totals.DocumentDiscountCents.
//   - Nr. 8, Steuersatz and Steuerbetrag or the exemption note:
//     Totals.VAT[], TaxNote.
//
// Section 34a UStDV for a Kleinunternehmer: Seller and Customer, one of
// Seller.TaxNumber, Seller.VATID or Seller.KleinunternehmerID,
// IssueDate, Lines, Totals.GrandTotalCents and TaxNote naming the
// exemption of section 19 UStG.
type FrozenPayloadV1 struct {
	Version int `json:"version"`
	// Type is TypeInvoice, TypeCorrection or TypeOffer.
	Type string `json:"type"`
	// Language is LanguageGerman or LanguageEnglish.
	Language string `json:"language"`
	// VATRegime is RegimeStandard or RegimeKleinunternehmer.
	VATRegime string `json:"vat_regime"`
	// TemplateVersion names the embedded template the document was first
	// rendered with. The renderer keeps every version it ever shipped.
	TemplateVersion string `json:"template_version"`
	// Currency is the ISO 4217 code of every amount. Only CurrencyEUR is
	// accepted; the field exists so a later currency never needs a new
	// payload version.
	Currency string `json:"currency"`

	// Number is the display number, for example "RE-20260009", rendered
	// from the seller's number format. For a correction it is the
	// original's number plus "-" and CorrectionSeq. For an imported
	// document it is the original number, unchanged.
	Number string `json:"number"`
	// NumberValue and NumberYear are the counter key behind Number. A
	// correction carries the values of its original.
	NumberValue int64 `json:"number_value"`
	NumberYear  int   `json:"number_year"`
	// CorrectionSeq is a correction's place in its chain, 1 or higher.
	// 0 for every other type.
	CorrectionSeq int `json:"correction_seq,omitempty"`

	// IssueDate is the Ausstellungsdatum, DateLayout.
	IssueDate string `json:"issue_date"`
	// ServicePeriod is the Leistungszeitpunkt. Required for an invoice
	// and a correction, absent for an offer.
	ServicePeriod *ServicePeriod `json:"service_period,omitempty"`
	// DueDate is the payment due date, DateLayout, optional.
	DueDate string `json:"due_date,omitempty"`
	// ValidUntil is an offer's validity date, DateLayout, offers only.
	ValidUntil string `json:"valid_until,omitempty"`

	Seller   SellerSnapshot   `json:"seller"`
	Customer CustomerSnapshot `json:"customer"`
	// CustomerReference is the customer's own reference, for example an
	// order number, printed as given. Optional.
	CustomerReference string `json:"customer_reference,omitempty"`

	// Corrects names the original invoice of a correction, the root of
	// the chain, nil otherwise. A correction replaces the invoice: its
	// lines are the full corrected lines, never a difference.
	Corrects *DocumentReference `json:"corrects,omitempty"`

	Lines            []Line   `json:"lines"`
	DocumentDiscount Discount `json:"document_discount"`
	Totals           Totals   `json:"totals"`

	// TaxNote is the printed note required under Nr. 8 for a tax exempt
	// supply and under section 34a UStDV for a Kleinunternehmer, frozen
	// as text in the document language. Empty when no note applies.
	TaxNote string `json:"tax_note,omitempty"`
	// PaymentTerms and Notes are free text, frozen as printed.
	PaymentTerms string `json:"payment_terms,omitempty"`
	Notes        string `json:"notes,omitempty"`

	// Imported is set for a historic document brought in by the import
	// (issue #81). Its lines may be empty, its totals come from the
	// import row, and Original references the original PDF.
	Imported bool              `json:"imported,omitempty"`
	Original *OriginalDocument `json:"original,omitempty"`
}

// ServicePeriod is a single service date (From equals To) or a period.
type ServicePeriod struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Address is a postal address as printed.
type Address struct {
	Street string `json:"street"`
	// Line2 is an optional second line (c/o, building).
	Line2       string `json:"line2,omitempty"`
	PostalCode  string `json:"postal_code"`
	City        string `json:"city"`
	CountryCode string `json:"country_code"`
}

// SellerSnapshot is the seller profile at issue time.
type SellerSnapshot struct {
	LegalName          string  `json:"legal_name"`
	Address            Address `json:"address"`
	TaxNumber          string  `json:"tax_number,omitempty"`
	VATID              string  `json:"vat_id,omitempty"`
	KleinunternehmerID string  `json:"kleinunternehmer_id,omitempty"`
	BankName           string  `json:"bank_name,omitempty"`
	IBAN               string  `json:"iban,omitempty"`
	BIC                string  `json:"bic,omitempty"`
	Email              string  `json:"email,omitempty"`
	Phone              string  `json:"phone,omitempty"`
	Website            string  `json:"website,omitempty"`
	FooterText         string  `json:"footer_text,omitempty"`
	// LogoSHA256 is the hash of the logo file printed, so a later
	// render proves it uses the same image. Empty without a logo.
	LogoSHA256 string `json:"logo_sha256,omitempty"`
}

// CustomerSnapshot is the customer at issue time.
type CustomerSnapshot struct {
	// ID is the customer's id in solobill, for reference only.
	ID string `json:"id"`
	// Number is the owner's own customer number, optional.
	Number  string  `json:"number,omitempty"`
	Name    string  `json:"name"`
	Address Address `json:"address"`
	VATID   string  `json:"vat_id,omitempty"`
}

// DocumentReference names another issued document.
type DocumentReference struct {
	ID        string `json:"id"`
	Number    string `json:"number"`
	IssueDate string `json:"issue_date"`
}

// OriginalDocument references the original PDF of an imported document.
type OriginalDocument struct {
	FileID string `json:"file_id"`
	SHA256 string `json:"sha256"`
}

// Discount is a percentage or an amount, never both. Both nil is no
// discount.
type Discount struct {
	PercentPermyriad *int32 `json:"percent_permyriad,omitempty"`
	AmountCents      *int64 `json:"amount_cents,omitempty"`
}

// Line is one document line with its precomputed figures.
type Line struct {
	Position            int32    `json:"position"`
	Description         string   `json:"description"`
	QuantityThousandths int64    `json:"quantity_thousandths"`
	Unit                string   `json:"unit"`
	UnitPriceCents      int64    `json:"unit_price_cents"`
	VATRatePermyriad    int32    `json:"vat_rate_permyriad"`
	LineDiscount        Discount `json:"line_discount"`
	// The three figures below are money.LineResult's.
	NetAfterLineDiscountCents  int64 `json:"net_after_line_discount_cents"`
	DocumentDiscountShareCents int64 `json:"document_discount_share_cents"`
	NetAfterAllDiscountsCents  int64 `json:"net_after_all_discounts_cents"`
}

// VATBucket is money.VATBucket with stable JSON names.
type VATBucket struct {
	RatePermyriad int32 `json:"rate_permyriad"`
	NetCents      int64 `json:"net_cents"`
	VATCents      int64 `json:"vat_cents"`
}

// Totals is money.Result's document level figures with stable JSON
// names.
type Totals struct {
	NetTotalCents                 int64       `json:"net_total_cents"`
	DocumentDiscountCents         int64       `json:"document_discount_cents"`
	NetAfterDocumentDiscountCents int64       `json:"net_after_document_discount_cents"`
	VAT                           []VATBucket `json:"vat"`
	VATTotalCents                 int64       `json:"vat_total_cents"`
	GrandTotalCents               int64       `json:"grand_total_cents"`
}

// TotalsFromMoney copies the document level figures of a money.Result.
func TotalsFromMoney(r money.Result) Totals {
	buckets := make([]VATBucket, 0, len(r.VAT))
	for _, b := range r.VAT {
		buckets = append(buckets, VATBucket{
			RatePermyriad: int32(b.RatePermyriad),
			NetCents:      int64(b.NetCents),
			VATCents:      int64(b.VATCents),
		})
	}

	return Totals{
		NetTotalCents:                 int64(r.NetTotalCents),
		DocumentDiscountCents:         int64(r.DocumentDiscountCents),
		NetAfterDocumentDiscountCents: int64(r.NetAfterDocumentDiscountCents),
		VAT:                           buckets,
		VATTotalCents:                 int64(r.VATTotalCents),
		GrandTotalCents:               int64(r.GrandTotalCents),
	}
}

// ApplyLineResults copies each money.LineResult onto the line at the
// same index. It fails when the counts differ.
func ApplyLineResults(lines []Line, results []money.LineResult) error {
	if len(lines) != len(results) {
		return fmt.Errorf("docpayload: %d lines but %d line results", len(lines), len(results))
	}
	for i, r := range results {
		lines[i].NetAfterLineDiscountCents = int64(r.NetAfterLineDiscountCents)
		lines[i].DocumentDiscountShareCents = int64(r.DocumentDiscountShareCents)
		lines[i].NetAfterAllDiscountsCents = int64(r.NetAfterAllDiscountsCents)
	}

	return nil
}

// Marshal encodes p as the bytes that are frozen and hashed. It sets
// nothing on p: the caller sets Version.
func Marshal(p FrozenPayloadV1) ([]byte, error) {
	if p.Version != Version1 {
		return nil, fmt.Errorf("docpayload: version %d, want %d", p.Version, Version1)
	}

	data, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("docpayload: marshal: %w", err)
	}

	return data, nil
}

// ErrUnsupportedVersion is returned by Unmarshal for a payload whose
// version this package does not know.
var ErrUnsupportedVersion = errors.New("docpayload: unsupported payload version")

// Unmarshal decodes a stored version 1 payload strictly: an unknown
// field or a different version is an error, never silently dropped.
func Unmarshal(data []byte) (FrozenPayloadV1, error) {
	var head struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return FrozenPayloadV1{}, fmt.Errorf("docpayload: read version: %w", err)
	}
	if head.Version != Version1 {
		return FrozenPayloadV1{}, fmt.Errorf("%w: %d", ErrUnsupportedVersion, head.Version)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var p FrozenPayloadV1
	if err := dec.Decode(&p); err != nil {
		return FrozenPayloadV1{}, fmt.Errorf("docpayload: decode: %w", err)
	}

	return p, nil
}

// validDate reports whether s is a DateLayout date.
func validDate(s string) bool {
	_, err := time.Parse(DateLayout, s)

	return err == nil
}
