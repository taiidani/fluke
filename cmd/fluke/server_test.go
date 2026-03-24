package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	flukeproto "github.com/taiidani/fluke/internal/proto/flukeproto"
	"github.com/taiidani/fluke/internal/reconcile"
)

func TestDiscoverManifestPaths_RespectsGlob(t *testing.T) {
	root := t.TempDir()

	mustWriteFile(t, filepath.Join(root, "infra", "web", "app.fluke.hcl"), "task \"x\" {}\n")
	mustWriteFile(t, filepath.Join(root, "infra", "worker", "job.fluke.hcl"), "task \"y\" {}\n")
	mustWriteFile(t, filepath.Join(root, "services", "api.fluke.hcl"), "task \"z\" {}\n")
	mustWriteFile(t, filepath.Join(root, "infra", "readme.md"), "ignore\n")

	got, err := discoverManifestPaths(root, "infra/**/*.fluke.hcl")
	if err != nil {
		t.Fatalf("discoverManifestPaths: %v", err)
	}

	want := []string{
		filepath.Join(root, "infra", "web", "app.fluke.hcl"),
		filepath.Join(root, "infra", "worker", "job.fluke.hcl"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("manifest paths = %#v, want %#v", got, want)
	}
}

func TestDiscoverManifestPaths_DefaultGlobIncludesTree(t *testing.T) {
	root := t.TempDir()

	mustWriteFile(t, filepath.Join(root, "a.fluke.hcl"), "task \"a\" {}\n")
	mustWriteFile(t, filepath.Join(root, "nested", "b.fluke.hcl"), "task \"b\" {}\n")
	mustWriteFile(t, filepath.Join(root, "nested", "c.txt"), "ignore\n")

	got, err := discoverManifestPaths(root, "")
	if err != nil {
		t.Fatalf("discoverManifestPaths: %v", err)
	}

	want := []string{
		filepath.Join(root, "a.fluke.hcl"),
		filepath.Join(root, "nested", "b.fluke.hcl"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("manifest paths = %#v, want %#v", got, want)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

func TestEnqueueManifestUpdate_TriggersStateReportForConnectedAgent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rec := reconcile.New(time.Minute, reconcile.DriftPolicyRemediate, "", log)

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- rec.Run(ctx)
	}()

	messages := make(chan *flukeproto.ServerMessage, 4)
	err := rec.Send(reconcile.AgentConnectedEvent(
		"agent-1",
		"host-1",
		map[string]string{"role": "web"},
		func(msg *flukeproto.ServerMessage) error {
			messages <- msg
			return nil
		},
	))
	if err != nil {
		t.Fatalf("send agent connected event: %v", err)
	}

	waitFor(t, func() bool {
		snap := rec.Snapshot()
		for _, agent := range snap.Agents {
			if agent.Hostname == "host-1" && agent.Connected {
				return true
			}
		}
		return false
	})

	manifestPath := filepath.Join(t.TempDir(), "task.fluke.hcl")
	mustWriteFile(t, manifestPath, `
task "web-task" {
  selector {
    match_labels = { role = "web" }
  }

  shell "deploy" {
    check   = "true"
    command = "true"
  }
}
`)

	if err := enqueueManifestUpdate(rec, reconcile.DriftPolicyRemediate, []string{manifestPath}); err != nil {
		t.Fatalf("enqueueManifestUpdate: %v", err)
	}

	select {
	case msg := <-messages:
		req := msg.GetRequestStateReport()
		if req == nil {
			t.Fatalf("server message = %T, want RequestStateReport", msg.GetPayload())
		}
		if len(req.Tasks) != 1 || req.Tasks[0].GetName() != "web-task" {
			t.Fatalf("RequestStateReport.Tasks = %#v, want web-task", req.Tasks)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for RequestStateReport after manifest update")
	}

	cancel()
	if err := <-runErrCh; err != context.Canceled {
		t.Fatalf("Run() error = %v, want %v", err, context.Canceled)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}
