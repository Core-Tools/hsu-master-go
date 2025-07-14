package domain

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/core-tools/hsu-master/pkg/logging"
)

type SystemProcessControlConfig struct {
	// Basic control
	CanTerminate bool // Can send SIGTERM/SIGKILL
	CanRestart   bool // Can restart (via service manager)

	// Service manager integration
	ServiceManager string // "systemd", "windows", "launchd"
	ServiceName    string // Service name for restart

	// Process signals
	AllowedSignals []os.Signal // Allowed signals to send

	// Graceful shutdown
	GracefulTimeout time.Duration // Time to wait for graceful shutdown
}

type RestartPolicy string

const (
	RestartNever         RestartPolicy = "never"
	RestartOnFailure     RestartPolicy = "on-failure"
	RestartAlways        RestartPolicy = "always"
	RestartUnlessStopped RestartPolicy = "unless-stopped"
)

type RestartConfig struct {
	Policy      RestartPolicy
	MaxRetries  int
	RetryDelay  time.Duration
	BackoffRate float64 // Exponential backoff multiplier
}

type ResourceLimits struct {
	// Process priority
	Priority int

	// CPU limits
	CPU       float64 // Number of CPU cores
	CPUShares int     // CPU weight (Linux cgroups)

	// Memory limits
	Memory     int64 // Memory limit in bytes
	MemorySwap int64 // Memory + swap limit

	// Process limits
	MaxProcesses int // Maximum number of processes
	MaxOpenFiles int // Maximum open file descriptors

	// I/O limits
	IOWeight   int   // I/O priority weight
	IOReadBPS  int64 // Read bandwidth limit
	IOWriteBPS int64 // Write bandwidth limit
}

type ManagedProcessControlConfig struct {
	// Process execution
	Execution ExecutionConfig

	// Process restart
	Restart RestartConfig

	// Resource management
	Limits ResourceLimits

	/*
		// I/O handling
		LogConfig      LogConfig
		StdoutRedirect string
		StderrRedirect string

		// Scheduling
		Schedule ScheduleConfig
	*/

	// Graceful shutdown
	GracefulTimeout time.Duration // Time to wait for graceful shutdown
}

type DiscoveryMethod string

const (
	DiscoveryMethodProcessName DiscoveryMethod = "process-name"
	DiscoveryMethodPort        DiscoveryMethod = "port"
	DiscoveryMethodPIDFile     DiscoveryMethod = "pid-file"
	DiscoveryMethodServiceName DiscoveryMethod = "service-name"
)

type DiscoveryConfig struct {
	Method DiscoveryMethod

	// Process name discovery
	ProcessName string
	ProcessArgs []string // Optional: match by command line args

	// Port discovery
	Port     int
	Protocol string // "tcp", "udp"

	// PID file discovery
	PIDFile string

	// Service discovery (systemd, Windows services)
	ServiceName string

	// Discovery frequency
	CheckInterval time.Duration
}

type ProcessControl interface {
	Process() (*os.Process, *os.ProcessState)
	HealthMonitor() HealthMonitor
	Stdout() io.ReadCloser
	Start() error
	Stop() error
	Restart() error
}

type ExecuteCmd func(ctx context.Context) (*exec.Cmd, io.ReadCloser, *HealthCheckConfig, error)

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
	Discovery  DiscoveryConfig // Discovery config, must be always present
	ExecuteCmd ExecuteCmd      // Execute command, nil if not executable

	// Process restart
	Restart *RestartConfig // nil if not restartable

	// Resource management
	Limits *ResourceLimits // nil if not limitable

	// Health check override
	HealthCheck *HealthCheckConfig // nil if not health checkable or if ExecuteCmd is provided
}

type processControl struct {
	config        ProcessControlOptions
	process       *os.Process
	state         *os.ProcessState
	stdout        io.ReadCloser
	healthMonitor HealthMonitor
	logger        logging.Logger
}

func NewProcessControl(config ProcessControlOptions, logger logging.Logger) ProcessControl {
	return &processControl{
		config: config,
		logger: logger,
	}
}

func (pc *processControl) Process() (*os.Process, *os.ProcessState) {
	return pc.process, pc.state
}

func (pc *processControl) HealthMonitor() HealthMonitor {
	return pc.healthMonitor
}

func (pc *processControl) Stdout() io.ReadCloser {
	return pc.stdout
}

