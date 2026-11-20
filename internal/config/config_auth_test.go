package config

import (
	"slices"
	"testing"
	"time"
)

func TestLoadAuth(t *testing.T) {
	t.Parallel()

	t.Run("defaults leave passkeys off and the limits to httpapi", func(t *testing.T) {
		t.Parallel()

		cfg, err := Load(validEnv(t, nil))
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.Auth.PasskeysEnabled {
			t.Error("PasskeysEnabled = true, want false without PASSKEY_RP_ID")
		}
		if cfg.Auth.LoginClientBurst != 0 || cfg.Auth.LoginBurst != 0 || cfg.Auth.LoginCeilingBurst != 0 {
			t.Errorf("login limits = %+v, want zero for httpapi's defaults", cfg.Auth)
		}
	})

	t.Run("passkeys with their relying party", func(t *testing.T) {
		t.Parallel()

		cfg, err := Load(validEnv(t, map[string]string{
			"PASSKEY_RP_ID":   "solobill.example.com",
			"PASSKEY_ORIGINS": "https://solobill.example.com",
		}))
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if !cfg.Auth.PasskeysEnabled || cfg.Auth.PasskeyRPID != "solobill.example.com" {
			t.Errorf("passkeys = %+v, want enabled for solobill.example.com", cfg.Auth)
		}
		if !slices.Equal(cfg.Auth.PasskeyOrigins, []string{"https://solobill.example.com"}) {
			t.Errorf("PasskeyOrigins = %v", cfg.Auth.PasskeyOrigins)
		}
		if cfg.Auth.PasskeyRPDisplayName != "solobill" {
			t.Errorf("PasskeyRPDisplayName = %q, want the default solobill", cfg.Auth.PasskeyRPDisplayName)
		}
	})

	t.Run("an rp id without origins is refused", func(t *testing.T) {
		t.Parallel()

		if _, err := Load(validEnv(t, map[string]string{"PASSKEY_RP_ID": "solobill.example.com"})); err == nil {
			t.Fatal("Load() error = nil, want PASSKEY_ORIGINS required")
		}
	})

	t.Run("login limits", func(t *testing.T) {
		t.Parallel()

		cfg, err := Load(validEnv(t, map[string]string{
			"LOGIN_RATE_LIMIT_CLIENT_BURST":  "3",
			"LOGIN_RATE_LIMIT_CLIENT_PER":    "2m",
			"LOGIN_RATE_LIMIT_LOGIN_BURST":   "4",
			"LOGIN_RATE_LIMIT_LOGIN_PER":     "10m",
			"LOGIN_RATE_LIMIT_CEILING_BURST": "50",
			"LOGIN_RATE_LIMIT_CEILING_PER":   "1h",
		}))
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		a := cfg.Auth
		if a.LoginClientBurst != 3 || a.LoginClientPer != 2*time.Minute || a.LoginBurst != 4 ||
			a.LoginPer != 10*time.Minute || a.LoginCeilingBurst != 50 || a.LoginCeilingPer != time.Hour {
			t.Errorf("login limits = %+v", a)
		}
	})

	t.Run("a zero limit is refused", func(t *testing.T) {
		t.Parallel()

		if _, err := Load(validEnv(t, map[string]string{"LOGIN_RATE_LIMIT_LOGIN_BURST": "0"})); err == nil {
			t.Fatal("Load() error = nil, want a zero burst refused")
		}
	})
}
