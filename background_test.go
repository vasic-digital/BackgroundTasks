package background

import (
	"context"
	"io"
	"testing"
	"time"

	"digital.vasic.models"
	"github.com/sirupsen/logrus"
)

// mockTaskExecutor is a simple executor that returns success
type mockTaskExecutor struct{}

func (m *mockTaskExecutor) Execute(ctx context.Context, task *models.BackgroundTask, reporter ProgressReporter) error {
	return nil
}

func (m *mockTaskExecutor) CanPause() bool {
	return false
}

func (m *mockTaskExecutor) Pause(ctx context.Context, task *models.BackgroundTask) ([]byte, error) {
	return nil, nil
}

func (m *mockTaskExecutor) Resume(ctx context.Context, task *models.BackgroundTask, checkpoint []byte) error {
	return nil
}

func (m *mockTaskExecutor) Cancel(ctx context.Context, task *models.BackgroundTask) error {
	return nil
}

func (m *mockTaskExecutor) GetResourceRequirements() ResourceRequirements {
	return ResourceRequirements{}
}

// newTestLogger creates a logger that discards output
func newTestLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	logger.SetLevel(logrus.ErrorLevel)
	return logger
}

func TestNewAdaptiveWorkerPool(t *testing.T) {
	// Create in-memory task queue
	logger := newTestLogger()
	queue := NewInMemoryTaskQueue(logger)

	// Create worker pool config
	config := &WorkerPoolConfig{
		MinWorkers:            1,
		MaxWorkers:            2,
		ScaleUpThreshold:      0.8,
		ScaleDownThreshold:    0.2,
		ScaleInterval:         time.Second,
		WorkerIdleTimeout:     time.Minute,
		QueuePollInterval:     100 * time.Millisecond,
		HeartbeatInterval:     time.Second,
		ResourceCheckInterval: 5 * time.Second,
		MaxCPUPercent:         80.0,
		MaxMemoryPercent:      80.0,
		GracefulShutdownTime:  10 * time.Second,
	}

	// Create worker pool
	pool := NewAdaptiveWorkerPool(
		config,
		queue,
		nil, // repository
		nil, // resource monitor
		nil, // stuck detector
		nil, // notifier
		logger,
	)

	// Register a mock executor
	executor := &mockTaskExecutor{}
	pool.RegisterExecutor("mock", executor)

	// Start the pool
	ctx := context.Background()
	err := pool.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start pool: %v", err)
	}

	// Give pool time to start workers
	time.Sleep(100 * time.Millisecond)

	// Check worker count
	count := pool.GetWorkerCount()
	if count != config.MinWorkers {
		t.Errorf("Expected %d workers, got %d", config.MinWorkers, count)
	}

	// Stop the pool
	err = pool.Stop(5 * time.Second)
	if err != nil {
		t.Fatalf("Failed to stop pool: %v", err)
	}

	// Wait for stop
	time.Sleep(100 * time.Millisecond)
}

func TestInMemoryTaskQueue(t *testing.T) {
	logger := newTestLogger()
	queue := NewInMemoryTaskQueue(logger)

	task := &models.BackgroundTask{
		ID:        "test-task",
		TaskType:  "mock",
		Status:    models.TaskStatusPending,
		CreatedAt: time.Now(),
	}

	// Enqueue
	err := queue.Enqueue(context.Background(), task)
	if err != nil {
		t.Fatalf("Failed to enqueue: %v", err)
	}

	// Dequeue with worker ID and requirements
	dequeued, err := queue.Dequeue(context.Background(), "test-worker", ResourceRequirements{})
	if err != nil {
		t.Fatalf("Failed to dequeue: %v", err)
	}
	if dequeued == nil {
		t.Fatal("Expected dequeued task")
	}
	if dequeued.ID != task.ID {
		t.Errorf("Expected task ID %s, got %s", task.ID, dequeued.ID)
	}
	// Check that task status is now running
	if dequeued.Status != models.TaskStatusRunning {
		t.Errorf("Expected task status %s, got %s", models.TaskStatusRunning, dequeued.Status)
	}

	// Requeue the task (put it back to pending)
	err = queue.Requeue(context.Background(), task.ID, 0)
	if err != nil {
		t.Fatalf("Failed to requeue: %v", err)
	}

	// Peek to see if task is pending again
	tasks, err := queue.Peek(context.Background(), 10)
	if err != nil {
		t.Fatalf("Failed to peek: %v", err)
	}
	found := false
	for _, t := range tasks {
		if t.ID == task.ID && t.Status == models.TaskStatusPending {
			found = true
			break
		}
	}
	if !found {
		t.Error("Task not found in pending queue after requeue")
	}
}
