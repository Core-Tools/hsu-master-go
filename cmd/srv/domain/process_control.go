package domain

import (
	"os"
	"time"
)

type SystemProcessControl struct {
	// Basic control
	CanTerminate bool // Can send SIGTERM/SIGKILL
	CanRestart   bool // Can restart (via service manager)

	// Service manager integration
	ServiceManager string // "systemd", "windows", "launchd"
	ServiceName    string // Service name for restart

	// Process signals
	AllowedSignals []os.Signal // Allowed signals to send

	// Graceful shutdown
	GracefulTimeout time.Duration // Time to wait for graceful shutdown
}

type RestartPolicy string

const (
	RestartNever         RestartPolicy = "never"
	RestartOnFailure     RestartPolicy = "on-failure"
	RestartAlways        RestartPolicy = "always"
	RestartUnlessStopped RestartPolicy = "unless-stopped"
)

type RestartConfig struct {
	Policy      RestartPolicy
	MaxRetries  int
	RetryDelay  time.Duration
	BackoffRate float64 // Exponential backoff multiplier
}

type ResourceLimits struct {
	// CPU limits
	CPU       float64 // Number of CPU cores
	CPUShares int     // CPU weight (Linux cgroups)

	// Memory limits
	Memory     int64 // Memory limit in bytes
	MemorySwap int64 // Memory + swap limit

	// Process limits
	MaxProcesses int // Maximum number of processes
	MaxOpenFiles int // Maximum open file descriptors

	// I/O limits
	IOWeight   int   // I/O priority weight
	IOReadBPS  int64 // Read bandwidth limit
	IOWriteBPS int64 // Write bandwidth limit
}

type ManagedProcessControl struct {
	// Process execution
	ExecutablePath   string
	Args             []string
	Environment      map[string]string
	WorkingDirectory string

	// Process control
	RestartPolicy RestartPolicy
	AutoStart     bool
	MaxRetries    int

	// Resource management
	ResourceLimits ResourceLimits
	Priority       int

	/*
		// I/O handling
		LogConfig      LogConfig
		StdoutRedirect string
		StderrRedirect string

		// Scheduling
		Schedule ScheduleConfig
	*/
}

type GenericProcessControl interface {
	Start() error
	Stop() error
	Restart() error
}
