package monitoring

import "github.com/core-tools/hsu-master/pkg/errors"

// ValidateRestartConfig validates restart configuration
func ValidateRestartConfig(config RestartConfig) error {
	// Validate restart policy
	validPolicies := []RestartPolicy{
		RestartNever,
		RestartOnFailure,
		RestartAlways,
		RestartUnlessStopped,
	}

	isValid := false
	for _, policy := range validPolicies {
		if config.Policy == policy {
			isValid = true
			break
		}
	}

	if !isValid {
		return errors.NewValidationError("invalid restart policy: "+string(config.Policy), nil)
	}

	// Validate max retries
	if config.MaxRetries < 0 {
		return errors.NewValidationError("max retries cannot be negative", nil)
	}

	// Validate retry delay
	if config.RetryDelay < 0 {
		return errors.NewValidationError("retry delay cannot be negative", nil)
	}

	// Validate backoff rate
	if config.BackoffRate < 1.0 {
		return errors.NewValidationError("backoff rate must be at least 1.0", nil)
	}

	return nil
}

// ValidateHealthCheckConfig validates health check configuration
func ValidateHealthCheckConfig(config HealthCheckConfig) error {
	// Validate run options
	if err := ValidateHealthCheckRunOptions(config.RunOptions); err != nil {
		return errors.NewValidationError("invalid health check run options", err)
	}

	// Validate type-specific configuration
	switch config.Type {
	case HealthCheckTypeHTTP:
		if config.HTTP.URL == "" {
			return errors.NewValidationError("HTTP URL is required for HTTP health check", nil)
		}

	case HealthCheckTypeGRPC:
		if config.GRPC.Address == "" {
			return errors.NewValidationError("gRPC address is required for gRPC health check", nil)
		}
		if config.GRPC.Service == "" {
			return errors.NewValidationError("gRPC service is required for gRPC health check", nil)
		}
		if config.GRPC.Method == "" {
			return errors.NewValidationError("gRPC method is required for gRPC health check", nil)
		}

	case HealthCheckTypeTCP:
		if config.TCP.Address == "" {
			return errors.NewValidationError("TCP address is required for TCP health check", nil)
		}
		if config.TCP.Port <= 0 || config.TCP.Port > 65535 {
			return errors.NewValidationError("TCP port must be between 1 and 65535", nil)
		}

	case HealthCheckTypeExec:
		if config.Exec.Command == "" {
			return errors.NewValidationError("command is required for exec health check", nil)
		}

	case HealthCheckTypeProcess:
		// Process health check doesn't need additional validation

	default:
		return errors.NewValidationError("unsupported health check type: "+string(config.Type), nil)
	}

	return nil
}

// ValidateHealthCheckRunOptions validates health check run options
func ValidateHealthCheckRunOptions(options HealthCheckRunOptions) error {
	if options.Interval <= 0 {
		return errors.NewValidationError("health check interval must be positive", nil)
	}

	if options.Timeout <= 0 {
		return errors.NewValidationError("health check timeout must be positive", nil)
	}

	if options.Timeout >= options.Interval {
		return errors.NewValidationError("health check timeout must be less than interval", nil)
	}

	if options.Retries < 0 {
		return errors.NewValidationError("health check retries cannot be negative", nil)
	}

	return nil
}
