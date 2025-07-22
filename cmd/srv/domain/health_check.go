package domain

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/core-tools/hsu-master/pkg/logging"
)

type HealthCheckType string

const (
	HealthCheckTypeHTTP    HealthCheckType = "http"
	HealthCheckTypeGRPC    HealthCheckType = "grpc"
	HealthCheckTypeTCP     HealthCheckType = "tcp"
	HealthCheckTypeExec    HealthCheckType = "exec"
	HealthCheckTypeProcess HealthCheckType = "process"
)

type HTTPHealthCheckConfig struct {
	URL     string            `yaml:"url"`
	PMethod string            `yaml:"method,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty"`
}

type GRPCHealthCheckConfig struct {
	Address string            `yaml:"address"`
	Service string            `yaml:"service,omitempty"`
	Method  string            `yaml:"method,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty"`
}

type TCPHealthCheckConfig struct {
	Address string `yaml:"address"`
	Port    int    `yaml:"port"`
}

type ExecHealthCheckConfig struct {
	Command string   `yaml:"command"`
	Args    []string `yaml:"args,omitempty"`
}

type HealthCheckConfig struct {
	Type HealthCheckType `yaml:"type"`

	// HTTP health check
	HTTP HTTPHealthCheckConfig `yaml:"http,omitempty"`

	// GRPC health check
	GRPC GRPCHealthCheckConfig `yaml:"grpc,omitempty"`

	// TCP health check
	TCP TCPHealthCheckConfig `yaml:"tcp,omitempty"`

	// Exec health check
	Exec ExecHealthCheckConfig `yaml:"exec,omitempty"`

	// Run options
	RunOptions HealthCheckRunOptions `yaml:"run_options,omitempty"`
}

type HealthCheckRunOptions struct {
	Enabled      bool          `yaml:"enabled,omitempty"`
	Interval     time.Duration `yaml:"interval,omitempty"`
	Timeout      time.Duration `yaml:"timeout,omitempty"`
	InitialDelay time.Duration `yaml:"initial_delay,omitempty"`
	Retries      int           `yaml:"retries,omitempty"`
}

type HealthCheckStatus string

const (
	HealthCheckStatusUnknown   HealthCheckStatus = "unknown"
	HealthCheckStatusHealthy   HealthCheckStatus = "healthy"
	HealthCheckStatusDegraded  HealthCheckStatus = "degraded"
	HealthCheckStatusUnhealthy HealthCheckStatus = "unhealthy"
)

type HealthCheckState struct {
	Status               HealthCheckStatus
	LastCheck            time.Time
	Message              string
	ConsecutiveFailures  int
	ConsecutiveSuccesses int
	Retries              int
}

// HealthRestartCallback defines a callback function for triggering restarts on health failures
type HealthRestartCallback func(reason string) error

// HealthRecoveryCallback defines a callback function for when health recovers
type HealthRecoveryCallback func()

type HealthMonitor interface {
	State() *HealthCheckState
	Start()
	Stop()
	SetRestartCallback(callback HealthRestartCallback)   // Add restart callback
	SetRecoveryCallback(callback HealthRecoveryCallback) // Add recovery callback
}

type healthMonitor struct {
	config           *HealthCheckConfig
	state            *HealthCheckState
	stopChan         chan struct{}
	wg               sync.WaitGroup
	mutex            sync.Mutex
	logger           logging.Logger
	workerID         string
	processInfo      *ProcessInfo           // Add process information for health checking
	restartCallback  HealthRestartCallback  // Callback for triggering restarts
	recoveryCallback HealthRecoveryCallback // Callback for health recovery
	restartPolicy    *RestartConfig         // Restart policy configuration
}

// ProcessInfo holds process information needed for health monitoring
type ProcessInfo struct {
	PID int // Only PID is needed for process health checking
}

func NewHealthMonitor(config *HealthCheckConfig, logger logging.Logger, workerID string) HealthMonitor {
	return &healthMonitor{
		config:   config,
		state:    &HealthCheckState{Status: HealthCheckStatusUnknown},
		stopChan: make(chan struct{}),
		logger:   logger,
		workerID: workerID,
	}
}

