package reconcile

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	flukeproto "github.com/taiidani/fluke/internal/proto/flukeproto"
)

// ── Test helpers ──────────────────────────────────────────────────────────────

// newTestReconciler creates a Reconciler with a no-op logger for tests.
func newTestReconciler() *Reconciler {
	return New(30*time.Second, DriftPolicyRemediate, "", slog.New(slog.NewTextHandler(noopWriter{}, nil)))
}

// noopWriter discards all log output.
type noopWriter struct{}

func (noopWriter) Write(p []byte) (int, error) { return len(p), nil }

// runReconciler starts the reconciler in the background and returns a cancel
// function. Call cancel (or defer it) to stop the loop.
func runReconciler(r *Reconciler) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	go r.Run(ctx) //nolint:errcheck
	return cancel
}

// noopSend is a send function that discards all messages.
func noopSend(*flukeproto.ServerMessage) error { return nil }

// captureSend captures sent ServerMessages to a slice, safe for concurrent use.
type captureSend struct {
	mu   sync.Mutex
	msgs []*flukeproto.ServerMessage
}

func (c *captureSend) send(msg *flukeproto.ServerMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, msg)
	return nil
}

// findStateReport returns the first RequestStateReport's report_id, or "".
func (c *captureSend) findStateReport() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, msg := range c.msgs {
		if req := msg.GetRequestStateReport(); req != nil {
			return req.ReportId
		}
	}
	return ""
}

// findExecuteTask returns the first ExecuteTask's execution_id, or "".
func (c *captureSend) findExecuteTask() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, msg := range c.msgs {
		if et := msg.GetExecuteTask(); et != nil {
			return et.ExecutionId
		}
	}
	return ""
}

// waitFor polls cond() until it returns true or a 500ms timeout expires.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

// waitForTaskStatus polls until (hostname, taskName) reaches want status.
func waitForTaskStatus(t *testing.T, r *Reconciler, hostname, taskName string, want TaskStatus) {
	t.Helper()
	waitFor(t, func() bool {
		for _, task := range r.Snapshot().Tasks {
			if task.Hostname == hostname && task.TaskName == taskName {
				return task.Status == want
			}
		}
		return false
	})
}

// waitForAgentConnected polls until hostname appears as connected in the snapshot.
func waitForAgentConnected(t *testing.T, r *Reconciler, hostname string) {
	t.Helper()
	waitFor(t, func() bool {
		for _, ag := range r.Snapshot().Agents {
			if ag.Hostname == hostname && ag.Connected {
				return true
			}
		}
		return false
	})
}

// waitForStateReport polls until cap has a RequestStateReport message.
func waitForStateReport(t *testing.T, cap *captureSend) string {
	t.Helper()
	var reportID string
	waitFor(t, func() bool {
		reportID = cap.findStateReport()
		return reportID != ""
	})
	return reportID
}

// waitForExecuteTask polls until cap has an ExecuteTask message.
func waitForExecuteTask(t *testing.T, cap *captureSend) string {
	t.Helper()
	var execID string
	waitFor(t, func() bool {
		execID = cap.findExecuteTask()
		return execID != ""
	})
	return execID
}

// ── Tests ──────────────────────────────────────────────────────────────────────

func TestReconciler_AgentConnectDisconnect(t *testing.T) {
	r := newTestReconciler()
	cancel := runReconciler(r)
	defer cancel()

	r.Send(AgentConnectedEvent("agent-1", "host1", map[string]string{"env": "test"}, noopSend))
	waitForAgentConnected(t, r, "host1")

	snap := r.Snapshot()
	if len(snap.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(snap.Agents))
	}
	if snap.Agents[0].Hostname != "host1" {
		t.Errorf("hostname = %q, want host1", snap.Agents[0].Hostname)
	}

	r.Send(AgentDisconnectedEvent("agent-1"))
	waitFor(t, func() bool {
		for _, ag := range r.Snapshot().Agents {
			if ag.Hostname == "host1" {
				return !ag.Connected
			}
		}
		return false
	})

	snap = r.Snapshot()
	if snap.Agents[0].Connected {
		t.Error("agent should be disconnected")
	}
}

