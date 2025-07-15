package domain

import (
	"github.com/core-tools/hsu-master/pkg/logging"
)

type unmanagedWorker struct {
	id     string
	unit   *UnmanagedUnit
	logger logging.Logger
}

func NewUnmanagedWorker(id string, unit *UnmanagedUnit, logger logging.Logger) Worker {
	return &unmanagedWorker{
		id:     id,
		unit:   unit,
		logger: logger,
	}
}

func (w *unmanagedWorker) ID() string {
	return w.id
}

func (w *unmanagedWorker) Metadata() UnitMetadata {
	return w.unit.Metadata
}

func (w *unmanagedWorker) ProcessControlOptions() ProcessControlOptions {
	return ProcessControlOptions{
		CanAttach:       true,                           // Must attach to existing processes
		CanTerminate:    w.unit.Control.CanTerminate,    // Based on system control config
		CanRestart:      w.unit.Control.CanRestart,      // Based on system control config
		Discovery:       w.unit.Discovery,               // Use configured discovery method
		ExecuteCmd:      nil,                            // Cannot execute new processes
		Restart:         nil,                            // No restart configuration for unmanaged
		Limits:          nil,                            // No resource limits for unmanaged
		GracefulTimeout: w.unit.Control.GracefulTimeout, // Use configured graceful timeout
		HealthCheck:     &w.unit.HealthCheck,            // Use configured health check
		AllowedSignals:  w.unit.Control.AllowedSignals,  // Use configured signal permissions
	}
}
