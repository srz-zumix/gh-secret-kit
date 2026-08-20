package workflow

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

// RunDump generates an (undocumented) secret dump workflow, pushes it to a
// temporary branch, and triggers it via workflow_dispatch. The generated
// workflow writes each repository secret's value, base64-encoded, to a
// "NAME=BASE64_VALUE" line in config.Output on the runner's filesystem.
//
// It shares the dispatch transport (self-rewrite / target-specified modes,
// branch creation, workflow registration, triggering, and optional wait) with
// RunDispatch; only the generated workflow's content differs.
func RunDump(ctx context.Context, config *DumpConfig) error {
	logger.Info("Dispatching secret dump workflow")

	setup, cleanup, err := prepareDispatchSetup(ctx, config.Source, config.RunnerLabel, config.WorkflowName, config.Unarchive, config.SkipArchiveCheck)
	if err != nil {
		return err
	}
	defer cleanup()

	secrets := config.Secrets
	if len(secrets) == 0 {
		logger.Info("No specific secrets specified, fetching repo secrets from source...")
		secrets, err = fetchRepoSecrets(ctx, setup.client, setup.sourceRepo)
		if err != nil {
			return fmt.Errorf("failed to fetch secrets from source: %w", err)
		}
		logger.Info(fmt.Sprintf("Found %d secrets to dump", len(secrets)))
	}
	secrets = excludeSecrets(secrets, config.ExcludeSecrets)

	branch, err := resolveDispatchBranch(config.Branch, "gh-secret-kit-dump-dispatch")
	if err != nil {
		return err
	}

	workflowName := strings.TrimSuffix(setup.workflowFileName, path.Ext(setup.workflowFileName))
	workflowConfig := migrator.DumpWorkflowConfig{
		WorkflowName:  workflowName,
		Secrets:       secrets,
		Output:        config.Output,
		RunsOn:        setup.runsOn,
		CleanupBranch: branch,
	}
	logger.Info("Generating dump workflow YAML...")
	workflowYAML, err := migrator.GenerateDumpWorkflowYAML(workflowConfig)
	if err != nil {
		return fmt.Errorf("failed to generate workflow YAML: %w", err)
	}

	if err := createDispatchBranch(ctx, setup.client, setup.sourceRepo, branch, setup.baseSHA); err != nil {
		return err
	}

	return triggerDispatchWorkflow(ctx, setup.client, setup.sourceRepo, setup, branch, workflowName, workflowYAML, config.Wait, config.Timeout)
}
