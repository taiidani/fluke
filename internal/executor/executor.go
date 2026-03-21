package executor

import "context"

// Executor is the agent extension point for check/apply behavior.
type Executor interface {
	Name() string
	Check(context.Context, Input) (CheckResult, error)
	Apply(context.Context, Input) (ApplyResult, error)
}

// Outcome describes the high-level result of a check/apply operation.
type Outcome string

const (
	OutcomeSatisfied      Outcome = "satisfied"
	OutcomeDrifted        Outcome = "drifted"
	OutcomeApplied        Outcome = "applied"
	OutcomeExecutionError Outcome = "execution_error"
)

// Input is shared executor input populated by the reconcile compiler.
// Executors consume the fields they support.
type Input struct {
	ExecutorType string
	ExecutorName string

	Check      string
	Apply      string
	WorkingDir string
	RunAs      string
	Env        map[string]string

	Attributes map[string]string
	Git        *GitInput
}

// GitInput contains optional git synchronization settings for an executor.
type GitInput struct {
	URL    string
	Branch string
}

// CheckResult is returned from Executor.Check.
type CheckResult struct {
	Outcome  Outcome
	Message  string
	ExitCode int
	Stdout   string
	Stderr   string
}

// ApplyResult is returned from Executor.Apply.
type ApplyResult struct {
	Outcome  Outcome
	Message  string
	ExitCode int
	Stdout   string
	Stderr   string
}
