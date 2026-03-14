package background

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"digital.vasic.models"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTaskRepository implements TaskRepository for testing
type mockTaskRepository struct {
	mu           sync.RWMutex
	tasks        map[string]*models.BackgroundTask
	events       []*models.TaskExecutionHistory
	snapshots    map[string][]*models.ResourceSnapshot
	checkpoints  map[string][]byte
	updateCalled int
}

func newMockTaskRepository() *mockTaskRepository {
	return &mockTaskRepository{
		tasks:       make(map[string]*models.BackgroundTask),
		snapshots:   make(map[string][]*models.ResourceSnapshot),
		checkpoints: make(map[string][]byte),
	}
}

func (m *mockTaskRepository) Create(ctx context.Context, task *models.BackgroundTask) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[task.ID] = task
	return nil
}

func (m *mockTaskRepository) GetByID(ctx context.Context, id string) (*models.BackgroundTask, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	task, ok := m.tasks[id]
	if !ok {
		return nil, nil
	}
	return task, nil
}

func (m *mockTaskRepository) Update(ctx context.Context, task *models.BackgroundTask) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[task.ID] = task
	m.updateCalled++
	return nil
}

func (m *mockTaskRepository) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tasks, id)
	return nil
}

func (m *mockTaskRepository) UpdateStatus(ctx context.Context, id string, status models.TaskStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if task, ok := m.tasks[id]; ok {
		task.Status = status
	}
	return nil
}

func (m *mockTaskRepository) UpdateProgress(ctx context.Context, id string, progress float64, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if task, ok := m.tasks[id]; ok {
		task.Progress = progress
		if message != "" {
			task.ProgressMessage = &message
		}
	}
	return nil
}

func (m *mockTaskRepository) UpdateHeartbeat(ctx context.Context, id string) error {
	// Simulate heartbeat update
	return nil
}

func (m *mockTaskRepository) SaveCheckpoint(ctx context.Context, id string, checkpoint []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkpoints[id] = checkpoint
	return nil
}

func (m *mockTaskRepository) GetByStatus(ctx context.Context, status models.TaskStatus, limit, offset int) ([]*models.BackgroundTask, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*models.BackgroundTask
	for _, task := range m.tasks {
		if task.Status == status {
			result = append(result, task)
		}
	}
	return result, nil
}

func (m *mockTaskRepository) GetPendingTasks(ctx context.Context, limit int) ([]*models.BackgroundTask, error) {
	return m.GetByStatus(ctx, models.TaskStatusPending, limit, 0)
}

func (m *mockTaskRepository) GetStaleTasks(ctx context.Context, threshold time.Duration) ([]*models.BackgroundTask, error) {
	// Simple implementation: return empty slice
	return []*models.BackgroundTask{}, nil
}

func (m *mockTaskRepository) GetByWorkerID(ctx context.Context, workerID string) ([]*models.BackgroundTask, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*models.BackgroundTask
	for _, task := range m.tasks {
		if task.WorkerID != nil && *task.WorkerID == workerID {
			result = append(result, task)
		}
	}
	return result, nil
}

func (m *mockTaskRepository) CountByStatus(ctx context.Context) (map[models.TaskStatus]int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	counts := make(map[models.TaskStatus]int64)
	for _, task := range m.tasks {
		counts[task.Status]++
	}
	return counts, nil
}

func (m *mockTaskRepository) Dequeue(ctx context.Context, workerID string, maxCPUCores, maxMemoryMB int) (*models.BackgroundTask, error) {
	// Not needed for tests using InMemoryTaskQueue
	return nil, nil
}

func (m *mockTaskRepository) SaveResourceSnapshot(ctx context.Context, snapshot *models.ResourceSnapshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.snapshots[snapshot.TaskID] = append(m.snapshots[snapshot.TaskID], snapshot)
	return nil
}

