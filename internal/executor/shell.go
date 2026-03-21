package executor

import (
	"context"
	"fmt"
	"strings"
)

type ShellExecutor struct {
	runner Runner
}

func NewShellExecutor(runner Runner) *ShellExecutor {
	return &ShellExecutor{runner: runner}
}

func (s *ShellExecutor) Name() string {
	return "shell"
}

func (s *ShellExecutor) Check(ctx context.Context, in Input) (CheckResult, error) {
	if strings.TrimSpace(in.Check) == "" {
		err := fmt.Errorf("shell executor %q: check is required", in.ExecutorName)
		return CheckResult{Outcome: OutcomeExecutionError, Message: err.Error()}, err
	}

	result, err := s.runner.Run(ctx, shellRequest(in.Check, in))
	if err != nil {
		wrapped := fmt.Errorf("shell executor %q check failed: %w", in.ExecutorName, err)
		return CheckResult{
			Outcome:  OutcomeExecutionError,
			Message:  wrapped.Error(),
			ExitCode: result.ExitCode,
			Stdout:   result.Stdout,
			Stderr:   result.Stderr,
		}, wrapped
	}

	outcome := OutcomeDrifted
	message := "shell check detected drift"
	if result.ExitCode == 0 {
		outcome = OutcomeSatisfied
		message = "shell check satisfied"
	}

	return CheckResult{
		Outcome:  outcome,
		Message:  message,
		ExitCode: result.ExitCode,
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
	}, nil
}

func (s *ShellExecutor) Apply(ctx context.Context, in Input) (ApplyResult, error) {
	if strings.TrimSpace(in.Apply) == "" {
		err := fmt.Errorf("shell executor %q: apply command is required", in.ExecutorName)
		return ApplyResult{Outcome: OutcomeExecutionError, Message: err.Error()}, err
	}

	policy, err := validateShellOnFailure(in)
	if err != nil {
		return ApplyResult{Outcome: OutcomeExecutionError, Message: err.Error()}, err
	}

	result, err := s.runner.Run(ctx, shellRequest(in.Apply, in))
	if err != nil || result.ExitCode != 0 {
		message := fmt.Sprintf("shell apply failed (on_failure=%s)", policy)

		if err == nil {
			err = fmt.Errorf("shell executor %q apply exited with code %d", in.ExecutorName, result.ExitCode)
		}

		return ApplyResult{
			Outcome:  OutcomeExecutionError,
			Message:  message,
			ExitCode: result.ExitCode,
			Stdout:   result.Stdout,
			Stderr:   result.Stderr,
		}, err
	}

	return ApplyResult{
		Outcome:  OutcomeApplied,
		Message:  "shell apply succeeded",
		ExitCode: result.ExitCode,
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
	}, nil
}

func shellRequest(expr string, in Input) Request {
	return Request{
		Command: "sh",
		Args:    []string{"-c", expr},
		Dir:     in.WorkingDir,
		RunAs:   in.RunAs,
		Env:     in.Env,
	}
}

func validateShellOnFailure(in Input) (string, error) {
	if in.Attributes == nil {
		return "abort", nil
	}
	if v := strings.TrimSpace(in.Attributes["on_failure"]); v != "" {
		switch v {
		case "abort", "continue":
			return v, nil
		default:
			return "", fmt.Errorf("shell executor %q: invalid on_failure value %q (allowed: abort, continue)", in.ExecutorName, v)
		}
	}
	return "abort", nil
}
