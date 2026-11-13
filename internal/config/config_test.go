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

	t.Run("database password set both directly and via file is an error", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "password")
		writeFile(t, path, "file-password\n")

		getenv := validEnv(t, map[string]string{
			"DATABASE_PASSWORD":      "a-password",
			"DATABASE_PASSWORD_FILE": path,
		})

		if _, err := Load(getenv); err == nil {
			t.Fatal("Load() error = nil, want an error for DATABASE_PASSWORD and DATABASE_PASSWORD_FILE both set")
		}
	})

	t.Run("database password file resolving empty is an error", func(t *testing.T) {
		t.Parallel()

		// govue's envconf.Loader.RequiredSecretOrFile (since v0.3.0) refuses
		// a _FILE that reads back empty as a hard error, rather than the
		// silent "" this package's own, now-removed secretValue used to
		// return for the same input. This is the one behavioural
		// difference the switch to govue's contract introduced; this case
		// pins it so a regression here fails loudly.
		dir := t.TempDir()
		path := filepath.Join(dir, "password")
		writeFile(t, path, "\n\n")

		getenv := validEnv(t, map[string]string{
			"DATABASE_PASSWORD":      "",
			"DATABASE_PASSWORD_FILE": path,
		})

		if _, err := Load(getenv); err == nil {
			t.Fatal("Load() error = nil, want an error for a DATABASE_PASSWORD_FILE that resolves to empty")
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

	t.Run("seed owner both empty is a noop by default", func(t *testing.T) {
		t.Parallel()

		getenv := validEnv(t, nil)

		cfg, err := Load(getenv)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.SeedOwnerEmail != "" || cfg.SeedOwnerOIDCSubject != "" {
			t.Error("SeedOwnerEmail or SeedOwnerOIDCSubject is set despite neither being configured")
		}
	})

	t.Run("seed owner subject without email is an error", func(t *testing.T) {
		t.Parallel()

		getenv := validEnv(t, map[string]string{
			"SEED_TENANT_SLUG":        "acme",
			"SEED_TENANT_NAME":        "Acme",
			"SEED_OWNER_OIDC_SUBJECT": "f:realm:owner-subject",
		})

		_, err := Load(getenv)
		if err == nil {
			t.Fatal("Load() error = nil, want an error for SEED_OWNER_OIDC_SUBJECT without SEED_OWNER_EMAIL")
		}
	})

	t.Run("seed owner email without subject is an error", func(t *testing.T) {
		t.Parallel()

		getenv := validEnv(t, map[string]string{
			"SEED_TENANT_SLUG": "acme",
			"SEED_TENANT_NAME": "Acme",
			"SEED_OWNER_EMAIL": "owner@acme.example",
		})

		_, err := Load(getenv)
		if err == nil {
			t.Fatal("Load() error = nil, want an error for SEED_OWNER_EMAIL without SEED_OWNER_OIDC_SUBJECT")
		}
	})

	t.Run("seed owner subject without a seed tenant is an error", func(t *testing.T) {
		t.Parallel()

		getenv := validEnv(t, map[string]string{
			"SEED_OWNER_EMAIL":        "owner@acme.example",
			"SEED_OWNER_OIDC_SUBJECT": "f:realm:owner-subject",
		})

		_, err := Load(getenv)
		if err == nil {
			t.Fatal("Load() error = nil, want an error for SEED_OWNER_OIDC_SUBJECT with no SEED_TENANT_SLUG")
		}
	})

	t.Run("seed owner with every field set", func(t *testing.T) {
		t.Parallel()

		getenv := validEnv(t, map[string]string{
			"SEED_TENANT_SLUG":        "acme",
			"SEED_TENANT_NAME":        "Acme",
			"SEED_OWNER_EMAIL":        "owner@acme.example",
			"SEED_OWNER_OIDC_SUBJECT": "f:realm:owner-subject",
		})

		cfg, err := Load(getenv)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.SeedOwnerEmail != "owner@acme.example" {
			t.Errorf("SeedOwnerEmail = %q", cfg.SeedOwnerEmail)
		}
		if cfg.SeedOwnerOIDCSubject != "f:realm:owner-subject" {
			t.Errorf("SeedOwnerOIDCSubject = %q", cfg.SeedOwnerOIDCSubject)
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
