package monitoring

import (
	"testing"
	"time"

	"github.com/core-tools/hsu-master/pkg/errors"

	"github.com/stretchr/testify/assert"
)

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
				assert.True(t, errors.IsValidationError(err))
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
