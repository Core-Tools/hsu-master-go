package resourcelimits

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/core-tools/hsu-master/pkg/logging"
	"github.com/core-tools/hsu-master/pkg/processstate"
)

// resourceMonitor implements ResourceMonitor interface
type resourceMonitor struct {
	pid    int
	config *ResourceMonitoringConfig
	logger logging.Logger

	// Callbacks
	usageCallback     ResourceUsageCallback
	violationCallback ResourceViolationCallback

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mutex  sync.RWMutex

	// State
	isRunning    bool
	lastUsage    *ResourceUsage
	usageHistory []*ResourceUsage

	// Platform-specific monitor
	platformMonitor PlatformResourceMonitor
}

// PlatformResourceMonitor defines platform-specific monitoring interface
type PlatformResourceMonitor interface {
	GetProcessUsage(pid int) (*ResourceUsage, error)
	SupportsRealTimeMonitoring() bool
}

// NewResourceMonitor creates a new resource monitor for a process
func NewResourceMonitor(pid int, config *ResourceMonitoringConfig, logger logging.Logger) ResourceMonitor {
	if config == nil {
		config = &ResourceMonitoringConfig{
			Enabled:          true,
			Interval:         30 * time.Second,
			HistoryRetention: 24 * time.Hour,
			AlertingEnabled:  true,
		}
	}

	// Set defaults
	if config.Interval == 0 {
		config.Interval = 30 * time.Second
	}
	if config.HistoryRetention == 0 {
		config.HistoryRetention = 24 * time.Hour
	}

	return &resourceMonitor{
		pid:             pid,
		config:          config,
		logger:          logger,
		usageHistory:    make([]*ResourceUsage, 0),
		platformMonitor: newPlatformResourceMonitor(logger),
	}
}

// Start begins resource monitoring
func (rm *resourceMonitor) Start(ctx context.Context) error {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	if rm.isRunning {
		return fmt.Errorf("resource monitor is already running")
	}

	if !rm.config.Enabled {
		rm.logger.Infof("Resource monitoring disabled for PID %d", rm.pid)
		return nil
	}

	// Check if process exists
	if !processstate.IsProcessRunning(rm.pid) {
		return fmt.Errorf("process %d is not running", rm.pid)
	}

	rm.ctx, rm.cancel = context.WithCancel(ctx)
	rm.isRunning = true

	rm.logger.Infof("Starting resource monitoring for PID %d, interval: %v", rm.pid, rm.config.Interval)

	// Start monitoring goroutine
	rm.wg.Add(1)
	go rm.monitorLoop()

	return nil
}

// Stop stops resource monitoring
func (rm *resourceMonitor) Stop() {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	if !rm.isRunning {
		return
	}

	rm.logger.Infof("Stopping resource monitoring for PID %d", rm.pid)

	rm.cancel()
	rm.isRunning = false

	// Wait for monitoring goroutine to finish
	rm.wg.Wait()

	rm.logger.Infof("Resource monitoring stopped for PID %d", rm.pid)
}

// GetCurrentUsage returns current resource usage
func (rm *resourceMonitor) GetCurrentUsage() (*ResourceUsage, error) {
	// Check if process exists
	if !processstate.IsProcessRunning(rm.pid) {
		return nil, fmt.Errorf("process %d is not running", rm.pid)
	}

	usage, err := rm.platformMonitor.GetProcessUsage(rm.pid)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource usage for PID %d: %v", rm.pid, err)
	}

	return usage, nil
}

// SetUsageCallback sets callback for resource usage updates
func (rm *resourceMonitor) SetUsageCallback(callback ResourceUsageCallback) {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()
	rm.usageCallback = callback
}

// SetViolationCallback sets callback for limit violations
func (rm *resourceMonitor) SetViolationCallback(callback ResourceViolationCallback) {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()
	rm.violationCallback = callback
}

// monitorLoop is the main monitoring goroutine
func (rm *resourceMonitor) monitorLoop() {
	defer rm.wg.Done()

	ticker := time.NewTicker(rm.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-rm.ctx.Done():
			rm.logger.Debugf("Resource monitoring loop stopped for PID %d", rm.pid)
			return

		case <-ticker.C:
			rm.collectUsage()
		}
	}
}

