package domain

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ValidateWorkerID validates worker ID format and constraints
func ValidateWorkerID(id string) error {
	if id == "" {
		return NewValidationError("worker ID cannot be empty", nil)
	}

	if len(id) > 64 {
		return NewValidationError("worker ID cannot exceed 64 characters", nil)
	}

	// Check for invalid characters
	for _, char := range id {
		if !isValidIDChar(char) {
			return NewValidationError("worker ID contains invalid characters: only letters, numbers, hyphens, and underscores are allowed", nil)
		}
	}

	return nil
}

// ValidateProcessControlOptions validates process control options
func ValidateProcessControlOptions(options ProcessControlOptions) error {
	// Validate discovery config
	if err := ValidateDiscoveryConfig(options.Discovery); err != nil {
		return NewValidationError("invalid discovery configuration", err)
	}

	// Validate graceful timeout
	if options.GracefulTimeout < 0 {
		return NewValidationError("graceful timeout cannot be negative", nil)
	}

	// Validate restart config if provided
	if options.Restart != nil {
		if err := ValidateRestartConfig(*options.Restart); err != nil {
			return NewValidationError("invalid restart configuration", err)
		}
	}

	// Validate resource limits if provided
	if options.Limits != nil {
		if err := ValidateResourceLimits(*options.Limits); err != nil {
			return NewValidationError("invalid resource limits configuration", err)
		}
	}

	// Validate health check config if provided
	if options.HealthCheck != nil {
		if err := ValidateHealthCheckConfig(*options.HealthCheck); err != nil {
			return NewValidationError("invalid health check configuration", err)
		}
	}

	// Validate consistency
	if !options.CanAttach && options.ExecuteCmd == nil {
		return NewValidationError("either CanAttach must be true or ExecuteCmd must be provided", nil)
	}

	if options.CanAttach && options.AttachCmd == nil {
		return NewValidationError("AttachCmd must be provided if CanAttach is true", nil)
	}

	return nil
}

// ValidateDiscoveryConfig validates discovery configuration
func ValidateDiscoveryConfig(config DiscoveryConfig) error {
	switch config.Method {
	case DiscoveryMethodPIDFile:
		if config.PIDFile == "" {
			return NewValidationError("PID file path is required for PID file discovery", nil)
		}
		if !filepath.IsAbs(config.PIDFile) {
			return NewValidationError("PID file path must be absolute", nil)
		}

	case DiscoveryMethodProcessName:
		if config.ProcessName == "" {
			return NewValidationError("process name is required for process name discovery", nil)
		}

	case DiscoveryMethodPort:
		if config.Port <= 0 || config.Port > 65535 {
			return NewValidationError("port must be between 1 and 65535", nil)
		}
		if config.Protocol != "tcp" && config.Protocol != "udp" {
			return NewValidationError("protocol must be 'tcp' or 'udp'", nil)
		}

	case DiscoveryMethodServiceName:
		if config.ServiceName == "" {
			return NewValidationError("service name is required for service name discovery", nil)
		}

	default:
		return NewValidationError("unsupported discovery method: "+string(config.Method), nil)
	}

	// Validate check interval
	if config.CheckInterval < 0 {
		return NewValidationError("check interval cannot be negative", nil)
	}

	return nil
}

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
		return NewValidationError("invalid restart policy: "+string(config.Policy), nil)
	}

	// Validate max retries
	if config.MaxRetries < 0 {
		return NewValidationError("max retries cannot be negative", nil)
	}

	// Validate retry delay
	if config.RetryDelay < 0 {
		return NewValidationError("retry delay cannot be negative", nil)
	}

	// Validate backoff rate
	if config.BackoffRate < 1.0 {
		return NewValidationError("backoff rate must be at least 1.0", nil)
	}

	return nil
}

// ValidateResourceLimits validates resource limits configuration
func ValidateResourceLimits(limits ResourceLimits) error {
	// Validate CPU limits
	if limits.CPU < 0 {
		return NewValidationError("CPU limit cannot be negative", nil)
	}

	if limits.CPUShares < 0 {
		return NewValidationError("CPU shares cannot be negative", nil)
	}

	// Validate memory limits
	if limits.Memory < 0 {
		return NewValidationError("memory limit cannot be negative", nil)
	}

	if limits.MemorySwap < 0 {
		return NewValidationError("memory swap limit cannot be negative", nil)
	}

	// Validate process limits
	if limits.MaxProcesses < 0 {
		return NewValidationError("max processes cannot be negative", nil)
	}

	if limits.MaxOpenFiles < 0 {
		return NewValidationError("max open files cannot be negative", nil)
	}

	// Validate I/O limits
	if limits.IOWeight < 0 {
		return NewValidationError("I/O weight cannot be negative", nil)
	}

	if limits.IOReadBPS < 0 {
		return NewValidationError("I/O read BPS cannot be negative", nil)
	}

	if limits.IOWriteBPS < 0 {
		return NewValidationError("I/O write BPS cannot be negative", nil)
	}

	return nil
}

