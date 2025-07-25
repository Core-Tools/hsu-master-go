package processcontrol

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/core-tools/hsu-master/pkg/monitoring"
	"github.com/core-tools/hsu-master/pkg/process"
	"github.com/core-tools/hsu-master/pkg/processfile"
	"github.com/core-tools/hsu-master/pkg/resourcelimits"
)

// ProcessControl interface defines the contract for process control operations
type ProcessControl interface {
	Process() *os.Process
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

// ProcessControlOptions provides configuration for ProcessControl instances
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
	Limits *resourcelimits.ResourceLimits // nil if not limitable

	// Process restart
	Restart *monitoring.RestartConfig // nil if not restartable

	// Health check override
	HealthCheck *monitoring.HealthCheckConfig // nil if not health checkable or if ExecuteCmd/AttachCmd are provided
}

// SystemProcessControlConfig defines configuration for system-managed processes
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

// ManagedProcessControlConfig defines configuration for managed processes
type ManagedProcessControlConfig struct {
	// Process execution
	Execution process.ExecutionConfig `yaml:"execution"`

	// PID file configuration (optional)
	ProcessFile *processfile.ProcessFileConfig `yaml:"process_file,omitempty"`

	// Process restart
	Restart monitoring.RestartConfig `yaml:"restart"`

	// Resource management
	Limits resourcelimits.ResourceLimits `yaml:"limits,omitempty"`

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
