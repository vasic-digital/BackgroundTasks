package background

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"digital.vasic.models"
)

// PostgresTaskQueue implements TaskQueue using PostgreSQL
type PostgresTaskQueue struct {
	repository TaskRepository
	logger     *logrus.Logger
	mu         sync.RWMutex

	// In-memory cache for queue depth (updated periodically)
	cachedDepth    map[models.TaskPriority]int64
	depthCacheTime time.Time
	depthCacheTTL  time.Duration
}

// NewPostgresTaskQueue creates a new PostgreSQL-backed task queue
func NewPostgresTaskQueue(repository TaskRepository, logger *logrus.Logger) *PostgresTaskQueue {
	return &PostgresTaskQueue{
		repository:    repository,
		logger:        logger,
		cachedDepth:   make(map[models.TaskPriority]int64),
		depthCacheTTL: 5 * time.Second,
	}
}

// Enqueue adds a task to the queue
func (q *PostgresTaskQueue) Enqueue(ctx context.Context, task *models.BackgroundTask) error {
	if task == nil {
		return fmt.Errorf("task cannot be nil")
	}

	// Ensure task is in pending status
	if task.Status == "" {
		task.Status = models.TaskStatusPending
	}

	// Set default priority if not specified
	if task.Priority == "" {
		task.Priority = models.TaskPriorityNormal
	}

	// Set scheduled time if not specified
	if task.ScheduledAt.IsZero() {
		task.ScheduledAt = time.Now()
	}

	// Create the task in the database
	if err := q.repository.Create(ctx, task); err != nil {
		return fmt.Errorf("failed to enqueue task: %w", err)
	}

	// Log the event
	if err := q.repository.LogEvent(ctx, task.ID, models.TaskEventCreated, map[string]interface{}{
		"task_type": task.TaskType,
		"task_name": task.TaskName,
		"priority":  task.Priority,
	}, nil); err != nil {
		q.logger.WithError(err).Debug("Failed to log task created event")
	}

	q.logger.WithFields(logrus.Fields{
		"task_id":   task.ID,
		"task_type": task.TaskType,
		"priority":  task.Priority,
	}).Debug("Task enqueued")

	// Invalidate cache
	q.invalidateCache()

	return nil
}

// Dequeue atomically retrieves and claims a task from the queue
func (q *PostgresTaskQueue) Dequeue(ctx context.Context, workerID string, requirements ResourceRequirements) (*models.BackgroundTask, error) {
	maxCPU := requirements.CPUCores
	maxMem := requirements.MemoryMB

	// Use 0 to indicate no limit
	if maxCPU <= 0 {
		maxCPU = 0
	}
	if maxMem <= 0 {
		maxMem = 0
	}

	task, err := q.repository.Dequeue(ctx, workerID, maxCPU, maxMem)
	if err != nil {
		return nil, fmt.Errorf("failed to dequeue task: %w", err)
	}

	if task == nil {
		return nil, nil // No task available
	}

	// Log the event
	if err := q.repository.LogEvent(ctx, task.ID, models.TaskEventStarted, map[string]interface{}{
		"worker_id": workerID,
	}, &workerID); err != nil {
		q.logger.WithError(err).Debug("Failed to log task started event")
	}

	q.logger.WithFields(logrus.Fields{
		"task_id":   task.ID,
		"worker_id": workerID,
		"task_type": task.TaskType,
	}).Debug("Task dequeued")

	// Invalidate cache
	q.invalidateCache()

	return task, nil
}

// Peek returns tasks without claiming them
func (q *PostgresTaskQueue) Peek(ctx context.Context, count int) ([]*models.BackgroundTask, error) {
	if count <= 0 {
		count = 10
	}

	return q.repository.GetPendingTasks(ctx, count)
}

// Requeue returns a task to the queue with optional delay
func (q *PostgresTaskQueue) Requeue(ctx context.Context, taskID string, delay time.Duration) error {
	task, err := q.repository.GetByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	// Update task for requeue
	task.Status = models.TaskStatusPending
	task.WorkerID = nil
	task.StartedAt = nil
	task.LastHeartbeat = nil
	task.RetryCount++

	// Set scheduled time with delay
	if delay > 0 {
		scheduledAt := time.Now().Add(delay)
		task.ScheduledAt = scheduledAt
	} else {
		task.ScheduledAt = time.Now()
	}

	if err := q.repository.Update(ctx, task); err != nil {
		return fmt.Errorf("failed to update task: %w", err)
	}

	// Log the event
	if err := q.repository.LogEvent(ctx, taskID, models.TaskEventRetrying, map[string]interface{}{
		"retry_count":  task.RetryCount,
		"delay_ms":     delay.Milliseconds(),
		"scheduled_at": task.ScheduledAt,
	}, nil); err != nil {
		q.logger.WithError(err).Debug("Failed to log task retrying event")
	}

	q.logger.WithFields(logrus.Fields{
		"task_id":     taskID,
		"retry_count": task.RetryCount,
		"delay":       delay,
	}).Debug("Task requeued")

	// Invalidate cache
	q.invalidateCache()

	return nil
}

