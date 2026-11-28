// Command site-frontend serves the prerendered solobill marketing site: the
// build of landing/ in FRONTEND_BUILD_DIR, through internal/siteserver
// (govue's frontend package in prerendered mode), on its public listener, and
// /healthz, /readyz and /metrics on a second, administrative listener. It
// also serves /config.json, the login and register links and the registration
// switch of SITE_LOGIN_URL, SITE_REGISTER_URL and SITE_REGISTRATION_ENABLED,
// which the landing reads at startup. It holds no secret and serves no API.
// This file only wires configuration, logger and shutdown.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ostendorf-cloud/govue/logging"

	"github.com/ostendorf-cloud/solobill/internal/frontendconfig"
	"github.com/ostendorf-cloud/solobill/internal/siteserver"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := frontendconfig.LoadWithBuildDir(os.Getenv, frontendconfig.DefaultSiteBuildDir)
	if err != nil {
		return err
	}
	logger := logging.New(os.Stdout, cfg.LogLevel)

	links, err := siteserver.LoadLinks(os.Getenv)
	if err != nil {
		return err
	}

	s, err := siteserver.NewFromDir(cfg, logger, siteserver.Options{Links: &links})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Run drains readiness before shutdown, on both listeners together.
	return s.Run(ctx, cfg.ListenAddr, cfg.AdminListenAddr)
}
