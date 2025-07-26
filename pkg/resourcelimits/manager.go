package resourcelimits

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/core-tools/hsu-master/pkg/logging"
)

// ResourceLimitManager coordinates resource monitoring and enforcement
type resourceLimitManager struct {
	pid      int
	limits   *ResourceLimits
	monitor  ResourceMonitor
	enforcer ResourceEnforcer
	logger   logging.Logger

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mutex  sync.RWMutex

	// State
	isRunning  bool
	violations []*ResourceViolation

	// Configuration
	checkInterval time.Duration

	// External violation handling
	externalViolationCallback ResourceViolationCallback
}

// NewResourceLimitManager creates a new resource limit manager
func NewResourceLimitManager(pid int, limits *ResourceLimits, logger logging.Logger) ResourceLimitManager {
	if limits == nil {
		// Return manager with no limits
		return &resourceLimitManager{
			pid:           pid,
			logger:        logger,
			checkInterval: 30 * time.Second,
		}
	}

	// Create monitoring config if not provided
	if limits.Monitoring == nil {
		limits.Monitoring = &ResourceMonitoringConfig{
			Enabled:           true,
			Interval:          30 * time.Second,
			HistoryRetention:  24 * time.Hour,
			AlertingEnabled:   true,
			ViolationHandling: ViolationHandlingInternal, // Default for backward compatibility
		}
	} else {
		// Set default violation handling mode if not specified
		if limits.Monitoring.ViolationHandling == "" {
			limits.Monitoring.ViolationHandling = ViolationHandlingInternal
		}
	}

	// Set default check interval
	checkInterval := limits.Monitoring.Interval
	if checkInterval == 0 {
		checkInterval = 30 * time.Second
	}

	monitor := NewResourceMonitor(pid, limits.Monitoring, logger)
	enforcer := NewResourceEnforcer(logger)

	return &resourceLimitManager{
		pid:           pid,
		limits:        limits,
		monitor:       monitor,
		enforcer:      enforcer,
		logger:        logger,
		checkInterval: checkInterval,
		violations:    make([]*ResourceViolation, 0),
	}
}

// Start begins resource monitoring and enforcement
func (rlm *resourceLimitManager) Start(ctx context.Context) error {
	rlm.mutex.Lock()
	defer rlm.mutex.Unlock()

	if rlm.isRunning {
		return fmt.Errorf("resource limit manager is already running")
	}

	if rlm.limits == nil {
		rlm.logger.Infof("No resource limits configured for PID %d", rlm.pid)
		return nil
	}

	rlm.logger.Infof("Starting resource limit management for PID %d", rlm.pid)

	// Apply initial limits
	if err := rlm.enforcer.ApplyLimits(rlm.pid, rlm.limits); err != nil {
		rlm.logger.Warnf("Failed to apply some resource limits to PID %d: %v", rlm.pid, err)
		// Continue anyway - monitoring can still work
	}

	rlm.ctx, rlm.cancel = context.WithCancel(ctx)
	rlm.isRunning = true

	// Start monitoring if enabled
	if rlm.monitor != nil && rlm.limits.Monitoring.Enabled {
		if err := rlm.monitor.Start(rlm.ctx); err != nil {
			rlm.logger.Errorf("Failed to start resource monitoring for PID %d: %v", rlm.pid, err)
			return fmt.Errorf("failed to start resource monitoring: %v", err)
		}

		// Set up monitoring callbacks
		rlm.monitor.SetUsageCallback(rlm.onUsageUpdate)
		rlm.monitor.SetViolationCallback(rlm.onViolationDispatch)

		// Start violation checking loop
		rlm.wg.Add(1)
		go rlm.violationCheckLoop()
	}

	rlm.logger.Infof("Resource limit management started for PID %d", rlm.pid)
	return nil
}

// Stop stops resource monitoring and enforcement
func (rlm *resourceLimitManager) Stop() {
	rlm.mutex.Lock()
	defer rlm.mutex.Unlock()

	if !rlm.isRunning {
		return
	}

	rlm.logger.Infof("Stopping resource limit management for PID %d", rlm.pid)

	rlm.cancel()
	rlm.isRunning = false

	// Stop monitoring
	if rlm.monitor != nil {
		rlm.monitor.Stop()
	}

	// Wait for background goroutines
	rlm.wg.Wait()

	rlm.logger.Infof("Resource limit management stopped for PID %d", rlm.pid)
}

// GetCurrentUsage returns current resource usage
func (rlm *resourceLimitManager) GetCurrentUsage() (*ResourceUsage, error) {
	if rlm.monitor == nil {
		return nil, fmt.Errorf("resource monitoring not available")
	}

	return rlm.monitor.GetCurrentUsage()
}

// GetViolations returns recent resource violations
func (rlm *resourceLimitManager) GetViolations() []*ResourceViolation {
	rlm.mutex.RLock()
	defer rlm.mutex.RUnlock()

	// Return a copy to prevent external modification
	violations := make([]*ResourceViolation, len(rlm.violations))
	copy(violations, rlm.violations)
	return violations
}

// GetLimits returns the current resource limits
func (rlm *resourceLimitManager) GetLimits() *ResourceLimits {
	return rlm.limits
}

// IsMonitoringEnabled returns true if resource monitoring is enabled
func (rlm *resourceLimitManager) IsMonitoringEnabled() bool {
	return rlm.limits != nil && rlm.limits.Monitoring != nil && rlm.limits.Monitoring.Enabled
}

