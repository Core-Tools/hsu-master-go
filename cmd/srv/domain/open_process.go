package domain

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

func OpenProcess(config DiscoveryConfig) (*os.Process, *os.ProcessState, io.ReadCloser, *HealthCheckConfig, error) {
	// Validate discovery configuration
	if err := ValidateDiscoveryConfig(config); err != nil {
		return nil, nil, nil, nil, NewValidationError("invalid discovery configuration", err)
	}

	var process *os.Process
	var err error

	// Delegate to specific discovery method
	switch config.Method {
	case DiscoveryMethodPIDFile:
		process, err = openProcessByPIDFile(config.PIDFile)
	case DiscoveryMethodProcessName:
		process, err = openProcessByName(config.ProcessName, config.ProcessArgs)
	case DiscoveryMethodPort:
		process, err = openProcessByPort(config.Port, config.Protocol)
	case DiscoveryMethodServiceName:
		process, err = openProcessByServiceName(config.ServiceName)
	default:
		return nil, nil, nil, nil, NewValidationError("unsupported discovery method: "+string(config.Method), nil)
	}

	if err != nil {
		return nil, nil, nil, nil, NewDiscoveryError("failed to discover process", err).WithContext("discovery_method", string(config.Method))
	}

	// Create common health check configuration for attached processes
	healthCheckConfig := &HealthCheckConfig{
		Type: HealthCheckTypeProcess,
		RunOptions: HealthCheckRunOptions{
			Interval:         30 * time.Second,
			Timeout:          5 * time.Second,
			SuccessThreshold: 1,
			FailureThreshold: 3,
		},
	}

	// For attached processes:
	// - ProcessState is nil (we haven't waited on the process)
	// - stdout is nil (we can't access stdout of existing processes)
	// - HealthCheckConfig uses process monitoring
	return process, nil, nil, healthCheckConfig, nil
}

// openProcessByPIDFile discovers a process by reading its PID from a file
func openProcessByPIDFile(pidFile string) (*os.Process, error) {
	// Validate PID file path
	if err := ValidatePIDFile(pidFile); err != nil {
		return nil, err
	}

	// Read PID from file
	pidBytes, err := os.ReadFile(pidFile)
	if err != nil {
		return nil, NewIOError("failed to read PID file", err).WithContext("pid_file", pidFile)
	}

	// Parse PID (trim whitespace/newlines)
	pidStr := strings.TrimSpace(string(pidBytes))
	if pidStr == "" {
		return nil, NewValidationError("PID file is empty", nil).WithContext("pid_file", pidFile)
	}

	pid, err := ValidatePID(pidStr)
	if err != nil {
		return nil, NewValidationError("invalid PID in file", err).WithContext("pid_file", pidFile).WithContext("pid_content", pidStr)
	}

	// Find the process
	process, err := os.FindProcess(pid)
	if err != nil {
		return nil, NewProcessError("failed to find process", err).WithContext("pid", pid).WithContext("pid_file", pidFile)
	}

	// Verify the process is actually running
	if err := verifyProcessRunning(process); err != nil {
		return nil, NewProcessError("process is not running", err).WithContext("pid", pid).WithContext("pid_file", pidFile)
	}

	return process, nil
}

// openProcessByName discovers a process by its executable name and optional arguments
func openProcessByName(processName string, processArgs []string) (*os.Process, error) {
	// TODO: Implement process discovery by name
	// This would involve scanning running processes (e.g., via /proc on Linux, or WMI on Windows)
	// processArgs can be used for additional filtering when multiple processes have the same name
	return nil, fmt.Errorf("process discovery by name not implemented yet")
}

// openProcessByPort discovers a process by the port it's listening on
func openProcessByPort(port int, protocol string) (*os.Process, error) {
	// TODO: Implement process discovery by port
	// This would involve scanning network connections and finding which process owns the port
	// protocol specifies "tcp" or "udp"
	return nil, fmt.Errorf("process discovery by port not implemented yet")
}

// openProcessByServiceName discovers a process by its service name (systemd, Windows services, etc.)
func openProcessByServiceName(serviceName string) (*os.Process, error) {
	// TODO: Implement process discovery by service name
	// This would involve querying the service manager (systemd, Windows service manager, etc.)
	return nil, fmt.Errorf("process discovery by service name not implemented yet")
}
