package executor

import (
	"context"
	"fmt"
	"strings"
)

type MiseExecutor struct {
	runner Runner
}

func NewMiseExecutor(runner Runner) *MiseExecutor {
	return &MiseExecutor{runner: runner}
}

func (m *MiseExecutor) Name() string {
	return "mise"
}

func (m *MiseExecutor) Check(ctx context.Context, in Input) (CheckResult, error) {
	workingDir, err := requireWorkingDir(in)
	if err != nil {
		return CheckResult{Outcome: OutcomeExecutionError, Message: err.Error()}, err
	}

	checkTask := taskAttrOrDefault(in, "check_task", "check")

	if in.Git != nil {
		if err := validateMiseGitConfig(in); err != nil {
			return CheckResult{Outcome: OutcomeExecutionError, Message: err.Error()}, err
		}

		prepResult, prepErr := m.gitPrep(ctx, in, workingDir)
		if prepErr != nil {
			wrapped := fmt.Errorf("mise executor %q git prep failed: %w", in.ExecutorName, prepErr)
			return CheckResult{
				Outcome:  OutcomeExecutionError,
				Message:  wrapped.Error(),
				ExitCode: prepResult.ExitCode,
				Stdout:   prepResult.Stdout,
				Stderr:   prepResult.Stderr,
			}, wrapped
		}
	}

	result, err := m.runner.Run(ctx, miseTaskRequest(in, workingDir, checkTask))
	if err != nil {
		wrapped := fmt.Errorf("mise executor %q check task %q failed: %w", in.ExecutorName, checkTask, err)
		return CheckResult{
			Outcome:  OutcomeExecutionError,
			Message:  wrapped.Error(),
			ExitCode: result.ExitCode,
			Stdout:   result.Stdout,
			Stderr:   result.Stderr,
		}, wrapped
	}

	if result.ExitCode == 0 {
		return CheckResult{
			Outcome:  OutcomeSatisfied,
			Message:  "mise check satisfied",
			ExitCode: result.ExitCode,
			Stdout:   result.Stdout,
			Stderr:   result.Stderr,
		}, nil
	}

	return CheckResult{
		Outcome:  OutcomeDrifted,
		Message:  "mise check detected drift",
		ExitCode: result.ExitCode,
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
	}, nil
}

func (m *MiseExecutor) Apply(ctx context.Context, in Input) (ApplyResult, error) {
	workingDir, err := requireWorkingDir(in)
	if err != nil {
		return ApplyResult{Outcome: OutcomeExecutionError, Message: err.Error()}, err
	}

	applyTask := taskAttrOrDefault(in, "apply_task", "apply")

	result, err := m.runner.Run(ctx, miseTaskRequest(in, workingDir, applyTask))
	if err != nil {
		wrapped := fmt.Errorf("mise executor %q apply task %q failed: %w", in.ExecutorName, applyTask, err)
		return ApplyResult{
			Outcome:  OutcomeExecutionError,
			Message:  wrapped.Error(),
			ExitCode: result.ExitCode,
			Stdout:   result.Stdout,
			Stderr:   result.Stderr,
		}, wrapped
	}
	if result.ExitCode != 0 {
		err = fmt.Errorf("mise executor %q apply task %q failed: exited with code %d", in.ExecutorName, applyTask, result.ExitCode)
		return ApplyResult{
			Outcome:  OutcomeExecutionError,
			Message:  err.Error(),
			ExitCode: result.ExitCode,
			Stdout:   result.Stdout,
			Stderr:   result.Stderr,
		}, err
	}

	return ApplyResult{
		Outcome:  OutcomeApplied,
		Message:  "mise apply succeeded",
		ExitCode: result.ExitCode,
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
	}, nil
}

func requireWorkingDir(in Input) (string, error) {
	workingDir := strings.TrimSpace(in.WorkingDir)
	if workingDir == "" {
		return "", fmt.Errorf("mise executor %q: working_dir is required", in.ExecutorName)
	}
	return workingDir, nil
}

func taskAttrOrDefault(in Input, key, fallback string) string {
	if in.Attributes == nil {
		return fallback
	}
	value := strings.TrimSpace(in.Attributes[key])
	if value == "" {
		return fallback
	}
	return value
}

func validateMiseGitConfig(in Input) error {
	if in.Git == nil {
		return nil
	}
	if strings.TrimSpace(in.Git.URL) == "" {
		return fmt.Errorf("mise executor %q: git.url is required when git is configured", in.ExecutorName)
	}
	return nil
}

func miseTaskRequest(in Input, workingDir, task string) Request {
	return Request{
		Command: "mise",
		Args:    []string{"run", task},
		Dir:     workingDir,
		RunAs:   in.RunAs,
		Env:     in.Env,
	}
}

func (m *MiseExecutor) gitPrep(ctx context.Context, in Input, workingDir string) (Result, error) {
	url := strings.TrimSpace(in.Git.URL)

	branch := strings.TrimSpace(in.Git.Branch)
	if branch == "" {
		branch = "main"
	}

	repoCheck, err := m.runner.Run(ctx, Request{
		Command: "git",
		Args:    []string{"-C", workingDir, "rev-parse", "--is-inside-work-tree"},
		Dir:     workingDir,
		RunAs:   in.RunAs,
		Env:     in.Env,
	})
	if err != nil {
		return repoCheck, err
	}

	if repoCheck.ExitCode == 0 {
		pullResult, pullErr := m.runner.Run(ctx, Request{
			Command: "git",
			Args:    []string{"-C", workingDir, "pull", "--ff-only", "origin", branch},
			Dir:     workingDir,
			RunAs:   in.RunAs,
			Env:     in.Env,
		})
		if pullErr != nil {
			return pullResult, pullErr
		}
		if pullResult.ExitCode != 0 {
			return pullResult, fmt.Errorf("git pull exited with code %d", pullResult.ExitCode)
		}
		return pullResult, nil
	}

	cloneResult, cloneErr := m.runner.Run(ctx, Request{
		Command: "git",
		Args:    []string{"clone", "--branch", branch, "--single-branch", url, workingDir},
		RunAs:   in.RunAs,
		Env:     in.Env,
	})
	if cloneErr != nil {
		return cloneResult, cloneErr
	}
	if cloneResult.ExitCode != 0 {
		return cloneResult, fmt.Errorf("git clone exited with code %d", cloneResult.ExitCode)
	}
	return cloneResult, nil
}