func TestReconciler_StateReport_Satisfied(t *testing.T) {
	cap := &captureSend{}
	r := newTestReconciler()
	r.tasks = []Task{
		{
			Spec:        &flukeproto.TaskSpec{Name: "task1", CheckIntervalSeconds: 30},
			Selector:    map[string]string{"env": "test"},
			DriftPolicy: DriftPolicyRemediate,
		},
	}
	cancel := runReconciler(r)
	defer cancel()

	r.Send(AgentConnectedEvent("agent-1", "host1", map[string]string{"env": "test"}, cap.send))
	reportID := waitForStateReport(t, cap)

	r.Send(StateReportReceivedEvent("agent-1", reportID, []*flukeproto.TaskState{
		{
			TaskName: "task1",
			StepResults: []*flukeproto.StepCheckResult{
				{StepName: "step1", Satisfied: true},
			},
		},
	}))
	waitForTaskStatus(t, r, "host1", "task1", TaskStatusSatisfied)

	snap := r.Snapshot()
	if len(snap.Drifts) != 0 {
		t.Errorf("expected no drifts, got %d", len(snap.Drifts))
	}
}

func TestReconciler_StateReport_Drifted_Remediate(t *testing.T) {
	cap := &captureSend{}
	r := newTestReconciler()
	r.tasks = []Task{
		{
			Spec:        &flukeproto.TaskSpec{Name: "task1", CheckIntervalSeconds: 30},
			Selector:    map[string]string{"env": "test"},
			DriftPolicy: DriftPolicyRemediate,
		},
	}
	cancel := runReconciler(r)
	defer cancel()

	r.Send(AgentConnectedEvent("agent-1", "host1", map[string]string{"env": "test"}, cap.send))
	reportID := waitForStateReport(t, cap)

	r.Send(StateReportReceivedEvent("agent-1", reportID, []*flukeproto.TaskState{
		{
			TaskName: "task1",
			StepResults: []*flukeproto.StepCheckResult{
				{StepName: "step1", Satisfied: false},
			},
		},
	}))
	// Remediate policy immediately dispatches execution.
	waitForTaskStatus(t, r, "host1", "task1", TaskStatusExecuting)

	if r.Snapshot().Tasks[0].ExecutionID == "" {
		t.Error("ExecutionID should be set when executing")
	}

	// Verify ExecuteTask was sent to agent.
	if execID := cap.findExecuteTask(); execID == "" {
		t.Error("no ExecuteTask was sent to agent")
	}
}

func TestReconciler_StateReport_Drifted_AlertOnly(t *testing.T) {
	cap := &captureSend{}
	r := newTestReconciler()
	r.tasks = []Task{
		{
			Spec:        &flukeproto.TaskSpec{Name: "task1", CheckIntervalSeconds: 30},
			Selector:    map[string]string{"env": "test"},
			DriftPolicy: DriftPolicyAlertOnly,
		},
	}
	cancel := runReconciler(r)
	defer cancel()

	r.Send(AgentConnectedEvent("agent-1", "host1", map[string]string{"env": "test"}, cap.send))
	reportID := waitForStateReport(t, cap)

	r.Send(StateReportReceivedEvent("agent-1", reportID, []*flukeproto.TaskState{
		{
			TaskName: "task1",
			StepResults: []*flukeproto.StepCheckResult{
				{StepName: "step1", Satisfied: false},
			},
		},
	}))
	// alert_only stays drifted; does not auto-execute.
	waitForTaskStatus(t, r, "host1", "task1", TaskStatusDrifted)

	snap := r.Snapshot()
	if len(snap.Drifts) != 1 {
		t.Errorf("expected 1 drift, got %d", len(snap.Drifts))
	}

	// No ExecuteTask should be sent.
	if execID := cap.findExecuteTask(); execID != "" {
		t.Error("ExecuteTask should not be sent with alert_only policy")
	}
}

