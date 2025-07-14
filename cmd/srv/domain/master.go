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

	m.logger.Infof("Adding worker, id: %s", id)

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
		err := processControl.Start()
		if err != nil {
			m.logger.Errorf("Failed to start worker, id: %s, error: %v", id, err)
		}
	}

	return nil
}

func (m *Master) RemoveWorker(id string, stop bool) error {
	m.logger.Infof("Removing worker, id: %s", id)

	m.mutex.Lock()
	defer m.mutex.Unlock()

	processControl, exists := m.controls[id]
	if !exists {
		return fmt.Errorf("worker not found")
	}

	if stop {
		err := processControl.Stop()
		if err != nil {
			m.logger.Errorf("Failed to stop worker, id: %s, error: %v", id, err)
		}
	}

	delete(m.controls, id)
	return nil
}

func (m *Master) StartWorker(id string) error {
	m.logger.Infof("Starting worker, id: %s", id)

	m.mutex.Lock()
	defer m.mutex.Unlock()

	processControl, exists := m.controls[id]
	if !exists {
		return fmt.Errorf("worker not found")
	}

	err := processControl.Start()
	if err != nil {
		m.logger.Errorf("Failed to start worker, id: %s, error: %v", id, err)
	}
	return err
}

func (m *Master) StopWorker(id string) error {
	m.logger.Infof("Stopping worker, id: %s", id)

	m.mutex.Lock()
	defer m.mutex.Unlock()

	processControl, exists := m.controls[id]
	if !exists {
		return fmt.Errorf("worker not found")
	}

	err := processControl.Stop()
	if err != nil {
		m.logger.Errorf("Failed to stop worker, id: %s, error: %v", id, err)
	}
	return err
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
	m.logger.Infof("Starting process controls...")

	m.mutex.Lock()
	defer m.mutex.Unlock()

	for id, processControl := range m.controls {
		err := processControl.Start()
		if err != nil {
			m.logger.Errorf("Failed to start process control, id: %s, error: %v", id, err)
		}
	}

	m.logger.Infof("Process controls started.")
}

func (m *Master) stopProcessControls() {
	m.logger.Infof("Stopping process controls...")

	m.mutex.Lock()
	defer m.mutex.Unlock()

	for id, processControl := range m.controls {
		err := processControl.Stop()
		if err != nil {
			m.logger.Errorf("Failed to stop process control, id: %s, error: %v", id, err)
		}
	}

	m.logger.Infof("Process controls stopped.")
}
