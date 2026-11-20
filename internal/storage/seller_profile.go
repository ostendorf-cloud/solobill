package storage

import (
	"time"

	"github.com/google/uuid"
)

// SellerProfile is the supplier data section 14 Abs. 4 Nr. 1 and 2 UStG
// and section 34a UStDV require on every invoice, one row per tenant.
// Issue #68 (I2) creates the table and the implementation; this type is
// the year end contract (issue #67) every other issue builds against.
//
// Every optional field is nil until known. Completeness is checked at
// issue time by the validation function issue #68 exports (see
// docs/concepts/solobill.md, "Seller data validation"), never by this
// package.
type SellerProfile struct {
	TenantID uuid.UUID
	// LegalName is the full name or company name (Nr. 1).
	LegalName string
	// Street, PostalCode, City and CountryCode are the full address
	// (Nr. 1). CountryCode is ISO 3166-1 alpha-2.
	Street string
	// AddressLine2 is an optional second address line (c/o, building).
	AddressLine2 *string
	PostalCode   string
	City         string
	CountryCode  string
	// TaxNumber is the Steuernummer (Nr. 2).
	TaxNumber *string
	// VATID is the USt-IdNr (Nr. 2).
	VATID *string
	// KleinunternehmerID is the Kleinunternehmer-Identifikationsnummer
	// section 34a UStDV accepts in place of the other two.
	KleinunternehmerID *string
	// DefaultVATRegime is the regime a new draft starts with. The regime
	// that actually applies on a date comes from VATRegimePeriod, never
	// from this default.
	DefaultVATRegime VATRegime
	// DefaultLanguage is the language a new customer starts with.
	DefaultLanguage DocumentLanguage
	BankName        *string
	IBAN            *string
	BIC             *string
	Email           *string
	Phone           *string
	Website         *string
	// FooterText is free text printed in the document footer.
	FooterText *string
	// NumberFormats maps DocumentTypeInvoice and DocumentTypeOffer to the
	// template their display number is rendered from, so solobill
	// continues the format the imported 2026 documents already use. The
	// owner's format "RE-{year}{seq:4}" renders number 9 of 2026 as
	// "RE-20260009". A correction has no template of its own: its
	// number is the corrected invoice's number plus "-1", "-2" and so
	// on. The grammar and the validation are internal/numbering: every
	// template parses, and the prefixes of the types never overlap
	// (numbering.ValidateSet). A missing type uses numbering.Defaults.
	NumberFormats map[DocumentType]string
	// LogoFileID references the logo in files. The column lands with
	// issue #75 (I9), not with issue #68.
	LogoFileID *uuid.UUID
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// VATRegimePeriod is the VAT regime that applies from ValidFrom onward,
// until the next effective period of the same tenant starts. One row per
// change, so a switch in the middle of a year (the Kleinunternehmer
// limit of section 19 Abs. 1 UStG exceeded) is one more row, not an
// edit. The table is append-only.
//
// A wrong row is corrected by a new row that names it in SupersedesID.
// A superseded row no longer counts. The correcting row carries its own
// ValidFrom, which may equal the wrong row's or differ from it.
//
// The regime on a date is the regime of the latest effective period
// whose ValidFrom is on or before that date. A date before the first
// effective period has no regime, and the caller refuses to proceed.
type VATRegimePeriod struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	ValidFrom time.Time
	Regime    VATRegime
	// SupersedesID names the row this row corrects, nil otherwise. A row
	// is superseded at most once.
	SupersedesID *uuid.UUID
	// Note is the owner's own reason, for example "Umsatzgrenze am
	// 2026-10-14 überschritten".
	Note      *string
	CreatedAt time.Time
}