// NewHealthMonitorWithProcessInfo creates a health monitor with process information for process health checks
func NewHealthMonitorWithProcessInfo(config *HealthCheckConfig, logger logging.Logger, workerID string, processInfo *ProcessInfo) HealthMonitor {
	return &healthMonitor{
		config:      config,
		state:       &HealthCheckState{Status: HealthCheckStatusUnknown},
		stopChan:    make(chan struct{}),
		logger:      logger,
		workerID:    workerID,
		processInfo: processInfo,
	}
}

// NewHealthMonitorWithRestart creates a health monitor with restart capability
func NewHealthMonitorWithRestart(config *HealthCheckConfig, logger logging.Logger, workerID string, processInfo *ProcessInfo, restartPolicy *RestartConfig) HealthMonitor {
	return &healthMonitor{
		config:        config,
		state:         &HealthCheckState{Status: HealthCheckStatusUnknown},
		stopChan:      make(chan struct{}),
		logger:        logger,
		workerID:      workerID,
		processInfo:   processInfo,
		restartPolicy: restartPolicy,
	}
}

func (h *healthMonitor) State() *HealthCheckState {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	// Return a copy to avoid race conditions
	stateCopy := *h.state
	return &stateCopy
}

func (h *healthMonitor) Start() {
	h.logger.Infof("Starting health monitor, worker: %s, type: %s, interval: %v", h.workerID, h.config.Type, h.config.RunOptions.Interval)

	h.wg.Add(1)
	go h.loop()
}

func (h *healthMonitor) Stop() {
	h.logger.Infof("Stopping health monitor, worker: %s", h.workerID)
	close(h.stopChan)
	h.wg.Wait()
	h.logger.Infof("Health monitor stopped, worker: %s", h.workerID)
}

func (h *healthMonitor) loop() {
	defer h.wg.Done()

	if h.config.Type == "" {
		h.logger.Debugf("Health monitor loop is disabled due to empty type, worker: %s", h.workerID)
		return
	}

	h.logger.Debugf("Health monitor loop started, worker: %s", h.workerID)

	// Initial delay before first check
	if h.config.RunOptions.InitialDelay > 0 {
		h.logger.Debugf("Health monitor initial delay, worker: %s, delay: %v", h.workerID, h.config.RunOptions.InitialDelay)
		select {
		case <-time.After(h.config.RunOptions.InitialDelay):
		case <-h.stopChan:
			h.logger.Debugf("Health monitor stopped during initial delay, worker: %s", h.workerID)
			return
		}
	}

	ticker := time.NewTicker(h.config.RunOptions.Interval)
	defer ticker.Stop()

	// Perform initial check
	h.performCheck()

	for {
		select {
		case <-ticker.C:
			h.performCheck()
		case <-h.stopChan:
			h.logger.Debugf("Health monitor loop stopping, worker: %s", h.workerID)
			return
		}
	}
}

func (h *healthMonitor) performCheck() {
	h.logger.Debugf("Performing health check, worker: %s, type: %s", h.workerID, h.config.Type)

	h.mutex.Lock()
	h.state.LastCheck = time.Now()
	h.mutex.Unlock()

	var isHealthy bool
	var message string

	switch h.config.Type {
	case HealthCheckTypeHTTP:
		isHealthy, message = h.checkHTTP()
	case HealthCheckTypeGRPC:
		isHealthy, message = h.checkGRPC()
	case HealthCheckTypeTCP:
		isHealthy, message = h.checkTCP()
	case HealthCheckTypeExec:
		isHealthy, message = h.checkExec()
	case HealthCheckTypeProcess:
		isHealthy, message = h.checkProcess()
	default:
		isHealthy = false
		message = "Unknown health check type: " + string(h.config.Type)
		h.logger.Errorf("Unknown health check type, worker: %s, type: %s", h.workerID, h.config.Type)
	}

	h.updateState(isHealthy, message)
}

