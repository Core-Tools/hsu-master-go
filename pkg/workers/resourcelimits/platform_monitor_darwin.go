//go:build darwin
// +build darwin

package resourcelimits

import (
	"github.com/core-tools/hsu-master/pkg/logging"
)

// darwinResourceMonitor provides macOS-specific monitoring
type darwinResourceMonitor struct {
	logger logging.Logger
}

func newDarwinResourceMonitor(logger logging.Logger) PlatformResourceMonitor {
	return &darwinResourceMonitor{
		logger: logger,
	}
}

func (d *darwinResourceMonitor) GetProcessUsage(pid int) (*ResourceUsage, error) {
	// TODO: Implement macOS-specific resource monitoring using libproc or sysctl
	d.logger.Debugf("macOS resource monitoring for PID %d - using generic implementation for now", pid)
	return newGenericResourceMonitor(d.logger).GetProcessUsage(pid)
}

func (d *darwinResourceMonitor) SupportsRealTimeMonitoring() bool {
	return true // macOS supports real-time monitoring via system APIs
}

// createPlatformSpecificMonitor creates macOS-specific monitor
func createPlatformSpecificMonitor(logger logging.Logger) PlatformResourceMonitor {
	return newDarwinResourceMonitor(logger)
}
