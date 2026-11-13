package config

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// validEnv returns a getenv function satisfying every required field, so a
// test can start from a known-good baseline and change one thing.
func validEnv(t *testing.T, overrides map[string]string) func(string) string {
	t.Helper()

	sessionKey := hex.EncodeToString(make([]byte, 32))
	csrfKey := hex.EncodeToString(make([]byte, 32))

	env := map[string]string{
		"DATABASE_URL":      "postgres://localhost:5432/solobill?sslmode=disable",
		"DATABASE_USER":     "solobill",
		"DATABASE_PASSWORD": "a-password",
		"SESSION_CODEC_KEY": sessionKey,
		"CSRF_KEY":          csrfKey,
	}
	for k, v := range overrides {
		if v == "" {
			delete(env, k)
			continue
		}
		env[k] = v
	}

	return func(key string) string { return env[key] }
}

func TestLoad(t *testing.T) {
	t.Parallel()

	t.Run("required field missing", func(t *testing.T) {
		t.Parallel()

		getenv := validEnv(t, map[string]string{"DATABASE_URL": ""})

		_, err := Load(getenv)
		if err == nil {
			t.Fatal("Load() error = nil, want an error for a missing DATABASE_URL")
		}
	})

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		getenv := validEnv(t, nil)

		cfg, err := Load(getenv)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.ListenAddr != ":8080" {
			t.Errorf("ListenAddr = %q, want default :8080", cfg.ListenAddr)
		}
		if cfg.DatabaseUser != "solobill" {
			t.Errorf("DatabaseUser = %q", cfg.DatabaseUser)
		}
		if len(cfg.SessionCodecKey) != 32 {
			t.Errorf("SessionCodecKey decodes to %d bytes, want 32", len(cfg.SessionCodecKey))
		}
		if cfg.OIDCEnabled {
			t.Error("OIDCEnabled = true, want false with no OIDC_ISSUER_URL set")
		}
	})

	t.Run("oidc disabled when issuer empty", func(t *testing.T) {
		t.Parallel()

		getenv := validEnv(t, nil)

		cfg, err := Load(getenv)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.OIDCEnabled {
			t.Error("OIDCEnabled = true, want false")
		}
		if cfg.OIDCClientID != "" || cfg.OIDCClientSecret != "" || cfg.OIDCRedirectURL != "" {
			t.Error("OIDC fields are set despite OIDC being disabled")
		}
	})

	t.Run("oidc enabled requires its own fields", func(t *testing.T) {
		t.Parallel()

		getenv := validEnv(t, map[string]string{
			"OIDC_ISSUER_URL": "https://idp.example.com",
		})

		_, err := Load(getenv)
		if err == nil {
			t.Fatal("Load() error = nil, want an error for OIDC enabled without its other required fields")
		}
	})

	t.Run("oidc enabled with every field set", func(t *testing.T) {
		t.Parallel()

		getenv := validEnv(t, map[string]string{
			"OIDC_ISSUER_URL":          "https://idp.example.com",
			"OIDC_CLIENT_ID":           "solobill",
			"OIDC_CLIENT_SECRET":       "a-client-secret",
			"OIDC_REDIRECT_URL":        "https://solobill.example.com/auth/oidc/callback",
			"OIDC_DEFAULT_TENANT_SLUG": "acme",
		})

		cfg, err := Load(getenv)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if !cfg.OIDCEnabled {
			t.Error("OIDCEnabled = false, want true")
		}
		if cfg.OIDCDefaultTenantSlug != "acme" {
			t.Errorf("OIDCDefaultTenantSlug = %q", cfg.OIDCDefaultTenantSlug)
		}
	})

	t.Run("session key too short", func(t *testing.T) {
		t.Parallel()

		getenv := validEnv(t, map[string]string{"SESSION_CODEC_KEY": hex.EncodeToString(make([]byte, 16))})

		_, err := Load(getenv)
		if err == nil {
			t.Fatal("Load() error = nil, want an error for a session key under 32 bytes")
		}
	})

	t.Run("session key not hex", func(t *testing.T) {
		t.Parallel()

		getenv := validEnv(t, map[string]string{"SESSION_CODEC_KEY": "not-hex!"})

		_, err := Load(getenv)
		if err == nil {
			t.Fatal("Load() error = nil, want an error for a non-hex session key")
		}
	})

	t.Run("database password from file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "password")
		writeFile(t, path, "file-password\n")

		getenv := validEnv(t, map[string]string{
			"DATABASE_PASSWORD":      "",
			"DATABASE_PASSWORD_FILE": path,
		})

		cfg, err := Load(getenv)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.DatabasePassword != "file-password" {
			t.Errorf("DatabasePassword = %q, want %q", cfg.DatabasePassword, "file-password")
		}
	})

	t.Run("rate limit defaults", func(t *testing.T) {
		t.Parallel()

		getenv := validEnv(t, nil)

		cfg, err := Load(getenv)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.RateLimitBurst != defaultRateLimitBurst {
			t.Errorf("RateLimitBurst = %d, want %d", cfg.RateLimitBurst, defaultRateLimitBurst)
		}
		if cfg.RateLimitPer != defaultRateLimitPer {
			t.Errorf("RateLimitPer = %s, want %s", cfg.RateLimitPer, defaultRateLimitPer)
		}
	})
}

