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
	options       MasterOptions
	server        coreControl.Server
	logger        masterLogging.Logger
	controls      map[string]ProcessControl
	stateMachines map[string]*WorkerStateMachine // State machines for each worker
	mutex         sync.Mutex
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
		options:       options,
		server:        server,
		logger:        masterLogger,
		controls:      make(map[string]ProcessControl),
		stateMachines: make(map[string]*WorkerStateMachine), // Initialize state machines map
		mutex:         sync.Mutex{},
	}

	return master, nil
}

func (m *Master) AddWorker(worker Worker) error {
	// Validate input
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

	m.logger.Infof("Adding worker, id: %s, can_attach: %t, can_execute: %t, can_terminate: %t, can_restart: %t",
		id, options.CanAttach, (options.ExecuteCmd != nil), options.CanTerminate, options.CanRestart)

	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Check if worker already exists
	if _, exists := m.controls[id]; exists {
		return NewConflictError("worker already exists", nil).WithContext("worker_id", id)
	}

	// Create state machine for the worker
	stateMachine := NewWorkerStateMachine(id, m.logger)

	// Validate that add operation is allowed
	if err := stateMachine.ValidateOperation("add"); err != nil {
		return err
	}

	// Transition to registered state
	if err := stateMachine.Transition(WorkerStateRegistered, "add", nil); err != nil {
		return NewInternalError("failed to transition worker to registered state", err).WithContext("worker_id", id)
	}

	// Create worker logger
	logger := masterLogging.NewLogger("worker: "+id+" , ", masterLogging.LogFuncs{
		Debugf: m.logger.Debugf,
		Infof:  m.logger.Infof,
		Warnf:  m.logger.Warnf,
		Errorf: m.logger.Errorf,
	})

	// Create process control
	processControl := NewProcessControl(options, id, logger)

	// Store worker and state machine
	m.controls[id] = processControl
	m.stateMachines[id] = stateMachine

	m.logger.Infof("Worker added successfully, id: %s, state: %s", id, stateMachine.GetCurrentState())
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

	// Check if worker exists
	_, exists := m.controls[id]
	stateMachine, stateMachineExists := m.stateMachines[id]
	if !exists {
		return NewNotFoundError("worker not found", nil).WithContext("worker_id", id)
	}

	// Validate operation using state machine (if it exists)
	if stateMachineExists {
		if err := stateMachine.ValidateOperation("remove"); err != nil {
			return err
		}
	}

	// Remove worker and state machine
	delete(m.controls, id)
	if stateMachineExists {
		delete(m.stateMachines, id)
	}

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

	// 1. Get process control and state machine under lock
	m.mutex.Lock()
	processControl, exists := m.controls[id]
	stateMachine, stateMachineExists := m.stateMachines[id]
	m.mutex.Unlock()

	if !exists || !stateMachineExists {
		return NewNotFoundError("worker not found", nil).WithContext("worker_id", id)
	}

	// 2. Validate operation using state machine (outside lock)
	if err := stateMachine.ValidateOperation("start"); err != nil {
		return err
	}

	// 3. Transition to starting state
	if err := stateMachine.Transition(WorkerStateStarting, "start", nil); err != nil {
		return NewInternalError("failed to transition worker to starting state", err).WithContext("worker_id", id)
	}

	// 4. Start process control (outside of lock, can be long-running)
	err := processControl.Start(ctx)

	// 5. Update state based on result
	if err != nil {
		// Transition to failed state
		transitionErr := stateMachine.Transition(WorkerStateFailed, "start", err)
		if transitionErr != nil {
			m.logger.Errorf("Failed to transition worker to failed state, id: %s, error: %v", id, transitionErr)
		}

		m.logger.Errorf("Failed to start worker, id: %s, error: %v", id, err)

		// Check if the error is a context cancellation
		if ctx.Err() != nil {
			return NewCancelledError("worker start was cancelled", ctx.Err()).WithContext("worker_id", id)
		}
		return NewProcessError("failed to start worker", err).WithContext("worker_id", id)
	}

	// 6. Transition to running state on success
	if err := stateMachine.Transition(WorkerStateRunning, "start", nil); err != nil {
		m.logger.Errorf("Failed to transition worker to running state, id: %s, error: %v", id, err)
		// Note: Process is actually running, but state tracking failed
	}

	m.logger.Infof("Worker started successfully, id: %s, state: %s", id, stateMachine.GetCurrentState())
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

	// 1. Get process control and state machine under lock
	m.mutex.Lock()
	processControl, exists := m.controls[id]
	stateMachine, stateMachineExists := m.stateMachines[id]
	m.mutex.Unlock()

	if !exists || !stateMachineExists {
		return NewNotFoundError("worker not found", nil).WithContext("worker_id", id)
	}

	// 2. Validate operation using state machine (outside lock)
	if err := stateMachine.ValidateOperation("stop"); err != nil {
		return err
	}

	// 3. Transition to stopping state
	if err := stateMachine.Transition(WorkerStateStopping, "stop", nil); err != nil {
		return NewInternalError("failed to transition worker to stopping state", err).WithContext("worker_id", id)
	}

	// 4. Stop process control (outside of lock, can be long-running)
	err := processControl.Stop(ctx)

	// 5. Update state based on result
	if err != nil {
		// Transition to failed state
		transitionErr := stateMachine.Transition(WorkerStateFailed, "stop", err)
		if transitionErr != nil {
			m.logger.Errorf("Failed to transition worker to failed state, id: %s, error: %v", id, transitionErr)
		}

		m.logger.Errorf("Failed to stop worker, id: %s, error: %v", id, err)

		// Check if the error is a context cancellation
		if ctx.Err() != nil {
			return NewCancelledError("worker stop was cancelled", ctx.Err()).WithContext("worker_id", id)
		}
		return NewProcessError("failed to stop worker", err).WithContext("worker_id", id)
	}

	// 6. Transition to stopped state on success
	if err := stateMachine.Transition(WorkerStateStopped, "stop", nil); err != nil {
		m.logger.Errorf("Failed to transition worker to stopped state, id: %s, error: %v", id, err)
		// Note: Process is actually stopped, but state tracking failed
	}

	m.logger.Infof("Worker stopped successfully, id: %s, state: %s", id, stateMachine.GetCurrentState())
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

	// 1. Get all process controls and state machines under lock
	m.mutex.Lock()
	controlsCopy := make(map[string]ProcessControl)
	stateMachinesCopy := make(map[string]*WorkerStateMachine)

	for id, control := range m.controls {
		controlsCopy[id] = control
	}
	for id, stateMachine := range m.stateMachines {
		stateMachinesCopy[id] = stateMachine
	}
	m.mutex.Unlock()

	// 2. Start only workers in registered state (outside of lock)
	errorCollection := NewErrorCollection()
	startedCount := 0
	skippedCount := 0

	for id, processControl := range controlsCopy {
		stateMachine, exists := stateMachinesCopy[id]
		if !exists {
			m.logger.Warnf("No state machine found for worker, id: %s, skipping start", id)
			skippedCount++
			continue
		}

		currentState := stateMachine.GetCurrentState()

		// Only start workers that are in registered state
		if currentState != WorkerStateRegistered {
			m.logger.Debugf("Worker not in registered state, id: %s, state: %s, skipping start", id, currentState)
			skippedCount++
			continue
		}

		// Validate and transition to starting state
		if err := stateMachine.ValidateOperation("start"); err != nil {
			m.logger.Warnf("Cannot start worker due to state validation, id: %s, error: %v", id, err)
			errorCollection.Add(err)
			continue
		}

		if err := stateMachine.Transition(WorkerStateStarting, "start", nil); err != nil {
			m.logger.Errorf("Failed to transition worker to starting state, id: %s, error: %v", id, err)
			errorCollection.Add(NewInternalError("failed to transition worker to starting state", err).WithContext("worker_id", id))
			continue
		}

		// Start the process control
		err := processControl.Start(ctx)
		if err != nil {
			// Transition to failed state
			transitionErr := stateMachine.Transition(WorkerStateFailed, "start", err)
			if transitionErr != nil {
				m.logger.Errorf("Failed to transition worker to failed state, id: %s, error: %v", id, transitionErr)
			}

			m.logger.Errorf("Failed to start process control, id: %s, error: %v", id, err)
			contextualErr := NewProcessError("failed to start process control", err).WithContext("worker_id", id)
			errorCollection.Add(contextualErr)
		} else {
			// Transition to running state on success
			if err := stateMachine.Transition(WorkerStateRunning, "start", nil); err != nil {
				m.logger.Errorf("Failed to transition worker to running state, id: %s, error: %v", id, err)
			}
			startedCount++
		}
	}

	if errorCollection.HasErrors() {
		m.logger.Errorf("Some process controls failed to start: %v", errorCollection.Error())
	}

	m.logger.Infof("Process controls start complete: %d started, %d skipped, %d total", startedCount, skippedCount, len(controlsCopy))
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

// GetWorkerState returns the current state of a worker
func (m *Master) GetWorkerState(id string) (WorkerState, error) {
	// Validate worker ID
	if err := ValidateWorkerID(id); err != nil {
		return WorkerStateUnknown, NewValidationError("invalid worker ID", err).WithContext("worker_id", id)
	}

	m.mutex.Lock()
	stateMachine, exists := m.stateMachines[id]
	m.mutex.Unlock()

	if !exists {
		return WorkerStateUnknown, NewNotFoundError("worker not found", nil).WithContext("worker_id", id)
	}

	return stateMachine.GetCurrentState(), nil
}

// GetWorkerStateInfo returns comprehensive state information for a worker
func (m *Master) GetWorkerStateInfo(id string) (WorkerStateInfo, error) {
	// Validate worker ID
	if err := ValidateWorkerID(id); err != nil {
		return WorkerStateInfo{}, NewValidationError("invalid worker ID", err).WithContext("worker_id", id)
	}

	m.mutex.Lock()
	stateMachine, exists := m.stateMachines[id]
	m.mutex.Unlock()

	if !exists {
		return WorkerStateInfo{}, NewNotFoundError("worker not found", nil).WithContext("worker_id", id)
	}

	return stateMachine.GetStateInfo(), nil
}

// GetAllWorkerStates returns state information for all workers
func (m *Master) GetAllWorkerStates() map[string]WorkerStateInfo {
	m.mutex.Lock()
	stateMachinesCopy := make(map[string]*WorkerStateMachine)
	for id, stateMachine := range m.stateMachines {
		stateMachinesCopy[id] = stateMachine
	}
	m.mutex.Unlock()

	result := make(map[string]WorkerStateInfo)
	for id, stateMachine := range stateMachinesCopy {
		result[id] = stateMachine.GetStateInfo()
	}
	return result
}

// IsWorkerOperationAllowed checks if an operation is allowed for a worker
func (m *Master) IsWorkerOperationAllowed(id string, operation string) (bool, error) {
	// Validate worker ID
	if err := ValidateWorkerID(id); err != nil {
		return false, NewValidationError("invalid worker ID", err).WithContext("worker_id", id)
	}

	m.mutex.Lock()
	stateMachine, exists := m.stateMachines[id]
	m.mutex.Unlock()

	if !exists {
		return false, NewNotFoundError("worker not found", nil).WithContext("worker_id", id)
	}

	return stateMachine.IsOperationAllowed(operation), nil
}
