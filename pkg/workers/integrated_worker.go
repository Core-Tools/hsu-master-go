package workers

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/core-tools/hsu-master/pkg/errors"
	"github.com/core-tools/hsu-master/pkg/logging"
	"github.com/core-tools/hsu-master/pkg/monitoring"
	"github.com/core-tools/hsu-master/pkg/process"
	"github.com/core-tools/hsu-master/pkg/processfile"
)

type integratedWorker struct {
	id                    string
	metadata              UnitMetadata
	processControlConfig  ManagedProcessControlConfig
	healthCheckRunOptions monitoring.HealthCheckRunOptions
	logger                logging.Logger
	pidManager            *processfile.ProcessFileManager
}

func NewIntegratedWorker(id string, unit *IntegratedUnit, logger logging.Logger) Worker {
	// Get PID file configuration from unit or use default
	pidConfig := unit.Control.ProcessFile
	if pidConfig == nil {
		// Use default system service configuration
		defaultConfig := processfile.GetRecommendedProcessFileConfig("system", "") // use default app name
		pidConfig = &defaultConfig
	}

	return &integratedWorker{
		id:                    id,
		metadata:              unit.Metadata,
		processControlConfig:  unit.Control,
		healthCheckRunOptions: unit.HealthCheckRunOptions,
		logger:                logger,
		pidManager:            processfile.NewProcessFileManager(*pidConfig, logger),
	}
}

func (w *integratedWorker) ID() string {
	return w.id
}

func (w *integratedWorker) Metadata() UnitMetadata {
	return w.metadata
}

func (w *integratedWorker) ProcessControlOptions() ProcessControlOptions {
	return ProcessControlOptions{
		CanAttach:       true,
		CanTerminate:    true,
		CanRestart:      true,
		ExecuteCmd:      w.ExecuteCmd,
		AttachCmd:       w.AttachCmd, // Use custom AttachCmd that creates dynamic gRPC health check
		Restart:         &w.processControlConfig.Restart,
		Limits:          &w.processControlConfig.Limits,
		GracefulTimeout: w.processControlConfig.GracefulTimeout,
		HealthCheck:     nil, // Provided by ExecuteCmd or AttachCmd
	}
}

// AttachCmd creates a dynamic gRPC health check configuration by reading the port file
func (w *integratedWorker) AttachCmd(ctx context.Context) (*os.Process, io.ReadCloser, *monitoring.HealthCheckConfig, error) {
	w.logger.Infof("Attaching to integrated worker, id: %s", w.id)

	pidFile := w.pidManager.GeneratePIDFilePath(w.id)

	// Use standard attachment to discover the process
	discovery := process.DiscoveryConfig{
		Method:        process.DiscoveryMethodPIDFile,
		PIDFile:       pidFile,
		CheckInterval: 30 * time.Second,
	}
	stdAttachCmd := process.NewStdAttachCmd(discovery, w.id, w.logger)
	process, stdout, err := stdAttachCmd(ctx)
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

	healthCheckConfig := w.newDynamicHealthCheckConfig(portStr, process.Pid)

	w.logger.Infof("Integrated worker attached successfully, id: %s, PID: %d", w.id, process.Pid)

	return process, stdout, healthCheckConfig, nil
}

func (w *integratedWorker) ExecuteCmd(ctx context.Context) (*os.Process, io.ReadCloser, *monitoring.HealthCheckConfig, error) {
	w.logger.Infof("Executing integrated worker command, id: %s", w.id)

	// Get a free port for the gRPC server
	port, err := getFreePort()
	if err != nil {
		return nil, nil, nil, errors.NewNetworkError("failed to get free port", err)
	}
	portStr := fmt.Sprintf("%d", port)

	w.logger.Infof("Got free port, port: %d", port)

	executionConfig := w.processControlConfig.Execution
	executionConfig.Args = append(executionConfig.Args, "--port", portStr)

	stdExecuteCmd := process.NewStdExecuteCmd(executionConfig, w.id, w.logger)
	process, stdout, err := stdExecuteCmd(ctx)
	if err != nil {
		return nil, nil, nil, errors.NewProcessError("failed to execute command", err).WithContext("port", port)
	}

	// Write PID file
	if err := w.pidManager.WritePIDFile(w.id, process.Pid); err != nil {
		// Log error but don't fail - the process is already running
		w.logger.Errorf("Failed to write PID file for worker %s: %v", w.id, err)
	} else {
		pidFile := w.pidManager.GeneratePIDFilePath(w.id)
		w.logger.Infof("PID file written for worker %s: %s (PID: %d)", w.id, pidFile, process.Pid)
	}

	// Write port file
	if err := w.pidManager.WritePortFile(w.id, port); err != nil {
		// Log error but don't fail - the process is already running
		w.logger.Errorf("Failed to write port file for worker %s: %v", w.id, err)
	} else {
		portFile := w.pidManager.GeneratePortFilePath(w.id)
		w.logger.Infof("Port file written for worker %s: %s (port: %d)", w.id, portFile, port)
	}

	healthCheckConfig := w.newDynamicHealthCheckConfig(portStr, process.Pid)

	w.logger.Infof("Integrated worker executed successfully, id: %s, PID: %d", w.id, process.Pid)

	return process, stdout, healthCheckConfig, nil
}

func (w *integratedWorker) newDynamicHealthCheckConfig(portStr string, pid int) *monitoring.HealthCheckConfig {
	healthCheckConfig := &monitoring.HealthCheckConfig{
		Type: monitoring.HealthCheckTypeGRPC,
		GRPC: monitoring.GRPCHealthCheckConfig{
			Address: "localhost:" + portStr,
			Service: "CoreService",
			Method:  "Ping",
		},
		RunOptions: w.healthCheckRunOptions,
	}

	w.logger.Infof("Created dynamic gRPC health check for executed process %d: %v", pid, healthCheckConfig)

	return healthCheckConfig
}

func getFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, errors.NewNetworkError("failed to resolve TCP address", err)
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, errors.NewNetworkError("failed to listen on TCP address", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
