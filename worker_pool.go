package background

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"digital.vasic.models"
)

// WorkerPoolConfig holds configuration for the worker pool
type WorkerPoolConfig struct {
	MinWorkers            int           `yaml:"min_workers"`
	MaxWorkers            int           `yaml:"max_workers"`
	ScaleUpThreshold      float64       `yaml:"scale_up_threshold"`
	ScaleDownThreshold    float64       `yaml:"scale_down_threshold"`
	ScaleInterval         time.Duration `yaml:"scale_interval"`
	WorkerIdleTimeout     time.Duration `yaml:"worker_idle_timeout"`
	QueuePollInterval     time.Duration `yaml:"queue_poll_interval"`
	HeartbeatInterval     time.Duration `yaml:"heartbeat_interval"`
	ResourceCheckInterval time.Duration `yaml:"resource_check_interval"`
	MaxCPUPercent         float64       `yaml:"max_cpu_percent"`
	MaxMemoryPercent      float64       `yaml:"max_memory_percent"`
	GracefulShutdownTime  time.Duration `yaml:"graceful_shutdown_time"`
}

// DefaultWorkerPoolConfig returns sensible defaults
func DefaultWorkerPoolConfig() *WorkerPoolConfig {
	return &WorkerPoolConfig{
		MinWorkers:            2,
		MaxWorkers:            runtime.NumCPU() * 2,
		ScaleUpThreshold:      0.7,
		ScaleDownThreshold:    0.3,
		ScaleInterval:         30 * time.Second,
		WorkerIdleTimeout:     5 * time.Minute,
		QueuePollInterval:     1 * time.Second,
		HeartbeatInterval:     10 * time.Second,
		ResourceCheckInterval: 5 * time.Second,
		MaxCPUPercent:         80.0,
		MaxMemoryPercent:      80.0,
		GracefulShutdownTime:  30 * time.Second,
	}
}

// AdaptiveWorkerPool manages background task execution with auto-scaling
type AdaptiveWorkerPool struct {
	config          *WorkerPoolConfig
	queue           TaskQueue
	repository      TaskRepository
	executors       map[string]TaskExecutor
	executorsMu     sync.RWMutex
	resourceMonitor ResourceMonitor
	stuckDetector   StuckDetector
	notifier        NotificationService
	logger          *logrus.Logger
	metrics         *WorkerPoolMetrics

	workers     map[string]*Worker
	workersMu   sync.RWMutex
	activeCount int32

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	scaling int32 // 1 if scaling in progress
	started bool
}

// Worker represents a single worker in the pool
type Worker struct {
	ID             string
	status         int32 // Use atomic for thread-safe access (workerState)
	currentTask    *models.BackgroundTask
	taskMu         sync.RWMutex // Protects currentTask
	StartedAt      time.Time
	lastActivity   time.Time
	activityMu     sync.RWMutex // Protects lastActivity
	TasksCompleted int64
	TasksFailed    int64
	TotalDuration  time.Duration

	pool         *AdaptiveWorkerPool
	ctx          context.Context
	cancel       context.CancelFunc
	stopChan     chan struct{}
	stopOnce     sync.Once // CONCURRENCY FIX: Prevent double-close panic
	stopChanDone int32     // Atomic flag to track if stopChan is closed
}

type workerState int32

const (
	workerStateIdle workerState = iota
	workerStateBusy
	workerStateStopping
	workerStateStopped
)

