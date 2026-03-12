// Package background provides a messaging adapter for the task queue system.
package background

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"

	"digital.vasic.models"
)

// MessagingTaskQueue wraps a TaskQueue and publishes events using an EventPublisher.
// It implements the TaskQueue interface and adds event publishing on lifecycle changes.
type MessagingTaskQueue struct {
	// delegate is the underlying task queue implementation.
	delegate TaskQueue
	// publisher is the event publisher.
	publisher *TaskEventPublisher
	// logger for logging.
	logger Logger
}

// MessagingTaskQueueConfig holds configuration for MessagingTaskQueue.
type MessagingTaskQueueConfig struct {
	// PublisherConfig is the event publisher configuration.
	PublisherConfig *TaskEventPublisherConfig `json:"publisher_config" yaml:"publisher_config"`
}

// DefaultMessagingTaskQueueConfig returns default configuration.
func DefaultMessagingTaskQueueConfig() *MessagingTaskQueueConfig {
	return &MessagingTaskQueueConfig{
		PublisherConfig: DefaultTaskEventPublisherConfig(),
	}
}

// NewMessagingTaskQueue creates a new messaging-enabled task queue.
func NewMessagingTaskQueue(
	delegate TaskQueue,
	publisher EventPublisher,
	logger Logger,
	config *MessagingTaskQueueConfig,
) *MessagingTaskQueue {
	if config == nil {
		config = DefaultMessagingTaskQueueConfig()
	}
	if logger == nil {
		logger = &noopLogger{}
	}

	taskPublisher := NewTaskEventPublisher(publisher, logger, config.PublisherConfig)

	return &MessagingTaskQueue{
		delegate:  delegate,
		publisher: taskPublisher,
		logger:    logger,
	}
}

// Start starts the messaging task queue.
func (q *MessagingTaskQueue) Start() {
	q.publisher.Start()
}

// Stop stops the messaging task queue.
func (q *MessagingTaskQueue) Stop() {
	q.publisher.Stop()
}

// Enqueue adds a task to the queue and publishes a created event.
func (q *MessagingTaskQueue) Enqueue(ctx context.Context, task *models.BackgroundTask) error {
	// Enqueue to the delegate
	if err := q.delegate.Enqueue(ctx, task); err != nil {
		return err
	}

	// Publish created event
	if err := q.publisher.PublishTaskCreated(ctx, task); err != nil {
		q.logger.Warnf("Failed to publish task created event: task_id=%s error=%v", task.ID, err)
	}

	return nil
}

// Dequeue retrieves and claims a task, publishing a started event.
func (q *MessagingTaskQueue) Dequeue(ctx context.Context, workerID string, requirements ResourceRequirements) (*models.BackgroundTask, error) {
	task, err := q.delegate.Dequeue(ctx, workerID, requirements)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, nil
	}

	// Publish started event
	if err := q.publisher.PublishTaskStarted(ctx, task); err != nil {
		q.logger.Warnf("Failed to publish task started event: task_id=%s error=%v", task.ID, err)
	}

	return task, nil
}

// Peek returns tasks without claiming them (no event published).
func (q *MessagingTaskQueue) Peek(ctx context.Context, count int) ([]*models.BackgroundTask, error) {
	return q.delegate.Peek(ctx, count)
}

// Requeue returns a task to the queue and publishes a retrying event.
func (q *MessagingTaskQueue) Requeue(ctx context.Context, taskID string, delay time.Duration) error {
	// First get the task for the event
	var task *models.BackgroundTask
	if pg, ok := q.delegate.(*PostgresTaskQueue); ok {
		var err error
		task, err = pg.repository.GetByID(ctx, taskID)
		if err != nil {
			q.logger.Debugf("Could not get task for requeue event: task_id=%s error=%v", taskID, err)
		}
	}

	// Requeue to the delegate
	if err := q.delegate.Requeue(ctx, taskID, delay); err != nil {
		return err
	}

	// Publish retrying event
	if task != nil {
		if err := q.publisher.PublishTaskRetrying(ctx, task, delay); err != nil {
			q.logger.Warnf("Failed to publish task retrying event: task_id=%s error=%v", taskID, err)
		}
	}

	return nil
}

