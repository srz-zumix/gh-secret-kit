package workflow

import (
	"context"
	"fmt"

	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

// RunAll executes the full migration pipeline: init → create → run → check → delete.
func RunAll(ctx context.Context, config *AllConfig) error {
	initConfig := &InitConfig{
		Source:       config.Source,
		WorkflowName: config.WorkflowName,
		Branch:       config.Branch,
		Label:        config.Label,
	}
	createConfig := &CreateConfig{
		Source:                 config.Source,
		Destination:            config.Destination,
		DestinationHost:        config.DestinationHost,
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
	}
	runConfig := &RunConfig{
		Source:       config.Source,
		WorkflowName: config.WorkflowName,
		Branch:       config.Branch,
		Label:        config.Label,
		Wait:         true,
		Timeout:      config.Timeout,
	}
	checkConfig := &CheckConfig{
		Source:           config.Source,
		Destination:      config.Destination,
		DestinationHost:  config.DestinationHost,
		SourceEnv:        config.SourceEnv,
		DestinationEnv:   config.DestinationEnv,
		Secrets:          config.Secrets,
		Rename:           config.Rename,
		DestinationToken: config.DestinationToken,
		Scope:            config.Scope,
	}
	deleteConfig := &DeleteConfig{
		Source:       config.Source,
		WorkflowName: config.WorkflowName,
		Branch:       config.Branch,
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