func (pc *processControl) Start() error {
	pc.logger.Infof("Starting process control, config: %+v", pc.config)

	var process *os.Process
	var state *os.ProcessState
	var stdout io.ReadCloser
	var err error

	healthCheckConfig := pc.config.HealthCheck

	executeCmd := pc.config.ExecuteCmd
	if pc.config.CanAttach {
		pc.logger.Infof("Trying to attach to process...")

		process, state, stdout, healthCheckConfig, err = OpenProcess(pc.config.Discovery)
		if err == nil {
			pc.logger.Infof("Attached to process, pid: %d", process.Pid)
			executeCmd = nil // attached successfully, no need to execute cmd
		}
	}

	if executeCmd != nil { // can't attach or not attached
		pc.logger.Infof("Running ExecuteCmd...")

		var cmd *exec.Cmd
		cmd, stdout, healthCheckConfig, err = pc.config.ExecuteCmd(context.Background())
		if err != nil {
			return fmt.Errorf("failed to start process: %v", err)
		}

		process, state = cmd.Process, cmd.ProcessState
		pc.logger.Infof("Executed command, pid: %d", process.Pid)
	}

	pc.logger.Infof("Process control started, process: %+v, state: %+v, stdout: %+v", process, state, stdout)

	pc.process = process
	pc.state = state
	pc.stdout = stdout

	var healthMonitor HealthMonitor
	if healthCheckConfig != nil {
		pc.logger.Infof("Starting health monitor, config: %+v", healthCheckConfig)

		healthMonitor = NewHealthMonitor(healthCheckConfig)
		healthMonitor.Start()
	}
	pc.healthMonitor = healthMonitor

	return nil
}

func (pc *processControl) Stop() error {
	pc.logger.Infof("Stopping process control...")

	if pc.process == nil {
		return fmt.Errorf("process not attached")
	}

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

	// 3. Terminate the process gracefully
	if err := pc.terminateProcess(); err != nil {
		pc.logger.Errorf("Failed to terminate process: %v", err)
		return fmt.Errorf("failed to terminate process: %v", err)
	}

	// 4. Clean up process references
	pc.process = nil
	pc.state = nil

	pc.logger.Infof("Process control stopped successfully")
	return nil
}

func (pc *processControl) Restart() error {
	pc.logger.Infof("Restarting process control...")

	// Store the original config for restart
	if pc.process == nil {
		return fmt.Errorf("process not attached")
	}

	// 1. Stop the current process
	if err := pc.Stop(); err != nil {
		pc.logger.Errorf("Failed to stop process during restart: %v", err)
		return fmt.Errorf("failed to stop process during restart: %v", err)
	}

	// 2. Start the process again
	if err := pc.Start(); err != nil {
		pc.logger.Errorf("Failed to start process during restart: %v", err)
		return fmt.Errorf("failed to start process during restart: %v", err)
	}

	pc.logger.Infof("Process control restarted successfully")
	return nil
}

// terminateProcess handles graceful process termination with timeout
func (pc *processControl) terminateProcess() error {
	if pc.process == nil {
		return fmt.Errorf("no process to terminate")
	}

	pid := pc.process.Pid
	pc.logger.Infof("Terminating process PID %d", pid)

	// Determine graceful timeout
	gracefulTimeout := pc.config.GracefulTimeout
	if gracefulTimeout <= 0 {
		gracefulTimeout = 30 * time.Second // Default timeout
	}

	// Create a channel to signal when process exits
	done := make(chan error, 1)

	// Start a goroutine to wait for process exit
	go func() {
		state, err := pc.process.Wait()
		if err != nil {
			done <- fmt.Errorf("process wait failed: %v", err)
		} else {
			pc.logger.Infof("Process PID %d exited with status: %v", pid, state)
			done <- nil
		}
	}()

	// Try graceful termination first
	pc.logger.Infof("Sending termination signal to process PID %d", pid)
	if err := pc.sendTerminationSignal(); err != nil {
		pc.logger.Warnf("Failed to send termination signal: %v", err)
	}

	// Wait for graceful shutdown or timeout
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("process termination failed: %v", err)
		}
		pc.logger.Infof("Process PID %d terminated gracefully", pid)
		return nil
	case <-time.After(gracefulTimeout):
		pc.logger.Warnf("Process PID %d did not terminate within %v, forcing termination", pid, gracefulTimeout)
	}

	// Force termination if graceful didn't work
	if err := pc.forceTermination(); err != nil {
		return fmt.Errorf("force termination failed: %v", err)
	}

	// Wait for forced termination (with shorter timeout)
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("forced termination failed: %v", err)
		}
		pc.logger.Infof("Process PID %d force terminated", pid)
		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("process PID %d did not terminate even after force termination", pid)
	}
}

// sendTerminationSignal sends a graceful termination signal to the process
func (pc *processControl) sendTerminationSignal() error {
	if pc.process == nil {
		return fmt.Errorf("no process to signal")
	}

	// Use the platform-specific termination signal
	// On Unix: SIGTERM to process group
	// On Windows: Ctrl-Break event
	return pc.sendGracefulSignal()
}

// forceTermination forcefully terminates the process
func (pc *processControl) forceTermination() error {
	if pc.process == nil {
		return fmt.Errorf("no process to kill")
	}

	pc.logger.Warnf("Force killing process PID %d", pc.process.Pid)

	// Use Kill() which sends SIGKILL on Unix and TerminateProcess on Windows
	if err := pc.process.Kill(); err != nil {
		return fmt.Errorf("failed to kill process: %v", err)
	}

	return nil
}