func TestReconciler_TaskComplete_Success(t *testing.T) {
	cap := &captureSend{}
	r := newTestReconciler()
	r.tasks = []Task{
		{
			Spec:        &flukeproto.TaskSpec{Name: "task1", CheckIntervalSeconds: 30},
			Selector:    map[string]string{"env": "test"},
			DriftPolicy: DriftPolicyRemediate,
		},
	}
	cancel := runReconciler(r)
	defer cancel()

	r.Send(AgentConnectedEvent("agent-1", "host1", map[string]string{"env": "test"}, cap.send))
	reportID := waitForStateReport(t, cap)

	r.Send(StateReportReceivedEvent("agent-1", reportID, []*flukeproto.TaskState{
		{TaskName: "task1", StepResults: []*flukeproto.StepCheckResult{{StepName: "s1", Satisfied: false}}},
	}))
	waitForTaskStatus(t, r, "host1", "task1", TaskStatusExecuting)

	execID := waitForExecuteTask(t, cap)

	r.Send(TaskCompleteReceivedEvent("agent-1", &flukeproto.TaskComplete{
		ExecutionId: execID,
		Outcome:     flukeproto.TaskOutcome_TASK_OUTCOME_SUCCESS,
	}))
	waitForTaskStatus(t, r, "host1", "task1", TaskStatusSucceeded)

	waitFor(t, func() bool { return len(r.Snapshot().Events) == 1 })
	if r.Snapshot().Events[0].Outcome != flukeproto.TaskOutcome_TASK_OUTCOME_SUCCESS {
		t.Errorf("event outcome = %v, want success", r.Snapshot().Events[0].Outcome)
	}
}

func TestReconciler_TaskComplete_Failed(t *testing.T) {
	cap := &captureSend{}
	r := newTestReconciler()
	r.tasks = []Task{
		{
			Spec:        &flukeproto.TaskSpec{Name: "task1", CheckIntervalSeconds: 30},
			Selector:    map[string]string{"env": "test"},
			DriftPolicy: DriftPolicyRemediate,
		},
	}
	cancel := runReconciler(r)
	defer cancel()

	r.Send(AgentConnectedEvent("agent-1", "host1", map[string]string{"env": "test"}, cap.send))
	reportID := waitForStateReport(t, cap)

	r.Send(StateReportReceivedEvent("agent-1", reportID, []*flukeproto.TaskState{
		{TaskName: "task1", StepResults: []*flukeproto.StepCheckResult{{StepName: "s1", Satisfied: false}}},
	}))
	waitForTaskStatus(t, r, "host1", "task1", TaskStatusExecuting)
	execID := waitForExecuteTask(t, cap)

	r.Send(TaskCompleteReceivedEvent("agent-1", &flukeproto.TaskComplete{
		ExecutionId: execID,
		Outcome:     flukeproto.TaskOutcome_TASK_OUTCOME_FAILED,
	}))
	waitForTaskStatus(t, r, "host1", "task1", TaskStatusFailed)
}

func TestReconciler_DisconnectDuringExecution(t *testing.T) {
	cap := &captureSend{}
	r := newTestReconciler()
	r.tasks = []Task{
		{
			Spec:        &flukeproto.TaskSpec{Name: "task1", CheckIntervalSeconds: 30},
			Selector:    map[string]string{"env": "test"},
			DriftPolicy: DriftPolicyRemediate,
		},
	}
	cancel := runReconciler(r)
	defer cancel()

	r.Send(AgentConnectedEvent("agent-1", "host1", map[string]string{"env": "test"}, cap.send))
	reportID := waitForStateReport(t, cap)

	r.Send(StateReportReceivedEvent("agent-1", reportID, []*flukeproto.TaskState{
		{TaskName: "task1", StepResults: []*flukeproto.StepCheckResult{{StepName: "s1", Satisfied: false}}},
	}))
	waitForTaskStatus(t, r, "host1", "task1", TaskStatusExecuting)

	r.Send(AgentDisconnectedEvent("agent-1"))
	waitForTaskStatus(t, r, "host1", "task1", TaskStatusFailed)
}

func TestReconciler_ManifestUpdate_AddsRecords(t *testing.T) {
	cap := &captureSend{}
	r := newTestReconciler()
	cancel := runReconciler(r)
	defer cancel()

	// Agent connects before manifest is loaded.
	r.Send(AgentConnectedEvent("agent-1", "host1", map[string]string{"env": "test"}, cap.send))
	waitForAgentConnected(t, r, "host1")

	if len(r.Snapshot().Tasks) != 0 {
		t.Errorf("expected 0 tasks before manifest, got %d", len(r.Snapshot().Tasks))
	}

	r.Send(ManifestUpdatedEvent([]Task{
		{
			Spec:        &flukeproto.TaskSpec{Name: "task1", CheckIntervalSeconds: 30},
			Selector:    map[string]string{"env": "test"},
			DriftPolicy: DriftPolicyRemediate,
		},
	}))
	waitFor(t, func() bool { return len(r.Snapshot().Tasks) == 1 })

	// A state report should have been sent.
	if reportID := cap.findStateReport(); reportID == "" {
		t.Error("no RequestStateReport sent after manifest update")
	}
}