// SetViolationCallback sets an external callback for handling resource violations
// This is only effective when ViolationHandling mode is set to External
func (rlm *resourceLimitManager) SetViolationCallback(callback ResourceViolationCallback) {
	rlm.mutex.Lock()
	defer rlm.mutex.Unlock()
	rlm.externalViolationCallback = callback

	// Also set the callback on the monitor for immediate violation detection
	if rlm.monitor != nil {
		rlm.monitor.SetViolationCallback(rlm.onViolationDispatch)
	}
}

// GetViolationHandlingMode returns the current violation handling mode
func (rlm *resourceLimitManager) GetViolationHandlingMode() ViolationHandlingMode {
	if rlm.limits == nil || rlm.limits.Monitoring == nil {
		return ViolationHandlingInternal // Default
	}

	mode := rlm.limits.Monitoring.ViolationHandling
	if mode == "" {
		return ViolationHandlingInternal // Default for backward compatibility
	}

	return mode
}

// onUsageUpdate handles resource usage updates
func (rlm *resourceLimitManager) onUsageUpdate(usage *ResourceUsage) {
	rlm.logger.Debugf("Resource usage update for PID %d: Memory RSS: %dMB, CPU: %.1f%%",
		rlm.pid, usage.MemoryRSS/(1024*1024), usage.CPUPercent)

	// Could add custom usage processing here
	// For example, trend analysis, alerting thresholds, etc.
}

// onViolationDispatch routes violations based on the violation handling mode
func (rlm *resourceLimitManager) onViolationDispatch(violation *ResourceViolation) {
	mode := rlm.GetViolationHandlingMode()

	switch mode {
	case ViolationHandlingExternal:
		// Delegate to external callback (e.g., ProcessControl)
		rlm.mutex.RLock()
		callback := rlm.externalViolationCallback
		rlm.mutex.RUnlock()

		if callback != nil {
			rlm.logger.Debugf("Delegating violation to external handler for PID %d: %s", rlm.pid, violation.Message)
			callback(violation)
		} else {
			rlm.logger.Warnf("External violation handling mode enabled but no callback set for PID %d", rlm.pid)
		}

	case ViolationHandlingInternal:
		// Use internal violation handling (existing behavior)
		rlm.onViolation(violation)

	case ViolationHandlingDisabled:
		// Only log the violation, no enforcement
		rlm.logger.Infof("Resource violation detected (handling disabled) for PID %d: %s", rlm.pid, violation.Message)

	default:
		rlm.logger.Warnf("Unknown violation handling mode: %s, falling back to internal handling", mode)
		rlm.onViolation(violation)
	}
}

// onViolation handles resource limit violations (internal mode)
func (rlm *resourceLimitManager) onViolation(violation *ResourceViolation) {
	rlm.mutex.Lock()
	rlm.violations = append(rlm.violations, violation)

	// Keep only recent violations (last 100)
	if len(rlm.violations) > 100 {
		rlm.violations = rlm.violations[len(rlm.violations)-100:]
	}
	rlm.mutex.Unlock()

	rlm.logger.Warnf("Resource violation for PID %d: %s", rlm.pid, violation.Message)

	// Trigger enforcement
	if violation.Severity == ViolationSeverityCritical {
		go rlm.handleCriticalViolation(violation)
	}
}

// handleCriticalViolation handles critical resource violations
func (rlm *resourceLimitManager) handleCriticalViolation(violation *ResourceViolation) {
	rlm.logger.Errorf("Handling critical resource violation for PID %d: %s", rlm.pid, violation.Message)

	// Execute enforcement policy
	if err := rlm.enforcer.EnforcePolicy(rlm.pid, violation); err != nil {
		rlm.logger.Errorf("Failed to enforce resource policy for PID %d: %v", rlm.pid, err)
	}
}

// violationCheckLoop periodically checks for resource violations
func (rlm *resourceLimitManager) violationCheckLoop() {
	defer rlm.wg.Done()

	// Use a shorter interval for violation checking than general monitoring
	checkInterval := rlm.checkInterval
	if checkInterval > 10*time.Second {
		checkInterval = 10 * time.Second
	}

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rlm.ctx.Done():
			rlm.logger.Debugf("Resource violation check loop stopped for PID %d", rlm.pid)
			return

		case <-ticker.C:
			rlm.checkViolations()
		}
	}
}

// checkViolations checks for resource limit violations
func (rlm *resourceLimitManager) checkViolations() {
	if rlm.monitor == nil || rlm.limits == nil {
		return
	}

	// Get current usage
	_, err := rlm.monitor.GetCurrentUsage()
	if err != nil {
		rlm.logger.Debugf("Failed to get current usage for violation check: %v", err)
		return
	}

	// Check violations using the monitor's violation checking
	// This is a bit of a circular call, but it allows us to centralize the violation logic
	if violationChecker, ok := rlm.monitor.(*resourceMonitor); ok {
		violations := violationChecker.CheckViolations(rlm.limits)

		// Process any new violations
		for _, violation := range violations {
			rlm.onViolation(violation)
		}
	}
}

// GetSupportedLimitTypes returns the resource limit types supported on this platform
func (rlm *resourceLimitManager) GetSupportedLimitTypes() []ResourceLimitType {
	var supportedTypes []ResourceLimitType

	allTypes := []ResourceLimitType{
		ResourceLimitTypeMemory,
		ResourceLimitTypeCPU,
		ResourceLimitTypeIO,
		ResourceLimitTypeNetwork,
		ResourceLimitTypeProcess,
	}

	for _, limitType := range allTypes {
		if rlm.enforcer.SupportsLimitType(limitType) {
			supportedTypes = append(supportedTypes, limitType)
		}
	}

	return supportedTypes
}
