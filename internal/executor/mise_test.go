package executor

import (
	"context"
	"errors"
	"testing"
)

func TestMiseExecutor_CheckSatisfiedAndDrifted(t *testing.T) {
	t.Run("exit code zero means satisfied", func(t *testing.T) {
		runner := &FakeRunner{HasResult: true, Result: Result{ExitCode: 0, Stdout: "ok"}}
		ex := NewMiseExecutor(runner)

		got, err := ex.Check(context.Background(), Input{WorkingDir: "/opt/app", Attributes: map[string]string{"check_task": "fluke-check"}})
		if err != nil {
			t.Fatalf("Check() error = %v", err)
		}
		if got.Outcome != OutcomeSatisfied {
			t.Fatalf("Outcome = %q, want %q", got.Outcome, OutcomeSatisfied)
		}
		if len(runner.Calls) != 1 {
			t.Fatalf("len(calls) = %d, want 1", len(runner.Calls))
		}
		if runner.Calls[0].Command != "mise" || len(runner.Calls[0].Args) != 2 || runner.Calls[0].Args[0] != "run" || runner.Calls[0].Args[1] != "fluke-check" {
			t.Fatalf("call[0] = %#v, want mise run fluke-check", runner.Calls[0])
		}
	})

	t.Run("non-zero means drifted", func(t *testing.T) {
		runner := &FakeRunner{HasResult: true, Result: Result{ExitCode: 2, Stderr: "drift"}}
		ex := NewMiseExecutor(runner)

		got, err := ex.Check(context.Background(), Input{WorkingDir: "/opt/app", Attributes: map[string]string{"check_task": "check"}})
		if err != nil {
			t.Fatalf("Check() error = %v", err)
		}
		if got.Outcome != OutcomeDrifted {
			t.Fatalf("Outcome = %q, want %q", got.Outcome, OutcomeDrifted)
		}
	})

	t.Run("missing check_task defaults to check", func(t *testing.T) {
		runner := &FakeRunner{HasResult: true, Result: Result{ExitCode: 0}}
		ex := NewMiseExecutor(runner)

		got, err := ex.Check(context.Background(), Input{WorkingDir: "/opt/app"})
		if err != nil {
			t.Fatalf("Check() error = %v", err)
		}
		if got.Outcome != OutcomeSatisfied {
			t.Fatalf("Outcome = %q, want %q", got.Outcome, OutcomeSatisfied)
		}
		if len(runner.Calls) != 1 {
			t.Fatalf("len(calls) = %d, want 1", len(runner.Calls))
		}
		if runner.Calls[0].Command != "mise" || len(runner.Calls[0].Args) != 2 || runner.Calls[0].Args[0] != "run" || runner.Calls[0].Args[1] != "check" {
			t.Fatalf("call[0] = %#v, want mise run check", runner.Calls[0])
		}
	})
}

func TestMiseExecutor_ApplyRunsApplyTask(t *testing.T) {
	runner := &FakeRunner{HasResult: true, Result: Result{ExitCode: 0}}
	ex := NewMiseExecutor(runner)

	got, err := ex.Apply(context.Background(), Input{WorkingDir: "/opt/app", Attributes: map[string]string{"apply_task": "fluke-apply"}})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if got.Outcome != OutcomeApplied {
		t.Fatalf("Outcome = %q, want %q", got.Outcome, OutcomeApplied)
	}
	if len(runner.Calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(runner.Calls))
	}
	if runner.Calls[0].Command != "mise" || len(runner.Calls[0].Args) != 2 || runner.Calls[0].Args[0] != "run" || runner.Calls[0].Args[1] != "fluke-apply" {
		t.Fatalf("call[0] = %#v, want mise run fluke-apply", runner.Calls[0])
	}
}

func TestMiseExecutor_ApplyDefaultsTaskName(t *testing.T) {
	runner := &FakeRunner{HasResult: true, Result: Result{ExitCode: 0}}
	ex := NewMiseExecutor(runner)

	got, err := ex.Apply(context.Background(), Input{WorkingDir: "/opt/app"})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if got.Outcome != OutcomeApplied {
		t.Fatalf("Outcome = %q, want %q", got.Outcome, OutcomeApplied)
	}
	if len(runner.Calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(runner.Calls))
	}
	if runner.Calls[0].Command != "mise" || len(runner.Calls[0].Args) != 2 || runner.Calls[0].Args[0] != "run" || runner.Calls[0].Args[1] != "apply" {
		t.Fatalf("call[0] = %#v, want mise run apply", runner.Calls[0])
	}
}