// MoveToDeadLetter moves a failed task to dead-letter queue
func (q *PostgresTaskQueue) MoveToDeadLetter(ctx context.Context, taskID string, reason string) error {
	if err := q.repository.MoveToDeadLetter(ctx, taskID, reason); err != nil {
		return fmt.Errorf("failed to move task to dead letter: %w", err)
	}

	q.logger.WithFields(logrus.Fields{
		"task_id": taskID,
		"reason":  reason,
	}).Warn("Task moved to dead letter queue")

	// Invalidate cache
	q.invalidateCache()

	return nil
}

// GetPendingCount returns the number of pending tasks
func (q *PostgresTaskQueue) GetPendingCount(ctx context.Context) (int64, error) {
	counts, err := q.repository.CountByStatus(ctx)
	if err != nil {
		return 0, err
	}

	return counts[models.TaskStatusPending], nil
}

// GetRunningCount returns the number of running tasks
func (q *PostgresTaskQueue) GetRunningCount(ctx context.Context) (int64, error) {
	counts, err := q.repository.CountByStatus(ctx)
	if err != nil {
		return 0, err
	}

	return counts[models.TaskStatusRunning], nil
}

// GetQueueDepth returns counts by priority
func (q *PostgresTaskQueue) GetQueueDepth(ctx context.Context) (map[models.TaskPriority]int64, error) {
	q.mu.RLock()
	if time.Since(q.depthCacheTime) < q.depthCacheTTL && len(q.cachedDepth) > 0 {
		// Return cached value
		result := make(map[models.TaskPriority]int64)
		for k, v := range q.cachedDepth {
			result[k] = v
		}
		q.mu.RUnlock()
		return result, nil
	}
	q.mu.RUnlock()

	// Query pending tasks and count by priority
	tasks, err := q.repository.GetByStatus(ctx, models.TaskStatusPending, 10000, 0)
	if err != nil {
		return nil, err
	}

	counts := make(map[models.TaskPriority]int64)
	for _, task := range tasks {
		counts[task.Priority]++
	}

	// Update cache
	q.mu.Lock()
	q.cachedDepth = counts
	q.depthCacheTime = time.Now()
	q.mu.Unlock()

	return counts, nil
}

// invalidateCache invalidates the queue depth cache
func (q *PostgresTaskQueue) invalidateCache() {
	q.mu.Lock()
	q.depthCacheTime = time.Time{}
	q.mu.Unlock()
}

// TaskQueueStats holds queue statistics
type TaskQueueStats struct {
	PendingCount    int64                         `json:"pending_count"`
	RunningCount    int64                         `json:"running_count"`
	DepthByPriority map[models.TaskPriority]int64 `json:"depth_by_priority"`
	StatusCounts    map[models.TaskStatus]int64   `json:"status_counts"`
}

// GetStats returns queue statistics
func (q *PostgresTaskQueue) GetStats(ctx context.Context) (*TaskQueueStats, error) {
	statusCounts, err := q.repository.CountByStatus(ctx)
	if err != nil {
		return nil, err
	}

	depth, err := q.GetQueueDepth(ctx)
	if err != nil {
		depth = make(map[models.TaskPriority]int64)
	}

	return &TaskQueueStats{
		PendingCount:    statusCounts[models.TaskStatusPending],
		RunningCount:    statusCounts[models.TaskStatusRunning],
		DepthByPriority: depth,
		StatusCounts:    statusCounts,
	}, nil
}

// InMemoryTaskQueue provides an in-memory queue implementation for testing
type InMemoryTaskQueue struct {
	tasks  map[string]*models.BackgroundTask
	queue  []*models.BackgroundTask
	mu     sync.RWMutex
	logger *logrus.Logger
}

// NewInMemoryTaskQueue creates a new in-memory task queue
func NewInMemoryTaskQueue(logger *logrus.Logger) *InMemoryTaskQueue {
	return &InMemoryTaskQueue{
		tasks:  make(map[string]*models.BackgroundTask),
		queue:  make([]*models.BackgroundTask, 0),
		logger: logger,
	}
}

// Enqueue adds a task to the in-memory queue
func (q *InMemoryTaskQueue) Enqueue(ctx context.Context, task *models.BackgroundTask) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if task.ID == "" {
		task.ID = fmt.Sprintf("task-%d", time.Now().UnixNano())
	}
	task.Status = models.TaskStatusPending
	task.CreatedAt = time.Now()
	task.UpdatedAt = time.Now()

	q.tasks[task.ID] = task
	q.queue = append(q.queue, task)

	// Sort by priority
	q.sortQueue()

	return nil
}

