package executor

import (
	"context"
	"fmt"
	"strings"
)

type SystemdExecutor struct {
	runner Runner
}

func NewSystemdExecutor(runner Runner) *SystemdExecutor {
	return &SystemdExecutor{runner: runner}
}

func (s *SystemdExecutor) Name() string {
	return "systemd"
}

func (s *SystemdExecutor) Check(ctx context.Context, in Input) (CheckResult, error) {
	desired, err := parseSystemdDesired(in)
	if err != nil {
		return CheckResult{Outcome: OutcomeExecutionError, Message: err.Error()}, err
	}

	actual, err := s.readSystemdState(ctx, in, desired.unit)
	if err != nil {
		return CheckResult{Outcome: OutcomeExecutionError, Message: err.Error()}, err
	}

	if actual.active == desired.running && actual.enabled == desired.enabled {
		return CheckResult{Outcome: OutcomeSatisfied, Message: "systemd unit already in desired state"}, nil
	}
	return CheckResult{Outcome: OutcomeDrifted, Message: "systemd unit drift detected"}, nil
}

func (s *SystemdExecutor) Apply(ctx context.Context, in Input) (ApplyResult, error) {
	desired, err := parseSystemdDesired(in)
	if err != nil {
		return ApplyResult{Outcome: OutcomeExecutionError, Message: err.Error()}, err
	}

	actual, err := s.readSystemdState(ctx, in, desired.unit)
	if err != nil {
		return ApplyResult{Outcome: OutcomeExecutionError, Message: err.Error()}, err
	}

	if actual.active != desired.running {
		action := "stop"
		if desired.running {
			action = "start"
		}
		result, err := s.runner.Run(ctx, Request{Command: "systemctl", Args: []string{action, desired.unit}, RunAs: in.RunAs, Env: in.Env})
		if err != nil {
			wrapped := fmt.Errorf("systemd executor %q %s %s failed: %w", in.ExecutorName, action, desired.unit, err)
			return ApplyResult{Outcome: OutcomeExecutionError, Message: wrapped.Error(), ExitCode: result.ExitCode, Stdout: result.Stdout, Stderr: result.Stderr}, wrapped
		}
		if result.ExitCode != 0 {
			wrapped := fmt.Errorf("systemd executor %q %s %s failed: exited with code %d", in.ExecutorName, action, desired.unit, result.ExitCode)
			return ApplyResult{Outcome: OutcomeExecutionError, Message: wrapped.Error(), ExitCode: result.ExitCode, Stdout: result.Stdout, Stderr: result.Stderr}, wrapped
		}
	}

	if actual.enabled != desired.enabled {
		action := "disable"
		if desired.enabled {
			action = "enable"
		}
		result, err := s.runner.Run(ctx, Request{Command: "systemctl", Args: []string{action, desired.unit}, RunAs: in.RunAs, Env: in.Env})
		if err != nil {
			wrapped := fmt.Errorf("systemd executor %q %s %s failed: %w", in.ExecutorName, action, desired.unit, err)
			return ApplyResult{Outcome: OutcomeExecutionError, Message: wrapped.Error(), ExitCode: result.ExitCode, Stdout: result.Stdout, Stderr: result.Stderr}, wrapped
		}
		if result.ExitCode != 0 {
			wrapped := fmt.Errorf("systemd executor %q %s %s failed: exited with code %d", in.ExecutorName, action, desired.unit, result.ExitCode)
			return ApplyResult{Outcome: OutcomeExecutionError, Message: wrapped.Error(), ExitCode: result.ExitCode, Stdout: result.Stdout, Stderr: result.Stderr}, wrapped
		}
	}

	return ApplyResult{Outcome: OutcomeApplied, Message: "systemd apply completed"}, nil
}

type systemdDesired struct {
	unit    string
	running bool
	enabled bool
}

type systemdActual struct {
	active  bool
	enabled bool
}

func parseSystemdDesired(in Input) (systemdDesired, error) {
	if in.Attributes == nil {
		return systemdDesired{}, fmt.Errorf("systemd executor %q: attributes are required", in.ExecutorName)
	}

	unit := strings.TrimSpace(in.Attributes["unit"])
	if unit == "" {
		return systemdDesired{}, fmt.Errorf("systemd executor %q: unit is required", in.ExecutorName)
	}

	state := strings.TrimSpace(in.Attributes["state"])
	if state == "" {
		state = "running"
	}

	running := false
	switch state {
	case "running":
		running = true
	case "stopped":
		running = false
	default:
		return systemdDesired{}, fmt.Errorf("systemd executor %q: unsupported state %q", in.ExecutorName, state)
	}

	enabledRaw := strings.TrimSpace(in.Attributes["enabled"])
	enabled := true
	if enabledRaw != "" {
		switch enabledRaw {
		case "true":
			enabled = true
		case "false":
			enabled = false
		default:
			return systemdDesired{}, fmt.Errorf("systemd executor %q: enabled must be true or false, got %q", in.ExecutorName, enabledRaw)
		}
	}

	return systemdDesired{unit: unit, running: running, enabled: enabled}, nil
}

func (s *SystemdExecutor) readSystemdState(ctx context.Context, in Input, unit string) (systemdActual, error) {
	activeResult, err := s.runner.Run(ctx, Request{Command: "systemctl", Args: []string{"is-active", unit}, RunAs: in.RunAs, Env: in.Env})
	if err != nil {
		return systemdActual{}, fmt.Errorf("systemd executor %q check is-active %s failed: %w", in.ExecutorName, unit, err)
	}

	enabledResult, err := s.runner.Run(ctx, Request{Command: "systemctl", Args: []string{"is-enabled", unit}, RunAs: in.RunAs, Env: in.Env})
	if err != nil {
		return systemdActual{}, fmt.Errorf("systemd executor %q check is-enabled %s failed: %w", in.ExecutorName, unit, err)
	}

	return systemdActual{
		active:  parseSystemdActive(activeResult),
		enabled: parseSystemdEnabled(enabledResult),
	}, nil
}

func parseSystemdActive(result Result) bool {
	out := strings.TrimSpace(result.Stdout)
	if out != "" {
		return out == "active"
	}
	return result.ExitCode == 0
}

func parseSystemdEnabled(result Result) bool {
	out := strings.TrimSpace(result.Stdout)
	if out != "" {
		return out == "enabled"
	}
	return result.ExitCode == 0
}
