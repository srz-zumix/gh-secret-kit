package migrate

// CommonOptions holds common options for migrate commands
type CommonOptions struct {
	Source           string
	Destination      string
	SourceEnv        string
	DestinationEnv   string
	Secrets          []string
	Rename           []string
	Overwrite        bool
	DestinationToken string
}

// RunnerOptions holds options for runner commands
type RunnerOptions struct {
	RunnerLabel    string
	ExistingRunner bool
}

// WorkflowOptions holds options for workflow commands
type WorkflowOptions struct {
	RunnerLabel  string
	WorkflowName string
	Branch       string
	Wait         bool
	Timeout      string
}
