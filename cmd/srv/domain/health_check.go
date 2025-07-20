package domain

import (
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

type HealthMonitor interface {
	State() *HealthCheckState
	Start()
	Stop()
}

type healthMonitor struct {
	config   *HealthCheckConfig
	state    *HealthCheckState
	stopChan chan struct{}
	wg       sync.WaitGroup
	mutex    sync.Mutex
	logger   logging.Logger
	workerID string
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

		if h.state.Status != HealthCheckStatusHealthy {
			h.state.Status = HealthCheckStatusHealthy
			h.logger.Infof("Health check recovered, worker: %s, previous: %s, consecutive_successes: %d",
				h.workerID, previousStatus, h.state.ConsecutiveSuccesses)
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
	}

	h.state.Message = message
}

func (h *healthMonitor) checkHTTP() (bool, string) {
	h.logger.Debugf("Performing HTTP health check, worker: %s, url: %s", h.workerID, h.config.HTTP.URL)
	// TODO: Implement HTTP health check
	return false, "HTTP health check not implemented"
}

func (h *healthMonitor) checkGRPC() (bool, string) {
	h.logger.Debugf("Performing gRPC health check, worker: %s, address: %s, service: %s",
		h.workerID, h.config.GRPC.Address, h.config.GRPC.Service)
	// TODO: Implement gRPC health check
	return false, "gRPC health check not implemented"
}

func (h *healthMonitor) checkTCP() (bool, string) {
	h.logger.Debugf("Performing TCP health check, worker: %s, address: %s, port: %d",
		h.workerID, h.config.TCP.Address, h.config.TCP.Port)
	// TODO: Implement TCP health check
	return false, "TCP health check not implemented"
}

func (h *healthMonitor) checkExec() (bool, string) {
	h.logger.Debugf("Performing exec health check, worker: %s, command: %s, args: %v",
		h.workerID, h.config.Exec.Command, h.config.Exec.Args)
	// TODO: Implement exec health check
	return false, "Exec health check not implemented"
}

func (h *healthMonitor) checkProcess() (bool, string) {
	h.logger.Debugf("Performing process health check, worker: %s", h.workerID)
	// TODO: Implement process health check
	return false, "Process health check not implemented"
}
