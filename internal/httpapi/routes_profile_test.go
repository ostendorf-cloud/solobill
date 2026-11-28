package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// profileSession is a logged in caller of the profile routes.
type profileSession struct {
	cookies []*http.Cookie
	token   string
}

// profileLogin seeds an active account with role in tenant and logs it
// in. The password and TOTP secret are test fixtures.
func profileLogin(t *testing.T, app testApp, tenant storage.Tenant, email string, role storage.AccountRole) profileSession {
	t.Helper()

	const password = "profile-test-password"
	hash := hashPassword(t, password)
	secret := totpSecret(t)
	app.store.addAccount(storage.Account{
		TenantID:     tenant.ID,
		Email:        email,
		PasswordHash: &hash,
		TOTPSecret:   &secret,
		Role:         role,
		Status:       storage.AccountStatusActive,
	})

	cookie, token := loginAndCSRF(t, app, tenant.Slug, email, password, secret)

	return profileSession{cookies: []*http.Cookie{cookie}, token: token}
}

// profileFixture is an app with one tenant, its owner and a member.
type profileFixture struct {
	app    testApp
	tenant storage.Tenant
	owner  profileSession
}

func newProfileFixture(t *testing.T) profileFixture {
	t.Helper()

	return newProfileFixtureWith(t, profileExtras{})
}

// newProfileFixtureWith is newProfileFixture with the repositories of
// other features this test needs: see profileExtras.
func newProfileFixtureWith(t *testing.T, extras profileExtras) profileFixture {
	t.Helper()

	profileExtrasByTest.Store(t, extras)
	t.Cleanup(func() { profileExtrasByTest.Delete(t) })

	app := buildTestApp(t, 1000)
	tenant := app.store.addTenant(storage.Tenant{Slug: "acme", Name: "acme"})

	return profileFixture{
		app:    app,
		tenant: tenant,
		owner:  profileLogin(t, app, tenant, "owner@acme.test", storage.AccountRoleOwner),
	}
}

// memberSession logs in a member of the fixture's tenant. It is lazy: a
// login hashes a password, which is slow under the race detector.
func (f profileFixture) memberSession(t *testing.T) profileSession {
	t.Helper()

	return profileLogin(t, f.app, f.tenant, "member@acme.test", storage.AccountRoleMember)
}

// fakeProfiles reads the profile fake back out of the test app.
func (f profileFixture) fakeProfiles(t *testing.T) *fakeSellerProfiles {
	t.Helper()

	profiles, ok := f.app.repos.SellerProfiles.(*fakeSellerProfiles)
	if !ok {
		t.Fatalf("SellerProfiles is %T, want the profile fake", f.app.repos.SellerProfiles)
	}

	return profiles
}

func (f profileFixture) do(t *testing.T, s profileSession, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	return doJSON(t, f.app.handler, method, path, body, s.cookies, s.token)
}

// validProfileBody is a complete Kleinunternehmer profile.
func validProfileBody() map[string]any {
	return map[string]any{
		"legal_name":          "Example Trading",
		"street":              "Main Street 1",
		"postal_code":         "12345",
		"city":                "Example City",
		"country_code":        "DE",
		"kleinunternehmer_id": "KU-1",
		"default_vat_regime":  "kleinunternehmer",
		"default_language":    "de",
		"iban":                "DE89 3704 0044 0532 0130 00",
		"bic":                 "COBADEFFXXX",
		"email":               "office@example.test",
	}
}

func profileDecodeMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}

	return out
}

// errorDetails returns the details of an error response.
func profileErrorDetails(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	e, _ := profileDecodeMap(t, rec)["error"].(map[string]any)
	details, _ := e["details"].(map[string]any)

	return details
}

func TestProfileRequiresASession(t *testing.T) {
	t.Parallel()

	f := newProfileFixture(t)
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		rec := doJSON(t, f.app.handler, method, "/api/v1/profile", nil, nil, "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s status = %d, want 401, body = %s", method, rec.Code, rec.Body.String())
		}
	}
}

