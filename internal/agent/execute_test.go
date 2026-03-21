package agent

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	flukeproto "github.com/taiidani/fluke/internal/proto/flukeproto"
)

type capturedAgentSend struct {
	mu   sync.Mutex
	msgs []*flukeproto.AgentMessage
}

func (c *capturedAgentSend) send(msg *flukeproto.AgentMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, msg)
}

func (c *capturedAgentSend) stepResults() []*flukeproto.StepResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*flukeproto.StepResult, 0, len(c.msgs))
	for _, msg := range c.msgs {
		if sr := msg.GetStepResult(); sr != nil {
			out = append(out, sr)
		}
	}
	return out
}

func newTestSession(sendFn func(*flukeproto.AgentMessage)) *session {
	return &session{
		cfg: Config{
			DefaultShell:   "/bin/sh",
			CommandTimeout: 2 * time.Second,
		},
		send:       sendFn,
		executions: make(map[string]context.CancelFunc),
		log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestExecuteTask_OnFailureAbort_StopsTask(t *testing.T) {
	cap := &capturedAgentSend{}
	s := newTestSession(cap.send)

	outcome := s.executeTask(context.Background(), "exec-abort", &flukeproto.TaskSpec{
		Name: "task-abort",
		Steps: []*flukeproto.StepSpec{
			{Name: "shell:fail", Command: "exit 7", OnFailure: flukeproto.OnFailure_ON_FAILURE_ABORT},
			{Name: "shell:after", Command: "exit 0", OnFailure: flukeproto.OnFailure_ON_FAILURE_ABORT},
		},
	})

	if outcome != flukeproto.TaskOutcome_TASK_OUTCOME_FAILED {
		t.Fatalf("outcome = %v, want failed", outcome)
	}

	results := cap.stepResults()
	if len(results) != 1 {
		t.Fatalf("len(step_results) = %d, want 1", len(results))
	}
	if results[0].StepName != "shell:fail" {
		t.Fatalf("step_name = %q, want shell:fail", results[0].StepName)
	}
	if results[0].Outcome != flukeproto.StepOutcome_STEP_OUTCOME_FAILED {
		t.Fatalf("step outcome = %v, want failed", results[0].Outcome)
	}
}

func TestExecuteTask_OnFailureContinue_ContinuesAndSucceeds(t *testing.T) {
	cap := &capturedAgentSend{}
	s := newTestSession(cap.send)

	outcome := s.executeTask(context.Background(), "exec-continue", &flukeproto.TaskSpec{
		Name: "task-continue",
		Steps: []*flukeproto.StepSpec{
			{Name: "shell:fail", Command: "exit 9", OnFailure: flukeproto.OnFailure_ON_FAILURE_CONTINUE},
			{Name: "shell:after", Command: "exit 0", OnFailure: flukeproto.OnFailure_ON_FAILURE_ABORT},
		},
	})

	if outcome != flukeproto.TaskOutcome_TASK_OUTCOME_SUCCESS {
		t.Fatalf("outcome = %v, want success", outcome)
	}

	results := cap.stepResults()
	if len(results) != 2 {
		t.Fatalf("len(step_results) = %d, want 2", len(results))
	}
	if results[0].StepName != "shell:fail" || results[0].Outcome != flukeproto.StepOutcome_STEP_OUTCOME_FAILED {
		t.Fatalf("step 0 = (%q, %v), want (shell:fail, failed)", results[0].StepName, results[0].Outcome)
	}
	if results[1].StepName != "shell:after" || results[1].Outcome != flukeproto.StepOutcome_STEP_OUTCOME_SUCCESS {
		t.Fatalf("step 1 = (%q, %v), want (shell:after, success)", results[1].StepName, results[1].Outcome)
	}
}

func TestCheckTask_ReportsExplicitCheckOutcomes(t *testing.T) {
	s := newTestSession(func(*flukeproto.AgentMessage) {})

	state := s.checkTask(context.Background(), &flukeproto.TaskSpec{
		Name: "task-check",
		Steps: []*flukeproto.StepSpec{
			{Name: "shell:satisfied", Check: "exit 0"},
			{Name: "shell:drift", Check: "exit 1"},
		},
	})

	if len(state.StepResults) != 2 {
		t.Fatalf("len(step_results) = %d, want 2", len(state.StepResults))
	}

	if state.StepResults[0].CheckOutcome != flukeproto.CheckOutcome_CHECK_OUTCOME_SATISFIED {
		t.Fatalf("step 0 check_outcome = %v, want satisfied", state.StepResults[0].CheckOutcome)
	}
	if !state.StepResults[0].Satisfied {
		t.Fatal("step 0 satisfied = false, want true")
	}

	if state.StepResults[1].CheckOutcome != flukeproto.CheckOutcome_CHECK_OUTCOME_DRIFT {
		t.Fatalf("step 1 check_outcome = %v, want drift", state.StepResults[1].CheckOutcome)
	}
	if state.StepResults[1].Satisfied {
		t.Fatal("step 1 satisfied = true, want false")
	}

}

func TestCheckTask_ReportsExecutionErrorWhenCheckCannotExecute(t *testing.T) {
	s := newTestSession(func(*flukeproto.AgentMessage) {})
	s.cfg.DefaultShell = "/definitely/missing/shell"

	state := s.checkTask(context.Background(), &flukeproto.TaskSpec{
		Name: "task-check-error",
		Steps: []*flukeproto.StepSpec{
			{Name: "shell:error", Check: "exit 0"},
		},
	})

	if len(state.StepResults) != 1 {
		t.Fatalf("len(step_results) = %d, want 1", len(state.StepResults))
	}
	if state.StepResults[0].CheckOutcome != flukeproto.CheckOutcome_CHECK_OUTCOME_EXECUTION_ERROR {
		t.Fatalf("check_outcome = %v, want execution_error", state.StepResults[0].CheckOutcome)
	}
	if state.StepResults[0].Satisfied {
		t.Fatal("satisfied = true, want false")
	}
}
