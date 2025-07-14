package domain

import (
	"context"
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

func (m *Master) AddWorker(worker Worker) error {
	if worker == nil {
		return NewValidationError("worker cannot be nil", nil)
	}

	id := worker.ID()

	// Validate worker ID
	if err := ValidateWorkerID(id); err != nil {
		return NewValidationError("invalid worker ID", err).WithContext("worker_id", id)
	}

	// Validate worker options
	options := worker.ProcessControlOptions()
	if err := ValidateProcessControlOptions(options); err != nil {
		return NewValidationError("invalid worker process control options", err).WithContext("worker_id", id)
	}

	m.logger.Infof("Adding worker, id: %s", id)

	m.mutex.Lock()
	defer m.mutex.Unlock()

	if _, exists := m.controls[id]; exists {
		return NewConflictError("worker already exists", nil).WithContext("worker_id", id)
	}

	logger := masterLogging.NewLogger("worker: "+id+" , ", masterLogging.LogFuncs{
		Debugf: m.logger.Debugf,
		Infof:  m.logger.Infof,
		Warnf:  m.logger.Warnf,
		Errorf: m.logger.Errorf,
	})

	processControl := NewProcessControl(options, logger)

	m.controls[id] = processControl

	m.logger.Infof("Worker added successfully, id: %s", id)
	return nil
}

func (m *Master) RemoveWorker(id string) error {
	// Validate worker ID
	if err := ValidateWorkerID(id); err != nil {
		return NewValidationError("invalid worker ID", err).WithContext("worker_id", id)
	}

	m.logger.Infof("Removing worker, id: %s", id)

	m.mutex.Lock()
	defer m.mutex.Unlock()

	_, exists := m.controls[id]
	if !exists {
		return NewNotFoundError("worker not found", nil).WithContext("worker_id", id)
	}

	delete(m.controls, id)

	m.logger.Infof("Worker removed successfully, id: %s", id)
	return nil
}

func (m *Master) StartWorker(ctx context.Context, id string) error {
	// Validate context
	if ctx == nil {
		return NewValidationError("context cannot be nil", nil)
	}

	// Validate worker ID
	if err := ValidateWorkerID(id); err != nil {
		return NewValidationError("invalid worker ID", err).WithContext("worker_id", id)
	}

	m.logger.Infof("Starting worker, id: %s", id)

	// 1. Get process control under lock
	processControl, exists := m.getControl(id)
	if !exists {
		return NewNotFoundError("worker not found", nil).WithContext("worker_id", id)
	}

	// 2. Start outside of lock (can be long-running)
	err := processControl.Start(ctx)
	if err != nil {
		m.logger.Errorf("Failed to start worker, id: %s, error: %v", id, err)
		// Check if the error is a context cancellation
		if ctx.Err() != nil {
			return NewCancelledError("worker start was cancelled", ctx.Err()).WithContext("worker_id", id)
		}
		return NewProcessError("failed to start worker", err).WithContext("worker_id", id)
	}

	m.logger.Infof("Worker started successfully, id: %s", id)
	return nil
}

func (m *Master) StopWorker(ctx context.Context, id string) error {
	// Validate context
	if ctx == nil {
		return NewValidationError("context cannot be nil", nil)
	}

	// Validate worker ID
	if err := ValidateWorkerID(id); err != nil {
		return NewValidationError("invalid worker ID", err).WithContext("worker_id", id)
	}

	m.logger.Infof("Stopping worker, id: %s", id)

	// 1. Get process control under lock
	processControl, exists := m.getControl(id)
	if !exists {
		return NewNotFoundError("worker not found", nil).WithContext("worker_id", id)
	}

	// 2. Stop outside of lock (can be long-running)
	err := processControl.Stop(ctx)
	if err != nil {
		m.logger.Errorf("Failed to stop worker, id: %s, error: %v", id, err)
		// Check if the error is a context cancellation
		if ctx.Err() != nil {
			return NewCancelledError("worker stop was cancelled", ctx.Err()).WithContext("worker_id", id)
		}
		return NewProcessError("failed to stop worker", err).WithContext("worker_id", id)
	}

	m.logger.Infof("Worker stopped successfully, id: %s", id)
	return nil
}

func (m *Master) Run(ctx context.Context) {
	m.logger.Infof("Starting master...")

	// Start workers with provided context
	m.startProcessControls(ctx)

	// Start the server (blocks until shutdown)
	m.server.Run(func() {
		m.logger.Infof("Shutting down workers...")
		// Use provided context for shutdown operations
		// The context allows the caller to control timeouts and cancellation
		m.stopProcessControls(ctx)
		m.logger.Infof("Workers shutdown complete.")
	})
}

func (m *Master) startProcessControls(ctx context.Context) {
	m.logger.Infof("Starting process controls...")

	// 1. Get all process controls under lock
	controlsCopy := m.getAllControls()

	// 2. Start processes outside of lock
	errorCollection := NewErrorCollection()
	for id, processControl := range controlsCopy {
		err := processControl.Start(ctx)
		if err != nil {
			m.logger.Errorf("Failed to start process control, id: %s, error: %v", id, err)
			// Add context to the error for better debugging
			contextualErr := NewProcessError("failed to start process control", err).WithContext("worker_id", id)
			errorCollection.Add(contextualErr)
		}
	}

	if errorCollection.HasErrors() {
		m.logger.Errorf("Some process controls failed to start: %v", errorCollection.Error())
	}

	m.logger.Infof("Process controls started.")
}

func (m *Master) stopProcessControls(ctx context.Context) {
	m.logger.Infof("Stopping process controls...")

	// 1. Get all process controls under lock
	controlsCopy := m.getAllControls()

	// 2. Stop processes outside of lock
	errorCollection := NewErrorCollection()
	for id, processControl := range controlsCopy {
		err := processControl.Stop(ctx)
		if err != nil {
			m.logger.Errorf("Failed to stop process control, id: %s, error: %v", id, err)
			// Add context to the error for better debugging
			contextualErr := NewProcessError("failed to stop process control", err).WithContext("worker_id", id)
			errorCollection.Add(contextualErr)
		}
	}

	if errorCollection.HasErrors() {
		m.logger.Errorf("Some process controls failed to stop: %v", errorCollection.Error())
	}

	m.logger.Infof("Process controls stopped.")
}

// getControl returns a single process control under lock
func (m *Master) getControl(id string) (ProcessControl, bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	processControl, exists := m.controls[id]
	return processControl, exists
}

// getAllControls returns a copy of all process controls under lock
func (m *Master) getAllControls() map[string]ProcessControl {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	controlsCopy := make(map[string]ProcessControl)
	for id, processControl := range m.controls {
		controlsCopy[id] = processControl
	}
	return controlsCopy
}
