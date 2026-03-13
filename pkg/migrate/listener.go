package migrate

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/actions/scaleset"
	"github.com/actions/scaleset/listener"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

// loggingSessionClient wraps a MessageSessionClient to log message details
type loggingSessionClient struct {
	inner *scaleset.MessageSessionClient
}

func (c *loggingSessionClient) GetMessage(ctx context.Context, lastMessageID, maxCapacity int) (*scaleset.RunnerScaleSetMessage, error) {
	msg, err := c.inner.GetMessage(ctx, lastMessageID, maxCapacity)
	if err != nil {
		return nil, err
	}
	if msg == nil {
		logger.Debug("GetMessage: received nil message (long-poll timeout)")
		return nil, nil
	}
	logger.Info(fmt.Sprintf("GetMessage: messageID=%d, assigned=%d, completed=%d, started=%d",
		msg.MessageID,
		len(msg.JobAssignedMessages),
		len(msg.JobCompletedMessages),
		len(msg.JobStartedMessages),
	))
	if msg.Statistics != nil {
		logger.Info(fmt.Sprintf("  Statistics: available=%d, acquired=%d, assigned=%d, running=%d, registered=%d, idle=%d, busy=%d",
			msg.Statistics.TotalAvailableJobs,
			msg.Statistics.TotalAcquiredJobs,
			msg.Statistics.TotalAssignedJobs,
			msg.Statistics.TotalRunningJobs,
			msg.Statistics.TotalRegisteredRunners,
			msg.Statistics.TotalIdleRunners,
			msg.Statistics.TotalBusyRunners,
		))

		// On GHES, the server may not assign jobs to the scale set until
		// runners are actually registered. The SDK listener uses only
		// TotalAssignedJobs to call HandleDesiredRunnerCount, which stays 0
		// and never triggers runner startup (chicken-and-egg problem).
		// Treat all pending (available+acquired) jobs as needing runners so
		// the listener proactively starts runners for every queued job,
		// including when another runner is already busy.
		pendingJobs := msg.Statistics.TotalAvailableJobs + msg.Statistics.TotalAcquiredJobs
		if pendingJobs > 0 {
			newAssigned := msg.Statistics.TotalAssignedJobs + pendingJobs
			logger.Info(fmt.Sprintf("  Adjusting TotalAssignedJobs: %d -> %d (pending jobs need runners)",
				msg.Statistics.TotalAssignedJobs, newAssigned))
			msg.Statistics.TotalAssignedJobs = newAssigned
		}
	}
	return msg, nil
}

func (c *loggingSessionClient) DeleteMessage(ctx context.Context, messageID int) error {
	return c.inner.DeleteMessage(ctx, messageID)
}

func (c *loggingSessionClient) Session() scaleset.RunnerScaleSetSession {
	return c.inner.Session()
}

// ListenerConfig holds configuration for the message session listener
type ListenerConfig struct {
	Client       *scaleset.Client
	ScaleSetID   int
	RunnerDir    string
	ConfigURL    string // GitHub config URL for runner registration
	RunnerLabel  string // Label to assign to runners
	// TokenRefresher, when non-nil, is called before each config.sh invocation to
	// obtain a fresh one-time-use registration token. Required for GHES because
	// registration tokens are invalidated after the first use.
	TokenRefresher func(ctx context.Context) (string, error)
	MaxRunners     int // Maximum number of concurrent runners (default: 1)
}

// RunListenerLoop runs the scaleset message session listener loop using the
// SDK's listener package. It creates a message session, starts the listener,
// and uses a Scaler to handle job lifecycle events. Runners are started
// on-demand when HandleDesiredRunnerCount is called with count > 0.
// This function blocks until the context is canceled or a termination signal
// is received. After each job completes, it restarts the session automatically
// to handle subsequent jobs.
func RunListenerLoop(ctx context.Context, config *ListenerConfig) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Handle OS signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case sig := <-sigCh:
			logger.Info(fmt.Sprintf("Received signal %v, shutting down listener...", sig))
			cancel()
		case <-ctx.Done():
			// Context canceled, stop waiting for signals
		}
	}()

	// Use hostname as session owner (per SDK convention)
	hostname, err := os.Hostname()
	if err != nil {
		hostname = GenerateRunnerName()
		logger.Warn(fmt.Sprintf("Failed to get hostname, using generated name: %s", hostname))
	}

	for {
		if ctx.Err() != nil {
			return nil
		}
		if err := runListenerOnce(ctx, config, hostname); err != nil {
			if ctx.Err() != nil {
				return nil // Graceful shutdown
			}
			return err
		}
		logger.Info("Job completed. Ready for next job.")
	}
}

