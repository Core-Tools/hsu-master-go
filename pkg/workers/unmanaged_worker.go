package workers

import (
	"context"
	"io"
	"os"

	"github.com/core-tools/hsu-master/pkg/logging"
	"github.com/core-tools/hsu-master/pkg/monitoring"
	"github.com/core-tools/hsu-master/pkg/process"
	"github.com/core-tools/hsu-master/pkg/workers/processcontrol"
)

type unmanagedWorker struct {
	id                   string
	metadata             UnitMetadata
	discoveryConfig      process.DiscoveryConfig
	processControlConfig processcontrol.SystemProcessControlConfig
	healthCheckConfig    monitoring.HealthCheckConfig
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

func (w *unmanagedWorker) ProcessControlOptions() processcontrol.ProcessControlOptions {
	w.logger.Debugf("Preparing process control options for unmanaged worker, id: %s, discovery: %s, can_terminate: %t, can_restart: %t",
		w.id, w.discoveryConfig.Method, w.processControlConfig.CanTerminate, w.processControlConfig.CanRestart)

	return processcontrol.ProcessControlOptions{
		CanAttach:           true,                                   // Must attach to existing processes
		CanTerminate:        w.processControlConfig.CanTerminate,    // Based on system control config
		CanRestart:          w.processControlConfig.CanRestart,      // Based on system control config
		ExecuteCmd:          nil,                                    // Cannot execute new processes
		AttachCmd:           w.AttachCmd,                            // Use unit's health check config with logging
		ContextAwareRestart: nil,                                    // No context-aware restart configuration for unmanaged
		RestartPolicy:       processcontrol.RestartNever,            // Unmanaged processes should not auto-restart
		Limits:              nil,                                    // No resource limits for unmanaged
		GracefulTimeout:     w.processControlConfig.GracefulTimeout, // Use configured graceful timeout
		HealthCheck:         nil,                                    // Provided by AttachCmd
		AllowedSignals:      w.processControlConfig.AllowedSignals,  // Use configured signal permissions
	}
}

func (w *unmanagedWorker) AttachCmd(ctx context.Context) (*os.Process, io.ReadCloser, *monitoring.HealthCheckConfig, error) {
	w.logger.Infof("Attaching to unmanaged worker, id: %s", w.id)

	stdAttachCmd := process.NewStdAttachCmd(w.discoveryConfig, w.id, w.logger)
	process, stdout, err := stdAttachCmd(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	w.logger.Infof("Unmanaged worker attached successfully, id: %s, PID: %d", w.id, process.Pid)

	return process, stdout, &w.healthCheckConfig, nil
}
