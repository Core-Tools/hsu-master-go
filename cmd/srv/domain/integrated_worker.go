package domain

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/core-tools/hsu-master/pkg/logging"
)

type integratedWorker struct {
	id                    string
	metadata              UnitMetadata
	processControlConfig  ManagedProcessControlConfig
	healthCheckRunOptions HealthCheckRunOptions
	logger                logging.Logger
	pidManager            *ProcessFileManager
}

func NewIntegratedWorker(id string, unit *IntegratedUnit, logger logging.Logger) Worker {
	// Get PID file configuration from unit or use default
	pidConfig := unit.Control.Execution.ProcessFileConfig
	if pidConfig == nil {
		// Use default system service configuration
		defaultConfig := GetRecommendedProcessFileConfig("system", DefaultAppName)
		pidConfig = &defaultConfig
	}

	return &integratedWorker{
		id:                    id,
		metadata:              unit.Metadata,
		processControlConfig:  unit.Control,
		healthCheckRunOptions: unit.HealthCheckRunOptions,
		logger:                logger,
		pidManager:            NewProcessFileManager(*pidConfig, logger),
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
			Method:        DiscoveryMethodPIDFile,
			PIDFile:       pidFile,
			CheckInterval: 30 * time.Second,
		},
		ExecuteCmd:      w.ExecuteCmd,
		AttachCmd:       w.AttachCmd, // Use custom AttachCmd that creates dynamic gRPC health check
		Restart:         &w.processControlConfig.Restart,
		Limits:          &w.processControlConfig.Limits,
		GracefulTimeout: w.processControlConfig.GracefulTimeout,
		HealthCheck:     nil, // Provided by ExecuteCmd or AttachCmd
	}
}

// AttachCmd creates a dynamic gRPC health check configuration by reading the port file
func (w *integratedWorker) AttachCmd(config DiscoveryConfig) (*os.Process, io.ReadCloser, *HealthCheckConfig, error) {
	w.logger.Infof("Executing integrated worker attach command, id: %s, config: %+v", w.id, config)

	// Use standard attachment to discover the process
	stdAttachCmd := NewStdAttachCmd(nil, w.logger, w.id) // No health check config yet, but include logging
	process, stdout, _, err := stdAttachCmd(config)      // Updated to match new signature
	if err != nil {
		return nil, nil, nil, err
	}

	// Read the port from the port file
	port, err := w.pidManager.ReadPortFile(w.id)
	if err != nil {
		w.logger.Warnf("Failed to read port file for worker %s: %v, using default port 50051", w.id, err)
		port = 50051 // Default fallback port
	}

	portStr := fmt.Sprintf("%d", port)

	// Create dynamic gRPC health check configuration
	healthCheckConfig := &HealthCheckConfig{
		Type: HealthCheckTypeGRPC,
		GRPC: GRPCHealthCheckConfig{
			Address: "localhost:" + portStr,
			Service: "CoreService",
			Method:  "Ping",
		},
		RunOptions: w.healthCheckRunOptions,
	}

	w.logger.Infof("Created dynamic gRPC health check for attached process %d: %s", process.Pid, healthCheckConfig.GRPC.Address)

	return process, stdout, healthCheckConfig, nil
}

func (w *integratedWorker) ExecuteCmd(ctx context.Context) (*exec.Cmd, io.ReadCloser, *HealthCheckConfig, error) {
	// Validate context
	if ctx == nil {
		return nil, nil, nil, NewValidationError("context cannot be nil", nil)
	}

	w.logger.Infof("Executing integrated worker command, id: %s", w.id)

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

	// Write port file
	if err := w.pidManager.WritePortFile(w.id, port); err != nil {
		// Log error but don't fail - the process is already running
		w.logger.Errorf("Failed to write port file for worker %s: %v", w.id, err)
	} else {
		portFile := w.pidManager.GeneratePortFilePath(w.id)
		w.logger.Infof("Port file written for worker %s: %s (port: %d)", w.id, portFile, port)
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
