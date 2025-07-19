package domain

import (
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockLogger for testing
type MockUnmanagedLogger struct {
	mock.Mock
}

func (m *MockUnmanagedLogger) LogLevelf(level int, format string, args ...interface{}) {
	m.Called(format, args)
}

func (m *MockUnmanagedLogger) Debugf(format string, args ...interface{}) {
	m.Called(format, args)
}

func (m *MockUnmanagedLogger) Infof(format string, args ...interface{}) {
	m.Called(format, args)
}

func (m *MockUnmanagedLogger) Warnf(format string, args ...interface{}) {
	m.Called(format, args)
}

func (m *MockUnmanagedLogger) Errorf(format string, args ...interface{}) {
	m.Called(format, args)
}

func createTestUnmanagedUnit() *UnmanagedUnit {
	// Use OS-dependent path for PID file
	var pidFile string
	if runtime.GOOS == "windows" {
		pidFile = "C:\\Temp\\test-process.pid"
	} else {
		pidFile = "/tmp/test-process.pid"
	}

	return &UnmanagedUnit{
		Metadata: UnitMetadata{
			Name:        "test-unmanaged-unit",
			Description: "Test unmanaged unit",
		},
		Discovery: DiscoveryConfig{
			Method:        DiscoveryMethodPIDFile,
			PIDFile:       pidFile,
			CheckInterval: 15 * time.Second,
		},
		Control: SystemProcessControlConfig{
			CanTerminate:    true,
			CanRestart:      false,
			ServiceManager:  "systemd",
			ServiceName:     "test-service",
			AllowedSignals:  []os.Signal{os.Interrupt, os.Kill},
			GracefulTimeout: 10 * time.Second,
		},
		HealthCheck: HealthCheckConfig{
			Type: HealthCheckTypeProcess,
			RunOptions: HealthCheckRunOptions{
				Enabled:      true,
				Interval:     30 * time.Second,
				Timeout:      5 * time.Second,
				InitialDelay: 10 * time.Second,
				Retries:      3,
			},
		},
	}
}

func createTestUnmanagedUnitWithPortDiscovery() *UnmanagedUnit {
	return &UnmanagedUnit{
		Metadata: UnitMetadata{
			Name:        "test-unmanaged-port-unit",
			Description: "Test unmanaged unit with port discovery",
		},
		Discovery: DiscoveryConfig{
			Method:        DiscoveryMethodPort,
			Port:          8080,
			Protocol:      "tcp",
			CheckInterval: 20 * time.Second,
		},
		Control: SystemProcessControlConfig{
			CanTerminate:    false,
			CanRestart:      false,
			ServiceManager:  "",
			ServiceName:     "",
			AllowedSignals:  []os.Signal{},
			GracefulTimeout: 5 * time.Second,
		},
		HealthCheck: HealthCheckConfig{
			Type: HealthCheckTypeGRPC,
			GRPC: GRPCHealthCheckConfig{
				Address: "localhost:50051",
				Service: "CoreService",
				Method:  "Ping",
			},
			RunOptions: HealthCheckRunOptions{
				Enabled:      true,
				Interval:     15 * time.Second,
				Timeout:      3 * time.Second,
				InitialDelay: 5 * time.Second,
				Retries:      2,
			},
		},
	}
}

func TestNewUnmanagedWorker(t *testing.T) {
	logger := &MockUnmanagedLogger{}
	unit := createTestUnmanagedUnit()

	worker := NewUnmanagedWorker("test-unmanaged-1", unit, logger)

	assert.NotNil(t, worker)
	assert.Equal(t, "test-unmanaged-1", worker.ID())
}

func TestUnmanagedWorker_ID(t *testing.T) {
	logger := &MockUnmanagedLogger{}
	unit := createTestUnmanagedUnit()

	worker := NewUnmanagedWorker("test-unmanaged-2", unit, logger)

	assert.Equal(t, "test-unmanaged-2", worker.ID())
}

func TestUnmanagedWorker_Metadata(t *testing.T) {
	logger := &MockUnmanagedLogger{}
	unit := createTestUnmanagedUnit()

	worker := NewUnmanagedWorker("test-unmanaged-3", unit, logger)

	metadata := worker.Metadata()
	assert.Equal(t, "test-unmanaged-unit", metadata.Name)
	assert.Equal(t, "Test unmanaged unit", metadata.Description)
}

func TestUnmanagedWorker_ProcessControlOptions_PIDFile(t *testing.T) {
	logger := &MockUnmanagedLogger{}
	logger.On("Debugf", mock.Anything, mock.Anything).Maybe()
	unit := createTestUnmanagedUnit()

	worker := NewUnmanagedWorker("test-unmanaged-4", unit, logger)

	options := worker.ProcessControlOptions()

	// Test basic capabilities
	assert.True(t, options.CanAttach, "UnmanagedWorker must support attachment")
	assert.True(t, options.CanTerminate, "UnmanagedWorker should support termination based on config")
	assert.False(t, options.CanRestart, "UnmanagedWorker should not support restart based on config")

	// Test discovery configuration matches unit
	assert.Equal(t, DiscoveryMethodPIDFile, options.Discovery.Method)
	assert.Equal(t, unit.Discovery.PIDFile, options.Discovery.PIDFile)
	assert.Equal(t, 15*time.Second, options.Discovery.CheckInterval)

	// Test ExecuteCmd is not present
	assert.Nil(t, options.ExecuteCmd, "UnmanagedWorker should not provide ExecuteCmd")

	// Test restart configuration is not present
	assert.Nil(t, options.Restart, "UnmanagedWorker should not provide restart configuration")

	// Test resource limits are not present
	assert.Nil(t, options.Limits, "UnmanagedWorker should not provide resource limits")

	// Test graceful timeout from system config
	assert.Equal(t, 10*time.Second, options.GracefulTimeout)

	// Test health check is provided by AttachCmd
	assert.Nil(t, options.HealthCheck, "UnmanagedWorker should provide health check via AttachCmd")
	assert.NotNil(t, options.AttachCmd, "UnmanagedWorker should provide AttachCmd")

	// Test that AttachCmd would return the correct health check configuration
	// Note: This is a unit test, so we can't actually test attachment without a real process
	// In a real scenario, AttachCmd would be called by ProcessControl

	// Test allowed signals
	require.NotNil(t, options.AllowedSignals)
	assert.Len(t, options.AllowedSignals, 2)
	assert.Contains(t, options.AllowedSignals, os.Interrupt)
	assert.Contains(t, options.AllowedSignals, os.Kill)
}

func TestUnmanagedWorker_ProcessControlOptions_Port(t *testing.T) {
	logger := &MockUnmanagedLogger{}
	logger.On("Debugf", mock.Anything, mock.Anything).Maybe()
	unit := createTestUnmanagedUnitWithPortDiscovery()

	worker := NewUnmanagedWorker("test-unmanaged-5", unit, logger)

	options := worker.ProcessControlOptions()

	// Test basic capabilities
	assert.True(t, options.CanAttach, "UnmanagedWorker must support attachment")
	assert.False(t, options.CanTerminate, "UnmanagedWorker should not support termination based on config")
	assert.False(t, options.CanRestart, "UnmanagedWorker should not support restart based on config")

	// Test discovery configuration matches unit
	assert.Equal(t, DiscoveryMethodPort, options.Discovery.Method)
	assert.Equal(t, 8080, options.Discovery.Port)
	assert.Equal(t, "tcp", options.Discovery.Protocol)
	assert.Equal(t, 20*time.Second, options.Discovery.CheckInterval)

	// Test ExecuteCmd is not present
	assert.Nil(t, options.ExecuteCmd, "UnmanagedWorker should not provide ExecuteCmd")

	// Test graceful timeout from system config
	assert.Equal(t, 5*time.Second, options.GracefulTimeout)

	// Test health check is provided by AttachCmd
	assert.Nil(t, options.HealthCheck, "UnmanagedWorker should provide health check via AttachCmd")
	assert.NotNil(t, options.AttachCmd, "UnmanagedWorker should provide AttachCmd")

	// Test that AttachCmd would return the correct health check configuration
	// Note: This is a unit test, so we can't actually test attachment without a real process
	// In a real scenario, AttachCmd would be called by ProcessControl

	// Test allowed signals (empty in this case)
	assert.Len(t, options.AllowedSignals, 0)
}

func TestUnmanagedWorker_IntegrationWithProcessControlOptions(t *testing.T) {
	logger := &MockUnmanagedLogger{}
	logger.On("Debugf", mock.Anything, mock.Anything).Maybe()
	unit := createTestUnmanagedUnit()

	worker := NewUnmanagedWorker("test-unmanaged-6", unit, logger)

	options := worker.ProcessControlOptions()

	// Test that options pass validation
	err := ValidateProcessControlOptions(options)
	assert.NoError(t, err, "UnmanagedWorker options should pass validation")
}

func TestUnmanagedWorker_IntegrationWithProcessControlOptions_Port(t *testing.T) {
	logger := &MockUnmanagedLogger{}
	logger.On("Debugf", mock.Anything, mock.Anything).Maybe()
	unit := createTestUnmanagedUnitWithPortDiscovery()

	worker := NewUnmanagedWorker("test-unmanaged-7", unit, logger)

	options := worker.ProcessControlOptions()

	// Test that options pass validation
	err := ValidateProcessControlOptions(options)
	assert.NoError(t, err, "UnmanagedWorker options with port discovery should pass validation")
}

func TestUnmanagedWorker_MultipleInstances(t *testing.T) {
	logger1 := &MockUnmanagedLogger{}
	logger1.On("Debugf", mock.Anything, mock.Anything).Maybe()
	logger2 := &MockUnmanagedLogger{}
	logger2.On("Debugf", mock.Anything, mock.Anything).Maybe()

	unit1 := createTestUnmanagedUnit()
	unit2 := createTestUnmanagedUnitWithPortDiscovery()

	worker1 := NewUnmanagedWorker("test-unmanaged-7", unit1, logger1)
	worker2 := NewUnmanagedWorker("test-unmanaged-8", unit2, logger2)

	// Test independence
	assert.NotEqual(t, worker1.ID(), worker2.ID())
	assert.NotEqual(t, worker1.Metadata(), worker2.Metadata()) // Different units, different metadata

	// Test different discovery configurations (since they use different units)
	options1 := worker1.ProcessControlOptions()
	options2 := worker2.ProcessControlOptions()

	assert.NotEqual(t, options1.Discovery, options2.Discovery)       // Different discovery methods
	assert.NotEqual(t, options1.CanTerminate, options2.CanTerminate) // Different capabilities too
	assert.Equal(t, options1.CanRestart, options2.CanRestart)        // Both false for CanRestart
}

func TestUnmanagedWorker_DifferentCapabilities(t *testing.T) {
	logger := &MockUnmanagedLogger{}
	logger.On("Debugf", mock.Anything, mock.Anything).Maybe()

	// Create unit with different capabilities
	unit1 := createTestUnmanagedUnit()
	unit1.Control.CanTerminate = true
	unit1.Control.CanRestart = true

	unit2 := createTestUnmanagedUnit()
	unit2.Control.CanTerminate = false
	unit2.Control.CanRestart = false

	worker1 := NewUnmanagedWorker("worker-1", unit1, logger)
	worker2 := NewUnmanagedWorker("worker-2", unit2, logger)

	options1 := worker1.ProcessControlOptions()
	options2 := worker2.ProcessControlOptions()

	// Test different capabilities based on system config
	assert.True(t, options1.CanTerminate)
	assert.True(t, options1.CanRestart)

	assert.False(t, options2.CanTerminate)
	assert.False(t, options2.CanRestart)
}