func (m *mockTaskRepository) GetResourceSnapshots(ctx context.Context, taskID string, limit int) ([]*models.ResourceSnapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	snapshots, ok := m.snapshots[taskID]
	if !ok {
		return []*models.ResourceSnapshot{}, nil
	}
	if limit > 0 && limit < len(snapshots) {
		return snapshots[:limit], nil
	}
	return snapshots, nil
}

func (m *mockTaskRepository) LogEvent(ctx context.Context, taskID, eventType string, data map[string]interface{}, workerID *string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Convert data to JSON raw message
	jsonData, _ := json.Marshal(data)
	m.events = append(m.events, &models.TaskExecutionHistory{
		ID:        fmt.Sprintf("event-%d", len(m.events)+1),
		TaskID:    taskID,
		EventType: eventType,
		EventData: json.RawMessage(jsonData),
		WorkerID:  workerID,
		CreatedAt: time.Now(),
	})
	return nil
}

func (m *mockTaskRepository) GetTaskHistory(ctx context.Context, taskID string, limit int) ([]*models.TaskExecutionHistory, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*models.TaskExecutionHistory
	for _, event := range m.events {
		if event.TaskID == taskID {
			result = append(result, event)
		}
	}
	return result, nil
}

func (m *mockTaskRepository) MoveToDeadLetter(ctx context.Context, taskID, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if task, ok := m.tasks[taskID]; ok {
		task.Status = models.TaskStatusDeadLetter
	}
	return nil
}

// mockResourceMonitor implements ResourceMonitor for testing
type mockResourceMonitor struct {
	systemResources *SystemResources
	monitoringTasks map[string]int
	mu              sync.RWMutex
}

func newMockResourceMonitor() *mockResourceMonitor {
	return &mockResourceMonitor{
		systemResources: &SystemResources{
			TotalCPUCores:     8,
			AvailableCPUCores: 4.0,
			TotalMemoryMB:     16384,
			AvailableMemoryMB: 8192,
			CPULoadPercent:    50.0,
			MemoryUsedPercent: 50.0,
			DiskUsedPercent:   30.0,
			LoadAvg1:          1.5,
			LoadAvg5:          1.2,
			LoadAvg15:         1.0,
		},
		monitoringTasks: make(map[string]int),
	}
}

func (m *mockResourceMonitor) GetSystemResources() (*SystemResources, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.systemResources, nil
}

func (m *mockResourceMonitor) GetProcessResources(pid int) (*models.ResourceSnapshot, error) {
	return &models.ResourceSnapshot{
		ID:             "snapshot-1",
		TaskID:         "test-task",
		SampledAt:      time.Now(),
		CPUPercent:     10.0,
		MemoryRSSBytes: 1024,
		MemoryVMSBytes: 2048,
		MemoryPercent:  5.0,
		IOReadBytes:    100,
		IOWriteBytes:   50,
		NetBytesSent:   200,
		NetBytesRecv:   300,
		NetConnections: 5,
		OpenFiles:      10,
		OpenFDs:        20,
		ProcessState:   "running",
		ThreadCount:    1,
	}, nil
}

func (m *mockResourceMonitor) StartMonitoring(taskID string, pid int, interval time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.monitoringTasks[taskID] = pid
	return nil
}

func (m *mockResourceMonitor) StopMonitoring(taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.monitoringTasks, taskID)
	return nil
}

func (m *mockResourceMonitor) GetLatestSnapshot(taskID string) (*models.ResourceSnapshot, error) {
	return &models.ResourceSnapshot{
		ID:             "latest-snapshot",
		TaskID:         taskID,
		SampledAt:      time.Now(),
		CPUPercent:     5.0,
		MemoryRSSBytes: 512,
		MemoryVMSBytes: 1024,
		MemoryPercent:  2.5,
		IOReadBytes:    50,
		IOWriteBytes:   25,
		NetBytesSent:   100,
		NetBytesRecv:   150,
		NetConnections: 3,
		OpenFiles:      5,
		OpenFDs:        10,
		ProcessState:   "running",
		ThreadCount:    1,
	}, nil
}

