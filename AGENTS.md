# AGENTS.md - Background Tasks Module

## Module Overview

`digital.vasic.background` is a generic, reusable Go module for background task processing with persistence, resource monitoring, stuck detection, and event publishing. It provides a complete solution for managing long-running tasks in distributed systems.

**Module path**: `digital.vasic.background`
**Go version**: 1.25.3+
**Dependencies**: 
- `digital.vasic.models`: BackgroundTask type definitions
- `github.com/prometheus/client_golang`: Metrics collection
- `github.com/shirou/gopsutil/v3`: System resource monitoring
- `github.com/sirupsen/logrus`: Structured logging (optional)
- `github.com/google/uuid`: UUID generation

## Package Structure (Flat Layout)

The module uses a flat layout with all files in the root directory, organized by responsibility:

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
| `doc.go` | Package documentation |
| `CLAUDE.md` | AI coding assistant instructions |
| `README.md` | User-facing documentation with quick start |

## Dependency Graph

```
task_queue.go    --> models.BackgroundTask, EventPublisher
worker_pool.go   --> TaskQueue, TaskExecutor, ResourceMonitor, StuckDetector, EventPublisher
resource_monitor.go --> gopsutil, Prometheus metrics
stuck_detector.go --> TaskQueue, ResourceMonitor, EventPublisher
events.go        --> models.BackgroundTask, EventPublisher interface
messaging_adapter.go --> TaskQueue, EventPublisher
metrics.go       --> Prometheus client
```

`interfaces.go` defines the contracts; all implementations depend on these interfaces.

## Agent Coordination Guide

### Division of Work

When multiple agents work on this module simultaneously, divide work by component boundary:

1. **TaskQueue Agent** -- Owns `task_queue.go`. PostgreSQL implementation of TaskQueue interface. Changes affect persistence layer and database schema.
2. **WorkerPool Agent** -- Owns `worker_pool.go`. Worker pool orchestration, scaling, and task execution.
3. **ResourceMonitor Agent** -- Owns `resource_monitor.go`. System resource monitoring using gopsutil.
4. **StuckDetector Agent** -- Owns `stuck_detector.go`. Heuristic-based stuck task detection.
5. **Events Agent** -- Owns `events.go`, `event_publisher.go`, `messaging_adapter.go`. Event publishing abstraction and adapters.
6. **Metrics Agent** -- Owns `metrics.go`. Prometheus metrics collection.
7. **Interface Agent** -- Owns `interfaces.go`. Core interface definitions. Changes here affect all implementations.

### Coordination Rules

- **Interface changes** require all agents to update. The interfaces in `interfaces.go` are the shared contracts.
- **TaskQueue changes** that affect schema require migration scripts and coordination with deployments.
- **WorkerPool changes** that affect scaling logic may impact performance and require load testing.
- **EventPublisher interface** changes affect all event publishing components.
- **ResourceMonitor and StuckDetector** are mostly independent but share some heuristics.

### Safe Parallel Changes

These changes can be made simultaneously without coordination:
- Adding new metrics to `metrics.go`
- Adding new stuck detection heuristics to `stuck_detector.go`
- Adding new resource monitoring metrics to `resource_monitor.go`
- Adding new EventPublisher implementations in `event_publisher.go`
- Adding tests for any component
- Updating documentation

### Changes Requiring Coordination

- Modifying any interface in `interfaces.go` (TaskExecutor, TaskQueue, WorkerPool, ResourceMonitor, StuckDetector, EventPublisher)
- Changing PostgreSQL schema in `task_queue.go` (requires migration)
- Modifying worker pool scaling algorithm that could affect system stability
- Changing event types in `events.go` (affects downstream consumers)
- Modifying resource threshold defaults that could affect task scheduling

## Build and Test Commands

```bash
# Build the module
go build ./...

# Run all tests with race detection
go test ./... -count=1 -race

# Run unit tests only (short mode)
go test ./... -short

# Run integration tests (requires PostgreSQL)
go test -tags=integration ./...

# Run benchmarks
go test -bench=. ./tests/benchmark/

# Run a specific test
go test -v -run TestTaskQueue_Enqueue ./...

# Format code
gofmt -w .

# Vet code
go vet ./...

# Check for unused dependencies
go mod tidy
```

## Commit Conventions

Follow Conventional Commits with component scope:

```
feat(taskqueue): add batch enqueue support
feat(workerpool): implement dynamic scaling based on queue depth
feat(resourcemonitor): add disk I/O monitoring
feat(stuckdetector): add progress stall detection
feat(events): add task.cancelled event type
fix(taskqueue): fix deadlock in concurrent dequeue
test(workerpool): add scaling concurrency tests
docs(background): update configuration examples
refactor(interfaces): extract ProgressReporter interface
```

## Thread Safety Notes

- `PostgresTaskQueue` uses connection pooling and transactional operations for thread safety.
- `WorkerPool` uses `sync.RWMutex` for worker state and `sync.WaitGroup` for graceful shutdown.
- `ResourceMonitor` uses atomic operations for metric updates and `sync.Once` for initialization.
- `StuckDetector` uses `sync.Mutex` for detection state and periodic scanning.
- Event publishing uses channel-based or direct invocation depending on EventPublisher implementation.
- All public methods should be safe for concurrent invocation unless explicitly documented otherwise.

## Integration with HelixAgent

The BackgroundTasks module is extracted from HelixAgent's internal background package. Integration adapters are required:

1. **EventPublisher adapter** to connect to HelixAgent's messaging system (via `messaging_adapter.go`)
2. **TaskExecutor implementations** for HelixAgent-specific tasks (notification sending, LLM processing, etc.)
3. **ResourceMonitor integration** with HelixAgent's observability stack
4. **Metrics integration** with HelixAgent's Prometheus registry

See `messaging_adapter.go` for example integration patterns.

## Database Schema Management

When modifying `task_queue.go` schema:
1. Create migration script in `migrations/` directory
2. Update `initSchema` function with versioned schema
3. Add backward compatibility checks for existing deployments
4. Test migration with production-like data volumes

## Performance Considerations

- **TaskQueue throughput**: Monitor PostgreSQL connection pool size and query performance
- **WorkerPool scaling**: Adjust scale-up/down thresholds based on workload patterns
- **ResourceMonitor overhead**: Sampling interval affects system load; default 5s is conservative
- **StuckDetector frequency**: Detection intervals trade off responsiveness vs. overhead
- **Event publishing**: Choose appropriate EventPublisher implementation (sync vs async) based on latency requirements

## Testing Strategy

### Unit Tests
- Mock dependencies (database, system resources)
- Test interface implementations in isolation
- Table-driven tests for edge cases

### Integration Tests
- Requires PostgreSQL instance (use test containers)
- Test full task lifecycle
- Test concurrent access patterns

### Performance Tests
- Benchmark task throughput
- Measure resource usage under load
- Test scaling behavior

## Monitoring and Alerting

Key metrics to monitor:
- `background_queue_depth`: Alert if growing continuously
- `background_workers_active`: Alert if at max capacity
- `background_system_cpu_usage`: Alert if above threshold
- `background_stuck_tasks_total`: Alert if increasing
- `background_task_duration_seconds`: Alert on p95 spikes

## Deployment Guidelines

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
- PriorityClass for critical background tasks

## Security Considerations

- **SQL Injection**: Use parameterized queries (pgx)
- **Resource Exhaustion**: Limit concurrent tasks per worker
- **Data Privacy**: Encrypt sensitive task input/output
- **Access Control**: Validate task permissions before execution
- **Network Security**: Secure database and metrics endpoints