package domain

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

func OpenProcess(config DiscoveryConfig) (*os.Process, *os.ProcessState, io.ReadCloser, *HealthCheckConfig, error) {
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
		return nil, nil, nil, nil, fmt.Errorf("unsupported discovery method: %s", config.Method)
	}

	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to discover process: %v", err)
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
	if pidFile == "" {
		return nil, fmt.Errorf("PID file path is required")
	}

	// Read PID from file
	pidBytes, err := os.ReadFile(pidFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read PID file %s: %v", pidFile, err)
	}

	// Parse PID (trim whitespace/newlines)
	pidStr := strings.TrimSpace(string(pidBytes))
	if pidStr == "" {
		return nil, fmt.Errorf("PID file %s is empty", pidFile)
	}

	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return nil, fmt.Errorf("invalid PID in file %s: %v", pidFile, err)
	}

	if pid <= 0 {
		return nil, fmt.Errorf("invalid PID %d in file %s", pid, pidFile)
	}

	// Find the process
	process, err := os.FindProcess(pid)
	if err != nil {
		return nil, fmt.Errorf("failed to find process with PID %d: %v", pid, err)
	}

	// Verify the process is actually running
	if err := verifyProcessRunning(process); err != nil {
		return nil, fmt.Errorf("process PID %d is not running: %v", pid, err)
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
