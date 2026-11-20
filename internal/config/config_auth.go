package config

import (
	"errors"
	"time"

	"github.com/ostendorf-cloud/govue/envconf"
)

// AuthConfig is the configuration of govue's account and passkey packages
// (issue #86): the login rate limits and the passkey relying party.
type AuthConfig struct {
	// The limits in front of the routes that verify a password, a TOTP
	// code or a passkey. Zero leaves internal/httpapi's default, the
	// wishlist adoption's values: 5 per minute per client address, 10 per
	// 15 minutes per login and address, 100 per hour per login. See
	// httpapi.LoginLimits.
	LoginClientBurst  int
	LoginClientPer    time.Duration
	LoginBurst        int
	LoginPer          time.Duration
	LoginCeilingBurst int
	LoginCeilingPer   time.Duration

	// PasskeysEnabled is true exactly when PasskeyRPID is set.
	PasskeysEnabled bool

	// PasskeyRPID is the WebAuthn relying party id, the host every passkey
	// is bound to, for example "solobill.example.com". It is
	// configuration, never derived from a request's Host header, and it
	// cannot change later: every passkey registered for one RP id is
	// refused for any other.
	PasskeyRPID string

	// PasskeyRPDisplayName is the name an authenticator shows next to a
	// passkey.
	PasskeyRPDisplayName string

	// PasskeyOrigins are the exact origins a passkey ceremony may come
	// from, each https (http only on localhost) with a host that is
	// PasskeyRPID or a subdomain of it. Required when PasskeyRPID is set.
	// govue's passkey.New checks both at startup.
	PasskeyOrigins []string
}

// loadAuth reads the account and passkey variables into cfg and reports
// every problem through fail.
func loadAuth(l *envconf.Loader, cfg *AuthConfig, fail func(error)) {
	// Unset is zero, internal/httpapi's default. A value that is set has
	// to be positive: a limit of zero would refuse every login.
	cfg.LoginClientBurst = l.Int("LOGIN_RATE_LIMIT_CLIENT_BURST", 0, 1, 100_000)
	cfg.LoginClientPer = l.Duration("LOGIN_RATE_LIMIT_CLIENT_PER", 0, time.Second, 24*time.Hour)
	cfg.LoginBurst = l.Int("LOGIN_RATE_LIMIT_LOGIN_BURST", 0, 1, 100_000)
	cfg.LoginPer = l.Duration("LOGIN_RATE_LIMIT_LOGIN_PER", 0, time.Second, 24*time.Hour)
	cfg.LoginCeilingBurst = l.Int("LOGIN_RATE_LIMIT_CEILING_BURST", 0, 1, 1_000_000)
	cfg.LoginCeilingPer = l.Duration("LOGIN_RATE_LIMIT_CEILING_PER", 0, time.Second, 7*24*time.Hour)

	cfg.PasskeyRPID = l.String("PASSKEY_RP_ID", "")
	cfg.PasskeysEnabled = cfg.PasskeyRPID != ""
	if !cfg.PasskeysEnabled {
		return
	}

	cfg.PasskeyRPDisplayName = l.String("PASSKEY_RP_DISPLAY_NAME", "solobill")
	cfg.PasskeyOrigins = l.List("PASSKEY_ORIGINS")
	if len(cfg.PasskeyOrigins) == 0 {
		fail(errors.New("PASSKEY_ORIGINS: is required when PASSKEY_RP_ID is set"))
	}
}
