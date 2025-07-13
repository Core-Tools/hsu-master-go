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

func OpenProcess(config DiscoveryConfig) (*os.Process, *os.ProcessState, io.ReadCloser, *HealthCheckConfig, error) {
	return nil, nil, nil, nil, fmt.Errorf("not implemented")
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

	return fmt.Errorf("not implemented")
}

func (pc *processControl) Restart() error {
	pc.logger.Infof("Restarting process control...")

	if pc.process == nil {
		return fmt.Errorf("process not attached")
	}

	return fmt.Errorf("not implemented")
}