// runListenerOnce creates a fresh message session and runs the listener until
// one job completes or the context is canceled.
func runListenerOnce(ctx context.Context, config *ListenerConfig, hostname string) error {
	jobCtx, jobCancel := context.WithCancel(ctx)
	defer jobCancel()

	// Create message session client
	logger.Info("Creating message session...")
	sessionClient, err := config.Client.MessageSessionClient(jobCtx, config.ScaleSetID, hostname)
	if err != nil {
		return fmt.Errorf("failed to create message session: %w", err)
	}
	defer func() {
		logger.Info("Closing message session...")
		closeCtx := context.Background()
		if closeErr := sessionClient.Close(closeCtx); closeErr != nil {
			logger.Warn(fmt.Sprintf("Failed to close message session: %v", closeErr))
		}
	}()

	// Log initial session statistics
	initSession := sessionClient.Session()
	if initSession.Statistics != nil {
		logger.Info(fmt.Sprintf("Initial session stats: available=%d, acquired=%d, assigned=%d, running=%d, registered=%d, idle=%d, busy=%d",
			initSession.Statistics.TotalAvailableJobs,
			initSession.Statistics.TotalAcquiredJobs,
			initSession.Statistics.TotalAssignedJobs,
			initSession.Statistics.TotalRunningJobs,
			initSession.Statistics.TotalRegisteredRunners,
			initSession.Statistics.TotalIdleRunners,
			initSession.Statistics.TotalBusyRunners,
		))
	}

	// Wrap session client with logging
	loggingClient := &loggingSessionClient{inner: sessionClient}

	// Create the SDK listener
	logger.Info("Initializing listener...")
	maxRunners := config.MaxRunners
	if maxRunners <= 0 {
		maxRunners = 1
	}
	sdkListener, err := listener.New(loggingClient, listener.Config{
		ScaleSetID: config.ScaleSetID,
		MaxRunners: maxRunners,
		Logger:     slog.Default().WithGroup("listener"),
	})
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}

	// Create the scaler that handles runner lifecycle
	scaler := &migrateScaler{
		scalesetClient: config.Client,
		scaleSetID:     config.ScaleSetID,
		runnerDir:      config.RunnerDir,
		configURL:      config.ConfigURL,
		runnerLabel:    config.RunnerLabel,
		tokenRefresher: config.TokenRefresher,
		maxRunners:     maxRunners,
		runners: runnerState{
			idle: make(map[string]*exec.Cmd),
			busy: make(map[string]*exec.Cmd),
		},
		doneCh: make(chan struct{}),
	}
	defer scaler.shutdown()

	logger.Info("Listener is ready. Waiting for job assignments...")
	logger.Info("(Dispatch the workflow from another terminal)")

	// Run listener in a goroutine so we can wait for job completion
	errCh := make(chan error, 1)
	go func() {
		errCh <- sdkListener.Run(jobCtx, scaler)
	}()

	// Wait for either job completion or listener error
	select {
	case <-scaler.doneCh:
		logger.Info("Migration job completed.")
		jobCancel() // Stop the listener goroutine
		<-errCh     // Wait for listener to exit cleanly
		return nil
	case err := <-errCh:
		if jobCtx.Err() != nil {
			return nil // Graceful shutdown
		}
		return fmt.Errorf("listener failed: %w", err)
	}
}

// migrateScaler implements listener.Scaler for migration.
// It supports concurrent jobs up to maxRunners.
type migrateScaler struct {
	scalesetClient *scaleset.Client
	scaleSetID     int
	runnerDir      string
	configURL      string
	runnerLabel    string
	tokenRefresher func(ctx context.Context) (string, error)
	maxRunners     int
	runners           runnerState
	runnerWg          sync.WaitGroup // tracks all watcher goroutines for clean shutdown
	doneCh            chan struct{}
	doneOnce          sync.Once
}

