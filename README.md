# digital.vasic.background

A generic, reusable Go module for background task processing with persistence, resource monitoring, stuck detection, and event publishing.

## Features

- **Task Queue**: PostgreSQL-based persistent task queue with priority support
- **Worker Pool**: Dynamic worker pool with resource-aware task allocation
- **Resource Monitoring**: Real-time CPU, memory, and system resource tracking
- **Stuck Detection**: Automatic detection of stuck tasks using multiple heuristics
- **Event Publishing**: Extensible event publishing for task lifecycle events
- **Pause/Resume**: Task checkpointing for pause and resume capabilities
- **Dead Letter Queue**: Failed task handling with dead-letter queue support
- **Progress Reporting**: Real-time progress reporting and heartbeat monitoring

## Installation

```bash
go get digital.vasic.background
```

## Quick Start

```go
package main

import (
	"context"
	"fmt"
	"time"

	"digital.vasic.background"
	"digital.vasic.models"
)

func main() {
	ctx := context.Background()

	// Create a PostgreSQL task queue
	queue, err := background.NewPostgresTaskQueue("postgres://user:pass@localhost/db")
	if err != nil {
		panic(err)
	}

	// Create a worker pool
	pool := background.NewWorkerPool(queue, 5) // 5 workers

	// Register task executors
	pool.RegisterExecutor("process_image", &ImageProcessor{})
	pool.RegisterExecutor("generate_report", &ReportGenerator{})

	// Start the worker pool
	if err := pool.Start(ctx); err != nil {
		panic(err)
	}
	defer pool.Stop(30 * time.Second)

	// Enqueue a task
	task := &models.BackgroundTask{
		ID:          "task-123",
		TaskType:    "process_image",
		TaskName:    "Process user upload",
		Priority:    models.TaskPriorityNormal,
		InputData:   []byte(`{"image_id": "img-456"}`),
	}

	if err := queue.Enqueue(ctx, task); err != nil {
		panic(err)
	}

	fmt.Println("Task enqueued successfully")
}

// ImageProcessor implements TaskExecutor
type ImageProcessor struct{}

func (p *ImageProcessor) Execute(ctx context.Context, task *models.BackgroundTask, reporter background.ProgressReporter) error {
	// Process the image
	reporter.ReportProgress(25, "Loading image")
	// ... image processing logic
	reporter.ReportProgress(100, "Completed")
	return nil
}

func (p *ImageProcessor) CanPause() bool { return true }
func (p *ImageProcessor) Pause(ctx context.Context, task *models.BackgroundTask) ([]byte, error) {
	return []byte("checkpoint"), nil
}
func (p *ImageProcessor) Resume(ctx context.Context, task *models.BackgroundTask, checkpoint []byte) error {
	return nil
}
func (p *ImageProcessor) Cancel(ctx context.Context, task *models.BackgroundTask) error {
	return nil
}
func (p *ImageProcessor) GetResourceRequirements() background.ResourceRequirements {
	return background.ResourceRequirements{
		CPUCores: 2,
		MemoryMB: 512,
		Priority: models.TaskPriorityNormal,
	}
}
```

## Core Components

### TaskQueue Interface
```go
type TaskQueue interface {
	Enqueue(ctx context.Context, task *models.BackgroundTask) error
	Dequeue(ctx context.Context, workerID string, requirements ResourceRequirements) (*models.BackgroundTask, error)
	Peek(ctx context.Context, count int) ([]*models.BackgroundTask, error)
	Requeue(ctx context.Context, taskID string, delay time.Duration) error
	MoveToDeadLetter(ctx context.Context, taskID string, reason string) error
	GetPendingCount(ctx context.Context) (int64, error)
	GetQueueDepth(ctx context.Context) (map[models.TaskPriority]int64, error)
}
```

### WorkerPool Interface
```go
type WorkerPool interface {
	Start(ctx context.Context) error
	Stop(gracePeriod time.Duration) error
	RegisterExecutor(taskType string, executor TaskExecutor)
	GetWorkerCount() int
	GetActiveTaskCount() int
	GetWorkerStatus() []WorkerStatus
	Scale(targetCount int) error
}
```

### ResourceMonitor Interface
```go
type ResourceMonitor interface {
	GetSystemResources() (*SystemResources, error)
	GetProcessResources(pid int) (*models.ResourceSnapshot, error)
	StartMonitoring(taskID string, pid int, interval time.Duration) error
	StopMonitoring(taskID string) error
	GetLatestSnapshot(taskID string) (*models.ResourceSnapshot, error)
	IsResourceAvailable(requirements ResourceRequirements) bool
}
```

### EventPublisher Interface
```go
type EventPublisher interface {
	Publish(ctx context.Context, event *BackgroundTaskEvent) error
}
```

## Configuration

### PostgreSQL Task Queue
```go
config := &background.PostgresTaskQueueConfig{
	MaxConnections:      20,
	ConnectionTimeout:   30 * time.Second,
	StatementTimeout:    60 * time.Second,
	HealthCheckInterval: 10 * time.Second,
}
queue := background.NewPostgresTaskQueueWithConfig("postgres://...", config)
```

### Worker Pool
```go
config := &background.WorkerPoolConfig{
	MinWorkers:          2,
	MaxWorkers:          10,
	ScaleUpThreshold:    0.8, // 80% utilization
	ScaleDownThreshold:  0.2, // 20% utilization
	ScaleCheckInterval:  30 * time.Second,
	MaxTaskDuration:     1 * time.Hour,
}
pool := background.NewWorkerPoolWithConfig(queue, config)
```

### Event Publishing
```go
// Use no-op publisher (discards events)
publisher := &background.NoOpEventPublisher{}

// Or use logging publisher
logger := &background.LoggingEventPublisher{Logger: myLogger}

// Or implement your own EventPublisher
type MyEventPublisher struct{}
func (p *MyEventPublisher) Publish(ctx context.Context, event *background.BackgroundTaskEvent) error {
	// Send to Kafka, RabbitMQ, etc.
	return nil
}
```

## Dependencies

- **PostgreSQL**: For persistent task queue (pgx driver)
- **Prometheus**: For metrics collection
- **gopsutil**: For system resource monitoring
- **logrus**: For structured logging

## Testing

Run all tests:
```bash
cd BackgroundTasks
go test ./... -count=1 -race
```

## License

MIT