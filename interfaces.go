package background

import (
	"context"
	"time"

	"digital.vasic.models"
)

// TaskExecutor defines the interface for task execution
type TaskExecutor interface {
	// Execute runs the task with context and progress reporting
	Execute(ctx context.Context, task *models.BackgroundTask, reporter ProgressReporter) error

	// CanPause returns whether this task type supports pause/resume
	CanPause() bool

	// Pause saves checkpoint for later resume
	Pause(ctx context.Context, task *models.BackgroundTask) ([]byte, error)

	// Resume restores from checkpoint
	Resume(ctx context.Context, task *models.BackgroundTask, checkpoint []byte) error

	// Cancel handles graceful cancellation
	Cancel(ctx context.Context, task *models.BackgroundTask) error

	// GetResourceRequirements returns resource needs for this executor
	GetResourceRequirements() ResourceRequirements
}

// ProgressReporter allows tasks to report progress
type ProgressReporter interface {
	// ReportProgress reports task progress (0-100 percentage)
	ReportProgress(percent float64, message string) error

	// ReportHeartbeat sends a heartbeat to indicate the task is still alive
	ReportHeartbeat() error

	// ReportCheckpoint saves a checkpoint for pause/resume capability
	ReportCheckpoint(data []byte) error

	// ReportMetrics reports custom metrics from the task
	ReportMetrics(metrics map[string]interface{}) error

	// ReportLog reports a log entry from the task
	ReportLog(level, message string, fields map[string]interface{}) error
}

// TaskQueue defines the queue interface for task management
type TaskQueue interface {
	// Enqueue adds a task to the queue
	Enqueue(ctx context.Context, task *models.BackgroundTask) error

	// Dequeue atomically retrieves and claims a task from the queue
	Dequeue(ctx context.Context, workerID string, requirements ResourceRequirements) (*models.BackgroundTask, error)

	// Peek returns tasks without claiming them
	Peek(ctx context.Context, count int) ([]*models.BackgroundTask, error)

	// Requeue returns a task to the queue with optional delay
	Requeue(ctx context.Context, taskID string, delay time.Duration) error

	// MoveToDeadLetter moves a failed task to dead-letter queue
	MoveToDeadLetter(ctx context.Context, taskID string, reason string) error

	// GetPendingCount returns the number of pending tasks
	GetPendingCount(ctx context.Context) (int64, error)

	// GetRunningCount returns the number of running tasks
	GetRunningCount(ctx context.Context) (int64, error)

	// GetQueueDepth returns counts by priority
	GetQueueDepth(ctx context.Context) (map[models.TaskPriority]int64, error)
}

// TaskWaiter provides synchronous waiting for task completion
type TaskWaiter interface {
	// WaitForCompletion blocks until the task completes, fails, or times out
	// Returns the final task state and any error
	// progressCallback is called with progress updates (can be nil)
	WaitForCompletion(ctx context.Context, taskID string, timeout time.Duration, progressCallback func(progress float64, message string)) (*models.BackgroundTask, error)

	// WaitForCompletionWithOutput waits and returns both task state and captured output
	WaitForCompletionWithOutput(ctx context.Context, taskID string, timeout time.Duration) (*models.BackgroundTask, []byte, error)
}

// WaitResult contains the result of waiting for a task
type WaitResult struct {
	Task     *models.BackgroundTask
	Output   []byte
	Duration time.Duration
	Error    error
}

// TaskRepository handles database operations for tasks
type TaskRepository interface {
	// CRUD operations
	Create(ctx context.Context, task *models.BackgroundTask) error
	GetByID(ctx context.Context, id string) (*models.BackgroundTask, error)
	Update(ctx context.Context, task *models.BackgroundTask) error
	Delete(ctx context.Context, id string) error

	// Status operations
	UpdateStatus(ctx context.Context, id string, status models.TaskStatus) error
	UpdateProgress(ctx context.Context, id string, progress float64, message string) error
	UpdateHeartbeat(ctx context.Context, id string) error
	SaveCheckpoint(ctx context.Context, id string, checkpoint []byte) error

	// Query operations
	GetByStatus(ctx context.Context, status models.TaskStatus, limit, offset int) ([]*models.BackgroundTask, error)
	GetPendingTasks(ctx context.Context, limit int) ([]*models.BackgroundTask, error)
	GetStaleTasks(ctx context.Context, threshold time.Duration) ([]*models.BackgroundTask, error)
	GetByWorkerID(ctx context.Context, workerID string) ([]*models.BackgroundTask, error)
	CountByStatus(ctx context.Context) (map[models.TaskStatus]int64, error)

	// Dequeue with atomic update
	Dequeue(ctx context.Context, workerID string, maxCPUCores, maxMemoryMB int) (*models.BackgroundTask, error)

	// Resource snapshots
	SaveResourceSnapshot(ctx context.Context, snapshot *models.ResourceSnapshot) error
	GetResourceSnapshots(ctx context.Context, taskID string, limit int) ([]*models.ResourceSnapshot, error)

	// Execution history
	LogEvent(ctx context.Context, taskID, eventType string, data map[string]interface{}, workerID *string) error
	GetTaskHistory(ctx context.Context, taskID string, limit int) ([]*models.TaskExecutionHistory, error)

	// Dead letter queue
	MoveToDeadLetter(ctx context.Context, taskID, reason string) error
}

