package sellerprofile_test

import (
	"strings"
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/sellerprofile"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

func TestNormalizeTrimsEveryRequiredField(t *testing.T) {
	t.Parallel()

	padded := func(s string) string { return " \t " + s + " \t\n " }

	tests := []struct {
		name string
		set  func(*storage.SellerProfile)
		got  func(storage.SellerProfile) string
	}{
		{"legal_name", func(p *storage.SellerProfile) { p.LegalName = padded("Acme") }, func(p storage.SellerProfile) string { return p.LegalName }},
		{"street", func(p *storage.SellerProfile) { p.Street = padded("Main 1") }, func(p storage.SellerProfile) string { return p.Street }},
		{"postal_code", func(p *storage.SellerProfile) { p.PostalCode = padded("12345") }, func(p storage.SellerProfile) string { return p.PostalCode }},
		{"city", func(p *storage.SellerProfile) { p.City = padded("Town") }, func(p storage.SellerProfile) string { return p.City }},
		{"country_code", func(p *storage.SellerProfile) { p.CountryCode = padded("de") }, func(p storage.SellerProfile) string { return p.CountryCode }},
	}
	want := map[string]string{"legal_name": "Acme", "street": "Main 1", "postal_code": "12345", "city": "Town", "country_code": "DE"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var p storage.SellerProfile
			tt.set(&p)
			if got := tt.got(sellerprofile.Normalize(p)); got != want[tt.name] {
				t.Errorf("Normalize() %s = %q, want %q", tt.name, got, want[tt.name])
			}
		})
	}
}

func TestNormalizeVATID(t *testing.T) {
	t.Parallel()

	p := sellerprofile.Normalize(storage.SellerProfile{VATID: ptr(" de 123 456 789 ")})
	if p.VATID == nil || *p.VATID != "DE123456789" {
		t.Errorf("VATID = %v, want DE123456789", p.VATID)
	}
}

// around puts r between two letters.
func around(r rune) string { return "a" + string(r) + "b" }

func TestCheckFormatControlAndFormatCharacters(t *testing.T) {
	t.Parallel()

	setters := map[string]func(*storage.SellerProfile, string){
		"legal_name":          func(p *storage.SellerProfile, v string) { p.LegalName = v },
		"street":              func(p *storage.SellerProfile, v string) { p.Street = v },
		"postal_code":         func(p *storage.SellerProfile, v string) { p.PostalCode = v },
		"city":                func(p *storage.SellerProfile, v string) { p.City = v },
		"address_line2":       func(p *storage.SellerProfile, v string) { p.AddressLine2 = &v },
		"tax_number":          func(p *storage.SellerProfile, v string) { p.TaxNumber = &v },
		"kleinunternehmer_id": func(p *storage.SellerProfile, v string) { p.KleinunternehmerID = &v },
		"bank_name":           func(p *storage.SellerProfile, v string) { p.BankName = &v },
		"phone":               func(p *storage.SellerProfile, v string) { p.Phone = &v },
		"footer_text":         func(p *storage.SellerProfile, v string) { p.FooterText = &v },
	}
	// Code points, so the source holds no invisible character.
	bad := map[string]rune{
		"NUL":          0x0000,
		"tab":          0x0009,
		"newline":      0x000A,
		"carriage":     0x000D,
		"escape":       0x001B,
		"DEL":          0x007F,
		"C1":           0x0085,
		"LRE":          0x202A,
		"RLO":          0x202E,
		"isolate":      0x2066,
		"isolate end":  0x2069,
		"zero width":   0x200B,
		"joiner":       0x200D,
		"soft hyphen":  0x00AD,
		"byte order":   0xFEFF,
		"language tag": 0xE0001,
	}

	for field, set := range setters {
		for name, r := range bad {
			// The footer text alone may hold a newline.
			if field == "footer_text" && name == "newline" {
				continue
			}
			t.Run(field+" "+name, func(t *testing.T) {
				t.Parallel()

				p := complete()
				set(&p, around(r))
				if _, ok := sellerprofile.CheckFormat(p)[field]; !ok {
					t.Errorf("CheckFormat() accepted %U in %s", r, field)
				}
			})
		}
	}

	t.Run("footer text keeps newlines", func(t *testing.T) {
		t.Parallel()

		p := complete()
		p.FooterText = ptr("line one\nline two")
		if got := sellerprofile.CheckFormat(p); len(got) != 0 {
			t.Errorf("CheckFormat() = %v, want none", got)
		}
	})

	t.Run("footer text refuses a bidi override", func(t *testing.T) {
		t.Parallel()

		p := complete()
		p.FooterText = ptr("line one\n" + around(0x202E))
		if _, ok := sellerprofile.CheckFormat(p)["footer_text"]; !ok {
			t.Error("CheckFormat() accepted a bidi override in footer_text")
		}
	})

	t.Run("the other fields refuse them too", func(t *testing.T) {
		t.Parallel()

		email, website, vatID := "a"+string(rune(0x202E))+"@example.test", "https://exa"+string(rune(0x200B))+"mple.test", "DE12345\x006789"
		for field, mutate := range map[string]func(*storage.SellerProfile){
			"email":   func(p *storage.SellerProfile) { p.Email = &email },
			"website": func(p *storage.SellerProfile) { p.Website = &website },
			"vat_id":  func(p *storage.SellerProfile) { p.VATID = &vatID },
		} {
			p := complete()
			mutate(&p)
			if _, ok := sellerprofile.CheckFormat(p)[field]; !ok {
				t.Errorf("CheckFormat() accepted a bad character in %s", field)
			}
		}
	})

	t.Run("plain unicode letters pass", func(t *testing.T) {
		t.Parallel()

		p := complete()
		p.LegalName = "Müller & Söhne Ärzte"
		if got := sellerprofile.CheckFormat(p); len(got) != 0 {
			t.Errorf("CheckFormat() = %v, want none", got)
		}
	})
}

