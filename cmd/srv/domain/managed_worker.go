package domain

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/core-tools/hsu-master/pkg/errors"
	"github.com/core-tools/hsu-master/pkg/logging"
	"github.com/core-tools/hsu-master/pkg/process"
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
	pidConfig := unit.Control.ProcessFile
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
		pidManager:           NewProcessFileManager(*pidConfig, logger),
	}
}

func (w *managedWorker) ID() string {
	return w.id
}

func (w *managedWorker) Metadata() UnitMetadata {
	return w.metadata
}

func (w *managedWorker) ProcessControlOptions() ProcessControlOptions {
	return ProcessControlOptions{
		CanAttach:       true, // Can attach to existing processes as fallback
		CanTerminate:    true, // Can terminate processes
		CanRestart:      true, // Can restart processes
		ExecuteCmd:      w.ExecuteCmd,
		AttachCmd:       w.AttachCmd,
		Restart:         &w.processControlConfig.Restart,
		Limits:          &w.processControlConfig.Limits,
		GracefulTimeout: w.processControlConfig.GracefulTimeout,
		HealthCheck:     nil, // Provided by ExecuteCmd or AttachCmd
	}
}

func (w *managedWorker) AttachCmd(ctx context.Context) (*os.Process, io.ReadCloser, *HealthCheckConfig, error) {
	w.logger.Infof("Attaching to managed worker, id: %s", w.id)

	healthCheck := w.healthCheckConfig

	// Validate health check configuration
	if err := ValidateHealthCheckConfig(healthCheck); err != nil {
		w.logger.Errorf("Managed worker health check configuration validation failed, id: %s, error: %v", w.id, err)
		return nil, nil, nil, errors.NewValidationError("invalid health check configuration", err).WithContext("id", w.id)
	}

	pidFile := w.pidManager.GeneratePIDFilePath(w.id)

	discovery := DiscoveryConfig{
		Method:        DiscoveryMethodPIDFile,
		PIDFile:       pidFile,
		CheckInterval: 30 * time.Second,
	}
	stdAttachCmd := NewStdAttachCmd(discovery, w.id, w.logger)
	process, stdout, err := stdAttachCmd(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	w.logger.Infof("Managed worker attached successfully, id: %s, PID: %d", w.id, process.Pid)

	return process, stdout, &healthCheck, nil
}

func (w *managedWorker) ExecuteCmd(ctx context.Context) (*os.Process, io.ReadCloser, *HealthCheckConfig, error) {
	w.logger.Infof("Executing managed worker command, id: %s", w.id)

	execution := w.processControlConfig.Execution
	healthCheck := w.healthCheckConfig

	// Validate execution configuration
	if err := ValidateExecutionConfig(execution); err != nil {
		w.logger.Errorf("Managed worker execution configuration validation failed, id: %s, error: %v", w.id, err)
		return nil, nil, nil, errors.NewValidationError("invalid execution configuration", err).WithContext("id", w.id)
	}

	// Validate health check configuration
	if err := ValidateHealthCheckConfig(healthCheck); err != nil {
		w.logger.Errorf("Managed worker health check configuration validation failed, id: %s, error: %v", w.id, err)
		return nil, nil, nil, errors.NewValidationError("invalid health check configuration", err).WithContext("id", w.id)
	}

	// Create the standard execute command

	processExecution := process.ExecutionConfig{
		ExecutablePath:   execution.ExecutablePath,
		Args:             execution.Args,
		Environment:      execution.Environment,
		WorkingDirectory: execution.WorkingDirectory,
		WaitDelay:        execution.WaitDelay,
	}

	stdExecuteCmd := process.NewStdExecuteCmd(processExecution, w.id, w.logger)
	process, stdout, err := stdExecuteCmd(ctx)
	if err != nil {
		return nil, nil, nil, errors.NewProcessError("failed to execute managed worker command", err).WithContext("worker_id", w.id)
	}

	// Write PID file
	if err := w.pidManager.WritePIDFile(w.id, process.Pid); err != nil {
		// Log error but don't fail - the process is already running
		w.logger.Errorf("Failed to write PID file for worker %s: %v", w.id, err)
	} else {
		pidFile := w.pidManager.GeneratePIDFilePath(w.id)
		w.logger.Infof("PID file written for worker %s: %s (PID: %d)", w.id, pidFile, process.Pid)
	}

	w.logger.Infof("Managed worker command executed successfully, id: %s, PID: %d", w.id, process.Pid)

	return process, stdout, &healthCheck, nil
}