// ValidateHealthCheckConfig validates health check configuration
func ValidateHealthCheckConfig(config HealthCheckConfig) error {
	// Validate run options
	if err := ValidateHealthCheckRunOptions(config.RunOptions); err != nil {
		return NewValidationError("invalid health check run options", err)
	}

	// Validate type-specific configuration
	switch config.Type {
	case HealthCheckTypeHTTP:
		if config.HTTP.URL == "" {
			return NewValidationError("HTTP URL is required for HTTP health check", nil)
		}

	case HealthCheckTypeGRPC:
		if config.GRPC.Address == "" {
			return NewValidationError("gRPC address is required for gRPC health check", nil)
		}
		if config.GRPC.Service == "" {
			return NewValidationError("gRPC service is required for gRPC health check", nil)
		}
		if config.GRPC.Method == "" {
			return NewValidationError("gRPC method is required for gRPC health check", nil)
		}

	case HealthCheckTypeTCP:
		if config.TCP.Address == "" {
			return NewValidationError("TCP address is required for TCP health check", nil)
		}
		if config.TCP.Port <= 0 || config.TCP.Port > 65535 {
			return NewValidationError("TCP port must be between 1 and 65535", nil)
		}

	case HealthCheckTypeExec:
		if config.Exec.Command == "" {
			return NewValidationError("command is required for exec health check", nil)
		}

	case HealthCheckTypeProcess:
		// Process health check doesn't need additional validation

	default:
		return NewValidationError("unsupported health check type: "+string(config.Type), nil)
	}

	return nil
}

// ValidateHealthCheckRunOptions validates health check run options
func ValidateHealthCheckRunOptions(options HealthCheckRunOptions) error {
	if options.Interval <= 0 {
		return NewValidationError("health check interval must be positive", nil)
	}

	if options.Timeout <= 0 {
		return NewValidationError("health check timeout must be positive", nil)
	}

	if options.Timeout >= options.Interval {
		return NewValidationError("health check timeout must be less than interval", nil)
	}

	if options.Retries < 0 {
		return NewValidationError("health check retries cannot be negative", nil)
	}

	if options.SuccessThreshold <= 0 {
		return NewValidationError("health check success threshold must be positive", nil)
	}

	if options.FailureThreshold <= 0 {
		return NewValidationError("health check failure threshold must be positive", nil)
	}

	return nil
}

// ValidateExecutionConfig validates execution configuration
func ValidateExecutionConfig(config ExecutionConfig) error {
	// Validate executable path
	if config.ExecutablePath == "" {
		return NewValidationError("executable path is required", nil)
	}

	// Check if executable exists
	if _, err := os.Stat(config.ExecutablePath); os.IsNotExist(err) {
		return NewValidationError("executable not found: "+config.ExecutablePath, err)
	}

	// Validate working directory if provided
	if config.WorkingDirectory != "" {
		if !filepath.IsAbs(config.WorkingDirectory) {
			return NewValidationError("working directory must be absolute path", nil)
		}

		if info, err := os.Stat(config.WorkingDirectory); err != nil {
			return NewValidationError("working directory not accessible: "+config.WorkingDirectory, err)
		} else if !info.IsDir() {
			return NewValidationError("working directory is not a directory: "+config.WorkingDirectory, nil)
		}
	}

	// Validate environment variables
	for _, env := range config.Environment {
		if !strings.Contains(env, "=") {
			return NewValidationError("invalid environment variable format: "+env, nil)
		}
	}

	// Validate wait delay
	if config.WaitDelay < 0 {
		return NewValidationError("wait delay cannot be negative", nil)
	}

	return nil
}

// ValidatePIDFile validates PID file format and accessibility
func ValidatePIDFile(pidFile string) error {
	if pidFile == "" {
		return NewValidationError("PID file path cannot be empty", nil)
	}

	if !filepath.IsAbs(pidFile) {
		return NewValidationError("PID file path must be absolute", nil)
	}

	// Check if parent directory exists
	dir := filepath.Dir(pidFile)
	if info, err := os.Stat(dir); err != nil {
		return NewIOError("PID file directory not accessible: "+dir, err)
	} else if !info.IsDir() {
		return NewValidationError("PID file parent is not a directory: "+dir, nil)
	}

	return nil
}

// ValidatePID validates PID value
func ValidatePID(pidStr string) (int, error) {
	if pidStr == "" {
		return 0, NewValidationError("PID cannot be empty", nil)
	}

	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, NewValidationError("invalid PID format: "+pidStr, err)
	}

	if pid <= 0 {
		return 0, NewValidationError("PID must be positive: "+pidStr, nil)
	}

	return pid, nil
}

// ValidatePort validates port number
func ValidatePort(port int) error {
	if port <= 0 || port > 65535 {
		return NewValidationError("port must be between 1 and 65535", nil)
	}
	return nil
}

// ValidateNetworkAddress validates network address format
func ValidateNetworkAddress(address string) error {
	if address == "" {
		return NewValidationError("network address cannot be empty", nil)
	}

	// Try to parse as host:port
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return NewValidationError("invalid network address format: "+address, err)
	}

	// Validate host
	if host == "" {
		return NewValidationError("host cannot be empty in address: "+address, nil)
	}

	// Validate port
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return NewValidationError("invalid port in address: "+address, err)
	}

	if err := ValidatePort(port); err != nil {
		return NewValidationError("invalid port in address: "+address, err)
	}

	return nil
}

// ValidateTimeout validates timeout duration
func ValidateTimeout(timeout time.Duration, name string) error {
	if timeout < 0 {
		return NewValidationError(name+" timeout cannot be negative", nil)
	}

	if timeout == 0 {
		return NewValidationError(name+" timeout cannot be zero", nil)
	}

	return nil
}

// Helper function to check if character is valid for ID
func isValidIDChar(char rune) bool {
	return (char >= 'a' && char <= 'z') ||
		(char >= 'A' && char <= 'Z') ||
		(char >= '0' && char <= '9') ||
		char == '-' || char == '_'
}
