package agent

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	flukeproto "github.com/taiidani/fluke/internal/proto/flukeproto"
)

// ── State reporting ───────────────────────────────────────────────────────────

// handleStateReport runs the check expression for every step in every task
// in the request, then sends a StateReport back to the server.
//
// Checks within a task run sequentially (steps have ordering semantics).
// Tasks run concurrently to keep report latency low when there are many tasks.
func (s *session) handleStateReport(ctx context.Context, req *flukeproto.RequestStateReport) {
	type result struct {
		index int
		state *flukeproto.TaskState
	}

	results := make([]result, len(req.Tasks))
	var wg sync.WaitGroup

	for i, task := range req.Tasks {
		wg.Add(1)
		go func(idx int, t *flukeproto.TaskSpec) {
			defer wg.Done()
			results[idx] = result{index: idx, state: s.checkTask(ctx, t)}
		}(i, task)
	}
	wg.Wait()

	taskStates := make([]*flukeproto.TaskState, len(req.Tasks))
	for _, r := range results {
		taskStates[r.index] = r.state
	}

	s.send(&flukeproto.AgentMessage{
		Payload: &flukeproto.AgentMessage_StateReport{
			StateReport: &flukeproto.StateReport{
				ReportId:   req.ReportId,
				TaskStates: taskStates,
			},
		},
	})
}

// checkTask runs the check expression for each step in a task and returns the
// aggregated TaskState. Steps with no check expression are always unsatisfied.
func (s *session) checkTask(ctx context.Context, task *flukeproto.TaskSpec) *flukeproto.TaskState {
	state := &flukeproto.TaskState{
		TaskName:    task.Name,
		StepResults: make([]*flukeproto.StepCheckResult, 0, len(task.Steps)),
	}
	for _, step := range task.Steps {
		result := &flukeproto.StepCheckResult{StepName: step.Name}
		if step.Check != "" {
			result.CheckOutcome, result.Stderr = s.runCheck(ctx, step)
			result.Satisfied = result.CheckOutcome == flukeproto.CheckOutcome_CHECK_OUTCOME_SATISFIED
		} else {
			result.CheckOutcome = flukeproto.CheckOutcome_CHECK_OUTCOME_DRIFT
		}
		// No check expression → always unsatisfied (step must run).
		state.StepResults = append(state.StepResults, result)
	}
	return state
}

// ── Task execution ────────────────────────────────────────────────────────────

// handleExecuteTask runs a task dispatched by the server. It registers a
// cancellation function so a concurrent CancelTask message can interrupt it,
// then sends TaskComplete when done.
func (s *session) handleExecuteTask(ctx context.Context, req *flukeproto.ExecuteTask) {
	execCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.executions[req.ExecutionId] = cancel
	s.mu.Unlock()

	defer func() {
		cancel()
		s.mu.Lock()
		delete(s.executions, req.ExecutionId)
		s.mu.Unlock()
	}()

	s.log.Info("execution started",
		"execution_id", req.ExecutionId,
		"task", req.Task.Name,
	)

	outcome := s.executeTask(execCtx, req.ExecutionId, req.Task)

	s.log.Info("execution completed",
		"execution_id", req.ExecutionId,
		"task", req.Task.Name,
		"outcome", outcome,
	)

	s.send(&flukeproto.AgentMessage{
		Payload: &flukeproto.AgentMessage_TaskComplete{
			TaskComplete: &flukeproto.TaskComplete{
				ExecutionId: req.ExecutionId,
				Outcome:     outcome,
			},
		},
	})
}

// executeTask runs each step in order, respecting on_failure policy and
// context cancellation.
func (s *session) executeTask(ctx context.Context, executionID string, task *flukeproto.TaskSpec) flukeproto.TaskOutcome {
	for _, step := range task.Steps {
		if ctx.Err() != nil {
			return flukeproto.TaskOutcome_TASK_OUTCOME_CANCELLED
		}

		outcome := s.executeStep(ctx, executionID, step)

		if outcome == flukeproto.StepOutcome_STEP_OUTCOME_FAILED {
			if step.OnFailure == flukeproto.OnFailure_ON_FAILURE_ABORT {
				return flukeproto.TaskOutcome_TASK_OUTCOME_FAILED
			}
			// ON_FAILURE_CONTINUE: log and proceed to the next step.
			s.log.Warn("step failed, continuing",
				"execution_id", executionID,
				"step", step.Name,
			)
		}
	}

	if ctx.Err() != nil {
		return flukeproto.TaskOutcome_TASK_OUTCOME_CANCELLED
	}
	return flukeproto.TaskOutcome_TASK_OUTCOME_SUCCESS
}

