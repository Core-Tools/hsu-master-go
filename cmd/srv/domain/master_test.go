package domain

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockWorker is a mock implementation of Worker for testing
type MockWorker struct {
	mock.Mock
}

func (m *MockWorker) ID() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockWorker) Metadata() UnitMetadata {
	args := m.Called()
	return args.Get(0).(UnitMetadata)
}

func (m *MockWorker) ProcessControlOptions() ProcessControlOptions {
	args := m.Called()
	return args.Get(0).(ProcessControlOptions)
}

// MockLogger is a mock implementation of Logger for testing
type MockLogger struct {
	mock.Mock
}

func (m *MockLogger) LogLevelf(level int, format string, args ...interface{}) {
	m.Called(format, args)
}

func (m *MockLogger) Debugf(format string, args ...interface{}) {
	m.Called(format, args)
}

func (m *MockLogger) Infof(format string, args ...interface{}) {
	m.Called(format, args)
}

func (m *MockLogger) Warnf(format string, args ...interface{}) {
	m.Called(format, args)
}

func (m *MockLogger) Errorf(format string, args ...interface{}) {
	m.Called(format, args)
}

func createTestMaster(t *testing.T) *Master {
	logger := &MockLogger{}
	logger.On("Debugf", mock.Anything, mock.Anything).Maybe()
	logger.On("Infof", mock.Anything, mock.Anything).Maybe()
	logger.On("Warnf", mock.Anything, mock.Anything).Maybe()
	logger.On("Errorf", mock.Anything, mock.Anything).Maybe()

	return &Master{
		logger:   logger,
		controls: make(map[string]ProcessControl),
	}
}

func createTestWorker(id string) *MockWorker {
	// Use OS-dependent path for PID file
	var pidFile string
	if runtime.GOOS == "windows" {
		pidFile = fmt.Sprintf("C:\\temp\\%s.pid", id)
	} else {
		pidFile = fmt.Sprintf("/tmp/%s.pid", id)
	}

	worker := &MockWorker{}
	worker.On("ID").Return(id)
	worker.On("Metadata").Return(UnitMetadata{
		Name:        id,
		Description: fmt.Sprintf("Test worker %s", id),
	}).Maybe() // Make this optional since AddWorker doesn't call Metadata()

	// Create a mock logger for the test
	mockLogger := &MockLogger{}
	mockLogger.On("Debugf", mock.Anything, mock.Anything).Maybe()
	mockLogger.On("Infof", mock.Anything, mock.Anything).Maybe()
	mockLogger.On("Warnf", mock.Anything, mock.Anything).Maybe()
	mockLogger.On("Errorf", mock.Anything, mock.Anything).Maybe()

	worker.On("ProcessControlOptions").Return(ProcessControlOptions{
		CanAttach:    true,
		CanTerminate: true,
		CanRestart:   true,
		Discovery: DiscoveryConfig{
			Method:  DiscoveryMethodPIDFile,
			PIDFile: pidFile,
		},
		AttachCmd: NewStdAttachCmd(nil, mockLogger, id), // Add AttachCmd with logger and worker ID
	})
	return worker
}

func TestMaster_AddWorker(t *testing.T) {
	t.Run("valid_worker", func(t *testing.T) {
		master := createTestMaster(t)
		worker := createTestWorker("test-worker-1")

		err := master.AddWorker(worker)

		assert.NoError(t, err)
		assert.Equal(t, 1, len(master.controls))
		worker.AssertExpectations(t)
	})

	t.Run("nil_worker", func(t *testing.T) {
		master := createTestMaster(t)
		err := master.AddWorker(nil)

		assert.Error(t, err)
		assert.True(t, IsValidationError(err))
	})

	t.Run("duplicate_worker", func(t *testing.T) {
		master := createTestMaster(t)
		worker1 := createTestWorker("test-worker-1")
		worker2 := createTestWorker("test-worker-1")

		err1 := master.AddWorker(worker1)
		err2 := master.AddWorker(worker2)

		assert.NoError(t, err1)
		require.Error(t, err2)
		assert.True(t, IsConflictError(err2), "Expected ConflictError but got: %v", err2)
		assert.Equal(t, 1, len(master.controls))

		// Clean up mock expectations
		worker1.AssertExpectations(t)
		worker2.AssertExpectations(t)
	})

	t.Run("invalid_worker_id", func(t *testing.T) {
		master := createTestMaster(t)
		worker := &MockWorker{}
		worker.On("ID").Return("") // Empty ID
		// Don't set up ProcessControlOptions expectation since validation fails before it's called

		err := master.AddWorker(worker)

		assert.Error(t, err)
		assert.True(t, IsValidationError(err))
		worker.AssertExpectations(t)
	})
}

