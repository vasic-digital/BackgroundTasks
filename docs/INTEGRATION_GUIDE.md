# BackgroundTasks Module - Integration Guide

**Module:** `digital.vasic.background`
**Last Updated:** March 2026

---

## Overview

This guide explains how to integrate the BackgroundTasks module into HelixAgent or any Go application that needs persistent background task processing with resource monitoring and event publishing.

---

## Prerequisites

- Go 1.25.3+
- PostgreSQL 15+ (for persistent task queue)
- Optional: Kafka/RabbitMQ (for event publishing)
- Optional: Prometheus (for metrics collection)

---

## Step 1: Implement a TaskExecutor

Every task type needs a `TaskExecutor` implementation. The executor handles actual task logic, pause/resume, cancellation, and resource requirements.

```go
package executors

import (
    "context"
    "encoding/json"
    "fmt"

    "digital.vasic.background"
    "digital.vasic.models"
)

// LLMCallExecutor processes LLM API calls in the background.
type LLMCallExecutor struct {
    providerRegistry ProviderRegistry
}

func NewLLMCallExecutor(registry ProviderRegistry) *LLMCallExecutor {
    return &LLMCallExecutor{providerRegistry: registry}
}

func (e *LLMCallExecutor) Execute(
    ctx context.Context,
    task *models.BackgroundTask,
    reporter background.ProgressReporter,
) error {
    // Parse task input
    var input LLMCallInput
    if err := json.Unmarshal(task.InputData, &input); err != nil {
        return fmt.Errorf("invalid input data: %w", err)
    }

    // Report initial progress
    _ = reporter.ReportProgress(10, "Starting LLM call")

    // Send heartbeat periodically
    _ = reporter.ReportHeartbeat()

    // Get the provider
    provider, err := e.providerRegistry.GetProvider(input.ProviderID)
    if err != nil {
        return fmt.Errorf("provider not found: %w", err)
    }

    // Execute the call
    _ = reporter.ReportProgress(50, "Calling provider")
    resp, err := provider.Complete(ctx, input.Request)
    if err != nil {
        return fmt.Errorf("LLM call failed: %w", err)
    }

    // Report completion with metrics
    _ = reporter.ReportMetrics(map[string]interface{}{
        "tokens_used": resp.TokensUsed,
        "latency_ms":  resp.LatencyMs,
    })
    _ = reporter.ReportProgress(100, "Completed")

    return nil
}

func (e *LLMCallExecutor) CanPause() bool {
    return false // LLM calls cannot be paused mid-request
}

func (e *LLMCallExecutor) Pause(ctx context.Context, task *models.BackgroundTask) ([]byte, error) {
    return nil, fmt.Errorf("LLM calls do not support pause")
}

func (e *LLMCallExecutor) Resume(ctx context.Context, task *models.BackgroundTask, checkpoint []byte) error {
    return fmt.Errorf("LLM calls do not support resume")
}

func (e *LLMCallExecutor) Cancel(ctx context.Context, task *models.BackgroundTask) error {
    // Context cancellation handles this
    return nil
}

func (e *LLMCallExecutor) GetResourceRequirements() background.ResourceRequirements {
    return background.ResourceRequirements{
        CPUCores: 1,
        MemoryMB: 256,
        Priority: models.TaskPriorityNormal,
    }
}
```

---

## Step 2: Implement a TaskRepository

The `TaskRepository` interface handles all database operations. In HelixAgent, this is implemented using PostgreSQL via pgx.

```go
// Simplified example -- HelixAgent's implementation is in internal/database/
type PostgresRepository struct {
    pool *pgxpool.Pool
}

func (r *PostgresRepository) Create(ctx context.Context, task *models.BackgroundTask) error {
    _, err := r.pool.Exec(ctx,
        `INSERT INTO background_tasks (id, task_type, task_name, status, priority,
         input_data, scheduled_at, created_at, updated_at)
         VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
        task.ID, task.TaskType, task.TaskName, task.Status, task.Priority,
        task.InputData, task.ScheduledAt, task.CreatedAt, task.UpdatedAt,
    )
    return err
}

func (r *PostgresRepository) Dequeue(ctx context.Context, workerID string, maxCPU, maxMem int) (*models.BackgroundTask, error) {
    row := r.pool.QueryRow(ctx,
        `UPDATE background_tasks
         SET status = 'running', worker_id = $1, started_at = NOW(), last_heartbeat = NOW()
         WHERE id = (
             SELECT id FROM background_tasks
             WHERE status = 'pending' AND scheduled_at <= NOW()
             ORDER BY priority DESC, created_at ASC
             FOR UPDATE SKIP LOCKED
             LIMIT 1
         )
         RETURNING *`,
        workerID,
    )
    // Scan and return...
}
```

---

## Step 3: Wire Components Together

```go
package main

import (
    "context"
    "time"

    "digital.vasic.background"
    "github.com/sirupsen/logrus"
)

