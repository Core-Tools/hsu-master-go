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
	masterControl "github.com/core-tools/hsu-master/pkg/control"
	masterLogging "github.com/core-tools/hsu-master/pkg/logging"

	"github.com/phayes/freeport"
)

type MasterOptions struct {
	Port        int
	WorkerPath  string
	WorkerCount int
}

type Master struct {
	options      MasterOptions
	server       coreControl.Server
	logger       coreLogging.Logger
	workers      []Worker
	workersMutex sync.RWMutex
}

type Worker struct {
	ID         string
	Port       int
	Controller coreProcess.Controller
	Gateway    coreDomain.Contract
	Status     string
}

func NewMaster(options MasterOptions, coreLogger coreLogging.Logger, masterLogger masterLogging.Logger) (*Master, error) {
	// Create gRPC server
	serverOptions := coreControl.ServerOptions{
		Port: options.Port,
	}

	server, err := coreControl.NewServer(serverOptions, coreLogger)
	if err != nil {
		return nil, fmt.Errorf("failed to create server: %v", err)
	}

	// Register core services
	coreHandler := coreDomain.NewDefaultHandler(coreLogger)
	coreControl.RegisterGRPCServerHandler(server.GRPC(), coreHandler, coreLogger)

	// Register business logic services
	masterHandler := NewMasterHandler(masterLogger)
	masterControl.RegisterGRPCServerHandler(server.GRPC(), masterHandler, masterLogger)

	master := &Master{
		options: options,
		server:  server,
		logger:  masterLogger,
		workers: make([]Worker, 0),
	}

	return master, nil
}

func (m *Master) Run() {
	m.logger.Infof("Starting master...")

	// Start worker management in background
	go m.manageWorkers()

	// Start the server (blocks until shutdown)
	m.server.Run(func() {
		m.logger.Infof("Shutting down workers...")
		m.shutdownWorkers()
	})
}

func (m *Master) manageWorkers() {
	if m.options.WorkerPath == "" {
		m.logger.Infof("No worker path specified, running in standalone mode")
		return
	}

	m.logger.Infof("Starting %d workers from %s", m.options.WorkerCount, m.options.WorkerPath)

	// Start initial workers
	for i := 0; i < m.options.WorkerCount; i++ {
		err := m.startWorker(fmt.Sprintf("worker-%d", i))
		if err != nil {
			m.logger.Errorf("Failed to start worker %d: %v", i, err)
		}
	}

	// Monitor workers
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.healthCheckWorkers()
		}
	}
}

func (m *Master) startWorker(workerID string) error {
	// Find free port
	port, err := freeport.GetFreePort()
	if err != nil {
		return fmt.Errorf("failed to get free port: %v", err)
	}

	// Start worker process
	args := []string{"--port", fmt.Sprintf("%d", port)}

	logConfig := coreProcess.ControllerLogConfig{
		Module: fmt.Sprintf("worker.%s", workerID),
		Funcs: coreLogging.LogFuncs{
			Debugf: m.logger.Debugf,
			Infof:  m.logger.Infof,
			Warnf:  m.logger.Warnf,
			Errorf: m.logger.Errorf,
		},
	}

	controller, err := coreProcess.NewController(
		m.options.WorkerPath,
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

	connection, err := coreControl.NewConnection(connectionOptions, m.logger)
	if err != nil {
		controller.Stop()
		return fmt.Errorf("failed to create connection: %v", err)
	}

	gateway := coreControl.NewGRPCClientGateway(connection.GRPC(), m.logger)

	// Wait for worker to be ready
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	retryOptions := coreDomain.RetryPingOptions{
		RetryAttempts: 30,
		RetryInterval: 1 * time.Second,
	}

	err = coreDomain.RetryPing(ctx, gateway, retryOptions, m.logger)
	if err != nil {
		controller.Stop()
		return fmt.Errorf("worker failed health check: %v", err)
	}

	// Add to workers list
	worker := Worker{
		ID:         workerID,
		Port:       port,
		Controller: controller,
		Gateway:    gateway,
		Status:     "running",
	}

	m.workersMutex.Lock()
	m.workers = append(m.workers, worker)
	m.workersMutex.Unlock()

	m.logger.Infof("Worker %s started successfully on port %d", workerID, port)
	return nil
}

func (m *Master) healthCheckWorkers() {
	m.workersMutex.RLock()
	workers := make([]Worker, len(m.workers))
	copy(workers, m.workers)
	m.workersMutex.RUnlock()

	for i, worker := range workers {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := worker.Gateway.Ping(ctx)
		cancel()

		if err != nil {
			m.logger.Warnf("Worker %s health check failed: %v", worker.ID, err)
			// Update status
			m.workersMutex.Lock()
			if i < len(m.workers) {
				m.workers[i].Status = "unhealthy"
			}
			m.workersMutex.Unlock()
		} else {
			m.workersMutex.Lock()
			if i < len(m.workers) {
				m.workers[i].Status = "running"
			}
			m.workersMutex.Unlock()
		}
	}
}

func (m *Master) shutdownWorkers() {
	m.workersMutex.Lock()
	defer m.workersMutex.Unlock()

	for _, worker := range m.workers {
		m.logger.Infof("Stopping worker %s", worker.ID)
		worker.Controller.Stop()
	}

	m.workers = nil
}
