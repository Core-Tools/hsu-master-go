package resourcelimits

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
	"time"

	"github.com/core-tools/hsu-master/pkg/logging"
	"github.com/core-tools/hsu-master/pkg/processstate"
)

// resourceEnforcer implements ResourceEnforcer interface
type resourceEnforcer struct {
	logger logging.Logger
}

// NewResourceEnforcer creates a new resource enforcer
func NewResourceEnforcer(logger logging.Logger) ResourceEnforcer {
	return &resourceEnforcer{
		logger: logger,
	}
}

// ApplyLimits applies resource limits to a process
func (re *resourceEnforcer) ApplyLimits(pid int, limits *ResourceLimits) error {
	if limits == nil {
		return nil
	}

	re.logger.Infof("Applying resource limits to PID %d", pid)

	// Check if process exists
	if !processstate.IsProcessRunning(pid) {
		return fmt.Errorf("process %d is not running", pid)
	}

	var errors []error

	// Apply memory limits if specified
	if limits.Memory != nil {
		if err := re.applyMemoryLimits(pid, limits.Memory); err != nil {
			errors = append(errors, fmt.Errorf("memory limits: %v", err))
		}
	}

	// Apply CPU limits if specified
	if limits.CPU != nil {
		if err := re.applyCPULimits(pid, limits.CPU); err != nil {
			errors = append(errors, fmt.Errorf("CPU limits: %v", err))
		}
	}

	// Apply I/O limits if specified (TODO: implement applyIOLimits method)
	if limits.IO != nil {
		re.logger.Debugf("I/O limits specified but not yet implemented for PID %d", pid)
		// TODO: if err := re.applyIOLimits(pid, limits.IO); err != nil {
		//	errors = append(errors, fmt.Errorf("I/O limits: %v", err))
		// }
	}

	// Apply process limits if specified
	if limits.Process != nil {
		if err := re.applyProcessLimits(pid, limits.Process); err != nil {
			errors = append(errors, fmt.Errorf("process limits: %v", err))
		}
	}

	// Apply basic limits if present in nested structs
	if limits.Memory != nil && (limits.Memory.MaxRSS > 0 || limits.Memory.MaxVirtual > 0) {
		re.logger.Debugf("Applying memory limits to PID %d (MaxRSS: %d, MaxVirtual: %d)",
			pid, limits.Memory.MaxRSS, limits.Memory.MaxVirtual)
	}

	if limits.Process != nil && limits.Process.MaxFileDescriptors > 0 {
		if err := re.applyFileDescriptorLimit(pid, limits.Process.MaxFileDescriptors); err != nil {
			errors = append(errors, fmt.Errorf("file descriptor limit: %v", err))
		}
	}

	if len(errors) > 0 {
		re.logger.Warnf("Some resource limits could not be applied to PID %d: %v", pid, errors)
		// Return first error for now, could be enhanced to return composite error
		return errors[0]
	}

	re.logger.Infof("Resource limits successfully applied to PID %d", pid)
	return nil
}

// EnforcePolicy executes the policy action for a limit violation
func (re *resourceEnforcer) EnforcePolicy(pid int, violation *ResourceViolation) error {
	// Extract policy from violation context (would need to be enhanced)
	// For now, we'll use a basic enforcement strategy

	re.logger.Warnf("Enforcing policy for resource violation: %s", violation.Message)

	switch violation.Severity {
	case ViolationSeverityWarning:
		// Just log warnings for now
		re.logger.Warnf("Resource warning for PID %d: %s", pid, violation.Message)
		return nil

	case ViolationSeverityCritical:
		// For critical violations, implement basic enforcement
		return re.enforceCriticalViolation(pid, violation)

	default:
		return fmt.Errorf("unknown violation severity: %s", violation.Severity)
	}
}

// SupportsLimitType checks if a limit type is supported on current platform
func (re *resourceEnforcer) SupportsLimitType(limitType ResourceLimitType) bool {
	switch runtime.GOOS {
	case "linux":
		return re.supportsLimitTypeLinux(limitType)
	case "windows":
		return re.supportsLimitTypeWindows(limitType)
	case "darwin":
		return re.supportsLimitTypeDarwin(limitType)
	default:
		// Generic support for basic limits
		return limitType == ResourceLimitTypeMemory || limitType == ResourceLimitTypeCPU
	}
}

// applyMemoryLimits applies memory-specific limits
func (re *resourceEnforcer) applyMemoryLimits(pid int, limits *MemoryLimits) error {
	if limits.MaxRSS <= 0 && limits.MaxVirtual <= 0 {
		return nil
	}

	switch runtime.GOOS {
	case "linux":
		return re.applyLinuxMemoryLimits(pid, limits)
	case "windows":
		return re.applyWindowsMemoryLimits(pid, limits)
	case "darwin":
		return re.applyDarwinMemoryLimits(pid, limits)
	default:
		re.logger.Warnf("Memory limits not supported on platform: %s", runtime.GOOS)
		return fmt.Errorf("memory limits not supported on platform: %s", runtime.GOOS)
	}
}

// applyCPULimits applies CPU-specific limits
func (re *resourceEnforcer) applyCPULimits(pid int, limits *CPULimits) error {
	if limits.MaxPercent <= 0 && limits.MaxTime <= 0 {
		return nil
	}

	switch runtime.GOOS {
	case "linux":
		return re.applyLinuxCPULimits(pid, limits)
	case "windows":
		return re.applyWindowsCPULimits(pid, limits)
	case "darwin":
		return re.applyDarwinCPULimits(pid, limits)
	default:
		re.logger.Warnf("CPU limits not supported on platform: %s", runtime.GOOS)
		return fmt.Errorf("CPU limits not supported on platform: %s", runtime.GOOS)
	}
}

