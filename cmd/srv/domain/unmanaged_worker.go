package domain

import (
	"github.com/core-tools/hsu-master/pkg/logging"
)

type unmanagedWorker struct {
	id                   string
	metadata             UnitMetadata
	discoveryConfig      DiscoveryConfig
	processControlConfig SystemProcessControlConfig
	healthCheckConfig    HealthCheckConfig
	logger               logging.Logger
}

func NewUnmanagedWorker(id string, unit *UnmanagedUnit, logger logging.Logger) Worker {
	return &unmanagedWorker{
		id:                   id,
		metadata:             unit.Metadata,
		discoveryConfig:      unit.Discovery,
		processControlConfig: unit.Control,
		healthCheckConfig:    unit.HealthCheck,
		logger:               logger,
	}
}

func (w *unmanagedWorker) ID() string {
	return w.id
}

func (w *unmanagedWorker) Metadata() UnitMetadata {
	return w.metadata
}

func (w *unmanagedWorker) ProcessControlOptions() ProcessControlOptions {
	return ProcessControlOptions{
		CanAttach:       true,                                   // Must attach to existing processes
		CanTerminate:    w.processControlConfig.CanTerminate,    // Based on system control config
		CanRestart:      w.processControlConfig.CanRestart,      // Based on system control config
		Discovery:       w.discoveryConfig,                      // Use configured discovery method
		ExecuteCmd:      nil,                                    // Cannot execute new processes
		Restart:         nil,                                    // No restart configuration for unmanaged
		Limits:          nil,                                    // No resource limits for unmanaged
		GracefulTimeout: w.processControlConfig.GracefulTimeout, // Use configured graceful timeout
		HealthCheck:     &w.healthCheckConfig,                   // Use configured health check
		AllowedSignals:  w.processControlConfig.AllowedSignals,  // Use configured signal permissions
	}
}
