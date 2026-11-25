package config

import (
	"errors"
	"testing"
)

// TestLoadOIDCRedirectURL runs OIDC_REDIRECT_URL through Load, so the
// refusal is proved where a deployment meets it: at startup.
func TestLoadOIDCRedirectURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		plainHTTP bool
		wantErr   error
	}{
		{name: "callback route over https", input: "https://solobill.example.com/api/v1/auth/oidc/callback"},
		{name: "callback route behind a path prefix", input: "https://example.com/solobill/api/v1/auth/oidc/callback"},
		{name: "plain http with plain http allowed", input: "http://localhost:8080/api/v1/auth/oidc/callback", plainHTTP: true},
		{
			name:    "old callback path",
			input:   "https://solobill.example.com/auth/oidc/callback",
			wantErr: ErrOIDCRedirectPath,
		},
		{
			name:    "login route instead of the callback",
			input:   "https://solobill.example.com/api/v1/auth/oidc/login",
			wantErr: ErrOIDCRedirectPath,
		},
		{
			name:    "trailing slash",
			input:   "https://solobill.example.com/api/v1/auth/oidc/callback/",
			wantErr: ErrOIDCRedirectPath,
		},
		{
			name:    "callback path glued to a longer segment",
			input:   "https://solobill.example.com/v2api/v1/auth/oidc/callback",
			wantErr: ErrOIDCRedirectPath,
		},
		{
			name:    "query string",
			input:   "https://solobill.example.com/api/v1/auth/oidc/callback?x=1",
			wantErr: ErrOIDCRedirectPath,
		},
		{
			name:    "fragment",
			input:   "https://solobill.example.com/api/v1/auth/oidc/callback#x",
			wantErr: ErrOIDCRedirectPath,
		},
		{
			name:    "plain http without plain http allowed",
			input:   "http://solobill.example.com/api/v1/auth/oidc/callback",
			wantErr: ErrOIDCRedirectScheme,
		},
		{
			name:      "other scheme with plain http allowed",
			input:     "ftp://solobill.example.com/api/v1/auth/oidc/callback",
			plainHTTP: true,
			wantErr:   ErrOIDCRedirectScheme,
		},
		{
			name:    "no scheme and no host",
			input:   "/api/v1/auth/oidc/callback",
			wantErr: ErrOIDCRedirectScheme,
		},
		{
			name:    "https with no host",
			input:   "https:///api/v1/auth/oidc/callback",
			wantErr: ErrOIDCRedirectScheme,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env := map[string]string{
				"OIDC_ISSUER_URL":          "https://idp.example.com/realms/solobill",
				"OIDC_CLIENT_ID":           "solobill",
				"OIDC_CLIENT_SECRET":       "a-client-secret",
				"OIDC_REDIRECT_URL":        tt.input,
				"OIDC_DEFAULT_TENANT_SLUG": "acme",
			}
			if tt.plainHTTP {
				env["SESSION_ALLOW_PLAIN_HTTP"] = "true"
			}

			cfg, err := Load(validEnv(t, env))

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Load() error = %v, want errors.Is(err, %v)", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.OIDCRedirectURL != tt.input {
				t.Errorf("OIDCRedirectURL = %q, want %q", cfg.OIDCRedirectURL, tt.input)
			}
		})
	}
}

// TestLoadOIDCRedirectURLIgnoredWithoutOIDC keeps a deployment without OIDC
// independent of the variable: a stale value left behind does not stop it.
func TestLoadOIDCRedirectURLIgnoredWithoutOIDC(t *testing.T) {
	t.Parallel()

	_, err := Load(validEnv(t, map[string]string{
		"OIDC_REDIRECT_URL": "https://solobill.example.com/auth/oidc/callback",
	}))
	if err != nil {
		t.Fatalf("Load() error = %v, want none with OIDC off", err)
	}
}
