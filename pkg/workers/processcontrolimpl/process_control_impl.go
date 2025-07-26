package processcontrolimpl

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/core-tools/hsu-master/pkg/errors"
	"github.com/core-tools/hsu-master/pkg/logging"
	"github.com/core-tools/hsu-master/pkg/monitoring"
	"github.com/core-tools/hsu-master/pkg/process"
	"github.com/core-tools/hsu-master/pkg/resourcelimits"
	"github.com/core-tools/hsu-master/pkg/workers/processcontrol"
)

type processControl struct {
	config   processcontrol.ProcessControlOptions
	process  *os.Process
	stdout   io.ReadCloser
	logger   logging.Logger
	workerID string

	// Health monitor
	healthMonitor monitoring.HealthMonitor

	// Resource limit management
	resourceManager resourcelimits.ResourceLimitManager

	// Persistent restart tracking (survives health monitor recreation)
	restartCircuitBreaker RestartCircuitBreaker
}

func NewProcessControl(config processcontrol.ProcessControlOptions, workerID string, logger logging.Logger) processcontrol.ProcessControl {
	var restartCircuitBreaker RestartCircuitBreaker
	if config.Restart != nil && config.CanRestart {
		restartCircuitBreaker = NewRestartCircuitBreaker(
			config.Restart, workerID, logger)
	}

	return &processControl{
		config:                config,
		logger:                logger,
		workerID:              workerID,
		restartCircuitBreaker: restartCircuitBreaker,
	}
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

	healthMonitor, err := pc.startHealthCheck(ctx, process.Pid, healthCheckConfig)
	if err != nil {
		pc.logger.Warnf("Failed to start health monitor, worker: %s, error: %v", pc.workerID, err)
		// ignore health monitor error
	}

	pc.healthMonitor = healthMonitor

	// Initialize resource monitoring if limits are specified
	resourceManager, err := pc.startResourceMonitoring(ctx, process.Pid)
	if err != nil {
		pc.logger.Warnf("Failed to initialize resource monitoring for worker %s: %v", pc.workerID, err)
		// Don't fail process start due to monitoring issues
	}

	pc.resourceManager = resourceManager

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

func (pc *processControl) startHealthCheck(ctx context.Context, pid int, healthCheckConfig *monitoring.HealthCheckConfig) (monitoring.HealthMonitor, error) {
	pc.logger.Infof("Starting health monitor for worker %s, config: %+v", pc.workerID, healthCheckConfig)

	if healthCheckConfig == nil {
		pc.logger.Errorf("Health check configuration is nil, worker: %s", pc.workerID)
		return nil, errors.NewValidationError("health check configuration is nil", nil).WithContext("id", pc.workerID)
	}

	var healthMonitor monitoring.HealthMonitor

	// Create process info for health monitoring - simplified
	processInfo := &monitoring.ProcessInfo{
		PID: pid, // Only PID needed
	}

	// Create health monitor with appropriate capabilities
	if healthCheckConfig.Type == monitoring.HealthCheckTypeProcess {
		if pc.config.Restart != nil && pc.config.CanRestart {
			// Create health monitor with restart capability
			healthMonitor = monitoring.NewHealthMonitorWithRestart(healthCheckConfig,
				pc.workerID, processInfo, pc.config.Restart, pc.logger)
		} else {
			// Create health monitor with process info but no restart
			healthMonitor = monitoring.NewHealthMonitorWithProcessInfo(healthCheckConfig,
				pc.workerID, processInfo, pc.logger)
		}
	} else {
		// For other health check types, create standard monitor
		healthMonitor = monitoring.NewHealthMonitor(healthCheckConfig, pc.workerID, pc.logger)
	}

	// Set up restart callback if restart is enabled
	if pc.restartCircuitBreaker != nil {
		healthMonitor.SetRestartCallback(func(reason string) error {
			pc.logger.Warnf("Restart requested, worker: %s, reason: %s", pc.workerID, reason)
			// Already async context in health monitor callback - no goroutine
			wrappedRestart := func() error {
				// Use a background context for restart since this is triggered by health failure
				ctx := context.Background()
				return pc.restartInternal(ctx)
			}
			return pc.restartCircuitBreaker.ExecuteRestart(wrappedRestart)
		})

		// Set up recovery callback to reset circuit breaker when process becomes healthy
		healthMonitor.SetRecoveryCallback(func() {
			pc.logger.Infof("Health recovered, resetting circuit breaker, worker: %s", pc.workerID)
			pc.restartCircuitBreaker.Reset()
		})
	}

	err := healthMonitor.Start(ctx)
	if err != nil {
		return nil, errors.NewInternalError("failed to start health monitoring", err).WithContext("id", pc.workerID)
	}

	pc.logger.Infof("Health monitor started, worker: %s", pc.workerID)

	return healthMonitor, nil
}

// Initialize resource monitoring (internal - no validation)
func (pc *processControl) startResourceMonitoring(ctx context.Context, pid int) (resourcelimits.ResourceLimitManager, error) {
	if pc.config.Limits == nil {
		return nil, errors.NewInternalError("no limits specified", nil).WithContext("id", pc.workerID)
	}

	pc.logger.Debugf("Initializing resource monitoring for worker %s, PID: %d", pc.workerID, pid)

	// Configure limits for external violation handling
	limits := pc.config.Limits
	if limits.Monitoring == nil {
		limits.Monitoring = &resourcelimits.ResourceMonitoringConfig{
			Enabled:           true,
			Interval:          30 * time.Second,
			HistoryRetention:  24 * time.Hour,
			AlertingEnabled:   true,
			ViolationHandling: resourcelimits.ViolationHandlingExternal, // Use external handling for ProcessControl integration
		}
	} else {
		// Override violation handling mode to external for ProcessControl integration
		limits.Monitoring.ViolationHandling = resourcelimits.ViolationHandlingExternal
	}

	// Create resource limit manager
	resourceManager := resourcelimits.NewResourceLimitManager(
		pc.process.Pid,
		limits,
		pc.logger,
	)

	// Set violation callback to integrate with ProcessControl restart logic
	resourceManager.SetViolationCallback(pc.handleResourceViolation)

	// Start monitoring
	err := pc.resourceManager.Start(ctx)
	if err != nil {
		return nil, errors.NewInternalError("failed to start resource monitoring", err).WithContext("id", pc.workerID)
	}

	pc.logger.Infof("Resource monitoring started for worker %s, PID: %d", pc.workerID, pc.process.Pid)
	return resourceManager, nil
}

// Handle resource violations by integrating with existing restart logic (internal)
func (pc *processControl) handleResourceViolation(violation *resourcelimits.ResourceViolation) {
	pc.logger.Warnf("Resource violation detected for worker %s: %s", pc.workerID, violation.Message)

	// Handle different violation policies
	switch violation.LimitType {
	case resourcelimits.ResourceLimitTypeMemory:
		if pc.config.Limits.Memory != nil {
			pc.executeViolationPolicy(pc.config.Limits.Memory.Policy, violation)
		}
	case resourcelimits.ResourceLimitTypeCPU:
		if pc.config.Limits.CPU != nil {
			pc.executeViolationPolicy(pc.config.Limits.CPU.Policy, violation)
		}
	case resourcelimits.ResourceLimitTypeProcess:
		if pc.config.Limits.Process != nil {
			pc.executeViolationPolicy(pc.config.Limits.Process.Policy, violation)
		}
	default:
		pc.logger.Warnf("Unknown resource violation type: %s", violation.LimitType)
	}
}

// Execute violation policy actions (internal)
func (pc *processControl) executeViolationPolicy(policy resourcelimits.ResourcePolicy, violation *resourcelimits.ResourceViolation) {
	switch policy {
	case resourcelimits.ResourcePolicyLog:
		pc.logger.Warnf("Resource limit exceeded (policy: log): %s", violation.Message)

	case resourcelimits.ResourcePolicyAlert:
		pc.logger.Errorf("ALERT: Resource limit exceeded: %s", violation.Message)
		// TODO: Integrate with alerting system when available

	case resourcelimits.ResourcePolicyRestart:
		pc.logger.Errorf("Resource limit exceeded, restarting process (policy: restart): %s", violation.Message)
		// Use goroutine to avoid blocking resource monitoring, but use circuit breaker for consistency
		go func() {
			if pc.restartCircuitBreaker != nil {
				// Use circuit breaker for consistent restart logic and protection against loops
				wrappedRestart := func() error {
					ctx := context.Background()
					return pc.restartInternal(ctx)
				}
				if err := pc.restartCircuitBreaker.ExecuteRestart(wrappedRestart); err != nil {
					pc.logger.Errorf("Failed to restart process after resource violation (circuit breaker): %v", err)
				}
			} else {
				// Fallback if no circuit breaker (shouldn't happen in normal operation)
				ctx := context.Background()
				if err := pc.restartInternal(ctx); err != nil {
					pc.logger.Errorf("Failed to restart process after resource violation: %v", err)
				}
			}
		}()

	case resourcelimits.ResourcePolicyGracefulShutdown:
		pc.logger.Errorf("Resource limit exceeded, performing graceful shutdown (policy: graceful_shutdown): %s", violation.Message)
		// Async to avoid blocking monitoring, but graceful shutdown doesn't need circuit breaker
		go func() {
			ctx := context.Background()
			if err := pc.stopInternal(ctx, false); err != nil {
				pc.logger.Errorf("Failed to stop process after resource violation: %v", err)
			}
		}()

	case resourcelimits.ResourcePolicyImmediateKill:
		pc.logger.Errorf("Resource limit exceeded, killing process immediately (policy: immediate_kill): %s", violation.Message)
		// Synchronous - Kill() is fast and immediate
		if pc.process != nil {
			pc.process.Kill()
		}

	default:
		pc.logger.Warnf("Unknown resource policy: %s", policy)
	}
}

func (pc *processControl) stopInternal(ctx context.Context, idDeadPID bool) error {
	pc.logger.Infof("Stopping process control...")

	// 1. Stop resource monitoring first
	if pc.resourceManager != nil {
		pc.resourceManager.Stop()
		pc.resourceManager = nil
		pc.logger.Debugf("Resource monitoring stopped for worker %s", pc.workerID)
	}

	// 2. Stop health monitor
	if pc.healthMonitor != nil {
		pc.logger.Infof("Stopping health monitor...")
		pc.healthMonitor.Stop()
		pc.healthMonitor = nil
	}

	// 3. Close stdout reader to prevent resource leaks
	if pc.stdout != nil {
		pc.logger.Infof("Closing stdout reader...")
		if err := pc.stdout.Close(); err != nil {
			pc.logger.Warnf("Failed to close stdout: %v", err)
		}
		pc.stdout = nil
	}

	// 4. Terminate the process gracefully with context
	if err := pc.terminateProcess(ctx, idDeadPID); err != nil {
		pc.logger.Errorf("Failed to terminate process: %v", err)
		return errors.NewProcessError("failed to terminate process", err)
	}

	// 5. Clean up process references
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
