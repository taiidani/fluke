package reconcile

import (
	"errors"
	"time"

	flukeproto "github.com/taiidani/fluke/internal/proto/flukeproto"
)

// defaultCheckInterval is the fallback when neither the task manifest nor the
// server config specifies a check_interval.
const defaultCheckInterval = 3 * time.Minute

// retryBase is the initial backoff duration for a failed task.
// Each retry doubles the delay up to the task's check_interval.
const retryBase = 15 * time.Second

// StepCheckResult mirrors the proto type but is kept as a plain Go struct so
// the reconcile package does not take a hard dependency on generated proto
// types in its core logic.
type StepCheckResult struct {
	StepName  string
	Satisfied bool
	Stderr    string
}

// TaskRecord holds the reconciliation state for a single (hostname, task_name)
// pair. All fields are managed by the reconciler and are not persisted — they
// are rebuilt from agent communication after a server restart.
type TaskRecord struct {
	Hostname string
	TaskName string
	Status   TaskStatus

	// CheckInterval is the resolved interval for this task (from the task
	// manifest, falling back to the server config default).
	CheckInterval time.Duration

	// Pending state report correlation (Status == Checking)
	ReportID         string
	CheckRequestedAt time.Time

	// Most recent state report results (Status >= Satisfied)
	LastCheckedAt    time.Time
	StepCheckResults []StepCheckResult

	// Most recent execution (Status >= Executing)
	ExecutionID      string
	ExecutionStarted time.Time
	ExecutionEnded   time.Time // zero while in progress
	LastOutcome      flukeproto.TaskOutcome

	// Retry backoff (Status == Failed)
	RetryCount  int
	NextRetryAt time.Time // zero = not scheduled

	// Drift metadata (Status == Drifted)
	DriftDetectedAt time.Time
	AlertFired      bool
}

// NewTaskRecord creates a TaskRecord in the Unknown state. checkInterval may
// be zero to use the default.
func NewTaskRecord(hostname, taskName string, checkInterval time.Duration) *TaskRecord {
	if checkInterval <= 0 {
		checkInterval = defaultCheckInterval
	}
	return &TaskRecord{
		Hostname:      hostname,
		TaskName:      taskName,
		Status:        TaskStatusUnknown,
		CheckInterval: checkInterval,
	}
}

// ── Transitions ──────────────────────────────────────────────────────────────

// OnAgentConnected resets the record when an agent (re)connects. The caller
// should immediately call OnStateReportRequested to issue a fresh check.
// Valid from any state.
func (r *TaskRecord) OnAgentConnected() {
	r.Status = TaskStatusUnknown
	r.ReportID = ""
	r.CheckRequestedAt = time.Time{}
	r.ExecutionID = ""
	r.ExecutionStarted = time.Time{}
	r.ExecutionEnded = time.Time{}
	r.RetryCount = 0
	r.NextRetryAt = time.Time{}
	r.DriftDetectedAt = time.Time{}
	r.AlertFired = false
}

// OnStateReportRequested records that a RequestStateReport has been sent.
// Valid from: Unknown, Satisfied, Drifted (re-check), Succeeded, Failed,
// Cancelled, and Checking (idempotent re-send).
func (r *TaskRecord) OnStateReportRequested(reportID string, now time.Time) error {
	if r.Status == TaskStatusExecuting {
		return errors.New("cannot request state report while task is executing")
	}
	r.Status = TaskStatusChecking
	r.ReportID = reportID
	r.CheckRequestedAt = now
	return nil
}

// OnStateReportReceived processes a StateReport from the agent. It transitions
// to Satisfied or Drifted based on whether all checks passed.
// Valid from: Checking.
func (r *TaskRecord) OnStateReportReceived(reportID string, results []StepCheckResult, now time.Time) error {
	if r.Status != TaskStatusChecking {
		return errors.New("received state report but task is not in checking state")
	}
	if r.ReportID != reportID {
		return errors.New("state report ID mismatch: possible stale report")
	}

	r.LastCheckedAt = now
	r.StepCheckResults = results
	r.ReportID = ""

	if allSatisfied(results) {
		r.Status = TaskStatusSatisfied
		// Clear drift metadata if it was previously drifted.
		r.DriftDetectedAt = time.Time{}
		r.AlertFired = false
	} else {
		r.Status = TaskStatusDrifted
		if r.DriftDetectedAt.IsZero() {
			r.DriftDetectedAt = now
		}
	}
	return nil
}

// OnExecutionDispatched records that an ExecuteTask has been sent to the agent.
// Valid from: Drifted.
func (r *TaskRecord) OnExecutionDispatched(executionID string, now time.Time) error {
	if r.Status != TaskStatusDrifted {
		return errors.New("can only dispatch execution from drifted state")
	}
	r.Status = TaskStatusExecuting
	r.ExecutionID = executionID
	r.ExecutionStarted = now
	r.ExecutionEnded = time.Time{}
	r.LastOutcome = flukeproto.TaskOutcome_TASK_OUTCOME_UNSPECIFIED
	return nil
}