func (s workerState) String() string {
	switch s {
	case workerStateIdle:
		return "idle"
	case workerStateBusy:
		return "busy"
	case workerStateStopping:
		return "stopping"
	case workerStateStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// CONCURRENCY FIX: Thread-safe worker state accessors

// Status returns the worker's current state (thread-safe)
func (w *Worker) Status() workerState {
	return workerState(atomic.LoadInt32(&w.status))
}

// setStatus sets the worker's state (thread-safe)
func (w *Worker) setStatus(s workerState) {
	atomic.StoreInt32(&w.status, int32(s))
}

// CurrentTask returns the worker's current task (thread-safe)
func (w *Worker) CurrentTask() *models.BackgroundTask {
	w.taskMu.RLock()
	defer w.taskMu.RUnlock()
	return w.currentTask
}

// setCurrentTask sets the worker's current task (thread-safe)
func (w *Worker) setCurrentTask(task *models.BackgroundTask) {
	w.taskMu.Lock()
	defer w.taskMu.Unlock()
	w.currentTask = task
}

// LastActivity returns the worker's last activity time (thread-safe)
func (w *Worker) LastActivity() time.Time {
	w.activityMu.RLock()
	defer w.activityMu.RUnlock()
	return w.lastActivity
}

// setLastActivity sets the worker's last activity time (thread-safe)
func (w *Worker) setLastActivity(t time.Time) {
	w.activityMu.Lock()
	defer w.activityMu.Unlock()
	w.lastActivity = t
}

// signalStop safely signals the worker to stop (prevents double-close panic)
func (w *Worker) signalStop() bool {
	sent := false
	w.stopOnce.Do(func() {
		close(w.stopChan)
		atomic.StoreInt32(&w.stopChanDone, 1)
		sent = true
	})
	return sent
}

// NewAdaptiveWorkerPool creates a new worker pool
func NewAdaptiveWorkerPool(
	config *WorkerPoolConfig,
	queue TaskQueue,
	repository TaskRepository,
	resourceMonitor ResourceMonitor,
	stuckDetector StuckDetector,
	notifier NotificationService,
	logger *logrus.Logger,
) *AdaptiveWorkerPool {
	if config == nil {
		config = DefaultWorkerPoolConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &AdaptiveWorkerPool{
		config:          config,
		queue:           queue,
		repository:      repository,
		executors:       make(map[string]TaskExecutor),
		resourceMonitor: resourceMonitor,
		stuckDetector:   stuckDetector,
		notifier:        notifier,
		logger:          logger,
		metrics:         NewWorkerPoolMetrics(),
		workers:         make(map[string]*Worker),
		ctx:             ctx,
		cancel:          cancel,
	}
}

// RegisterExecutor registers a task executor for a task type
func (wp *AdaptiveWorkerPool) RegisterExecutor(taskType string, executor TaskExecutor) {
	wp.executorsMu.Lock()
	defer wp.executorsMu.Unlock()
	wp.executors[taskType] = executor
	wp.logger.WithField("task_type", taskType).Debug("Registered task executor")
}

// Start initializes and starts the worker pool
func (wp *AdaptiveWorkerPool) Start(ctx context.Context) error {
	if wp.started {
		return fmt.Errorf("worker pool already started")
	}

	wp.logger.WithFields(logrus.Fields{
		"min_workers": wp.config.MinWorkers,
		"max_workers": wp.config.MaxWorkers,
	}).Info("Starting worker pool")

	// Start minimum workers
	for i := 0; i < wp.config.MinWorkers; i++ {
		wp.spawnWorker()
	}

	// Start scaling monitor
	wp.wg.Add(1)
	go wp.scalingLoop()

	// Start stuck detection loop
	wp.wg.Add(1)
	go wp.stuckDetectionLoop()

	// Start heartbeat monitor
	wp.wg.Add(1)
	go wp.heartbeatMonitorLoop()

	wp.started = true
	return nil
}

// Stop gracefully stops the worker pool
func (wp *AdaptiveWorkerPool) Stop(gracePeriod time.Duration) error {
	wp.logger.Info("Stopping worker pool")
	wp.cancel()

	// Signal all workers to stop (using safe signalStop to prevent double-close panic)
	wp.workersMu.Lock()
	for _, worker := range wp.workers {
		worker.signalStop()
	}
	wp.workersMu.Unlock()

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wp.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		wp.logger.Info("Worker pool stopped gracefully")
	case <-time.After(gracePeriod):
		wp.logger.Warn("Worker pool stop timed out")
	}

	wp.started = false
	return nil
}

// GetWorkerCount returns the current number of workers
func (wp *AdaptiveWorkerPool) GetWorkerCount() int {
	return int(atomic.LoadInt32(&wp.activeCount))
}

// GetActiveTaskCount returns the number of currently executing tasks
func (wp *AdaptiveWorkerPool) GetActiveTaskCount() int {
	wp.workersMu.RLock()
	defer wp.workersMu.RUnlock()

	count := 0
	for _, worker := range wp.workers {
		// CONCURRENCY FIX: Use thread-safe accessor
		if worker.Status() == workerStateBusy {
			count++
		}
	}
	return count
}

// GetWorkerStatus returns status information for all workers
func (wp *AdaptiveWorkerPool) GetWorkerStatus() []WorkerStatus {
	wp.workersMu.RLock()
	defer wp.workersMu.RUnlock()

	statuses := make([]WorkerStatus, 0, len(wp.workers))
	for _, worker := range wp.workers {
		var avgDuration time.Duration
		completed := atomic.LoadInt64(&worker.TasksCompleted)
		if completed > 0 {
			avgDuration = worker.TotalDuration / time.Duration(completed)
		}

		// CONCURRENCY FIX: Use thread-safe accessors
		statuses = append(statuses, WorkerStatus{
			ID:              worker.ID,
			Status:          worker.Status().String(),
			CurrentTask:     worker.CurrentTask(),
			StartedAt:       worker.StartedAt,
			LastActivity:    worker.LastActivity(),
			TasksCompleted:  completed,
			TasksFailed:     atomic.LoadInt64(&worker.TasksFailed),
			AvgTaskDuration: avgDuration,
		})
	}
	return statuses
}

// Scale manually adjusts the worker count
func (wp *AdaptiveWorkerPool) Scale(targetCount int) error {
	if targetCount < wp.config.MinWorkers {
		targetCount = wp.config.MinWorkers
	}
	if targetCount > wp.config.MaxWorkers {
		targetCount = wp.config.MaxWorkers
	}

	current := wp.GetWorkerCount()
	diff := targetCount - current

	if diff > 0 {
		for i := 0; i < diff; i++ {
			wp.spawnWorker()
		}
	} else if diff < 0 {
		// Workers will stop themselves via idle timeout
		// or we can explicitly stop some
		// CONCURRENCY FIX: Use thread-safe accessors and signalStop
		wp.workersMu.Lock()
		stopped := 0
		for _, worker := range wp.workers {
			if worker.Status() == workerStateIdle && stopped < -diff {
				if worker.signalStop() {
					stopped++
				}
			}
		}
		wp.workersMu.Unlock()
	}

	return nil
}

// spawnWorker creates and starts a new worker
func (wp *AdaptiveWorkerPool) spawnWorker() {
	workerID := uuid.New().String()[:8]
	workerCtx, workerCancel := context.WithCancel(wp.ctx)

	worker := &Worker{
		ID:           workerID,
		status:       int32(workerStateIdle),
		StartedAt:    time.Now(),
		lastActivity: time.Now(),
		pool:         wp,
		ctx:          workerCtx,
		cancel:       workerCancel,
		stopChan:     make(chan struct{}),
		// stopOnce and stopChanDone are zero-initialized
	}

	wp.workersMu.Lock()
	wp.workers[workerID] = worker
	wp.workersMu.Unlock()

	atomic.AddInt32(&wp.activeCount, 1)
	if wp.metrics != nil {
		wp.metrics.WorkersActive.Inc()
	}

	wp.wg.Add(1)
	go wp.workerLoop(worker)

	wp.logger.WithField("worker_id", workerID).Debug("Worker spawned")
}

// workerLoop is the main loop for a worker
func (wp *AdaptiveWorkerPool) workerLoop(worker *Worker) {
	defer wp.wg.Done()
	defer func() {
		atomic.AddInt32(&wp.activeCount, -1)
		if wp.metrics != nil {
			wp.metrics.WorkersActive.Dec()
		}
		wp.workersMu.Lock()
		delete(wp.workers, worker.ID)
		wp.workersMu.Unlock()
		// CONCURRENCY FIX: Use thread-safe setter
		worker.setStatus(workerStateStopped)
	}()

	idleTimer := time.NewTimer(wp.config.WorkerIdleTimeout)
	defer idleTimer.Stop()

	for {
		select {
		case <-worker.ctx.Done():
			return
		case <-worker.stopChan:
			return
		case <-idleTimer.C:
			// Check if we can scale down
			if wp.canScaleDown() {
				wp.logger.WithField("worker_id", worker.ID).Debug("Worker stopping due to idle timeout")
				return
			}
			idleTimer.Reset(wp.config.WorkerIdleTimeout)
		default:
			// Try to get a task
			requirements := wp.calculateWorkerRequirements()

			task, err := wp.queue.Dequeue(worker.ctx, worker.ID, requirements)
			if err != nil {
				wp.logger.WithError(err).Debug("Failed to dequeue task")
				time.Sleep(wp.config.QueuePollInterval)
				continue
			}

			if task == nil {
				time.Sleep(wp.config.QueuePollInterval)
				continue
			}

			// Reset idle timer
			idleTimer.Reset(wp.config.WorkerIdleTimeout)

			// Execute task
			// CONCURRENCY FIX: Use thread-safe setters
			worker.setStatus(workerStateBusy)
			worker.setCurrentTask(task)
			worker.setLastActivity(time.Now())

			wp.executeTask(worker, task)

			worker.setStatus(workerStateIdle)
			worker.setCurrentTask(nil)
			worker.setLastActivity(time.Now())
		}
	}
}

// executeTask handles the execution of a single task
func (wp *AdaptiveWorkerPool) executeTask(worker *Worker, task *models.BackgroundTask) {
	startTime := time.Now()

	wp.logger.WithFields(logrus.Fields{
		"task_id":   task.ID,
		"task_type": task.TaskType,
		"worker_id": worker.ID,
	}).Debug("Starting task execution")

	// Get executor
	wp.executorsMu.RLock()
	executor, exists := wp.executors[task.TaskType]
	wp.executorsMu.RUnlock()

	if !exists {
		wp.handleTaskError(task, worker, fmt.Errorf("no executor registered for task type: %s", task.TaskType))
		return
	}

	// Start resource monitoring if process PID is set
	if task.ProcessPID != nil && wp.resourceMonitor != nil {
		if err := wp.resourceMonitor.StartMonitoring(task.ID, *task.ProcessPID, wp.config.ResourceCheckInterval); err != nil {
			wp.logger.WithError(err).Debug("Failed to start resource monitoring")
		}
		defer func() {
			if err := wp.resourceMonitor.StopMonitoring(task.ID); err != nil {
				wp.logger.WithError(err).Debug("Failed to stop resource monitoring")
			}
		}()
	}

	// Create progress reporter
	reporter := &taskProgressReporter{
		taskID:     task.ID,
		repository: wp.repository,
		notifier:   wp.notifier,
		task:       task,
		workerID:   worker.ID,
	}

	// Create task context with timeout
	var taskCtx context.Context
	var taskCancel context.CancelFunc

	if task.Config.Endless {
		taskCtx, taskCancel = context.WithCancel(worker.ctx)
	} else {
		timeout := time.Duration(task.Config.TimeoutSeconds) * time.Second
		if timeout == 0 {
			timeout = 30 * time.Minute // Default timeout
		}
		taskCtx, taskCancel = context.WithTimeout(worker.ctx, timeout)
	}
	defer taskCancel()

	// Notify task started
	if wp.notifier != nil {
		if err := wp.notifier.NotifyTaskEvent(taskCtx, task, models.TaskEventStarted, map[string]interface{}{
			"worker_id": worker.ID,
		}); err != nil {
			wp.logger.WithError(err).Debug("Failed to notify task started event")
		}
	}

	// Execute
	err := executor.Execute(taskCtx, task, reporter)

	duration := time.Since(startTime)
	durationSec := int(duration.Seconds())
	task.ActualDurationSeconds = &durationSec
	worker.TotalDuration += duration

	if err != nil {
		wp.handleTaskError(task, worker, err)
	} else {
		wp.handleTaskSuccess(task, worker, duration)
	}

	if wp.metrics != nil {
		wp.metrics.TaskDuration.WithLabelValues(task.TaskType).Observe(duration.Seconds())
	}
}

// handleTaskSuccess handles successful task completion
func (wp *AdaptiveWorkerPool) handleTaskSuccess(task *models.BackgroundTask, worker *Worker, duration time.Duration) {
	task.Status = models.TaskStatusCompleted
	now := time.Now()
	task.CompletedAt = &now
	task.Progress = 100

	if err := wp.repository.Update(context.Background(), task); err != nil {
		wp.logger.WithError(err).Error("Failed to update completed task")
	}

	if err := wp.repository.LogEvent(context.Background(), task.ID, models.TaskEventCompleted, map[string]interface{}{
		"duration_ms": duration.Milliseconds(),
		"worker_id":   worker.ID,
	}, &worker.ID); err != nil {
		wp.logger.WithError(err).Debug("Failed to log task completed event")
	}

	if wp.notifier != nil {
		if err := wp.notifier.NotifyTaskEvent(context.Background(), task, models.TaskEventCompleted, map[string]interface{}{
			"duration_ms": duration.Milliseconds(),
		}); err != nil {
			wp.logger.WithError(err).Debug("Failed to notify task completed event")
		}
	}

	atomic.AddInt64(&worker.TasksCompleted, 1)
	if wp.metrics != nil {
		wp.metrics.TasksTotal.WithLabelValues(task.TaskType, "completed").Inc()
	}

	wp.logger.WithFields(logrus.Fields{
		"task_id":   task.ID,
		"task_type": task.TaskType,
		"duration":  duration,
	}).Debug("Task completed successfully")
}

// handleTaskError handles task failure
func (wp *AdaptiveWorkerPool) handleTaskError(task *models.BackgroundTask, worker *Worker, err error) {
	errorMsg := err.Error()
	task.LastError = &errorMsg

	wp.logger.WithError(err).WithFields(logrus.Fields{
		"task_id":     task.ID,
		"task_type":   task.TaskType,
		"retry_count": task.RetryCount,
		"max_retries": task.MaxRetries,
	}).Warn("Task execution failed")

	// Check if we should retry
	if task.CanRetry() {
		delay := time.Duration(task.RetryDelaySeconds) * time.Second
		if err := wp.queue.Requeue(context.Background(), task.ID, delay); err != nil {
			wp.logger.WithError(err).Error("Failed to requeue task")
		}
		if wp.metrics != nil {
			wp.metrics.TaskRetries.WithLabelValues(task.TaskType).Inc()
		}
	} else {
		// Move to dead letter queue
		task.Status = models.TaskStatusFailed
		if err := wp.repository.Update(context.Background(), task); err != nil {
			wp.logger.WithError(err).Error("Failed to update failed task")
		}

		if err := wp.queue.MoveToDeadLetter(context.Background(), task.ID, errorMsg); err != nil {
			wp.logger.WithError(err).Error("Failed to move task to dead letter")
		}

		if wp.notifier != nil {
			if err := wp.notifier.NotifyTaskEvent(context.Background(), task, models.TaskEventFailed, map[string]interface{}{
				"error":       errorMsg,
				"retry_count": task.RetryCount,
			}); err != nil {
				wp.logger.WithError(err).Debug("Failed to notify task failed event")
			}
		}

		if wp.metrics != nil {
			wp.metrics.TasksTotal.WithLabelValues(task.TaskType, "failed").Inc()
			wp.metrics.DeadLetterTasks.Inc()
		}
	}

	if err := wp.repository.LogEvent(context.Background(), task.ID, models.TaskEventFailed, map[string]interface{}{
		"error":       errorMsg,
		"retry_count": task.RetryCount,
		"worker_id":   worker.ID,
	}, &worker.ID); err != nil {
		wp.logger.WithError(err).Debug("Failed to log task failed event")
	}

	atomic.AddInt64(&worker.TasksFailed, 1)
}

// scalingLoop monitors load and scales workers
func (wp *AdaptiveWorkerPool) scalingLoop() {
	defer wp.wg.Done()

	ticker := time.NewTicker(wp.config.ScaleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-wp.ctx.Done():
			return
		case <-ticker.C:
			wp.checkAndScale()
		}
	}
}

// checkAndScale evaluates if scaling is needed
func (wp *AdaptiveWorkerPool) checkAndScale() {
	// Prevent concurrent scaling
	if !atomic.CompareAndSwapInt32(&wp.scaling, 0, 1) {
		return
	}
	defer atomic.StoreInt32(&wp.scaling, 0)

	if wp.resourceMonitor == nil {
		return
	}

	resources, err := wp.resourceMonitor.GetSystemResources()
	if err != nil {
		wp.logger.WithError(err).Warn("Failed to get system resources")
		return
	}

	currentWorkers := wp.GetWorkerCount()
	pendingCount, _ := wp.queue.GetPendingCount(wp.ctx) //nolint:errcheck

	// Calculate load factor
	cpuLoad := resources.CPULoadPercent / 100.0
	memLoad := resources.MemoryUsedPercent / 100.0
	avgLoad := (cpuLoad + memLoad) / 2.0

	queuePressure := float64(pendingCount) / float64(currentWorkers+1)

	// Scale up conditions
	if avgLoad < wp.config.ScaleUpThreshold &&
		queuePressure > 2.0 &&
		currentWorkers < wp.config.MaxWorkers &&
		resources.CPULoadPercent < wp.config.MaxCPUPercent &&
		resources.MemoryUsedPercent < wp.config.MaxMemoryPercent {

		// Scale up
		workersToAdd := min(
			wp.config.MaxWorkers-currentWorkers,
			int(queuePressure),
			3, // Max workers to add at once
		)

		for i := 0; i < workersToAdd; i++ {
			wp.spawnWorker()
		}

		wp.logger.WithFields(logrus.Fields{
			"added":   workersToAdd,
			"current": currentWorkers + workersToAdd,
			"load":    avgLoad,
			"pending": pendingCount,
		}).Info("Scaled up workers")

		if wp.metrics != nil {
			wp.metrics.ScalingEvents.WithLabelValues("up").Inc()
		}
	}

	// Scale down is handled by idle timeout in worker loop
	if avgLoad < wp.config.ScaleDownThreshold && queuePressure < 0.5 && currentWorkers > wp.config.MinWorkers {
		if wp.metrics != nil {
			wp.metrics.ScalingEvents.WithLabelValues("down").Inc()
		}
	}
}

// canScaleDown returns true if we can reduce workers
func (wp *AdaptiveWorkerPool) canScaleDown() bool {
	return wp.GetWorkerCount() > wp.config.MinWorkers
}

// stuckDetectionLoop periodically checks for stuck tasks
func (wp *AdaptiveWorkerPool) stuckDetectionLoop() {
	defer wp.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-wp.ctx.Done():
			return
		case <-ticker.C:
			wp.checkForStuckTasks()
		}
	}
}