// applyProcessLimits applies process/file descriptor limits
func (re *resourceEnforcer) applyProcessLimits(pid int, limits *ProcessLimits) error {
	if limits.MaxFileDescriptors <= 0 && limits.MaxChildProcesses <= 0 {
		return nil
	}

	// File descriptor limits can be applied using setrlimit on Unix systems
	if limits.MaxFileDescriptors > 0 {
		if err := re.applyFileDescriptorLimit(pid, limits.MaxFileDescriptors); err != nil {
			return fmt.Errorf("file descriptor limit: %v", err)
		}
	}

	re.logger.Debugf("Process limits applied to PID %d", pid)
	return nil
}

// enforceCriticalViolation handles critical resource violations
func (re *resourceEnforcer) enforceCriticalViolation(pid int, violation *ResourceViolation) error {
	re.logger.Errorf("Critical resource violation for PID %d: %s", pid, violation.Message)

	// Basic enforcement: send SIGTERM then SIGKILL if necessary
	// In a full implementation, this would be configurable based on the policy

	// Check if process still exists
	if !processstate.IsProcessRunning(pid) {
		re.logger.Infof("Process %d no longer running, no enforcement needed", pid)
		return nil
	}

	// Send SIGTERM first (graceful shutdown)
	re.logger.Warnf("Sending SIGTERM to PID %d due to resource violation", pid)
	if err := re.sendSignal(pid, syscall.SIGTERM); err != nil {
		re.logger.Errorf("Failed to send SIGTERM to PID %d: %v", pid, err)
	}

	// Wait a bit for graceful shutdown
	time.Sleep(5 * time.Second)

	// Check if process is still running
	if processstate.IsProcessRunning(pid) {
		re.logger.Warnf("Process %d still running after SIGTERM, sending SIGKILL", pid)
		if err := re.sendSignal(pid, syscall.SIGKILL); err != nil {
			return fmt.Errorf("failed to send SIGKILL to PID %d: %v", pid, err)
		}
	}

	return nil
}

// sendSignal sends a signal to a process (cross-platform wrapper)
func (re *resourceEnforcer) sendSignal(pid int, sig os.Signal) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process %d: %v", pid, err)
	}

	return proc.Signal(sig)
}

// Platform-specific limit support checks
func (re *resourceEnforcer) supportsLimitTypeLinux(limitType ResourceLimitType) bool {
	switch limitType {
	case ResourceLimitTypeMemory, ResourceLimitTypeCPU, ResourceLimitTypeIO, ResourceLimitTypeProcess:
		return true // Linux supports all via cgroups and rlimit
	default:
		return false
	}
}

func (re *resourceEnforcer) supportsLimitTypeWindows(limitType ResourceLimitType) bool {
	switch limitType {
	case ResourceLimitTypeMemory, ResourceLimitTypeCPU:
		return true // Windows supports these via Job Objects
	case ResourceLimitTypeProcess:
		return true // Partial support
	default:
		return false
	}
}

func (re *resourceEnforcer) supportsLimitTypeDarwin(limitType ResourceLimitType) bool {
	switch limitType {
	case ResourceLimitTypeMemory, ResourceLimitTypeCPU, ResourceLimitTypeProcess:
		return true // macOS supports these via various mechanisms
	default:
		return false
	}
}

// Platform-specific implementation stubs (to be implemented in Phase 2)
func (re *resourceEnforcer) applyLinuxMemoryLimits(pid int, limits *MemoryLimits) error {
	re.logger.Debugf("Linux memory limits for PID %d - implementation pending", pid)
	// TODO: Implement using cgroups v2
	return nil
}

func (re *resourceEnforcer) applyWindowsMemoryLimits(pid int, limits *MemoryLimits) error {
	return applyWindowsMemoryLimitsImpl(pid, limits, re.logger)
}

func (re *resourceEnforcer) applyDarwinMemoryLimits(pid int, limits *MemoryLimits) error {
	re.logger.Debugf("macOS memory limits for PID %d - implementation pending", pid)
	// TODO: Implement using setrlimit and other macOS APIs
	return nil
}

func (re *resourceEnforcer) applyLinuxCPULimits(pid int, limits *CPULimits) error {
	re.logger.Debugf("Linux CPU limits for PID %d - implementation pending", pid)
	// TODO: Implement using cgroups v2 cpu controller
	return nil
}

func (re *resourceEnforcer) applyWindowsCPULimits(pid int, limits *CPULimits) error {
	return applyWindowsCPULimitsImpl(pid, limits, re.logger)
}

func (re *resourceEnforcer) applyDarwinCPULimits(pid int, limits *CPULimits) error {
	re.logger.Debugf("macOS CPU limits for PID %d - implementation pending", pid)
	// TODO: Implement using setpriority and process scheduling
	return nil
}

func (re *resourceEnforcer) applyFileDescriptorLimit(pid int, maxFDs int) error {
	re.logger.Debugf("File descriptor limit for PID %d: %d - implementation pending", pid, maxFDs)
	// TODO: Implement using setrlimit (Unix) or Job Objects (Windows)
	return nil
}
