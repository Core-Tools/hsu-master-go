package domain

import (
	"context"
	"io"
	"os/exec"
	"time"

	"github.com/core-tools/hsu-master/pkg/logging"
)

type managedWorker struct {
	id                   string
	metadata             UnitMetadata
	processControlConfig ManagedProcessControlConfig
	healthCheckConfig    HealthCheckConfig
	logger               logging.Logger
	pidManager           *ProcessFileManager
}

func NewManagedWorker(id string, unit *ManagedUnit, logger logging.Logger) Worker {
	// Get PID file configuration from unit or use default
	pidConfig := unit.Control.Execution.ProcessFileConfig
	if pidConfig == nil {
		// Use default system service configuration
		defaultConfig := GetRecommendedProcessFileConfig("system", DefaultAppName)
		pidConfig = &defaultConfig
	}

	return &managedWorker{
		id:                   id,
		metadata:             unit.Metadata,
		processControlConfig: unit.Control,
		healthCheckConfig:    unit.HealthCheck,
		logger:               logger,
		pidManager:           NewProcessFileManager(*pidConfig),
	}
}

func (w *managedWorker) ID() string {
	return w.id
}

func (w *managedWorker) Metadata() UnitMetadata {
	return w.metadata
}

func (w *managedWorker) ProcessControlOptions() ProcessControlOptions {
	pidFile := w.pidManager.GeneratePIDFilePath(w.id)

	return ProcessControlOptions{
		CanAttach:    true, // Can attach to existing processes as fallback
		CanTerminate: true, // Can terminate processes
		CanRestart:   true, // Can restart processes
		Discovery: DiscoveryConfig{
			Method:        DiscoveryMethodPIDFile,
			PIDFile:       pidFile,
			CheckInterval: 30 * time.Second,
		},
		ExecuteCmd:      w.ExecuteCmd,
		AttachCmd:       NewStdAttachCmd(&w.healthCheckConfig), // Use unit's health check config
		Restart:         &w.processControlConfig.Restart,
		Limits:          &w.processControlConfig.Limits,
		GracefulTimeout: w.processControlConfig.GracefulTimeout,
		HealthCheck:     nil, // Provided by ExecuteCmd or AttachCmd
	}
}

func (w *managedWorker) ExecuteCmd(ctx context.Context) (*exec.Cmd, io.ReadCloser, *HealthCheckConfig, error) {
	// Validate context
	if ctx == nil {
		return nil, nil, nil, NewValidationError("context cannot be nil", nil)
	}

	w.logger.Infof("Executing managed worker command, id: %s, config: %+v", w.id, w.processControlConfig)

	execution := w.processControlConfig.Execution

	// Create the standard execute command
	stdCmd := NewStdExecuteCmd(execution, w.logger)
	cmd, stdout, err := stdCmd(ctx)
	if err != nil {
		return nil, nil, nil, NewProcessError("failed to execute managed worker command", err).WithContext("worker_id", w.id)
	}

	// Write PID file
	if err := w.pidManager.WritePIDFile(w.id, cmd.Process.Pid); err != nil {
		// Log error but don't fail - the process is already running
		w.logger.Errorf("Failed to write PID file for worker %s: %v", w.id, err)
	} else {
		pidFile := w.pidManager.GeneratePIDFilePath(w.id)
		w.logger.Infof("PID file written for worker %s: %s (PID: %d)", w.id, pidFile, cmd.Process.Pid)
	}

	// Create health check configuration based on the unit's health check settings
	healthCheckConfig := &w.healthCheckConfig

	w.logger.Infof("Managed worker command executed successfully, id: %s, PID: %d", w.id, cmd.Process.Pid)

	return cmd, stdout, healthCheckConfig, nil
}
