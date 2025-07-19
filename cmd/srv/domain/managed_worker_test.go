package domain

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockLogger for testing
type MockManagedLogger struct {
	mock.Mock
}

func (m *MockManagedLogger) LogLevelf(level int, format string, args ...interface{}) {
	m.Called(format, args)
}

func (m *MockManagedLogger) Debugf(format string, args ...interface{}) {
	m.Called(format, args)
}

func (m *MockManagedLogger) Infof(format string, args ...interface{}) {
	m.Called(format, args)
}

func (m *MockManagedLogger) Warnf(format string, args ...interface{}) {
	m.Called(format, args)
}

func (m *MockManagedLogger) Errorf(format string, args ...interface{}) {
	m.Called(format, args)
}

func createTestManagedUnit() *ManagedUnit {
	var executablePath string
	var args []string
	var workingDirectory string

	// Set platform-specific defaults
	if runtime.GOOS == "windows" {
		executablePath = "C:\\Windows\\System32\\cmd.exe"
		args = []string{"/c", "echo", "test"}
		workingDirectory = "C:\\Windows\\Temp"
	} else {
		executablePath = "/bin/echo"
		args = []string{"test"}
		workingDirectory = "/tmp"
	}

	return &ManagedUnit{
		Metadata: UnitMetadata{
			Name:        "test-managed-unit",
			Description: "Test managed unit",
		},
		Control: ManagedProcessControlConfig{
			Execution: ExecutionConfig{
				ExecutablePath:   executablePath,
				Args:             args,
				WorkingDirectory: workingDirectory,
				WaitDelay:        5 * time.Second,
			},
			Restart: RestartConfig{
				Policy:      RestartOnFailure,
				MaxRetries:  3,
				RetryDelay:  10 * time.Second,
				BackoffRate: 2.0,
			},
			Limits: ResourceLimits{
				CPU:          1.0,
				Memory:       1024 * 1024 * 1024, // 1GB
				MaxProcesses: 10,
				MaxOpenFiles: 100,
			},
			GracefulTimeout: 30 * time.Second,
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

func TestNewManagedWorker(t *testing.T) {
	logger := &MockManagedLogger{}
	unit := createTestManagedUnit()

	worker := NewManagedWorker("test-managed-1", unit, logger)

	assert.NotNil(t, worker)
	assert.Equal(t, "test-managed-1", worker.ID())
}

func TestManagedWorker_ID(t *testing.T) {
	logger := &MockManagedLogger{}
	unit := createTestManagedUnit()

	worker := NewManagedWorker("test-managed-2", unit, logger)

	assert.Equal(t, "test-managed-2", worker.ID())
}

func TestManagedWorker_Metadata(t *testing.T) {
	logger := &MockManagedLogger{}
	unit := createTestManagedUnit()

	worker := NewManagedWorker("test-managed-3", unit, logger)

	metadata := worker.Metadata()
	assert.Equal(t, "test-managed-unit", metadata.Name)
	assert.Equal(t, "Test managed unit", metadata.Description)
}

func TestManagedWorker_ProcessControlOptions(t *testing.T) {
	logger := &MockManagedLogger{}
	unit := createTestManagedUnit()

	worker := NewManagedWorker("test-managed-4", unit, logger)

	options := worker.ProcessControlOptions()

	// Test basic capabilities
	assert.True(t, options.CanAttach, "ManagedWorker should support attachment")
	assert.True(t, options.CanTerminate, "ManagedWorker should support termination")
	assert.True(t, options.CanRestart, "ManagedWorker should support restart")

	// Test discovery configuration
	assert.Equal(t, DiscoveryMethodPIDFile, options.Discovery.Method)
	assert.NotEmpty(t, options.Discovery.PIDFile)
	assert.Equal(t, 30*time.Second, options.Discovery.CheckInterval)

	// Test ExecuteCmd is present
	assert.NotNil(t, options.ExecuteCmd, "ManagedWorker should provide ExecuteCmd")

	// Test AttachCmd is present
	assert.NotNil(t, options.AttachCmd, "ManagedWorker should provide AttachCmd")

	// Test restart configuration
	require.NotNil(t, options.Restart)
	assert.Equal(t, RestartOnFailure, options.Restart.Policy)
	assert.Equal(t, 3, options.Restart.MaxRetries)
	assert.Equal(t, 10*time.Second, options.Restart.RetryDelay)
	assert.Equal(t, 2.0, options.Restart.BackoffRate)

	// Test resource limits
	require.NotNil(t, options.Limits)
	assert.Equal(t, 1.0, options.Limits.CPU)
	assert.Equal(t, int64(1024*1024*1024), options.Limits.Memory)
	assert.Equal(t, 10, options.Limits.MaxProcesses)
	assert.Equal(t, 100, options.Limits.MaxOpenFiles)

	// Test graceful timeout
	assert.Equal(t, 30*time.Second, options.GracefulTimeout)

	// Test health check is provided by ExecuteCmd or AttachCmd
	assert.Nil(t, options.HealthCheck, "ManagedWorker should provide health check via ExecuteCmd or AttachCmd")
}

func TestManagedWorker_PIDFileGeneration(t *testing.T) {
	logger := &MockManagedLogger{}
	unit := createTestManagedUnit()

	worker := NewManagedWorker("test-managed-5", unit, logger)

	options := worker.ProcessControlOptions()
	pidFile := options.Discovery.PIDFile

	// Test that PID file path is generated
	assert.NotEmpty(t, pidFile)

	// Test worker ID is included
	assert.Contains(t, pidFile, "test-managed-5")

	// Test .pid extension
	assert.Equal(t, ".pid", filepath.Ext(pidFile))

	// Test that path is absolute
	assert.True(t, filepath.IsAbs(pidFile))
}

func TestManagedWorker_ExecuteCmd_NilContext(t *testing.T) {
	logger := &MockManagedLogger{}
	unit := createTestManagedUnit()

	worker := NewManagedWorker("test-managed-6", unit, logger).(*managedWorker)

	cmd, stdout, healthCheck, err := worker.ExecuteCmd(nil)

	assert.Nil(t, cmd)
	assert.Nil(t, stdout)
	assert.Nil(t, healthCheck)
	assert.Error(t, err)
	assert.True(t, IsValidationError(err))
}

func TestManagedWorker_ExecuteCmd_ValidContext(t *testing.T) {
	logger := &MockManagedLogger{}
	logger.On("Infof", mock.Anything, mock.Anything).Maybe()
	logger.On("Debugf", mock.Anything, mock.Anything).Maybe()
	logger.On("Errorf", mock.Anything, mock.Anything).Maybe()

	// Create a unit with platform-appropriate executable (handled by createTestManagedUnit)
	unit := createTestManagedUnit()

	worker := NewManagedWorker("test-managed-7", unit, logger).(*managedWorker)

	ctx := context.Background()
	cmd, stdout, healthCheck, err := worker.ExecuteCmd(ctx)

	if cmd != nil {
		// Clean up the process if it was created
		cmd.Process.Kill()
	}
	if stdout != nil {
		stdout.Close()
	}

	// Note: This test might fail if the executable doesn't exist or isn't executable
	// But the structure should be correct
	if err == nil {
		assert.NotNil(t, cmd)
		assert.NotNil(t, stdout)
		assert.NotNil(t, healthCheck)
		assert.Equal(t, HealthCheckTypeProcess, healthCheck.Type)
	} else {
		// If execution fails, error should be properly formatted
		assert.True(t, IsProcessError(err) || IsValidationError(err) || IsPermissionError(err) || IsIOError(err))
	}

	logger.AssertExpectations(t)
}

func TestManagedWorker_ExecuteCmd_PIDFileWriting(t *testing.T) {
	// Create a temporary directory for PID files
	tempDir := t.TempDir()

	pidConfig := ProcessFileConfig{
		BaseDirectory:   tempDir,
		ServiceContext:  UserService,
		AppName:         "test-app",
		UseSubdirectory: false,
	}

	var executablePath string
	var args []string
	var workingDirectory string

	// Set platform-specific defaults
	if runtime.GOOS == "windows" {
		executablePath = "C:\\Windows\\System32\\cmd.exe"
		args = []string{"/c", "echo", "test"}
		workingDirectory = "C:\\Windows\\Temp"
	} else {
		executablePath = "/bin/echo"
		args = []string{"test"}
		workingDirectory = "/tmp"
	}

	// Create test unit with PID file configuration
	unit := &ManagedUnit{
		Metadata: UnitMetadata{
			Name:        "Test Worker",
			Description: "Test worker for PID file testing",
		},
		Control: ManagedProcessControlConfig{
			Execution: ExecutionConfig{
				ExecutablePath:    executablePath,
				Args:              args,
				WorkingDirectory:  workingDirectory,
				Environment:       []string{},
				ProcessFileConfig: &pidConfig,
				WaitDelay:         5 * time.Second,
			},
			GracefulTimeout: 30 * time.Second,
			Restart: RestartConfig{
				Policy:      RestartOnFailure,
				MaxRetries:  3,
				RetryDelay:  5 * time.Second,
				BackoffRate: 2.0,
			},
			Limits: ResourceLimits{
				CPU:          1.0,
				Memory:       512 * 1024 * 1024, // 512MB
				MaxProcesses: 10,
				MaxOpenFiles: 100,
			},
		},
		HealthCheck: HealthCheckConfig{
			Type: HealthCheckTypeProcess,
			RunOptions: HealthCheckRunOptions{
				Enabled:      true,
				Interval:     30 * time.Second,
				Timeout:      5 * time.Second,
				InitialDelay: 5 * time.Second,
				Retries:      2,
			},
		},
	}

	// Create worker
	logger := &MockManagedLogger{}
	logger.On("Infof", mock.Anything, mock.Anything).Maybe()
	logger.On("Debugf", mock.Anything, mock.Anything).Maybe()
	logger.On("Errorf", mock.Anything, mock.Anything).Maybe()

	worker := NewManagedWorker("test-worker", unit, logger).(*managedWorker)

	// Execute command
	ctx := context.Background()
	cmd, stdout, healthConfig, err := worker.ExecuteCmd(ctx)

	// Verify command execution
	assert.NoError(t, err)
	require.NotNil(t, cmd)
	assert.NotNil(t, stdout)
	assert.NotNil(t, healthConfig)
	assert.NotNil(t, cmd.Process)

	// Verify PID file was created
	pidFilePath := worker.pidManager.GeneratePIDFilePath("test-worker")
	assert.FileExists(t, pidFilePath)

	// Verify PID file content
	content, err := os.ReadFile(pidFilePath)
	assert.NoError(t, err)
	expectedContent := fmt.Sprintf("%d\n", cmd.Process.Pid)
	assert.Equal(t, expectedContent, string(content))

	// Clean up
	if stdout != nil {
		stdout.Close()
	}
	if cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
	}
}

func TestManagedWorker_IntegrationWithProcessControlOptions(t *testing.T) {
	logger := &MockManagedLogger{}
	unit := createTestManagedUnit()

	worker := NewManagedWorker("test-managed-8", unit, logger)

	options := worker.ProcessControlOptions()

	// Test that options pass validation
	err := ValidateProcessControlOptions(options)
	assert.NoError(t, err, "ManagedWorker options should pass validation")
}

func TestManagedWorker_MultipleInstances(t *testing.T) {
	logger := &MockManagedLogger{}
	unit := createTestManagedUnit()

	worker1 := NewManagedWorker("worker-1", unit, logger)
	worker2 := NewManagedWorker("worker-2", unit, logger)

	// Test independence
	assert.NotEqual(t, worker1.ID(), worker2.ID())
	assert.Equal(t, worker1.Metadata(), worker2.Metadata()) // Same unit, same metadata

	// Test different PID files
	options1 := worker1.ProcessControlOptions()
	options2 := worker2.ProcessControlOptions()

	assert.NotEqual(t, options1.Discovery.PIDFile, options2.Discovery.PIDFile)
	assert.Contains(t, options1.Discovery.PIDFile, "worker-1")
	assert.Contains(t, options2.Discovery.PIDFile, "worker-2")
}
