package domain

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPIDFileManager(t *testing.T) {
	config := PIDFileConfig{
		ServiceContext: SystemService,
		AppName:        "test-app",
	}

	manager := NewPIDFileManager(config)

	require.NotNil(t, manager)
	assert.Equal(t, "test-app", manager.config.AppName)
	assert.Equal(t, SystemService, manager.config.ServiceContext)
}

func TestNewPIDFileManager_WithDefaults(t *testing.T) {
	config := PIDFileConfig{} // Empty config

	manager := NewPIDFileManager(config)

	require.NotNil(t, manager)
	assert.Equal(t, "hsu-master", manager.config.AppName)
	assert.Equal(t, SystemService, manager.config.ServiceContext)
}

func TestGeneratePIDFilePath_SystemService(t *testing.T) {
	config := PIDFileConfig{
		ServiceContext:  SystemService,
		AppName:         "test-app",
		UseSubdirectory: true,
	}

	manager := NewPIDFileManager(config)
	pidFile := manager.GeneratePIDFilePath("worker-123")

	// Test that path is absolute
	assert.True(t, filepath.IsAbs(pidFile))

	// Test that worker ID is included
	assert.Contains(t, pidFile, "worker-123")

	// Test .pid extension
	assert.Equal(t, ".pid", filepath.Ext(pidFile))

	// Test that app name is in path when UseSubdirectory is true
	assert.Contains(t, pidFile, "test-app")

	// Test OS-specific behavior
	switch runtime.GOOS {
	case "windows":
		// Should use ProgramData or similar
		assert.True(t,
			filepath.HasPrefix(pidFile, "C:\\ProgramData") ||
				filepath.HasPrefix(pidFile, "C:\\programdata"), // case insensitive
			"Expected ProgramData path on Windows, got: %s", pidFile)
	default:
		// Should use /run or /var/run
		assert.True(t,
			filepath.HasPrefix(pidFile, "/run") ||
				filepath.HasPrefix(pidFile, "/var/run"),
			"Expected /run or /var/run path on Unix, got: %s", pidFile)
	}
}

func TestGeneratePIDFilePath_UserService(t *testing.T) {
	config := PIDFileConfig{
		ServiceContext: UserService,
		AppName:        "test-app",
	}

	manager := NewPIDFileManager(config)
	pidFile := manager.GeneratePIDFilePath("user-worker-456")

	// Test that path is absolute
	assert.True(t, filepath.IsAbs(pidFile))

	// Test that worker ID is included
	assert.Contains(t, pidFile, "user-worker-456")

	// Test .pid extension
	assert.Equal(t, ".pid", filepath.Ext(pidFile))
}

func TestGeneratePIDFilePath_SessionService(t *testing.T) {
	config := PIDFileConfig{
		ServiceContext: SessionService,
		AppName:        "test-app",
	}

	manager := NewPIDFileManager(config)
	pidFile := manager.GeneratePIDFilePath("session-worker-789")

	// Test that path is absolute
	assert.True(t, filepath.IsAbs(pidFile))

	// Test that worker ID is included
	assert.Contains(t, pidFile, "session-worker-789")

	// Test .pid extension
	assert.Equal(t, ".pid", filepath.Ext(pidFile))
}

func TestGeneratePIDFilePath_WithCustomBaseDirectory(t *testing.T) {
	customDir := filepath.Join(os.TempDir(), "custom-pid-dir")

	config := PIDFileConfig{
		BaseDirectory:  customDir,
		ServiceContext: SystemService,
		AppName:        "test-app",
	}

	manager := NewPIDFileManager(config)
	pidFile := manager.GeneratePIDFilePath("custom-worker")

	// Test that custom base directory is used
	assert.Contains(t, pidFile, customDir)

	// Test that worker ID is included
	assert.Contains(t, pidFile, "custom-worker")

	// Test .pid extension
	assert.Equal(t, ".pid", filepath.Ext(pidFile))
}

func TestGeneratePIDFilePath_WithoutSubdirectory(t *testing.T) {
	config := PIDFileConfig{
		ServiceContext:  SystemService,
		AppName:         "test-app",
		UseSubdirectory: false,
	}

	manager := NewPIDFileManager(config)
	pidFile := manager.GeneratePIDFilePath("direct-worker")

	// Test that worker ID is included
	assert.Contains(t, pidFile, "direct-worker")

	// Test that app name is NOT in path when UseSubdirectory is false
	// (it should be in the filename directory, not a subdirectory)
	dir := filepath.Dir(pidFile)
	assert.NotContains(t, dir, "test-app")
}

