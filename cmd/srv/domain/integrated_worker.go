package domain

import (
	"context"
	"fmt"
	"io"
	"net"
	"os/exec"

	"github.com/core-tools/hsu-master/pkg/logging"
)

type integratedWorker struct {
	id                    string
	metadata              UnitMetadata
	processControlConfig  ManagedProcessControlConfig
	healthCheckRunOptions HealthCheckRunOptions
	logger                logging.Logger
	pidManager            *PIDFileManager
}

func NewIntegratedWorker(id string, unit *IntegratedUnit, logger logging.Logger) Worker {
	// Get PID file configuration from unit or use default
	pidConfig := unit.Control.Execution.PIDFileConfig
	if pidConfig == nil {
		// Use default system service configuration
		defaultConfig := GetRecommendedPIDFileConfig("system", DefaultAppName)
		pidConfig = &defaultConfig
	}

	return &integratedWorker{
		id:                    id,
		metadata:              unit.Metadata,
		processControlConfig:  unit.Control,
		healthCheckRunOptions: unit.HealthCheckRunOptions,
		logger:                logger,
		pidManager:            NewPIDFileManager(*pidConfig),
	}
}

func (w *integratedWorker) ID() string {
	return w.id
}

func (w *integratedWorker) Metadata() UnitMetadata {
	return w.metadata
}

func (w *integratedWorker) ProcessControlOptions() ProcessControlOptions {
	pidFile := w.pidManager.GeneratePIDFilePath(w.id)

	return ProcessControlOptions{
		CanAttach:    true,
		CanTerminate: true,
		CanRestart:   true,
		Discovery: DiscoveryConfig{
			Method:  DiscoveryMethodPIDFile,
			PIDFile: pidFile,
		},
		ExecuteCmd:      w.ExecuteCmd,
		Restart:         &w.processControlConfig.Restart,
		Limits:          &w.processControlConfig.Limits,
		GracefulTimeout: w.processControlConfig.GracefulTimeout,
		HealthCheck:     nil, // returned by ExecuteCmd
	}
}

func (w *integratedWorker) ExecuteCmd(ctx context.Context) (*exec.Cmd, io.ReadCloser, *HealthCheckConfig, error) {
	// Validate context
	if ctx == nil {
		return nil, nil, nil, NewValidationError("context cannot be nil", nil)
	}

	w.logger.Infof("Executing integrated worker command, id: %s, config: %+v", w.id, w.processControlConfig)

	execution := w.processControlConfig.Execution

	// Get a free port for the gRPC server
	port, err := getFreePort()
	if err != nil {
		return nil, nil, nil, NewNetworkError("failed to get free port", err)
	}
	portStr := fmt.Sprintf("%d", port)

	w.logger.Infof("Got free port, port: %d", port)

	execution.Args = append(execution.Args, "--port", portStr)

	stdCmd := NewStdExecuteCmd(execution, w.logger)
	cmd, stdout, err := stdCmd(ctx)
	if err != nil {
		return nil, nil, nil, NewProcessError("failed to execute command", err).WithContext("port", port)
	}

	// Write PID file
	if err := w.pidManager.WritePIDFile(w.id, cmd.Process.Pid); err != nil {
		// Log error but don't fail - the process is already running
		w.logger.Errorf("Failed to write PID file for worker %s: %v", w.id, err)
	} else {
		pidFile := w.pidManager.GeneratePIDFilePath(w.id)
		w.logger.Infof("PID file written for worker %s: %s (PID: %d)", w.id, pidFile, cmd.Process.Pid)
	}

	healthCheckConfig := &HealthCheckConfig{
		Type: HealthCheckTypeGRPC,
		GRPC: GRPCHealthCheckConfig{
			Address: "localhost:" + portStr,
			Service: "CoreService",
			Method:  "Ping",
		},
		RunOptions: w.healthCheckRunOptions,
	}

	return cmd, stdout, healthCheckConfig, nil
}

func getFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, NewNetworkError("failed to resolve TCP address", err)
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, NewNetworkError("failed to listen on TCP address", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