func TestProfileIsOwnerOnly(t *testing.T) {
	t.Parallel()

	f := newProfileFixture(t)
	if rec := f.do(t, f.owner, http.MethodPut, "/api/v1/profile", validProfileBody()); rec.Code != http.StatusOK {
		t.Fatalf("owner PUT status = %d, body = %s", rec.Code, rec.Body.String())
	}

	member := f.memberSession(t)
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		var body any
		if method == http.MethodPut {
			body = validProfileBody()
		}
		rec := f.do(t, member, method, "/api/v1/profile", body)
		if rec.Code != http.StatusForbidden {
			t.Errorf("member %s status = %d, want 403, body = %s", method, rec.Code, rec.Body.String())
		}
	}

	// A refused member PUT changed nothing.
	if profiles := f.fakeProfiles(t); len(profiles.putTenants) != 1 {
		t.Errorf("Put called %d times, want 1 (the owner's)", len(profiles.putTenants))
	}
}

func TestProfilePutNeedsTheCSRFToken(t *testing.T) {
	t.Parallel()

	f := newProfileFixture(t)
	rec := doJSON(t, f.app.handler, http.MethodPut, "/api/v1/profile", validProfileBody(), f.owner.cookies, "")
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403, body = %s", rec.Code, rec.Body.String())
	}
}

func TestProfileGetBeforeAnySaveIs404(t *testing.T) {
	t.Parallel()

	f := newProfileFixture(t)
	if rec := f.do(t, f.owner, http.MethodGet, "/api/v1/profile", nil); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404, body = %s", rec.Code, rec.Body.String())
	}
}

func TestProfilePutThenGet(t *testing.T) {
	t.Parallel()

	f := newProfileFixture(t)
	put := f.do(t, f.owner, http.MethodPut, "/api/v1/profile", validProfileBody())
	if put.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", put.Code, put.Body.String())
	}

	get := f.do(t, f.owner, http.MethodGet, "/api/v1/profile", nil)
	if get.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", get.Code, get.Body.String())
	}
	if put.Body.String() != get.Body.String() {
		t.Errorf("PUT answered %s, GET answered %s", put.Body.String(), get.Body.String())
	}

	got := profileDecodeMap(t, get)
	if got["iban"] != "DE89370400440532013000" {
		t.Errorf("iban = %v, want the normalised spelling", got["iban"])
	}
	if got["has_logo"] != false {
		t.Errorf("has_logo = %v, want false", got["has_logo"])
	}
	if got["address_line2"] != nil || got["tax_number"] != nil {
		t.Errorf("unset optional fields must be null, got %v and %v", got["address_line2"], got["tax_number"])
	}
	formats, _ := got["number_formats"].(map[string]any)
	if formats["invoice"] != "RE-{year}{seq:4}" || formats["offer"] != "AN-{year}{seq:4}" {
		t.Errorf("number_formats = %v, want the defaults", formats)
	}
}

func TestProfileIssueReadyPerRegime(t *testing.T) {
	t.Parallel()

	f := newProfileFixture(t)

	tests := []struct {
		name               string
		taxField           string
		standard, kleinunt bool
	}{
		{"kleinunternehmer id only", "kleinunternehmer_id", false, true},
		{"tax number", "tax_number", true, true},
		{"vat id", "vat_id", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := validProfileBody()
			delete(body, "kleinunternehmer_id")
			body[tt.taxField] = "12/345/67890"
			if tt.taxField == "vat_id" {
				body[tt.taxField] = "DE123456789"
			}

			rec := f.do(t, f.owner, http.MethodPut, "/api/v1/profile", body)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			ready, _ := profileDecodeMap(t, rec)["issue_ready"].(map[string]any)
			if ready["standard"] != tt.standard || ready["kleinunternehmer"] != tt.kleinunt {
				t.Errorf("issue_ready = %v, want standard %v kleinunternehmer %v", ready, tt.standard, tt.kleinunt)
			}
		})
	}
}

