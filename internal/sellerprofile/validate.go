// Package sellerprofile holds the rules around the seller profile: the
// completeness check issue time runs (Validate), and the checks and the
// normalisation a save runs (Normalize, CheckFormat, CheckNumberFormats).
// No database, no clock, no I/O.
package sellerprofile

import (
	"math/big"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/ostendorf-cloud/solobill/internal/numbering"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// Validate reports what is missing for the profile to be used on an
// issued document under regime. The result maps a JSON path under
// "seller." to a reason. Empty means complete.
//
// Section 14 Abs. 4 Nr. 1 and 2 UStG: the supplier's name and full
// address and a Steuernummer or USt-IdNr. Section 34a UStDV lets a
// Kleinunternehmer carry the Kleinunternehmer-Identifikationsnummer
// instead of both.
func Validate(p storage.SellerProfile, regime storage.VATRegime) map[string]string {
	problems := map[string]string{}

	require := func(key, value string) {
		if strings.TrimSpace(value) == "" {
			problems["seller."+key] = "required"
		}
	}
	require("legal_name", p.LegalName)
	require("street", p.Street)
	require("postal_code", p.PostalCode)
	require("city", p.City)
	require("country_code", p.CountryCode)

	switch regime {
	case storage.VATRegimeStandard:
		if blank(p.TaxNumber) && blank(p.VATID) {
			problems["seller.tax_number"] = "a Steuernummer or USt-IdNr is required"
		}
	case storage.VATRegimeKleinunternehmer:
		if blank(p.TaxNumber) && blank(p.VATID) && blank(p.KleinunternehmerID) {
			problems["seller.tax_number"] = "a Steuernummer, USt-IdNr or Kleinunternehmer-Identifikationsnummer is required"
		}
	default:
		problems["seller.default_vat_regime"] = "unknown VAT regime"
	}

	return problems
}

func blank(s *string) bool { return s == nil || strings.TrimSpace(*s) == "" }

// Normalize trims every text field, folds an empty optional field to nil,
// upper cases the country code, the IBAN and the BIC and removes the
// spaces of the IBAN. A save stores the result, so every consumer reads
// one spelling.
func Normalize(p storage.SellerProfile) storage.SellerProfile {
	p.LegalName = strings.TrimSpace(p.LegalName)
	p.Street = strings.TrimSpace(p.Street)
	p.PostalCode = strings.TrimSpace(p.PostalCode)
	p.City = strings.TrimSpace(p.City)
	p.CountryCode = strings.ToUpper(strings.TrimSpace(p.CountryCode))

	p.AddressLine2 = tidy(p.AddressLine2, nil)
	p.TaxNumber = tidy(p.TaxNumber, nil)
	p.VATID = tidy(p.VATID, func(s string) string {
		return strings.ToUpper(strings.Join(strings.Fields(s), ""))
	})
	p.KleinunternehmerID = tidy(p.KleinunternehmerID, nil)
	p.BankName = tidy(p.BankName, nil)
	p.IBAN = tidy(p.IBAN, func(s string) string {
		return strings.ToUpper(strings.Join(strings.Fields(s), ""))
	})
	p.BIC = tidy(p.BIC, strings.ToUpper)
	p.Email = tidy(p.Email, nil)
	p.Phone = tidy(p.Phone, nil)
	p.Website = tidy(p.Website, nil)
	p.FooterText = tidy(p.FooterText, nil)

	return p
}

func tidy(s *string, fold func(string) string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil
	}
	if fold != nil {
		v = fold(v)
	}

	return &v
}

const (
	maxShort = 200
	maxLong  = 2000
	// maxEmail is the longest address RFC 5321 allows.
	maxEmail = 254
	// maxWebsite is the usual practical URL limit.
	maxWebsite = 2048
)

var (
	ibanPattern  = regexp.MustCompile(`^[A-Z]{2}[0-9]{2}[A-Z0-9]{11,30}$`)
	bicPattern   = regexp.MustCompile(`^[A-Z]{6}[A-Z0-9]{2}([A-Z0-9]{3})?$`)
	vatIDPattern = regexp.MustCompile(`^[A-Z]{2}[A-Z0-9]{8,12}$`)
)

