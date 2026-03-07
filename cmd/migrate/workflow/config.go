package workflow

import (
	"github.com/srz-zumix/gh-secret-kit/pkg/migrate"
)

// InitConfig holds configuration for the init operation
type InitConfig struct {
	Source       string
	WorkflowName string
	Branch       string
	Label        string
}

// CreateConfig holds configuration for the create operation
type CreateConfig struct {
	Source                 string
	Destination            string
	DestinationHost        string
	SourceEnv              string
	DestinationEnv         string
	Secrets                []string
	Rename                 []string
	Overwrite              bool
	DestinationTokenSecret string
	Scope                  migrate.SecretScope
	RunnerLabel            string
	WorkflowName           string
	Branch                 string
	Label                  string
}

// RunConfig holds configuration for the run operation
type RunConfig struct {
	Source       string
	WorkflowName string
	Branch       string
	Label        string
	Wait         bool
	Timeout      string
	// PRNumber is an optional PR number to use directly, skipping the search.
	// Set by RunAll to avoid API race conditions between init and run.
	PRNumber     int
}

// DeleteConfig holds configuration for the delete operation
type DeleteConfig struct {
	Source       string
	WorkflowName string
	Branch       string
}

// CheckConfig holds configuration for the check operation
type CheckConfig struct {
	Source           string
	Destination      string
	DestinationHost  string
	SourceEnv        string
	DestinationEnv   string
	Secrets          []string
	Rename           []string
	DestinationToken string
	Scope            migrate.SecretScope
}

// AllConfig holds configuration for the all-in-one operation that runs
// init, create, run, check, and delete in sequence.
type AllConfig struct {
	// Common fields
	Source                 string
	Destination            string
	DestinationHost        string
	SourceEnv              string
	DestinationEnv         string
	Secrets                []string
	Rename                 []string
	Overwrite              bool
	DestinationTokenSecret string
	DestinationToken       string
	Scope                  migrate.SecretScope
	RunnerLabel            string
	WorkflowName           string
	Branch                 string
	Label                  string
	Timeout                string
}