// Dequeue retrieves and claims a task
func (q *InMemoryTaskQueue) Dequeue(ctx context.Context, workerID string, requirements ResourceRequirements) (*models.BackgroundTask, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for i, task := range q.queue {
		if task.Status == models.TaskStatusPending && task.ScheduledAt.Before(time.Now()) {
			// Check resource requirements
			if requirements.CPUCores > 0 && task.RequiredCPUCores > requirements.CPUCores {
				continue
			}
			if requirements.MemoryMB > 0 && task.RequiredMemoryMB > requirements.MemoryMB {
				continue
			}

			// Claim the task
			task.Status = models.TaskStatusRunning
			task.WorkerID = &workerID
			now := time.Now()
			task.StartedAt = &now
			task.LastHeartbeat = &now
			task.UpdatedAt = now

			// Remove from queue
			q.queue = append(q.queue[:i], q.queue[i+1:]...)

			return task, nil
		}
	}

	return nil, nil
}

// Peek returns tasks without claiming them
func (q *InMemoryTaskQueue) Peek(ctx context.Context, count int) ([]*models.BackgroundTask, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	result := make([]*models.BackgroundTask, 0, count)
	for i, task := range q.queue {
		if i >= count {
			break
		}
		if task.Status == models.TaskStatusPending {
			result = append(result, task)
		}
	}

	return result, nil
}

// Requeue returns a task to the queue
func (q *InMemoryTaskQueue) Requeue(ctx context.Context, taskID string, delay time.Duration) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	task, exists := q.tasks[taskID]
	if !exists {
		return fmt.Errorf("task not found: %s", taskID)
	}

	task.Status = models.TaskStatusPending
	task.WorkerID = nil
	task.StartedAt = nil
	task.RetryCount++
	task.ScheduledAt = time.Now().Add(delay)
	task.UpdatedAt = time.Now()

	q.queue = append(q.queue, task)
	q.sortQueue()

	return nil
}

// MoveToDeadLetter moves a task to dead letter
func (q *InMemoryTaskQueue) MoveToDeadLetter(ctx context.Context, taskID string, reason string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	task, exists := q.tasks[taskID]
	if !exists {
		return fmt.Errorf("task not found: %s", taskID)
	}

	task.Status = models.TaskStatusDeadLetter
	task.LastError = &reason
	task.UpdatedAt = time.Now()

	return nil
}

// GetPendingCount returns pending task count
func (q *InMemoryTaskQueue) GetPendingCount(ctx context.Context) (int64, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	var count int64
	for _, task := range q.tasks {
		if task.Status == models.TaskStatusPending {
			count++
		}
	}
	return count, nil
}

// GetRunningCount returns running task count
func (q *InMemoryTaskQueue) GetRunningCount(ctx context.Context) (int64, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	var count int64
	for _, task := range q.tasks {
		if task.Status == models.TaskStatusRunning {
			count++
		}
	}
	return count, nil
}

// GetQueueDepth returns counts by priority
func (q *InMemoryTaskQueue) GetQueueDepth(ctx context.Context) (map[models.TaskPriority]int64, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	counts := make(map[models.TaskPriority]int64)
	for _, task := range q.tasks {
		if task.Status == models.TaskStatusPending {
			counts[task.Priority]++
		}
	}
	return counts, nil
}

// sortQueue sorts the queue by priority and creation time
func (q *InMemoryTaskQueue) sortQueue() {
	// Sort by priority weight, then by creation time
	for i := 0; i < len(q.queue)-1; i++ {
		for j := i + 1; j < len(q.queue); j++ {
			if q.shouldSwap(q.queue[i], q.queue[j]) {
				q.queue[i], q.queue[j] = q.queue[j], q.queue[i]
			}
		}
	}
}

func (q *InMemoryTaskQueue) shouldSwap(a, b *models.BackgroundTask) bool {
	// Higher priority (lower weight) should come first
	if a.Priority.Weight() != b.Priority.Weight() {
		return a.Priority.Weight() > b.Priority.Weight()
	}
	// Earlier created should come first
	return a.CreatedAt.After(b.CreatedAt)
}

// GetTask returns a task by ID (for testing)
func (q *InMemoryTaskQueue) GetTask(taskID string) *models.BackgroundTask {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.tasks[taskID]
}

// UpdateTask updates a task (for testing)
func (q *InMemoryTaskQueue) UpdateTask(task *models.BackgroundTask) {
	q.mu.Lock()
	defer q.mu.Unlock()
	task.UpdatedAt = time.Now()
	q.tasks[task.ID] = task
}

// MarshalJSON for TaskQueueStats
func (s *TaskQueueStats) MarshalJSON() ([]byte, error) {
	type Alias TaskQueueStats
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(s),
	})
}