func TestValidatePIDFileDirectory_Success(t *testing.T) {
	config := PIDFileConfig{
		BaseDirectory: os.TempDir(),
		AppName:       "test-validation",
	}

	manager := NewPIDFileManager(config)
	pidFile := manager.GeneratePIDFilePath("validate-worker")

	// Should succeed for temp directory
	err := manager.ValidatePIDFileDirectory(pidFile)
	assert.NoError(t, err)
}

func TestValidatePIDFileDirectory_CreateDirectory(t *testing.T) {
	// Use a subdirectory that doesn't exist
	testDir := filepath.Join(os.TempDir(), "test-pid-creation", "subdir")

	config := PIDFileConfig{
		BaseDirectory: testDir,
		AppName:       "test-creation",
	}

	manager := NewPIDFileManager(config)
	pidFile := manager.GeneratePIDFilePath("creation-worker")

	// Should create directory if it doesn't exist
	err := manager.ValidatePIDFileDirectory(pidFile)
	assert.NoError(t, err)

	// Verify directory was created
	assert.DirExists(t, filepath.Dir(pidFile))

	// Cleanup
	os.RemoveAll(filepath.Join(os.TempDir(), "test-pid-creation"))
}

func TestValidatePIDFileDirectory_InvalidPath(t *testing.T) {
	// Use a file as directory (invalid)
	tempFile := filepath.Join(os.TempDir(), "test-file")
	file, err := os.Create(tempFile)
	require.NoError(t, err)
	file.Close()
	defer os.Remove(tempFile)

	config := PIDFileConfig{
		BaseDirectory: tempFile, // This is a file, not a directory
		AppName:       "test-invalid",
	}

	manager := NewPIDFileManager(config)
	pidFile := manager.GeneratePIDFilePath("invalid-worker")

	// Should fail because the path is a file, not a directory
	err = manager.ValidatePIDFileDirectory(pidFile)
	assert.Error(t, err)
	assert.True(t, IsValidationError(err))
}

func TestGetRecommendedPIDFileConfig(t *testing.T) {
	testCases := []struct {
		name              string
		scenario          string
		appName           string
		expectedContext   ServiceContext
		expectedUseSubdir bool
		expectedAppName   string
		expectedBaseDir   string // For development scenario
	}{
		{
			name:              "system service",
			scenario:          "system",
			appName:           "my-app",
			expectedContext:   SystemService,
			expectedUseSubdir: true,
			expectedAppName:   "my-app",
		},
		{
			name:              "user service",
			scenario:          "user",
			appName:           "my-app",
			expectedContext:   UserService,
			expectedUseSubdir: true,
			expectedAppName:   "my-app",
		},
		{
			name:              "session service",
			scenario:          "session",
			appName:           "my-app",
			expectedContext:   SessionService,
			expectedUseSubdir: false,
			expectedAppName:   "my-app",
		},
		{
			name:              "development",
			scenario:          "development",
			appName:           "my-app",
			expectedContext:   UserService,
			expectedUseSubdir: false,
			expectedAppName:   "my-app",
		},
		{
			name:              "empty app name uses default",
			scenario:          "system",
			appName:           "",
			expectedContext:   SystemService,
			expectedUseSubdir: true,
			expectedAppName:   DefaultAppName,
		},
		{
			name:              "unknown scenario defaults to system",
			scenario:          "unknown",
			appName:           "my-app",
			expectedContext:   SystemService,
			expectedUseSubdir: true,
			expectedAppName:   "my-app",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := GetRecommendedPIDFileConfig(tc.scenario, tc.appName)

			assert.Equal(t, tc.expectedContext, config.ServiceContext)
			assert.Equal(t, tc.expectedUseSubdir, config.UseSubdirectory)
			assert.Equal(t, tc.expectedAppName, config.AppName)

			if tc.scenario == "development" || tc.scenario == "dev" || tc.scenario == "test" {
				assert.Contains(t, config.BaseDirectory, tc.expectedAppName+"-dev")
			}
		})
	}
}

