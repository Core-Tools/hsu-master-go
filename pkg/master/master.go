package master

import (
	"context"
	"fmt"
	"sync"
	"time"

	corecontrol "github.com/core-tools/hsu-core/pkg/control"
	coredomain "github.com/core-tools/hsu-core/pkg/domain"
	corelogging "github.com/core-tools/hsu-core/pkg/logging"
	mastercontrol "github.com/core-tools/hsu-master/pkg/control"
	"github.com/core-tools/hsu-master/pkg/errors"
	"github.com/core-tools/hsu-master/pkg/logcollection"
	logconfig "github.com/core-tools/hsu-master/pkg/logcollection/config"
	masterlogging "github.com/core-tools/hsu-master/pkg/logging"
	"github.com/core-tools/hsu-master/pkg/master/workerstatemachine"
	"github.com/core-tools/hsu-master/pkg/workers"
	"github.com/core-tools/hsu-master/pkg/workers/processcontrol"
	"github.com/core-tools/hsu-master/pkg/workers/processcontrolimpl"
)

type MasterOptions struct {
	Port                 int
	ForceShutdownTimeout time.Duration
}

// MasterState represents the current state of the master server
type MasterState string

const (
	// MasterStateNotStarted is the initial state before Run() is called
	MasterStateNotStarted MasterState = "not_started"

	// MasterStateRunning means master is running and can manage workers
	MasterStateRunning MasterState = "running"

	// MasterStateStopping means master is shutting down
	MasterStateStopping MasterState = "stopping"

	// MasterStateStopped means master has stopped
	MasterStateStopped MasterState = "stopped"
)

// WorkerEntry combines ProcessControl and StateMachine for a worker
type WorkerEntry struct {
	ProcessControl processcontrol.ProcessControl
	StateMachine   *workerstatemachine.WorkerStateMachine
}

type Master struct {
	options              MasterOptions
	server               corecontrol.Server
	logger               masterlogging.Logger
	workers              map[string]*WorkerEntry // Combined map for controls and state machines
	masterState          MasterState             // Track master state
	mutex                sync.Mutex
	logCollectionService logcollection.LogCollectionService // Log collection service
}

func NewMaster(options MasterOptions, coreLogger corelogging.Logger, masterLogger masterlogging.Logger) (*Master, error) {
	// Create gRPC server
	serverOptions := corecontrol.ServerOptions{
		Port: options.Port,
	}

	server, err := corecontrol.NewServer(serverOptions, coreLogger)
	if err != nil {
		return nil, errors.NewInternalError("failed to create server", err)
	}

	// Register core services
	coreHandler := coredomain.NewDefaultHandler(coreLogger)
	corecontrol.RegisterGRPCServerHandler(server.GRPC(), coreHandler, coreLogger)

	// Register business logic services
	masterHandler := NewMasterHandler(masterLogger)
	mastercontrol.RegisterGRPCServerHandler(server.GRPC(), masterHandler, masterLogger)

	master := &Master{
		options:     options,
		server:      server,
		logger:      masterLogger,
		workers:     make(map[string]*WorkerEntry),
		masterState: MasterStateNotStarted,
		mutex:       sync.Mutex{},
	}

	return master, nil
}