// HandleDesiredRunnerCount is called by the listener when the desired runner
// count changes. Starts runners on-demand when jobs are assigned.
func (s *migrateScaler) HandleDesiredRunnerCount(ctx context.Context, count int) (int, error) {
	currentCount := s.runners.count()
	logger.Info(fmt.Sprintf("HandleDesiredRunnerCount: desired=%d, current=%d", count, currentCount))

	// Cap at maxRunners
	targetCount := min(s.maxRunners, count)

	if targetCount <= currentCount {
		return currentCount, nil
	}

	// Scale up: start a new JIT runner
	scaleUp := targetCount - currentCount
	logger.Info(fmt.Sprintf("Scaling up: current=%d, target=%d, starting=%d runners",
		currentCount, targetCount, scaleUp))

	for range scaleUp {
		if err := s.startRunner(ctx); err != nil {
			return s.runners.count(), fmt.Errorf("failed to start runner: %w", err)
		}
	}

	return s.runners.count(), nil
}

// HandleJobStarted is called when a job starts on a runner
func (s *migrateScaler) HandleJobStarted(ctx context.Context, jobInfo *scaleset.JobStarted) error {
	logger.Info(fmt.Sprintf("Job started: %s (runner: %s, runnerID: %d)",
		jobInfo.JobDisplayName, jobInfo.RunnerName, jobInfo.RunnerID))
	s.runners.markBusy(jobInfo.RunnerName)
	return nil
}

// HandleJobCompleted is called when a job completes on a runner
func (s *migrateScaler) HandleJobCompleted(ctx context.Context, jobInfo *scaleset.JobCompleted) error {
	logger.Info(fmt.Sprintf("Job completed: %s (result: %s, runner: %s)",
		jobInfo.JobDisplayName, jobInfo.Result, jobInfo.RunnerName))

	cmd := s.runners.markDone(jobInfo.RunnerName)
	if cmd != nil && cmd.Process != nil {
		logger.Info(fmt.Sprintf("Stopping runner process (PID: %d)...", cmd.Process.Pid))
		_ = StopRunner(cmd.Process.Pid)
		// cmd.Wait() is owned by the watcher goroutine; do not call it here.
	}

	// Signal done only when all concurrent runners have finished.
	if s.runners.count() == 0 {
		s.doneOnce.Do(func() {
			close(s.doneCh)
		})
	}

	return nil
}