func TestMiseExecutor_CheckRunsGitPrepBeforeTask(t *testing.T) {
	runner := &FakeRunner{
		Results: []Result{{ExitCode: 0}, {ExitCode: 0}, {ExitCode: 0}},
	}
	ex := NewMiseExecutor(runner)

	got, err := ex.Check(context.Background(), Input{
		WorkingDir: "/opt/app",
		Attributes: map[string]string{"check_task": "check"},
		Git:        &GitInput{URL: "https://example.com/org/app.git", Branch: "main"},
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if got.Outcome != OutcomeSatisfied {
		t.Fatalf("Outcome = %q, want %q", got.Outcome, OutcomeSatisfied)
	}
	if len(runner.Calls) != 3 {
		t.Fatalf("len(calls) = %d, want 3", len(runner.Calls))
	}

	if runner.Calls[0].Command != "git" || len(runner.Calls[0].Args) != 4 || runner.Calls[0].Args[0] != "-C" || runner.Calls[0].Args[1] != "/opt/app" || runner.Calls[0].Args[2] != "rev-parse" || runner.Calls[0].Args[3] != "--is-inside-work-tree" {
		t.Fatalf("call[0] = %#v, want git -C /opt/app rev-parse --is-inside-work-tree", runner.Calls[0])
	}
	if runner.Calls[1].Command != "git" || len(runner.Calls[1].Args) != 6 || runner.Calls[1].Args[0] != "-C" || runner.Calls[1].Args[1] != "/opt/app" || runner.Calls[1].Args[2] != "pull" || runner.Calls[1].Args[3] != "--ff-only" || runner.Calls[1].Args[4] != "origin" || runner.Calls[1].Args[5] != "main" {
		t.Fatalf("call[1] = %#v, want git pull --ff-only origin main", runner.Calls[1])
	}
	if runner.Calls[2].Command != "mise" || len(runner.Calls[2].Args) != 2 || runner.Calls[2].Args[0] != "run" || runner.Calls[2].Args[1] != "check" {
		t.Fatalf("call[2] = %#v, want mise run check", runner.Calls[2])
	}
}

func TestMiseExecutor_CheckGitPrepCloneWhenRepoAbsent(t *testing.T) {
	runner := &FakeRunner{
		Results: []Result{{ExitCode: 1}, {ExitCode: 0}, {ExitCode: 0}},
	}
	ex := NewMiseExecutor(runner)

	got, err := ex.Check(context.Background(), Input{
		WorkingDir: "/opt/app",
		Git:        &GitInput{URL: "https://example.com/org/app.git"},
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if got.Outcome != OutcomeSatisfied {
		t.Fatalf("Outcome = %q, want %q", got.Outcome, OutcomeSatisfied)
	}
	if len(runner.Calls) != 3 {
		t.Fatalf("len(calls) = %d, want 3", len(runner.Calls))
	}
	if runner.Calls[1].Command != "git" || len(runner.Calls[1].Args) != 6 || runner.Calls[1].Args[0] != "clone" || runner.Calls[1].Args[1] != "--branch" || runner.Calls[1].Args[2] != "main" || runner.Calls[1].Args[3] != "--single-branch" || runner.Calls[1].Args[4] != "https://example.com/org/app.git" || runner.Calls[1].Args[5] != "/opt/app" {
		t.Fatalf("call[1] = %#v, want git clone --branch main --single-branch <url> /opt/app", runner.Calls[1])
	}
}

func TestMiseExecutor_CheckGitURLRequiredWhenGitBlockPresent(t *testing.T) {
	ex := NewMiseExecutor(&FakeRunner{})

	got, err := ex.Check(context.Background(), Input{WorkingDir: "/opt/app", Git: &GitInput{URL: "  "}})
	if err == nil {
		t.Fatal("Check() error = nil, want missing git.url error")
	}
	if got.Outcome != OutcomeExecutionError {
		t.Fatalf("Outcome = %q, want %q", got.Outcome, OutcomeExecutionError)
	}
	if got.Message != "mise executor \"\": git.url is required when git is configured" {
		t.Fatalf("Message = %q, want actionable missing git.url message", got.Message)
	}
}

func TestMiseExecutor_GitPrepFailureIsCheckExecutionError(t *testing.T) {
	errBoom := errors.New("git prep failed")
	runner := &FakeRunner{
		Results: []Result{{ExitCode: 27, Stderr: "could not fetch"}},
		Errs:    []error{errBoom},
	}
	ex := NewMiseExecutor(runner)

	got, err := ex.Check(context.Background(), Input{
		WorkingDir: "/opt/app",
		Attributes: map[string]string{"check_task": "check"},
		Git:        &GitInput{URL: "https://example.com/org/app.git", Branch: "main"},
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("Check() error = %v, want git prep failure", err)
	}
	if got.Outcome != OutcomeExecutionError {
		t.Fatalf("Outcome = %q, want %q", got.Outcome, OutcomeExecutionError)
	}
	if len(runner.Calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1 (git prep only)", len(runner.Calls))
	}
	if runner.Calls[0].Command != "git" {
		t.Fatalf("call[0].Command = %q, want git", runner.Calls[0].Command)
	}
}

func TestMiseExecutor_MissingRequiredAttributesAreActionable(t *testing.T) {
	t.Run("working_dir is required", func(t *testing.T) {
		ex := NewMiseExecutor(&FakeRunner{})
		got, err := ex.Check(context.Background(), Input{})
		if err == nil {
			t.Fatal("Check() error = nil, want missing working_dir error")
		}
		if got.Outcome != OutcomeExecutionError {
			t.Fatalf("Outcome = %q, want %q", got.Outcome, OutcomeExecutionError)
		}
		if got.Message != "mise executor \"\": working_dir is required" {
			t.Fatalf("Message = %q, want actionable missing working_dir message", got.Message)
		}
	})
}