func (m *Master) AddWorker(worker workers.Worker) error {
	// Validate input
	if worker == nil {
		return errors.NewValidationError("worker cannot be nil", nil)
	}

	id := worker.ID()

	// Validate worker ID
	if err := ValidateWorkerID(id); err != nil {
		return errors.NewValidationError("invalid worker ID", err).WithContext("worker_id", id)
	}

	// Validate worker options
	options := worker.ProcessControlOptions()
	if err := workers.ValidateProcessControlOptions(options); err != nil {
		return errors.NewValidationError("invalid worker process control options", err).WithContext("worker_id", id)
	}

	m.logger.Infof("Adding worker, id: %s, can_attach: %t, can_execute: %t, can_terminate: %t, can_restart: %t",
		id, options.CanAttach, (options.ExecuteCmd != nil), options.CanTerminate, options.CanRestart)

	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Check if worker already exists
	if _, exists := m.workers[id]; exists {
		return errors.NewConflictError("worker already exists", nil).WithContext("worker_id", id)
	}

	// Create state machine for the worker
	stateMachine := workerstatemachine.NewWorkerStateMachine(id, m.logger)

	// Validate that add operation is allowed
	if err := stateMachine.ValidateOperation("add"); err != nil {
		return err
	}

	// Transition to registered state
	if err := stateMachine.Transition(workerstatemachine.WorkerStateRegistered, "add", nil); err != nil {
		return errors.NewInternalError("failed to transition worker to registered state", err).WithContext("worker_id", id)
	}

	// Create worker logger
	logger := masterlogging.NewLogger("worker: "+id+" , ", masterlogging.LogFuncs{
		Debugf: m.logger.Debugf,
		Infof:  m.logger.Infof,
		Warnf:  m.logger.Warnf,
		Errorf: m.logger.Errorf,
	})

	// Create process control
	processControl := processcontrolimpl.NewProcessControl(options, id, logger)

	// Store worker and state machine
	m.workers[id] = &WorkerEntry{
		ProcessControl: processControl,
		StateMachine:   stateMachine,
	}

	m.logger.Infof("Worker added successfully, id: %s, state: %s", id, stateMachine.GetCurrentState())
	return nil
}

func (m *Master) RemoveWorker(id string) error {
	// Validate worker ID
	if err := ValidateWorkerID(id); err != nil {
		return errors.NewValidationError("invalid worker ID", err).WithContext("worker_id", id)
	}

	m.logger.Infof("Removing worker, id: %s", id)

	// Get worker and check if it's safely removable
	workerEntry, _, exists := m.getWorkerAndMasterState(id)
	if !exists {
		return errors.NewNotFoundError("worker not found", nil).WithContext("worker_id", id)
	}

	// Check if worker is in a safe state for removal
	currentState := workerEntry.StateMachine.GetCurrentState()
	if !isWorkerSafelyRemovable(currentState) {
		return errors.NewValidationError(
			fmt.Sprintf("cannot remove worker in state '%s': worker must be stopped before removal", currentState),
			nil,
		).WithContext("worker_id", id).
			WithContext("current_state", string(currentState)).
			WithContext("required_states", "stopped, failed").
			WithContext("suggested_action", "call StopWorker first")
	}

	// Safe to remove - acquire lock and remove
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Double-check existence under lock (could have been removed by another goroutine)
	if _, exists := m.workers[id]; !exists {
		return errors.NewNotFoundError("worker not found", nil).WithContext("worker_id", id)
	}

	// Remove worker and state machine
	delete(m.workers, id)

	m.logger.Infof("Worker removed successfully, id: %s", id)
	return nil
}

// isWorkerSafelyRemovable checks if a worker is in a state safe for removal
func isWorkerSafelyRemovable(state workerstatemachine.WorkerState) bool {
	switch state {
	case workerstatemachine.WorkerStateStopped, workerstatemachine.WorkerStateFailed:
		return true // Safe to remove - process is not running
	case workerstatemachine.WorkerStateUnknown, workerstatemachine.WorkerStateRegistered:
		return true // Safe to remove - no process started yet
	case workerstatemachine.WorkerStateStarting, workerstatemachine.WorkerStateRunning, workerstatemachine.WorkerStateStopping, workerstatemachine.WorkerStateRestarting:
		return false // Unsafe - process may be running
	default:
		return false // Unknown state - be conservative
	}
}