// executeStep runs a single step: evaluates the check (if present), skips if
// already satisfied, otherwise runs the command and streams output.
// Sends a StepResult before returning.
func (s *session) executeStep(ctx context.Context, executionID string, step *flukeproto.StepSpec) flukeproto.StepOutcome {
	start := time.Now()

	// Run check if present; skip the command if already satisfied.
	if step.Check != "" {
		checkOutcome, _ := s.runCheck(ctx, step)
		if checkOutcome == flukeproto.CheckOutcome_CHECK_OUTCOME_SATISFIED {
			s.log.Debug("step already satisfied, skipping",
				"execution_id", executionID,
				"step", step.Name,
			)
			s.sendStepResult(executionID, step.Name, flukeproto.StepOutcome_STEP_OUTCOME_SKIPPED, 0, start)
			return flukeproto.StepOutcome_STEP_OUTCOME_SKIPPED
		}
	}

	// Execute the command.
	exitCode, err := s.runCommand(ctx, executionID, step)
	outcome := flukeproto.StepOutcome_STEP_OUTCOME_SUCCESS
	if err != nil || exitCode != 0 {
		if ctx.Err() != nil {
			// Context was cancelled — report as FAILED so the server sees a
			// terminal step result; TaskComplete{CANCELLED} follows immediately.
			outcome = flukeproto.StepOutcome_STEP_OUTCOME_FAILED
		} else {
			outcome = flukeproto.StepOutcome_STEP_OUTCOME_FAILED
		}
	}

	s.sendStepResult(executionID, step.Name, outcome, int32(exitCode), start)
	return outcome
}

// sendStepResult emits a StepResult message.
func (s *session) sendStepResult(executionID, stepName string, outcome flukeproto.StepOutcome, exitCode int32, start time.Time) {
	s.send(&flukeproto.AgentMessage{
		Payload: &flukeproto.AgentMessage_StepResult{
			StepResult: &flukeproto.StepResult{
				ExecutionId: executionID,
				StepName:    stepName,
				Outcome:     outcome,
				ExitCode:    exitCode,
				DurationMs:  time.Since(start).Milliseconds(),
			},
		},
	})
}

// ── Command execution ─────────────────────────────────────────────────────────

// runCheck executes a step's check expression and returns explicit outcome.
// stderr is captured and returned for the StateReport (helps diagnose broken
// checks). stdout is discarded.
func (s *session) runCheck(ctx context.Context, step *flukeproto.StepSpec) (flukeproto.CheckOutcome, string) {
	checkCtx, cancel := context.WithTimeout(ctx, s.cfg.CommandTimeout)
	defer cancel()

	cmd := s.buildCmd(checkCtx, step.Check, step)
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	if err == nil {
		return flukeproto.CheckOutcome_CHECK_OUTCOME_SATISFIED, stderrBuf.String()
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return flukeproto.CheckOutcome_CHECK_OUTCOME_DRIFT, stderrBuf.String()
	}
	return flukeproto.CheckOutcome_CHECK_OUTCOME_EXECUTION_ERROR, stderrBuf.String()
}

// runCommand executes a step's command, streaming stdout and stderr as
// StepLogLine messages. Returns the exit code and any unexpected error.
func (s *session) runCommand(ctx context.Context, executionID string, step *flukeproto.StepSpec) (exitCode int, err error) {
	cmdCtx, cancel := context.WithTimeout(ctx, s.cfg.CommandTimeout)
	defer cancel()

	cmd := s.buildCmd(cmdCtx, step.Command, step)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return -1, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return -1, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return -1, fmt.Errorf("start command: %w", err)
	}

	// Stream stdout and stderr concurrently. Both goroutines must finish before
	// cmd.Wait() is called or we risk reading from closed pipes.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		s.pipeOutput(executionID, step.Name, flukeproto.LogStream_LOG_STREAM_STDOUT, stdout)
	}()
	go func() {
		defer wg.Done()
		s.pipeOutput(executionID, step.Name, flukeproto.LogStream_LOG_STREAM_STDERR, stderr)
	}()
	wg.Wait()

	if err := cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		// Context cancellation kills the process; cmd.Wait returns a non-ExitError.
		return -1, err
	}
	return 0, nil
}

// buildCmd constructs an exec.Cmd for a shell expression, applying the step's
// working_dir, env, and run_as settings.
func (s *session) buildCmd(ctx context.Context, expr string, step *flukeproto.StepSpec) *exec.Cmd {
	var cmd *exec.Cmd
	if step.RunAs != "" {
		cmd = exec.CommandContext(ctx, "sudo", "-u", step.RunAs, s.cfg.DefaultShell, "-c", expr)
	} else {
		cmd = exec.CommandContext(ctx, s.cfg.DefaultShell, "-c", expr)
	}

	if step.WorkingDir != "" {
		cmd.Dir = step.WorkingDir
	}

	// Inherit the agent process's environment and overlay step-specific vars.
	cmd.Env = append(os.Environ(), envPairs(step.Env)...)

	return cmd
}

// pipeOutput reads lines from r and sends each as a StepLogLine.
func (s *session) pipeOutput(executionID, stepName string, stream flukeproto.LogStream, r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		s.send(&flukeproto.AgentMessage{
			Payload: &flukeproto.AgentMessage_LogLine{
				LogLine: &flukeproto.StepLogLine{
					ExecutionId:     executionID,
					StepName:        stepName,
					Stream:          stream,
					Text:            scanner.Text(),
					TimestampUnixMs: time.Now().UnixMilli(),
				},
			},
		})
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// envPairs converts a map of environment variables to KEY=VALUE strings
// suitable for exec.Cmd.Env.
func envPairs(m map[string]string) []string {
	pairs := make([]string, 0, len(m))
	for k, v := range m {
		pairs = append(pairs, k+"="+v)
	}
	return pairs
}