// MoveToDeadLetter moves a failed task and publishes a dead letter event.
func (q *MessagingTaskQueue) MoveToDeadLetter(ctx context.Context, taskID string, reason string) error {
	// First get the task for the event
	var task *models.BackgroundTask
	if pg, ok := q.delegate.(*PostgresTaskQueue); ok {
		var err error
		task, err = pg.repository.GetByID(ctx, taskID)
		if err != nil {
			q.logger.Debugf("Could not get task for dead letter event: task_id=%s error=%v", taskID, err)
		}
	}

	// Move to dead letter
	if err := q.delegate.MoveToDeadLetter(ctx, taskID, reason); err != nil {
		return err
	}

	// Publish dead letter event
	if task != nil {
		if err := q.publisher.PublishTaskDeadLetter(ctx, task, reason); err != nil {
			q.logger.Warnf("Failed to publish task dead letter event: task_id=%s error=%v", taskID, err)
		}
	}

	return nil
}

// GetPendingCount returns the number of pending tasks.
func (q *MessagingTaskQueue) GetPendingCount(ctx context.Context) (int64, error) {
	return q.delegate.GetPendingCount(ctx)
}

// GetRunningCount returns the number of running tasks.
func (q *MessagingTaskQueue) GetRunningCount(ctx context.Context) (int64, error) {
	return q.delegate.GetRunningCount(ctx)
}

// GetQueueDepth returns counts by priority.
func (q *MessagingTaskQueue) GetQueueDepth(ctx context.Context) (map[models.TaskPriority]int64, error) {
	return q.delegate.GetQueueDepth(ctx)
}

// Publisher returns the event publisher.
func (q *MessagingTaskQueue) Publisher() *TaskEventPublisher {
	return q.publisher
}

// Delegate returns the underlying task queue.
func (q *MessagingTaskQueue) Delegate() TaskQueue {
	return q.delegate
}

// MessagingProgressReporter wraps a ProgressReporter and publishes progress events.
type MessagingProgressReporter struct {
	// delegate is the underlying progress reporter.
	delegate ProgressReporter
	// publisher is the event publisher.
	publisher *TaskEventPublisher
	// task is the current task.
	task *models.BackgroundTask
	// logger for logging.
	logger *logrus.Logger
}

// NewMessagingProgressReporter creates a new messaging-enabled progress reporter.
func NewMessagingProgressReporter(
	delegate ProgressReporter,
	publisher *TaskEventPublisher,
	task *models.BackgroundTask,
	logger *logrus.Logger,
) *MessagingProgressReporter {
	return &MessagingProgressReporter{
		delegate:  delegate,
		publisher: publisher,
		task:      task,
		logger:    logger,
	}
}

// ReportProgress reports task progress and publishes a progress event.
func (r *MessagingProgressReporter) ReportProgress(percent float64, message string) error {
	// Update task state
	r.task.Progress = percent
	r.task.ProgressMessage = &message

	// Report to delegate
	if err := r.delegate.ReportProgress(percent, message); err != nil {
		return err
	}

	// Publish progress event
	if err := r.publisher.PublishTaskProgress(context.Background(), r.task); err != nil {
		r.logger.WithError(err).WithField("task_id", r.task.ID).
			Debug("Failed to publish task progress event")
	}

	return nil
}

// ReportHeartbeat sends a heartbeat and optionally publishes a heartbeat event.
func (r *MessagingProgressReporter) ReportHeartbeat() error {
	return r.delegate.ReportHeartbeat()
}

// ReportCheckpoint saves a checkpoint for pause/resume capability.
func (r *MessagingProgressReporter) ReportCheckpoint(data []byte) error {
	return r.delegate.ReportCheckpoint(data)
}

// ReportMetrics reports custom metrics from the task.
func (r *MessagingProgressReporter) ReportMetrics(metrics map[string]interface{}) error {
	return r.delegate.ReportMetrics(metrics)
}

// ReportLog reports a log entry from the task.
func (r *MessagingProgressReporter) ReportLog(level, message string, fields map[string]interface{}) error {
	return r.delegate.ReportLog(level, message, fields)
}

// MessagingTaskExecutorWrapper wraps a TaskExecutor and publishes events.
type MessagingTaskExecutorWrapper struct {
	// executor is the underlying task executor.
	executor TaskExecutor
	// publisher is the event publisher.
	publisher *TaskEventPublisher
	// logger for logging.
	logger *logrus.Logger
}

