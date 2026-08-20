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

// DispatchConfig holds configuration for the dispatch operation. It supports two
// modes:
//
//   - Self-rewrite (Source empty): run from inside a workflow_dispatch-triggered
//     workflow. The currently running workflow is rewritten with a migration
//     workflow, pushed to a temporary branch, and re-triggered via
//     workflow_dispatch. The runs-on of the running workflow is reused.
//   - Target-specified (Source set): the workflow does not exist in the target
//     repository, so the precondition that it runs inside a workflow is skipped.
//     A syntax-error workflow is pushed first to register it, then the corrected
//     workflow is pushed and dispatched. RunnerLabel is required in this mode.
type DispatchConfig struct {
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
	// RunnerLabel overrides the runs-on value of the generated workflow. When
	// empty in self-rewrite mode, the runs-on of the currently running workflow
	// is reused. It is required in target-specified mode.
	RunnerLabel string
	// WorkflowName is the workflow file name (without extension) used in
	// target-specified mode. It is ignored in self-rewrite mode, where the
	// running workflow file name is used.
	WorkflowName string
	// Branch is the temporary dispatch branch name. When empty, a unique name
	// derived from the workflow run ID or a timestamp is used.
	Branch string
	// Wait, when true, causes RunDispatch to block until the dispatched workflow
	// run finishes (or Timeout elapses).
	Wait bool
	// Timeout is the maximum duration to wait when Wait is true (e.g. "10m").
	Timeout string
	// Unarchive, when true, temporarily unarchives the source repository if it
	// is archived, then re-archives it after the dispatch completes.
	Unarchive bool
	// SkipArchiveCheck skips the archived-repository check. Set internally by
	// RunAll to avoid a redundant API call when the check was already done.
	SkipArchiveCheck bool
}

// DumpConfig holds configuration for the (undocumented) dump operation, which
// writes repository secret values to a file on the workflow runner's
// filesystem via the same dispatch transport as DispatchConfig.
type DumpConfig struct {
	Source string
	// Output is the path (relative to the runner's working directory, or
	// absolute) of the file to write NAME=BASE64_VALUE lines to. The file is
	// truncated and recreated on every run.
	Output         string
	Secrets        []string
	ExcludeSecrets []string
	// RunnerLabel overrides the runs-on value of the generated workflow. When
	// empty in self-rewrite mode, the runs-on of the currently running workflow
	// is reused. It is required in target-specified mode.
	RunnerLabel string
	// WorkflowName is the workflow file name (without extension) used in
	// target-specified mode. It is ignored in self-rewrite mode, where the
	// running workflow file name is used.
	WorkflowName string
	// Branch is the temporary dispatch branch name. When empty, a unique name
	// derived from the workflow run ID or a timestamp is used.
	Branch string
	// Wait, when true, causes RunDump to block until the dispatched workflow
	// run finishes (or Timeout elapses).
	Wait bool
	// Timeout is the maximum duration to wait when Wait is true (e.g. "10m").
	Timeout string
	// Unarchive, when true, temporarily unarchives the source repository if it
	// is archived, then re-archives it after the dispatch completes.
	Unarchive bool
	// SkipArchiveCheck skips the archived-repository check.
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
	SkipCheck              bool
	Scope                  migrator.SecretScope
	RunnerLabel            string
	WorkflowName           string
	Branch                 string
	Label                  string
	Timeout                string
	Unarchive              bool
}
