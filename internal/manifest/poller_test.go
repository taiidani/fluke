package manifest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestPoller_InvokesOnChangeWhenFingerprintChanges(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.fluke.hcl")
	pathB := filepath.Join(dir, "b.fluke.hcl")
	mustWritePollerFile(t, pathA, "task \"a\" {}\n")
	mustWritePollerFile(t, pathB, "task \"b\" {}\n")

	var discoverCalls atomic.Int32
	want := []string{pathA, pathB}
	gotCh := make(chan []string, 1)

	p := &Poller{
		Interval: 5 * time.Millisecond,
		Discover: func(string, string) ([]string, error) {
			if discoverCalls.Add(1) == 1 {
				return []string{pathA}, nil
			}
			return []string{pathA, pathB}, nil
		},
		OnChange: func(paths []string) error {
			gotCh <- paths
			cancel()
			return nil
		},
	}

	err := p.Run(ctx)
	if err != context.Canceled {
		t.Fatalf("Run() error = %v, want %v", err, context.Canceled)
	}

	select {
	case got := <-gotCh:
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("OnChange paths = %#v, want %#v", got, want)
		}
	default:
		t.Fatal("OnChange was not called")
	}
}

func TestPoller_DoesNotInvokeOnUnchangedFingerprint(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.fluke.hcl")
	mustWritePollerFile(t, pathA, "task \"a\" {}\n")

	var discoverCalls atomic.Int32
	called := make(chan struct{}, 1)

	p := &Poller{
		Interval: 2 * time.Millisecond,
		Discover: func(string, string) ([]string, error) {
			if discoverCalls.Add(1) >= 3 {
				cancel()
			}
			return []string{pathA}, nil
		},
		OnChange: func([]string) error {
			called <- struct{}{}
			return nil
		},
	}

	err := p.Run(ctx)
	if err != context.Canceled {
		t.Fatalf("Run() error = %v, want %v", err, context.Canceled)
	}

	select {
	case <-called:
		t.Fatal("OnChange called for unchanged fingerprint")
	default:
	}
}

func TestPoller_ContentOnlyChangeTriggersOnChange(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "task.fluke.hcl")
	mustWritePollerFile(t, manifestPath, "task \"a\" {}\n")

	var discoverCalls atomic.Int32
	called := make(chan []string, 1)

	p := &Poller{
		Interval: 2 * time.Millisecond,
		Discover: func(string, string) ([]string, error) {
			if discoverCalls.Add(1) == 2 {
				mustWritePollerFile(t, manifestPath, "task \"b\" {}\n")
			}
			return []string{manifestPath}, nil
		},
		OnChange: func(paths []string) error {
			called <- paths
			cancel()
			return nil
		},
	}

	err := p.Run(ctx)
	if err != context.Canceled {
		t.Fatalf("Run() error = %v, want %v", err, context.Canceled)
	}

	select {
	case got := <-called:
		if !reflect.DeepEqual(got, []string{manifestPath}) {
			t.Fatalf("OnChange paths = %#v, want %#v", got, []string{manifestPath})
		}
	default:
		t.Fatal("OnChange was not called for content-only change")
	}
}

func TestPoller_OnChangeFailureRetriesWithSameFingerprint(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "task.fluke.hcl")
	mustWritePollerFile(t, manifestPath, "task \"a\" {}\n")

	var discoverCalls atomic.Int32
	var onChangeCalls atomic.Int32
	failErr := errors.New("transient enqueue failure")

	p := &Poller{
		Interval: 2 * time.Millisecond,
		Discover: func(string, string) ([]string, error) {
			if discoverCalls.Add(1) == 2 {
				mustWritePollerFile(t, manifestPath, "task \"b\" {}\n")
			}
			return []string{manifestPath}, nil
		},
		OnChange: func([]string) error {
			if onChangeCalls.Add(1) == 1 {
				return failErr
			}
			cancel()
			return nil
		},
	}

	err := p.Run(ctx)
	if err != context.Canceled {
		t.Fatalf("Run() error = %v, want %v", err, context.Canceled)
	}

	if got := onChangeCalls.Load(); got != 2 {
		t.Fatalf("OnChange calls = %d, want 2", got)
	}
}

func mustWritePollerFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