// collectUsage collects current resource usage
func (rm *resourceMonitor) collectUsage() {
	// Check if process is still running
	if !processstate.IsProcessRunning(rm.pid) {
		rm.logger.Warnf("Process %d is no longer running, stopping resource monitoring", rm.pid)
		rm.Stop()
		return
	}

	usage, err := rm.platformMonitor.GetProcessUsage(rm.pid)
	if err != nil {
		rm.logger.Errorf("Failed to collect resource usage for PID %d: %v", rm.pid, err)
		return
	}

	rm.logger.Debugf("Resource usage for PID %d: Memory RSS: %dMB, CPU: %.1f%%, FDs: %d",
		rm.pid, usage.MemoryRSS/(1024*1024), usage.CPUPercent, usage.OpenFileDescriptors)

	// Store usage
	rm.mutex.Lock()
	rm.lastUsage = usage
	rm.addToHistory(usage)
	rm.mutex.Unlock()

	// Call usage callback
	if rm.usageCallback != nil {
		rm.usageCallback(usage)
	}
}

// addToHistory adds usage to history and manages retention
func (rm *resourceMonitor) addToHistory(usage *ResourceUsage) {
	rm.usageHistory = append(rm.usageHistory, usage)

	// Clean old entries based on retention policy
	cutoff := time.Now().Add(-rm.config.HistoryRetention)
	for len(rm.usageHistory) > 0 && rm.usageHistory[0].Timestamp.Before(cutoff) {
		rm.usageHistory = rm.usageHistory[1:]
	}
}

// GetUsageHistory returns historical usage data
func (rm *resourceMonitor) GetUsageHistory(since time.Time) []*ResourceUsage {
	rm.mutex.RLock()
	defer rm.mutex.RUnlock()

	var result []*ResourceUsage
	for _, usage := range rm.usageHistory {
		if usage.Timestamp.After(since) {
			result = append(result, usage)
		}
	}

	return result
}

// CheckViolations checks for resource limit violations
func (rm *resourceMonitor) CheckViolations(limits *EnhancedResourceLimits) []*ResourceViolation {
	if rm.lastUsage == nil || limits == nil {
		return nil
	}

	var violations []*ResourceViolation

	// Check memory violations
	if limits.MemoryLimits != nil {
		violations = append(violations, rm.checkMemoryViolations(limits.MemoryLimits)...)
	}

	// Check CPU violations
	if limits.CPULimits != nil {
		violations = append(violations, rm.checkCPUViolations(limits.CPULimits)...)
	}

	// Check I/O violations
	if limits.IOLimits != nil {
		violations = append(violations, rm.checkIOViolations(limits.IOLimits)...)
	}

	// Check process violations
	if limits.ProcessLimits != nil {
		violations = append(violations, rm.checkProcessViolations(limits.ProcessLimits)...)
	}

	// Call violation callback for any violations
	for _, violation := range violations {
		if rm.violationCallback != nil {
			rm.violationCallback(violation)
		}
	}

	return violations
}

// checkMemoryViolations checks for memory limit violations
func (rm *resourceMonitor) checkMemoryViolations(limits *MemoryLimits) []*ResourceViolation {
	var violations []*ResourceViolation

	usage := rm.lastUsage
	now := time.Now()

	// Check RSS limit
	if limits.MaxRSS > 0 && usage.MemoryRSS > limits.MaxRSS {
		violations = append(violations, &ResourceViolation{
			LimitType:    ResourceLimitTypeMemory,
			CurrentValue: usage.MemoryRSS,
			LimitValue:   limits.MaxRSS,
			Severity:     ViolationSeverityCritical,
			Timestamp:    now,
			Message:      fmt.Sprintf("Memory RSS (%d bytes) exceeds limit (%d bytes)", usage.MemoryRSS, limits.MaxRSS),
		})
	}

	// Check virtual memory limit
	if limits.MaxVirtual > 0 && usage.MemoryVirtual > limits.MaxVirtual {
		violations = append(violations, &ResourceViolation{
			LimitType:    ResourceLimitTypeMemory,
			CurrentValue: usage.MemoryVirtual,
			LimitValue:   limits.MaxVirtual,
			Severity:     ViolationSeverityCritical,
			Timestamp:    now,
			Message:      fmt.Sprintf("Virtual memory (%d bytes) exceeds limit (%d bytes)", usage.MemoryVirtual, limits.MaxVirtual),
		})
	}

	// Check warning threshold for RSS
	if limits.WarningThreshold > 0 && limits.MaxRSS > 0 {
		warningLimit := float64(limits.MaxRSS) * (limits.WarningThreshold / 100.0)
		if float64(usage.MemoryRSS) > warningLimit {
			violations = append(violations, &ResourceViolation{
				LimitType:    ResourceLimitTypeMemory,
				CurrentValue: usage.MemoryRSS,
				LimitValue:   int64(warningLimit),
				Severity:     ViolationSeverityWarning,
				Timestamp:    now,
				Message:      fmt.Sprintf("Memory RSS (%d bytes) exceeds warning threshold (%.0f bytes)", usage.MemoryRSS, warningLimit),
			})
		}
	}

	return violations
}