func TestMaster_RemoveWorker(t *testing.T) {
	t.Run("existing_worker", func(t *testing.T) {
		master := createTestMaster(t)
		worker := createTestWorker("test-worker-1")

		// Add worker first
		err := master.AddWorker(worker)
		require.NoError(t, err)

		// Remove worker
		err = master.RemoveWorker("test-worker-1")

		assert.NoError(t, err)
		assert.Equal(t, 0, len(master.controls))
	})

	t.Run("nonexistent_worker", func(t *testing.T) {
		master := createTestMaster(t)
		err := master.RemoveWorker("nonexistent-worker")

		assert.Error(t, err)
		assert.True(t, IsNotFoundError(err))
	})

	t.Run("invalid_worker_id", func(t *testing.T) {
		master := createTestMaster(t)
		err := master.RemoveWorker("")

		assert.Error(t, err)
		assert.True(t, IsValidationError(err))
	})
}

func TestMaster_StartWorker(t *testing.T) {
	t.Run("nil_context", func(t *testing.T) {
		master := createTestMaster(t)
		err := master.StartWorker(nil, "test-worker-1")

		assert.Error(t, err)
		assert.True(t, IsValidationError(err))
	})

	t.Run("invalid_worker_id", func(t *testing.T) {
		master := createTestMaster(t)
		ctx := context.Background()
		err := master.StartWorker(ctx, "")

		assert.Error(t, err)
		assert.True(t, IsValidationError(err))
	})

	t.Run("nonexistent_worker", func(t *testing.T) {
		master := createTestMaster(t)
		ctx := context.Background()
		err := master.StartWorker(ctx, "nonexistent-worker")

		assert.Error(t, err)
		assert.True(t, IsNotFoundError(err))
	})
}

func TestMaster_StopWorker(t *testing.T) {
	t.Run("nil_context", func(t *testing.T) {
		master := createTestMaster(t)
		err := master.StopWorker(nil, "test-worker-1")

		assert.Error(t, err)
		assert.True(t, IsValidationError(err))
	})

	t.Run("invalid_worker_id", func(t *testing.T) {
		master := createTestMaster(t)
		ctx := context.Background()
		err := master.StopWorker(ctx, "")

		assert.Error(t, err)
		assert.True(t, IsValidationError(err))
	})

	t.Run("nonexistent_worker", func(t *testing.T) {
		master := createTestMaster(t)
		ctx := context.Background()
		err := master.StopWorker(ctx, "nonexistent-worker")

		assert.Error(t, err)
		assert.True(t, IsNotFoundError(err))
	})
}

func TestMaster_GetControl(t *testing.T) {
	master := createTestMaster(t)

	// Add a worker
	worker := createTestWorker("test-worker-1")
	err := master.AddWorker(worker)
	require.NoError(t, err)

	// Test getControl
	control, exists := master.getControl("test-worker-1")
	assert.True(t, exists)
	assert.NotNil(t, control)

	// Test nonexistent worker
	control, exists = master.getControl("nonexistent-worker")
	assert.False(t, exists)
	assert.Nil(t, control)
}

func TestMaster_GetAllControls(t *testing.T) {
	master := createTestMaster(t)

	// Initially empty
	controls := master.getAllControls()
	assert.Equal(t, 0, len(controls))

	// Add some workers
	worker1 := createTestWorker("worker-1")
	worker2 := createTestWorker("worker-2")

	err := master.AddWorker(worker1)
	require.NoError(t, err)
	err = master.AddWorker(worker2)
	require.NoError(t, err)

	// Get all controls
	controls = master.getAllControls()
	assert.Equal(t, 2, len(controls))
	assert.Contains(t, controls, "worker-1")
	assert.Contains(t, controls, "worker-2")

	// Verify it's a copy (modifications don't affect original)
	delete(controls, "worker-1")
	originalControls := master.getAllControls()
	assert.Equal(t, 2, len(originalControls))
}

func TestMaster_ConcurrentOperations(t *testing.T) {
	master := createTestMaster(t)

	// Test concurrent AddWorker operations
	done := make(chan bool)
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		go func(id int) {
			worker := createTestWorker(fmt.Sprintf("worker-%d", id))
			err := master.AddWorker(worker)
			errors <- err
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Check that all workers were added successfully
	errorCount := 0
	for i := 0; i < 10; i++ {
		if err := <-errors; err != nil {
			errorCount++
		}
	}

	assert.Equal(t, 0, errorCount, "No errors should occur during concurrent AddWorker operations")
	assert.Equal(t, 10, len(master.controls))
}

func TestMaster_ContextCancellation(t *testing.T) {
	master := createTestMaster(t)

	// Create a context that we can cancel
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel the context immediately
	cancel()

	// Operations with cancelled context should handle it gracefully
	err := master.StartWorker(ctx, "test-worker")
	assert.Error(t, err)
	// The actual error depends on whether the worker exists or not
	// If worker doesn't exist, we get NotFoundError before checking context
	assert.True(t, IsNotFoundError(err) || IsCancelledError(err))
}

func TestMaster_ContextTimeout(t *testing.T) {
	master := createTestMaster(t)

	// Create a context with a very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Wait for context to timeout
	time.Sleep(2 * time.Millisecond)

	// Operations with timed out context should handle it gracefully
	err := master.StartWorker(ctx, "test-worker")
	assert.Error(t, err)
	// The actual error depends on whether the worker exists or not
	assert.True(t, IsNotFoundError(err) || IsCancelledError(err))
}