func (h *healthMonitor) updateState(isHealthy bool, message string) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	previousStatus := h.state.Status

	if isHealthy {
		h.state.ConsecutiveSuccesses++
		h.state.ConsecutiveFailures = 0
		h.state.Retries = 0

		previousWasUnhealthy := previousStatus == HealthCheckStatusDegraded || previousStatus == HealthCheckStatusUnhealthy

		if h.state.Status != HealthCheckStatusHealthy {
			h.state.Status = HealthCheckStatusHealthy
			h.logger.Infof("Health check recovered, worker: %s, previous: %s, consecutive_successes: %d",
				h.workerID, previousStatus, h.state.ConsecutiveSuccesses)

			// Call recovery callback if process recovered from unhealthy state
			if previousWasUnhealthy && h.recoveryCallback != nil {
				h.logger.Infof("Triggering recovery callback, worker: %s, recovered from: %s", h.workerID, previousStatus)
				go h.recoveryCallback() // Call in goroutine to avoid blocking health check
			}
		} else {
			h.logger.Debugf("Health check passed, worker: %s, consecutive_successes: %d",
				h.workerID, h.state.ConsecutiveSuccesses)
		}
	} else {
		h.state.ConsecutiveFailures++
		h.state.ConsecutiveSuccesses = 0

		// Determine new status based on failure count
		var newStatus HealthCheckStatus
		if h.state.ConsecutiveFailures == 1 {
			newStatus = HealthCheckStatusDegraded
		} else {
			newStatus = HealthCheckStatusUnhealthy
		}

		if h.state.Status != newStatus {
			h.state.Status = newStatus
			h.logger.Warnf("Health check status changed, worker: %s, status: %s->%s, consecutive_failures: %d, message: %s",
				h.workerID, previousStatus, newStatus, h.state.ConsecutiveFailures, message)
		} else {
			h.logger.Warnf("Health check failed, worker: %s, status: %s, consecutive_failures: %d, message: %s",
				h.workerID, h.state.Status, h.state.ConsecutiveFailures, message)
		}

		// Check if we should trigger a restart
		h.checkRestartCondition(message)
	}

	h.state.Message = message
}

// checkRestartCondition determines if a restart should be triggered based on health failures
func (h *healthMonitor) checkRestartCondition(message string) {
	// Only trigger restart if we have both restart policy and callback
	if h.restartPolicy == nil || h.restartCallback == nil {
		return
	}

	// Check restart policy
	shouldRestart := false
	switch h.restartPolicy.Policy {
	case RestartAlways:
		shouldRestart = true
	case RestartOnFailure:
		// Restart on health check failures if we've reached the threshold
		shouldRestart = h.state.Status == HealthCheckStatusUnhealthy
	case RestartUnlessStopped:
		// Similar to always, but should check if process was intentionally stopped
		shouldRestart = h.state.Status == HealthCheckStatusUnhealthy
	case RestartNever:
		shouldRestart = false
	}

	if !shouldRestart {
		return
	}

	// Check if we've exceeded max retries
	if h.restartPolicy.MaxRetries > 0 && h.state.Retries >= h.restartPolicy.MaxRetries {
		h.logger.Errorf("Max restart retries exceeded, worker: %s, retries: %d, max: %d",
			h.workerID, h.state.Retries, h.restartPolicy.MaxRetries)
		return
	}

	// Trigger restart
	h.state.Retries++
	h.logger.Warnf("Triggering restart due to health check failure, worker: %s, retry: %d, reason: %s",
		h.workerID, h.state.Retries, message)

	// Call restart callback in a goroutine to avoid blocking health check loop
	go func() {
		if err := h.restartCallback(fmt.Sprintf("Health check failure: %s", message)); err != nil {
			h.logger.Errorf("Failed to trigger restart, worker: %s, error: %v", h.workerID, err)
		}
	}()
}

func (h *healthMonitor) checkHTTP() (bool, string) {
	h.logger.Debugf("Performing HTTP health check, worker: %s, url: %s", h.workerID, h.config.HTTP.URL)

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: h.config.RunOptions.Timeout,
	}

	method := h.config.HTTP.PMethod
	if method == "" {
		method = "GET"
	}

	req, err := http.NewRequest(method, h.config.HTTP.URL, nil)
	if err != nil {
		return false, fmt.Sprintf("Failed to create HTTP request: %v", err)
	}

	// Add custom headers
	for key, value := range h.config.HTTP.Headers {
		req.Header.Set(key, value)
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Sprintf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	// Consider 2xx status codes as healthy
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, fmt.Sprintf("HTTP health check passed: %d %s", resp.StatusCode, resp.Status)
	}

	return false, fmt.Sprintf("HTTP health check failed: %d %s", resp.StatusCode, resp.Status)
}