func TestProfileAcceptsAnIncompleteProfile(t *testing.T) {
	t.Parallel()

	f := newProfileFixture(t)
	rec := f.do(t, f.owner, http.MethodPut, "/api/v1/profile", map[string]any{"default_vat_regime": "standard", "default_language": "de"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	ready, _ := profileDecodeMap(t, rec)["issue_ready"].(map[string]any)
	if ready["standard"] != false || ready["kleinunternehmer"] != false {
		t.Errorf("issue_ready = %v, want both false", ready)
	}
}

func TestProfilePutRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(map[string]any)
		wantKey string
	}{
		{"iban checksum", func(b map[string]any) { b["iban"] = "DE89370400440532013001" }, "iban"},
		{"country code", func(b map[string]any) { b["country_code"] = "Germany" }, "country_code"},
		{"regime", func(b map[string]any) { b["default_vat_regime"] = "flat" }, "default_vat_regime"},
		{"missing regime", func(b map[string]any) { delete(b, "default_vat_regime") }, "default_vat_regime"},
		{"language", func(b map[string]any) { b["default_language"] = "fr" }, "default_language"},
		{"bic", func(b map[string]any) { b["bic"] = "X" }, "bic"},
		{"email", func(b map[string]any) { b["email"] = "nope" }, "email"},
		{"website", func(b map[string]any) { b["website"] = "ftp://example.test" }, "website"},
		{"unknown token", func(b map[string]any) { b["number_formats"] = map[string]string{"invoice": "RE-{month}{seq}"} }, "number_formats.invoice"},
		{"bad character", func(b map[string]any) { b["number_formats"] = map[string]string{"offer": "AN 1-{year}{seq}"} }, "number_formats.offer"},
		{"seq width", func(b map[string]any) { b["number_formats"] = map[string]string{"offer": "AN-{year}{seq:12}"} }, "number_formats.offer"},
		{"leading dot", func(b map[string]any) { b["number_formats"] = map[string]string{"invoice": ".RE-{year}{seq}"} }, "number_formats.invoice"},
		{"double slash", func(b map[string]any) { b["number_formats"] = map[string]string{"invoice": "RE//{year}{seq}"} }, "number_formats.invoice"},
		{"no year", func(b map[string]any) { b["number_formats"] = map[string]string{"invoice": "RE-{seq}"} }, "number_formats.invoice"},
		{"unknown type", func(b map[string]any) { b["number_formats"] = map[string]string{"correction": "KO-{year}{seq}"} }, "number_formats.correction"},
		{
			"overlapping prefixes",
			func(b map[string]any) {
				b["number_formats"] = map[string]string{"invoice": "RE-{year}{seq:4}", "offer": "RE-{year}{seq}"}
			},
			"number_formats",
		},
		{
			"offer prefix is a prefix of the default invoice prefix",
			func(b map[string]any) { b["number_formats"] = map[string]string{"offer": "R{year}{seq}"} },
			"number_formats",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newProfileFixture(t)
			body := validProfileBody()
			tt.mutate(body)

			rec := f.do(t, f.owner, http.MethodPut, "/api/v1/profile", body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
			}
			if _, ok := profileErrorDetails(t, rec)[tt.wantKey]; !ok {
				t.Errorf("details = %v, want key %q", profileErrorDetails(t, rec), tt.wantKey)
			}
			if profiles := f.fakeProfiles(t); len(profiles.putTenants) != 0 {
				t.Error("a refused profile was stored")
			}
		})
	}
}