func TestReconciler_ManifestUpdate_RemovesUnmatchedTasks(t *testing.T) {
	cap := &captureSend{}
	r := newTestReconciler()
	r.tasks = []Task{
		{
			Spec:        &flukeproto.TaskSpec{Name: "task1", CheckIntervalSeconds: 30},
			Selector:    map[string]string{"env": "test"},
			DriftPolicy: DriftPolicyRemediate,
		},
	}
	cancel := runReconciler(r)
	defer cancel()

	r.Send(AgentConnectedEvent("agent-1", "host1", map[string]string{"env": "test"}, cap.send))
	waitFor(t, func() bool { return len(r.Snapshot().Tasks) == 1 })

	// Update manifest to remove the task.
	r.Send(ManifestUpdatedEvent([]Task{}))
	waitFor(t, func() bool { return len(r.Snapshot().Tasks) == 0 })
}

func TestReconciler_ManifestUpdate_SkipsNonMatchingAgents(t *testing.T) {
	cap := &captureSend{}
	r := newTestReconciler()
	cancel := runReconciler(r)
	defer cancel()

	r.Send(AgentConnectedEvent("agent-1", "host1", map[string]string{"env": "prod"}, cap.send))
	waitForAgentConnected(t, r, "host1")

	// Manifest task requires env=staging; should not match host1 (env=prod).
	r.Send(ManifestUpdatedEvent([]Task{
		{
			Spec:        &flukeproto.TaskSpec{Name: "staging-task", CheckIntervalSeconds: 30},
			Selector:    map[string]string{"env": "staging"},
			DriftPolicy: DriftPolicyRemediate,
		},
	}))

	// Give time for any spurious processing.
	time.Sleep(20 * time.Millisecond)

	if len(r.Snapshot().Tasks) != 0 {
		t.Errorf("expected 0 tasks for non-matching agent, got %d", len(r.Snapshot().Tasks))
	}
}

func TestReconciler_Heartbeat_AgentRemainsConnected(t *testing.T) {
	r := newTestReconciler()
	cancel := runReconciler(r)
	defer cancel()

	r.Send(AgentConnectedEvent("agent-1", "host1", map[string]string{}, noopSend))
	waitForAgentConnected(t, r, "host1")

	// Send several heartbeats — agent should remain connected and not panic.
	for i := 0; i < 5; i++ {
		r.Send(HeartbeatReceivedEvent("agent-1"))
	}
	// Give reconciler time to drain the heartbeat events.
	time.Sleep(20 * time.Millisecond)

	snap := r.Snapshot()
	if len(snap.Agents) == 0 || !snap.Agents[0].Connected {
		t.Error("agent should remain connected after heartbeats")
	}
}

func TestReconciler_Snapshot_EmptyBeforeRun(t *testing.T) {
	r := newTestReconciler()
	if snap := r.Snapshot(); snap == nil {
		t.Error("Snapshot() should never return nil")
	}
}

func TestReconciler_SendBufferFull(t *testing.T) {
	r := newTestReconciler()
	// Do NOT start the run loop — events will pile up.
	for i := 0; i < 256; i++ {
		_ = r.Send(HeartbeatReceivedEvent("agent-1"))
	}
	err := r.Send(HeartbeatReceivedEvent("agent-1"))
	if err == nil {
		t.Error("expected error when event buffer is full")
	}
}

func TestReconciler_IgnoresUnknownAgent(t *testing.T) {
	r := newTestReconciler()
	cancel := runReconciler(r)
	defer cancel()

	// State report from unknown agent should not panic or alter state.
	r.Send(StateReportReceivedEvent("unknown-agent", "rep1", nil))
	time.Sleep(10 * time.Millisecond) // give time to process
	if len(r.Snapshot().Tasks) != 0 {
		t.Errorf("unexpected tasks after report from unknown agent")
	}
}

