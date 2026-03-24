package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/taiidani/fluke/internal/agent"
	"github.com/taiidani/fluke/pkg/config"
)

func runAgent(args []string) error {
	fs := flag.NewFlagSet("agent", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to agent HCL config file (required)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: fluke agent --config <path>\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" {
		fs.Usage()
		return fmt.Errorf("--config is required")
	}

	// ── Load config ───────────────────────────────────────────────────────────

	cfg, err := config.LoadAgent(*configPath)
	if err != nil {
		return err
	}

	log := config.NewLogger(cfg.Log)
	log.Info("starting fluke agent", "version", version)

	// ── Context wired to OS signals ───────────────────────────────────────────

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// ── Build and run agent ───────────────────────────────────────────────────

	a, err := agent.New(agent.Config{
		ServerURL:      cfg.ServerURL,
		Token:          cfg.Token,
		Labels:         cfg.Labels,
		DefaultShell:   cfg.Execution.DefaultShell,
		CommandTimeout: cfg.Execution.CommandTimeout,
		MaxConcurrency: 1,
		// TODO: wire cfg.TLS into grpc.DialOption when pkg/config TLS support
		// is plumbed through internal/agent.
	}, log)
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}

	if err := a.Run(ctx); err != nil && err != context.Canceled {
		return fmt.Errorf("agent error: %w", err)
	}

	log.Info("agent stopped")
	return nil
}