func TestCheckFormatCountry(t *testing.T) {
	t.Parallel()

	for _, code := range []string{"DE", "AT", "CH", "US", "ZW", "AD", "FR"} {
		p := complete()
		p.CountryCode = code
		if got := sellerprofile.CheckFormat(p); len(got) != 0 {
			t.Errorf("country %s refused: %v", code, got)
		}
	}
	for _, code := range []string{"XX", "XK", "ZZ", "AA", "UK", "D", "de"} {
		p := complete()
		p.CountryCode = code
		if _, ok := sellerprofile.CheckFormat(p)["country_code"]; !ok {
			t.Errorf("country %q accepted", code)
		}
	}
}

func TestCountryListHas249Codes(t *testing.T) {
	t.Parallel()

	count := 0
	for a := 'A'; a <= 'Z'; a++ {
		for b := 'A'; b <= 'Z'; b++ {
			p := complete()
			p.CountryCode = string(a) + string(b)
			if _, bad := sellerprofile.CheckFormat(p)["country_code"]; !bad {
				count++
			}
		}
	}
	if count != 249 {
		t.Errorf("accepted country codes = %d, want 249", count)
	}
}

func TestCheckFormatIBAN(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		iban string
		ok   bool
	}{
		{"DE", "DE89370400440532013000", true},
		{"GB", "GB82WEST12345698765432", true},
		{"NL", "NL91ABNA0417164300", true},
		{"FR", "FR1420041010050500013M02606", true},
		{"one character too many for DE", "DE543704004405320130001", false},
		{"one character too few for DE", "DE8937040044053201300", false},
		{"unknown country accepted by checksum and range", "BR1800360305000010009795493C1", true},
		{"unknown country with a wrong checksum", "BR1900360305000010009795493C1", false},
		{"too short", "DE89", false},
		{"too long", "DE89" + strings.Repeat("0", 31), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := complete()
			p.IBAN = ptr(tt.iban)
			_, bad := sellerprofile.CheckFormat(p)["iban"]
			if bad == tt.ok {
				t.Errorf("IBAN %q: refused = %v, want %v", tt.iban, bad, !tt.ok)
			}
		})
	}
}

func TestCheckFormatEmailAndWebsite(t *testing.T) {
	t.Parallel()

	emails := map[string]bool{
		"office@example.test": true,
		"a@b":                 false,
		"a@.test":             false,
		"a@test.":             false,
		strings.Repeat("a", 250) + "@example.test": false,
		"A <a@example.test>":                       false,
	}
	for email, ok := range emails {
		p := complete()
		p.Email = ptr(email)
		if _, bad := sellerprofile.CheckFormat(p)["email"]; bad == ok {
			t.Errorf("email %q: refused = %v, want %v", email, bad, !ok)
		}
	}

	websites := map[string]bool{
		"https://example.test":                              true,
		"http://example.test/path?x=1":                      true,
		"https://user@example.test":                         false,
		"https://user:pw@example.test":                      false,
		"https://":                                          false,
		"example.test":                                      false,
		"ftp://example.test":                                false,
		"https://example.test/" + strings.Repeat("a", 2048): false,
	}
	for website, ok := range websites {
		p := complete()
		p.Website = ptr(website)
		if _, bad := sellerprofile.CheckFormat(p)["website"]; bad == ok {
			t.Errorf("website %q: refused = %v, want %v", website, bad, !ok)
		}
	}
}

func TestCheckFormatVATID(t *testing.T) {
	t.Parallel()

	for vatID, ok := range map[string]bool{
		"DE123456789":     true,
		"ATU12345678":     true,
		"FR12345678901":   true,
		"DE12":            false,
		"DE1234567":       false,
		"1234567890":      false,
		"DE1234567890123": false,
		"DE12345-6789":    false,
	} {
		p := complete()
		p.VATID = ptr(vatID)
		if _, bad := sellerprofile.CheckFormat(p)["vat_id"]; bad == ok {
			t.Errorf("vat id %q: refused = %v, want %v", vatID, bad, !ok)
		}
	}
}

func TestCheckFormatRequiresRegimeAndLanguage(t *testing.T) {
	t.Parallel()

	p := complete()
	p.DefaultVATRegime = ""
	p.DefaultLanguage = ""
	got := sellerprofile.CheckFormat(p)
	if got["default_vat_regime"] != "required" || got["default_language"] != "required" {
		t.Errorf("CheckFormat() = %v, want both required", got)
	}
}

func TestValidateUnknownRegimeKey(t *testing.T) {
	t.Parallel()

	got := sellerprofile.Validate(complete(), storage.VATRegime("other"))
	if _, ok := got["seller.default_vat_regime"]; !ok {
		t.Errorf("Validate() = %v, want seller.default_vat_regime", got)
	}
}
