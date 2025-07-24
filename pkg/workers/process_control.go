package workers

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/core-tools/hsu-master/pkg/errors"
	"github.com/core-tools/hsu-master/pkg/logging"
	"github.com/core-tools/hsu-master/pkg/monitoring"
	"github.com/core-tools/hsu-master/pkg/process"
	"github.com/core-tools/hsu-master/pkg/processfile"
)

type SystemProcessControlConfig struct {
	// Basic control
	CanTerminate bool `yaml:"can_terminate,omitempty"` // Can send SIGTERM/SIGKILL
	CanRestart   bool `yaml:"can_restart,omitempty"`   // Can restart (via service manager)

	// Service manager integration
	ServiceManager string `yaml:"service_manager,omitempty"` // "systemd", "windows", "launchd"
	ServiceName    string `yaml:"service_name,omitempty"`    // Service name for restart

	// Process signals
	AllowedSignals []os.Signal `yaml:"allowed_signals,omitempty"` // Allowed signals to send

	// Graceful shutdown
	GracefulTimeout time.Duration `yaml:"graceful_timeout,omitempty"` // Time to wait for graceful shutdown
}

type ManagedProcessControlConfig struct {
	// Process execution
	Execution process.ExecutionConfig `yaml:"execution"`

	// PID file configuration (optional)
	ProcessFile *processfile.ProcessFileConfig `yaml:"process_file,omitempty"`

	// Process restart
	Restart monitoring.RestartConfig `yaml:"restart"`

	// Resource management
	Limits process.ResourceLimits `yaml:"limits,omitempty"`

	/*
		// I/O handling
		LogConfig      LogConfig
		StdoutRedirect string
		StderrRedirect string

		// Scheduling
		Schedule ScheduleConfig
	*/

	// Graceful shutdown
	GracefulTimeout time.Duration `yaml:"graceful_timeout,omitempty"` // Time to wait for graceful shutdown
}

type ProcessControl interface {
	Process() *os.Process // Simplified to only return process
	HealthMonitor() monitoring.HealthMonitor
	Stdout() io.ReadCloser
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Restart(ctx context.Context) error
}

// AttachCmd represents a command that attaches to an existing process
type AttachCmd func(ctx context.Context) (*os.Process, io.ReadCloser, *monitoring.HealthCheckConfig, error)

// ExecuteCmd represents a command that executes a new process
type ExecuteCmd func(ctx context.Context) (*os.Process, io.ReadCloser, *monitoring.HealthCheckConfig, error)

type ProcessControlOptions struct {
	// Basic control
	CanAttach    bool // Can attach to existing process
	CanTerminate bool // Can send SIGTERM/SIGKILL
	CanRestart   bool // Can restart

	// Process signals
	AllowedSignals []os.Signal // Allowed signals to send

	// Graceful shutdown
	GracefulTimeout time.Duration // Time to wait for graceful shutdown

	// Process start
	ExecuteCmd ExecuteCmd // Execute command, nil if not executable
	AttachCmd  AttachCmd  // Attach command, nil if not attachable

	// Resource management
	Limits *process.ResourceLimits // nil if not limitable

	// Process restart
	Restart *monitoring.RestartConfig // nil if not restartable

	// Health check override
	HealthCheck *monitoring.HealthCheckConfig // nil if not health checkable or if ExecuteCmd/AttachCmd are provided
}

type processControl struct {
	config        ProcessControlOptions
	process       *os.Process
	stdout        io.ReadCloser
	healthMonitor monitoring.HealthMonitor
	logger        logging.Logger
	workerID      string

	// Persistent restart tracking (survives health monitor recreation)
	restartAttempts    int        // Track attempts across health monitor restarts
	lastRestartTime    time.Time  // Track timing for retry delay and backoff
	circuitBreakerOpen bool       // Circuit breaker to stop excessive restarts
	mutex              sync.Mutex // Protect restart state
}

func NewProcessControl(config ProcessControlOptions, workerID string, logger logging.Logger) ProcessControl {
	return &processControl{
		config:   config,
		logger:   logger,
		workerID: workerID,
	}
}

func (pc *processControl) Process() *os.Process {
	return pc.process // Simplified to only return process
}

func (pc *processControl) HealthMonitor() monitoring.HealthMonitor {
	return pc.healthMonitor
}

func (pc *processControl) Stdout() io.ReadCloser {
	return pc.stdout
}

func (pc *processControl) Start(ctx context.Context) error {
	// Validate context
	if ctx == nil {
		return errors.NewValidationError("context cannot be nil", nil)
	}

	return pc.startInternal(ctx)
}

func (pc *processControl) Stop(ctx context.Context) error {
	// Validate context
	if ctx == nil {
		return errors.NewValidationError("context cannot be nil", nil)
	}

	if pc.process == nil {
		return errors.NewProcessError("process not attached", nil)
	}

	return pc.stopInternal(ctx, false)
}

