package processcontrolimpl

import (
	"sync"
	"time"

	"github.com/core-tools/hsu-master-go/pkg/errors"
	"github.com/core-tools/hsu-master/pkg/logging"
	"github.com/core-tools/hsu-master/pkg/monitoring"
)

type RestartFunc func() error

type RestartCircuitBreaker interface {
	ExecuteRestart(restartFunc RestartFunc) error
	Reset()
}

func NewRestartCircuitBreaker(config *monitoring.RestartConfig, id string, logger logging.Logger) RestartCircuitBreaker {
	return &restartCircuitBreaker{
		config:             config,
		id:                 id,
		logger:             logger,
		restartAttempts:    0,
		lastRestartTime:    time.Now(),
		circuitBreakerOpen: false,
	}
}

type restartCircuitBreaker struct {
	config      *monitoring.RestartConfig
	restartFunc RestartFunc
	id          string
	logger      logging.Logger

	// State
	restartAttempts    int        // Track attempts across health monitor restarts
	lastRestartTime    time.Time  // Track timing for retry delay and backoff
	circuitBreakerOpen bool       // Circuit breaker to stop excessive restarts
	mutex              sync.Mutex // Protect restart state
}

func (rcb *restartCircuitBreaker) ExecuteRestart(restartFunc RestartFunc) error {
	rcb.mutex.Lock()
	defer rcb.mutex.Unlock()

	// Check circuit breaker
	if rcb.circuitBreakerOpen {
		rcb.logger.Errorf("Circuit breaker is open, ignoring restart request, id: %s, attempts: %d",
			rcb.id, rcb.restartAttempts)
		return errors.NewInternalError("restart circuit breaker is open", nil).WithContext("id", rcb.id)
	}

	// Check max retries
	if rcb.config.MaxRetries > 0 && rcb.restartAttempts >= rcb.config.MaxRetries {
		rcb.logger.Errorf("Max restart retries exceeded, opening circuit breaker, id: %s, attempts: %d, max: %d",
			rcb.id, rcb.restartAttempts, rcb.config.MaxRetries)
		rcb.circuitBreakerOpen = true
		return errors.NewInternalError("max restart retries exceeded", nil).WithContext("id", rcb.id)
	}

	// Calculate retry delay with exponential backoff
	now := time.Now()
	timeSinceLastRestart := now.Sub(rcb.lastRestartTime)

	retryDelay := rcb.config.RetryDelay
	if rcb.restartAttempts > 0 {
		// Apply exponential backoff
		backoffMultiplier := 1.0
		for i := 0; i < rcb.restartAttempts; i++ {
			backoffMultiplier *= rcb.config.BackoffRate
		}
		retryDelay = time.Duration(float64(retryDelay) * backoffMultiplier)
	}

	// Enforce retry delay
	if timeSinceLastRestart < retryDelay {
		waitTime := retryDelay - timeSinceLastRestart
		rcb.logger.Infof("Enforcing retry delay, id: %s, attempt: %d, waiting: %v",
			rcb.id, rcb.restartAttempts+1, waitTime)

		// Release lock during sleep to prevent deadlock
		rcb.mutex.Unlock()
		time.Sleep(waitTime)
		rcb.mutex.Lock()

		// Re-check circuit breaker after sleep
		if rcb.circuitBreakerOpen {
			return errors.NewInternalError("restart circuit breaker opened during delay", nil).WithContext("id", rcb.id)
		}
	}

	// Increment attempt counter and update timestamp
	rcb.restartAttempts++
	rcb.lastRestartTime = time.Now()

	rcb.logger.Warnf("Proceeding with restart, id: %s, attempt: %d/%d, delay: %v",
		rcb.id, rcb.restartAttempts, rcb.config.MaxRetries, retryDelay)

	// Release lock before calling restart to prevent deadlock
	rcb.mutex.Unlock()
	defer func() { rcb.mutex.Lock() }()

	if err := restartFunc(); err != nil {
		rcb.logger.Errorf("Failed to restart, id: %s, error: %v", rcb.id, err)
		return err
	}

	rcb.logger.Infof("Restart completed, id: %s, attempt: %d", rcb.id, rcb.restartAttempts)
	return nil
}

func (rcb *restartCircuitBreaker) Reset() {
	rcb.mutex.Lock()
	defer rcb.mutex.Unlock()

	if rcb.restartAttempts > 0 || rcb.circuitBreakerOpen {
		rcb.logger.Infof("Resetting circuit breaker, id: %s, previous attempts: %d",
			rcb.id, rcb.restartAttempts)
		rcb.restartAttempts = 0
		rcb.circuitBreakerOpen = false
		rcb.lastRestartTime = time.Time{} // Reset timestamp
	}
}
