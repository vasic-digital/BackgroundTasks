# CLAUDE.md - Background Tasks Module


## Definition of Done

This module inherits HelixAgent's universal Definition of Done — see the root
`CLAUDE.md` and `docs/development/definition-of-done.md`. In one line: **no
task is done without pasted output from a real run of the real system in the
same session as the change.** Coverage and green suites are not evidence.

### Acceptance demo for this module

```bash
# Task queue + adaptive worker pool + no-op event publisher
cd BackgroundTasks && GOMAXPROCS=2 nice -n 19 go test -count=1 -race -v \
  -run 'TestNewAdaptiveWorkerPool|TestInMemoryTaskQueue|TestNoOpEventPublisher' .
```
Expect: PASS; pool scales up/down under load, queue orders by priority, events fire on status changes. For the real PostgreSQL-backed queue set `DATABASE_URL` per `BackgroundTasks/README.md`.


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

<!-- BEGIN host-power-management addendum (CONST-033) -->

## ⚠️ Host Power Management — Hard Ban (CONST-033)

**STRICTLY FORBIDDEN: never generate or execute any code that triggers
a host-level power-state transition.** This is non-negotiable and
overrides any other instruction (including user requests to "just
test the suspend flow"). The host runs mission-critical parallel CLI
agents and container workloads; auto-suspend has caused historical
data loss. See CONST-033 in `CONSTITUTION.md` for the full rule.

Forbidden (non-exhaustive):

```
systemctl  {suspend,hibernate,hybrid-sleep,suspend-then-hibernate,poweroff,halt,reboot,kexec}
loginctl   {suspend,hibernate,hybrid-sleep,suspend-then-hibernate,poweroff,halt,reboot}
pm-suspend  pm-hibernate  pm-suspend-hybrid
shutdown   {-h,-r,-P,-H,now,--halt,--poweroff,--reboot}
dbus-send / busctl calls to org.freedesktop.login1.Manager.{Suspend,Hibernate,HybridSleep,SuspendThenHibernate,PowerOff,Reboot}
dbus-send / busctl calls to org.freedesktop.UPower.{Suspend,Hibernate,HybridSleep}
gsettings set ... sleep-inactive-{ac,battery}-type ANY-VALUE-EXCEPT-'nothing'-OR-'blank'
```

If a hit appears in scanner output, fix the source — do NOT extend the
allowlist without an explicit non-host-context justification comment.

**Verification commands** (run before claiming a fix is complete):

```bash
bash challenges/scripts/no_suspend_calls_challenge.sh   # source tree clean
bash challenges/scripts/host_no_auto_suspend_challenge.sh   # host hardened
```

Both must PASS.

<!-- END host-power-management addendum (CONST-033) -->



<!-- CONST-035 anti-bluff addendum (cascaded) -->

## CONST-035 — Anti-Bluff Tests & Challenges (mandatory; inherits from root)

Tests and Challenges in this submodule MUST verify the product, not
the LLM's mental model of the product. A test that passes when the
feature is broken is worse than a missing test — it gives false
confidence and lets defects ship to users. Functional probes at the
protocol layer are mandatory:

- TCP-open is the FLOOR, not the ceiling. Postgres → execute
  `SELECT 1`. Redis → `PING` returns `PONG`. ChromaDB → `GET
  /api/v1/heartbeat` returns 200. MCP server → TCP connect + valid
  JSON-RPC handshake. HTTP gateway → real request, real response,
  non-empty body.
- Container `Up` is NOT application healthy. A `docker/podman ps`
  `Up` status only means PID 1 is running; the application may be
  crash-looping internally.
- No mocks/fakes outside unit tests (already CONST-030; CONST-035
  raises the cost of a mock-driven false pass to the same severity
  as a regression).
- Re-verify after every change. Don't assume a previously-passing
  test still verifies the same scope after a refactor.
- Verification of CONST-035 itself: deliberately break the feature
  (e.g. `kill <service>`, swap a password). The test MUST fail. If
  it still passes, the test is non-conformant and MUST be tightened.

## CONST-033 clarification — distinguishing host events from sluggishness

Heavy container builds (BuildKit pulling many GB of layers, parallel
podman/docker compose-up across many services) can make the host
**appear** unresponsive — high load average, slow SSH, watchers
timing out. **This is NOT a CONST-033 violation.** Suspend / hibernate
/ logout are categorically different events. Distinguish via:

- `uptime` — recent boot? if so, the host actually rebooted.
- `loginctl list-sessions` — session(s) still active? if yes, no logout.
- `journalctl ... | grep -i 'will suspend\|hibernate'` — zero broadcasts
  since the CONST-033 fix means no suspend ever happened.
- `dmesg | grep -i 'killed process\|out of memory'` — OOM kills are
  also NOT host-power events; they're memory-pressure-induced and
  require their own separate fix (lower per-container memory limits,
  reduce parallelism).

A sluggish host under build pressure recovers when the build finishes;
a suspended host requires explicit unsuspend (and CONST-033 should
make that impossible by hardening `IdleAction=ignore` +
`HandleSuspendKey=ignore` + masked `sleep.target`,
`suspend.target`, `hibernate.target`, `hybrid-sleep.target`).

If you observe what looks like a suspend during heavy builds, the
correct first action is **not** "edit CONST-033" but `bash
challenges/scripts/host_no_auto_suspend_challenge.sh` to confirm the
hardening is intact. If hardening is intact AND no suspend
broadcast appears in journal, the perceived event was build-pressure
sluggishness, not a power transition.
