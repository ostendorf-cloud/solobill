package main

import (
	"slices"
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/config"
)

func TestPasskeyConfig(t *testing.T) {
	t.Parallel()

	if got := passkeyConfig(config.Config{}); got != nil {
		t.Errorf("passkeyConfig without PASSKEY_RP_ID = %+v, want nil, passkeys off", got)
	}

	got := passkeyConfig(config.Config{
		SessionAllowPlainHTTP: true,
		Auth: config.AuthConfig{
			PasskeysEnabled:      true,
			PasskeyRPID:          "localhost",
			PasskeyRPDisplayName: "solobill",
			PasskeyOrigins:       []string{"http://localhost:8080"},
		},
	})
	if got == nil {
		t.Fatal("passkeyConfig with PASSKEY_RP_ID = nil, want the relying party")
	}
	if got.RPID != "localhost" || got.RPDisplayName != "solobill" ||
		!slices.Equal(got.Origins, []string{"http://localhost:8080"}) || !got.AllowPlainHTTP {
		t.Errorf("passkeyConfig = %+v", got)
	}
}