// checkForStuckTasks identifies and handles stuck tasks
func (wp *AdaptiveWorkerPool) checkForStuckTasks() {
	if wp.stuckDetector == nil {
		return
	}

	threshold := 5 * time.Minute // Default threshold
	staleTasks, err := wp.repository.GetStaleTasks(wp.ctx, threshold)
	if err != nil {
		wp.logger.WithError(err).Warn("Failed to get stale tasks")
		return
	}

	for _, task := range staleTasks {
		// Get resource snapshots
		var snapshots []*models.ResourceSnapshot
		if wp.repository != nil {
			snapshots, _ = wp.repository.GetResourceSnapshots(wp.ctx, task.ID, 10) //nolint:errcheck
		}

		isStuck, reason := wp.stuckDetector.IsStuck(wp.ctx, task, snapshots)
		if isStuck {
			wp.handleStuckTask(task, reason)
		}
	}
}

// handleStuckTask handles a task that has been detected as stuck
func (wp *AdaptiveWorkerPool) handleStuckTask(task *models.BackgroundTask, reason string) {
	wp.logger.WithFields(logrus.Fields{
		"task_id": task.ID,
		"reason":  reason,
	}).Warn("Task detected as stuck")

	task.Status = models.TaskStatusStuck
	task.LastError = &reason

	if err := wp.repository.Update(wp.ctx, task); err != nil {
		wp.logger.WithError(err).Error("Failed to update stuck task")
	}

	if err := wp.repository.LogEvent(wp.ctx, task.ID, models.TaskEventStuck, map[string]interface{}{
		"reason": reason,
	}, task.WorkerID); err != nil {
		wp.logger.WithError(err).Debug("Failed to log task stuck event")
	}

	if wp.notifier != nil {
		if err := wp.notifier.NotifyTaskEvent(wp.ctx, task, models.TaskEventStuck, map[string]interface{}{
			"reason": reason,
		}); err != nil {
			wp.logger.WithError(err).Debug("Failed to notify task stuck event")
		}
	}

	if wp.metrics != nil {
		wp.metrics.StuckTasks.Inc()
	}
}