func (m *Master) StartWorker(ctx context.Context, id string) error {
	// Validate context
	if ctx == nil {
		return errors.NewValidationError("context cannot be nil", nil)
	}

	// Validate worker ID
	if err := ValidateWorkerID(id); err != nil {
		return errors.NewValidationError("invalid worker ID", err).WithContext("worker_id", id)
	}

	// Get worker and master state safely
	workerEntry, currentMasterState, exists := m.getWorkerAndMasterState(id)

	if !exists {
		return errors.NewNotFoundError("worker not found", nil).WithContext("worker_id", id)
	}

	// Validate master is running - after worker existence check
	if currentMasterState != MasterStateRunning {
		return errors.NewValidationError(
			fmt.Sprintf("master must be running to start workers, current state: %s", currentMasterState),
			nil,
		).WithContext("worker_id", id).WithContext("master_state", string(currentMasterState))
	}

	m.logger.Infof("Starting worker, id: %s", id)

	// 2. Validate operation using state machine (outside lock)
	if err := workerEntry.StateMachine.ValidateOperation("start"); err != nil {
		return err
	}

	// 3. Transition to starting state
	if err := workerEntry.StateMachine.Transition(workerstatemachine.WorkerStateStarting, "start", nil); err != nil {
		return errors.NewInternalError("failed to transition worker to starting state", err).WithContext("worker_id", id)
	}

	// 4. Start process control (outside of lock, can be long-running)
	err := workerEntry.ProcessControl.Start(ctx)

	// 5. Update state based on result
	if err != nil {
		// Transition to failed state
		transitionErr := workerEntry.StateMachine.Transition(workerstatemachine.WorkerStateFailed, "start", err)
		if transitionErr != nil {
			m.logger.Errorf("Failed to transition worker to failed state, id: %s, error: %v", id, transitionErr)
		}

		m.logger.Errorf("Failed to start worker, id: %s, error: %v", id, err)

		// Check if the error is a context cancellation
		if ctx.Err() != nil {
			return errors.NewCancelledError("worker start was cancelled", ctx.Err()).WithContext("worker_id", id)
		}
		return errors.NewProcessError("failed to start worker", err).WithContext("worker_id", id)
	}

	// 6. Transition to running state on success
	if err := workerEntry.StateMachine.Transition(workerstatemachine.WorkerStateRunning, "start", nil); err != nil {
		m.logger.Errorf("Failed to transition worker to running state, id: %s, error: %v", id, err)
		// Note: Process is actually running, but state tracking failed
	}

	m.logger.Infof("Worker started successfully, id: %s, state: %s", id, workerEntry.StateMachine.GetCurrentState())
	return nil
}

func (m *Master) StopWorker(ctx context.Context, id string) error {
	// Validate context
	if ctx == nil {
		return errors.NewValidationError("context cannot be nil", nil)
	}

	// Validate worker ID
	if err := ValidateWorkerID(id); err != nil {
		return errors.NewValidationError("invalid worker ID", err).WithContext("worker_id", id)
	}

	// Get worker and master state safely
	workerEntry, currentMasterState, exists := m.getWorkerAndMasterState(id)

	if !exists {
		return errors.NewNotFoundError("worker not found", nil).WithContext("worker_id", id)
	}

	// Validate master is running - after worker existence check
	if currentMasterState != MasterStateRunning {
		return errors.NewValidationError(
			fmt.Sprintf("master must be running to stop workers, current state: %s", currentMasterState),
			nil,
		).WithContext("worker_id", id).WithContext("master_state", string(currentMasterState))
	}

	m.logger.Infof("Stopping worker, id: %s", id)

	// 2. Validate operation using state machine (outside lock)
	if err := workerEntry.StateMachine.ValidateOperation("stop"); err != nil {
		return err
	}

	// 3. Transition to stopping state
	if err := workerEntry.StateMachine.Transition(workerstatemachine.WorkerStateStopping, "stop", nil); err != nil {
		return errors.NewInternalError("failed to transition worker to stopping state", err).WithContext("worker_id", id)
	}

	// 4. Stop process control (outside of lock, can be long-running)
	err := workerEntry.ProcessControl.Stop(ctx)

	// 5. Update state based on result
	if err != nil {
		// Transition to failed state
		transitionErr := workerEntry.StateMachine.Transition(workerstatemachine.WorkerStateFailed, "stop", err)
		if transitionErr != nil {
			m.logger.Errorf("Failed to transition worker to failed state, id: %s, error: %v", id, transitionErr)
		}

		m.logger.Errorf("Failed to stop worker, id: %s, error: %v", id, err)

		// Check if the error is a context cancellation
		if ctx.Err() != nil {
			return errors.NewCancelledError("worker stop was cancelled", ctx.Err()).WithContext("worker_id", id)
		}
		return errors.NewProcessError("failed to stop worker", err).WithContext("worker_id", id)
	}

	// 6. Transition to stopped state on success
	if err := workerEntry.StateMachine.Transition(workerstatemachine.WorkerStateStopped, "stop", nil); err != nil {
		m.logger.Errorf("Failed to transition worker to stopped state, id: %s, error: %v", id, err)
		// Note: Process is actually stopped, but state tracking failed
	}

	m.logger.Infof("Worker stopped successfully, id: %s, state: %s", id, workerEntry.StateMachine.GetCurrentState())
	return nil
}

