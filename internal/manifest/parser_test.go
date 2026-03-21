package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFiles_RejectsDuplicateTaskNames(t *testing.T) {
	_, err := ParseFiles([]string{
		filepath.Join("testdata", "a.fluke.hcl"),
		filepath.Join("testdata", "b.fluke.hcl"),
	})
	if err == nil {
		t.Fatal("expected duplicate task name error")
	}
	if !strings.Contains(err.Error(), "duplicate task name \"web\"") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseFiles_PreservesDeclarationOrder(t *testing.T) {
	manifest, err := ParseFiles([]string{filepath.Join("testdata", "a.fluke.hcl")})
	if err != nil {
		t.Fatalf("ParseFiles: %v", err)
	}

	if len(manifest.Tasks) != 2 {
		t.Fatalf("len(tasks) = %d, want 2", len(manifest.Tasks))
	}

	if manifest.Tasks[0].Name != "web" {
		t.Fatalf("tasks[0].name = %q, want web", manifest.Tasks[0].Name)
	}
	if manifest.Tasks[1].Name != "worker" {
		t.Fatalf("tasks[1].name = %q, want worker", manifest.Tasks[1].Name)
	}

	if len(manifest.Tasks[0].Executors) != 2 {
		t.Fatalf("len(tasks[0].executors) = %d, want 2", len(manifest.Tasks[0].Executors))
	}

	if manifest.Tasks[0].Executors[0].Type != "mise" || manifest.Tasks[0].Executors[0].Name != "runtime" {
		t.Fatalf("executors[0] = %#v, want mise runtime", manifest.Tasks[0].Executors[0])
	}
	if manifest.Tasks[0].Executors[1].Type != "shell" || manifest.Tasks[0].Executors[1].Name != "deploy" {
		t.Fatalf("executors[1] = %#v, want shell deploy", manifest.Tasks[0].Executors[1])
	}
}

func TestParseFiles_RejectsMissingSelector(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-selector.fluke.hcl")
	writeManifestFixture(t, path, `
task "broken" {
  shell "noop" {
    check   = "true"
    command = "true"
  }
}
`)

	_, err := ParseFiles([]string{path})
	if err == nil {
		t.Fatal("expected missing selector error")
	}
	if !strings.Contains(err.Error(), "selector block is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseFiles_RejectsMultipleSelectors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duplicate-selector.fluke.hcl")
	writeManifestFixture(t, path, `
task "broken" {
  selector {
    match_labels = { role = "web" }
  }

  selector {
    match_labels = { env = "prod" }
  }

  shell "noop" {
    check   = "true"
    command = "true"
  }
}
`)

	_, err := ParseFiles([]string{path})
	if err == nil {
		t.Fatal("expected duplicate selector error")
	}
	if !strings.Contains(err.Error(), "selector block must be declared at most once") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseFiles_RejectsUnlabeledTaskBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unlabeled-task.fluke.hcl")
	writeManifestFixture(t, path, `
task {
  selector {
    match_labels = { role = "web" }
  }

  shell "noop" {
    check   = "true"
    command = "true"
  }
}
`)

	_, err := ParseFiles([]string{path})
	if err == nil {
		t.Fatal("expected unlabeled task error")
	}
	if !strings.Contains(err.Error(), "task block must include exactly one name label") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseFiles_RejectsUnknownExecutorType(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unknown-executor.fluke.hcl")
	writeManifestFixture(t, path, `
task "web" {
  selector {
    match_labels = { role = "web" }
  }

  docker "app" {
    image = "nginx:latest"
  }
}
`)

	_, err := ParseFiles([]string{path})
	if err == nil {
		t.Fatal("expected unknown executor type error")
	}
	if !strings.Contains(err.Error(), "unknown executor block type \"docker\"") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseFiles_RejectsExecutorWithoutSingleNameLabel(t *testing.T) {
	t.Run("missing label", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing-executor-label.fluke.hcl")
		writeManifestFixture(t, path, `
task "web" {
  selector {
    match_labels = { role = "web" }
  }

  shell {
    check   = "true"
    command = "true"
  }
}
`)

		_, err := ParseFiles([]string{path})
		if err == nil {
			t.Fatal("expected executor label validation error")
		}
		if !strings.Contains(err.Error(), "executor block \"shell\" in task \"web\" must include exactly one name label") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("extra labels", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "extra-executor-labels.fluke.hcl")
		writeManifestFixture(t, path, `
task "web" {
  selector {
    match_labels = { role = "web" }
  }

  shell "deploy" "extra" {
    check   = "true"
    command = "true"
  }
}
`)

		_, err := ParseFiles([]string{path})
		if err == nil {
			t.Fatal("expected executor label validation error")
		}
		if !strings.Contains(err.Error(), "executor block \"shell\" in task \"web\" must include exactly one name label") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestParseFiles_RejectsTaskWithoutExecutors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-executors.fluke.hcl")
	writeManifestFixture(t, path, `
task "web" {
  selector {
    match_labels = { role = "web" }
  }

  drift {
    policy = "remediate"
  }
}
`)

	_, err := ParseFiles([]string{path})
	if err == nil {
		t.Fatal("expected missing executor error")
	}
	if !strings.Contains(err.Error(), "task \"web\" must declare at least one executor block") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writeManifestFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
