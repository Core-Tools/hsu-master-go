package master

import (
	"fmt"
	"io/ioutil"
	"time"

	"github.com/core-tools/hsu-master/pkg/errors"
	"github.com/core-tools/hsu-master/pkg/logging"
	"github.com/core-tools/hsu-master/pkg/monitoring"
	"github.com/core-tools/hsu-master/pkg/workers"

	"gopkg.in/yaml.v3"
)

// MasterConfig represents the top-level configuration file structure
type MasterConfig struct {
	Master  MasterConfigOptions `yaml:"master"`
	Workers []WorkerConfig      `yaml:"workers"`
}

// MasterConfigOptions represents master-level configuration
type MasterConfigOptions struct {
	Port                 int           `yaml:"port"`
	LogLevel             string        `yaml:"log_level,omitempty"`
	ForceShutdownTimeout time.Duration `yaml:"force_shutdown_timeout,omitempty"`
}

// WorkerConfig represents a single worker configuration
type WorkerConfig struct {
	ID      string           `yaml:"id"`
	Type    WorkerType       `yaml:"type"`
	Enabled *bool            `yaml:"enabled,omitempty"` // Pointer to distinguish unset from false
	Unit    WorkerUnitConfig `yaml:"unit"`
}

// WorkerType represents the type of worker
type WorkerType string

const (
	WorkerTypeManaged    WorkerType = "managed"
	WorkerTypeUnmanaged  WorkerType = "unmanaged"
	WorkerTypeIntegrated WorkerType = "integrated"
)

// WorkerUnitConfig is a union type that holds configuration for different worker types
type WorkerUnitConfig struct {
	// Only one of these should be populated based on WorkerConfig.Type
	Managed    *workers.ManagedUnit    `yaml:"managed,omitempty"`
	Unmanaged  *workers.UnmanagedUnit  `yaml:"unmanaged,omitempty"`
	Integrated *workers.IntegratedUnit `yaml:"integrated,omitempty"`
}

// LoadConfigFromFile loads master configuration from a YAML file
func LoadConfigFromFile(filename string) (*MasterConfig, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, errors.NewIOError("failed to read configuration file", err).WithContext("filename", filename)
	}

	var config MasterConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, errors.NewValidationError("failed to parse YAML configuration", err).WithContext("filename", filename)
	}

	// Set defaults
	if err := setConfigDefaults(&config); err != nil {
		return nil, errors.NewValidationError("failed to apply configuration defaults", err)
	}

	return &config, nil
}

// ValidateConfig validates the entire configuration structure
func ValidateConfig(config *MasterConfig) error {
	if config == nil {
		return errors.NewValidationError("configuration cannot be nil", nil)
	}

	// Validate master configuration
	if err := validateMasterConfig(&config.Master); err != nil {
		return errors.NewValidationError("invalid master configuration", err)
	}

	// Validate workers
	if err := validateWorkersConfig(config.Workers); err != nil {
		return errors.NewValidationError("invalid workers configuration", err)
	}

	return nil
}

// CreateWorkersFromConfig creates worker instances from configuration
func CreateWorkersFromConfig(config *MasterConfig, logger logging.Logger) ([]workers.Worker, error) {
	if config == nil {
		return nil, errors.NewValidationError("configuration cannot be nil", nil)
	}

	var workers []workers.Worker

	for i, workerConfig := range config.Workers {
		// Skip disabled workers (only skip if explicitly set to false)
		if workerConfig.Enabled != nil && !*workerConfig.Enabled {
			logger.Infof("Skipping disabled worker, id: %s", workerConfig.ID)
			continue
		}

		worker, err := createWorkerFromConfig(workerConfig, logger)
		if err != nil {
			return nil, errors.NewValidationError(
				fmt.Sprintf("failed to create worker at index %d", i),
				err,
			).WithContext("worker_id", workerConfig.ID).WithContext("worker_index", fmt.Sprintf("%d", i))
		}

		workers = append(workers, worker)
	}

	return workers, nil
}

