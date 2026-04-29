package runner

import (
	"context"
	"fmt"

	"github.com/actions/scaleset"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

// RunListenerForState starts the foreground message session listener using
// values from the saved migration state. It is shared by runner setup and
// runner restart.
func RunListenerForState(ctx context.Context, sourceRepo repository.Repository, scalesetClient *scaleset.Client, state *migrator.MigrateState, maxRunners int) error {
	if state.ScaleSetID <= 0 {
		return fmt.Errorf("state does not contain a runner scale set ID")
	}
	configURL := state.ConfigURL
	if configURL == "" {
		configURL = migrator.BuildGitHubConfigURL(sourceRepo)
	}
	runnerDir := state.RunnerDir
	if runnerDir == "" {
		var err error
		runnerDir, err = migrator.RunnerDirPathForCwd()
		if err != nil {
			return fmt.Errorf("failed to determine runner directory: %w", err)
		}
	}

	logger.Info("Detecting runner binary for current platform...")
	binaryInfo, err := migrator.DetectRunnerBinary("")
	if err != nil {
		return fmt.Errorf("failed to detect runner binary: %w", err)
	}
	logger.Info(fmt.Sprintf("Ensuring runner binary: %s", binaryInfo.Filename))
	if err := migrator.DownloadRunnerBinary(ctx, binaryInfo.URL, runnerDir); err != nil {
		return fmt.Errorf("failed to download runner binary: %w", err)
	}

	migrator.SetScaleSetSystemInfo(scalesetClient, state.ScaleSetID)

	tokenRefresher, err := registrationTokenRefresher(ctx, sourceRepo)
	if err != nil {
		return err
	}

	logger.Info("Starting message session listener (foreground)...")
	logger.Info("Dispatch the workflow from another terminal, then the listener will")
	logger.Info("automatically start an ephemeral runner when a job is assigned.")
	logger.Info("The listener will keep running after job completion, ready for subsequent runs.")
	logger.Info("Press Ctrl+C to stop the listener.")
	logger.Info("")

	listenerConfig := &migrator.ListenerConfig{
		Client:         scalesetClient,
		ScaleSetID:     state.ScaleSetID,
		RunnerDir:      runnerDir,
		ConfigURL:      configURL,
		RunnerLabel:    state.RunnerLabel,
		RunnerGroup:    state.RunnerGroupName,
		TokenRefresher: tokenRefresher,
		MaxRunners:     maxRunners,
	}
	listenerErr := migrator.RunListenerLoop(ctx, listenerConfig)

	logger.Info("")
	if listenerErr == nil {
		logger.Info("Listener stopped.")
	} else if ctx.Err() != nil {
		logger.Info("Listener was interrupted.")
	} else {
		logger.Warn(fmt.Sprintf("Listener exited with error: %v", listenerErr))
	}
	logger.Info("To clean up resources, run:")
	logger.Info("  gh secret-kit migrate runner teardown")

	return listenerErr
}

func registrationTokenRefresher(ctx context.Context, sourceRepo repository.Repository) (func(ctx context.Context) (string, error), error) {
	client, err := gh.NewGitHubClientWithRepo(sourceRepo)
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}

	logger.Info("Verifying registration token availability...")
	_, err = gh.CreateRegistrationToken(ctx, client, sourceRepo)
	if err != nil {
		logger.Warn(fmt.Sprintf("Failed to obtain registration token (will use JIT config): %v", err))
		return nil, nil
	}

	logger.Info("Registration token available; using config.sh mode for runners")
	return func(ctx context.Context) (string, error) {
		token, err := gh.CreateRegistrationToken(ctx, client, sourceRepo)
		if err != nil {
			return "", fmt.Errorf("failed to create registration token: %w", err)
		}
		return token.GetToken(), nil
	}, nil
}
