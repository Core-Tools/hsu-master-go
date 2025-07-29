package processcontrol

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/core-tools/hsu-master/pkg/logcollection"
	config "github.com/core-tools/hsu-master/pkg/logcollection/config"
	"github.com/core-tools/hsu-master/pkg/monitoring"
	"github.com/core-tools/hsu-master/pkg/process"
	"github.com/core-tools/hsu-master/pkg/processfile"
	"github.com/core-tools/hsu-master/pkg/resourcelimits"
)

// ProcessState represents the current lifecycle state of the process control
type ProcessState string

const (
	ProcessStateIdle        ProcessState = "idle"        // No process, ready to start
	ProcessStateStarting    ProcessState = "starting"    // Process startup in progress
	ProcessStateRunning     ProcessState = "running"     // Process running normally
	ProcessStateStopping    ProcessState = "stopping"    // Graceful shutdown initiated
	ProcessStateTerminating ProcessState = "terminating" // Force termination in progress
)

// ===== RESTART CONFIGURATION TYPES (moved from processcontrolimpl and monitoring) =====

// RestartPolicy defines when a process should be restarted (moved from monitoring package)
type RestartPolicy string

const (
	RestartNever         RestartPolicy = "never"
	RestartOnFailure     RestartPolicy = "on-failure"
	RestartAlways        RestartPolicy = "always"
	RestartUnlessStopped RestartPolicy = "unless-stopped"
)

// RestartTriggerType defines what triggered the restart request (moved from processcontrolimpl)
type RestartTriggerType string

const (
	RestartTriggerHealthFailure     RestartTriggerType = "health_failure"
	RestartTriggerResourceViolation RestartTriggerType = "resource_violation"
	RestartTriggerManual            RestartTriggerType = "manual"
)

// RestartContext provides context about restart trigger for intelligent decision making (moved from processcontrolimpl)
type RestartContext struct {
	TriggerType       RestartTriggerType `json:"trigger_type"`
	Severity          string             `json:"severity"`            // warning, critical, emergency
	WorkerProfileType string             `json:"worker_profile_type"` // batch, web, database, etc. - worker's load profile
	ViolationType     string             `json:"violation_type"`      // memory, cpu, health, etc.
	Message           string             `json:"message"`             // Human-readable reason
}

// RestartConfig defines retry mechanics (moved from processcontrolimpl, originally from monitoring)
type RestartConfig struct {
	MaxRetries  int           `yaml:"max_retries"`
	RetryDelay  time.Duration `yaml:"retry_delay"`
	BackoffRate float64       `yaml:"backoff_rate"` // Exponential backoff multiplier
}

// ValidateRestartConfig validates restart configuration values
func ValidateRestartConfig(config RestartConfig) error {
	if config.MaxRetries < 0 {
		return fmt.Errorf("max_retries cannot be negative: %d", config.MaxRetries)
	}
	if config.RetryDelay < 0 {
		return fmt.Errorf("retry_delay cannot be negative: %v", config.RetryDelay)
	}
	if config.BackoffRate <= 0 {
		return fmt.Errorf("backoff_rate must be positive: %f", config.BackoffRate)
	}
	return nil
}

// ContextAwareRestartConfig provides context-aware restart configuration (moved from processcontrolimpl)
type ContextAwareRestartConfig struct {
	// Default configuration for all restart scenarios
	Default RestartConfig `yaml:"default"`

	// Context-specific overrides
	HealthFailures     *RestartConfig `yaml:"health_failures,omitempty"`
	ResourceViolations *RestartConfig `yaml:"resource_violations,omitempty"`

	// Multipliers for different severities (applied to max_retries and retry_delay)
	SeverityMultipliers map[string]float64 `yaml:"severity_multipliers,omitempty"`

	// Multipliers for different worker profile types (applied to max_retries and retry_delay)
	WorkerProfileMultipliers map[string]float64 `yaml:"worker_profile_multipliers,omitempty"`

	// Time-based context awareness
	StartupGracePeriod     time.Duration `yaml:"startup_grace_period,omitempty"`     // No restarts during startup
	SustainedViolationTime time.Duration `yaml:"sustained_violation_time,omitempty"` // Resource violations must be sustained
	SpikeToleranceTime     time.Duration `yaml:"spike_tolerance_time,omitempty"`     // Allow brief resource spikes
}

