package executor

import (
	"context"
	"errors"
	"testing"
)

func TestShellExecutor_CheckRequired(t *testing.T) {
	ex := NewShellExecutor(&FakeRunner{})

	got, err := ex.Check(context.Background(), Input{})
	if err == nil {
		t.Fatal("Check() error = nil, want missing check error")
	}
	if got.Outcome != OutcomeExecutionError {
		t.Fatalf("Outcome = %q, want %q", got.Outcome, OutcomeExecutionError)
	}
}

func TestShellExecutor_CheckSatisfiedAndDrifted(t *testing.T) {
	t.Run("exit code zero means satisfied", func(t *testing.T) {
		runner := &FakeRunner{HasResult: true, Result: Result{ExitCode: 0, Stdout: "ok"}}
		ex := NewShellExecutor(runner)

		got, err := ex.Check(context.Background(), Input{Check: "test -f /tmp/x"})
		if err != nil {
			t.Fatalf("Check() error = %v", err)
		}
		if got.Outcome != OutcomeSatisfied {
			t.Fatalf("Outcome = %q, want %q", got.Outcome, OutcomeSatisfied)
		}
	})

	t.Run("non-zero means drifted", func(t *testing.T) {
		runner := &FakeRunner{HasResult: true, Result: Result{ExitCode: 1, Stderr: "missing"}}
		ex := NewShellExecutor(runner)

		got, err := ex.Check(context.Background(), Input{Check: "test -f /tmp/x"})
		if err != nil {
			t.Fatalf("Check() error = %v", err)
		}
		if got.Outcome != OutcomeDrifted {
			t.Fatalf("Outcome = %q, want %q", got.Outcome, OutcomeDrifted)
		}
	})

	t.Run("runner error reports execution_error", func(t *testing.T) {
		runner := &FakeRunner{HasResult: true, Result: Result{ExitCode: 0}, Err: errors.New("runner failed")}
		ex := NewShellExecutor(runner)

		got, err := ex.Check(context.Background(), Input{Check: "test -f /tmp/x"})
		if err == nil {
			t.Fatal("Check() error = nil, want failure")
		}
		if got.Outcome != OutcomeExecutionError {
			t.Fatalf("Outcome = %q, want %q", got.Outcome, OutcomeExecutionError)
		}
	})
}

func TestShellExecutor_ApplyFailureHonorsOnFailureContract(t *testing.T) {
	errBoom := errors.New("boom")

	t.Run("on_failure=abort reports execution_error", func(t *testing.T) {
		runner := &FakeRunner{
			HasResult: true,
			Result:    Result{ExitCode: 42, Stderr: "failed"},
			Err:       errBoom,
		}
		ex := NewShellExecutor(runner)

		got, err := ex.Apply(context.Background(), Input{Apply: "deploy.sh", Attributes: map[string]string{"on_failure": "abort"}})
		if !errors.Is(err, errBoom) {
			t.Fatalf("Apply() error = %v, want boom", err)
		}
		if got.Outcome != OutcomeExecutionError {
			t.Fatalf("Outcome = %q, want %q", got.Outcome, OutcomeExecutionError)
		}
		if got.Message != "shell apply failed (on_failure=abort)" {
			t.Fatalf("Message = %q, want abort failure message", got.Message)
		}
	})

	t.Run("on_failure=continue reports execution_error with continue policy", func(t *testing.T) {
		runner := &FakeRunner{
			HasResult: true,
			Result:    Result{ExitCode: 17, Stderr: "failed"},
		}
		ex := NewShellExecutor(runner)

		got, err := ex.Apply(context.Background(), Input{Apply: "deploy.sh", Attributes: map[string]string{"on_failure": "continue"}})
		if err == nil {
			t.Fatal("Apply() error = nil, want failure")
		}
		if got.Outcome != OutcomeExecutionError {
			t.Fatalf("Outcome = %q, want %q", got.Outcome, OutcomeExecutionError)
		}
		if got.Message != "shell apply failed (on_failure=continue)" {
			t.Fatalf("Message = %q, want continue failure message", got.Message)
		}
	})
}

func TestShellExecutor_ApplyCommandRequired(t *testing.T) {
	ex := NewShellExecutor(&FakeRunner{})

	got, err := ex.Apply(context.Background(), Input{})
	if err == nil {
		t.Fatal("Apply() error = nil, want missing command error")
	}
	if got.Outcome != OutcomeExecutionError {
		t.Fatalf("Outcome = %q, want %q", got.Outcome, OutcomeExecutionError)
	}
}

func TestShellExecutor_ApplyRejectsInvalidOnFailurePolicy(t *testing.T) {
	runner := &FakeRunner{HasResult: true, Result: Result{ExitCode: 0}}
	ex := NewShellExecutor(runner)

	got, err := ex.Apply(context.Background(), Input{
		Apply:      "deploy.sh",
		Attributes: map[string]string{"on_failure": "ignore"},
	})
	if err == nil {
		t.Fatal("Apply() error = nil, want invalid on_failure error")
	}
	if got.Outcome != OutcomeExecutionError {
		t.Fatalf("Outcome = %q, want %q", got.Outcome, OutcomeExecutionError)
	}
	if got.Message != "shell executor \"\": invalid on_failure value \"ignore\" (allowed: abort, continue)" {
		t.Fatalf("Message = %q, want actionable invalid-policy message", got.Message)
	}
	if len(runner.Calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0", len(runner.Calls))
	}
}
