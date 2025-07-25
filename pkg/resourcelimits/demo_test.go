package resourcelimits

import (
	"os"
	"testing"
	"time"
)

// MockLogger for testing
type MockLogger struct{}

func (m *MockLogger) Infof(format string, args ...interface{})                {}
func (m *MockLogger) Warnf(format string, args ...interface{})                {}
func (m *MockLogger) Errorf(format string, args ...interface{})               {}
func (m *MockLogger) Debugf(format string, args ...interface{})               {}
func (m *MockLogger) LogLevelf(level int, format string, args ...interface{}) {}

func TestResourceLimitManager(t *testing.T) {
	// Create sample resource limits
	limits := &ResourceLimits{
		Memory: &MemoryLimits{
			MaxRSS:           512 * 1024 * 1024, // 512MB
			WarningThreshold: 80.0,
			Policy:           ResourcePolicyLog,
			CheckInterval:    5 * time.Second,
		},
		CPU: &CPULimits{
			MaxPercent:       50.0,
			WarningThreshold: 80.0,
			Policy:           ResourcePolicyThrottle,
			CheckInterval:    5 * time.Second,
		},
		Process: &ProcessLimits{
			MaxFileDescriptors: 100,
			MaxChildProcesses:  5,
			WarningThreshold:   90.0,
			Policy:             ResourcePolicyAlert,
			CheckInterval:      10 * time.Second,
		},
		Monitoring: &ResourceMonitoringConfig{
			Enabled:         true,
			Interval:        2 * time.Second,
			AlertingEnabled: true,
		},
	}

	logger := &MockLogger{}
	manager := NewResourceLimitManager(os.Getpid(), limits, logger)
	if !manager.IsMonitoringEnabled() {
		t.Error("Expected monitoring to be enabled with limits")
	}

	// Test getting limits
	retrievedLimits := manager.GetLimits()
	if retrievedLimits == nil {
		t.Error("Expected to retrieve limits")
	}

	if retrievedLimits.Memory.MaxRSS != 512*1024*1024 {
		t.Errorf("Expected MaxRSS to be 512MB, got %d", retrievedLimits.Memory.MaxRSS)
	}
}

func TestResourceMonitorCreation(t *testing.T) {
	logger := &MockLogger{}

	config := &ResourceMonitoringConfig{
		Enabled:          true,
		Interval:         30 * time.Second,
		HistoryRetention: 24 * time.Hour,
		AlertingEnabled:  true,
	}

	monitor := NewResourceMonitor(os.Getpid(), config, logger)
	if monitor == nil {
		t.Error("Expected to create resource monitor")
	}

	// Test getting current usage (should work even without starting)
	usage, err := monitor.GetCurrentUsage()
	if err != nil {
		t.Logf("Expected error getting usage without starting: %v", err)
	} else if usage != nil {
		t.Logf("Successfully got resource usage: Memory RSS: %dMB, CPU: %.1f%%",
			usage.MemoryRSS/(1024*1024), usage.CPUPercent)
	}
}

func TestResourceEnforcerCreation(t *testing.T) {
	logger := &MockLogger{}

	enforcer := NewResourceEnforcer(logger)
	if enforcer == nil {
		t.Error("Expected to create resource enforcer")
	}

	// Test supported limit types
	supportedTypes := []ResourceLimitType{
		ResourceLimitTypeMemory,
		ResourceLimitTypeCPU,
		ResourceLimitTypeIO,
		ResourceLimitTypeNetwork,
		ResourceLimitTypeProcess,
	}

	for _, limitType := range supportedTypes {
		supported := enforcer.SupportsLimitType(limitType)
		t.Logf("Limit type %s supported: %v", limitType, supported)
	}
}

func TestResourceViolationCreation(t *testing.T) {
	violation := &ResourceViolation{
		LimitType:    ResourceLimitTypeMemory,
		CurrentValue: int64(512 * 1024 * 1024), // 512MB
		LimitValue:   int64(256 * 1024 * 1024), // 256MB
		Severity:     ViolationSeverityCritical,
		Timestamp:    time.Now(),
		Message:      "Memory RSS exceeds limit",
	}

	if violation.LimitType != ResourceLimitTypeMemory {
		t.Error("Expected memory limit type")
	}

	if violation.Severity != ViolationSeverityCritical {
		t.Error("Expected critical severity")
	}

	t.Logf("Created resource violation: %s", violation.Message)
}

func TestResourceUsageStructure(t *testing.T) {
	usage := &ResourceUsage{
		Timestamp:            time.Now(),
		MemoryRSS:            128 * 1024 * 1024, // 128MB
		MemoryVirtual:        256 * 1024 * 1024, // 256MB
		MemoryPercent:        25.0,
		CPUPercent:           15.5,
		CPUTime:              120.5,
		IOReadBytes:          1024 * 1024, // 1MB
		IOWriteBytes:         512 * 1024,  // 512KB
		IOReadOps:            100,
		IOWriteOps:           50,
		OpenFileDescriptors:  25,
		ChildProcesses:       2,
		NetworkBytesReceived: 2048,
		NetworkBytesSent:     1024,
	}

	if usage.MemoryRSS != 128*1024*1024 {
		t.Error("Expected memory RSS to be 128MB")
	}

	if usage.CPUPercent != 15.5 {
		t.Error("Expected CPU percent to be 15.5")
	}

	t.Logf("Resource usage structure validated: Memory: %dMB, CPU: %.1f%%, FDs: %d",
		usage.MemoryRSS/(1024*1024), usage.CPUPercent, usage.OpenFileDescriptors)
}
