package workflow

import (
	"time"

	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
)

// InitConfig holds configuration for the init operation
type InitConfig struct {
	Source           string
	WorkflowName     string
	Branch           string
	Label            string
	Unarchive        bool
	SkipArchiveCheck bool
}

// CreateConfig holds configuration for the create operation
type CreateConfig struct {
	Source                 string
	Destination            string
	SourceEnv              string
	DestinationEnv         string
	Secrets                []string
	ExcludeSecrets         []string
	Rename                 []string
	Overwrite              bool
	DestinationTokenSecret string
	Scope                  migrator.SecretScope
	RunnerLabel            string
	WorkflowName           string
	Branch                 string
	Label                  string
	Unarchive              bool
	SkipArchiveCheck       bool
}

// RunConfig holds configuration for the run operation
type RunConfig struct {
	Source           string
	WorkflowName     string
	Branch           string
	Label            string
	Wait             bool
	Timeout          string
	Unarchive        bool
	SkipArchiveCheck bool
	// PRNumber is an optional PR number to use directly, skipping the search.
	// Set by RunAll to avoid API race conditions between init and run.
	PRNumber int
	// InitialWait, when non-zero, adds a fixed sleep before the first trigger
	// label addition. Set by RunAll to give GitHub Actions extra time after the
	// create step's file push before the label fires the workflow.
	InitialWait time.Duration
	// LabelRetries is the number of additional label-trigger attempts to make
	// when no workflow run is queued within the queue-detection window.
	// Set by RunAll; 0 means no retry (standalone run command).
	LabelRetries int
}

// DeleteConfig holds configuration for the delete operation
type DeleteConfig struct {
	Source           string
	WorkflowName     string
	Branch           string
	Unarchive        bool
	SkipArchiveCheck bool
}

// CheckConfig holds configuration for the check operation
type CheckConfig struct {
	Source           string
	Destination      string
	SourceEnv        string
	DestinationEnv   string
	Secrets          []string
	Rename           []string
	DestinationToken string
	Scope            migrator.SecretScope
}

// AllConfig holds configuration for the all-in-one operation that runs
// init, create, run, check, and delete in sequence.
type AllConfig struct {
	// Common fields
	Source                 string
	Destination            string
	SourceEnv              string
	DestinationEnv         string
	Secrets                []string
	ExcludeSecrets         []string
	Rename                 []string
	Overwrite              bool
	DestinationTokenSecret string
	DestinationToken       string
	Scope                  migrator.SecretScope
	RunnerLabel            string
	WorkflowName           string
	Branch                 string
	Label                  string
	Timeout                string
	Unarchive              bool
}
