package reconcile

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	flukeproto "github.com/taiidani/fluke/internal/proto/flukeproto"
)

// agentSession holds runtime state for a connected agent. It is created on
// agentConnectedEvent and removed on agentDisconnectedEvent.
type agentSession struct {
	agentID  string
	hostname string
	labels   map[string]string
	// send delivers a ServerMessage to the agent. Must not block; the gRPC
	// handler provides a non-blocking implementation.
	send     func(*flukeproto.ServerMessage) error
	lastSeen time.Time
}

// recordKey uniquely identifies a TaskRecord within the reconciler.
type recordKey struct {
	hostname string
	taskName string
}

type executionRuntime struct {
	hostname    string
	taskName    string
	agentID     string
	stepOrder   []string
	stepResults map[string]*flukeproto.StepResult
}

// Reconciler is the single-threaded event loop that owns all reconciliation
// state. Every mutation flows through the events channel, so no locking is
// needed on any field.
//
// External goroutines (gRPC handlers, Git poller, management API) push events
// via Send; the reconciler processes them serially in Run.
type Reconciler struct {
	defaultCheckInterval time.Duration
	defaultDriftPolicy   DriftPolicy
	webhookURL           string // empty = no webhook configured

	// Desired state — atomically replaced on each manifestUpdatedEvent.
	tasks []Task

	// agentID → session (connected agents only).
	sessions map[string]*agentSession

	// hostname → labels (all known hosts; survives agent reconnects).
	hosts map[string]map[string]string

	// (hostname, taskName) → TaskRecord.
	records map[recordKey]*TaskRecord

	// (hostname, taskName) → scheduled timer (cancelled on state change).
	timers map[recordKey]*time.Timer

	// executionID → in-flight runtime metadata.
	executions map[string]*executionRuntime

	pubsub   *executionPubSub
	events   chan event
	log      *slog.Logger
	snapshot atomic.Pointer[Snapshot]
}

// New creates a Reconciler. Call Run to start the event loop.
func New(
	checkInterval time.Duration,
	driftPolicy DriftPolicy,
	webhookURL string,
	log *slog.Logger,
) *Reconciler {
	if checkInterval <= 0 {
		checkInterval = defaultCheckInterval
	}
	return &Reconciler{
		defaultCheckInterval: checkInterval,
		defaultDriftPolicy:   driftPolicy,
		webhookURL:           webhookURL,
		sessions:             make(map[string]*agentSession),
		hosts:                make(map[string]map[string]string),
		records:              make(map[recordKey]*TaskRecord),
		timers:               make(map[recordKey]*time.Timer),
		executions:           make(map[string]*executionRuntime),
		pubsub:               newExecutionPubSub(),
		events:               make(chan event, 256),
		log:                  log,
	}
}

// Run starts the reconciler's event loop. It blocks until ctx is cancelled.
func (r *Reconciler) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev := <-r.events:
			r.handle(ev)
		}
	}
}

// Send enqueues an event for processing. Safe to call from any goroutine.
// Returns an error if the event buffer is full (backpressure signal).
func (r *Reconciler) Send(ev event) error {
	select {
	case r.events <- ev:
		return nil
	default:
		return fmt.Errorf("reconciler event buffer full, dropping %T", ev)
	}
}

// PubSub returns the execution pub/sub so the management gRPC handler can
// subscribe to live execution events for StreamExecution.
func (r *Reconciler) PubSub() *executionPubSub {
	return r.pubsub
}

// ── Event dispatcher ──────────────────────────────────────────────────────────

