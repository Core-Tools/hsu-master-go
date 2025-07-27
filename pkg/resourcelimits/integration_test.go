package resourcelimits

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
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

func TestResourceLimitManagerViolationDispatch(t *testing.T) {
	logger := &MockResourceLimitLogger{}
	logger.On("Infof", mock.Anything, mock.Anything).Maybe()
	logger.On("Debugf", mock.Anything, mock.Anything).Maybe()
	logger.On("Warnf", mock.Anything, mock.Anything).Maybe()
	logger.On("Errorf", mock.Anything, mock.Anything).Maybe()

	// Test External mode with callback
	// Create limits with external violation handling
	limits := &ResourceLimits{
		Memory: &MemoryLimits{
			MaxRSS: 100 * 1024 * 1024, // 100MB
			Policy: ResourcePolicyRestart,
		},
		Monitoring: &ResourceMonitoringConfig{
			Enabled:  true,
			Interval: time.Second,
		},
	}

	manager := NewResourceLimitManager(12345, limits, logger)

	// Set up callback
	var callbackInvoked bool
	var receivedPolicy ResourcePolicy
	var receivedViolation *ResourceViolation
	var mu sync.Mutex

	manager.SetViolationCallback(func(policy ResourcePolicy, violation *ResourceViolation) {
		mu.Lock()
		defer mu.Unlock()
		callbackInvoked = true
		receivedPolicy = policy
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
	manager.(*resourceLimitManager).dispatchViolation(testViolation)

	// Wait a bit for async processing
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	require.True(t, callbackInvoked, "External callback was not invoked")
	assert.Equal(t, ResourcePolicyRestart, receivedPolicy)
	require.NotNil(t, receivedViolation)
	assert.Equal(t, "Test violation", receivedViolation.Message)
	mu.Unlock()
}

func TestProcessControlIntegrationPattern(t *testing.T) {
	logger := &MockResourceLimitLogger{}
	logger.On("Infof", mock.Anything, mock.Anything).Maybe()
	logger.On("Debugf", mock.Anything, mock.Anything).Maybe()
	logger.On("Warnf", mock.Anything, mock.Anything).Maybe()
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

	manager := NewResourceLimitManager(12345, limits, logger)

	// Simulate ProcessControl violation handling
	var receivedPolicies []ResourcePolicy
	var receivedViolations []*ResourceViolation
	var mu sync.Mutex

	processControlViolationHandler := func(policy ResourcePolicy, violation *ResourceViolation) {
		mu.Lock()
		defer mu.Unlock()
		receivedPolicies = append(receivedPolicies, policy)
		receivedViolations = append(receivedViolations, violation)

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
		manager.(*resourceLimitManager).dispatchViolation(violation)
	}

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Verify all violations were handled
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 2, len(receivedPolicies), "Expected 2 policies handled, got %d", len(receivedPolicies))
	assert.Equal(t, ResourcePolicyRestart, receivedPolicies[0])
	assert.Equal(t, ResourcePolicyGracefulShutdown, receivedPolicies[1])

	require.Equal(t, 2, len(receivedViolations), "Expected 2 violations handled, got %d", len(receivedViolations))
	assert.Equal(t, "Memory limit exceeded", receivedViolations[0].Message)
	assert.Equal(t, "CPU limit exceeded", receivedViolations[1].Message)
}
