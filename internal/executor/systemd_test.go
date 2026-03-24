package executor

import (
	"context"
	"strings"
	"testing"
)

func TestSystemdExecutor_CheckSatisfiedAndDrifted(t *testing.T) {
	t.Run("running+enabled is satisfied", func(t *testing.T) {
		runner := &FakeRunner{
			Results: []Result{{ExitCode: 0, Stdout: "active\n"}, {ExitCode: 0, Stdout: "enabled\n"}},
		}
		ex := NewSystemdExecutor(runner)

		got, err := ex.Check(context.Background(), Input{Attributes: map[string]string{"unit": "app.service", "state": "running", "enabled": "true"}})
		if err != nil {
			t.Fatalf("Check() error = %v", err)
		}
		if got.Outcome != OutcomeSatisfied {
			t.Fatalf("Outcome = %q, want %q", got.Outcome, OutcomeSatisfied)
		}
		if len(runner.Calls) != 2 {
			t.Fatalf("len(calls) = %d, want 2", len(runner.Calls))
		}
	})

	t.Run("mismatch is drifted", func(t *testing.T) {
		runner := &FakeRunner{
			Results: []Result{{ExitCode: 3, Stdout: "inactive\n"}, {ExitCode: 1, Stdout: "disabled\n"}},
		}
		ex := NewSystemdExecutor(runner)

		got, err := ex.Check(context.Background(), Input{Attributes: map[string]string{"unit": "app.service", "state": "running", "enabled": "true"}})
		if err != nil {
			t.Fatalf("Check() error = %v", err)
		}
		if got.Outcome != OutcomeDrifted {
			t.Fatalf("Outcome = %q, want %q", got.Outcome, OutcomeDrifted)
		}
	})
}

func TestSystemdExecutor_ApplyRunsOnlyRequiredTransitions(t *testing.T) {
	t.Run("inactive+disabled with desired running+enabled runs start then enable", func(t *testing.T) {
		runner := &FakeRunner{
			Results: []Result{
				{ExitCode: 3, Stdout: "inactive\n"},
				{ExitCode: 1, Stdout: "disabled\n"},
				{ExitCode: 0},
				{ExitCode: 0},
			},
		}
		ex := NewSystemdExecutor(runner)

		got, err := ex.Apply(context.Background(), Input{Attributes: map[string]string{"unit": "app.service", "state": "running", "enabled": "true"}})
		if err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if got.Outcome != OutcomeApplied {
			t.Fatalf("Outcome = %q, want %q", got.Outcome, OutcomeApplied)
		}
		if len(runner.Calls) != 4 {
			t.Fatalf("len(calls) = %d, want 4", len(runner.Calls))
		}

		if runner.Calls[2].Command != "systemctl" || len(runner.Calls[2].Args) != 2 || runner.Calls[2].Args[0] != "start" || runner.Calls[2].Args[1] != "app.service" {
			t.Fatalf("call[2] = %#v, want systemctl start app.service", runner.Calls[2])
		}
		if runner.Calls[3].Command != "systemctl" || len(runner.Calls[3].Args) != 2 || runner.Calls[3].Args[0] != "enable" || runner.Calls[3].Args[1] != "app.service" {
			t.Fatalf("call[3] = %#v, want systemctl enable app.service", runner.Calls[3])
		}
	})

	t.Run("already desired does not run transitions", func(t *testing.T) {
		runner := &FakeRunner{
			Results: []Result{{ExitCode: 0, Stdout: "active\n"}, {ExitCode: 0, Stdout: "enabled\n"}},
		}
		ex := NewSystemdExecutor(runner)

		got, err := ex.Apply(context.Background(), Input{Attributes: map[string]string{"unit": "app.service", "state": "running", "enabled": "true"}})
		if err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if got.Outcome != OutcomeApplied {
			t.Fatalf("Outcome = %q, want %q", got.Outcome, OutcomeApplied)
		}
		if len(runner.Calls) != 2 {
			t.Fatalf("len(calls) = %d, want 2", len(runner.Calls))
		}
	})

	t.Run("transition non-zero exit reports execution_error", func(t *testing.T) {
		runner := &FakeRunner{
			Results: []Result{
				{ExitCode: 3, Stdout: "inactive\n"},
				{ExitCode: 1, Stdout: "disabled\n"},
				{ExitCode: 5, Stderr: "start failed"},
			},
		}
		ex := NewSystemdExecutor(runner)

		got, err := ex.Apply(context.Background(), Input{Attributes: map[string]string{"unit": "app.service", "state": "running", "enabled": "true"}})
		if err == nil {
			t.Fatal("Apply() error = nil, want failure")
		}
		if got.Outcome != OutcomeExecutionError {
			t.Fatalf("Outcome = %q, want %q", got.Outcome, OutcomeExecutionError)
		}
		if !strings.Contains(got.Message, "exited with code 5") {
			t.Fatalf("Message = %q, want exit code detail", got.Message)
		}
	})

	t.Run("transition runner error preserves result diagnostics", func(t *testing.T) {
		runner := &FakeRunner{
			Results: []Result{
				{ExitCode: 3, Stdout: "inactive\n"},
				{ExitCode: 1, Stdout: "disabled\n"},
				{ExitCode: 9, Stdout: "partial out", Stderr: "partial err"},
			},
			Errs: []error{nil, nil, context.DeadlineExceeded},
		}
		ex := NewSystemdExecutor(runner)

		got, err := ex.Apply(context.Background(), Input{Attributes: map[string]string{"unit": "app.service", "state": "running", "enabled": "true"}})
		if err == nil {
			t.Fatal("Apply() error = nil, want failure")
		}
		if got.Outcome != OutcomeExecutionError {
			t.Fatalf("Outcome = %q, want %q", got.Outcome, OutcomeExecutionError)
		}
		if got.ExitCode != 9 {
			t.Fatalf("ExitCode = %d, want 9", got.ExitCode)
		}
		if got.Stdout != "partial out" {
			t.Fatalf("Stdout = %q, want %q", got.Stdout, "partial out")
		}
		if got.Stderr != "partial err" {
			t.Fatalf("Stderr = %q, want %q", got.Stderr, "partial err")
		}
	})
}