func (r *Reconciler) handle(ev event) {
	switch e := ev.(type) {
	case agentConnectedEvent:
		r.handleAgentConnected(e)
	case agentDisconnectedEvent:
		r.handleAgentDisconnected(e)
	case stateReportReceivedEvent:
		r.handleStateReportReceived(e)
	case stepLogLineReceivedEvent:
		r.pubsub.publishLogLine(e.line)
	case stepResultReceivedEvent:
		r.handleStepResult(e)
	case taskCompleteReceivedEvent:
		r.handleTaskComplete(e)
	case manifestUpdatedEvent:
		r.handleManifestUpdated(e)
	case timerFiredEvent:
		r.handleTimerFired(e)
	case triggerReconcileEvent:
		r.handleTriggerReconcile(e)
	case cancelExecutionEvent:
		r.handleCancelExecution(e)
	case heartbeatEvent:
		if session, ok := r.sessions[e.agentID]; ok {
			session.lastSeen = time.Now()
		}
	}
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func (r *Reconciler) handleAgentConnected(e agentConnectedEvent) {
	r.log.Info("agent connected",
		"event", "agent_connected",
		"agent_id", e.agentID,
		"hostname", e.hostname,
		"labels", e.labels,
	)

	session := &agentSession{
		agentID:  e.agentID,
		hostname: e.hostname,
		labels:   e.labels,
		send:     e.send,
		lastSeen: time.Now(),
	}
	r.sessions[e.agentID] = session
	r.hosts[e.hostname] = e.labels

	// Reset all task records for this hostname — the agent is a fresh connection.
	for key, rec := range r.records {
		if key.hostname == e.hostname {
			rec.OnAgentConnected()
			r.cancelTimer(key)
		}
	}

	r.issueStateReports(session)
	r.rebuildSnapshot()
}

func (r *Reconciler) handleAgentDisconnected(e agentDisconnectedEvent) {
	session, ok := r.sessions[e.agentID]
	if !ok {
		return
	}

	r.log.Info("agent disconnected",
		"event", "agent_disconnected",
		"agent_id", e.agentID,
		"hostname", session.hostname,
	)

	now := time.Now()
	for key, rec := range r.records {
		if key.hostname != session.hostname {
			continue
		}
		if rec.Status == TaskStatusExecuting {
			_ = rec.OnAgentDisconnectedDuringExecution(now)
			r.log.Warn("execution interrupted by agent disconnect",
				"event", "execution_completed",
				"hostname", session.hostname,
				"task", key.taskName,
				"execution_id", rec.ExecutionID,
				"outcome", "failed",
			)
			r.pubsub.publishTaskComplete(&flukeproto.TaskComplete{
				ExecutionId: rec.ExecutionID,
				Outcome:     flukeproto.TaskOutcome_TASK_OUTCOME_FAILED,
			})
			r.pubsub.closeExecution(rec.ExecutionID)
			delete(r.executions, rec.ExecutionID)
		}
		r.scheduleTimer(key, rec)
	}

	delete(r.sessions, e.agentID)
	r.rebuildSnapshot()
}

func (r *Reconciler) handleStateReportReceived(e stateReportReceivedEvent) {
	session, ok := r.sessions[e.agentID]
	if !ok {
		return
	}

	r.log.Debug("state report received",
		"event", "state_report_received",
		"hostname", session.hostname,
		"tasks_reported", len(e.taskStates),
	)

	now := time.Now()
	for _, ts := range e.taskStates {
		key := recordKey{hostname: session.hostname, taskName: ts.TaskName}
		rec, ok := r.records[key]
		if !ok {
			continue
		}

		results := make([]StepCheckResult, len(ts.StepResults))
		hasCheckExecErr := false
		pendingOpEvents := make([]OperationEventView, 0, len(ts.StepResults))
		for i, sr := range ts.StepResults {
			checkOutcome := sr.CheckOutcome
			if checkOutcome == flukeproto.CheckOutcome_CHECK_OUTCOME_UNSPECIFIED {
				if sr.Satisfied {
					checkOutcome = flukeproto.CheckOutcome_CHECK_OUTCOME_SATISFIED
				} else {
					checkOutcome = flukeproto.CheckOutcome_CHECK_OUTCOME_DRIFT
				}
			}

			satisfied := checkOutcome == flukeproto.CheckOutcome_CHECK_OUTCOME_SATISFIED
			results[i] = StepCheckResult{
				StepName:  sr.StepName,
				Satisfied: satisfied,
				Stderr:    sr.Stderr,
			}

			phaseOutcome := OperationOutcomeDrift
			errorMessage := ""
			switch checkOutcome {
			case flukeproto.CheckOutcome_CHECK_OUTCOME_SATISFIED:
				phaseOutcome = OperationOutcomeSatisfied
			case flukeproto.CheckOutcome_CHECK_OUTCOME_EXECUTION_ERROR:
				phaseOutcome = OperationOutcomeExecutionError
				errorMessage = sr.Stderr
				hasCheckExecErr = true
			}

			executorType, executorName := parseExecutorRef(sr.StepName)
			pendingOpEvents = append(pendingOpEvents, OperationEventView{
				AgentID:      session.agentID,
				AgentName:    session.hostname,
				TaskName:     ts.TaskName,
				ExecutorType: executorType,
				ExecutorName: executorName,
				Phase:        OperationPhaseCheck,
				Outcome:      phaseOutcome,
				ErrorMessage: errorMessage,
				Timestamp:    now,
			})
		}

		if err := rec.OnStateReportReceived(e.reportID, results, now); err != nil {
			r.log.Warn("ignoring unexpected state report",
				"error", err,
				"hostname", session.hostname,
				"task", ts.TaskName,
			)
			continue
		}

		for _, opEvent := range pendingOpEvents {
			r.recordOperationEvent(opEvent)
		}

		r.cancelTimer(key)

		if hasCheckExecErr {
			if err := rec.OnCheckExecutionError(now); err != nil {
				r.log.Warn("could not transition check execution error",
					"error", err,
					"hostname", session.hostname,
					"task", ts.TaskName,
				)
				continue
			}
			r.scheduleTimer(key, rec)
			continue
		}

		switch rec.Status {
		case TaskStatusSatisfied:
			r.log.Info("task satisfied",
				"event", "state_report_received",
				"hostname", session.hostname,
				"task", ts.TaskName,
				"outcome", "satisfied",
			)
			r.scheduleTimer(key, rec)

		case TaskStatusDrifted:
			r.handleDrift(session, rec, key)
		}
	}
	r.rebuildSnapshot()
}

func (r *Reconciler) handleStepResult(e stepResultReceivedEvent) {
	r.pubsub.publishStepResult(e.result)
	runtime, ok := r.executions[e.result.ExecutionId]
	if !ok {
		return
	}
	runtime.stepResults[e.result.StepName] = e.result
}

func (r *Reconciler) handleDrift(session *agentSession, rec *TaskRecord, key recordKey) {
	task := r.findTask(key.taskName)
	if task == nil {
		return
	}

	driftedSteps := make([]string, 0, len(rec.StepCheckResults))
	for _, cr := range rec.StepCheckResults {
		if !cr.Satisfied {
			driftedSteps = append(driftedSteps, cr.StepName)
		}
	}

	r.log.Warn("drift detected",
		"event", "drift_detected",
		"hostname", session.hostname,
		"task", key.taskName,
		"drifted_steps", driftedSteps,
		"policy", task.DriftPolicy,
	)

	if task.DriftPolicy != DriftPolicyRemediate && !rec.AlertFired {
		_ = rec.OnAlertFired()
		r.fireWebhook(session, rec, key.taskName, driftedSteps, task.DriftPolicy)
	}

	if task.DriftPolicy == DriftPolicyAlertOnly {
		// Webhook fired; wait for a manual trigger before executing.
		return
	}

	r.dispatchExecution(session, rec, key, task)
}

func (r *Reconciler) handleTaskComplete(e taskCompleteReceivedEvent) {
	session, ok := r.sessions[e.agentID]
	if !ok {
		return
	}

	execID := e.complete.ExecutionId
	runtime, ok := r.executions[execID]
	if !ok || runtime.hostname != session.hostname {
		return
	}

	key := recordKey{hostname: runtime.hostname, taskName: runtime.taskName}
	rec := r.records[key]
	if rec == nil {
		return
	}

	now := time.Now()
	outcome := e.complete.Outcome
	if err := rec.OnExecutionCompleted(execID, outcome, now); err != nil {
		r.log.Warn("invalid task completion transition",
			"error", err,
			"hostname", session.hostname,
			"task", key.taskName,
			"execution_id", execID,
			"outcome", outcome.String(),
		)
		return
	}
	delete(r.executions, execID)

	r.log.Info("execution completed",
		"event", "execution_completed",
		"hostname", session.hostname,
		"task", key.taskName,
		"execution_id", execID,
		"outcome", outcome.String(),
		"duration_ms", now.Sub(rec.ExecutionStarted).Milliseconds(),
	)

	r.recordApplyOperationEvents(runtime, now)

	r.pubsub.publishTaskComplete(e.complete)
	r.pubsub.closeExecution(execID)

	r.recordEvent(EventView{
		ExecutionID: execID,
		Hostname:    session.hostname,
		TaskName:    key.taskName,
		Outcome:     outcome,
		StartedAt:   rec.ExecutionStarted,
		EndedAt:     rec.ExecutionEnded,
	})

	if outcome == flukeproto.TaskOutcome_TASK_OUTCOME_SUCCESS {
		if task := r.findTask(key.taskName); task != nil {
			if task.DriftPolicy == DriftPolicyRemediateAndAlert {
				r.fireRemediationWebhook(session, key.taskName)
			}
		}
	}

	r.scheduleTimer(key, rec)
	r.rebuildSnapshot()
}

func (r *Reconciler) handleManifestUpdated(e manifestUpdatedEvent) {
	r.log.Info("manifest updated", "task_count", len(e.tasks))
	r.tasks = e.tasks
	for _, session := range r.sessions {
		r.reconcileAgentTasks(session)
	}
	r.rebuildSnapshot()
}

func (r *Reconciler) handleTimerFired(e timerFiredEvent) {
	key := recordKey{hostname: e.hostname, taskName: e.taskName}
	rec, ok := r.records[key]
	if !ok {
		return
	}

	session := r.sessionForHostname(e.hostname)
	if session == nil {
		// Agent is disconnected; timer fires are no-ops until it reconnects.
		return
	}

	task := r.findTask(e.taskName)
	if task == nil {
		return
	}

	reportID := newID()
	if err := rec.OnStateReportRequested(reportID, time.Now()); err != nil {
		r.log.Warn("could not issue state report on timer",
			"error", err,
			"hostname", e.hostname,
			"task", e.taskName,
		)
		return
	}

	_ = session.send(&flukeproto.ServerMessage{
		Payload: &flukeproto.ServerMessage_RequestStateReport{
			RequestStateReport: &flukeproto.RequestStateReport{
				ReportId: reportID,
				Tasks:    []*flukeproto.TaskSpec{task.Spec},
			},
		},
	})
}

func (r *Reconciler) handleTriggerReconcile(e triggerReconcileEvent) {
	// Close replyCh when done so the management handler's stream loop exits.
	defer close(e.replyCh)

	var targets []*agentSession
	var taskFilter string // empty = all matching tasks

	switch t := e.target.Target.(type) {
	case *flukeproto.TriggerReconcileRequest_All:
		for _, s := range r.sessions {
			targets = append(targets, s)
		}
	case *flukeproto.TriggerReconcileRequest_AgentId:
		if s := r.sessions[t.AgentId]; s != nil {
			targets = append(targets, s)
		}
	case *flukeproto.TriggerReconcileRequest_TaskName:
		taskFilter = t.TaskName
		for _, s := range r.sessions {
			targets = append(targets, s)
		}
	}

	e.replyCh <- &flukeproto.ReconciliationEvent{
		Event: &flukeproto.ReconciliationEvent_Started{
			Started: &flukeproto.ReconcileStarted{
				AgentsTargeted: int32(len(targets)),
				StartedUnix:    time.Now().Unix(),
			},
		},
	}

	now := time.Now()
	for _, session := range targets {
		for key, rec := range r.records {
			if key.hostname != session.hostname {
				continue
			}
			if taskFilter != "" && key.taskName != taskFilter {
				continue
			}
			if rec.Status == TaskStatusExecuting {
				continue
			}
			task := r.findTask(key.taskName)
			if task == nil {
				continue
			}

			// Reset retry backoff — manual trigger means immediate re-check.
			rec.RetryCount = 0
			rec.NextRetryAt = time.Time{}
			r.cancelTimer(key)

			reportID := newID()
			if err := rec.OnStateReportRequested(reportID, now); err != nil {
				continue
			}

			_ = session.send(&flukeproto.ServerMessage{
				Payload: &flukeproto.ServerMessage_RequestStateReport{
					RequestStateReport: &flukeproto.RequestStateReport{
						ReportId: reportID,
						Tasks:    []*flukeproto.TaskSpec{task.Spec},
					},
				},
			})

			e.replyCh <- &flukeproto.ReconciliationEvent{
				Event: &flukeproto.ReconciliationEvent_AgentStarted{
					AgentStarted: &flukeproto.AgentReconcileStarted{
						AgentId:  session.agentID,
						Hostname: session.hostname,
						TaskName: key.taskName,
					},
				},
			}
		}
	}

	r.log.Info("reconciliation triggered",
		"event", "reconcile_triggered",
		"agents_targeted", len(targets),
		"task_filter", taskFilter,
	)

	// TODO: stream AgentReconcileResult and ReconcileComplete events back
	// through replyCh as state reports resolve. Currently the stream closes
	// immediately after dispatching; the caller sees the started events only.
	// Completing this requires correlating incoming state reports back to the
	// trigger that initiated them.
}

func (r *Reconciler) handleCancelExecution(e cancelExecutionEvent) {
	runtime, ok := r.executions[e.executionID]
	if !ok {
		return
	}
	session := r.sessionForHostname(runtime.hostname)
	if session == nil {
		return
	}
	_ = session.send(&flukeproto.ServerMessage{
		Payload: &flukeproto.ServerMessage_CancelTask{
			CancelTask: &flukeproto.CancelTask{ExecutionId: e.executionID},
		},
	})
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// issueStateReports sends a single RequestStateReport covering all tasks that
// match the given session's labels. Called on agent connect and after a
// manifest update adds new tasks.
func (r *Reconciler) issueStateReports(session *agentSession) {
	var specs []*flukeproto.TaskSpec
	reportID := newID()
	now := time.Now()

	for i := range r.tasks {
		task := &r.tasks[i]
		if !task.Matches(session.labels) {
			continue
		}
		key := recordKey{hostname: session.hostname, taskName: task.Spec.Name}
		rec, ok := r.records[key]
		if !ok {
			interval := time.Duration(task.Spec.CheckIntervalSeconds) * time.Second
			rec = NewTaskRecord(session.hostname, task.Spec.Name, interval)
			r.records[key] = rec
		}
		if err := rec.OnStateReportRequested(reportID, now); err != nil {
			continue
		}
		specs = append(specs, task.Spec)
	}

	if len(specs) == 0 {
		return
	}

	_ = session.send(&flukeproto.ServerMessage{
		Payload: &flukeproto.ServerMessage_RequestStateReport{
			RequestStateReport: &flukeproto.RequestStateReport{
				ReportId: reportID,
				Tasks:    specs,
			},
		},
	})
}

// reconcileAgentTasks is called after a manifest update. It removes records
// for tasks that no longer match the agent, and issues state reports for new
// or changed tasks.
func (r *Reconciler) reconcileAgentTasks(session *agentSession) {
	desired := make(map[string]*Task, len(r.tasks))
	for i := range r.tasks {
		t := &r.tasks[i]
		if t.Matches(session.labels) {
			desired[t.Spec.Name] = t
		}
	}

	// Remove records for tasks no longer in the desired set.
	for key, rec := range r.records {
		if key.hostname != session.hostname {
			continue
		}
		if _, ok := desired[key.taskName]; ok {
			continue
		}
		if rec.Status == TaskStatusExecuting {
			_ = session.send(&flukeproto.ServerMessage{
				Payload: &flukeproto.ServerMessage_CancelTask{
					CancelTask: &flukeproto.CancelTask{ExecutionId: rec.ExecutionID},
				},
			})
			delete(r.executions, rec.ExecutionID)
		}
		r.cancelTimer(key)
		delete(r.records, key)
	}

	// Issue state reports for tasks that are new or not currently executing.
	var specs []*flukeproto.TaskSpec
	reportID := newID()
	now := time.Now()

	for name, task := range desired {
		key := recordKey{hostname: session.hostname, taskName: name}
		rec, exists := r.records[key]
		if !exists {
			interval := time.Duration(task.Spec.CheckIntervalSeconds) * time.Second
			rec = NewTaskRecord(session.hostname, name, interval)
			r.records[key] = rec
		}
		if rec.Status == TaskStatusExecuting {
			continue
		}
		r.cancelTimer(key)
		if err := rec.OnStateReportRequested(reportID, now); err != nil {
			continue
		}
		specs = append(specs, task.Spec)
	}

	if len(specs) > 0 {
		_ = session.send(&flukeproto.ServerMessage{
			Payload: &flukeproto.ServerMessage_RequestStateReport{
				RequestStateReport: &flukeproto.RequestStateReport{
					ReportId: reportID,
					Tasks:    specs,
				},
			},
		})
	}
}

// dispatchExecution sends an ExecuteTask to the agent and transitions the
// record to Executing.
func (r *Reconciler) dispatchExecution(session *agentSession, rec *TaskRecord, key recordKey, task *Task) {
	execID := newID()
	if err := rec.OnExecutionDispatched(execID, time.Now()); err != nil {
		r.log.Warn("could not dispatch execution",
			"error", err,
			"hostname", session.hostname,
			"task", key.taskName,
		)
		return
	}

	runtime := &executionRuntime{
		hostname:    session.hostname,
		taskName:    key.taskName,
		agentID:     session.agentID,
		stepResults: make(map[string]*flukeproto.StepResult, len(task.Spec.Steps)),
	}
	for _, step := range task.Spec.Steps {
		runtime.stepOrder = append(runtime.stepOrder, step.Name)
	}
	r.executions[execID] = runtime
	r.pubsub.openExecution(execID)

	r.log.Info("execution started",
		"event", "execution_started",
		"hostname", session.hostname,
		"task", key.taskName,
		"execution_id", execID,
	)

	_ = session.send(&flukeproto.ServerMessage{
		Payload: &flukeproto.ServerMessage_ExecuteTask{
			ExecuteTask: &flukeproto.ExecuteTask{
				ExecutionId: execID,
				Task:        task.Spec,
			},
		},
	})
}

func (r *Reconciler) scheduleTimer(key recordKey, rec *TaskRecord) {
	r.cancelTimer(key)
	due := rec.NextCheckDue()
	if due.IsZero() {
		return
	}
	delay := time.Until(due)
	if delay < 0 {
		delay = 0
	}
	hostname, taskName := key.hostname, key.taskName
	r.timers[key] = time.AfterFunc(delay, func() {
		_ = r.Send(timerFiredEvent{hostname: hostname, taskName: taskName})
	})
}

func (r *Reconciler) cancelTimer(key recordKey) {
	if t, ok := r.timers[key]; ok {
		t.Stop()
		delete(r.timers, key)
	}
}

func (r *Reconciler) findTask(name string) *Task {
	for i := range r.tasks {
		if r.tasks[i].Spec.Name == name {
			return &r.tasks[i]
		}
	}
	return nil
}

func (r *Reconciler) sessionForHostname(hostname string) *agentSession {
	for _, s := range r.sessions {
		if s.hostname == hostname {
			return s
		}
	}
	return nil
}

func newID() string {
	return uuid.NewString()
}

func parseExecutorRef(stepName string) (string, string) {
	parts := strings.SplitN(stepName, ":", 2)
	if len(parts) != 2 {
		if strings.TrimSpace(stepName) == "" {
			return "unknown", "unknown"
		}
		return "unknown", stepName
	}
	executorType := strings.TrimSpace(parts[0])
	executorName := strings.TrimSpace(parts[1])
	if executorType == "" {
		executorType = "unknown"
	}
	if executorName == "" {
		executorName = "unknown"
	}
	return executorType, executorName
}

func (r *Reconciler) recordApplyOperationEvents(runtime *executionRuntime, now time.Time) {
	for _, stepName := range runtime.stepOrder {
		result, ok := runtime.stepResults[stepName]
		if !ok {
			continue
		}
		if result.Outcome == flukeproto.StepOutcome_STEP_OUTCOME_SKIPPED {
			continue
		}

		executorType, executorName := parseExecutorRef(stepName)
		outcome := OperationOutcomeApplied
		errorMessage := ""
		if result.Outcome == flukeproto.StepOutcome_STEP_OUTCOME_FAILED {
			outcome = OperationOutcomeExecutionError
			errorMessage = fmt.Sprintf("step failed with exit code %d", result.ExitCode)
		}

		r.recordOperationEvent(OperationEventView{
			AgentID:      runtime.agentID,
			AgentName:    runtime.hostname,
			TaskName:     runtime.taskName,
			ExecutorType: executorType,
			ExecutorName: executorName,
			Phase:        OperationPhaseApply,
			Outcome:      outcome,
			ErrorMessage: errorMessage,
			Timestamp:    now,
		})
	}
}

// ── Webhook stubs ─────────────────────────────────────────────────────────────
// Full implementations live in internal/drift.

func (r *Reconciler) fireWebhook(session *agentSession, rec *TaskRecord, taskName string, driftedSteps []string, policy DriftPolicy) {
	if r.webhookURL == "" {
		return
	}
	// TODO: delegate to internal/drift webhook sender.
}

func (r *Reconciler) fireRemediationWebhook(session *agentSession, taskName string) {
	if r.webhookURL == "" {
		return
	}
	// TODO: delegate to internal/drift webhook sender.
}
