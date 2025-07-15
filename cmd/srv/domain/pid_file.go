package domain

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Default application name for HSU Master
const DefaultAppName = "hsu-master"

// PIDFileConfig holds configuration for PID file generation
type PIDFileConfig struct {
	// Base directory for PID files. If empty, uses OS-appropriate default
	BaseDirectory string

	// Service context - affects directory selection
	ServiceContext ServiceContext

	// Application name for subdirectory creation
	AppName string

	// Create subdirectory for the app (recommended for system services)
	UseSubdirectory bool
}

// ServiceContext defines the context in which the service runs
type ServiceContext string

const (
	// SystemService runs as a system service (daemon)
	SystemService ServiceContext = "system"

	// UserService runs as a user service
	UserService ServiceContext = "user"

	// SessionService runs as a session service (cleaned up on logout)
	SessionService ServiceContext = "session"
)

// PIDFileManager provides PID file path generation and management
type PIDFileManager struct {
	config PIDFileConfig
}

// NewPIDFileManager creates a new PID file manager with the given configuration
func NewPIDFileManager(config PIDFileConfig) *PIDFileManager {
	// Set defaults
	if config.AppName == "" {
		config.AppName = DefaultAppName
	}

	if config.ServiceContext == "" {
		config.ServiceContext = SystemService
	}

	return &PIDFileManager{
		config: config,
	}
}

// GeneratePIDFilePath generates an appropriate PID file path for the given worker ID
func (m *PIDFileManager) GeneratePIDFilePath(workerID string) string {
	baseDir := m.getBaseDirectory()

	// Create app subdirectory if requested
	if m.config.UseSubdirectory {
		baseDir = filepath.Join(baseDir, m.config.AppName)
	}

	return filepath.Join(baseDir, workerID+".pid")
}

// getBaseDirectory returns the appropriate base directory for PID files
func (m *PIDFileManager) getBaseDirectory() string {
	// Use explicit configuration if provided
	if m.config.BaseDirectory != "" {
		return m.config.BaseDirectory
	}

	// Use OS-appropriate defaults based on service context
	switch m.config.ServiceContext {
	case SystemService:
		return m.getSystemServiceDirectory()
	case UserService:
		return m.getUserServiceDirectory()
	case SessionService:
		return m.getSessionServiceDirectory()
	default:
		return m.getSystemServiceDirectory()
	}
}

// getSystemServiceDirectory returns the directory for system services
func (m *PIDFileManager) getSystemServiceDirectory() string {
	switch runtime.GOOS {
	case "windows":
		// Use ProgramData for system services on Windows
		programData := os.Getenv("PROGRAMDATA")
		if programData == "" {
			programData = "C:\\ProgramData"
		}
		return programData

	case "darwin":
		// macOS system services
		return "/var/run"

	default:
		// Linux and other Unix systems
		// Modern standard is /run, with fallback to /var/run
		if _, err := os.Stat("/run"); err == nil {
			return "/run"
		}
		return "/var/run"
	}
}

// getUserServiceDirectory returns the directory for user services
func (m *PIDFileManager) getUserServiceDirectory() string {
	switch runtime.GOOS {
	case "windows":
		// Use LocalAppData for user services on Windows
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			userProfile := os.Getenv("USERPROFILE")
			if userProfile != "" {
				localAppData = filepath.Join(userProfile, "AppData", "Local")
			} else {
				localAppData = "C:\\Users\\Default\\AppData\\Local"
			}
		}
		return localAppData

	case "darwin":
		// macOS user services
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "/tmp"
		}
		return filepath.Join(homeDir, "Library", "Application Support")

	default:
		// Linux and other Unix systems
		// Use XDG_RUNTIME_DIR if available, otherwise /tmp
		if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
			return runtimeDir
		}
		return "/tmp"
	}
}

// getSessionServiceDirectory returns the directory for session services
func (m *PIDFileManager) getSessionServiceDirectory() string {
	switch runtime.GOOS {
	case "windows":
		// Windows doesn't have a direct equivalent, use temp directory
		return os.TempDir()

	case "darwin":
		// macOS session services
		return os.TempDir()

	default:
		// Linux systemd session services
		uid := os.Getuid()
		sessionDir := fmt.Sprintf("/run/user/%d", uid)

		// Check if session directory exists
		if _, err := os.Stat(sessionDir); err == nil {
			return sessionDir
		}

		// Fallback to temp directory
		return "/tmp"
	}
}

// ValidatePIDFileDirectory validates that the PID file directory exists and is writable
func (m *PIDFileManager) ValidatePIDFileDirectory(pidFilePath string) error {
	dir := filepath.Dir(pidFilePath)

	// Check if directory exists
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			// Try to create the directory
			if err := os.MkdirAll(dir, 0755); err != nil {
				return NewIOError("failed to create PID file directory", err).WithContext("directory", dir)
			}
		} else {
			return NewIOError("failed to access PID file directory", err).WithContext("directory", dir)
		}
	} else if !info.IsDir() {
		return NewValidationError("PID file path is not a directory", nil).WithContext("path", dir)
	}

	// Check if directory is writable
	testFile := filepath.Join(dir, ".write_test")
	if file, err := os.Create(testFile); err != nil {
		return NewPermissionError("PID file directory is not writable", err).WithContext("directory", dir)
	} else {
		file.Close()
		os.Remove(testFile)
	}

	return nil
}

// GetRecommendedPIDFileConfig returns recommended PID file configuration for different deployment scenarios
func GetRecommendedPIDFileConfig(scenario string, appName string) PIDFileConfig {
	if appName == "" {
		appName = DefaultAppName
	}

	switch strings.ToLower(scenario) {
	case "system", "daemon", "service":
		return PIDFileConfig{
			ServiceContext:  SystemService,
			AppName:         appName,
			UseSubdirectory: true,
		}

	case "user", "personal":
		return PIDFileConfig{
			ServiceContext:  UserService,
			AppName:         appName,
			UseSubdirectory: true,
		}

	case "session", "desktop":
		return PIDFileConfig{
			ServiceContext:  SessionService,
			AppName:         appName,
			UseSubdirectory: false,
		}

	case "development", "dev", "test":
		return PIDFileConfig{
			BaseDirectory:   filepath.Join(os.TempDir(), appName+"-dev"),
			ServiceContext:  UserService,
			AppName:         appName,
			UseSubdirectory: false,
		}

	default:
		// Default to system service
		return PIDFileConfig{
			ServiceContext:  SystemService,
			AppName:         appName,
			UseSubdirectory: true,
		}
	}
}
