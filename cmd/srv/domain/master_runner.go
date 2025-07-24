package domain

import (
	"context"
	"fmt"
	"time"

	coreLogging "github.com/core-tools/hsu-core/pkg/logging"
	"github.com/core-tools/hsu-master/pkg/errors"
	"github.com/core-tools/hsu-master/pkg/logging"
)

// RunWithConfig runs the master with configuration loaded from a file
// This is the main entry point for configuration-driven master execution
func RunWithConfig(configFile string, coreLogger coreLogging.Logger, masterLogger logging.Logger) error {
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

	// Run with the loaded configuration
	return RunWithConfigStruct(config, coreLogger, masterLogger)
}

// RunWithConfigStruct runs the master with a configuration structure
// This allows programmatic configuration for testing and embedding
func RunWithConfigStruct(config *MasterConfig, coreLogger coreLogging.Logger, masterLogger logging.Logger) error {
	if config == nil {
		return errors.NewValidationError("configuration cannot be nil", nil)
	}

	masterLogger.Infof("Master runner starting...")

	// Create master options from config
	masterOptions := MasterOptions{
		Port: config.Master.Port,
	}

	// Create master instance
	master, err := NewMaster(masterOptions, coreLogger, masterLogger)
	if err != nil {
		return errors.NewInternalError("failed to create master", err)
	}

	// Create workers from configuration
	workers, err := CreateWorkersFromConfig(config, masterLogger)
	if err != nil {
		return errors.NewValidationError("failed to create workers from configuration", err)
	}

	masterLogger.Infof("Created %d workers from configuration", len(workers))

	// Create application context
	ctx := context.Background()

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

	masterLogger.Infof("All workers started, master is fully operational")

	// Wait for stop signal
	<-stopped

	masterLogger.Infof("Master runner stopped")

	return nil
}

// RunWithConfigAndCustomLogger runs master with configuration and custom logger setup
// This provides more control over logging configuration
func RunWithConfigAndCustomLogger(
	configFile string,
	loggerFactory func(string) logging.Logger,
	coreLoggerFactory func(string) coreLogging.Logger,
) error {
	// Create loggers using the provided factories
	masterLogger := loggerFactory("hsu-master")
	coreLogger := coreLoggerFactory("hsu-core")

	return RunWithConfig(configFile, coreLogger, masterLogger)
}

// CreateMasterFromConfig creates a master instance from configuration without running it
// This is useful for testing and more advanced use cases
func CreateMasterFromConfig(
	config *MasterConfig,
	coreLogger coreLogging.Logger,
	masterLogger logging.Logger,
) (*Master, []Worker, error) {
	if config == nil {
		return nil, nil, errors.NewValidationError("configuration cannot be nil", nil)
	}

	// Validate configuration
	if err := ValidateConfig(config); err != nil {
		return nil, nil, errors.NewValidationError("configuration validation failed", err)
	}

	// Create master options from config
	masterOptions := MasterOptions{
		Port: config.Master.Port,
	}

	// Create master instance
	master, err := NewMaster(masterOptions, coreLogger, masterLogger)
	if err != nil {
		return nil, nil, errors.NewInternalError("failed to create master", err)
	}

	// Create workers from configuration
	workers, err := CreateWorkersFromConfig(config, masterLogger)
	if err != nil {
		return nil, nil, errors.NewValidationError("failed to create workers from configuration", err)
	}

	return master, workers, nil
}

// ValidateConfigFile validates a configuration file without loading/running
// This is useful for configuration testing and CI/CD validation
func ValidateConfigFile(configFile string) error {
	// Load configuration
	config, err := LoadConfigFromFile(configFile)
	if err != nil {
		return errors.NewIOError("failed to load configuration", err).WithContext("config_file", configFile)
	}

	// Validate configuration
	if err := ValidateConfig(config); err != nil {
		return errors.NewValidationError("configuration validation failed", err).WithContext("config_file", configFile)
	}

	return nil
}

// GetConfigSummary returns a human-readable summary of the configuration
// This is useful for debugging and operational visibility
func GetConfigSummary(config *MasterConfig) ConfigSummary {
	if config == nil {
		return ConfigSummary{Error: "configuration is nil"}
	}

	summary := ConfigSummary{
		MasterPort: config.Master.Port,
		LogLevel:   config.Master.LogLevel,
		Workers:    make([]WorkerSummary, 0, len(config.Workers)),
	}

	for _, worker := range config.Workers {
		enabled := false
		if worker.Enabled != nil {
			enabled = *worker.Enabled
		}

		workerSummary := WorkerSummary{
			ID:      worker.ID,
			Type:    string(worker.Type),
			Enabled: enabled,
		}

		// Add type-specific information
		switch worker.Type {
		case WorkerTypeManaged:
			if worker.Unit.Managed != nil {
				workerSummary.ExecutablePath = worker.Unit.Managed.Control.Execution.ExecutablePath
				workerSummary.HealthCheckType = string(worker.Unit.Managed.HealthCheck.Type)
			}
		case WorkerTypeUnmanaged:
			if worker.Unit.Unmanaged != nil {
				workerSummary.DiscoveryMethod = string(worker.Unit.Unmanaged.Discovery.Method)
				workerSummary.HealthCheckType = string(worker.Unit.Unmanaged.HealthCheck.Type)
			}
		case WorkerTypeIntegrated:
			if worker.Unit.Integrated != nil {
				workerSummary.ExecutablePath = worker.Unit.Integrated.Control.Execution.ExecutablePath
			}
		}

		summary.Workers = append(summary.Workers, workerSummary)
	}

	summary.TotalWorkers = len(summary.Workers)
	summary.EnabledWorkers = 0
	for _, worker := range summary.Workers {
		if worker.Enabled {
			summary.EnabledWorkers++
		}
	}

	return summary
}

// ConfigSummary provides a high-level overview of configuration
type ConfigSummary struct {
	MasterPort     int             `json:"master_port"`
	LogLevel       string          `json:"log_level"`
	TotalWorkers   int             `json:"total_workers"`
	EnabledWorkers int             `json:"enabled_workers"`
	Workers        []WorkerSummary `json:"workers"`
	Error          string          `json:"error,omitempty"`
}

// WorkerSummary provides a summary of worker configuration
type WorkerSummary struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	Enabled         bool   `json:"enabled"`
	ExecutablePath  string `json:"executable_path,omitempty"`
	DiscoveryMethod string `json:"discovery_method,omitempty"`
	HealthCheckType string `json:"health_check_type,omitempty"`
}
