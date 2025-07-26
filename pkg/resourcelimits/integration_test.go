package resourcelimits

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
)

// MockLogger for testing
type MockResourceLimitLogger struct {
	mock.Mock
}

func (m *MockResourceLimitLogger) LogLevelf(level int, format string, args ...interface{}) {
	m.Called(format, args)
}

func (m *MockResourceLimitLogger) Debugf(format string, args ...interface{}) {
	m.Called(format, args)
}

func (m *MockResourceLimitLogger) Infof(format string, args ...interface{}) {
	m.Called(format, args)
}

func (m *MockResourceLimitLogger) Warnf(format string, args ...interface{}) {
	m.Called(format, args)
}

func (m *MockResourceLimitLogger) Errorf(format string, args ...interface{}) {
	m.Called(format, args)
}

func TestResourceLimitManagerViolationHandlingModes(t *testing.T) {
	logger := &MockResourceLimitLogger{}
	logger.On("Infof", mock.Anything, mock.Anything).Maybe()
	logger.On("Debugf", mock.Anything, mock.Anything).Maybe()
	logger.On("Errorf", mock.Anything, mock.Anything).Maybe()

	// Test External mode with callback
	t.Run("ExternalMode", func(t *testing.T) {
		// Create limits with external violation handling
		limits := &ResourceLimits{
			Memory: &MemoryLimits{
				MaxRSS: 100 * 1024 * 1024, // 100MB
				Policy: ResourcePolicyRestart,
			},
			Monitoring: &ResourceMonitoringConfig{
				Enabled:           true,
				Interval:          time.Second,
				ViolationHandling: ViolationHandlingExternal,
			},
		}

		manager := NewResourceLimitManager(12345, limits, logger)

		// Verify mode is external
		if mode := manager.GetViolationHandlingMode(); mode != ViolationHandlingExternal {
			t.Errorf("Expected external mode, got %s", mode)
		}

		// Set up external callback
		var callbackInvoked bool
		var receivedViolation *ResourceViolation
		var mu sync.Mutex

		manager.SetViolationCallback(func(violation *ResourceViolation) {
			mu.Lock()
			defer mu.Unlock()
			callbackInvoked = true
			receivedViolation = violation
		})

		// Test that callback gets called
		testViolation := &ResourceViolation{
			LimitType:    ResourceLimitTypeMemory,
			CurrentValue: 200 * 1024 * 1024,
			LimitValue:   100 * 1024 * 1024,
			Severity:     ViolationSeverityCritical,
			Timestamp:    time.Now(),
			Message:      "Test violation",
		}

		// Simulate violation dispatch
		manager.(*resourceLimitManager).onViolationDispatch(testViolation)

		// Wait a bit for async processing
		time.Sleep(100 * time.Millisecond)

		mu.Lock()
		if !callbackInvoked {
			t.Error("External callback was not invoked")
		}
		if receivedViolation == nil {
			t.Error("No violation received by callback")
		} else if receivedViolation.Message != "Test violation" {
			t.Errorf("Wrong violation received: %s", receivedViolation.Message)
		}
		mu.Unlock()
	})

	// Test Internal mode (default behavior)
	t.Run("InternalMode", func(t *testing.T) {
		limits := &ResourceLimits{
			Memory: &MemoryLimits{
				MaxRSS: 100 * 1024 * 1024,
				Policy: ResourcePolicyLog,
			},
			// No ViolationHandling specified - should default to Internal
		}

		manager := NewResourceLimitManager(12345, limits, logger)

		// Verify mode is internal (default)
		if mode := manager.GetViolationHandlingMode(); mode != ViolationHandlingInternal {
			t.Errorf("Expected internal mode, got %s", mode)
		}
	})

	// Test Disabled mode
	t.Run("DisabledMode", func(t *testing.T) {
		limits := &ResourceLimits{
			Memory: &MemoryLimits{
				MaxRSS: 100 * 1024 * 1024,
				Policy: ResourcePolicyRestart,
			},
			Monitoring: &ResourceMonitoringConfig{
				Enabled:           true,
				ViolationHandling: ViolationHandlingDisabled,
			},
		}

		manager := NewResourceLimitManager(12345, limits, logger)

		// Verify mode is disabled
		if mode := manager.GetViolationHandlingMode(); mode != ViolationHandlingDisabled {
			t.Errorf("Expected disabled mode, got %s", mode)
		}

		// Test that violations are only logged, not enforced
		testViolation := &ResourceViolation{
			LimitType: ResourceLimitTypeMemory,
			Severity:  ViolationSeverityCritical,
			Message:   "Test disabled violation",
		}

		// This should only log, not trigger enforcement
		manager.(*resourceLimitManager).onViolationDispatch(testViolation)
	})
}

