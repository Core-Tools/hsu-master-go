package resourcelimits

import (
	"context"
	"time"

	"github.com/core-tools/hsu-master/pkg/process"
)

// ResourceMonitor provides real-time resource usage monitoring
type ResourceMonitor interface {
	// GetCurrentUsage returns current resource usage
	GetCurrentUsage() (*ResourceUsage, error)

	// Start begins resource monitoring
	Start(ctx context.Context) error

	// Stop stops resource monitoring
	Stop()

	// SetUsageCallback sets callback for resource usage updates
	SetUsageCallback(callback ResourceUsageCallback)

	// SetViolationCallback sets callback for limit violations
	SetViolationCallback(callback ResourceViolationCallback)
}

// ResourceEnforcer applies and enforces resource limits
type ResourceEnforcer interface {
	// ApplyLimits applies resource limits to a process
	ApplyLimits(pid int, limits *EnhancedResourceLimits) error

	// EnforcePolicy executes the policy action for a limit violation
	EnforcePolicy(pid int, violation *ResourceViolation) error

	// SupportsLimitType checks if a limit type is supported on current platform
	SupportsLimitType(limitType ResourceLimitType) bool
}

// ResourceLimitType represents different types of resource limits
type ResourceLimitType string

const (
	ResourceLimitTypeMemory  ResourceLimitType = "memory"
	ResourceLimitTypeCPU     ResourceLimitType = "cpu"
	ResourceLimitTypeIO      ResourceLimitType = "io"
	ResourceLimitTypeNetwork ResourceLimitType = "network"
	ResourceLimitTypeProcess ResourceLimitType = "process"
)

// ResourceUsage represents current resource usage
type ResourceUsage struct {
	Timestamp time.Time `json:"timestamp"`

	// Memory usage
	MemoryRSS     int64   `json:"memory_rss"`     // Resident Set Size
	MemoryVirtual int64   `json:"memory_virtual"` // Virtual memory
	MemoryPercent float64 `json:"memory_percent"` // % of system memory

	// CPU usage
	CPUPercent float64 `json:"cpu_percent"` // % CPU usage
	CPUTime    float64 `json:"cpu_time"`    // Total CPU time in seconds

	// I/O usage
	IOReadBytes  int64 `json:"io_read_bytes"`  // Bytes read
	IOWriteBytes int64 `json:"io_write_bytes"` // Bytes written
	IOReadOps    int64 `json:"io_read_ops"`    // Read operations
	IOWriteOps   int64 `json:"io_write_ops"`   // Write operations

	// Process/File descriptor usage
	OpenFileDescriptors int `json:"open_file_descriptors"`
	ChildProcesses      int `json:"child_processes"`

	// Network usage (if available)
	NetworkBytesReceived int64 `json:"network_bytes_received"`
	NetworkBytesSent     int64 `json:"network_bytes_sent"`
}

// ResourceViolation represents a resource limit violation
type ResourceViolation struct {
	LimitType    ResourceLimitType `json:"limit_type"`
	CurrentValue interface{}       `json:"current_value"`
	LimitValue   interface{}       `json:"limit_value"`
	Severity     ViolationSeverity `json:"severity"`
	Timestamp    time.Time         `json:"timestamp"`
	Message      string            `json:"message"`
}

// ViolationSeverity indicates how severe a resource violation is
type ViolationSeverity string

const (
	ViolationSeverityWarning  ViolationSeverity = "warning"
	ViolationSeverityCritical ViolationSeverity = "critical"
)

// ResourcePolicy defines what action to take when limits are violated
type ResourcePolicy string

const (
	ResourcePolicyNone             ResourcePolicy = "none"              // No action
	ResourcePolicyLog              ResourcePolicy = "log"               // Log violation only
	ResourcePolicyAlert            ResourcePolicy = "alert"             // Send alert/notification
	ResourcePolicyThrottle         ResourcePolicy = "throttle"          // Suspend/resume process
	ResourcePolicyGracefulShutdown ResourcePolicy = "graceful_shutdown" // SIGTERM then SIGKILL
	ResourcePolicyImmediateKill    ResourcePolicy = "immediate_kill"    // SIGKILL immediately
	ResourcePolicyRestart          ResourcePolicy = "restart"           // Restart with same/adjusted limits
	ResourcePolicyRestartAdjusted  ResourcePolicy = "restart_adjusted"  // Restart with increased limits
)