func TestConfigDatabaseDSN(t *testing.T) {
	t.Parallel()

	cfg := Config{
		DatabaseURL:      "postgres://localhost:5432/solobill?sslmode=disable",
		DatabaseUser:     "solobill",
		DatabasePassword: "s3cret",
	}

	dsn, err := cfg.DatabaseDSN()
	if err != nil {
		t.Fatalf("DatabaseDSN() error = %v", err)
	}
	want := "postgres://solobill:s3cret@localhost:5432/solobill?sslmode=disable"
	if dsn != want {
		t.Errorf("DatabaseDSN() = %q, want %q", dsn, want)
	}
}

func TestSecretValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     map[string]string
		want    string
		wantErr bool
	}{
		{
			name: "direct value works",
			env:  map[string]string{"SECRET": "direct-value"},
			want: "direct-value",
		},
		{
			name: "neither set is empty, not an error",
			env:  map[string]string{},
			want: "",
		},
		{
			name:    "both set is an error",
			env:     map[string]string{"SECRET": "direct-value", "SECRET_FILE": "placeholder"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			getenv := func(key string) string { return tt.env[key] }

			got, err := secretValue(getenv, "SECRET")
			if (err != nil) != tt.wantErr {
				t.Fatalf("secretValue() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("secretValue() = %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("file content trimmed", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "secret")
		writeFile(t, path, "  file-value  \r\n")

		getenv := func(key string) string {
			if key == "SECRET_FILE" {
				return path
			}
			return ""
		}

		got, err := secretValue(getenv, "SECRET")
		if err != nil {
			t.Fatalf("secretValue() error = %v", err)
		}
		// Only the trailing CRLF is trimmed by secretValue itself, matching
		// ostendorf.tech's own implementation; a leading or inner space is
		// a value the caller actually wrote into the file and is kept.
		want := "  file-value  "
		if got != want {
			t.Errorf("secretValue() = %q, want %q", got, want)
		}
	})

	t.Run("file does not exist", func(t *testing.T) {
		t.Parallel()

		getenv := func(key string) string {
			if key == "SECRET_FILE" {
				return filepath.Join(t.TempDir(), "missing")
			}
			return ""
		}

		if _, err := secretValue(getenv, "SECRET"); err == nil {
			t.Fatal("secretValue() error = nil, want an error for a missing file")
		}
	})
}

func TestRequiredSecret(t *testing.T) {
	t.Parallel()

	t.Run("missing is an error", func(t *testing.T) {
		t.Parallel()

		getenv := func(string) string { return "" }

		_, err := requiredSecret(getenv, "SECRET")
		if err == nil {
			t.Fatal("requiredSecret() error = nil, want an error for an unset secret")
		}
	})

	t.Run("present is not an error", func(t *testing.T) {
		t.Parallel()

		getenv := func(key string) string {
			if key == "SECRET" {
				return "value"
			}
			return ""
		}

		got, err := requiredSecret(getenv, "SECRET")
		if err != nil {
			t.Fatalf("requiredSecret() error = %v", err)
		}
		if got != "value" {
			t.Errorf("requiredSecret() = %q", got)
		}
	})
}

func TestDecodeHexKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "valid 32 byte key", raw: hex.EncodeToString(make([]byte, 32))},
		{name: "too short", raw: hex.EncodeToString(make([]byte, 16)), wantErr: true},
		{name: "not hex", raw: "not-hex!", wantErr: true},
		{name: "empty", raw: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := decodeHexKey("KEY", tt.raw)
			if (err != nil) != tt.wantErr {
				t.Errorf("decodeHexKey() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// writeFile is a small helper so the secretValue tests above read as table
// driven data rather than repeating the same os.WriteFile error handling.
func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