func (m *Master) Start(ctx context.Context) {
	m.logger.Infof("Starting master...")

	// Start the server
	m.server.Start(ctx)

	// Transition master to running state
	m.setMasterState(MasterStateRunning)

	m.logger.Infof("Master started")

}

func (m *Master) Stop(ctx context.Context) {
	m.logger.Infof("Stopping master...")

	// Transition to stopping state
	m.setMasterState(MasterStateStopping)

	if ctx == nil {
		ctx = context.Background()
	}

	forcedShutdownTimeout := m.options.ForceShutdownTimeout
	if forcedShutdownTimeout <= 0 {
		forcedShutdownTimeout = 30 * time.Second // Timeout super-default
	}

	// Set forced shutdown timeout, it will be used for both server and workers
	ctx, _ = context.WithTimeout(ctx, forcedShutdownTimeout)

	// Stop server
	m.server.Shutdown(ctx)

	// Stop workers
	m.stopWorkerProcessControls(ctx)

	// Transition to stopped state
	m.setMasterState(MasterStateStopped)

	m.logger.Infof("Master stopped")
}

// GetAllWorkerStatesWithDiagnostics returns comprehensive state and diagnostic information for all workers
func (m *Master) GetAllWorkerStatesWithDiagnostics() map[string]WorkerStateWithDiagnostics {
	workerEntriesCopy := m.getAllWorkers()

	result := make(map[string]WorkerStateWithDiagnostics)
	for id, workerEntry := range workerEntriesCopy {
		processDiagnostics := workerEntry.ProcessControl.GetDiagnostics()
		workerStateInfo := workerEntry.StateMachine.GetStateInfo()

		result[id] = WorkerStateWithDiagnostics{
			WorkerStateInfo:    workerStateInfo,
			ProcessDiagnostics: processDiagnostics,
		}
	}
	return result
}

// GetWorkerState returns the current state of a worker
func (m *Master) GetWorkerState(id string) (workerstatemachine.WorkerState, error) {
	// Validate worker ID
	if err := ValidateWorkerID(id); err != nil {
		return workerstatemachine.WorkerStateUnknown, errors.NewValidationError("invalid worker ID", err).WithContext("worker_id", id)
	}

	workerEntry, _, exists := m.getWorkerAndMasterState(id)

	if !exists {
		return workerstatemachine.WorkerStateUnknown, errors.NewNotFoundError("worker not found", nil).WithContext("worker_id", id)
	}

	return workerEntry.StateMachine.GetCurrentState(), nil
}

// GetWorkerStateWithDiagnostics returns comprehensive state and diagnostic information for a worker
func (m *Master) GetWorkerStateWithDiagnostics(id string) (WorkerStateWithDiagnostics, error) {
	// Validate worker ID
	if err := ValidateWorkerID(id); err != nil {
		return WorkerStateWithDiagnostics{}, errors.NewValidationError("invalid worker ID", err).WithContext("worker_id", id)
	}

	workerEntry, _, exists := m.getWorkerAndMasterState(id)

	if !exists {
		return WorkerStateWithDiagnostics{}, errors.NewNotFoundError("worker not found", nil).WithContext("worker_id", id)
	}

	processDiagnostics := workerEntry.ProcessControl.GetDiagnostics()
	workerStateInfo := workerEntry.StateMachine.GetStateInfo()

	return WorkerStateWithDiagnostics{
		WorkerStateInfo:    workerStateInfo,
		ProcessDiagnostics: processDiagnostics,
	}, nil
}

