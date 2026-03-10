package workflow

import (
	"context"
	"fmt"

	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// RunAll executes the full migration pipeline: init → create → run → check → delete.
func RunAll(ctx context.Context, config *AllConfig) error {
	// Handle unarchive at the top level to avoid repeated archive/unarchive cycles
	sourceRepo, err := parser.Repository(parser.RepositoryInput(config.Source))
	if err != nil {
		return fmt.Errorf("failed to parse source repository: %w", err)
	}
	client, err := gh.NewGitHubClientWithRepo(sourceRepo)
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}
	cleanup, err := handleUnarchiveWithCheck(ctx, client, sourceRepo, config.Unarchive)
	if err != nil {
		return err
	}
	defer cleanup()

	// Sub-configs do not need to handle unarchive since we handle it at this level
	initConfig := &InitConfig{
		Source:           config.Source,
		WorkflowName:     config.WorkflowName,
		Branch:           config.Branch,
		Label:            config.Label,
		Unarchive:        false,
		SkipArchiveCheck: true,
	}
	createConfig := &CreateConfig{
		Source:                 config.Source,
		Destination:            config.Destination,
		SourceEnv:              config.SourceEnv,
		DestinationEnv:         config.DestinationEnv,
		Secrets:                config.Secrets,
		Rename:                 config.Rename,
		Overwrite:              config.Overwrite,
		DestinationTokenSecret: config.DestinationTokenSecret,
		Scope:                  config.Scope,
		RunnerLabel:            config.RunnerLabel,
		WorkflowName:           config.WorkflowName,
		Branch:                 config.Branch,
		Label:                  config.Label,
		Unarchive:              false,
		SkipArchiveCheck:       true,
	}
	runConfig := &RunConfig{
		Source:           config.Source,
		WorkflowName:     config.WorkflowName,
		Branch:           config.Branch,
		Label:            config.Label,
		Wait:             true,
		Timeout:          config.Timeout,
		Unarchive:        false,
		SkipArchiveCheck: true,
	}
	checkConfig := &CheckConfig{
		Source:           config.Source,
		Destination:      config.Destination,
		SourceEnv:        config.SourceEnv,
		DestinationEnv:   config.DestinationEnv,
		Secrets:          config.Secrets,
		Rename:           config.Rename,
		DestinationToken: config.DestinationToken,
		Scope:            config.Scope,
	}
	deleteConfig := &DeleteConfig{
		Source:           config.Source,
		WorkflowName:     config.WorkflowName,
		Branch:           config.Branch,
		Unarchive:        false,
		SkipArchiveCheck: true,
	}

	// Step 1: init
	logger.Info("Step 1/5: Initializing migration workflow...")
	prNumber, err := RunInit(ctx, initConfig)
	if err != nil {
		return fmt.Errorf("init failed: %w", err)
	}

	// Step 2: create
	logger.Info("Step 2/5: Creating migration workflow...")
	if err := RunCreate(ctx, createConfig); err != nil {
		return fmt.Errorf("create failed: %w", err)
	}

	// Step 3: run (always wait)
	// Pass the PR number from init to avoid API race conditions on GHES
	runConfig.PRNumber = prNumber
	logger.Info("Step 3/5: Running migration workflow...")
	if err := RunWorkflow(ctx, runConfig); err != nil {
		return fmt.Errorf("run failed: %w", err)
	}

	// Step 4: check
	logger.Info("Step 4/5: Checking migration results...")
	if err := RunCheck(ctx, checkConfig); err != nil {
		return fmt.Errorf("check failed: %w", err)
	}

	// Step 5: delete
	logger.Info("Step 5/5: Cleaning up migration resources...")
	if err := RunDelete(ctx, deleteConfig); err != nil {
		return fmt.Errorf("delete failed: %w", err)
	}

	logger.Info("Migration completed successfully!")
	return nil
}