// checkCPUViolations checks for CPU limit violations
func (rm *resourceMonitor) checkCPUViolations(limits *CPULimits) []*ResourceViolation {
	var violations []*ResourceViolation

	usage := rm.lastUsage
	now := time.Now()

	// Check CPU percentage limit
	if limits.MaxPercent > 0 && usage.CPUPercent > limits.MaxPercent {
		violations = append(violations, &ResourceViolation{
			LimitType:    ResourceLimitTypeCPU,
			CurrentValue: usage.CPUPercent,
			LimitValue:   limits.MaxPercent,
			Severity:     ViolationSeverityCritical,
			Timestamp:    now,
			Message:      fmt.Sprintf("CPU usage (%.1f%%) exceeds limit (%.1f%%)", usage.CPUPercent, limits.MaxPercent),
		})
	}

	// Check CPU time limit
	if limits.MaxTime > 0 && time.Duration(usage.CPUTime)*time.Second > limits.MaxTime {
		violations = append(violations, &ResourceViolation{
			LimitType:    ResourceLimitTypeCPU,
			CurrentValue: usage.CPUTime,
			LimitValue:   limits.MaxTime.Seconds(),
			Severity:     ViolationSeverityCritical,
			Timestamp:    now,
			Message:      fmt.Sprintf("CPU time (%.1fs) exceeds limit (%v)", usage.CPUTime, limits.MaxTime),
		})
	}

	// Check warning threshold
	if limits.WarningThreshold > 0 && limits.MaxPercent > 0 {
		warningLimit := limits.MaxPercent * (limits.WarningThreshold / 100.0)
		if usage.CPUPercent > warningLimit {
			violations = append(violations, &ResourceViolation{
				LimitType:    ResourceLimitTypeCPU,
				CurrentValue: usage.CPUPercent,
				LimitValue:   warningLimit,
				Severity:     ViolationSeverityWarning,
				Timestamp:    now,
				Message:      fmt.Sprintf("CPU usage (%.1f%%) exceeds warning threshold (%.1f%%)", usage.CPUPercent, warningLimit),
			})
		}
	}

	return violations
}

// checkIOViolations checks for I/O limit violations
func (rm *resourceMonitor) checkIOViolations(limits *IOLimits) []*ResourceViolation {
	var violations []*ResourceViolation
	// TODO: Implement I/O rate checking (requires tracking rates over time)
	return violations
}

// checkProcessViolations checks for process/file descriptor violations
func (rm *resourceMonitor) checkProcessViolations(limits *ProcessLimits) []*ResourceViolation {
	var violations []*ResourceViolation

	usage := rm.lastUsage
	now := time.Now()

	// Check file descriptor limit
	if limits.MaxFileDescriptors > 0 && usage.OpenFileDescriptors > limits.MaxFileDescriptors {
		violations = append(violations, &ResourceViolation{
			LimitType:    ResourceLimitTypeProcess,
			CurrentValue: usage.OpenFileDescriptors,
			LimitValue:   limits.MaxFileDescriptors,
			Severity:     ViolationSeverityCritical,
			Timestamp:    now,
			Message:      fmt.Sprintf("Open file descriptors (%d) exceeds limit (%d)", usage.OpenFileDescriptors, limits.MaxFileDescriptors),
		})
	}

	// Check child process limit
	if limits.MaxChildProcesses > 0 && usage.ChildProcesses > limits.MaxChildProcesses {
		violations = append(violations, &ResourceViolation{
			LimitType:    ResourceLimitTypeProcess,
			CurrentValue: usage.ChildProcesses,
			LimitValue:   limits.MaxChildProcesses,
			Severity:     ViolationSeverityCritical,
			Timestamp:    now,
			Message:      fmt.Sprintf("Child processes (%d) exceeds limit (%d)", usage.ChildProcesses, limits.MaxChildProcesses),
		})
	}

	return violations
}
