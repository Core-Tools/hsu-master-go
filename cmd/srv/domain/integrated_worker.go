package domain

import (
	"context"
	"fmt"
	"io"
	"net"
	"os/exec"

	"github.com/core-tools/hsu-core/pkg/logging"
)

type integratedWorker struct {
	id                    string
	metadata              UnitMetadata
	processControlConfig  ManagedProcessControlConfig
	healthCheckRunOptions HealthCheckRunOptions
	logger                logging.Logger
}

func NewIntegratedWorker(id string, unit *IntegratedUnit, logger logging.Logger) Worker {
	return &integratedWorker{
		id:                    id,
		metadata:              unit.Metadata,
		processControlConfig:  unit.Control,
		healthCheckRunOptions: unit.HealthCheckRunOptions,
		logger:                logger,
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
		CanAttach:    true,
		CanTerminate: true,
		CanRestart:   true,
		Discovery: DiscoveryConfig{
			Method:  DiscoveryMethodPIDFile,
			PIDFile: "",
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

	w.logger.Infof("Executing command, config: %+v", w.processControlConfig)

	execution := w.processControlConfig.Execution

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

/*
type integratedProcessControl struct {
	id         string
	config     *ManagedProcessControlConfig
	mutex      sync.Mutex
	port       int
	controller coreProcess.Controller
	gateway    coreDomain.Contract
	running    bool
	logger     coreLogging.Logger
}

func (pc *integratedProcessControl) Start() error {
	pc.mutex.Lock()
	defer pc.mutex.Unlock()

	// Find free port
	port, err := freeport.GetFreePort()
	if err != nil {
		return fmt.Errorf("failed to get free port: %v", err)
	}

	// Start worker process
	args := append(pc.config.Args, "--port", fmt.Sprintf("%d", port))

	logConfig := coreProcess.ControllerLogConfig{
		Module: fmt.Sprintf("worker.%s", pc.id),
		Funcs: coreLogging.LogFuncs{
			Debugf: pc.logger.Debugf,
			Infof:  pc.logger.Infof,
			Warnf:  pc.logger.Warnf,
			Errorf: pc.logger.Errorf,
		},
	}

	controller, err := coreProcess.NewController(
		pc.config.ExecutablePath,
		args,
		10*time.Second, // retry period
		logConfig,
	)
	if err != nil {
		return fmt.Errorf("failed to create process controller: %v", err)
	}

	// Create connection to worker
	connectionOptions := coreControl.ConnectionOptions{
		AttachPort: port,
	}

	connection, err := coreControl.NewConnection(connectionOptions, pc.logger)
	if err != nil {
		controller.Stop()
		return fmt.Errorf("failed to create connection: %v", err)
	}

	gateway := coreControl.NewGRPCClientGateway(connection.GRPC(), pc.logger)

	// Wait for worker to be ready
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	retryOptions := coreDomain.RetryPingOptions{
		RetryAttempts: 30,
		RetryInterval: 1 * time.Second,
	}

	err = coreDomain.RetryPing(ctx, gateway, retryOptions, pc.logger)
	if err != nil {
		controller.Stop()
		return fmt.Errorf("worker failed health check: %v", err)
	}

	// Add to workers list
	pc.controller = controller
	pc.gateway = gateway
	pc.port = port
	pc.running = true

	pc.logger.Infof("Integrated unit %s started successfully on port %d", pc.id, port)
	return nil
}

func (pc *integratedProcessControl) Stop() error {
	pc.mutex.Lock()
	defer pc.mutex.Unlock()

	pc.controller.Stop()
	pc.running = false

	pc.logger.Infof("Integrated unit %s stopped successfully", pc.id)
	return nil
}

func (pc *integratedProcessControl) Restart() error {
	return fmt.Errorf("not implemented")
}
*/