// ValidateContextAwareRestartConfig validates context-aware restart configuration
func ValidateContextAwareRestartConfig(config ContextAwareRestartConfig) error {
	// Validate default configuration
	if err := ValidateRestartConfig(config.Default); err != nil {
		return fmt.Errorf("invalid default restart config: %w", err)
	}

	// Validate health failures override if provided
	if config.HealthFailures != nil {
		if err := ValidateRestartConfig(*config.HealthFailures); err != nil {
			return fmt.Errorf("invalid health_failures restart config: %w", err)
		}
	}

	// Validate resource violations override if provided
	if config.ResourceViolations != nil {
		if err := ValidateRestartConfig(*config.ResourceViolations); err != nil {
			return fmt.Errorf("invalid resource_violations restart config: %w", err)
		}
	}

	// Validate time durations
	if config.StartupGracePeriod < 0 {
		return fmt.Errorf("startup_grace_period cannot be negative: %v", config.StartupGracePeriod)
	}
	if config.SustainedViolationTime < 0 {
		return fmt.Errorf("sustained_violation_time cannot be negative: %v", config.SustainedViolationTime)
	}
	if config.SpikeToleranceTime < 0 {
		return fmt.Errorf("spike_tolerance_time cannot be negative: %v", config.SpikeToleranceTime)
	}

	// Validate severity multipliers
	for severity, multiplier := range config.SeverityMultipliers {
		if multiplier <= 0 {
			return fmt.Errorf("severity multiplier for '%s' must be positive: %f", severity, multiplier)
		}
	}

	// Validate worker profile multipliers
	for profile, multiplier := range config.WorkerProfileMultipliers {
		if multiplier <= 0 {
			return fmt.Errorf("worker profile multiplier for '%s' must be positive: %f", profile, multiplier)
		}
	}

	return nil
}

// ===== PROCESS CONTROL INTERFACE =====

// ProcessControl defines the interface for controlling a process lifecycle
type ProcessControl interface {
	// Start starts the process
	Start(ctx context.Context) error

	// Stop stops the process gracefully
	Stop(ctx context.Context) error

	// Restart restarts the process (stop then start)
	// force: if true, bypasses circuit breaker for immediate restart
	// force: if false, uses circuit breaker safety mechanisms (default/recommended)
	Restart(ctx context.Context, force bool) error

	// GetState returns the current process state
	GetState() ProcessState
}

// ===== COMMAND FUNCTION TYPES =====

type AttachCmd func(ctx context.Context) (*os.Process, io.ReadCloser, *monitoring.HealthCheckConfig, error)

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

	// Process restart configuration (Context-aware restart)
	ContextAwareRestart *ContextAwareRestartConfig // nil if not restartable
	RestartPolicy       RestartPolicy              // Policy for health monitor

	// Worker profile type for context-aware restart decisions
	WorkerProfileType string // "batch", "web", "database", etc. - worker's load/resource profile for restart policies

	// Log collection
	LogCollectionService logcollection.LogCollectionService // Log collection service
	LogConfig            *config.WorkerLogConfig            // Log collection configuration for this worker

	// Health check override (✅ FIXED: Using monitoring.HealthCheckConfig for proper cohesiveness)
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
	ProcessFile processfile.ProcessFileConfig `yaml:"process_file,omitempty"`

	// Process restart configuration (Context-aware restart replaces simple Restart)
	ContextAwareRestart ContextAwareRestartConfig `yaml:"context_aware_restart"`
	RestartPolicy       RestartPolicy             `yaml:"restart_policy"` // Policy for health monitor

	// Resource management
	Limits resourcelimits.ResourceLimits `yaml:"limits,omitempty"`

	// Graceful shutdown
	GracefulTimeout time.Duration `yaml:"graceful_timeout,omitempty"` // Time to wait for graceful shutdown
}