func (pc *processControl) Restart(ctx context.Context) error {
	// Validate context
	if ctx == nil {
		return errors.NewValidationError("context cannot be nil", nil)
	}

	if pc.process == nil {
		return errors.NewProcessError("process not attached", nil)
	}

	return pc.restartInternal(ctx)
}

func (pc *processControl) startInternal(ctx context.Context) error {
	pc.logger.Infof("Starting process control for worker %s", pc.workerID)

	process, stdout, healthCheckConfig, err := pc.startProcess(ctx)
	if err != nil {
		return errors.NewInternalError("failed to start process", err)
	}

	pc.process = process
	pc.stdout = stdout

	healthMonitor, err := pc.startHealthCheck(process.Pid, healthCheckConfig)
	if err != nil {
		pc.logger.Warnf("Failed to start health monitor, worker: %s, error: %v", pc.workerID, err)
		// ignore health monitor error
	}

	pc.healthMonitor = healthMonitor

	pc.logger.Infof("Process control started, worker: %s", pc.workerID)

	return nil
}

func (pc *processControl) startProcess(ctx context.Context) (*os.Process, io.ReadCloser, *monitoring.HealthCheckConfig, error) {
	pc.logger.Infof("Starting process for worker %s, can_attach: %t, can_execute: %t, can_terminate: %t",
		pc.workerID, pc.config.CanAttach, (pc.config.ExecuteCmd != nil), pc.config.CanTerminate)

	var process *os.Process
	var stdout io.ReadCloser
	var err error

	healthCheckConfig := pc.config.HealthCheck

	executeCmd := pc.config.ExecuteCmd
	attachCmd := pc.config.AttachCmd

	if pc.config.CanAttach && attachCmd != nil {
		pc.logger.Infof("Attempting to attach to existing process, worker: %s", pc.workerID)

		process, stdout, healthCheckConfig, err = attachCmd(ctx) // Overrides healthCheckConfig
		if err == nil {
			pc.logger.Infof("Successfully attached to existing process, worker: %s, PID: %d", pc.workerID, process.Pid)
			executeCmd = nil // attached successfully, no need to execute cmd
		} else {
			pc.logger.Warnf("Failed to attach to existing process, worker: %s, error: %v", pc.workerID, err)
		}
	}

	if executeCmd != nil { // can't attach or not attached
		pc.logger.Infof("Executing new process, worker: %s", pc.workerID)

		process, stdout, healthCheckConfig, err = executeCmd(ctx) // Overrides healthCheckConfig
		if err != nil {
			return nil, nil, nil, errors.NewProcessError("failed to start process", err)
		}

		pc.logger.Infof("New process started successfully, worker: %s, PID: %d", pc.workerID, process.Pid)
	}

	// Check if context was cancelled during startup
	if ctx.Err() != nil {
		pc.logger.Infof("Context cancelled during startup, cleaning up...")
		if process != nil {
			process.Kill()
		}
		return nil, nil, nil, errors.NewCancelledError("startup cancelled", ctx.Err())
	}

	// Validate that we have a process
	if process == nil {
		return nil, nil, nil, errors.NewInternalError("no process available after startup", nil)
	}

	pc.logger.Infof("Process started, process: %+v, stdout: %+v", process, stdout) // Removed state logging

	return process, stdout, healthCheckConfig, nil
}

