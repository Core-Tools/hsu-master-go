package workers

import (
	"github.com/core-tools/hsu-master/pkg/errors"
	"github.com/core-tools/hsu-master/pkg/monitoring"
	"github.com/core-tools/hsu-master/pkg/process"
	"github.com/core-tools/hsu-master/pkg/workers/processcontrol"
)

func ValidateManagedUnit(config ManagedUnit) error {
	// Validate metadata
	if config.Metadata.Name == "" {
		return errors.NewValidationError("unit name is required", nil)
	}

	// Validate execution config
	if config.Control.Execution.ExecutablePath == "" {
		return errors.NewValidationError("executable path is required for managed worker", nil)
	}

	// Additional validation using existing functions
	if err := process.ValidateExecutionConfig(config.Control.Execution); err != nil {
		return err
	}

	if err := monitoring.ValidateRestartConfig(config.Control.Restart); err != nil {
		return err
	}

	if config.HealthCheck.Type != "" {
		if err := monitoring.ValidateHealthCheckConfig(config.HealthCheck); err != nil {
			return err
		}
	}

	return nil
}

func ValidateUnmanagedUnit(config UnmanagedUnit) error {
	// Validate metadata
	if config.Metadata.Name == "" {
		return errors.NewValidationError("unit name is required", nil)
	}

	// Validate discovery config
	if err := process.ValidateDiscoveryConfig(config.Discovery); err != nil {
		return err
	}

	// Validate health check if specified
	if config.HealthCheck.Type != "" {
		if err := monitoring.ValidateHealthCheckConfig(config.HealthCheck); err != nil {
			return err
		}
	}

	return nil
}

func ValidateIntegratedUnit(config IntegratedUnit) error {
	// Validate metadata
	if config.Metadata.Name == "" {
		return errors.NewValidationError("unit name is required", nil)
	}

	// Validate execution config
	if config.Control.Execution.ExecutablePath == "" {
		return errors.NewValidationError("executable path is required for integrated worker", nil)
	}

	// Additional validation
	if err := process.ValidateExecutionConfig(config.Control.Execution); err != nil {
		return err
	}

	if err := monitoring.ValidateRestartConfig(config.Control.Restart); err != nil {
		return err
	}

	return nil
}

// ValidateProcessControlOptions validates process control options
func ValidateProcessControlOptions(options processcontrol.ProcessControlOptions) error {
	// Validate graceful timeout
	if options.GracefulTimeout < 0 {
		return errors.NewValidationError("graceful timeout cannot be negative", nil)
	}

	// Validate restart config if provided
	if options.Restart != nil {
		if err := monitoring.ValidateRestartConfig(*options.Restart); err != nil {
			return errors.NewValidationError("invalid restart configuration", err)
		}
	}

	// Validate resource limits if provided
	if options.Limits != nil {
		if err := process.ValidateResourceLimits(*options.Limits); err != nil {
			return errors.NewValidationError("invalid resource limits configuration", err)
		}
	}

	// Validate health check config if provided
	if options.HealthCheck != nil {
		if err := monitoring.ValidateHealthCheckConfig(*options.HealthCheck); err != nil {
			return errors.NewValidationError("invalid health check configuration", err)
		}
	}

	// Validate consistency
	if !options.CanAttach && options.ExecuteCmd == nil {
		return errors.NewValidationError("either CanAttach must be true or ExecuteCmd must be provided", nil)
	}

	if options.CanAttach && options.AttachCmd == nil {
		return errors.NewValidationError("AttachCmd must be provided if CanAttach is true", nil)
	}

	return nil
}