// hasBadRune reports a control character (C0 including NUL, DEL, C1) or a
// Unicode format character (Cf: bidi overrides and isolates, zero width
// joiners, the soft hyphen and so on). A newline passes when newline is
// true, which only the footer text allows.
func hasBadRune(s string, newline bool) bool {
	for _, r := range s {
		if r == '\n' && newline {
			continue
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return true
		}
	}

	return false
}

// CheckFormat reports malformed values of a normalised profile. The keys
// are the JSON names of the profile, without a prefix. An empty optional
// value is fine: completeness is Validate's job at issue time. The values
// are not Unicode normalised (NFC): golang.org/x/text is not a dependency.
func CheckFormat(p storage.SellerProfile) map[string]string {
	problems := map[string]string{}

	text := func(key, value string, limit int, newline bool) {
		switch {
		case hasBadRune(value, newline):
			problems[key] = "must not contain control or formatting characters"
		case len([]rune(value)) > limit:
			problems[key] = "too long"
		}
	}
	textPtr := func(key string, value *string, limit int, newline bool) {
		if value != nil {
			text(key, *value, limit, newline)
		}
	}
	text("legal_name", p.LegalName, maxShort, false)
	text("street", p.Street, maxShort, false)
	text("postal_code", p.PostalCode, maxShort, false)
	text("city", p.City, maxShort, false)
	textPtr("address_line2", p.AddressLine2, maxShort, false)
	textPtr("tax_number", p.TaxNumber, maxShort, false)
	textPtr("vat_id", p.VATID, maxShort, false)
	textPtr("kleinunternehmer_id", p.KleinunternehmerID, maxShort, false)
	textPtr("bank_name", p.BankName, maxShort, false)
	textPtr("phone", p.Phone, maxShort, false)
	textPtr("email", p.Email, maxEmail, false)
	textPtr("website", p.Website, maxWebsite, false)
	textPtr("footer_text", p.FooterText, maxLong, true)

	// Steuernummer and Kleinunternehmer-Identifikationsnummer have no
	// format check: the contract asks for none. The USt-IdNr gets a loose
	// shape check only: a country prefix and 8 to 12 letters or digits,
	// which covers every EU number, so "DE12" is refused.
	if p.VATID != nil && problems["vat_id"] == "" && !vatIDPattern.MatchString(*p.VATID) {
		problems["vat_id"] = "not a valid USt-IdNr: two letters and 8 to 12 letters or digits, such as DE123456789"
	}

	if p.CountryCode != "" && !validCountry(p.CountryCode) {
		problems["country_code"] = "must be an ISO 3166-1 alpha-2 code such as DE"
	}

	switch p.DefaultVATRegime {
	case storage.VATRegimeStandard, storage.VATRegimeKleinunternehmer:
	case "":
		problems["default_vat_regime"] = "required"
	default:
		problems["default_vat_regime"] = "must be standard or kleinunternehmer"
	}

	switch p.DefaultLanguage {
	case storage.DocumentLanguageGerman, storage.DocumentLanguageEnglish:
	case "":
		problems["default_language"] = "required"
	default:
		problems["default_language"] = "must be de or en"
	}

	if p.IBAN != nil && !validIBAN(*p.IBAN) {
		problems["iban"] = "not a valid IBAN"
	}
	if p.BIC != nil && !bicPattern.MatchString(*p.BIC) {
		problems["bic"] = "not a valid BIC"
	}
	if p.Email != nil && problems["email"] == "" {
		if reason := checkEmail(*p.Email); reason != "" {
			problems["email"] = reason
		}
	}
	if p.Website != nil && problems["website"] == "" {
		if reason := checkWebsite(*p.Website); reason != "" {
			problems["website"] = reason
		}
	}

	return problems
}

// checkEmail returns a reason, or "" for an acceptable address: a bare
// address (no display name) whose domain has a dot, at most 254 characters.
func checkEmail(email string) string {
	if len(email) > maxEmail {
		return "too long"
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return "not a valid email address"
	}
	_, domain, _ := strings.Cut(email, "@")
	if !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return "not a valid email address"
	}

	return ""
}

// checkWebsite returns a reason, or "" for an http or https URL with a
// host, no user info and at most 2048 characters.
func checkWebsite(website string) string {
	if len(website) > maxWebsite {
		return "too long"
	}
	u, err := url.Parse(website)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
		return "must be an http or https URL with a host and no user info"
	}

	return ""
}

