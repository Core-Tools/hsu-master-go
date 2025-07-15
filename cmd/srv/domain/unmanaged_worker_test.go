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
				Interval:         60 * time.Second,
				Timeout:          10 * time.Second,
				Retries:          2,
				SuccessThreshold: 1,
				FailureThreshold: 2,
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
			Type: HealthCheckTypeTCP,
			TCP: TCPHealthCheckConfig{
				Address: "localhost",
				Port:    8080,
			},
			RunOptions: HealthCheckRunOptions{
				Interval:         30 * time.Second,
				Timeout:          5 * time.Second,
				Retries:          1,
				SuccessThreshold: 1,
				FailureThreshold: 1,
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

	// Test health check
	require.NotNil(t, options.HealthCheck)
	assert.Equal(t, HealthCheckTypeProcess, options.HealthCheck.Type)
	assert.Equal(t, 60*time.Second, options.HealthCheck.RunOptions.Interval)
	assert.Equal(t, 10*time.Second, options.HealthCheck.RunOptions.Timeout)
	assert.Equal(t, 2, options.HealthCheck.RunOptions.Retries)
	assert.Equal(t, 1, options.HealthCheck.RunOptions.SuccessThreshold)
	assert.Equal(t, 2, options.HealthCheck.RunOptions.FailureThreshold)

	// Test allowed signals
	require.NotNil(t, options.AllowedSignals)
	assert.Len(t, options.AllowedSignals, 2)
	assert.Contains(t, options.AllowedSignals, os.Interrupt)
	assert.Contains(t, options.AllowedSignals, os.Kill)
}

func TestUnmanagedWorker_ProcessControlOptions_Port(t *testing.T) {
	logger := &MockUnmanagedLogger{}
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

	// Test health check
	require.NotNil(t, options.HealthCheck)
	assert.Equal(t, HealthCheckTypeTCP, options.HealthCheck.Type)
	assert.Equal(t, "localhost", options.HealthCheck.TCP.Address)
	assert.Equal(t, 8080, options.HealthCheck.TCP.Port)
	assert.Equal(t, 30*time.Second, options.HealthCheck.RunOptions.Interval)
	assert.Equal(t, 5*time.Second, options.HealthCheck.RunOptions.Timeout)
	assert.Equal(t, 1, options.HealthCheck.RunOptions.Retries)
	assert.Equal(t, 1, options.HealthCheck.RunOptions.SuccessThreshold)
	assert.Equal(t, 1, options.HealthCheck.RunOptions.FailureThreshold)

	// Test allowed signals (empty in this case)
	assert.Len(t, options.AllowedSignals, 0)
}

func TestUnmanagedWorker_IntegrationWithProcessControlOptions(t *testing.T) {
	logger := &MockUnmanagedLogger{}
	unit := createTestUnmanagedUnit()

	worker := NewUnmanagedWorker("test-unmanaged-6", unit, logger)

	options := worker.ProcessControlOptions()

	// Test that options pass validation
	err := ValidateProcessControlOptions(options)
	assert.NoError(t, err, "UnmanagedWorker options should pass validation")
}

func TestUnmanagedWorker_IntegrationWithProcessControlOptions_Port(t *testing.T) {
	logger := &MockUnmanagedLogger{}
	unit := createTestUnmanagedUnitWithPortDiscovery()

	worker := NewUnmanagedWorker("test-unmanaged-7", unit, logger)

	options := worker.ProcessControlOptions()

	// Test that options pass validation
	err := ValidateProcessControlOptions(options)
	assert.NoError(t, err, "UnmanagedWorker options with port discovery should pass validation")
}

func TestUnmanagedWorker_MultipleInstances(t *testing.T) {
	logger := &MockUnmanagedLogger{}
	unit := createTestUnmanagedUnit()

	worker1 := NewUnmanagedWorker("worker-1", unit, logger)
	worker2 := NewUnmanagedWorker("worker-2", unit, logger)

	// Test independence
	assert.NotEqual(t, worker1.ID(), worker2.ID())
	assert.Equal(t, worker1.Metadata(), worker2.Metadata()) // Same unit, same metadata

	// Test same discovery configuration (since they share the same unit)
	options1 := worker1.ProcessControlOptions()
	options2 := worker2.ProcessControlOptions()

	assert.Equal(t, options1.Discovery, options2.Discovery)
	assert.Equal(t, options1.CanTerminate, options2.CanTerminate)
	assert.Equal(t, options1.CanRestart, options2.CanRestart)
}

func TestUnmanagedWorker_DifferentCapabilities(t *testing.T) {
	logger := &MockUnmanagedLogger{}

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