// createWorkerFromConfig creates a single worker from its configuration
func createWorkerFromConfig(config WorkerConfig, logger logging.Logger) (workers.Worker, error) {
	switch config.Type {
	case WorkerTypeManaged:
		if config.Unit.Managed == nil {
			return nil, errors.NewValidationError("managed unit configuration is required for managed worker", nil)
		}
		return workers.NewManagedWorker(config.ID, config.Unit.Managed, logger), nil

	case WorkerTypeUnmanaged:
		if config.Unit.Unmanaged == nil {
			return nil, errors.NewValidationError("unmanaged unit configuration is required for unmanaged worker", nil)
		}
		return workers.NewUnmanagedWorker(config.ID, config.Unit.Unmanaged, logger), nil

	case WorkerTypeIntegrated:
		if config.Unit.Integrated == nil {
			return nil, errors.NewValidationError("integrated unit configuration is required for integrated worker", nil)
		}
		return workers.NewIntegratedWorker(config.ID, config.Unit.Integrated, logger), nil

	default:
		return nil, errors.NewValidationError(
			fmt.Sprintf("unsupported worker type: %s", config.Type),
			nil,
		).WithContext("supported_types", "managed, unmanaged, integrated")
	}
}

// setConfigDefaults applies default values to configuration
func setConfigDefaults(config *MasterConfig) error {
	// Set master defaults
	if config.Master.Port == 0 {
		config.Master.Port = 50055 // Default port
	}
	if config.Master.LogLevel == "" {
		config.Master.LogLevel = "info"
	}

	// Set worker defaults
	for i := range config.Workers {
		worker := &config.Workers[i]

		// Default enabled to true if not specified
		if worker.Enabled == nil {
			enabled := true
			worker.Enabled = &enabled
		}

		// Apply type-specific defaults
		switch worker.Type {
		case WorkerTypeManaged:
			if worker.Unit.Managed != nil {
				if err := setManagedUnitDefaults(worker.Unit.Managed); err != nil {
					return err
				}
			}
		case WorkerTypeUnmanaged:
			if worker.Unit.Unmanaged != nil {
				if err := setUnmanagedUnitDefaults(worker.Unit.Unmanaged); err != nil {
					return err
				}
			}
		case WorkerTypeIntegrated:
			if worker.Unit.Integrated != nil {
				if err := setIntegratedUnitDefaults(worker.Unit.Integrated); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func setManagedUnitDefaults(config *workers.ManagedUnit) error {
	// Set execution defaults
	if config.Control.Execution.WaitDelay == 0 {
		config.Control.Execution.WaitDelay = 10 * time.Second
	}

	// Set restart defaults
	if config.Control.Restart.Policy == "" {
		config.Control.Restart.Policy = monitoring.RestartOnFailure
	}
	if config.Control.Restart.MaxRetries == 0 {
		config.Control.Restart.MaxRetries = 3
	}
	if config.Control.Restart.RetryDelay == 0 {
		config.Control.Restart.RetryDelay = 5 * time.Second
	}
	if config.Control.Restart.BackoffRate == 0 {
		config.Control.Restart.BackoffRate = 1.5
	}

	return nil
}

func setUnmanagedUnitDefaults(config *workers.UnmanagedUnit) error {
	// Set discovery defaults
	if config.Discovery.CheckInterval == 0 {
		config.Discovery.CheckInterval = 30 * time.Second
	}

	// Set control defaults
	if config.Control.GracefulTimeout == 0 {
		config.Control.GracefulTimeout = 30 * time.Second
	}

	return nil
}

func setIntegratedUnitDefaults(config *workers.IntegratedUnit) error {
	// Set execution defaults
	if config.Control.Execution.WaitDelay == 0 {
		config.Control.Execution.WaitDelay = 10 * time.Second
	}

	// Set health check defaults
	if config.HealthCheckRunOptions.Interval == 0 {
		config.HealthCheckRunOptions.Interval = 30 * time.Second
	}
	if config.HealthCheckRunOptions.Timeout == 0 {
		config.HealthCheckRunOptions.Timeout = 5 * time.Second
	}

	return nil
}

// Validation functions

func validateMasterConfig(config *MasterConfigOptions) error {
	if config.Port <= 0 || config.Port > 65535 {
		return errors.NewValidationError(
			fmt.Sprintf("invalid port number: %d", config.Port),
			nil,
		).WithContext("valid_range", "1-65535")
	}

	validLogLevels := []string{"debug", "info", "warn", "error"}
	if config.LogLevel != "" {
		valid := false
		for _, level := range validLogLevels {
			if config.LogLevel == level {
				valid = true
				break
			}
		}
		if !valid {
			return errors.NewValidationError(
				fmt.Sprintf("invalid log level: %s", config.LogLevel),
				nil,
			).WithContext("valid_levels", "debug, info, warn, error")
		}
	}

	return nil
}

func validateWorkersConfig(workers []WorkerConfig) error {
	if len(workers) == 0 {
		return nil // Allow empty workers list
	}

	// Check for duplicate worker IDs
	seenIDs := make(map[string]int)
	for i, worker := range workers {
		if err := ValidateWorkerID(worker.ID); err != nil {
			return errors.NewValidationError(
				fmt.Sprintf("invalid worker ID at index %d", i),
				err,
			).WithContext("worker_id", worker.ID)
		}

		if prevIndex, exists := seenIDs[worker.ID]; exists {
			return errors.NewValidationError(
				fmt.Sprintf("duplicate worker ID '%s' found at indices %d and %d", worker.ID, prevIndex, i),
				nil,
			)
		}
		seenIDs[worker.ID] = i

		// Validate worker type
		if err := validateWorkerType(worker.Type); err != nil {
			return errors.NewValidationError(
				fmt.Sprintf("invalid worker type at index %d", i),
				err,
			).WithContext("worker_id", worker.ID)
		}

		// Validate unit configuration matches type
		if err := validateWorkerUnitConfig(worker.Type, worker.Unit); err != nil {
			return errors.NewValidationError(
				fmt.Sprintf("invalid unit configuration for worker at index %d", i),
				err,
			).WithContext("worker_id", worker.ID).WithContext("worker_type", string(worker.Type))
		}
	}

	return nil
}

func validateWorkerType(workerType WorkerType) error {
	validTypes := []WorkerType{WorkerTypeManaged, WorkerTypeUnmanaged, WorkerTypeIntegrated}
	for _, validType := range validTypes {
		if workerType == validType {
			return nil
		}
	}

	return errors.NewValidationError(
		fmt.Sprintf("unsupported worker type: %s", workerType),
		nil,
	).WithContext("supported_types", "managed, unmanaged, integrated")
}

func validateWorkerUnitConfig(workerType WorkerType, unitConfig WorkerUnitConfig) error {
	switch workerType {
	case WorkerTypeManaged:
		if unitConfig.Managed == nil {
			return errors.NewValidationError("managed unit configuration is required for managed worker", nil)
		}
		if unitConfig.Unmanaged != nil || unitConfig.Integrated != nil {
			return errors.NewValidationError("only managed unit configuration should be specified for managed worker", nil)
		}
		return workers.ValidateManagedUnit(*unitConfig.Managed)

	case WorkerTypeUnmanaged:
		if unitConfig.Unmanaged == nil {
			return errors.NewValidationError("unmanaged unit configuration is required for unmanaged worker", nil)
		}
		if unitConfig.Managed != nil || unitConfig.Integrated != nil {
			return errors.NewValidationError("only unmanaged unit configuration should be specified for unmanaged worker", nil)
		}
		return workers.ValidateUnmanagedUnit(*unitConfig.Unmanaged)

	case WorkerTypeIntegrated:
		if unitConfig.Integrated == nil {
			return errors.NewValidationError("integrated unit configuration is required for integrated worker", nil)
		}
		if unitConfig.Managed != nil || unitConfig.Unmanaged != nil {
			return errors.NewValidationError("only integrated unit configuration should be specified for integrated worker", nil)
		}
		return workers.ValidateIntegratedUnit(*unitConfig.Integrated)

	default:
		return errors.NewValidationError(fmt.Sprintf("unsupported worker type: %s", workerType), nil)
	}
}