func (m *mockResourceMonitor) IsResourceAvailable(requirements ResourceRequirements) bool {
	return true
}

// mockStuckDetector implements StuckDetector for testing
type mockStuckDetector struct {
	thresholds map[string]time.Duration
}

func newMockStuckDetector() *mockStuckDetector {
	return &mockStuckDetector{
		thresholds: make(map[string]time.Duration),
	}
}

func (m *mockStuckDetector) IsStuck(ctx context.Context, task *models.BackgroundTask, snapshots []*models.ResourceSnapshot) (bool, string) {
	// Always return false for tests
	return false, ""
}

func (m *mockStuckDetector) GetStuckThreshold(taskType string) time.Duration {
	if threshold, ok := m.thresholds[taskType]; ok {
		return threshold
	}
	return 5 * time.Minute
}

func (m *mockStuckDetector) SetThreshold(taskType string, threshold time.Duration) {
	m.thresholds[taskType] = threshold
}

// mockNotificationService implements NotificationService for testing
type mockNotificationService struct {
	events []TaskEvent
	mu     sync.RWMutex
}

func newMockNotificationService() *mockNotificationService {
	return &mockNotificationService{
		events: make([]TaskEvent, 0),
	}
}

func (m *mockNotificationService) NotifyTaskEvent(ctx context.Context, task *models.BackgroundTask, event string, data map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, TaskEvent{
		TaskID:    task.ID,
		EventType: event,
		Timestamp: time.Now(),
		Data:      data,
	})
	return nil
}

func (m *mockNotificationService) RegisterSSEClient(ctx context.Context, taskID string, client chan<- []byte) error {
	return nil
}

func (m *mockNotificationService) UnregisterSSEClient(ctx context.Context, taskID string, client chan<- []byte) error {
	return nil
}

func (m *mockNotificationService) RegisterWebSocketClient(ctx context.Context, taskID string, client WebSocketClient) error {
	return nil
}

func (m *mockNotificationService) BroadcastToTask(ctx context.Context, taskID string, message []byte) error {
	return nil
}

// mockTaskExecutorWithBehavior is a simple executor that returns success
type mockTaskExecutorWithBehavior struct {
	executeCalled int
	executeDelay  time.Duration
	shouldFail    bool
}

func (m *mockTaskExecutorWithBehavior) Execute(ctx context.Context, task *models.BackgroundTask, reporter ProgressReporter) error {
	m.executeCalled++
	if m.executeDelay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.executeDelay):
		}
	}
	if m.shouldFail {
		return assert.AnError
	}
	// Report some progress
	reporter.ReportProgress(50.0, "Halfway there")
	reporter.ReportProgress(100.0, "Completed")
	return nil
}

func (m *mockTaskExecutorWithBehavior) CanPause() bool {
	return false
}

func (m *mockTaskExecutorWithBehavior) Pause(ctx context.Context, task *models.BackgroundTask) ([]byte, error) {
	return nil, nil
}

func (m *mockTaskExecutorWithBehavior) Resume(ctx context.Context, task *models.BackgroundTask, checkpoint []byte) error {
	return nil
}

func (m *mockTaskExecutorWithBehavior) Cancel(ctx context.Context, task *models.BackgroundTask) error {
	return nil
}

func (m *mockTaskExecutorWithBehavior) GetResourceRequirements() ResourceRequirements {
	return ResourceRequirements{
		CPUCores: 1,
		MemoryMB: 256,
	}
}

// testLogger creates a logger that discards output
func testLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	logger.SetLevel(logrus.ErrorLevel)
	return logger
}