// ibanLengths is the IBAN length per country for the SEPA area and the
// rest of Europe. A country missing here is accepted when the shape and
// the mod 97 checksum hold and the length is 15 to 34: the registry has
// many more countries, and a wrong table entry would refuse a real
// account, while the checksum already catches typos.
var ibanLengths = map[string]int{
	"AD": 24, "AT": 20, "BE": 16, "BG": 22, "CH": 21, "CY": 28, "CZ": 24, "DE": 22,
	"DK": 18, "EE": 20, "ES": 24, "FI": 18, "FR": 27, "GB": 22, "GR": 27, "HR": 21,
	"HU": 28, "IE": 22, "IS": 26, "IT": 27, "LI": 21, "LT": 20, "LU": 20, "LV": 21,
	"MC": 27, "MT": 31, "NL": 18, "NO": 15, "PL": 28, "PT": 25, "RO": 24, "SE": 24,
	"SI": 19, "SK": 24, "SM": 27, "VA": 22,
}

// validIBAN checks the shape, the length of a known country and the
// ISO 7064 mod 97 checksum of a normalised IBAN (upper case, no spaces).
func validIBAN(iban string) bool {
	if len(iban) > 34 || !ibanPattern.MatchString(iban) {
		return false
	}
	if want, known := ibanLengths[iban[:2]]; known && len(iban) != want {
		return false
	}

	rearranged := iban[4:] + iban[:4]
	var digits strings.Builder
	for _, c := range rearranged {
		if c >= 'A' && c <= 'Z' {
			digits.WriteString(big.NewInt(int64(c-'A') + 10).String())
		} else {
			digits.WriteRune(c)
		}
	}

	n, ok := new(big.Int).SetString(digits.String(), 10)
	if !ok {
		return false
	}

	return new(big.Int).Mod(n, big.NewInt(97)).Int64() == 1
}

// NumberFormatTypes are the document types that carry a template.
var NumberFormatTypes = []storage.DocumentType{storage.DocumentTypeInvoice, storage.DocumentTypeOffer}

// WithDefaultFormats returns formats with numbering.Defaults filled in
// for a missing or empty type. The input map is not changed.
func WithDefaultFormats(formats map[storage.DocumentType]string) map[storage.DocumentType]string {
	out := make(map[storage.DocumentType]string, len(NumberFormatTypes))
	for _, docType := range NumberFormatTypes {
		if v := formats[docType]; v != "" {
			out[docType] = v
		} else {
			out[docType] = numbering.Defaults[string(docType)]
		}
	}

	return out
}

// CheckNumberFormats checks the templates of next, which must already
// carry a value for every type (see WithDefaultFormats). Each is parsed
// with numbering.Parse, the set with numbering.ValidateSet. For a type in
// issued, the change from old is checked with numbering.ValidateChange.
// The keys are "number_formats.invoice" and "number_formats.offer", and
// "number_formats" for a failure of the set.
func CheckNumberFormats(next, old map[storage.DocumentType]string, issued map[storage.DocumentType]bool) map[string]string {
	problems := map[string]string{}
	parsed := map[string]numbering.Template{}

	for _, docType := range NumberFormatTypes {
		key := "number_formats." + string(docType)

		tpl, err := numbering.Parse(next[docType])
		if err != nil {
			problems[key] = reason(err)

			continue
		}
		parsed[string(docType)] = tpl

		if !issued[docType] {
			continue
		}
		oldRaw := old[docType]
		if oldRaw == "" {
			oldRaw = numbering.Defaults[string(docType)]
		}
		oldTpl, err := numbering.Parse(oldRaw)
		if err != nil {
			// A stored template that no longer parses cannot anchor a
			// change check; the parse of the new one above still ran.
			continue
		}
		if err := numbering.ValidateChange(oldTpl, tpl); err != nil {
			problems[key] = reason(err)
		}
	}

	if len(problems) == 0 && len(parsed) == len(NumberFormatTypes) {
		if err := numbering.ValidateSet(parsed); err != nil {
			problems["number_formats"] = reason(err)
		}
	}

	return problems
}

// reason strips the package's own sentinel prefix from an error message.
func reason(err error) string {
	return strings.TrimPrefix(err.Error(), numbering.ErrTemplate.Error()+": ")
}