func TestReconciler_CheckExecutionError_StopsTask_NoApplyEvent(t *testing.T) {
	cap := &captureSend{}
	r := newTestReconciler()
	r.tasks = []Task{{
		Spec:        &flukeproto.TaskSpec{Name: "task1", CheckIntervalSeconds: 30, Steps: []*flukeproto.StepSpec{{Name: "shell:deploy"}}},
		Selector:    map[string]string{"env": "test"},
		DriftPolicy: DriftPolicyRemediate,
	}}
	cancel := runReconciler(r)
	defer cancel()

	r.Send(AgentConnectedEvent("agent-1", "host1", map[string]string{"env": "test"}, cap.send))
	reportID := waitForStateReport(t, cap)

	r.Send(StateReportReceivedEvent("agent-1", reportID, []*flukeproto.TaskState{{
		TaskName: "task1",
		StepResults: []*flukeproto.StepCheckResult{{
			StepName:     "shell:deploy",
			Satisfied:    false,
			Stderr:       "check command not found",
			CheckOutcome: flukeproto.CheckOutcome_CHECK_OUTCOME_EXECUTION_ERROR,
		}},
	}}))

	waitForTaskStatus(t, r, "host1", "task1", TaskStatusFailed)

	if execID := cap.findExecuteTask(); execID != "" {
		t.Fatalf("ExecuteTask should not be sent on check execution_error, got execution_id=%q", execID)
	}

	snap := r.Snapshot()
	if len(snap.OperationEvents) == 0 {
		t.Fatal("expected at least one operation event")
	}
	for _, ev := range snap.OperationEvents {
		if ev.Phase == OperationPhaseApply {
			t.Fatalf("unexpected apply event for check execution_error path: %#v", ev)
		}
	}
}

func TestReconciler_ApplyFailure_ContinuePath_RecordsErrorAndContinues(t *testing.T) {
	cap := &captureSend{}
	r := newTestReconciler()
	r.tasks = []Task{{
		Spec: &flukeproto.TaskSpec{
			Name:                 "task1",
			CheckIntervalSeconds: 30,
			Steps: []*flukeproto.StepSpec{
				{Name: "shell:deploy", OnFailure: flukeproto.OnFailure_ON_FAILURE_CONTINUE},
				{Name: "shell:verify", OnFailure: flukeproto.OnFailure_ON_FAILURE_ABORT},
			},
		},
		Selector:    map[string]string{"env": "test"},
		DriftPolicy: DriftPolicyRemediate,
	}}
	cancel := runReconciler(r)
	defer cancel()

	r.Send(AgentConnectedEvent("agent-1", "host1", map[string]string{"env": "test"}, cap.send))
	reportID := waitForStateReport(t, cap)
	r.Send(StateReportReceivedEvent("agent-1", reportID, []*flukeproto.TaskState{{
		TaskName:    "task1",
		StepResults: []*flukeproto.StepCheckResult{{StepName: "shell:deploy", Satisfied: false}, {StepName: "shell:verify", Satisfied: false}},
	}}))

	waitForTaskStatus(t, r, "host1", "task1", TaskStatusExecuting)
	execID := waitForExecuteTask(t, cap)

	r.Send(StepResultReceivedEvent("agent-1", &flukeproto.StepResult{
		ExecutionId: execID,
		StepName:    "shell:deploy",
		Outcome:     flukeproto.StepOutcome_STEP_OUTCOME_FAILED,
		ExitCode:    7,
	}))
	r.Send(StepResultReceivedEvent("agent-1", &flukeproto.StepResult{
		ExecutionId: execID,
		StepName:    "shell:verify",
		Outcome:     flukeproto.StepOutcome_STEP_OUTCOME_SUCCESS,
		ExitCode:    0,
	}))
	r.Send(TaskCompleteReceivedEvent("agent-1", &flukeproto.TaskComplete{
		ExecutionId: execID,
		Outcome:     flukeproto.TaskOutcome_TASK_OUTCOME_SUCCESS,
	}))

	waitForTaskStatus(t, r, "host1", "task1", TaskStatusSucceeded)

	snap := r.Snapshot()
	var sawApplyError bool
	for _, ev := range snap.OperationEvents {
		if ev.Phase == OperationPhaseApply && ev.Outcome == OperationOutcomeExecutionError {
			sawApplyError = true
		}
	}
	if !sawApplyError {
		t.Fatal("expected apply execution_error operation event")
	}

	var sawApplySuccess bool
	for _, ev := range snap.OperationEvents {
		if ev.Phase == OperationPhaseApply && ev.Outcome == OperationOutcomeApplied {
			sawApplySuccess = true
		}
	}
	if !sawApplySuccess {
		t.Fatal("expected apply success operation event for continued step")
	}
}

