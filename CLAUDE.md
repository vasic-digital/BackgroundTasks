# CLAUDE.md - Background Tasks Module


## Definition of Done

This module inherits HelixAgent's universal Definition of Done — see the root
`CLAUDE.md` and `docs/development/definition-of-done.md`. In one line: **no
task is done without pasted output from a real run of the real system in the
same session as the change.** Coverage and green suites are not evidence.

### Acceptance demo for this module

<!-- TODO: replace this block with the exact command(s) that exercise this
     module end-to-end against real dependencies, and the expected output.
     The commands must run the real artifact (built binary, deployed
     container, real service) — no in-process fakes, no mocks, no
     `httptest.NewServer`, no Robolectric, no JSDOM as proof of done. -->

```bash
# TODO
```

## Overview

`digital.vasic.background` is a generic, reusable Go module for background task processing with persistence, resource monitoring, stuck detection, and event publishing. It provides a complete solution for managing long-running tasks in distributed systems.

**Module**: `digital.vasic.background` (Go 1.25.3+)

## Build & Test

```bash
go build ./...
go test ./... -count=1 -race
go test ./... -short              # Unit tests only
go test -tags=integration ./...   # Integration tests (requires PostgreSQL)
go test -bench=. ./tests/benchmark/
```

## Code Style

- Standard Go conventions, `gofmt` formatting
- Imports grouped: stdlib, third-party, internal (blank line separated)
- Line length <= 100 chars
- Naming: `camelCase` private, `PascalCase` exported, acronyms all-caps
- Errors: always check, wrap with `fmt.Errorf("...: %w", err)`
- Tests: table-driven, `testify`, naming `Test<Struct>_<Method>_<Scenario>`

## Package Structure

| File | Purpose |
|------|---------|
| `interfaces.go` | Core interfaces: TaskExecutor, TaskQueue, WorkerPool, ResourceMonitor, StuckDetector, EventPublisher |
| `task_queue.go` | PostgreSQL-based persistent task queue implementation |
| `worker_pool.go` | Worker pool with dynamic scaling and resource-aware task allocation |
| `resource_monitor.go` | System and process resource monitoring using gopsutil |
| `stuck_detector.go` | Stuck task detection using multiple heuristics (timeout, resource starvation, progress stall) |
| `events.go` | Task lifecycle events and event publishing abstraction |
| `event_publisher.go` | EventPublisher interface and implementations (NoOp, Logging) |
| `messaging_adapter.go` | Adapter for integrating TaskQueue with EventPublisher |
| `metrics.go` | Prometheus metrics collection for task execution and resource usage |

## Key Interfaces

### TaskQueue
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

### WorkerPool
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

### TaskExecutor
```go
type TaskExecutor interface {
    Execute(ctx context.Context, task *models.BackgroundTask, reporter ProgressReporter) error
    CanPause() bool
    Pause(ctx context.Context, task *models.BackgroundTask) ([]byte, error)
    Resume(ctx context.Context, task *models.BackgroundTask, checkpoint []byte) error
    Cancel(ctx context.Context, task *models.BackgroundTask) error
    GetResourceRequirements() ResourceRequirements
}
```

### EventPublisher
```go
type EventPublisher interface {
    Publish(ctx context.Context, event *BackgroundTaskEvent) error
}
```

## Dependencies

### External
- **PostgreSQL**: Persistent task storage (via pgx)
- **Prometheus**: Metrics collection (`github.com/prometheus/client_golang`)
- **gopsutil**: System resource monitoring (`github.com/shirou/gopsutil/v3`)
- **logrus**: Structured logging (`github.com/sirupsen/logrus`)

### Internal Modules
- `digital.vasic.models`: BackgroundTask type definitions
- `digital.vasic.concurrency`: Worker pool primitives (optional)

## Configuration

### PostgreSQL Task Queue
```go
config := &PostgresTaskQueueConfig{
    MaxConnections:      20,
    ConnectionTimeout:   30 * time.Second,
    StatementTimeout:    60 * time.Second,
    HealthCheckInterval: 10 * time.Second,
    EnableDeadLetter:    true,
    DeadLetterRetention: 7 * 24 * time.Hour,
}
```

### Worker Pool
```go
config := &WorkerPoolConfig{
    MinWorkers:          2,
    MaxWorkers:          10,
    ScaleUpThreshold:    0.8, // 80% utilization
    ScaleDownThreshold:  0.2, // 20% utilization
    ScaleCheckInterval:  30 * time.Second,
    MaxTaskDuration:     1 * time.Hour,
    EnableStuckDetection: true,
    StuckCheckInterval:  5 * time.Minute,
}
```

