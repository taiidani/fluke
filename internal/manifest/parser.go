package manifest

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

var allowedExecutorTypes = map[string]struct{}{
	"mise":    {},
	"shell":   {},
	"systemd": {},
}

type rawTask struct {
	Description string        `hcl:"description,optional"`
	Selector    []rawSelector `hcl:"selector,block"`
	Remain      hcl.Body      `hcl:",remain"`
}

type rawSelector struct {
	MatchLabels map[string]string `hcl:"match_labels,attr"`
}

// ParseFiles parses manifest files and merges them in the provided file order.
// Task and executor declaration order is preserved.
func ParseFiles(paths []string) (*Manifest, error) {
	manifest := &Manifest{}
	seenTaskNames := make(map[string]string)

	for _, path := range paths {
		tasks, err := parseFile(path)
		if err != nil {
			return nil, err
		}
		for _, task := range tasks {
			if firstPath, exists := seenTaskNames[task.Name]; exists {
				return nil, fmt.Errorf("parse %s: duplicate task name %q (already declared in %s)", path, task.Name, firstPath)
			}
			seenTaskNames[task.Name] = path
			manifest.Tasks = append(manifest.Tasks, task)
		}
	}

	return manifest, nil
}

func parseFile(path string) ([]Task, error) {
	parser := hclparse.NewParser()
	file, diags := parser.ParseHCLFile(path)
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse %s: %s", path, diags.Error())
	}

	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("parse %s: unsupported HCL body type", path)
	}

	tasks := make([]Task, 0)
	for _, block := range body.Blocks {
		if block.Type != "task" {
			continue
		}
		if len(block.Labels) != 1 {
			return nil, fmt.Errorf("parse %s: task block must include exactly one name label", path)
		}
		taskName := block.Labels[0]

		raw, err := decodeTask(path, taskName, block)
		if err != nil {
			return nil, err
		}

		executors := make([]ExecutorBlock, 0)
		for _, child := range block.Body.Blocks {
			switch child.Type {
			case "selector", "drift":
				continue
			default:
				if _, ok := allowedExecutorTypes[child.Type]; !ok {
					return nil, fmt.Errorf("parse %s task %q: unknown executor block type %q (allowed: mise, shell, systemd)", path, taskName, child.Type)
				}
				if len(child.Labels) != 1 {
					return nil, fmt.Errorf("parse %s task %q: executor block %q in task %q must include exactly one name label", path, taskName, child.Type, taskName)
				}
				execName := child.Labels[0]
				executors = append(executors, ExecutorBlock{Type: child.Type, Name: execName})
			}
		}
		if len(executors) == 0 {
			return nil, fmt.Errorf("parse %s task %q: task %q must declare at least one executor block", path, taskName, taskName)
		}

		tasks = append(tasks, Task{
			Name:        taskName,
			Description: raw.Description,
			Selector:    raw.Selector[0].MatchLabels,
			Executors:   executors,
		})
	}

	return tasks, nil
}

func decodeTask(path, taskName string, block *hclsyntax.Block) (*rawTask, error) {
	var decoded rawTask
	diags := gohcl.DecodeBody(block.Body, nil, &decoded)
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse %s task %q: %s", path, taskName, diags.Error())
	}

	if len(decoded.Selector) == 0 {
		return nil, fmt.Errorf("parse %s task %q: selector block is required", path, taskName)
	}
	if len(decoded.Selector) > 1 {
		return nil, fmt.Errorf("parse %s task %q: selector block must be declared at most once", path, taskName)
	}
	if decoded.Selector[0].MatchLabels == nil {
		decoded.Selector[0].MatchLabels = map[string]string{}
	}

	return &decoded, nil
}