func TestReconciler_DriftPolicyMatrix(t *testing.T) {
	tests := []struct {
		name          string
		policy        DriftPolicy
		satisfied     bool
		expectExecute bool
	}{
		{name: "no drift", policy: DriftPolicyRemediate, satisfied: true, expectExecute: false},
		{name: "alert_only", policy: DriftPolicyAlertOnly, satisfied: false, expectExecute: false},
		{name: "remediate", policy: DriftPolicyRemediate, satisfied: false, expectExecute: true},
		{name: "remediate_and_alert", policy: DriftPolicyRemediateAndAlert, satisfied: false, expectExecute: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cap := &captureSend{}
			r := newTestReconciler()
			r.tasks = []Task{{
				Spec:        &flukeproto.TaskSpec{Name: "task1", CheckIntervalSeconds: 30, Steps: []*flukeproto.StepSpec{{Name: "shell:deploy"}}},
				Selector:    map[string]string{"env": "test"},
				DriftPolicy: tc.policy,
			}}
			cancel := runReconciler(r)
			defer cancel()

			r.Send(AgentConnectedEvent("agent-1", "host1", map[string]string{"env": "test"}, cap.send))
			reportID := waitForStateReport(t, cap)
			r.Send(StateReportReceivedEvent("agent-1", reportID, []*flukeproto.TaskState{{
				TaskName:    "task1",
				StepResults: []*flukeproto.StepCheckResult{{StepName: "shell:deploy", Satisfied: tc.satisfied}},
			}}))

			waitFor(t, func() bool {
				snap := r.Snapshot()
				if len(snap.Tasks) == 0 {
					return false
				}
				if tc.expectExecute {
					return snap.Tasks[0].Status == TaskStatusExecuting
				}
				return snap.Tasks[0].Status == TaskStatusSatisfied || snap.Tasks[0].Status == TaskStatusDrifted
			})

			execID := cap.findExecuteTask()
			if tc.expectExecute && execID == "" {
				t.Fatal("expected ExecuteTask dispatch")
			}
			if !tc.expectExecute && execID != "" {
				t.Fatalf("unexpected ExecuteTask dispatch: %q", execID)
			}

			if tc.name == "alert_only" || tc.name == "no drift" {
				for _, ev := range r.Snapshot().OperationEvents {
					if ev.Phase == OperationPhaseApply {
						t.Fatalf("unexpected apply event for %s: %#v", tc.name, ev)
					}
				}
			}
		})
	}
}

func TestReconciler_OperationEventMetadata_RequiredFields(t *testing.T) {
	cap := &captureSend{}
	r := newTestReconciler()
	r.tasks = []Task{{
		Spec: &flukeproto.TaskSpec{
			Name:                 "task1",
			CheckIntervalSeconds: 30,
			Steps:                []*flukeproto.StepSpec{{Name: "shell:deploy"}},
		},
		Selector:    map[string]string{"env": "test"},
		DriftPolicy: DriftPolicyRemediate,
	}}
	cancel := runReconciler(r)
	defer cancel()

	r.Send(AgentConnectedEvent("agent-1", "host1", map[string]string{"env": "test"}, cap.send))
	reportID := waitForStateReport(t, cap)
	r.Send(StateReportReceivedEvent("agent-1", reportID, []*flukeproto.TaskState{{
		TaskName:    "task1",
		StepResults: []*flukeproto.StepCheckResult{{StepName: "shell:deploy", Satisfied: false, CheckOutcome: flukeproto.CheckOutcome_CHECK_OUTCOME_DRIFT}},
	}}))

	execID := waitForExecuteTask(t, cap)
	r.Send(StepResultReceivedEvent("agent-1", &flukeproto.StepResult{
		ExecutionId: execID,
		StepName:    "shell:deploy",
		Outcome:     flukeproto.StepOutcome_STEP_OUTCOME_SUCCESS,
		ExitCode:    0,
	}))
	r.Send(TaskCompleteReceivedEvent("agent-1", &flukeproto.TaskComplete{
		ExecutionId: execID,
		Outcome:     flukeproto.TaskOutcome_TASK_OUTCOME_SUCCESS,
	}))

	waitForTaskStatus(t, r, "host1", "task1", TaskStatusSucceeded)

	snap := r.Snapshot()
	if len(snap.OperationEvents) < 2 {
		t.Fatalf("len(operation_events) = %d, want at least 2 (check + apply)", len(snap.OperationEvents))
	}

	for _, ev := range snap.OperationEvents {
		if ev.AgentID == "" {
			t.Fatal("agent_id must be set")
		}
		if ev.AgentName == "" {
			t.Fatal("agent_name must be set")
		}
		if ev.TaskName == "" {
			t.Fatal("task_name must be set")
		}
		if ev.ExecutorType == "" {
			t.Fatal("executor_type must be set")
		}
		if ev.ExecutorName == "" {
			t.Fatal("executor_name must be set")
		}
		if ev.Phase != OperationPhaseCheck && ev.Phase != OperationPhaseApply {
			t.Fatalf("invalid phase %q", ev.Phase)
		}
		switch ev.Phase {
		case OperationPhaseCheck:
			if ev.Outcome != OperationOutcomeSatisfied && ev.Outcome != OperationOutcomeDrift && ev.Outcome != OperationOutcomeExecutionError {
				t.Fatalf("invalid check outcome %q", ev.Outcome)
			}
		case OperationPhaseApply:
			if ev.Outcome != OperationOutcomeApplied && ev.Outcome != OperationOutcomeExecutionError {
				t.Fatalf("invalid apply outcome %q", ev.Outcome)
			}
		}
		if ev.Timestamp.IsZero() {
			t.Fatal("timestamp must be set")
		}
		if ev.Outcome == OperationOutcomeExecutionError && strings.TrimSpace(ev.ErrorMessage) == "" {
			t.Fatal("error_message must be set for execution_error")
		}
	}
}