// EnhancedResourceLimits extends the basic ResourceLimits with policies and monitoring
type EnhancedResourceLimits struct {
	// Embed existing ResourceLimits
	process.ResourceLimits `yaml:",inline"`

	// Memory limits with policies
	MemoryLimits *MemoryLimits `yaml:"memory_limits,omitempty"`

	// CPU limits with policies
	CPULimits *CPULimits `yaml:"cpu_limits,omitempty"`

	// I/O limits with policies
	IOLimits *IOLimits `yaml:"io_limits,omitempty"`

	// Process limits with policies
	ProcessLimits *ProcessLimits `yaml:"process_limits,omitempty"`

	// Monitoring configuration
	Monitoring *ResourceMonitoringConfig `yaml:"monitoring,omitempty"`
}

// MemoryLimits defines memory-specific limits and policies
type MemoryLimits struct {
	MaxRSS           int64          `yaml:"max_rss,omitempty"`           // Max RSS in bytes
	MaxVirtual       int64          `yaml:"max_virtual,omitempty"`       // Max virtual memory
	WarningThreshold float64        `yaml:"warning_threshold,omitempty"` // Warning threshold (0-100%)
	Policy           ResourcePolicy `yaml:"policy,omitempty"`            // Action to take
	CheckInterval    time.Duration  `yaml:"check_interval,omitempty"`    // How often to check
}

// CPULimits defines CPU-specific limits and policies
type CPULimits struct {
	MaxPercent       float64        `yaml:"max_percent,omitempty"`       // Max CPU percentage
	MaxTime          time.Duration  `yaml:"max_time,omitempty"`          // Max total CPU time
	WarningThreshold float64        `yaml:"warning_threshold,omitempty"` // Warning threshold (0-100%)
	Policy           ResourcePolicy `yaml:"policy,omitempty"`            // Action to take
	CheckInterval    time.Duration  `yaml:"check_interval,omitempty"`    // How often to check
}

// IOLimits defines I/O-specific limits and policies
type IOLimits struct {
	MaxReadRate      int64          `yaml:"max_read_rate,omitempty"`     // Bytes per second
	MaxWriteRate     int64          `yaml:"max_write_rate,omitempty"`    // Bytes per second
	MaxReadOps       int64          `yaml:"max_read_ops,omitempty"`      // Operations per second
	MaxWriteOps      int64          `yaml:"max_write_ops,omitempty"`     // Operations per second
	WarningThreshold float64        `yaml:"warning_threshold,omitempty"` // Warning threshold (0-100%)
	Policy           ResourcePolicy `yaml:"policy,omitempty"`            // Action to take
	CheckInterval    time.Duration  `yaml:"check_interval,omitempty"`    // How often to check
}

// ProcessLimits defines process/file descriptor limits and policies
type ProcessLimits struct {
	MaxFileDescriptors int            `yaml:"max_file_descriptors,omitempty"` // Max open FDs
	MaxChildProcesses  int            `yaml:"max_child_processes,omitempty"`  // Max child processes
	WarningThreshold   float64        `yaml:"warning_threshold,omitempty"`    // Warning threshold (0-100%)
	Policy             ResourcePolicy `yaml:"policy,omitempty"`               // Action to take
	CheckInterval      time.Duration  `yaml:"check_interval,omitempty"`       // How often to check
}

// ResourceMonitoringConfig defines monitoring behavior
type ResourceMonitoringConfig struct {
	Enabled          bool          `yaml:"enabled"`                     // Enable monitoring
	Interval         time.Duration `yaml:"interval,omitempty"`          // Monitoring interval
	HistoryRetention time.Duration `yaml:"history_retention,omitempty"` // How long to keep history
	AlertingEnabled  bool          `yaml:"alerting_enabled,omitempty"`  // Enable alerting
}

// Callback types
type ResourceUsageCallback func(usage *ResourceUsage)
type ResourceViolationCallback func(violation *ResourceViolation)
