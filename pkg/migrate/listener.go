package migrate

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
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
		// Work around this by treating available+acquired jobs as assigned
		// so the listener will start runners, allowing the server to then
		// assign the jobs to them.
		pendingJobs := msg.Statistics.TotalAvailableJobs + msg.Statistics.TotalAcquiredJobs
		if msg.Statistics.TotalAssignedJobs == 0 && pendingJobs > 0 {
			logger.Info(fmt.Sprintf("  Adjusting TotalAssignedJobs: %d -> %d (pending jobs need runners)",
				msg.Statistics.TotalAssignedJobs, pendingJobs))
			msg.Statistics.TotalAssignedJobs = pendingJobs
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
	Client            *scaleset.Client
	ScaleSetID        int
	RunnerDir         string
	ConfigURL         string // GitHub config URL for runner registration
	RunnerLabel       string // Label to assign to runners
	RegistrationToken string // Token for runner registration via config.sh
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
		sig := <-sigCh
		logger.Info(fmt.Sprintf("Received signal %v, shutting down listener...", sig))
		cancel()
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
	sdkListener, err := listener.New(loggingClient, listener.Config{
		ScaleSetID: config.ScaleSetID,
		MaxRunners: 1,
		Logger:     slog.Default().WithGroup("listener"),
	})
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}

	// Create the scaler that handles runner lifecycle
	scaler := &migrateScaler{
		scalesetClient:    config.Client,
		scaleSetID:        config.ScaleSetID,
		runnerDir:         config.RunnerDir,
		configURL:         config.ConfigURL,
		runnerLabel:       config.RunnerLabel,
		registrationToken: config.RegistrationToken,
		runners: runnerState{
			idle: make(map[string]int),
			busy: make(map[string]int),
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

// migrateScaler implements listener.Scaler for single-job migration
type migrateScaler struct {
	scalesetClient    *scaleset.Client
	scaleSetID        int
	runnerDir         string
	configURL         string
	runnerLabel       string
	registrationToken string
	runners           runnerState
	doneCh            chan struct{}
	doneOnce          sync.Once
	completed         atomic.Bool
}

// HandleDesiredRunnerCount is called by the listener when the desired runner
// count changes. Starts runners on-demand when jobs are assigned.
func (s *migrateScaler) HandleDesiredRunnerCount(ctx context.Context, count int) (int, error) {
	currentCount := s.runners.count()
	logger.Info(fmt.Sprintf("HandleDesiredRunnerCount: desired=%d, current=%d", count, currentCount))

	// Don't start new runners after migration is done
	if s.completed.Load() {
		return currentCount, nil
	}

	// Cap at 1 runner max for migration
	targetCount := min(1, count)

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

	s.completed.Store(true)

	pid := s.runners.markDone(jobInfo.RunnerName)
	if pid > 0 {
		logger.Info(fmt.Sprintf("Stopping runner process (PID: %d)...", pid))
		_ = StopRunner(pid)
	}

	// Signal that migration is done
	s.doneOnce.Do(func() {
		close(s.doneCh)
	})

	return nil
}

// startRunner configures and starts an ephemeral runner process.
// Uses config.sh with explicit labels (for GHES compatibility) when a
// registration token is provided, otherwise falls back to JIT config.
func (s *migrateScaler) startRunner(ctx context.Context) error {
	runnerName := GenerateRunnerName()

	if s.registrationToken != "" {
		// Use config.sh with explicit labels (GHES-compatible)
		logger.Info(fmt.Sprintf("Configuring runner via config.sh: %s (label: %s)", runnerName, s.runnerLabel))
		if err := ConfigureRunner(s.runnerDir, s.configURL, s.registrationToken, runnerName, s.runnerLabel); err != nil {
			return fmt.Errorf("failed to configure runner: %w", err)
		}

		logger.Info(fmt.Sprintf("Starting ephemeral runner: %s", runnerName))
		process, err := StartRunner(s.runnerDir, "")
		if err != nil {
			return fmt.Errorf("failed to start runner: %w", err)
		}

		s.runners.addIdle(runnerName, process.Pid)
		logger.Info(fmt.Sprintf("Runner started: %s (PID: %d)", runnerName, process.Pid))
	} else {
		// Use JIT config (github.com)
		logger.Info(fmt.Sprintf("Generating JIT config for runner: %s", runnerName))
		jitConfig, err := GenerateJITConfig(ctx, s.scalesetClient, s.scaleSetID, runnerName)
		if err != nil {
			return fmt.Errorf("failed to generate JIT config: %w", err)
		}

		logger.Info(fmt.Sprintf("Starting ephemeral runner: %s", runnerName))
		process, err := StartRunner(s.runnerDir, jitConfig.EncodedJITConfig)
		if err != nil {
			return fmt.Errorf("failed to start runner: %w", err)
		}

		s.runners.addIdle(runnerName, process.Pid)
		logger.Info(fmt.Sprintf("Runner started: %s (PID: %d)", runnerName, process.Pid))
	}

	// Wait for runner to become ready before returning
	logger.Info("Waiting for runner to become ready...")
	if err := WaitForRunnerReady(ctx, s.runnerDir, RunnerStartTimeout); err != nil {
		logger.Warn(fmt.Sprintf("Runner may not be fully ready: %v", err))
	} else {
		logger.Info("Runner is ready and listening for jobs")
	}

	return nil
}

// shutdown stops all running runner processes
func (s *migrateScaler) shutdown() {
	s.runners.mu.Lock()
	defer s.runners.mu.Unlock()

	for name, pid := range s.runners.idle {
		logger.Info(fmt.Sprintf("Stopping idle runner: %s (PID: %d)", name, pid))
		_ = StopRunner(pid)
	}
	clear(s.runners.idle)

	for name, pid := range s.runners.busy {
		logger.Info(fmt.Sprintf("Stopping busy runner: %s (PID: %d)", name, pid))
		_ = StopRunner(pid)
	}
	clear(s.runners.busy)
}

// runnerState tracks runner processes by name -> PID
type runnerState struct {
	mu   sync.Mutex
	idle map[string]int // name -> PID
	busy map[string]int // name -> PID
}

func (r *runnerState) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.idle) + len(r.busy)
}

func (r *runnerState) addIdle(name string, pid int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.idle[name] = pid
}

func (r *runnerState) markBusy(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	pid, ok := r.idle[name]
	if !ok {
		return
	}
	delete(r.idle, name)
	r.busy[name] = pid
}

func (r *runnerState) markDone(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if pid, ok := r.busy[name]; ok {
		delete(r.busy, name)
		return pid
	}
	if pid, ok := r.idle[name]; ok {
		delete(r.idle, name)
		return pid
	}
	return 0
}
