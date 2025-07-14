package domain

import (
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestValidateWorkerID(t *testing.T) {
	tests := []struct {
		name      string
		id        string
		shouldErr bool
		errorType ErrorType
	}{
		{"valid_simple", "worker-1", false, ""},
		{"valid_with_underscore", "worker_1", false, ""},
		{"valid_alphanumeric", "worker123", false, ""},
		{"empty_id", "", true, ErrorTypeValidation},
		{"too_long", "a" + string(make([]byte, 65)), true, ErrorTypeValidation},
		{"invalid_chars", "worker@1", true, ErrorTypeValidation},
		{"invalid_space", "worker 1", true, ErrorTypeValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWorkerID(tt.id)

			if tt.shouldErr {
				assert.Error(t, err)
				var domainErr *DomainError
				assert.ErrorAs(t, err, &domainErr)
				assert.Equal(t, tt.errorType, domainErr.Type)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateDiscoveryConfig(t *testing.T) {
	// Use OS-dependent path for PID file
	var pidFile string
	if runtime.GOOS == "windows" {
		pidFile = "C:\\Temp\\test.pid"
	} else {
		pidFile = "/tmp/test.pid"
	}

	tests := []struct {
		name      string
		config    DiscoveryConfig
		shouldErr bool
	}{
		{
			name: "valid_pid_file",
			config: DiscoveryConfig{
				Method:  DiscoveryMethodPIDFile,
				PIDFile: pidFile,
			},
			shouldErr: false,
		},
		{
			name: "valid_process_name",
			config: DiscoveryConfig{
				Method:      DiscoveryMethodProcessName,
				ProcessName: "nginx",
			},
			shouldErr: false,
		},
		{
			name: "valid_port",
			config: DiscoveryConfig{
				Method:   DiscoveryMethodPort,
				Port:     8080,
				Protocol: "tcp",
			},
			shouldErr: false,
		},
		{
			name: "valid_service",
			config: DiscoveryConfig{
				Method:      DiscoveryMethodServiceName,
				ServiceName: "nginx",
			},
			shouldErr: false,
		},
		{
			name: "invalid_pid_file_empty",
			config: DiscoveryConfig{
				Method:  DiscoveryMethodPIDFile,
				PIDFile: "",
			},
			shouldErr: true,
		},
		{
			name: "invalid_pid_file_relative",
			config: DiscoveryConfig{
				Method:  DiscoveryMethodPIDFile,
				PIDFile: "relative/path.pid",
			},
			shouldErr: true,
		},
		{
			name: "invalid_port_zero",
			config: DiscoveryConfig{
				Method:   DiscoveryMethodPort,
				Port:     0,
				Protocol: "tcp",
			},
			shouldErr: true,
		},
		{
			name: "invalid_port_too_high",
			config: DiscoveryConfig{
				Method:   DiscoveryMethodPort,
				Port:     65536,
				Protocol: "tcp",
			},
			shouldErr: true,
		},
		{
			name: "invalid_protocol",
			config: DiscoveryConfig{
				Method:   DiscoveryMethodPort,
				Port:     8080,
				Protocol: "http",
			},
			shouldErr: true,
		},
		{
			name: "negative_check_interval",
			config: DiscoveryConfig{
				Method:        DiscoveryMethodPIDFile,
				PIDFile:       pidFile,
				CheckInterval: -1 * time.Second,
			},
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDiscoveryConfig(tt.config)

			if tt.shouldErr {
				assert.Error(t, err)
				assert.True(t, IsValidationError(err))
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateRestartConfig(t *testing.T) {
	tests := []struct {
		name      string
		config    RestartConfig
		shouldErr bool
	}{
		{
			name: "valid_config",
			config: RestartConfig{
				Policy:      RestartOnFailure,
				MaxRetries:  5,
				RetryDelay:  30 * time.Second,
				BackoffRate: 1.5,
			},
			shouldErr: false,
		},
		{
			name: "invalid_policy",
			config: RestartConfig{
				Policy:      RestartPolicy("invalid"),
				MaxRetries:  5,
				RetryDelay:  30 * time.Second,
				BackoffRate: 1.5,
			},
			shouldErr: true,
		},
		{
			name: "negative_max_retries",
			config: RestartConfig{
				Policy:      RestartOnFailure,
				MaxRetries:  -1,
				RetryDelay:  30 * time.Second,
				BackoffRate: 1.5,
			},
			shouldErr: true,
		},
		{
			name: "negative_retry_delay",
			config: RestartConfig{
				Policy:      RestartOnFailure,
				MaxRetries:  5,
				RetryDelay:  -1 * time.Second,
				BackoffRate: 1.5,
			},
			shouldErr: true,
		},
		{
			name: "low_backoff_rate",
			config: RestartConfig{
				Policy:      RestartOnFailure,
				MaxRetries:  5,
				RetryDelay:  30 * time.Second,
				BackoffRate: 0.5,
			},
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRestartConfig(tt.config)

			if tt.shouldErr {
				assert.Error(t, err)
				assert.True(t, IsValidationError(err))
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidatePID(t *testing.T) {
	tests := []struct {
		name        string
		pidStr      string
		expectedPID int
		shouldErr   bool
	}{
		{"valid_pid", "1234", 1234, false},
		{"empty_pid", "", 0, true},
		{"invalid_format", "abc", 0, true},
		{"zero_pid", "0", 0, true},
		{"negative_pid", "-1", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pid, err := ValidatePID(tt.pidStr)

			if tt.shouldErr {
				assert.Error(t, err)
				assert.True(t, IsValidationError(err))
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedPID, pid)
			}
		})
	}
}

func TestValidatePort(t *testing.T) {
	tests := []struct {
		name      string
		port      int
		shouldErr bool
	}{
		{"valid_port", 8080, false},
		{"port_1", 1, false},
		{"port_65535", 65535, false},
		{"port_0", 0, true},
		{"port_negative", -1, true},
		{"port_too_high", 65536, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePort(tt.port)

			if tt.shouldErr {
				assert.Error(t, err)
				assert.True(t, IsValidationError(err))
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateNetworkAddress(t *testing.T) {
	tests := []struct {
		name      string
		address   string
		shouldErr bool
	}{
		{"valid_localhost", "localhost:8080", false},
		{"valid_ip", "127.0.0.1:8080", false},
		{"valid_hostname", "example.com:80", false},
		{"empty_address", "", true},
		{"no_port", "localhost", true},
		{"invalid_port", "localhost:abc", true},
		{"port_too_high", "localhost:65536", true},
		{"empty_host", ":8080", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNetworkAddress(tt.address)

			if tt.shouldErr {
				assert.Error(t, err)
				assert.True(t, IsValidationError(err))
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateTimeout(t *testing.T) {
	tests := []struct {
		name      string
		timeout   time.Duration
		shouldErr bool
	}{
		{"valid_timeout", 30 * time.Second, false},
		{"negative_timeout", -1 * time.Second, true},
		{"zero_timeout", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTimeout(tt.timeout, "test")

			if tt.shouldErr {
				assert.Error(t, err)
				assert.True(t, IsValidationError(err))
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
