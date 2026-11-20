package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

const vatRegimesPath = "/api/v1/vat-regimes"

func periodsOf(t *testing.T, f profileFixture, s profileSession) []any {
	t.Helper()

	rec := f.do(t, s, http.MethodGet, vatRegimesPath, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", rec.Code, rec.Body.String())
	}
	periods, ok := decodeMap(t, rec)["periods"].([]any)
	if !ok {
		t.Fatalf("periods is not a list: %s", rec.Body.String())
	}

	return periods
}

func TestVATRegimesRequireASession(t *testing.T) {
	t.Parallel()

	f := newProfileFixture(t)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		if rec := doJSON(t, f.app.handler, method, vatRegimesPath, nil, nil, ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s status = %d, want 401", method, rec.Code)
		}
	}
}

func TestVATRegimesListIsEmptyArrayAndOpenToMembers(t *testing.T) {
	t.Parallel()

	f := newProfileFixture(t)
	if got := periodsOf(t, f, f.memberSession(t)); len(got) != 0 {
		t.Errorf("periods = %v, want empty", got)
	}
}

func TestVATRegimesPostIsOwnerOnly(t *testing.T) {
	t.Parallel()

	f := newProfileFixture(t)
	body := map[string]any{"valid_from": "2026-01-01", "regime": "kleinunternehmer"}

	if rec := f.do(t, f.memberSession(t), http.MethodPost, vatRegimesPath, body); rec.Code != http.StatusForbidden {
		t.Errorf("member status = %d, want 403, body = %s", rec.Code, rec.Body.String())
	}
	if got := periodsOf(t, f, f.owner); len(got) != 0 {
		t.Errorf("a refused POST stored %v", got)
	}
	if rec := f.do(t, f.owner, http.MethodPost, vatRegimesPath, body); rec.Code != http.StatusCreated {
		t.Errorf("owner status = %d, want 201, body = %s", rec.Code, rec.Body.String())
	}
}

func TestVATRegimesCreateAndSupersede(t *testing.T) {
	t.Parallel()

	f := newProfileFixture(t)

	first := f.do(t, f.owner, http.MethodPost, vatRegimesPath,
		map[string]any{"valid_from": "2026-01-01", "regime": "kleinunternehmer", "note": " start "})
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d, body = %s", first.Code, first.Body.String())
	}
	created := decodeMap(t, first)
	if created["valid_from"] != "2026-01-01" || created["regime"] != "kleinunternehmer" ||
		created["note"] != "start" || created["superseded"] != false || created["supersedes_id"] != nil {
		t.Errorf("created = %v", created)
	}
	firstID, _ := created["id"].(string)

	// A second period from a later date is fine.
	if rec := f.do(t, f.owner, http.MethodPost, vatRegimesPath,
		map[string]any{"valid_from": "2026-10-14", "regime": "standard"}); rec.Code != http.StatusCreated {
		t.Fatalf("second status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// The same valid_from as an effective period is a conflict.
	if rec := f.do(t, f.owner, http.MethodPost, vatRegimesPath,
		map[string]any{"valid_from": "2026-01-01", "regime": "standard"}); rec.Code != http.StatusConflict {
		t.Errorf("duplicate status = %d, want 409, body = %s", rec.Code, rec.Body.String())
	}

	// A correction names the wrong row and may reuse its valid_from.
	fix := f.do(t, f.owner, http.MethodPost, vatRegimesPath,
		map[string]any{"valid_from": "2026-01-01", "regime": "standard", "supersedes_id": firstID})
	if fix.Code != http.StatusCreated {
		t.Fatalf("correction status = %d, body = %s", fix.Code, fix.Body.String())
	}
	if decodeMap(t, fix)["supersedes_id"] != firstID {
		t.Errorf("correction = %v, want supersedes_id %s", decodeMap(t, fix), firstID)
	}

	// A row is superseded at most once.
	if rec := f.do(t, f.owner, http.MethodPost, vatRegimesPath,
		map[string]any{"valid_from": "2026-02-01", "regime": "standard", "supersedes_id": firstID}); rec.Code != http.StatusConflict {
		t.Errorf("second correction status = %d, want 409, body = %s", rec.Code, rec.Body.String())
	}

	periods := periodsOf(t, f, f.owner)
	if len(periods) != 3 {
		t.Fatalf("periods = %d, want 3 (superseded rows stay)", len(periods))
	}
	for _, p := range periods {
		row, _ := p.(map[string]any)
		if want := row["id"] == firstID; row["superseded"] != want {
			t.Errorf("period %v: superseded = %v, want %v", row["id"], row["superseded"], want)
		}
	}
}

func TestVATRegimesSupersedingAnUnknownRowIs404(t *testing.T) {
	t.Parallel()

	f := newProfileFixture(t)
	rec := f.do(t, f.owner, http.MethodPost, vatRegimesPath,
		map[string]any{"valid_from": "2026-01-01", "regime": "standard", "supersedes_id": uuid.NewString()})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404, body = %s", rec.Code, rec.Body.String())
	}
}

func TestVATRegimesRejectInvalidBodies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    map[string]any
		wantKey string
	}{
		{"bad date", map[string]any{"valid_from": "14.10.2026", "regime": "standard"}, "valid_from"},
		{"missing date", map[string]any{"regime": "standard"}, "valid_from"},
		{"bad regime", map[string]any{"valid_from": "2026-01-01", "regime": "flat"}, "regime"},
		{"bad supersedes id", map[string]any{"valid_from": "2026-01-01", "regime": "standard", "supersedes_id": "x"}, "supersedes_id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newProfileFixture(t)
			rec := f.do(t, f.owner, http.MethodPost, vatRegimesPath, tt.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
			}
			if _, ok := errorDetails(t, rec)[tt.wantKey]; !ok {
				t.Errorf("details = %v, want %q", errorDetails(t, rec), tt.wantKey)
			}
		})
	}

	f := newProfileFixture(t)
	if rec := f.do(t, f.owner, http.MethodPost, vatRegimesPath,
		map[string]any{"valid_from": "2026-01-01", "regime": "standard", "tenant_id": uuid.NewString()}); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown field status = %d, want 400", rec.Code)
	}
}