func (h *healthMonitor) checkGRPC() (bool, string) {
	h.logger.Debugf("Performing gRPC health check, worker: %s, address: %s, service: %s",
		h.workerID, h.config.GRPC.Address, h.config.GRPC.Service)

	// For now, implement as TCP connection check
	// TODO: Implement proper gRPC health check protocol
	address := h.config.GRPC.Address

	// Parse address to get host and port
	if !strings.Contains(address, ":") {
		return false, "Invalid gRPC address format (missing port)"
	}

	conn, err := net.DialTimeout("tcp", address, h.config.RunOptions.Timeout)
	if err != nil {
		return false, fmt.Sprintf("gRPC connection failed: %v", err)
	}
	defer conn.Close()

	return true, fmt.Sprintf("gRPC connection successful to %s", address)
}

func (h *healthMonitor) checkTCP() (bool, string) {
	h.logger.Debugf("Performing TCP health check, worker: %s, address: %s, port: %d",
		h.workerID, h.config.TCP.Address, h.config.TCP.Port)

	address := fmt.Sprintf("%s:%d", h.config.TCP.Address, h.config.TCP.Port)

	conn, err := net.DialTimeout("tcp", address, h.config.RunOptions.Timeout)
	if err != nil {
		return false, fmt.Sprintf("TCP connection failed: %v", err)
	}
	defer conn.Close()

	return true, fmt.Sprintf("TCP connection successful to %s", address)
}

func (h *healthMonitor) checkExec() (bool, string) {
	h.logger.Debugf("Performing exec health check, worker: %s, command: %s, args: %v",
		h.workerID, h.config.Exec.Command, h.config.Exec.Args)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), h.config.RunOptions.Timeout)
	defer cancel()

	// Create command
	cmd := exec.CommandContext(ctx, h.config.Exec.Command, h.config.Exec.Args...)

	// Execute command
	output, err := cmd.CombinedOutput()

	if ctx.Err() == context.DeadlineExceeded {
		return false, fmt.Sprintf("Exec health check timed out after %v", h.config.RunOptions.Timeout)
	}

	if err != nil {
		return false, fmt.Sprintf("Exec health check failed: %v, output: %s", err, string(output))
	}

	return true, fmt.Sprintf("Exec health check passed, output: %s", string(output))
}

func (h *healthMonitor) checkProcess() (bool, string) {
	h.logger.Debugf("Performing process health check, worker: %s", h.workerID)

	// If we have process info, use it for more accurate checking
	if h.processInfo != nil {
		return h.checkProcessWithInfo()
	}

	// Fallback: basic existence check using discovery
	return h.checkProcessBasic()
}

func (h *healthMonitor) checkProcessWithInfo() (bool, string) {
	pid := h.processInfo.PID

	// Check if process exists using cross-platform approach
	exists, err := isProcessRunning(pid)
	if err != nil {
		return false, fmt.Sprintf("Process check failed: PID %d, error: %v", pid, err)
	}

	if !exists {
		return false, fmt.Sprintf("Process not running: PID %d", pid)
	}

	return true, fmt.Sprintf("Process is running: PID %d", pid)
}

// isProcessRunning checks if a process with the given PID is running (cross-platform)
func isProcessRunning(pid int) (bool, error) {
	if pid <= 0 {
		return false, fmt.Errorf("invalid PID: %d", pid)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		// On Unix, FindProcess always succeeds if PID > 0
		// On Windows, FindProcess fails if process doesn't exist
		return false, nil
	}

	// Use the existing platform-specific verification logic
	if err := verifyProcessRunning(process); err != nil {
		// Process doesn't exist or is not running
		return false, nil
	}

	return true, nil
}

func (h *healthMonitor) checkProcessBasic() (bool, string) {
	h.logger.Debugf("Basic process health check, worker: %s (no process info available)", h.workerID)

	// Without process info, we can't do much more than assume healthy
	// This should ideally not happen in production
	h.logger.Warnf("Process health check has no process information, worker: %s", h.workerID)
	return true, "Process health check: no process information available (assuming healthy)"
}

// SetProcessInfo allows updating process information after health monitor creation
func (h *healthMonitor) SetProcessInfo(processInfo *ProcessInfo) {
	h.processInfo = processInfo
	h.logger.Debugf("Process info updated for health monitor, worker: %s, PID: %d", h.workerID, processInfo.PID)
}

func (h *healthMonitor) SetRestartCallback(callback HealthRestartCallback) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.restartCallback = callback
	h.logger.Debugf("Restart callback set for health monitor, worker: %s", h.workerID)
}

func (h *healthMonitor) SetRecoveryCallback(callback HealthRecoveryCallback) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.recoveryCallback = callback
	h.logger.Debugf("Recovery callback set for health monitor, worker: %s", h.workerID)
}
