package executor

import (
	"context"
	"errors"
)

// Runner executes a command request and returns captured output.
type Runner interface {
	Run(context.Context, Request) (Result, error)
}

// Request defines one command invocation.
type Request struct {
	Command string
	Args    []string
	Dir     string
	RunAs   string
	Env     map[string]string
	Stdin   string
}

// Result captures command completion output.
type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// ErrNoResultConfigured reports that a fake runner has no queued result.
var ErrNoResultConfigured = errors.New("fake runner: no result configured")

// FakeRunner is a deterministic test runner that records calls and returns
// configured results in order.
//
// FakeRunner is intended for single-threaded unit tests. It mutates internal
// slices and indexes on each call and is not concurrency-safe.
//
// Precedence semantics:
//   - Results queue takes precedence over default Result/HasResult.
//   - Errs queue takes precedence over default Err.
//   - After queue exhaustion, FakeRunner does not fall back to defaults.
//     nextResult/nextError report "not configured" for that stream instead.
type FakeRunner struct {
	Result    Result
	HasResult bool
	Results   []Result
	Err       error
	Errs      []error
	Calls     []Request

	resultIndex int
	errIndex    int
}

// Run records the request and returns the next configured output.
func (f *FakeRunner) Run(_ context.Context, request Request) (Result, error) {
	f.Calls = append(f.Calls, cloneRequest(request))

	err, hasErr := f.nextError()
	result, hasResult := f.nextResult()

	if !hasResult && !hasErr {
		return Result{}, ErrNoResultConfigured
	}
	return result, err
}

func (f *FakeRunner) nextResult() (Result, bool) {
	if len(f.Results) > 0 {
		if f.resultIndex >= len(f.Results) {
			return Result{}, false
		}
		result := f.Results[f.resultIndex]
		f.resultIndex++
		return result, true
	}
	if f.HasResult {
		return f.Result, true
	}
	return Result{}, false
}

func (f *FakeRunner) nextError() (error, bool) {
	if len(f.Errs) > 0 {
		if f.errIndex >= len(f.Errs) {
			return nil, false
		}
		err := f.Errs[f.errIndex]
		f.errIndex++
		return err, true
	}
	if f.Err != nil {
		return f.Err, true
	}
	return nil, false
}

func cloneRequest(in Request) Request {
	clone := in
	if in.Args != nil {
		clone.Args = append([]string(nil), in.Args...)
	}
	if in.Env != nil {
		clone.Env = make(map[string]string, len(in.Env))
		for k, v := range in.Env {
			clone.Env[k] = v
		}
	}
	return clone
}
