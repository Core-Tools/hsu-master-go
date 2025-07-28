package resourcelimits

import (
	"fmt"

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

// SupportsLimitType checks if a limit type is supported on current platform
func (re *resourceEnforcer) SupportsLimitType(limitType ResourceLimitType) bool {
	return supportsLimitTypeImpl(limitType)
}

// applyMemoryLimits applies memory-specific limits
func (re *resourceEnforcer) applyMemoryLimits(pid int, limits *MemoryLimits) error {
	if limits.MaxRSS <= 0 && limits.MaxVirtual <= 0 {
		return nil
	}

	return applyMemoryLimitsImpl(pid, limits, re.logger)
}

// applyCPULimits applies CPU-specific limits
func (re *resourceEnforcer) applyCPULimits(pid int, limits *CPULimits) error {
	if limits.MaxPercent <= 0 && limits.MaxTime <= 0 {
		return nil
	}

	return applyCPULimitsImpl(pid, limits, re.logger)
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

func (re *resourceEnforcer) applyFileDescriptorLimit(pid int, maxFDs int) error {
	re.logger.Debugf("File descriptor limit for PID %d: %d - implementation pending", pid, maxFDs)
	// TODO: Implement using setrlimit (Unix) or Job Objects (Windows)
	return nil
}