// startRunner configures and starts an ephemeral runner process.
// Uses config.sh with explicit labels (for GHES compatibility) when a
// TokenRefresher is provided, otherwise falls back to JIT config.
func (s *migrateScaler) startRunner(ctx context.Context) error {
	runnerName := GenerateRunnerName()

	if s.tokenRefresher != nil {
		// Use config.sh with explicit labels (GHES-compatible).
		// Fetch a fresh one-time-use registration token for each runner.
		token, err := s.tokenRefresher(ctx)
		if err != nil {
			return fmt.Errorf("failed to obtain registration token: %w", err)
		}

		// Create a per-runner instance directory as a hard-linked copy of the
		// shared runner binary template. config.sh writes .runner/.credentials
		// into the directory where config.sh lives, so each concurrent runner
		// must have its own isolated directory to avoid overwriting each other's
		// configuration files.
		// Instance dirs live under RunnerInstancesBaseDir (a sibling of runnerDir)
		// so that WalkDir in CreateRunnerInstanceDir does not recurse into them.
		instanceDir := filepath.Join(RunnerInstancesBaseDir(s.runnerDir), runnerName)
		logger.Info(fmt.Sprintf("Creating runner instance directory: %s", instanceDir))
		if err := CreateRunnerInstanceDir(s.runnerDir, instanceDir); err != nil {
			return fmt.Errorf("failed to create runner instance directory: %w", err)
		}

		logger.Info(fmt.Sprintf("Configuring runner via config.sh: %s (label: %s)", runnerName, s.runnerLabel))
		if err := ConfigureRunner(instanceDir, instanceDir, s.configURL, token, runnerName, s.runnerLabel); err != nil {
			return fmt.Errorf("failed to configure runner: %w", err)
		}

		logger.Info(fmt.Sprintf("Starting ephemeral runner: %s", runnerName))
		logPath := filepath.Join(instanceDir, runnerName+".log")
		cmd, err := StartRunner(instanceDir, instanceDir, "", logPath)
		if err != nil {
			return fmt.Errorf("failed to start runner: %w", err)
		}

		s.runners.addIdle(runnerName, cmd)
		logger.Info(fmt.Sprintf("Runner started: %s (PID: %d, log: %s)", runnerName, cmd.Process.Pid, logPath))
		// Watch for unexpected process exit and remove from state so the next
		// HandleDesiredRunnerCount call correctly reflects the actual runner count.
		// Also wait for the runner to become ready and log when it is.
		s.runnerWg.Add(1)
		go func() {
			defer s.runnerWg.Done()
			logger.Info(fmt.Sprintf("Waiting for runner to become ready: %s", runnerName))
			if err := WaitForRunnerReady(ctx, logPath, RunnerStartTimeout); err != nil {
				logger.Warn(fmt.Sprintf("Runner may not be fully ready (%s): %v", runnerName, err))
			} else {
				logger.Info(fmt.Sprintf("Runner is ready and listening for jobs: %s", runnerName))
			}
			_ = cmd.Wait()
			logger.Info(fmt.Sprintf("Runner process exited: %s", runnerName))
			s.runners.remove(runnerName)
		}()
	} else {
		// Use JIT config (github.com)
		logger.Info(fmt.Sprintf("Generating JIT config for runner: %s", runnerName))
		jitConfig, err := GenerateJITConfig(ctx, s.scalesetClient, s.scaleSetID, runnerName)
		if err != nil {
			return fmt.Errorf("failed to generate JIT config: %w", err)
		}

		logger.Info(fmt.Sprintf("Starting ephemeral runner: %s", runnerName))
		logPath := filepath.Join(s.runnerDir, runnerName+".log")
		cmd, err := StartRunner(s.runnerDir, "", jitConfig.EncodedJITConfig, logPath)
		if err != nil {
			return fmt.Errorf("failed to start runner: %w", err)
		}

		s.runners.addIdle(runnerName, cmd)
		logger.Info(fmt.Sprintf("Runner started: %s (PID: %d, log: %s)", runnerName, cmd.Process.Pid, logPath))
		// Watch for unexpected process exit and remove from state so the next
		// HandleDesiredRunnerCount call correctly reflects the actual runner count.
		// Also wait for the runner to become ready and log when it is.
		s.runnerWg.Add(1)
		go func() {
			defer s.runnerWg.Done()
			logger.Info(fmt.Sprintf("Waiting for runner to become ready: %s", runnerName))
			if err := WaitForRunnerReady(ctx, logPath, RunnerStartTimeout); err != nil {
				logger.Warn(fmt.Sprintf("Runner may not be fully ready (%s): %v", runnerName, err))
			} else {
				logger.Info(fmt.Sprintf("Runner is ready and listening for jobs: %s", runnerName))
			}
			_ = cmd.Wait()
			logger.Info(fmt.Sprintf("Runner process exited: %s", runnerName))
			s.runners.remove(runnerName)
		}()
	}

	return nil
}

// shutdown stops all running runner processes and waits for them to exit.
func (s *migrateScaler) shutdown() {
	s.runners.mu.Lock()
	for name, cmd := range s.runners.idle {
		logger.Info(fmt.Sprintf("Stopping idle runner: %s (PID: %d)", name, cmd.Process.Pid))
		_ = StopRunner(cmd.Process.Pid)
	}
	clear(s.runners.idle)

	for name, cmd := range s.runners.busy {
		logger.Info(fmt.Sprintf("Stopping busy runner: %s (PID: %d)", name, cmd.Process.Pid))
		_ = StopRunner(cmd.Process.Pid)
	}
	clear(s.runners.busy)
	s.runners.mu.Unlock()

	// Wait for all watcher goroutines (which own cmd.Wait()) to finish.
	s.runnerWg.Wait()
}

// runnerState tracks runner processes by name -> *exec.Cmd
type runnerState struct {
	mu   sync.Mutex
	idle map[string]*exec.Cmd // name -> Cmd
	busy map[string]*exec.Cmd // name -> Cmd
}

func (r *runnerState) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.idle) + len(r.busy)
}

func (r *runnerState) addIdle(name string, cmd *exec.Cmd) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.idle[name] = cmd
}

func (r *runnerState) markBusy(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cmd, ok := r.idle[name]; ok {
		delete(r.idle, name)
		r.busy[name] = cmd
	}
}

func (r *runnerState) markDone(name string) *exec.Cmd {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cmd, ok := r.busy[name]; ok {
		delete(r.busy, name)
		return cmd
	}
	if cmd, ok := r.idle[name]; ok {
		delete(r.idle, name)
		return cmd
	}
	return nil
}

// remove removes the runner from state without stopping it.
// Called by the process watcher goroutine when the process exits on its own.
func (r *runnerState) remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.idle, name)
	delete(r.busy, name)
}
