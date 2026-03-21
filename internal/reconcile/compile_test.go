package reconcile

import (
	"testing"

	"github.com/taiidani/fluke/internal/manifest"
)

func TestCompile_PreservesExecutorDeclarationOrder(t *testing.T) {
	compiled, err := CompileTasks([]manifest.Task{
		{
			Name:        "deploy",
			Description: "deploy app",
			Selector:    map[string]string{"role": "web"},
			Executors: []manifest.ExecutorBlock{
				{Type: "mise", Name: "runtime"},
				{Type: "shell", Name: "assets"},
				{Type: "systemd", Name: "service"},
			},
		},
	}, DriftPolicyRemediate)
	if err != nil {
		t.Fatalf("CompileTasks: %v", err)
	}

	if len(compiled) != 1 {
		t.Fatalf("len(compiled) = %d, want 1", len(compiled))
	}

	steps := compiled[0].Spec.GetSteps()
	if len(steps) != 3 {
		t.Fatalf("len(steps) = %d, want 3", len(steps))
	}

	if steps[0].GetName() != "mise:runtime" {
		t.Fatalf("steps[0].name = %q, want %q", steps[0].GetName(), "mise:runtime")
	}
	if steps[1].GetName() != "shell:assets" {
		t.Fatalf("steps[1].name = %q, want %q", steps[1].GetName(), "shell:assets")
	}
	if steps[2].GetName() != "systemd:service" {
		t.Fatalf("steps[2].name = %q, want %q", steps[2].GetName(), "systemd:service")
	}
}

func TestCompile_UsesDefaultDriftPolicyWhenTaskOverrideMissing(t *testing.T) {
	compiled, err := CompileTasks([]manifest.Task{
		{
			Name:      "worker",
			Selector:  map[string]string{"role": "worker"},
			Executors: []manifest.ExecutorBlock{{Type: "shell", Name: "sync"}},
		},
	}, DriftPolicyAlertOnly)
	if err != nil {
		t.Fatalf("CompileTasks: %v", err)
	}

	if len(compiled) != 1 {
		t.Fatalf("len(compiled) = %d, want 1", len(compiled))
	}

	if compiled[0].DriftPolicy != DriftPolicyAlertOnly {
		t.Fatalf("drift policy = %v, want %v", compiled[0].DriftPolicy, DriftPolicyAlertOnly)
	}
}
