//go:build darwin
// +build darwin

package resourcelimits

import (
	"errors"

	"github.com/core-tools/hsu-master/pkg/logging"
)

// Platform-specific limit support checks
func supportsLimitTypeImpl(limitType ResourceLimitType) bool {
	switch limitType {
	case ResourceLimitTypeMemory, ResourceLimitTypeCPU, ResourceLimitTypeProcess:
		return true // macOS supports these via various mechanisms
	default:
		return false
	}
}

// applyMemoryLimitsImpl applies memory limits using Job Objects (implementation)
func applyMemoryLimitsImpl(pid int, limits *MemoryLimits, logger logging.Logger) error {
	return errors.New("applyMemoryLimitsImpl not implemented")
}

// applyCPULimitsImpl applies CPU limits using Job Objects (implementation)
func applyCPULimitsImpl(pid int, limits *CPULimits, logger logging.Logger) error {
	return errors.New("applyCPULimitsImpl not implemented")
}
