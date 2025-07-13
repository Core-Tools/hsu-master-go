package domain

import (
	"fmt"
	"sync"

	coreControl "github.com/core-tools/hsu-core/pkg/control"
	coreDomain "github.com/core-tools/hsu-core/pkg/domain"
	coreLogging "github.com/core-tools/hsu-core/pkg/logging"
	masterControl "github.com/core-tools/hsu-master/pkg/control"
	masterLogging "github.com/core-tools/hsu-master/pkg/logging"
)

type MasterOptions struct {
	Port int
}

type Master struct {
	options     MasterOptions
	server      coreControl.Server
	logger      coreLogging.Logger
	running     bool
	workers     []Worker
	healthCheck *HealthCheck
	mutex       sync.Mutex
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
		options:     options,
		server:      server,
		logger:      masterLogger,
		workers:     make([]Worker, 0),
		healthCheck: NewHealthCheck(),
	}

	return master, nil
}

func (m *Master) AddWorker(worker Worker) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.workers = append(m.workers, worker)

	if m.running {
		worker.Start()
	}
}

func (m *Master) Run() {
	m.logger.Infof("Starting master...")

	// Start workers
	m.startWorkers()

	// Start the server (blocks until shutdown)
	m.server.Run(func() {
		m.logger.Infof("Shutting down workers...")
		m.shutdownWorkers()
		m.logger.Infof("Workers shutdown complete.")
	})
}

func (m *Master) startWorkers() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.running {
		return
	}

	m.running = true

	for _, worker := range m.workers {
		worker.Start()
	}
}

func (m *Master) shutdownWorkers() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if !m.running {
		return
	}

	for _, worker := range m.workers {
		worker.Stop()
	}

	m.running = false
}