func (pc *processControl) startHealthCheck(pid int, healthCheckConfig *monitoring.HealthCheckConfig) (monitoring.HealthMonitor, error) {
	pc.logger.Infof("Starting health monitor for worker %s, config: %+v", pc.workerID, healthCheckConfig)

	if healthCheckConfig == nil {
		pc.logger.Errorf("Health check configuration is nil, worker: %s", pc.workerID)
		return nil, errors.NewValidationError("health check configuration is nil", nil).WithContext("id", pc.workerID)
	}

	var healthMonitor monitoring.HealthMonitor
	var err error

	// Create process info for health monitoring - simplified
	processInfo := &monitoring.ProcessInfo{
		PID: pid, // Only PID needed
	}

	// Create health monitor with appropriate capabilities
	if healthCheckConfig.Type == monitoring.HealthCheckTypeProcess {
		if pc.config.Restart != nil && pc.config.CanRestart {
			// Create health monitor with restart capability
			healthMonitor, err = monitoring.NewHealthMonitorWithRestart(healthCheckConfig,
				pc.workerID, processInfo, pc.config.Restart, pc.logger)
		} else {
			// Create health monitor with process info but no restart
			healthMonitor, err = monitoring.NewHealthMonitorWithProcessInfo(healthCheckConfig,
				pc.workerID, processInfo, pc.logger)
		}
	} else {
		// For other health check types, create standard monitor
		healthMonitor, err = monitoring.NewHealthMonitor(healthCheckConfig, pc.workerID, pc.logger)
	}

	if err != nil {
		pc.logger.Errorf("Failed to create health monitor, id: %s, error: %v", pc.workerID, err)
		return nil, errors.NewInternalError("failed to create health monitor", err).WithContext("id", pc.workerID)
	}

	// Set up restart callback if restart is enabled
	if pc.config.Restart != nil && pc.config.CanRestart {
		healthMonitor.SetRestartCallback(func(reason string) error {
			pc.logger.Warnf("Health monitor requesting restart, worker: %s, reason: %s", pc.workerID, reason)

			pc.mutex.Lock()
			defer pc.mutex.Unlock()

			// Check circuit breaker
			if pc.circuitBreakerOpen {
				pc.logger.Errorf("Circuit breaker is open, ignoring restart request, worker: %s, attempts: %d",
					pc.workerID, pc.restartAttempts)
				return fmt.Errorf("restart circuit breaker is open")
			}

			// Check max retries
			if pc.config.Restart.MaxRetries > 0 && pc.restartAttempts >= pc.config.Restart.MaxRetries {
				pc.logger.Errorf("Max restart retries exceeded, opening circuit breaker, worker: %s, attempts: %d, max: %d",
					pc.workerID, pc.restartAttempts, pc.config.Restart.MaxRetries)
				pc.circuitBreakerOpen = true
				return fmt.Errorf("max restart retries exceeded: %d", pc.restartAttempts)
			}

			// Calculate retry delay with exponential backoff
			now := time.Now()
			timeSinceLastRestart := now.Sub(pc.lastRestartTime)

			retryDelay := pc.config.Restart.RetryDelay
			if pc.restartAttempts > 0 {
				// Apply exponential backoff
				backoffMultiplier := 1.0
				for i := 0; i < pc.restartAttempts; i++ {
					backoffMultiplier *= pc.config.Restart.BackoffRate
				}
				retryDelay = time.Duration(float64(retryDelay) * backoffMultiplier)
			}

			// Enforce retry delay
			if timeSinceLastRestart < retryDelay {
				waitTime := retryDelay - timeSinceLastRestart
				pc.logger.Infof("Enforcing retry delay, worker: %s, attempt: %d, waiting: %v",
					pc.workerID, pc.restartAttempts+1, waitTime)

				// Release lock during sleep to prevent deadlock
				pc.mutex.Unlock()
				time.Sleep(waitTime)
				pc.mutex.Lock()

				// Re-check circuit breaker after sleep
				if pc.circuitBreakerOpen {
					return fmt.Errorf("restart circuit breaker opened during delay")
				}
			}

			// Increment attempt counter and update timestamp
			pc.restartAttempts++
			pc.lastRestartTime = time.Now()

			pc.logger.Warnf("Proceeding with restart, worker: %s, attempt: %d/%d, delay: %v",
				pc.workerID, pc.restartAttempts, pc.config.Restart.MaxRetries, retryDelay)

			// Release lock before calling restart to prevent deadlock
			pc.mutex.Unlock()
			defer func() { pc.mutex.Lock() }()

			// Use a background context for restart since this is triggered by health failure
			ctx := context.Background()
			if err := pc.restartInternal(ctx); err != nil {
				pc.logger.Errorf("Failed to restart process, worker: %s, error: %v", pc.workerID, err)
				return err
			}

			pc.logger.Infof("Process restart completed, worker: %s, attempt: %d", pc.workerID, pc.restartAttempts)
			return nil
		})

		// Set up recovery callback to reset circuit breaker when process becomes healthy
		healthMonitor.SetRecoveryCallback(func() {
			pc.logger.Infof("Health recovered, resetting circuit breaker, worker: %s", pc.workerID)
			pc.resetCircuitBreaker()
		})
	}

	healthMonitor.Start()

	pc.logger.Infof("Health monitor started, worker: %s", pc.workerID)

	return healthMonitor, nil
}

func (pc *processControl) stopInternal(ctx context.Context, idDeadPID bool) error {
	pc.logger.Infof("Stopping process control...")

	// 1. Stop health monitor first
	if pc.healthMonitor != nil {
		pc.logger.Infof("Stopping health monitor...")
		pc.healthMonitor.Stop()
		pc.healthMonitor = nil
	}

	// 2. Close stdout reader to prevent resource leaks
	if pc.stdout != nil {
		pc.logger.Infof("Closing stdout reader...")
		if err := pc.stdout.Close(); err != nil {
			pc.logger.Warnf("Failed to close stdout: %v", err)
		}
		pc.stdout = nil
	}

	// 3. Terminate the process gracefully with context
	if err := pc.terminateProcess(ctx, idDeadPID); err != nil {
		pc.logger.Errorf("Failed to terminate process: %v", err)
		return errors.NewProcessError("failed to terminate process", err)
	}

	// 4. Clean up process references
	pc.process = nil

	pc.logger.Infof("Process control stopped successfully")
	return nil
}

