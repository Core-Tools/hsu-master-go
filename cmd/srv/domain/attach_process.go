package domain

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/core-tools/hsu-master/pkg/logging"
)

// AttachCmd represents a command that attaches to an existing process
type AttachCmd func(config DiscoveryConfig) (*os.Process, io.ReadCloser, *HealthCheckConfig, error)

// NewStdAttachCmd creates a standard attachment command function with logging
func NewStdAttachCmd(healthCheckConfig *HealthCheckConfig, logger logging.Logger, workerID string) AttachCmd {
	return func(config DiscoveryConfig) (*os.Process, io.ReadCloser, *HealthCheckConfig, error) {
		logger.Infof("Executing standard attach command, worker: %s, discovery: %s, config: %+v", workerID, config.Method, config)

		// Validate discovery configuration
		if err := ValidateDiscoveryConfig(config); err != nil {
			logger.Errorf("Discovery configuration validation failed, worker: %s, error: %v", workerID, err)
			return nil, nil, nil, NewValidationError("invalid discovery configuration", err)
		}

		var process *os.Process
		var err error

		// Delegate to specific discovery method
		logger.Debugf("Starting process discovery, worker: %s, method: %s", workerID, config.Method)
		switch config.Method {
		case DiscoveryMethodPIDFile:
			logger.Debugf("Discovering process by PID file, worker: %s, file: %s", workerID, config.PIDFile)
			process, err = openProcessByPIDFile(config.PIDFile)
		case DiscoveryMethodProcessName:
			logger.Debugf("Discovering process by name, worker: %s, name: %s, args: %v", workerID, config.ProcessName, config.ProcessArgs)
			process, err = openProcessByName(config.ProcessName, config.ProcessArgs)
		case DiscoveryMethodPort:
			logger.Debugf("Discovering process by port, worker: %s, port: %d, protocol: %s", workerID, config.Port, config.Protocol)
			process, err = openProcessByPort(config.Port, config.Protocol)
		case DiscoveryMethodServiceName:
			logger.Debugf("Discovering process by service name, worker: %s, service: %s", workerID, config.ServiceName)
			process, err = openProcessByServiceName(config.ServiceName)
		default:
			logger.Errorf("Unsupported discovery method, worker: %s, method: %s", workerID, config.Method)
			return nil, nil, nil, NewValidationError("unsupported discovery method: "+string(config.Method), nil)
		}

		if err != nil {
			logger.Errorf("Failed to discover process, worker: %s, method: %s, error: %v", workerID, config.Method, err)
			return nil, nil, nil, NewDiscoveryError("failed to discover process", err).WithContext("discovery_method", string(config.Method))
		}

		logger.Infof("Successfully attached to process, worker: %s, PID: %d, discovery: %s", workerID, process.Pid, config.Method)

		// For attached processes:
		// - ProcessState is nil (we haven't waited on the process)
		// - stdout is nil (we can't access stdout of existing processes)
		// - HealthCheckConfig comes from the caller (worker-specific)
		return process, nil, healthCheckConfig, nil
	}
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
	if !isProcessRunning(process.Pid) {
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