func TestNewAdaptiveWorkerPool_Integration(t *testing.T) {
	logger := testLogger()
	queue := NewInMemoryTaskQueue(logger)
	repo := newMockTaskRepository()
	resourceMonitor := newMockResourceMonitor()
	stuckDetector := newMockStuckDetector()
	notifier := newMockNotificationService()

	config := &WorkerPoolConfig{
		MinWorkers:            2,
		MaxWorkers:            4,
		ScaleUpThreshold:      0.8,
		ScaleDownThreshold:    0.2,
		ScaleInterval:         100 * time.Millisecond,
		WorkerIdleTimeout:     time.Minute,
		QueuePollInterval:     50 * time.Millisecond,
		HeartbeatInterval:     100 * time.Millisecond,
		ResourceCheckInterval: 5 * time.Second,
		MaxCPUPercent:         80.0,
		MaxMemoryPercent:      80.0,
		GracefulShutdownTime:  5 * time.Second,
	}

	pool := NewAdaptiveWorkerPool(
		config,
		queue,
		repo,
		resourceMonitor,
		stuckDetector,
		notifier,
		logger,
	)

	// Register executor
	executor := &mockTaskExecutorWithBehavior{}
	pool.RegisterExecutor("test-task", executor)

	// Start the pool
	ctx := context.Background()
	err := pool.Start(ctx)
	require.NoError(t, err)

	// Wait for workers to start
	time.Sleep(200 * time.Millisecond)

	// Verify worker count
	workerCount := pool.GetWorkerCount()
	assert.Equal(t, config.MinWorkers, workerCount)

	// Create a task
	task := &models.BackgroundTask{
		ID:       "test-task-1",
		TaskType: "test-task",
		Status:   models.TaskStatusPending,
		Config: models.TaskConfig{
			TimeoutSeconds: 30,
			CaptureOutput:  true,
		},
		CreatedAt: time.Now(),
	}

	// Enqueue the task
	err = queue.Enqueue(ctx, task)
	require.NoError(t, err)

	// Wait for task to be processed
	time.Sleep(500 * time.Millisecond)

	// Verify task was processed
	assert.Greater(t, executor.executeCalled, 0)

	// Get worker status
	statuses := pool.GetWorkerStatus()
	assert.NotEmpty(t, statuses)

	// Verify active task count
	activeCount := pool.GetActiveTaskCount()
	assert.Equal(t, 0, activeCount) // Task should be completed

	// Scale workers
	err = pool.Scale(3)
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 3, pool.GetWorkerCount())

	// Stop the pool
	err = pool.Stop(2 * time.Second)
	require.NoError(t, err)
}

func TestDefaultWorkerPoolConfig(t *testing.T) {
	config := DefaultWorkerPoolConfig()
	assert.NotNil(t, config)
	assert.Greater(t, config.MinWorkers, 0)
	assert.GreaterOrEqual(t, config.MaxWorkers, config.MinWorkers)
	assert.Greater(t, config.ScaleInterval, time.Duration(0))
	assert.Greater(t, config.QueuePollInterval, time.Duration(0))
	assert.Greater(t, config.HeartbeatInterval, time.Duration(0))
}

func TestWorkerState_String(t *testing.T) {
	var ws workerState
	ws = workerStateIdle
	assert.Equal(t, "idle", ws.String())
	ws = workerStateBusy
	assert.Equal(t, "busy", ws.String())
	ws = workerStateStopping
	assert.Equal(t, "stopping", ws.String())
	ws = workerStateStopped
	assert.Equal(t, "stopped", ws.String())
	ws = workerState(99)
	assert.Equal(t, "unknown", ws.String())
}

func TestWorker_StatusMethods(t *testing.T) {
	pool := &AdaptiveWorkerPool{
		config: DefaultWorkerPoolConfig(),
		logger: testLogger(),
	}
	worker := &Worker{
		ID:        "test-worker",
		pool:      pool,
		status:    int32(workerStateIdle),
		StartedAt: time.Now(),
	}

	// Test status getter/setter
	assert.Equal(t, workerStateIdle, worker.Status())
	worker.setStatus(workerStateBusy)
	assert.Equal(t, workerStateBusy, worker.Status())

	// Test current task getter/setter
	task := &models.BackgroundTask{ID: "test-task"}
	worker.setCurrentTask(task)
	assert.Equal(t, task, worker.CurrentTask())
	worker.setCurrentTask(nil)
	assert.Nil(t, worker.CurrentTask())

	// Test last activity getter/setter
	now := time.Now()
	worker.setLastActivity(now)
	assert.Equal(t, now, worker.LastActivity())
}

