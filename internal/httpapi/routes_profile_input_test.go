package httpapi_test

import (
	"net/http"
	"testing"
)

// TestProfilePutTrimsEveryRequiredField sends leading and trailing spaces
// and tabs on every required field and reads the trimmed values back.
func TestProfilePutTrimsEveryRequiredField(t *testing.T) {
	t.Parallel()

	for _, field := range []string{"legal_name", "street", "postal_code", "city", "country_code"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()

			f := newProfileFixture(t)
			body := validProfileBody()
			want, _ := body[field].(string)
			body[field] = " \t " + want + " \t "

			rec := f.do(t, f.owner, http.MethodPut, "/api/v1/profile", body)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if got := profileDecodeMap(t, rec)[field]; got != want {
				t.Errorf("%s = %q, want %q", field, got, want)
			}
		})
	}
}

// TestProfilePutRefusesControlAndFormatCharacters sends one bad character
// in a field of each class: required text, optional text, contact fields.
func TestProfilePutRefusesControlAndFormatCharacters(t *testing.T) {
	t.Parallel()

	bidi := "a" + string(rune(0x202E)) + "b"
	for field, value := range map[string]string{
		"legal_name":    "a\x00b",
		"street":        bidi,
		"city":          "a\tb",
		"address_line2": "a\nb",
		"bank_name":     bidi,
		"footer_text":   bidi,
		"phone":         "a\x1bb",
	} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()

			f := newProfileFixture(t)
			body := validProfileBody()
			body[field] = value

			rec := f.do(t, f.owner, http.MethodPut, "/api/v1/profile", body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
			}
			if _, ok := profileErrorDetails(t, rec)[field]; !ok {
				t.Errorf("details = %v, want key %q", profileErrorDetails(t, rec), field)
			}
		})
	}
}

func TestProfilePutKeepsNewlinesInTheFooterText(t *testing.T) {
	t.Parallel()

	f := newProfileFixture(t)
	body := validProfileBody()
	body["footer_text"] = "line one\nline two"

	rec := f.do(t, f.owner, http.MethodPut, "/api/v1/profile", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := profileDecodeMap(t, rec)["footer_text"]; got != "line one\nline two" {
		t.Errorf("footer_text = %q", got)
	}
}

// TestProfilePutRequiresRegimeAndLanguageAlike checks that a missing
// default_vat_regime and a missing default_language are both a 400.
func TestProfilePutRequiresRegimeAndLanguageAlike(t *testing.T) {
	t.Parallel()

	for _, field := range []string{"default_vat_regime", "default_language"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()

			f := newProfileFixture(t)
			body := validProfileBody()
			delete(body, field)

			rec := f.do(t, f.owner, http.MethodPut, "/api/v1/profile", body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
			}
			if profileErrorDetails(t, rec)[field] != "required" {
				t.Errorf("details = %v, want %s required", profileErrorDetails(t, rec), field)
			}
		})
	}
}

func TestProfilePutRefusesAnUnassignedCountryAndAShortVATID(t *testing.T) {
	t.Parallel()

	for field, value := range map[string]string{"country_code": "XX", "vat_id": "DE12", "email": "a@b"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()

			f := newProfileFixture(t)
			body := validProfileBody()
			body[field] = value

			rec := f.do(t, f.owner, http.MethodPut, "/api/v1/profile", body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
			}
			if _, ok := profileErrorDetails(t, rec)[field]; !ok {
				t.Errorf("details = %v, want key %q", profileErrorDetails(t, rec), field)
			}
		})
	}
}