// heartbeatMonitorLoop monitors worker heartbeats
func (wp *AdaptiveWorkerPool) heartbeatMonitorLoop() {
	defer wp.wg.Done()

	ticker := time.NewTicker(wp.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-wp.ctx.Done():
			return
		case <-ticker.C:
			wp.updateHeartbeats()
		}
	}
}

// updateHeartbeats updates heartbeats for running tasks
func (wp *AdaptiveWorkerPool) updateHeartbeats() {
	wp.workersMu.RLock()
	defer wp.workersMu.RUnlock()

	for _, worker := range wp.workers {
		// CONCURRENCY FIX: Use thread-safe accessors
		currentTask := worker.CurrentTask()
		if currentTask != nil && worker.Status() == workerStateBusy {
			if err := wp.repository.UpdateHeartbeat(wp.ctx, currentTask.ID); err != nil {
				wp.logger.WithError(err).WithField("task_id", currentTask.ID).Debug("Failed to update heartbeat")
			}
		}
	}
}

// calculateWorkerRequirements calculates resource requirements for dequeuing
func (wp *AdaptiveWorkerPool) calculateWorkerRequirements() ResourceRequirements {
	if wp.resourceMonitor == nil {
		return ResourceRequirements{}
	}

	resources, err := wp.resourceMonitor.GetSystemResources()
	if err != nil {
		return ResourceRequirements{}
	}

	// Calculate available resources per worker
	workerCount := wp.GetWorkerCount()
	if workerCount == 0 {
		workerCount = 1
	}

	return ResourceRequirements{
		CPUCores: int(resources.AvailableCPUCores) / workerCount,
		MemoryMB: int(resources.AvailableMemoryMB) / workerCount,
	}
}