// restartInternal performs the actual restart without additional validation
func (pc *processControl) restartInternal(ctx context.Context) error {
	pc.logger.Infof("Restarting process control...")

	// 1. Stop the current process
	if err := pc.stopInternal(ctx, true); err != nil {
		pc.logger.Errorf("Failed to stop process during restart: %v", err)
		return fmt.Errorf("failed to stop process during restart: %v", err)
	}

	// 2. Start the process again
	if err := pc.startInternal(ctx); err != nil {
		pc.logger.Errorf("Failed to start process during restart: %v", err)
		return fmt.Errorf("failed to start process during restart: %v", err)
	}

	pc.logger.Infof("Process control restarted successfully")
	return nil
}

// resetCircuitBreaker resets the circuit breaker when process runs successfully
func (pc *processControl) resetCircuitBreaker() {
	pc.mutex.Lock()
	defer pc.mutex.Unlock()

	if pc.restartAttempts > 0 || pc.circuitBreakerOpen {
		pc.logger.Infof("Resetting circuit breaker, worker: %s, previous attempts: %d",
			pc.workerID, pc.restartAttempts)
		pc.restartAttempts = 0
		pc.circuitBreakerOpen = false
		pc.lastRestartTime = time.Time{} // Reset timestamp
	}
}

// terminateProcess handles graceful process termination with timeout and context cancellation
func (pc *processControl) terminateProcess(ctx context.Context, idDeadPID bool) error {
	if pc.process == nil {
		return errors.NewProcessError("no process to terminate", nil)
	}

	pid := pc.process.Pid
	pc.logger.Infof("Terminating process PID %d", pid)

	// Determine graceful timeout
	gracefulTimeout := pc.config.GracefulTimeout
	if gracefulTimeout <= 0 {
		gracefulTimeout = 30 * time.Second // Timeout super-default
	}

	// Create a channel to signal when process exits
	done := make(chan error, 1)

	// Start a goroutine to wait for process exit
	go func() {
		state, err := pc.process.Wait()
		if err != nil {
			done <- errors.NewProcessError("process wait failed", err).WithContext("pid", pid)
		} else {
			pc.logger.Infof("Process PID %d exited with status: %v", pid, state)
			done <- nil
		}
	}()

	// Try graceful termination first
	pc.logger.Infof("Sending termination signal to PID %d, idDead: %t, timeout: %v", pid, idDeadPID, gracefulTimeout)
	// Use the platform-specific termination signal
	// On Unix: SIGTERM to process group
	// On Windows: Ctrl-Break event
	if err := process.SendTerminationSignal(pid, idDeadPID, gracefulTimeout); err != nil {
		pc.logger.Warnf("Failed to send termination signal: %v", err)
	}

	pc.logger.Infof("Waiting for process PID %d to terminate gracefully", pid)

	// Wait for graceful shutdown, timeout, or context cancellation
	select {
	case err := <-done:
		if err != nil {
			return errors.NewProcessError("process termination failed", err).WithContext("pid", pid)
		}
		pc.logger.Infof("Process PID %d terminated gracefully", pid)
		return nil
	case <-time.After(gracefulTimeout):
		pc.logger.Warnf("Process PID %d did not terminate within %v, forcing termination", pid, gracefulTimeout)
	case <-ctx.Done():
		pc.logger.Warnf("Context cancelled during graceful termination of PID %d, forcing termination", pid)
	}

	// Force termination if graceful didn't work
	pc.logger.Warnf("Force killing process PID %d", pid)

	// Use Kill() which sends SIGKILL on Unix and TerminateProcess on Windows
	if err := pc.process.Kill(); err != nil {
		return errors.NewProcessError("failed to kill process", err).WithContext("pid", pid)
	}

	// Wait for forced termination (with shorter timeout) or context cancellation
	select {
	case err := <-done:
		if err != nil {
			return errors.NewProcessError("forced termination failed", err).WithContext("pid", pid)
		}
		pc.logger.Infof("Process PID %d force terminated", pid)
		return nil
	case <-time.After(5 * time.Second):
		return errors.NewTimeoutError("process did not terminate even after force termination", nil).WithContext("pid", pid)
	case <-ctx.Done():
		pc.logger.Warnf("Context cancelled during force termination of PID %d", pid)
		return errors.NewCancelledError("termination cancelled", ctx.Err()).WithContext("pid", pid)
	}
}