func TestProfilePutRefusesUnknownAndReadOnlyFields(t *testing.T) {
	t.Parallel()

	for _, field := range []string{"tenant_id", "has_logo", "issue_ready", "updated_at", "logo_file_id"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()

			f := newProfileFixture(t)
			body := validProfileBody()
			body[field] = uuid.NewString()

			rec := f.do(t, f.owner, http.MethodPut, "/api/v1/profile", body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestProfileTenantComesFromTheSessionOnly(t *testing.T) {
	t.Parallel()

	f := newProfileFixture(t)
	other := f.app.store.addTenant(storage.Tenant{Slug: "beta", Name: "beta"})
	otherOwner := profileLogin(t, f.app, other, "owner@beta.test", storage.AccountRoleOwner)

	if rec := f.do(t, f.owner, http.MethodPut, "/api/v1/profile", validProfileBody()); rec.Code != http.StatusOK {
		t.Fatalf("acme PUT status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(t, otherOwner, http.MethodPut, "/api/v1/profile", validProfileBody()); rec.Code != http.StatusOK {
		t.Fatalf("beta PUT status = %d, body = %s", rec.Code, rec.Body.String())
	}

	profiles := f.fakeProfiles(t)
	if len(profiles.putTenants) != 2 || profiles.putTenants[0] != f.tenant.ID || profiles.putTenants[1] != other.ID {
		t.Errorf("Put tenants = %v, want [%v %v]", profiles.putTenants, f.tenant.ID, other.ID)
	}

	// A tenant named in the body is refused, never honoured.
	body := validProfileBody()
	body["tenant_id"] = other.ID.String()
	if rec := f.do(t, f.owner, http.MethodPut, "/api/v1/profile", body); rec.Code != http.StatusBadRequest {
		t.Errorf("body tenant_id status = %d, want 400", rec.Code)
	}
}

func TestProfileHasLogoFollowsTheLogoRepository(t *testing.T) {
	t.Parallel()

	logos := &fakeSellerLogos{}
	f := newProfileFixtureWith(t, profileExtras{logos: logos})
	f.do(t, f.owner, http.MethodPut, "/api/v1/profile", validProfileBody())

	id := uuid.New()
	logos.fileID = &id

	rec := f.do(t, f.owner, http.MethodGet, "/api/v1/profile", nil)
	if profileDecodeMap(t, rec)["has_logo"] != true {
		t.Errorf("has_logo = %v, want true", profileDecodeMap(t, rec)["has_logo"])
	}

	// Saving the profile never reports or changes the logo state.
	rec = f.do(t, f.owner, http.MethodPut, "/api/v1/profile", validProfileBody())
	if profileDecodeMap(t, rec)["has_logo"] != true || logos.fileID == nil {
		t.Error("PUT lost the logo")
	}
}

func TestProfileTemplateChangeAfterTheFirstIssue(t *testing.T) {
	t.Parallel()

	docs := &fakeIssuedDocuments{}
	f := newProfileFixtureWith(t, profileExtras{docs: docs})

	// Before any issued document every valid template may be saved.
	body := validProfileBody()
	body["number_formats"] = map[string]string{"invoice": "RE-{year}{seq:4}", "offer": "AN-{year}{seq:4}"}
	if rec := f.do(t, f.owner, http.MethodPut, "/api/v1/profile", body); rec.Code != http.StatusOK {
		t.Fatalf("first PUT status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body["number_formats"] = map[string]string{"invoice": "RE-{seq:6}-{year}", "offer": "AN-{year}{seq:4}"}
	if rec := f.do(t, f.owner, http.MethodPut, "/api/v1/profile", body); rec.Code != http.StatusOK {
		t.Fatalf("pre issue change status = %d, body = %s", rec.Code, rec.Body.String())
	}

	docs.documents = []storage.Document{{Type: storage.DocumentTypeInvoice, State: storage.DocumentStateIssued}}

	tests := []struct {
		name     string
		invoice  string
		wantCode int
	}{
		{"unchanged", "RE-{seq:6}-{year}", http.StatusOK},
		{"changed tokens", "RE-{year}{seq:4}", http.StatusBadRequest},
		{"overlapping new prefix", "RE-X-{seq:6}-{year}", http.StatusBadRequest},
		{"new non overlapping prefix", "RG-{seq:6}-{year}", http.StatusOK},
	}
	for _, tt := range tests {
		body["number_formats"] = map[string]string{"invoice": tt.invoice, "offer": "AN-{year}{seq:4}"}
		rec := f.do(t, f.owner, http.MethodPut, "/api/v1/profile", body)
		if rec.Code != tt.wantCode {
			t.Errorf("%s: status = %d, want %d, body = %s", tt.name, rec.Code, tt.wantCode, rec.Body.String())
		}
		if tt.wantCode == http.StatusOK {
			// The next case compares against what is stored now.
			continue
		}
		if _, ok := profileErrorDetails(t, rec)["number_formats.invoice"]; !ok {
			t.Errorf("%s: details = %v, want number_formats.invoice", tt.name, profileErrorDetails(t, rec))
		}
	}

	// The offer has no issued document yet, so its tokens may still change.
	body["number_formats"] = map[string]string{"invoice": "RG-{seq:6}-{year}", "offer": "AN-{year}{seq:5}"}
	if rec := f.do(t, f.owner, http.MethodPut, "/api/v1/profile", body); rec.Code != http.StatusOK {
		t.Errorf("offer change status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

// TestProfileSaveLocksBeforeReadingIssuedTypes pins the order the issue
// race rests on: the profile row is locked before the issued document
// types are read, so a first issue cannot commit between the two.
func TestProfileSaveLocksBeforeReadingIssuedTypes(t *testing.T) {
	t.Parallel()

	profiles := &fakeSellerProfiles{}
	docs := &fakeIssuedDocuments{profileLocked: profiles.isLocked}
	f := newProfileFixtureWith(t, profileExtras{docs: docs, profiles: profiles})

	if rec := f.do(t, f.owner, http.MethodPut, "/api/v1/profile", validProfileBody()); rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", rec.Code, rec.Body.String())
	}

	if len(docs.listedLocked) != 1 || !docs.listedLocked[0] {
		t.Errorf("the issued types were read with the profile locked = %v, want [true]", docs.listedLocked)
	}
}