func setupBackgroundTasks(repo background.TaskRepository) {
    logger := logrus.New()

    // 1. Create the task queue
    queue := background.NewPostgresTaskQueue(repo, logger)

    // 2. Create the event publisher
    publisher := &background.NoOpEventPublisher{} // or KafkaPublisher
    eventPub := background.NewTaskEventPublisher(
        publisher,
        logger,
        background.DefaultTaskEventPublisherConfig(),
    )
    eventPub.Start()
    defer eventPub.Stop()

    // 3. Create the worker pool (implementation in worker_pool.go)
    // pool := background.NewWorkerPoolImpl(queue, logger, config)

    // 4. Register executors for each task type
    // pool.RegisterExecutor("llm_call", NewLLMCallExecutor(registry))
    // pool.RegisterExecutor("debate", NewDebateExecutor(debateService))
    // pool.RegisterExecutor("embedding", NewEmbeddingExecutor(embeddingService))

    // 5. Start the pool
    ctx := context.Background()
    // pool.Start(ctx)
    // defer pool.Stop(30 * time.Second)
}
```

---

## Step 4: Enqueue Tasks

```go
func enqueueTask(ctx context.Context, queue background.TaskQueue) error {
    task := &models.BackgroundTask{
        ID:        "task-" + uuid.New().String(),
        TaskType:  "llm_call",
        TaskName:  "Process user request",
        Priority:  models.TaskPriorityNormal,
        InputData: []byte(`{"provider_id": "claude", "prompt": "Hello"}`),
        Config: models.TaskConfig{
            MaxRetries:        3,
            StuckThresholdSecs: 180,
            Endless:           false,
        },
    }

    return queue.Enqueue(ctx, task)
}
```

---

## Step 5: Configure Stuck Detection

```go
// Create a stuck detector with custom thresholds
detector := background.NewDefaultStuckDetector(logger)

// Override thresholds for specific task types
detector.SetThreshold("debate", 15*time.Minute)     // Debates take longer
detector.SetThreshold("embedding", 1*time.Minute)   // Embeddings should be fast
detector.SetThreshold("endless", 0)                  // Disable for endless tasks
```

---

## Step 6: Implement Event Publishing

### Kafka Integration

```go
type KafkaEventPublisher struct {
    writer *kafka.Writer
}

func (p *KafkaEventPublisher) Publish(ctx context.Context, event *background.BackgroundTaskEvent) error {
    data, err := json.Marshal(event)
    if err != nil {
        return err
    }

    return p.writer.WriteMessages(ctx, kafka.Message{
        Topic: event.EventType.Topic(),
        Key:   []byte(event.TaskID),
        Value: data,
    })
}
```

### Logging Publisher

```go
type LoggingEventPublisher struct {
    logger *logrus.Logger
}

func (p *LoggingEventPublisher) Publish(ctx context.Context, event *background.BackgroundTaskEvent) error {
    p.logger.WithFields(logrus.Fields{
        "event_type": event.EventType,
        "task_id":    event.TaskID,
        "task_type":  event.TaskType,
        "status":     event.Status,
        "progress":   event.Progress,
    }).Info("Task event")
    return nil
}
```

---

## Step 7: Monitor Queue Health

```go
// Get queue statistics
stats, err := queue.GetStats(ctx)
if err != nil {
    log.Printf("Failed to get queue stats: %v", err)
}

fmt.Printf("Pending: %d, Running: %d\n", stats.PendingCount, stats.RunningCount)
for priority, count := range stats.DepthByPriority {
    fmt.Printf("  Priority %s: %d\n", priority, count)
}
```

---

## Integration with HelixAgent

In HelixAgent, the BackgroundTasks module is integrated through:

- **`internal/background/`** -- Adapter layer connecting the module to HelixAgent's services
- **`internal/handlers/background_task_handler.go`** -- REST API endpoints for task management (`/v1/tasks`)
- **`internal/services/boot_manager.go`** -- Worker pool started during HelixAgent boot
- **`internal/notifications/`** -- SSE and WebSocket notifications for task progress

**REST API Endpoints:**

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/tasks` | Enqueue a new task |
| `GET` | `/v1/tasks/:id` | Get task status |
| `GET` | `/v1/tasks` | List tasks (with filtering) |
| `DELETE` | `/v1/tasks/:id` | Cancel a task |
| `POST` | `/v1/tasks/:id/pause` | Pause a task |
| `POST` | `/v1/tasks/:id/resume` | Resume a paused task |

---

## Error Handling Patterns

### Transient Errors (Retry)

```go
func (e *MyExecutor) Execute(ctx context.Context, task *models.BackgroundTask, reporter background.ProgressReporter) error {
    err := doWork(ctx)
    if isTransient(err) {
        // Return the error -- the worker pool will requeue with backoff
        return fmt.Errorf("transient error, will retry: %w", err)
    }
    return err
}
```

### Permanent Errors (Dead Letter)

Tasks that exceed their maximum retry count are automatically moved to the dead letter queue. The dead letter reason includes the last error message.

### Resource Errors

When `ResourceMonitor.IsResourceAvailable()` returns false, workers pause dequeuing until resources are available. This prevents overloading the host system.