func TestWorkerPool_Scale(t *testing.T) {
	logger := testLogger()
	queue := NewInMemoryTaskQueue(logger)
	pool := NewAdaptiveWorkerPool(
		&WorkerPoolConfig{
			MinWorkers:            2,
			MaxWorkers:            5,
			ScaleInterval:         time.Second,
			WorkerIdleTimeout:     time.Minute,
			QueuePollInterval:     100 * time.Millisecond,
			HeartbeatInterval:     100 * time.Millisecond,
			ResourceCheckInterval: 5 * time.Second,
			MaxCPUPercent:         80.0,
			MaxMemoryPercent:      80.0,
			GracefulShutdownTime:  5 * time.Second,
		},
		queue,
		nil, nil, nil, nil,
		logger,
	)

	// Start pool
	ctx := context.Background()
	err := pool.Start(ctx)
	require.NoError(t, err)
	defer pool.Stop(time.Second)

	// Wait for workers
	time.Sleep(200 * time.Millisecond)
	initialCount := pool.GetWorkerCount()
	assert.Equal(t, 2, initialCount)

	// Scale up
	err = pool.Scale(4)
	require.NoError(t, err)
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, 4, pool.GetWorkerCount())

	// Scale beyond max should be capped
	err = pool.Scale(10)
	require.NoError(t, err)
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, 5, pool.GetWorkerCount()) // Max is 5

	// Scale below min should be capped
	err = pool.Scale(0)
	require.NoError(t, err)
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, 2, pool.GetWorkerCount()) // Min is 2
}

func TestTaskQueue_EnqueueDequeue(t *testing.T) {
	logger := testLogger()
	queue := NewInMemoryTaskQueue(logger)

	task := &models.BackgroundTask{
		ID:       "test-task",
		TaskType: "test",
		Status:   models.TaskStatusPending,
	}

	// Enqueue
	err := queue.Enqueue(context.Background(), task)
	require.NoError(t, err)

	// Dequeue
	dequeued, err := queue.Dequeue(context.Background(), "worker-1", ResourceRequirements{})
	require.NoError(t, err)
	require.NotNil(t, dequeued)
	assert.Equal(t, task.ID, dequeued.ID)
	assert.Equal(t, models.TaskStatusRunning, dequeued.Status)

	// Get pending count
	count, err := queue.GetPendingCount(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(0), count) // Task is now running

	// Get running count
	running, err := queue.GetRunningCount(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1), running)

	// Requeue
	err = queue.Requeue(context.Background(), task.ID, 0)
	require.NoError(t, err)

	// Now pending count should be 1
	count, err = queue.GetPendingCount(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}

func TestTaskQueue_PriorityOrdering(t *testing.T) {
	logger := testLogger()
	queue := NewInMemoryTaskQueue(logger)

	// Create tasks with different priorities
	lowTask := &models.BackgroundTask{
		ID:       "low",
		TaskType: "test",
		Status:   models.TaskStatusPending,
		Priority: models.TaskPriorityLow,
	}
	highTask := &models.BackgroundTask{
		ID:       "high",
		TaskType: "test",
		Status:   models.TaskStatusPending,
		Priority: models.TaskPriorityHigh,
	}

	// Enqueue low first, then high
	queue.Enqueue(context.Background(), lowTask)
	queue.Enqueue(context.Background(), highTask)

	// Dequeue should get high priority first
	dequeued, err := queue.Dequeue(context.Background(), "worker-1", ResourceRequirements{})
	require.NoError(t, err)
	assert.Equal(t, "high", dequeued.ID)

	// Next dequeue should get low
	dequeued, err = queue.Dequeue(context.Background(), "worker-1", ResourceRequirements{})
	require.NoError(t, err)
	assert.Equal(t, "low", dequeued.ID)
}