func TestReconciler_CheckStderrOnly_RemainsDrift_NotExecutionError(t *testing.T) {
	cap := &captureSend{}
	r := newTestReconciler()
	r.tasks = []Task{{
		Spec:        &flukeproto.TaskSpec{Name: "task1", CheckIntervalSeconds: 30, Steps: []*flukeproto.StepSpec{{Name: "shell:deploy"}}},
		Selector:    map[string]string{"env": "test"},
		DriftPolicy: DriftPolicyAlertOnly,
	}}
	cancel := runReconciler(r)
	defer cancel()

	r.Send(AgentConnectedEvent("agent-1", "host1", map[string]string{"env": "test"}, cap.send))
	reportID := waitForStateReport(t, cap)
	r.Send(StateReportReceivedEvent("agent-1", reportID, []*flukeproto.TaskState{{
		TaskName: "task1",
		StepResults: []*flukeproto.StepCheckResult{{
			StepName:     "shell:deploy",
			Satisfied:    false,
			Stderr:       "check wrote stderr",
			CheckOutcome: flukeproto.CheckOutcome_CHECK_OUTCOME_DRIFT,
		}},
	}}))

	waitForTaskStatus(t, r, "host1", "task1", TaskStatusDrifted)

	for _, ev := range r.Snapshot().OperationEvents {
		if ev.Phase == OperationPhaseCheck {
			if ev.Outcome != OperationOutcomeDrift {
				t.Fatalf("check outcome = %q, want drift", ev.Outcome)
			}
			return
		}
	}
	t.Fatal("expected check operation event")
}

func TestReconciler_TaskComplete_InvalidTransition_DoesNotRecordCompletion(t *testing.T) {
	cap := &captureSend{}
	r := newTestReconciler()
	r.tasks = []Task{{
		Spec:        &flukeproto.TaskSpec{Name: "task1", CheckIntervalSeconds: 30, Steps: []*flukeproto.StepSpec{{Name: "shell:deploy"}}},
		Selector:    map[string]string{"env": "test"},
		DriftPolicy: DriftPolicyRemediate,
	}}
	cancel := runReconciler(r)
	defer cancel()

	r.Send(AgentConnectedEvent("agent-1", "host1", map[string]string{"env": "test"}, cap.send))
	reportID := waitForStateReport(t, cap)
	r.Send(StateReportReceivedEvent("agent-1", reportID, []*flukeproto.TaskState{{
		TaskName:    "task1",
		StepResults: []*flukeproto.StepCheckResult{{StepName: "shell:deploy", Satisfied: false}},
	}}))

	execID := waitForExecuteTask(t, cap)
	key := recordKey{hostname: "host1", taskName: "task1"}
	r.records[key].Status = TaskStatusUnknown

	r.Send(TaskCompleteReceivedEvent("agent-1", &flukeproto.TaskComplete{
		ExecutionId: execID,
		Outcome:     flukeproto.TaskOutcome_TASK_OUTCOME_SUCCESS,
	}))

	time.Sleep(20 * time.Millisecond)
	if len(r.Snapshot().Events) != 0 {
		t.Fatal("unexpected completion event recorded for invalid transition")
	}
	if _, ok := r.executions[execID]; !ok {
		t.Fatal("execution runtime should remain when completion transition is invalid")
	}
}

