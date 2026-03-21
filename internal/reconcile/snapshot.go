package reconcile

import (
	"sync/atomic"
	"time"

	flukeproto "github.com/taiidani/fluke/internal/proto/flukeproto"
)

// eventHistorySize is the maximum number of completed executions retained in
// the in-memory sliding window. Oldest entries are evicted when the cap is hit.
const eventHistorySize = 500

// AgentView is a point-in-time view of a known host.
type AgentView struct {
	AgentID   string // current session ID; empty if disconnected
	Hostname  string
	Labels    map[string]string
	Connected bool
	LastSeen  time.Time
}

// TaskView is a point-in-time view of a single (hostname, task) record.
type TaskView struct {
	Hostname       string
	TaskName       string
	Status         TaskStatus
	LastCheckedAt  time.Time
	DriftedSteps   []string // non-empty when Status == Drifted
	ExecutionID    string
	LastOutcome    flukeproto.TaskOutcome
	LastExecutedAt time.Time
}

// DriftView describes an active drift condition (Status == Drifted).
type DriftView struct {
	Hostname     string
	TaskName     string
	DriftedSteps []string
	DetectedAt   time.Time
	AlertFired   bool
}

// EventView is a summary of a completed task execution.
type EventView struct {
	ExecutionID string
	Hostname    string
	TaskName    string
	Outcome     flukeproto.TaskOutcome
	StartedAt   time.Time
	EndedAt     time.Time
}

type OperationPhase string

const (
	OperationPhaseCheck OperationPhase = "check"
	OperationPhaseApply OperationPhase = "apply"
)

type OperationOutcome string

const (
	OperationOutcomeSatisfied      OperationOutcome = "satisfied"
	OperationOutcomeDrift          OperationOutcome = "drift"
	OperationOutcomeApplied        OperationOutcome = "applied"
	OperationOutcomeExecutionError OperationOutcome = "execution_error"
)

// OperationEventView captures per-executor check/apply outcomes.
type OperationEventView struct {
	AgentID      string
	AgentName    string
	TaskName     string
	ExecutorType string
	ExecutorName string
	Phase        OperationPhase
	Outcome      OperationOutcome
	ErrorMessage string
	Timestamp    time.Time
}

// Snapshot is a point-in-time read-only view of all reconciler state. It is
// rebuilt after each state-changing event and published atomically so
// management handlers can read it without blocking the reconciler event loop.
type Snapshot struct {
	Agents []AgentView
	Tasks  []TaskView
	Drifts []DriftView
	// Events holds recent completed executions, newest first, capped at
	// eventHistorySize. This is the in-memory event history for ListEvents.
	Events []EventView
	// OperationEvents holds recent per-executor check/apply outcomes.
	OperationEvents []OperationEventView
}

// Snapshot returns the most recently published state snapshot. Safe to call
// from any goroutine; reads never block the reconciler.
func (r *Reconciler) Snapshot() *Snapshot {
	if s := r.snapshot.Load(); s != nil {
		return s
	}
	return &Snapshot{}
}

// rebuildSnapshot constructs a fresh Snapshot from current reconciler state
// and publishes it atomically. Called at the end of each state-changing event
// handler.
func (r *Reconciler) rebuildSnapshot() {
	s := &Snapshot{}

	// Agents — one entry per known hostname.
	for hostname, labels := range r.hosts {
		view := AgentView{
			Hostname: hostname,
			Labels:   labels,
		}
		for _, session := range r.sessions {
			if session.hostname == hostname {
				view.AgentID = session.agentID
				view.Connected = true
				view.LastSeen = session.lastSeen
				break
			}
		}
		s.Agents = append(s.Agents, view)
	}

	// Tasks and active drift entries.
	for key, rec := range r.records {
		task := TaskView{
			Hostname:       key.hostname,
			TaskName:       key.taskName,
			Status:         rec.Status,
			LastCheckedAt:  rec.LastCheckedAt,
			ExecutionID:    rec.ExecutionID,
			LastOutcome:    rec.LastOutcome,
			LastExecutedAt: rec.ExecutionEnded,
		}
		for _, cr := range rec.StepCheckResults {
			if !cr.Satisfied {
				task.DriftedSteps = append(task.DriftedSteps, cr.StepName)
			}
		}
		s.Tasks = append(s.Tasks, task)

		if rec.Status == TaskStatusDrifted {
			s.Drifts = append(s.Drifts, DriftView{
				Hostname:     key.hostname,
				TaskName:     key.taskName,
				DriftedSteps: task.DriftedSteps,
				DetectedAt:   rec.DriftDetectedAt,
				AlertFired:   rec.AlertFired,
			})
		}
	}

	// Preserve the existing event history (it is append-only via recordEvent).
	if prev := r.snapshot.Load(); prev != nil {
		s.Events = prev.Events
		s.OperationEvents = prev.OperationEvents
	}

	r.snapshot.Store(s)
}

// recordEvent appends a completed execution to the event history in the
// current snapshot. Called from handleTaskComplete.
func (r *Reconciler) recordEvent(ev EventView) {
	prev := r.snapshot.Load()
	var existing []EventView
	if prev != nil {
		existing = prev.Events
	}

	// Prepend newest first; evict oldest if over cap.
	updated := make([]EventView, 0, min(len(existing)+1, eventHistorySize))
	updated = append(updated, ev)
	for _, e := range existing {
		if len(updated) >= eventHistorySize {
			break
		}
		updated = append(updated, e)
	}

	next := *r.snapshot.Load()
	next.Events = updated
	r.snapshot.Store(&next)
}

func (r *Reconciler) recordOperationEvent(ev OperationEventView) {
	prev := r.snapshot.Load()
	var existing []OperationEventView
	if prev != nil {
		existing = prev.OperationEvents
	}

	updated := make([]OperationEventView, 0, min(len(existing)+1, eventHistorySize))
	updated = append(updated, ev)
	for _, e := range existing {
		if len(updated) >= eventHistorySize {
			break
		}
		updated = append(updated, e)
	}

	next := *r.snapshot.Load()
	next.OperationEvents = updated
	r.snapshot.Store(&next)
}

// snapshot field on Reconciler — declared here to keep it alongside the logic.
// (The field itself is defined in reconciler.go.)
var _ = (*atomic.Pointer[Snapshot])(nil) // compile-time check
