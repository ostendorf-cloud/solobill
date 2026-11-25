package frontendconfig_test

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ostendorf-cloud/solobill/internal/frontendconfig"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := frontendconfig.Load(env(nil))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ListenAddr != ":8080" || cfg.AdminListenAddr != ":8081" {
		t.Errorf("listen addresses = %q, %q, want :8080, :8081", cfg.ListenAddr, cfg.AdminListenAddr)
	}
	if cfg.BuildDir != "/usr/share/solobill/web" {
		t.Errorf("BuildDir = %q, want the image's /usr/share/solobill/web", cfg.BuildDir)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want info", cfg.LogLevel)
	}
	if cfg.AllowPlainHTTP || cfg.Development() || cfg.DevAPIProxy != "" {
		t.Errorf("plain HTTP or the development switch is on by default: %+v", cfg)
	}
	if len(cfg.TrustedProxies) != 0 {
		t.Errorf("TrustedProxies = %v, want none", cfg.TrustedProxies)
	}
	if cfg.RateLimitBurst != frontendconfig.DefaultRateLimitBurst || cfg.RateLimitPer != time.Minute {
		t.Errorf("rate limit = %d per %v, want the defaults", cfg.RateLimitBurst, cfg.RateLimitPer)
	}
}

func TestLoadReadsEveryVariable(t *testing.T) {
	t.Parallel()

	cfg, err := frontendconfig.Load(env(map[string]string{
		"LISTEN_ADDR":            ":9000",
		"ADMIN_LISTEN_ADDR":      ":9001",
		"LOG_LEVEL":              "debug",
		"FRONTEND_BUILD_DIR":     "/srv/web",
		"TRUSTED_PROXIES":        "10.0.0.0/8,192.168.1.1/32",
		"RATE_LIMIT_BURST":       "50",
		"RATE_LIMIT_PER":         "30s",
		"ALLOW_PLAIN_HTTP":       "true",
		"FRONTEND_DEV_API_PROXY": "http://api:8080",
	}))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ListenAddr != ":9000" || cfg.AdminListenAddr != ":9001" || cfg.BuildDir != "/srv/web" {
		t.Errorf("addresses or build dir not read: %+v", cfg)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want debug", cfg.LogLevel)
	}
	if len(cfg.TrustedProxies) != 2 {
		t.Errorf("TrustedProxies = %v, want two prefixes", cfg.TrustedProxies)
	}
	if cfg.RateLimitBurst != 50 || cfg.RateLimitPer != 30*time.Second {
		t.Errorf("rate limit = %d per %v, want 50 per 30s", cfg.RateLimitBurst, cfg.RateLimitPer)
	}
	if !cfg.AllowPlainHTTP || !cfg.Development() || cfg.DevAPIProxy != "http://api:8080" {
		t.Errorf("plain HTTP or the development switch not read: %+v", cfg)
	}
}

// TestLoadCollectsEveryProblem pins that every invalid variable is named in
// one error, not only the first.
func TestLoadCollectsEveryProblem(t *testing.T) {
	t.Parallel()

	_, err := frontendconfig.Load(env(map[string]string{
		"LOG_LEVEL":          "loud",
		"TRUSTED_PROXIES":    "not-a-cidr",
		"ADMIN_LISTEN_ADDR":  "",
		"RATE_LIMIT_BURST":   "0",
		"FRONTEND_BUILD_DIR": "",
	}))
	if err == nil {
		t.Fatal("Load() error = nil, want one")
	}
	for _, name := range []string{"LOG_LEVEL", "TRUSTED_PROXIES", "RATE_LIMIT_BURST"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name %s", err, name)
		}
	}
}