func TestReconciler_ApplySkipped_DoesNotRecordAppliedEvent(t *testing.T) {
	cap := &captureSend{}
	r := newTestReconciler()
	r.tasks = []Task{{
		Spec: &flukeproto.TaskSpec{
			Name:                 "task1",
			CheckIntervalSeconds: 30,
			Steps: []*flukeproto.StepSpec{
				{Name: "shell:skip"},
				{Name: "shell:run"},
			},
		},
		Selector:    map[string]string{"env": "test"},
		DriftPolicy: DriftPolicyRemediate,
	}}
	cancel := runReconciler(r)
	defer cancel()

	r.Send(AgentConnectedEvent("agent-1", "host1", map[string]string{"env": "test"}, cap.send))
	reportID := waitForStateReport(t, cap)
	r.Send(StateReportReceivedEvent("agent-1", reportID, []*flukeproto.TaskState{{
		TaskName:    "task1",
		StepResults: []*flukeproto.StepCheckResult{{StepName: "shell:skip", Satisfied: false}, {StepName: "shell:run", Satisfied: false}},
	}}))

	execID := waitForExecuteTask(t, cap)
	r.Send(StepResultReceivedEvent("agent-1", &flukeproto.StepResult{
		ExecutionId: execID,
		StepName:    "shell:skip",
		Outcome:     flukeproto.StepOutcome_STEP_OUTCOME_SKIPPED,
		ExitCode:    0,
	}))
	r.Send(StepResultReceivedEvent("agent-1", &flukeproto.StepResult{
		ExecutionId: execID,
		StepName:    "shell:run",
		Outcome:     flukeproto.StepOutcome_STEP_OUTCOME_SUCCESS,
		ExitCode:    0,
	}))
	r.Send(TaskCompleteReceivedEvent("agent-1", &flukeproto.TaskComplete{
		ExecutionId: execID,
		Outcome:     flukeproto.TaskOutcome_TASK_OUTCOME_SUCCESS,
	}))
	waitForTaskStatus(t, r, "host1", "task1", TaskStatusSucceeded)

	for _, ev := range r.Snapshot().OperationEvents {
		if ev.Phase == OperationPhaseApply && ev.ExecutorName == "skip" {
			t.Fatalf("unexpected apply event for skipped step: %#v", ev)
		}
	}
}

func TestReconciler_InvalidStateReport_DoesNotRecordOperationEvents(t *testing.T) {
	cap := &captureSend{}
	r := newTestReconciler()
	r.tasks = []Task{{
		Spec:        &flukeproto.TaskSpec{Name: "task1", CheckIntervalSeconds: 30, Steps: []*flukeproto.StepSpec{{Name: "shell:deploy"}}},
		Selector:    map[string]string{"env": "test"},
		DriftPolicy: DriftPolicyRemediate,
	}}
	cancel := runReconciler(r)
	defer cancel()

	r.Send(AgentConnectedEvent("agent-1", "host1", map[string]string{"env": "test"}, cap.send))
	_ = waitForStateReport(t, cap)

	r.Send(StateReportReceivedEvent("agent-1", "stale-report-id", []*flukeproto.TaskState{{
		TaskName: "task1",
		StepResults: []*flukeproto.StepCheckResult{{
			StepName:     "shell:deploy",
			Satisfied:    false,
			CheckOutcome: flukeproto.CheckOutcome_CHECK_OUTCOME_EXECUTION_ERROR,
			Stderr:       "stale report should be ignored",
		}},
	}}))

	time.Sleep(20 * time.Millisecond)
	if len(r.Snapshot().OperationEvents) != 0 {
		t.Fatalf("len(operation_events) = %d, want 0 for stale/invalid report", len(r.Snapshot().OperationEvents))
	}
}
