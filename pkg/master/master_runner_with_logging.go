package master

import (
	"context"
	"fmt"
	"time"

	coreLogging "github.com/core-tools/hsu-core/pkg/logging"
	"github.com/core-tools/hsu-master/pkg/errors"
	"github.com/core-tools/hsu-master/pkg/logging"
	"github.com/core-tools/hsu-master/pkg/workers"
	"github.com/core-tools/hsu-master/pkg/workers/processcontrol"
)

// RunWithConfigAndLogCollection runs the master with log collection enabled
// This is the enhanced version that includes log collection integration
func RunWithConfigAndLogCollection(configFile string, coreLogger coreLogging.Logger, masterLogger logging.Logger) error {
	// Load configuration
	config, err := LoadConfigFromFile(configFile)
	if err != nil {
		return errors.NewIOError("failed to load configuration", err).WithContext("config_file", configFile)
	}

	// Validate configuration
	if err := ValidateConfig(config); err != nil {
		return errors.NewValidationError("configuration validation failed", err).WithContext("config_file", configFile)
	}

	masterLogger.Infof("Configuration loaded successfully from %s", configFile)
	masterLogger.Infof("Master port: %d, Workers: %d", config.Master.Port, len(config.Workers))

	// ===== LOG COLLECTION INTEGRATION =====

	// Create log collection integration
	logIntegration, err := NewLogCollectionIntegration(config, masterLogger)
	if err != nil {
		return errors.NewInternalError("failed to create log collection integration", err)
	}

	// Start log collection service
	ctx := context.Background()
	if err := logIntegration.Start(ctx); err != nil {
		return errors.NewInternalError("failed to start log collection", err)
	}
	defer logIntegration.Stop()

	if logIntegration.IsEnabled() {
		masterLogger.Infof("Log collection service started successfully")
	} else {
		masterLogger.Infof("Log collection is disabled")
	}

	// ===== MASTER SERVICE SETUP =====

	// Create master options from config
	masterOptions := MasterOptions{
		Port: config.Master.Port,
	}

	// Create master instance
	master, err := NewMaster(masterOptions, coreLogger, masterLogger)
	if err != nil {
		return errors.NewInternalError("failed to create master", err)
	}

	// Set log collection service on master
	if logIntegration.IsEnabled() {
		master.SetLogCollectionService(logIntegration.GetLogCollectionService())
	}

	// Create workers from configuration with log collection support
	workers, err := CreateWorkersFromConfigWithLogCollection(config, masterLogger, logIntegration)
	if err != nil {
		return errors.NewValidationError("failed to create workers from configuration", err)
	}

	masterLogger.Infof("Created %d workers from configuration", len(workers))

	// ===== CONTINUE WITH STANDARD MASTER RUNNER =====

	// Create application context
	ctx = context.Background()

	// Create a channel to signal when the master has stopped
	stopped := make(chan struct{})

	// Create master run options
	masterRunOptions := RunOptions{
		Context:               ctx,
		ForcedShutdownTimeout: config.Master.ForceShutdownTimeout,
		Stopped:               stopped,
	}

	// Add all workers to master (registration phase)
	for _, worker := range workers {
		err := master.AddWorker(worker)
		if err != nil {
			return errors.NewValidationError(
				fmt.Sprintf("failed to add worker: %s", worker.ID()),
				err,
			).WithContext("worker_id", worker.ID())
		}
		masterLogger.Infof("Added worker: %s", worker.ID())
	}

	// Start master in background (master.Run blocks until shutdown)
	masterLogger.Infof("Starting master server...")
	go func() {
		master.RunWithOptions(masterRunOptions)
	}()

	// Wait for master to be running before starting workers
	masterLogger.Infof("Waiting for master to be ready...")
	for {
		if master.GetMasterState() == MasterStateRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	masterLogger.Infof("Master is ready, starting workers...")

	// Start all workers (lifecycle phase)
	for _, worker := range workers {
		err := master.StartWorker(ctx, worker.ID())
		if err != nil {
			masterLogger.Errorf("Failed to start worker %s: %v", worker.ID(), err)
			// Continue with other workers rather than failing completely
			continue
		}
		masterLogger.Infof("Started worker: %s", worker.ID())
	}

	masterLogger.Infof("All workers started, master is fully operational with log collection")

	// Wait for stop signal
	<-stopped

	masterLogger.Infof("Master runner stopped")
	return nil
}

// CreateWorkersFromConfigWithLogCollection creates workers with log collection support
func CreateWorkersFromConfigWithLogCollection(
	config *MasterConfig,
	logger logging.Logger,
	logIntegration *LogCollectionIntegration,
) ([]workers.Worker, error) {
	if config == nil {
		return nil, errors.NewValidationError("configuration cannot be nil", nil)
	}

	var workersResult []workers.Worker

	for i, workerConfig := range config.Workers {
		// Skip disabled workers
		if workerConfig.Enabled != nil && !*workerConfig.Enabled {
			logger.Infof("Skipping disabled worker, id: %s", workerConfig.ID)
			continue
		}

		// Create worker with log collection support
		worker, err := createWorkerFromConfigWithLogCollection(workerConfig, logger, logIntegration)
		if err != nil {
			return nil, errors.NewValidationError(
				fmt.Sprintf("failed to create worker at index %d", i),
				err,
			).WithContext("worker_id", workerConfig.ID).WithContext("worker_index", fmt.Sprintf("%d", i))
		}

		workersResult = append(workersResult, worker)
	}

	return workersResult, nil
}

// createWorkerFromConfigWithLogCollection creates a single worker with log collection support
func createWorkerFromConfigWithLogCollection(
	config WorkerConfig,
	logger logging.Logger,
	logIntegration *LogCollectionIntegration,
) (workers.Worker, error) {

	// Create the base worker first
	baseWorker, err := createWorkerFromConfig(config, logger)
	if err != nil {
		return nil, err
	}

	// If log collection is not enabled, return the base worker
	if !logIntegration.IsEnabled() {
		return baseWorker, nil
	}

	// Enhance worker with log collection
	enhancedWorker := &logCollectionEnabledWorker{
		Worker:         baseWorker,
		logIntegration: logIntegration,
		workerConfig:   config,
	}

	logger.Infof("Worker %s created with log collection support", config.ID)
	return enhancedWorker, nil
}

// ===== LOG COLLECTION ENABLED WORKER WRAPPER =====

// logCollectionEnabledWorker wraps a worker to add log collection capabilities
type logCollectionEnabledWorker struct {
	workers.Worker
	logIntegration *LogCollectionIntegration
	workerConfig   WorkerConfig
}

// ProcessControlOptions enhances the base worker's process control options with log collection
func (w *logCollectionEnabledWorker) ProcessControlOptions() processcontrol.ProcessControlOptions {
	// Get base options from the wrapped worker
	baseOptions := w.Worker.ProcessControlOptions()

	// Add log collection service and config
	baseOptions.LogCollectionService = w.logIntegration.GetLogCollectionService()
	baseOptions.LogConfig = w.logIntegration.GetWorkerLogConfig(w.Worker.ID(), w.workerConfig)

	return baseOptions
}
