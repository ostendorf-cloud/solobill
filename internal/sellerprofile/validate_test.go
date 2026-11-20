package sellerprofile_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/sellerprofile"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

func ptr(s string) *string { return &s }

func complete() storage.SellerProfile {
	return storage.SellerProfile{
		LegalName:        "Example Trading",
		Street:           "Main Street 1",
		PostalCode:       "12345",
		City:             "Example City",
		CountryCode:      "DE",
		DefaultVATRegime: storage.VATRegimeKleinunternehmer,
		DefaultLanguage:  storage.DocumentLanguageGerman,
	}
}

func keys(m map[string]string) string {
	var out []string
	for k := range m {
		out = append(out, k)
	}

	slices.Sort(out)

	return strings.Join(out, ",")
}

func TestValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*storage.SellerProfile)
		regime  storage.VATRegime
		wantKey string
	}{
		{"standard with tax number", func(p *storage.SellerProfile) { p.TaxNumber = ptr("12/345/67890") }, storage.VATRegimeStandard, ""},
		{"standard with vat id", func(p *storage.SellerProfile) { p.VATID = ptr("DE123456789") }, storage.VATRegimeStandard, ""},
		{"standard without tax id", func(*storage.SellerProfile) {}, storage.VATRegimeStandard, "seller.tax_number"},
		{
			"standard refuses the kleinunternehmer id alone",
			func(p *storage.SellerProfile) { p.KleinunternehmerID = ptr("DE-K-1") },
			storage.VATRegimeStandard, "seller.tax_number",
		},
		{"kleinunternehmer with its own id", func(p *storage.SellerProfile) { p.KleinunternehmerID = ptr("DE-K-1") }, storage.VATRegimeKleinunternehmer, ""},
		{"kleinunternehmer with tax number", func(p *storage.SellerProfile) { p.TaxNumber = ptr("12/345/67890") }, storage.VATRegimeKleinunternehmer, ""},
		{"kleinunternehmer with vat id", func(p *storage.SellerProfile) { p.VATID = ptr("DE123456789") }, storage.VATRegimeKleinunternehmer, ""},
		{"kleinunternehmer without any id", func(*storage.SellerProfile) {}, storage.VATRegimeKleinunternehmer, "seller.tax_number"},
		{
			"blank tax number counts as missing",
			func(p *storage.SellerProfile) { p.TaxNumber = ptr("  ") },
			storage.VATRegimeKleinunternehmer, "seller.tax_number",
		},
		{
			"missing name",
			func(p *storage.SellerProfile) { p.LegalName = ""; p.TaxNumber = ptr("1") },
			storage.VATRegimeStandard, "seller.legal_name",
		},
		{"missing city", func(p *storage.SellerProfile) { p.City = " "; p.TaxNumber = ptr("1") }, storage.VATRegimeStandard, "seller.city"},
		{"unknown regime", func(p *storage.SellerProfile) { p.TaxNumber = ptr("1") }, storage.VATRegime("other"), "seller.default_vat_regime"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := complete()
			tt.mutate(&p)

			got := sellerprofile.Validate(p, tt.regime)
			if tt.wantKey == "" {
				if len(got) != 0 {
					t.Fatalf("Validate() = %v, want complete", got)
				}

				return
			}
			if keys(got) != tt.wantKey {
				t.Errorf("Validate() keys = %q, want %q", keys(got), tt.wantKey)
			}
		})
	}
}

func TestValidateEmptyProfileNamesEveryRequiredField(t *testing.T) {
	t.Parallel()

	got := sellerprofile.Validate(storage.SellerProfile{}, storage.VATRegimeStandard)
	want := "seller.city,seller.country_code,seller.legal_name,seller.postal_code,seller.street,seller.tax_number"
	if keys(got) != want {
		t.Errorf("keys = %q, want %q", keys(got), want)
	}
}

func TestNormalize(t *testing.T) {
	t.Parallel()

	p := complete()
	p.CountryCode = " de "
	p.IBAN = ptr("de89 3704 0044 0532 0130 00")
	p.BIC = ptr(" cobadeff ")
	p.TaxNumber = ptr("   ")
	p.Phone = ptr(" 0123 ")

	got := sellerprofile.Normalize(p)

	if got.CountryCode != "DE" || *got.IBAN != "DE89370400440532013000" || *got.BIC != "COBADEFF" {
		t.Errorf("Normalize() = %q %q %q", got.CountryCode, *got.IBAN, *got.BIC)
	}
	if got.TaxNumber != nil {
		t.Errorf("blank tax number = %q, want nil", *got.TaxNumber)
	}
	if got.Phone == nil || *got.Phone != "0123" {
		t.Errorf("Phone = %v, want trimmed", got.Phone)
	}
}

func TestCheckFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*storage.SellerProfile)
		wantKey string
	}{
		{"valid", func(p *storage.SellerProfile) { p.IBAN = ptr("DE89370400440532013000") }, ""},
		{"valid empty profile values", func(p *storage.SellerProfile) { p.CountryCode = "" }, ""},
		{"iban checksum", func(p *storage.SellerProfile) { p.IBAN = ptr("DE89370400440532013001") }, "iban"},
		{"iban shape", func(p *storage.SellerProfile) { p.IBAN = ptr("12345") }, "iban"},
		{"bic", func(p *storage.SellerProfile) { p.BIC = ptr("XX") }, "bic"},
		{"bic valid 11", func(p *storage.SellerProfile) { p.BIC = ptr("COBADEFFXXX") }, ""},
		{"country", func(p *storage.SellerProfile) { p.CountryCode = "DEU" }, "country_code"},
		{"regime", func(p *storage.SellerProfile) { p.DefaultVATRegime = "x" }, "default_vat_regime"},
		{"language", func(p *storage.SellerProfile) { p.DefaultLanguage = "fr" }, "default_language"},
		{"email", func(p *storage.SellerProfile) { p.Email = ptr("not an email") }, "email"},
		{"email with display name", func(p *storage.SellerProfile) { p.Email = ptr("A <a@example.test>") }, "email"},
		{"website scheme", func(p *storage.SellerProfile) { p.Website = ptr("javascript:alert(1)") }, "website"},
		{"website ok", func(p *storage.SellerProfile) { p.Website = ptr("https://example.test") }, ""},
		{"too long", func(p *storage.SellerProfile) { p.LegalName = strings.Repeat("x", 201) }, "legal_name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := complete()
			tt.mutate(&p)

			if got := keys(sellerprofile.CheckFormat(p)); got != tt.wantKey {
				t.Errorf("CheckFormat() keys = %q, want %q", got, tt.wantKey)
			}
		})
	}
}

func TestCheckNumberFormats(t *testing.T) {
	t.Parallel()

	inv, off := storage.DocumentTypeInvoice, storage.DocumentTypeOffer
	defaults := sellerprofile.WithDefaultFormats(nil)

	tests := []struct {
		name    string
		next    map[storage.DocumentType]string
		issued  map[storage.DocumentType]bool
		wantKey string
	}{
		{"defaults", defaults, nil, ""},
		{"owner format", map[storage.DocumentType]string{inv: "RE-{year}{seq:4}", off: "AN-{year}{seq:4}"}, nil, ""},
		{"unknown token", map[storage.DocumentType]string{inv: "RE-{month}{seq}", off: defaults[off]}, nil, "number_formats.invoice"},
		{"space", map[storage.DocumentType]string{inv: "RE {year}{seq}", off: defaults[off]}, nil, "number_formats.invoice"},
		{"seq digit zero", map[storage.DocumentType]string{inv: defaults[inv], off: "AN-{year}{seq:0}"}, nil, "number_formats.offer"},
		{"seq two digits", map[storage.DocumentType]string{inv: defaults[inv], off: "AN-{year}{seq:10}"}, nil, "number_formats.offer"},
		{"leading dash", map[storage.DocumentType]string{inv: "-{year}{seq}", off: defaults[off]}, nil, "number_formats.invoice"},
		{"double dot", map[storage.DocumentType]string{inv: "RE..{year}{seq}", off: defaults[off]}, nil, "number_formats.invoice"},
		{"double slash", map[storage.DocumentType]string{inv: "RE//{year}{seq}", off: defaults[off]}, nil, "number_formats.invoice"},
		{"no seq", map[storage.DocumentType]string{inv: "RE-{year}", off: defaults[off]}, nil, "number_formats.invoice"},
		{"no year", map[storage.DocumentType]string{inv: "RE-{seq}", off: defaults[off]}, nil, "number_formats.invoice"},
		{"overlapping prefixes", map[storage.DocumentType]string{inv: "RE-{year}{seq:4}", off: "RE-{year}{seq}"}, nil, "number_formats"},
		{"prefix of the other", map[storage.DocumentType]string{inv: "R{year}{seq}", off: "RE-{year}{seq}"}, nil, "number_formats"},
		{
			"prefix change after issue",
			map[storage.DocumentType]string{inv: "RG-{year}{seq:4}", off: defaults[off]},
			map[storage.DocumentType]bool{inv: true},
			"",
		},
		{
			"token change after issue",
			map[storage.DocumentType]string{inv: "RE-{year}{seq:5}", off: defaults[off]},
			map[storage.DocumentType]bool{inv: true},
			"number_formats.invoice",
		},
		{
			"overlapping prefix change after issue",
			map[storage.DocumentType]string{inv: "RE-X-{year}{seq:4}", off: defaults[off]},
			map[storage.DocumentType]bool{inv: true},
			"number_formats.invoice",
		},
		{
			"token change before issue is free",
			map[storage.DocumentType]string{inv: "RE-{year}{seq:5}", off: defaults[off]},
			map[storage.DocumentType]bool{off: true},
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := sellerprofile.CheckNumberFormats(tt.next, defaults, tt.issued)
			if keys(got) != tt.wantKey {
				t.Errorf("CheckNumberFormats() = %v, want key %q", got, tt.wantKey)
			}
		})
	}
}

func TestWithDefaultFormatsFillsMissingTypes(t *testing.T) {
	t.Parallel()

	got := sellerprofile.WithDefaultFormats(map[storage.DocumentType]string{storage.DocumentTypeInvoice: "X-{year}{seq}"})
	if got[storage.DocumentTypeInvoice] != "X-{year}{seq}" || got[storage.DocumentTypeOffer] != "AN-{year}{seq:4}" {
		t.Errorf("WithDefaultFormats() = %v", got)
	}
}
