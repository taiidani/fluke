package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/taiidani/fluke/internal/httpserver"
	"github.com/taiidani/fluke/internal/manifest"
	"github.com/taiidani/fluke/internal/reconcile"
	"github.com/taiidani/fluke/internal/server"
	"github.com/taiidani/fluke/pkg/config"
)

func runServer(args []string) error {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to server HCL config file (required)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: fluke server --config <path>\n\nFlags:\n")
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

	cfg, err := config.LoadServer(*configPath)
	if err != nil {
		return err
	}

	log := config.NewLogger(cfg.Log)
	log.Info("starting fluke server",
		"version", version,
		"grpc", cfg.ListenGRPC,
		"http", cfg.ListenHTTP,
	)

	// ── Context wired to OS signals ───────────────────────────────────────────

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// ── Reconciler ────────────────────────────────────────────────────────────

	driftPolicy, err := parseDriftPolicy(cfg.Drift.Policy)
	if err != nil {
		return err
	}

	rec := reconcile.New(
		0,
		driftPolicy,
		cfg.Drift.AlertWebhook,
		log,
	)

	configDir := filepath.Dir(*configPath)
	if err := loadManifestTasksAtStartup(rec, driftPolicy, configDir, cfg.Git.ManifestGlob); err != nil {
		return err
	}

	// ── gRPC server ───────────────────────────────────────────────────────────

	srv := server.New(rec, cfg.AgentTokens)

	// ── HTTP server ───────────────────────────────────────────────────────────

	httpSrv, err := httpserver.New(rec, log)
	if err != nil {
		return fmt.Errorf("create HTTP server: %w", err)
	}

	// ── Run ───────────────────────────────────────────────────────────────────

	errCh := make(chan error, 4)

	go func() {
		log.Info("reconciler started")
		errCh <- rec.Run(ctx)
	}()

	go func() {
		log.Info("gRPC server listening", "addr", cfg.ListenGRPC)
		errCh <- srv.Serve(ctx, cfg.ListenGRPC)
	}()

	go func() {
		log.Info("HTTP server listening", "addr", cfg.ListenHTTP)
		errCh <- httpSrv.Serve(ctx, cfg.ListenHTTP)
	}()

	go func() {
		poller := &manifest.Poller{
			RootDir:  configDir,
			Pattern:  cfg.Git.ManifestGlob,
			Interval: cfg.Git.PollInterval,
			Discover: discoverManifestPaths,
			OnChange: func(paths []string) error {
				return enqueueManifestUpdate(rec, driftPolicy, paths)
			},
		}
		log.Info("manifest poller started", "interval", poller.Interval)
		errCh <- poller.Run(ctx)
	}()

	// Wait for first fatal error or context cancellation.
	if err := <-errCh; err != nil && err != context.Canceled {
		return fmt.Errorf("server error: %w", err)
	}

	log.Info("server stopped")
	return nil
}

func loadManifestTasksAtStartup(
	rec *reconcile.Reconciler,
	defaultPolicy reconcile.DriftPolicy,
	baseDir string,
	manifestGlob string,
) error {
	manifestPaths, err := discoverManifestPaths(baseDir, manifestGlob)
	if err != nil {
		return fmt.Errorf("discover manifests: %w", err)
	}

	if len(manifestPaths) == 0 {
		return nil
	}

	if err := enqueueManifestUpdate(rec, defaultPolicy, manifestPaths); err != nil {
		return fmt.Errorf("enqueue startup manifest update: %w", err)
	}

	return nil
}

func enqueueManifestUpdate(
	rec *reconcile.Reconciler,
	defaultPolicy reconcile.DriftPolicy,
	manifestPaths []string,
) error {

	model, err := manifest.ParseFiles(manifestPaths)
	if err != nil {
		return fmt.Errorf("load manifests: %w", err)
	}

	tasks, err := reconcile.CompileTasks(model.Tasks, defaultPolicy)
	if err != nil {
		return fmt.Errorf("compile manifests for reconcile: %w", err)
	}

	if err := rec.Send(reconcile.ManifestUpdatedEvent(tasks)); err != nil {
		return fmt.Errorf("enqueue manifest update: %w", err)
	}

	return nil
}

func discoverManifestPaths(root, pattern string) ([]string, error) {
	if strings.TrimSpace(pattern) == "" {
		pattern = "**/*.fluke.hcl"
	}

	paths := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if globMatch(pattern, rel) {
			paths = append(paths, filepath.Clean(path))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(paths)
	return paths, nil
}

func globMatch(pattern, value string) bool {
	patternParts := splitPath(pattern)
	valueParts := splitPath(value)
	return matchGlobParts(patternParts, valueParts)
}

func splitPath(p string) []string {
	p = strings.TrimSpace(p)
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

func matchGlobParts(pattern, value []string) bool {
	if len(pattern) == 0 {
		return len(value) == 0
	}

	head := pattern[0]
	if head == "**" {
		if matchGlobParts(pattern[1:], value) {
			return true
		}
		if len(value) == 0 {
			return false
		}
		return matchGlobParts(pattern, value[1:])
	}

	if len(value) == 0 {
		return false
	}

	ok, err := path.Match(head, value[0])
	if err != nil || !ok {
		return false
	}

	return matchGlobParts(pattern[1:], value[1:])
}

// parseDriftPolicy converts a config string to a reconcile.DriftPolicy.
func parseDriftPolicy(s string) (reconcile.DriftPolicy, error) {
	switch s {
	case "", "remediate":
		return reconcile.DriftPolicyRemediate, nil
	case "remediate_and_alert":
		return reconcile.DriftPolicyRemediateAndAlert, nil
	case "alert_only":
		return reconcile.DriftPolicyAlertOnly, nil
	default:
		return 0, fmt.Errorf("unknown drift policy %q: must be remediate, remediate_and_alert, or alert_only", s)
	}
}
