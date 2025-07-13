package domain

import (
	"context"
	"fmt"
	"sync"
	"time"

	coreControl "github.com/core-tools/hsu-core/pkg/control"
	coreDomain "github.com/core-tools/hsu-core/pkg/domain"
	coreLogging "github.com/core-tools/hsu-core/pkg/logging"
	coreProcess "github.com/core-tools/hsu-core/pkg/process"
)

type integratedWorker struct {
	id                    string
	metadata              UnitMetadata
	healthCheckRunOptions HealthCheckRunOptions
	processControl        *integratedProcessControl
}

func NewIntegratedWorker(id string, unit *IntegratedUnit, logger coreLogging.Logger) Worker {
	return &integratedWorker{
		id:                    id,
		metadata:              unit.Metadata,
		healthCheckRunOptions: unit.HealthCheckRunOptions,
		processControl: &integratedProcessControl{
			id:     id,
			config: &unit.Control,
			logger: logger,
		},
	}
}

func (w *integratedWorker) ID() string {
	return w.id
}

func (w *integratedWorker) Metadata() UnitMetadata {
	return w.metadata
}

func (w *integratedWorker) ProcessControl() GenericProcessControl {
	return w.processControl
}

func (w *integratedWorker) HealthCheckConfig() GenericHealthCheckConfig {
	return GenericHealthCheckConfig{
		Type: HealthCheckTypeGRPC,
		GRPC: GRPCHealthCheckConfig{
			Service: w.id,
			Method:  "Ping",
		},
		RunOptions: w.healthCheckRunOptions,
	}
}

func (w *integratedWorker) DiscoveryConfig() GenericDiscoveryConfig {
	return GenericDiscoveryConfig{
		Method:  DiscoveryMethodPIDFile,
		PIDFile: "",
	}
}

type integratedProcessControl struct {
	id         string
	config     *ManagedProcessControl
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
