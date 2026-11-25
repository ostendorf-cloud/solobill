// Command frontend serves the solobill Vue app: the Vite build in
// FRONTEND_BUILD_DIR, through internal/frontendserver (govue's frontend
// package in app mode), on its public listener, and /healthz, /readyz and
// /metrics on a second, administrative listener. It holds no secret and
// serves no API: the Ingress routes /api to the API image (cmd/solobill).
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
	"github.com/ostendorf-cloud/solobill/internal/frontendserver"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := frontendconfig.Load(os.Getenv)
	if err != nil {
		return err
	}
	logger := logging.New(os.Stdout, cfg.LogLevel)

	s, err := frontendserver.NewFromDir(cfg, logger, frontendserver.Options{})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Run drains readiness before shutdown, on both listeners together.
	return s.Run(ctx, cfg.ListenAddr, cfg.AdminListenAddr)
}