// taskProgressReporter implements ProgressReporter
type taskProgressReporter struct {
	taskID     string
	repository TaskRepository
	notifier   NotificationService
	task       *models.BackgroundTask
	workerID   string
}

func (r *taskProgressReporter) ReportProgress(percent float64, message string) error {
	if err := r.repository.UpdateProgress(context.Background(), r.taskID, percent, message); err != nil {
		return err
	}

	if r.notifier != nil {
		_ = r.notifier.NotifyTaskEvent(context.Background(), r.task, models.TaskEventProgress, map[string]interface{}{ //nolint:errcheck
			"progress": percent,
			"message":  message,
		})
	}

	return nil
}

func (r *taskProgressReporter) ReportHeartbeat() error {
	return r.repository.UpdateHeartbeat(context.Background(), r.taskID)
}

func (r *taskProgressReporter) ReportCheckpoint(data []byte) error {
	return r.repository.SaveCheckpoint(context.Background(), r.taskID, data)
}

func (r *taskProgressReporter) ReportMetrics(metrics map[string]interface{}) error {
	if r.notifier != nil {
		return r.notifier.NotifyTaskEvent(context.Background(), r.task, models.TaskEventResource, metrics)
	}
	return nil
}

func (r *taskProgressReporter) ReportLog(level, message string, fields map[string]interface{}) error {
	data := map[string]interface{}{
		"level":   level,
		"message": message,
	}
	for k, v := range fields {
		data[k] = v
	}

	_ = r.repository.LogEvent(context.Background(), r.taskID, models.TaskEventLog, data, &r.workerID) //nolint:errcheck

	if r.notifier != nil {
		_ = r.notifier.NotifyTaskEvent(context.Background(), r.task, models.TaskEventLog, data) //nolint:errcheck
	}

	return nil
}

