// Package background provides background task execution for HelixAgent.
//
// This package implements a robust task queue system with worker pools,
// resource monitoring, and stuck task detection.
//
// # Background Task System
//
// Key components:
//
//   - TaskQueue: PostgreSQL-backed persistent queue
//   - WorkerPool: Concurrent task execution
//   - ResourceMonitor: System resource tracking
//   - StuckDetector: Detect and recover stuck tasks
//
// # Task Lifecycle
//
// Task states:
//
//	pending -> queued -> running -> completed/failed/stuck/cancelled
//
// # Task Queue
//
// Persistent task queue with PostgreSQL:
//
//	queue := background.NewTaskQueue(db, config)
//
//	// Submit task
//	taskID, err := queue.Submit(ctx, &Task{
//	    Type:     "llm_completion",
//	    Payload:  payload,
//	    Priority: background.PriorityNormal,
//	})
//
//	// Get task status
//	status, err := queue.GetStatus(ctx, taskID)
//
//	// Cancel task
//	err := queue.Cancel(ctx, taskID)
//
// # Worker Pool
//
// Concurrent task execution:
//
//	pool := background.NewWorkerPool(&WorkerPoolConfig{
//	    Workers:     10,
//	    QueueSize:   100,
//	    TaskTimeout: 5 * time.Minute,
//	})
//
//	// Register handler
//	pool.RegisterHandler("llm_completion", func(ctx context.Context, task *Task) error {
//	    // Process task
//	    return nil
//	})
//
//	// Start pool
//	pool.Start(ctx)
//
// # Resource Monitor
//
// Track system resources:
//
//	monitor := background.NewResourceMonitor(config)
//	monitor.Start(ctx)
//
//	// Check resources
//	if !monitor.HasCapacity() {
//	    // Pause task submission
//	}
//
//	// Get current usage
//	usage := monitor.GetUsage()
//	log.Printf("CPU: %.1f%%, Memory: %.1f%%", usage.CPU, usage.Memory)
//
// # Stuck Task Detection
//
// Detect and recover stuck tasks:
//
//	detector := background.NewStuckDetector(&DetectorConfig{
//	    CheckInterval: 1 * time.Minute,
//	    StuckThreshold: 10 * time.Minute,
//	    MaxRetries: 3,
//	})
//
//	detector.OnStuckTask(func(task *Task) {
//	    // Handle stuck task
//	    log.Warn("Stuck task detected:", task.ID)
//	    queue.Retry(ctx, task.ID)
//	})
//
//	detector.Start(ctx)
//
// # Task Events
//
// Subscribe to task events:
//
//	// Server-Sent Events
//	GET /v1/tasks/:id/events
//
//	// WebSocket
//	GET /v1/ws/tasks/:id
//
// Event types:
//   - task.created
//   - task.started
//   - task.progress
//   - task.completed
//   - task.failed
//   - task.cancelled
//
// # Task Types
//
// Built-in task types:
//
//   - llm_completion: LLM completion request
//   - debate: AI debate session
//   - embedding: Generate embeddings
//   - batch_process: Batch processing
//   - scheduled: Scheduled tasks
//
// # Priority Levels
//
// Task priority:
//
//	const (
//	    PriorityLow    = 0
//	    PriorityNormal = 5
//	    PriorityHigh   = 10
//	    PriorityCritical = 15
//	)
//
// Higher priority tasks are processed first.
//
// # Configuration
//
//	config := &background.Config{
//	    Workers:         10,
//	    QueueSize:       1000,
//	    TaskTimeout:     5 * time.Minute,
//	    RetryAttempts:   3,
//	    RetryDelay:      30 * time.Second,
//	    StuckThreshold:  10 * time.Minute,
//	    CleanupInterval: 1 * time.Hour,
//	    RetentionDays:   7,
//	}
//
// # Key Files
//
//   - task_queue.go: Task queue implementation
//   - worker_pool.go: Worker pool management
//   - resource_monitor.go: Resource tracking
//   - stuck_detector.go: Stuck task detection
//   - task.go: Task type definitions
//   - events.go: Event handling
//
// # Example: Submit and Monitor Task
//
//	// Submit task
//	taskID, err := queue.Submit(ctx, &Task{
//	    Type:    "llm_completion",
//	    Payload: map[string]interface{}{
//	        "prompt": "Hello, world!",
//	        "model":  "claude-3",
//	    },
//	    Priority: background.PriorityNormal,
//	})
//
//	// Monitor progress
//	for {
//	    status, _ := queue.GetStatus(ctx, taskID)
//	    if status.State == background.StateCompleted {
//	        fmt.Println("Result:", status.Result)
//	        break
//	    }
//	    if status.State == background.StateFailed {
//	        fmt.Println("Error:", status.Error)
//	        break
//	    }
//	    time.Sleep(1 * time.Second)
//	}
package background