// OnExecutionCompleted processes a TaskComplete message from the agent.
// Valid from: Executing.
func (r *TaskRecord) OnExecutionCompleted(executionID string, outcome flukeproto.TaskOutcome, now time.Time) error {
	if r.Status != TaskStatusExecuting {
		return errors.New("received execution result but task is not executing")
	}
	if r.ExecutionID != executionID {
		return errors.New("execution ID mismatch: possible stale result")
	}

	r.ExecutionEnded = now
	r.LastOutcome = outcome

	switch outcome {
	case flukeproto.TaskOutcome_TASK_OUTCOME_SUCCESS:
		r.Status = TaskStatusSucceeded
		r.RetryCount = 0
		r.NextRetryAt = time.Time{}
		r.DriftDetectedAt = time.Time{}
		r.AlertFired = false

	case flukeproto.TaskOutcome_TASK_OUTCOME_FAILED:
		r.Status = TaskStatusFailed
		r.NextRetryAt = r.nextRetryDelay(now)
		r.RetryCount++

	case flukeproto.TaskOutcome_TASK_OUTCOME_CANCELLED:
		r.Status = TaskStatusCancelled
		r.RetryCount = 0
		r.NextRetryAt = time.Time{}
	}
	return nil
}

// OnCheckExecutionError marks the current check cycle as failed due to an
// execution-time error (for example, command invocation failure). The task
// enters Failed and schedules retry backoff.
// Valid from: Checking, Drifted.
func (r *TaskRecord) OnCheckExecutionError(now time.Time) error {
	if r.Status != TaskStatusChecking && r.Status != TaskStatusDrifted {
		return errors.New("check execution error but task is not checking or drifted")
	}
	r.Status = TaskStatusFailed
	r.ReportID = ""
	r.LastCheckedAt = now
	r.ExecutionID = ""
	r.ExecutionStarted = time.Time{}
	r.ExecutionEnded = now
	r.LastOutcome = flukeproto.TaskOutcome_TASK_OUTCOME_FAILED
	r.NextRetryAt = r.nextRetryDelay(now)
	r.RetryCount++
	return nil
}

// OnAgentDisconnectedDuringExecution handles the case where the gRPC stream
// closes while a task is executing. The execution is recorded as failed and
// backoff is scheduled.
// Valid from: Executing.
func (r *TaskRecord) OnAgentDisconnectedDuringExecution(now time.Time) error {
	if r.Status != TaskStatusExecuting {
		return errors.New("agent disconnected but task is not executing")
	}
	r.Status = TaskStatusFailed
	r.ExecutionEnded = now
	r.LastOutcome = flukeproto.TaskOutcome_TASK_OUTCOME_FAILED
	r.NextRetryAt = r.nextRetryDelay(now)
	r.RetryCount++
	return nil
}

// OnAlertFired records that a drift webhook notification has been sent.
// Valid from: Drifted.
func (r *TaskRecord) OnAlertFired() error {
	if r.Status != TaskStatusDrifted {
		return errors.New("can only mark alert fired from drifted state")
	}
	r.AlertFired = true
	return nil
}

// ── Scheduling helpers ────────────────────────────────────────────────────────

// NextCheckDue returns the time at which this record should re-enter Checking.
// Returns the zero time if the record is not in a state that schedules a
// future check (e.g. Unknown, Checking, Executing, Drifted).
func (r *TaskRecord) NextCheckDue() time.Time {
	switch r.Status {
	case TaskStatusSatisfied, TaskStatusSucceeded, TaskStatusCancelled:
		return r.LastCheckedAt.Add(r.CheckInterval)
	case TaskStatusFailed:
		return r.NextRetryAt
	default:
		return time.Time{}
	}
}

// IsDue reports whether this record should be re-checked at or before now.
func (r *TaskRecord) IsDue(now time.Time) bool {
	due := r.NextCheckDue()
	return !due.IsZero() && !now.Before(due)
}

// nextRetryDelay computes the next retry time using exponential backoff,
// capped at CheckInterval.
//
//	attempt 0 →  15s
//	attempt 1 →  30s
//	attempt 2 →   1m
//	attempt 3 →   2m
//	attempt 4+ →  check_interval (capped)
func (r *TaskRecord) nextRetryDelay(now time.Time) time.Time {
	delay := retryBase
	for i := 0; i < r.RetryCount; i++ {
		delay *= 2
		if delay >= r.CheckInterval {
			delay = r.CheckInterval
			break
		}
	}
	return now.Add(delay)
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func allSatisfied(results []StepCheckResult) bool {
	for _, r := range results {
		if !r.Satisfied {
			return false
		}
	}
	return true
}