func min(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// Verify AdaptiveWorkerPool implements TaskWaiter
var _ TaskWaiter = (*AdaptiveWorkerPool)(nil)

// WaitForCompletion blocks until the task completes, fails, or times out
// progressCallback is called with progress updates during execution
func (wp *AdaptiveWorkerPool) WaitForCompletion(ctx context.Context, taskID string, timeout time.Duration, progressCallback func(progress float64, message string)) (*models.BackgroundTask, error) {
	startTime := time.Now()

	// Create a context with timeout
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Polling interval - start fast, then slow down
	pollInterval := 100 * time.Millisecond
	maxPollInterval := 2 * time.Second
	lastProgress := float64(-1)

	wp.logger.WithFields(logrus.Fields{
		"task_id": taskID,
		"timeout": timeout,
	}).Info("Waiting for task completion")

	for {
		select {
		case <-waitCtx.Done():
			if waitCtx.Err() == context.DeadlineExceeded {
				return nil, fmt.Errorf("timeout waiting for task %s after %v", taskID, timeout)
			}
			return nil, waitCtx.Err()
		case <-time.After(pollInterval):
			// Get current task state
			task, err := wp.repository.GetByID(ctx, taskID)
			if err != nil {
				return nil, fmt.Errorf("failed to get task %s: %w", taskID, err)
			}

			// Report progress if changed
			if progressCallback != nil && task.Progress != lastProgress {
				msg := ""
				if task.ProgressMessage != nil {
					msg = *task.ProgressMessage
				}
				progressCallback(task.Progress, msg)
				lastProgress = task.Progress
			}

			// Check if task is in a terminal state
			if isTerminalStatus(task.Status) {
				wp.logger.WithFields(logrus.Fields{
					"task_id":  taskID,
					"status":   task.Status,
					"duration": time.Since(startTime),
				}).Info("Task completed")

				if task.Status == models.TaskStatusFailed {
					errMsg := "unknown error"
					if task.ProgressMessage != nil {
						errMsg = *task.ProgressMessage
					}
					return task, fmt.Errorf("task failed: %s", errMsg)
				}
				if task.Status == models.TaskStatusCancelled {
					return task, fmt.Errorf("task was cancelled")
				}
				if task.Status == models.TaskStatusDeadLetter {
					return task, fmt.Errorf("task moved to dead letter queue")
				}

				return task, nil
			}

			// Adaptive polling - slow down over time
			if pollInterval < maxPollInterval {
				pollInterval = time.Duration(float64(pollInterval) * 1.5)
				if pollInterval > maxPollInterval {
					pollInterval = maxPollInterval
				}
			}
		}
	}
}

// WaitForCompletionWithOutput waits for task completion and returns captured output
func (wp *AdaptiveWorkerPool) WaitForCompletionWithOutput(ctx context.Context, taskID string, timeout time.Duration) (*models.BackgroundTask, []byte, error) {
	task, err := wp.WaitForCompletion(ctx, taskID, timeout, nil)
	if err != nil {
		return task, nil, err
	}

	// Get output from task result if available
	var output []byte
	if task != nil && task.Config.CaptureOutput {
		// Output would be stored in the task's checkpoint or a separate storage
		// For now, we return the progress message as output
		if task.ProgressMessage != nil {
			output = []byte(*task.ProgressMessage)
		}
	}

	return task, output, nil
}

// isTerminalStatus returns true if the status is a terminal state
func isTerminalStatus(status models.TaskStatus) bool {
	switch status {
	case models.TaskStatusCompleted,
		models.TaskStatusFailed,
		models.TaskStatusCancelled,
		models.TaskStatusDeadLetter:
		return true
	default:
		return false
	}
}

// WaitForMultiple waits for multiple tasks to complete
// Returns a map of taskID to WaitResult
func (wp *AdaptiveWorkerPool) WaitForMultiple(ctx context.Context, taskIDs []string, timeout time.Duration) map[string]*WaitResult {
	results := make(map[string]*WaitResult)
	resultChan := make(chan struct {
		id     string
		result *WaitResult
	}, len(taskIDs))

	// Wait for each task in parallel
	for _, taskID := range taskIDs {
		go func(id string) {
			startTime := time.Now()
			task, output, err := wp.WaitForCompletionWithOutput(ctx, id, timeout)
			resultChan <- struct {
				id     string
				result *WaitResult
			}{
				id: id,
				result: &WaitResult{
					Task:     task,
					Output:   output,
					Duration: time.Since(startTime),
					Error:    err,
				},
			}
		}(taskID)
	}

	// Collect results
	for range taskIDs {
		r := <-resultChan
		results[r.id] = r.result
	}

	return results
}