func TestProcessControlIntegrationPattern(t *testing.T) {
	logger := &MockResourceLimitLogger{}
	logger.On("Infof", mock.Anything, mock.Anything).Maybe()
	logger.On("Debugf", mock.Anything, mock.Anything).Maybe()
	logger.On("Errorf", mock.Anything, mock.Anything).Maybe()

	// Simulate ProcessControl configuration
	limits := &ResourceLimits{
		Memory: &MemoryLimits{
			MaxRSS: 256 * 1024 * 1024, // 256MB
			Policy: ResourcePolicyRestart,
		},
		CPU: &CPULimits{
			MaxPercent: 75.0,
			Policy:     ResourcePolicyGracefulShutdown,
		},
		Process: &ProcessLimits{
			MaxFileDescriptors: 1024,
			Policy:             ResourcePolicyAlert,
		},
	}

	// Configure for external handling (like ProcessControl would)
	if limits.Monitoring == nil {
		limits.Monitoring = &ResourceMonitoringConfig{}
	}
	limits.Monitoring.ViolationHandling = ViolationHandlingExternal

	manager := NewResourceLimitManager(12345, limits, logger)

	// Simulate ProcessControl violation handling
	var violations []*ResourceViolation
	var mu sync.Mutex

	processControlViolationHandler := func(violation *ResourceViolation) {
		mu.Lock()
		defer mu.Unlock()
		violations = append(violations, violation)

		// Simulate ProcessControl policy handling
		switch violation.LimitType {
		case ResourceLimitTypeMemory:
			if limits.Memory.Policy == ResourcePolicyRestart {
				logger.Infof("ProcessControl: Triggering restart for memory violation")
				// Would call pc.restartInternal(ctx) here
			}
		case ResourceLimitTypeCPU:
			if limits.CPU.Policy == ResourcePolicyGracefulShutdown {
				logger.Infof("ProcessControl: Triggering graceful shutdown for CPU violation")
				// Would call pc.stopInternal(ctx, false) here
			}
		case ResourceLimitTypeProcess:
			if limits.Process.Policy == ResourcePolicyAlert {
				logger.Infof("ProcessControl: Alerting for process violation")
				// Would log alert here
			}
		}
	}

	manager.SetViolationCallback(processControlViolationHandler)

	// Test different violation types
	testViolations := []*ResourceViolation{
		{
			LimitType: ResourceLimitTypeMemory,
			Severity:  ViolationSeverityCritical,
			Message:   "Memory limit exceeded",
		},
		{
			LimitType: ResourceLimitTypeCPU,
			Severity:  ViolationSeverityCritical,
			Message:   "CPU limit exceeded",
		},
		{
			LimitType: ResourceLimitTypeProcess,
			Severity:  ViolationSeverityWarning,
			Message:   "File descriptor limit exceeded",
		},
	}

	// Dispatch violations
	for _, violation := range testViolations {
		manager.(*resourceLimitManager).onViolationDispatch(violation)
	}

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Verify all violations were handled
	mu.Lock()
	defer mu.Unlock()
	if len(violations) != 3 {
		t.Errorf("Expected 3 violations handled, got %d", len(violations))
	}
}
