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
	options  MasterOptions
	server   coreControl.Server
	logger   masterLogging.Logger
	controls map[string]ProcessControl
	mutex    sync.Mutex
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
		options:  options,
		server:   server,
		logger:   masterLogger,
		controls: make(map[string]ProcessControl),
	}

	return master, nil
}

func (m *Master) AddWorker(worker Worker, start bool) error {
	id := worker.ID()

	m.mutex.Lock()
	defer m.mutex.Unlock()

	if _, exists := m.controls[id]; exists {
		return fmt.Errorf("worker already exists")
	}

	logger := masterLogging.NewLogger("worker: "+id+" , ", masterLogging.LogFuncs{
		Debugf: m.logger.Debugf,
		Infof:  m.logger.Infof,
		Warnf:  m.logger.Warnf,
		Errorf: m.logger.Errorf,
	})

	processControl := NewProcessControl(worker.ProcessControlOptions(), logger)

	m.controls[id] = processControl

	if start {
		processControl.Start()
	}

	return nil
}

func (m *Master) RemoveWorker(id string, stop bool) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	processControl, exists := m.controls[id]
	if !exists {
		return fmt.Errorf("worker not found")
	}

	if stop {
		processControl.Stop()
	}

	delete(m.controls, id)
	return nil
}

func (m *Master) StartWorker(id string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	processControl, exists := m.controls[id]
	if !exists {
		return fmt.Errorf("worker not found")
	}

	return processControl.Start()
}

func (m *Master) StopWorker(id string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	processControl, exists := m.controls[id]
	if !exists {
		return fmt.Errorf("worker not found")
	}

	return processControl.Stop()
}

func (m *Master) Run() {
	m.logger.Infof("Starting master...")

	// Start workers
	m.startProcessControls()

	// Start the server (blocks until shutdown)
	m.server.Run(func() {
		m.logger.Infof("Shutting down workers...")
		m.stopProcessControls()
		m.logger.Infof("Workers shutdown complete.")
	})
}

func (m *Master) startProcessControls() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for _, processControl := range m.controls {
		processControl.Start()
	}
}

func (m *Master) stopProcessControls() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for _, processControl := range m.controls {
		processControl.Stop()
	}
}