func TestPIDFileManager_MultipleWorkers(t *testing.T) {
	config := PIDFileConfig{
		ServiceContext: SystemService,
		AppName:        "multi-test",
	}

	manager := NewPIDFileManager(config)

	// Test multiple workers get different paths
	pidFile1 := manager.GeneratePIDFilePath("worker-1")
	pidFile2 := manager.GeneratePIDFilePath("worker-2")

	assert.NotEqual(t, pidFile1, pidFile2)
	assert.Contains(t, pidFile1, "worker-1")
	assert.Contains(t, pidFile2, "worker-2")
}

func TestPIDFileManager_PathSeparators(t *testing.T) {
	config := PIDFileConfig{
		ServiceContext:  SystemService,
		AppName:         "path-test",
		UseSubdirectory: true,
	}

	manager := NewPIDFileManager(config)
	pidFile := manager.GeneratePIDFilePath("path-worker")

	// Test that path uses correct separators for the OS
	assert.True(t, filepath.IsAbs(pidFile))
	assert.Equal(t, filepath.Clean(pidFile), pidFile, "Path should be clean")
}

func TestPIDFileManager_WorkerIDValidation(t *testing.T) {
	config := PIDFileConfig{
		ServiceContext: SystemService,
		AppName:        "validation-test",
	}

	manager := NewPIDFileManager(config)

	// Test various worker ID formats
	testCases := []struct {
		workerID string
		expected bool
	}{
		{"valid-worker", true},
		{"worker_123", true},
		{"Worker-ABC", true},
		{"123-worker", true},
		{"", true}, // Empty should still generate a valid path
	}

	for _, tc := range testCases {
		pidFile := manager.GeneratePIDFilePath(tc.workerID)

		// Should always generate a valid path
		assert.True(t, filepath.IsAbs(pidFile), "Worker ID: %s", tc.workerID)
		assert.Equal(t, ".pid", filepath.Ext(pidFile), "Worker ID: %s", tc.workerID)

		if tc.workerID != "" {
			assert.Contains(t, pidFile, tc.workerID, "Worker ID: %s", tc.workerID)
		}
	}
}

func TestPIDFileManager_WritePIDFile(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	config := PIDFileConfig{
		BaseDirectory:   tempDir,
		ServiceContext:  UserService,
		AppName:         "test-app",
		UseSubdirectory: false,
	}

	manager := NewPIDFileManager(config)

	// Test writing PID file
	workerID := "test-worker"
	pid := 12345

	err := manager.WritePIDFile(workerID, pid)
	assert.NoError(t, err)

	// Verify file was created with correct content
	pidFilePath := manager.GeneratePIDFilePath(workerID)
	assert.FileExists(t, pidFilePath)

	content, err := os.ReadFile(pidFilePath)
	assert.NoError(t, err)
	assert.Equal(t, "12345\n", string(content))
}

func TestPIDFileManager_WritePIDFile_InvalidDirectory(t *testing.T) {
	// Use a path that cannot be created on any platform
	var invalidPath string
	if runtime.GOOS == "windows" {
		// Use a path with invalid characters on Windows
		invalidPath = "C:\\invalid\\path\\with\\null\\char\x00\\that\\cannot\\be\\created"
	} else {
		// Use a path under /dev/null which is a file, not a directory
		invalidPath = "/dev/null/invalid/path/that/cannot/be/created"
	}

	config := PIDFileConfig{
		BaseDirectory:   invalidPath,
		ServiceContext:  UserService,
		AppName:         "test-app",
		UseSubdirectory: false,
	}

	manager := NewPIDFileManager(config)

	// Test writing PID file to invalid directory
	workerID := "test-worker"
	pid := 12345

	err := manager.WritePIDFile(workerID, pid)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "PID file directory validation failed")
}

func TestPIDFileManager_WritePIDFile_WithSubdirectory(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	config := PIDFileConfig{
		BaseDirectory:   tempDir,
		ServiceContext:  UserService,
		AppName:         "test-app",
		UseSubdirectory: true,
	}

	manager := NewPIDFileManager(config)

	// Test writing PID file with subdirectory
	workerID := "test-worker"
	pid := 54321

	err := manager.WritePIDFile(workerID, pid)
	assert.NoError(t, err)

	// Verify file was created in subdirectory with correct content
	pidFilePath := manager.GeneratePIDFilePath(workerID)
	assert.FileExists(t, pidFilePath)
	assert.Contains(t, pidFilePath, "test-app") // Should contain subdirectory

	content, err := os.ReadFile(pidFilePath)
	assert.NoError(t, err)
	assert.Equal(t, "54321\n", string(content))
}
