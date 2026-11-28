package siteserver_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/siteserver"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadLinks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     map[string]string
		want    siteserver.Links
		wantErr string
	}{
		{
			name: "nothing set is the app on app.solobill.de with registration on",
			want: siteserver.Links{
				LoginURL:            "https://app.solobill.de/login",
				RegisterURL:         "https://app.solobill.de/register",
				RegistrationEnabled: true,
			},
		},
		{
			name: "every variable set",
			env: map[string]string{
				siteserver.EnvLoginURL:            "https://staging.solobill.de/sign-in",
				siteserver.EnvRegisterURL:         "https://staging.solobill.de/sign-up",
				siteserver.EnvRegistrationEnabled: "false",
			},
			want: siteserver.Links{
				LoginURL:            "https://staging.solobill.de/sign-in",
				RegisterURL:         "https://staging.solobill.de/sign-up",
				RegistrationEnabled: false,
			},
		},
		{
			name: "registration off keeps the default URLs",
			env:  map[string]string{siteserver.EnvRegistrationEnabled: "false"},
			want: siteserver.Links{
				LoginURL:            siteserver.DefaultLoginURL,
				RegisterURL:         siteserver.DefaultRegisterURL,
				RegistrationEnabled: false,
			},
		},
		{
			name:    "a script URL is refused",
			env:     map[string]string{siteserver.EnvLoginURL: "javascript:alert(1)"},
			wantErr: siteserver.EnvLoginURL,
		},
		{
			name:    "a URL without a host is refused",
			env:     map[string]string{siteserver.EnvRegisterURL: "https:///register"},
			wantErr: siteserver.EnvRegisterURL,
		},
		{
			name:    "a URL with credentials is refused",
			env:     map[string]string{siteserver.EnvLoginURL: "https://user:pw@app.solobill.de/login"},
			wantErr: siteserver.EnvLoginURL,
		},
		{
			name:    "a switch that is not a boolean is refused",
			env:     map[string]string{siteserver.EnvRegistrationEnabled: "maybe"},
			wantErr: siteserver.EnvRegistrationEnabled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := siteserver.LoadLinks(env(tt.env))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("LoadLinks() error = %v, want one naming %s", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("LoadLinks() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("LoadLinks() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDefaultLinksEqualAnUnsetEnvironment(t *testing.T) {
	t.Parallel()

	got, err := siteserver.LoadLinks(env(nil))
	if err != nil {
		t.Fatalf("LoadLinks() error = %v", err)
	}
	if got != siteserver.DefaultLinks() {
		t.Errorf("LoadLinks(nothing) = %+v, want DefaultLinks() %+v", got, siteserver.DefaultLinks())
	}
}

// TestConfigJSON pins the file the landing fetches: served at /config.json
// with no option set, revalidated by its ETag, with the key names
// landing/src/siteConfig.ts reads.
func TestConfigJSON(t *testing.T) {
	t.Parallel()

	rec := get(t, newTestHandler(t), "/config.json", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /config.json status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if rec.Header().Get("ETag") == "" {
		t.Error("ETag is empty, the landing revalidates by it")
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %v: %s", err, rec.Body.String())
	}
	want := map[string]any{
		"loginUrl":            "https://app.solobill.de/login",
		"registerUrl":         "https://app.solobill.de/register",
		"registrationEnabled": true,
	}
	if len(got) != len(want) {
		t.Errorf("body = %v, want exactly the keys of %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("body[%q] = %v, want %v", k, got[k], v)
		}
	}
}

func TestConfigJSONCarriesTheConfiguredLinks(t *testing.T) {
	t.Parallel()

	links := siteserver.Links{
		LoginURL:            "https://staging.solobill.de/sign-in",
		RegisterURL:         "https://staging.solobill.de/sign-up",
		RegistrationEnabled: false,
	}
	s, err := siteserver.New(testConfig(), testBuild(), testLogger(), siteserver.Options{Links: &links})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	rec := get(t, s.Handler(), "/config.json", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /config.json status = %d, want 200", rec.Code)
	}
	var got siteserver.Links
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if got != links {
		t.Errorf("body = %+v, want %+v", got, links)
	}
}

// TestConfigJSONHidesAFileOfTheBuild pins that the environment wins over a
// config.json that a build carries, so a stale file never decides.
func TestConfigJSONHidesAFileOfTheBuild(t *testing.T) {
	t.Parallel()

	build := testBuild()
	build[siteserver.ConfigFile] = build["sitemap.xml"]
	s, err := siteserver.New(testConfig(), build, testLogger(), siteserver.Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	rec := get(t, s.Handler(), "/config.json", "")
	if !strings.Contains(rec.Body.String(), "loginUrl") {
		t.Errorf("body = %q, want the generated configuration", rec.Body.String())
	}
}

func TestNewRefusesEmptyLinks(t *testing.T) {
	t.Parallel()

	_, err := siteserver.New(testConfig(), testBuild(), testLogger(), siteserver.Options{Links: &siteserver.Links{}})
	if err == nil {
		t.Error("New() error = nil for empty links, want one")
	}
}
