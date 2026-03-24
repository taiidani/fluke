package executor

import (
	"context"
	"errors"
	"testing"
)

func TestRunnerFake_ReturnsConfiguredResult(t *testing.T) {
	fake := &FakeRunner{HasResult: true, Result: Result{ExitCode: 1, Stderr: "boom"}}

	got, err := fake.Run(context.Background(), Request{Command: "x"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got.ExitCode != 1 {
		t.Fatalf("ExitCode = %d, want 1", got.ExitCode)
	}
	if got.Stderr != "boom" {
		t.Fatalf("Stderr = %q, want %q", got.Stderr, "boom")
	}
}

func TestRunnerFake_ZeroValueReturnsNoResultConfigured(t *testing.T) {
	fake := &FakeRunner{}

	got, err := fake.Run(context.Background(), Request{Command: "x"})
	if !errors.Is(err, ErrNoResultConfigured) {
		t.Fatalf("Run() error = %v, want ErrNoResultConfigured", err)
	}
	if got != (Result{}) {
		t.Fatalf("Run() result = %#v, want zero Result", got)
	}
}

func TestRunnerFake_QueuedResultsAreDeterministic(t *testing.T) {
	fake := &FakeRunner{
		Results: []Result{
			{ExitCode: 0, Stdout: "first"},
			{ExitCode: 2, Stderr: "second"},
		},
	}

	first, err := fake.Run(context.Background(), Request{Command: "cmd"})
	if err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	second, err := fake.Run(context.Background(), Request{Command: "cmd"})
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	_, err = fake.Run(context.Background(), Request{Command: "cmd"})
	if !errors.Is(err, ErrNoResultConfigured) {
		t.Fatalf("third Run() error = %v, want ErrNoResultConfigured", err)
	}

	if first.Stdout != "first" || first.ExitCode != 0 {
		t.Fatalf("first result = %#v, want stdout first exit 0", first)
	}
	if second.Stderr != "second" || second.ExitCode != 2 {
		t.Fatalf("second result = %#v, want stderr second exit 2", second)
	}
}

func TestRunnerFake_RecordsRequestSnapshot(t *testing.T) {
	fake := &FakeRunner{HasResult: true, Result: Result{ExitCode: 0}}
	request := Request{
		Command: "tool",
		Args:    []string{"a"},
		Env:     map[string]string{"K": "V"},
	}

	if _, err := fake.Run(context.Background(), request); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	request.Args[0] = "changed"
	request.Env["K"] = "changed"

	if len(fake.Calls) != 1 {
		t.Fatalf("len(Calls) = %d, want 1", len(fake.Calls))
	}
	if got := fake.Calls[0].Args[0]; got != "a" {
		t.Fatalf("Calls[0].Args[0] = %q, want %q", got, "a")
	}
	if got := fake.Calls[0].Env["K"]; got != "V" {
		t.Fatalf("Calls[0].Env[K] = %q, want %q", got, "V")
	}
}

func TestRunnerFake_ErrAndResultBehaviorIsDeterministic(t *testing.T) {
	errOne := errors.New("e1")
	errDefault := errors.New("default")

	t.Run("returns both configured result and error", func(t *testing.T) {
		errRunnerBoom := errors.New("runner boom")
		fake := &FakeRunner{
			HasResult: true,
			Result:    Result{ExitCode: 7, Stderr: "fail"},
			Err:       errRunnerBoom,
		}

		got, err := fake.Run(context.Background(), Request{Command: "x"})
		if !errors.Is(err, errRunnerBoom) {
			t.Fatalf("Run() error = %v, want runner boom", err)
		}
		if got.ExitCode != 7 || got.Stderr != "fail" {
			t.Fatalf("Run() result = %#v, want exit 7 stderr fail", got)
		}
	})

	t.Run("queue order is stable across result and error streams", func(t *testing.T) {
		fake := &FakeRunner{
			Results: []Result{{ExitCode: 1}, {ExitCode: 2}},
			Errs:    []error{errOne, nil},
		}

		first, err := fake.Run(context.Background(), Request{Command: "x"})
		if !errors.Is(err, errOne) {
			t.Fatalf("first Run() error = %v, want e1", err)
		}
		if first.ExitCode != 1 {
			t.Fatalf("first Run() result = %#v, want exit 1", first)
		}

		second, err := fake.Run(context.Background(), Request{Command: "x"})
		if err != nil {
			t.Fatalf("second Run() error = %v, want nil", err)
		}
		if second.ExitCode != 2 {
			t.Fatalf("second Run() result = %#v, want exit 2", second)
		}
	})

	t.Run("queued results do not fall back to default result after exhaustion", func(t *testing.T) {
		fake := &FakeRunner{
			HasResult: true,
			Result:    Result{ExitCode: 99, Stdout: "default"},
			Results:   []Result{{ExitCode: 1, Stdout: "queued"}},
		}

		first, err := fake.Run(context.Background(), Request{Command: "x"})
		if err != nil {
			t.Fatalf("first Run() error = %v, want nil", err)
		}
		if first.ExitCode != 1 || first.Stdout != "queued" {
			t.Fatalf("first Run() result = %#v, want queued result", first)
		}

		second, err := fake.Run(context.Background(), Request{Command: "x"})
		if !errors.Is(err, ErrNoResultConfigured) {
			t.Fatalf("second Run() error = %v, want ErrNoResultConfigured", err)
		}
		if second != (Result{}) {
			t.Fatalf("second Run() result = %#v, want zero Result", second)
		}
	})

	t.Run("queued errors do not fall back to default error after exhaustion", func(t *testing.T) {
		fake := &FakeRunner{
			HasResult: true,
			Result:    Result{ExitCode: 0},
			Err:       errDefault,
			Errs:      []error{errOne},
		}

		first, err := fake.Run(context.Background(), Request{Command: "x"})
		if !errors.Is(err, errOne) {
			t.Fatalf("first Run() error = %v, want e1", err)
		}
		if first.ExitCode != 0 {
			t.Fatalf("first Run() result = %#v, want exit 0", first)
		}

		second, err := fake.Run(context.Background(), Request{Command: "x"})
		if err != nil {
			t.Fatalf("second Run() error = %v, want nil", err)
		}
		if second.ExitCode != 0 {
			t.Fatalf("second Run() result = %#v, want exit 0", second)
		}
	})
}
