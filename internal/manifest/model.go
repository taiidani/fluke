package manifest

// Manifest is the merged desired-state model from one or more manifest files.
type Manifest struct {
	Tasks []Task
}

// Task is a desired-state task declaration from a manifest.
type Task struct {
	Name        string
	Description string
	Selector    map[string]string
	Executors   []ExecutorBlock
}

// ExecutorBlock describes one executor declaration within a task.
// Executors are preserved in declaration order.
type ExecutorBlock struct {
	Type string
	Name string
}
