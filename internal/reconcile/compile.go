package reconcile

import (
	"fmt"

	"github.com/taiidani/fluke/internal/manifest"
	flukeproto "github.com/taiidani/fluke/internal/proto/flukeproto"
)

// CompileTasks maps manifest tasks into the reconciler task model.
//
// Executor declaration order from the manifest is preserved in TaskSpec.Steps.
// Per-task drift overrides are not yet represented in manifest.Task, so every
// compiled task currently uses defaultPolicy.
func CompileTasks(manifestTasks []manifest.Task, defaultPolicy DriftPolicy) ([]Task, error) {
	compiled := make([]Task, 0, len(manifestTasks))

	for _, mt := range manifestTasks {
		steps := make([]*flukeproto.StepSpec, 0, len(mt.Executors))
		for _, exec := range mt.Executors {
			if exec.Type == "" {
				return nil, fmt.Errorf("compile task %q: executor type is required", mt.Name)
			}
			if exec.Name == "" {
				return nil, fmt.Errorf("compile task %q: executor name is required", mt.Name)
			}
			steps = append(steps, &flukeproto.StepSpec{Name: exec.Type + ":" + exec.Name})
		}

		compiled = append(compiled, Task{
			Spec: &flukeproto.TaskSpec{
				Name:        mt.Name,
				Description: mt.Description,
				Steps:       steps,
			},
			Selector:    cloneSelector(mt.Selector),
			DriftPolicy: defaultPolicy,
		})
	}

	return compiled, nil
}

func cloneSelector(in map[string]string) map[string]string {
	if in == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
