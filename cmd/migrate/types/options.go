package types

// CommonOptions holds common options for migrate commands
type CommonOptions struct {
Source           string
Destination      string
DestinationHost  string
SourceEnv        string
DestinationEnv   string
Secrets          []string
Rename           []string
Overwrite        bool
DestinationToken string
}

// RunnerOptions holds options for runner commands
type RunnerOptions struct {
RunnerLabel string
}

// WorkflowOptions holds options for workflow commands
type WorkflowOptions struct {
RunnerLabel  string
WorkflowName string
Branch       string
Label        string
Wait         bool
Timeout      string
}