// ResourceRequirements specifies resource needs for a task
type ResourceRequirements struct {
	CPUCores int                 `json:"cpu_cores"`
	MemoryMB int                 `json:"memory_mb"`
	DiskMB   int                 `json:"disk_mb"`
	GPUCount int                 `json:"gpu_count"`
	Priority models.TaskPriority `json:"priority"`
}

// SystemResources represents available system resources
type SystemResources struct {
	TotalCPUCores     int     `json:"total_cpu_cores"`
	AvailableCPUCores float64 `json:"available_cpu_cores"`
	TotalMemoryMB     int64   `json:"total_memory_mb"`
	AvailableMemoryMB int64   `json:"available_memory_mb"`
	CPULoadPercent    float64 `json:"cpu_load_percent"`
	MemoryUsedPercent float64 `json:"memory_used_percent"`
	DiskUsedPercent   float64 `json:"disk_used_percent"`
	LoadAvg1          float64 `json:"load_avg_1"`
	LoadAvg5          float64 `json:"load_avg_5"`
	LoadAvg15         float64 `json:"load_avg_15"`
}

// ResourceMonitor tracks system and process resources
type ResourceMonitor interface {
	// GetSystemResources returns current system resource usage
	GetSystemResources() (*SystemResources, error)

	// GetProcessResources returns resource usage for a specific process
	GetProcessResources(pid int) (*models.ResourceSnapshot, error)

	// StartMonitoring begins periodic monitoring of a process
	StartMonitoring(taskID string, pid int, interval time.Duration) error

	// StopMonitoring stops monitoring a process
	StopMonitoring(taskID string) error

	// GetLatestSnapshot returns the most recent snapshot for a task
	GetLatestSnapshot(taskID string) (*models.ResourceSnapshot, error)

	// IsResourceAvailable checks if system has enough resources
	IsResourceAvailable(requirements ResourceRequirements) bool
}

// StuckDetector identifies stuck processes
type StuckDetector interface {
	// IsStuck determines if a task is stuck based on various criteria
	IsStuck(ctx context.Context, task *models.BackgroundTask, snapshots []*models.ResourceSnapshot) (bool, string)

	// GetStuckThreshold returns the stuck detection threshold for a task type
	GetStuckThreshold(taskType string) time.Duration

	// SetThreshold sets a custom threshold for a task type
	SetThreshold(taskType string, threshold time.Duration)
}

// NotificationService handles task notifications
type NotificationService interface {
	// NotifyTaskEvent sends notifications for a task event
	NotifyTaskEvent(ctx context.Context, task *models.BackgroundTask, event string, data map[string]interface{}) error

	// RegisterSSEClient registers a client for SSE notifications
	RegisterSSEClient(ctx context.Context, taskID string, client chan<- []byte) error

	// UnregisterSSEClient removes an SSE client
	UnregisterSSEClient(ctx context.Context, taskID string, client chan<- []byte) error

	// RegisterWebSocketClient registers a WebSocket client
	RegisterWebSocketClient(ctx context.Context, taskID string, client WebSocketClient) error

	// BroadcastToTask broadcasts a message to all clients watching a task
	BroadcastToTask(ctx context.Context, taskID string, message []byte) error
}

// WebSocketClient interface for WebSocket connections
type WebSocketClient interface {
	// Send sends data to the client
	Send(data []byte) error

	// Close closes the connection
	Close() error

	// ID returns the client identifier
	ID() string
}

// WorkerPool manages background task workers
type WorkerPool interface {
	// Start initializes and starts the worker pool
	Start(ctx context.Context) error

	// Stop gracefully stops the worker pool
	Stop(gracePeriod time.Duration) error

	// RegisterExecutor registers a task executor for a task type
	RegisterExecutor(taskType string, executor TaskExecutor)

	// GetWorkerCount returns the current number of workers
	GetWorkerCount() int

	// GetActiveTaskCount returns the number of currently executing tasks
	GetActiveTaskCount() int

	// GetWorkerStatus returns status information for all workers
	GetWorkerStatus() []WorkerStatus

	// Scale manually adjusts the worker count
	Scale(targetCount int) error
}

// WorkerStatus represents the status of a worker
type WorkerStatus struct {
	ID              string                 `json:"id"`
	Status          string                 `json:"status"` // idle, busy, stopping, stopped
	CurrentTask     *models.BackgroundTask `json:"current_task,omitempty"`
	StartedAt       time.Time              `json:"started_at"`
	LastActivity    time.Time              `json:"last_activity"`
	TasksCompleted  int64                  `json:"tasks_completed"`
	TasksFailed     int64                  `json:"tasks_failed"`
	AvgTaskDuration time.Duration          `json:"avg_task_duration"`
}

// TaskEvent represents a task lifecycle event
type TaskEvent struct {
	TaskID    string                 `json:"task_id"`
	EventType string                 `json:"event_type"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data,omitempty"`
	WorkerID  *string                `json:"worker_id,omitempty"`
}

// ExecutionResult represents the result of task execution
type ExecutionResult struct {
	TaskID          string                   `json:"task_id"`
	Status          models.TaskStatus        `json:"status"`
	Output          []byte                   `json:"output,omitempty"`
	Error           string                   `json:"error,omitempty"`
	Duration        time.Duration            `json:"duration"`
	RetryCount      int                      `json:"retry_count"`
	ResourceMetrics *models.ResourceSnapshot `json:"resource_metrics,omitempty"`
}