// IsWorkerOperationAllowed checks if an operation is allowed for a worker
func (m *Master) IsWorkerOperationAllowed(id string, operation string) (bool, error) {
	// Validate worker ID
	if err := ValidateWorkerID(id); err != nil {
		return false, errors.NewValidationError("invalid worker ID", err).WithContext("worker_id", id)
	}

	workerEntry, _, exists := m.getWorkerAndMasterState(id)

	if !exists {
		return false, errors.NewNotFoundError("worker not found", nil).WithContext("worker_id", id)
	}

	return workerEntry.StateMachine.IsOperationAllowed(operation), nil
}

// GetMasterState returns the current state of the master
func (m *Master) GetMasterState() MasterState {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.masterState
}

// WorkerStateWithDiagnostics combines worker state info with process diagnostics
type WorkerStateWithDiagnostics struct {
	workerstatemachine.WorkerStateInfo
	ProcessDiagnostics processcontrol.ProcessDiagnostics // Detailed process diagnostics (includes State)
}

// GetWorkerProcessDiagnostics returns detailed process diagnostics for a worker
func (m *Master) GetWorkerProcessDiagnostics(id string) (processcontrol.ProcessDiagnostics, error) {
	// Validate worker ID
	if err := ValidateWorkerID(id); err != nil {
		return processcontrol.ProcessDiagnostics{}, errors.NewValidationError("invalid worker ID", err).WithContext("worker_id", id)
	}

	workerEntry, _, exists := m.getWorkerAndMasterState(id)

	if !exists {
		return processcontrol.ProcessDiagnostics{}, errors.NewNotFoundError("worker not found", nil).WithContext("worker_id", id)
	}

	return workerEntry.ProcessControl.GetDiagnostics(), nil
}

func (m *Master) stopWorkerProcessControls(ctx context.Context) {
	m.logger.Infof("Stopping process controls...")

	if ctx == nil {
		ctx = context.Background()
	}

	// 1. Get all process controls under lock
	workerEntriesCopy := m.getAllWorkers()

	// 2. Stop processes outside of lock
	errorCollection := errors.NewErrorCollection()
	for id, workerEntry := range workerEntriesCopy {
		err := workerEntry.ProcessControl.Stop(ctx)
		if err != nil {
			m.logger.Errorf("Failed to stop process control, id: %s, error: %v", id, err)
			// Add context to the error for better debugging
			contextualErr := errors.NewProcessError("failed to stop process control", err).WithContext("worker_id", id)
			errorCollection.Add(contextualErr)
		}
	}

	if errorCollection.HasErrors() {
		m.logger.Errorf("Some process controls failed to stop: %v", errorCollection.Error())
	}

	m.logger.Infof("Process controls stopped.")
}

// getAllWorkers returns a copy of all worker entries under lock
func (m *Master) getAllWorkers() map[string]*WorkerEntry {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	workerEntriesCopy := make(map[string]*WorkerEntry)
	for id, workerEntry := range m.workers {
		workerEntriesCopy[id] = workerEntry
	}
	return workerEntriesCopy
}

// getWorkerAndMasterState returns worker entry and master state under lock
// Returns: workerEntry, masterState, exists
func (m *Master) getWorkerAndMasterState(id string) (*WorkerEntry, MasterState, bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	workerEntry, exists := m.workers[id]
	return workerEntry, m.masterState, exists
}

// setMasterState sets the master state and releases the lock
func (m *Master) setMasterState(state MasterState) {
	m.mutex.Lock()
	m.masterState = state
	m.mutex.Unlock()
}

// SetLogCollectionService sets the log collection service for the master (NEW)
func (m *Master) SetLogCollectionService(service logcollection.LogCollectionService) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.logCollectionService = service
	m.logger.Infof("Log collection service configured for master")
}

// getLogCollectionConfig creates a default log collection config for workers
func (m *Master) getLogCollectionConfig() *logconfig.WorkerLogConfig {
	if m.logCollectionService == nil {
		return nil
	}

	// Create default worker log configuration
	defaultConfig := logconfig.DefaultWorkerLogConfig()
	return &defaultConfig
}