// NewMessagingTaskExecutorWrapper creates a new messaging-enabled executor wrapper.
func NewMessagingTaskExecutorWrapper(
	executor TaskExecutor,
	publisher *TaskEventPublisher,
	logger *logrus.Logger,
) *MessagingTaskExecutorWrapper {
	return &MessagingTaskExecutorWrapper{
		executor:  executor,
		publisher: publisher,
		logger:    logger,
	}
}

// Execute runs the task and publishes completion/failure events.
func (w *MessagingTaskExecutorWrapper) Execute(ctx context.Context, task *models.BackgroundTask, reporter ProgressReporter) error {
	// Wrap reporter with messaging
	msgReporter := NewMessagingProgressReporter(reporter, w.publisher, task, w.logger)

	// Execute the task
	err := w.executor.Execute(ctx, task, msgReporter)

	if err != nil {
		// Publish failed event
		task.Status = models.TaskStatusFailed
		errStr := err.Error()
		task.LastError = &errStr
		if pubErr := w.publisher.PublishTaskFailed(ctx, task, err); pubErr != nil {
			w.logger.WithError(pubErr).WithField("task_id", task.ID).
				Warn("Failed to publish task failed event")
		}
	} else {
		// Publish completed event
		task.Status = models.TaskStatusCompleted
		task.Progress = 100
		if pubErr := w.publisher.PublishTaskCompleted(ctx, task, nil); pubErr != nil {
			w.logger.WithError(pubErr).WithField("task_id", task.ID).
				Warn("Failed to publish task completed event")
		}
	}

	return err
}

// CanPause returns whether this task type supports pause/resume.
func (w *MessagingTaskExecutorWrapper) CanPause() bool {
	return w.executor.CanPause()
}

// Pause saves checkpoint for later resume.
func (w *MessagingTaskExecutorWrapper) Pause(ctx context.Context, task *models.BackgroundTask) ([]byte, error) {
	checkpoint, err := w.executor.Pause(ctx, task)
	if err != nil {
		return nil, err
	}

	// Publish paused event
	task.Status = models.TaskStatusPaused
	event := NewBackgroundTaskEvent(TaskEventTypePaused, task)
	if pubErr := w.publisher.Publish(ctx, event); pubErr != nil {
		w.logger.WithError(pubErr).WithField("task_id", task.ID).
			Warn("Failed to publish task paused event")
	}

	return checkpoint, nil
}

// Resume restores from checkpoint.
func (w *MessagingTaskExecutorWrapper) Resume(ctx context.Context, task *models.BackgroundTask, checkpoint []byte) error {
	if err := w.executor.Resume(ctx, task, checkpoint); err != nil {
		return err
	}

	// Publish resumed event
	task.Status = models.TaskStatusRunning
	event := NewBackgroundTaskEvent(TaskEventTypeResumed, task)
	if pubErr := w.publisher.Publish(ctx, event); pubErr != nil {
		w.logger.WithError(pubErr).WithField("task_id", task.ID).
			Warn("Failed to publish task resumed event")
	}

	return nil
}

// Cancel handles graceful cancellation.
func (w *MessagingTaskExecutorWrapper) Cancel(ctx context.Context, task *models.BackgroundTask) error {
	if err := w.executor.Cancel(ctx, task); err != nil {
		return err
	}

	// Publish cancelled event
	task.Status = models.TaskStatusCancelled
	if pubErr := w.publisher.PublishTaskCancelled(ctx, task); pubErr != nil {
		w.logger.WithError(pubErr).WithField("task_id", task.ID).
			Warn("Failed to publish task cancelled event")
	}

	return nil
}

// GetResourceRequirements returns resource needs for this executor.
func (w *MessagingTaskExecutorWrapper) GetResourceRequirements() ResourceRequirements {
	return w.executor.GetResourceRequirements()
}

// SetupMessagingForWorkerPool configures messaging integration for a worker pool.
// This is a convenience function to wire up all the messaging components.
func SetupMessagingForWorkerPool(
	pool WorkerPool,
	queue TaskQueue,
	publisher EventPublisher,
	logger Logger,
	config *MessagingTaskQueueConfig,
) (*MessagingTaskQueue, error) {
	// Create messaging task queue
	msgQueue := NewMessagingTaskQueue(queue, publisher, logger, config)
	msgQueue.Start()

	return msgQueue, nil
}