### Resource Monitor
```go
config := &ResourceMonitorConfig{
    SamplingInterval:    5 * time.Second,
    HistoryRetention:    1 * time.Hour,
    CPUThreshold:        0.9,  // 90% CPU usage triggers alerts
    MemoryThreshold:     0.85, // 85% memory usage triggers alerts
    EnableProcessStats:  true,
}
```

## Event Types

| Event Type | Description | When Published |
|------------|-------------|----------------|
| `task.created` | Task created and enqueued | On `Enqueue()` |
| `task.started` | Task started execution | On `Dequeue()` |
| `task.progress` | Task progress updated | On `ReportProgress()` |
| `task.completed` | Task completed successfully | On successful execution |
| `task.failed` | Task failed with error | On execution failure |
| `task.stuck` | Task detected as stuck | By stuck detector |
| `task.cancelled` | Task cancelled by user | On `Cancel()` |
| `task.retrying` | Task being retried | On `Requeue()` |
| `task.deadletter` | Task moved to dead letter queue | On `MoveToDeadLetter()` |

## Stuck Detection Heuristics

1. **Timeout**: Task running longer than type-specific threshold
2. **Resource Starvation**: Task consuming resources but not making progress
3. **Progress Stall**: No progress reported for configurable period
4. **Heartbeat Missing**: No heartbeat received from worker
5. **CPU/Memory Spike**: Abnormal resource usage patterns

## Metrics

Prometheus metrics exposed:
- `background_tasks_total` (counter): Total tasks processed
- `background_tasks_duration_seconds` (histogram): Task execution duration
- `background_queue_depth` (gauge): Number of pending tasks
- `background_workers_active` (gauge): Number of active workers
- `background_system_cpu_usage` (gauge): System CPU usage percentage
- `background_system_memory_usage` (gauge): System memory usage percentage

## Testing Strategy

### Unit Tests
- Mock dependencies (database, system resources)
- Test interface implementations in isolation
- Table-driven tests for edge cases

### Integration Tests
- Requires PostgreSQL instance
- Test full task lifecycle
- Test concurrent access patterns

### Performance Tests
- Benchmark task throughput
- Measure resource usage under load
- Test scaling behavior

## Error Handling

- **Transient Errors**: Retry with exponential backoff
- **Permanent Errors**: Move to dead letter queue
- **Resource Errors**: Scale workers or wait for resources
- **Database Errors**: Circuit breaker pattern for DB connections

## Security Considerations

- **SQL Injection**: Use parameterized queries (pgx)
- **Resource Exhaustion**: Limit concurrent tasks per worker
- **Data Privacy**: Encrypt sensitive task input/output
- **Access Control**: Validate task permissions before execution

## Deployment

### Containerized
```dockerfile
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o background-worker ./cmd/worker

FROM alpine:latest
COPY --from=builder /app/background-worker /usr/local/bin/
CMD ["background-worker"]
```

### Kubernetes
- Horizontal Pod Autoscaler based on queue depth
- Readiness probe on health check endpoint
- Liveness probe on worker pool status
- Resource limits based on task requirements

## Monitoring & Observability

- **Health Checks**: `/health` endpoint with DB connectivity check
- **Metrics**: Prometheus metrics endpoint `/metrics`
- **Logging**: Structured JSON logs with correlation IDs
- **Tracing**: OpenTelemetry spans for task execution

## Migration Notes

When updating the module:
1. Check for breaking changes in interfaces
2. Update database schema if needed (migrations in `task_queue.go`)
3. Test backward compatibility with existing tasks
4. Update dependent modules (HelixAgent, etc.)

## Contributing

1. Follow existing code style and patterns
2. Write comprehensive tests for new features
3. Update documentation (README, CLAUDE.md, AGENTS.md)
4. Run `go fmt`, `go vet`, `go test` before committing
5. Update CHANGELOG.md with changes

## Integration Seams

| Direction | Sibling modules |
|-----------|-----------------|
| Upstream (this module imports) | Concurrency, Models |
| Downstream (these import this module) | HelixLLM |

*Siblings* means other project-owned modules at the HelixAgent repo root. The root HelixAgent app and external systems are not listed here — the list above is intentionally scoped to module-to-module seams, because drift *between* sibling modules is where the "tests pass, product broken" class of bug most often lives. See root `CLAUDE.md` for the rules that keep these seams contract-tested.
