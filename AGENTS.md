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
<!-- BEGIN host-power-management addendum (CONST-033) -->

## Host Power Management — Hard Ban (CONST-033)

**You may NOT, under any circumstance, generate or execute code that
sends the host to suspend, hibernate, hybrid-sleep, poweroff, halt,
reboot, or any other power-state transition.** This rule applies to:

- Every shell command you run via the Bash tool.
- Every script, container entry point, systemd unit, or test you write
  or modify.
- Every CLI suggestion, snippet, or example you emit.

**Forbidden invocations** (non-exhaustive — see CONST-033 in
`CONSTITUTION.md` for the full list):

- `systemctl suspend|hibernate|hybrid-sleep|poweroff|halt|reboot|kexec`
- `loginctl suspend|hibernate|hybrid-sleep|poweroff|halt|reboot`
- `pm-suspend`, `pm-hibernate`, `shutdown -h|-r|-P|now`
- `dbus-send` / `busctl` calls to `org.freedesktop.login1.Manager.Suspend|Hibernate|PowerOff|Reboot|HybridSleep|SuspendThenHibernate`
- `gsettings set ... sleep-inactive-{ac,battery}-type` to anything but `'nothing'` or `'blank'`

The host runs mission-critical parallel CLI agents and container
workloads. Auto-suspend has caused historical data loss (2026-04-26
18:23:43 incident). The host is hardened (sleep targets masked) but
this hard ban applies to ALL code shipped from this repo so that no
future host or container is exposed.

**Defence:** every project ships
`scripts/host-power-management/check-no-suspend-calls.sh` (static
scanner) and
`challenges/scripts/no_suspend_calls_challenge.sh` (challenge wrapper).
Both MUST be wired into the project's CI / `run_all_challenges.sh`.

**Full background:** `docs/HOST_POWER_MANAGEMENT.md` and `CONSTITUTION.md` (CONST-033).

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

<!-- BEGIN no-session-termination addendum (CONST-036) -->

## User-Session Termination — Hard Ban (CONST-036)

**You may NOT, under any circumstance, generate or execute code that
ends the currently-logged-in user's desktop session, kills their
`user@<UID>.service` user manager, or indirectly forces them to
manually log out / power off.** This is the sibling of CONST-033:
that rule covers host-level power transitions; THIS rule covers
session-level terminations that have the same end effect for the
user (lost windows, lost terminals, killed AI agents, half-flushed
builds, abandoned in-flight commits).

**Why this rule exists.** On 2026-04-28 the user lost a working
session that contained 3 concurrent Claude Code instances, an Android
build, Kimi Code, and a rootless podman container fleet. The
`user.slice` consumed 60.6 GiB peak / 5.2 GiB swap, the GUI became
unresponsive, the user was forced to log out and then power off via
the GNOME shell. The host could not auto-suspend (CONST-033 was in
place and verified) and the kernel OOM killer never fired — but the
user had to manually end the session anyway, because nothing
prevented overlapping heavy workloads from saturating the slice.
CONST-036 closes that loophole at both the source-code layer and the
operational layer. See
`docs/issues/fixed/SESSION_LOSS_2026-04-28.md` in the HelixAgent
project.

**Forbidden direct invocations** (non-exhaustive):

- `loginctl terminate-user|terminate-session|kill-user|kill-session`
- `systemctl stop user@<UID>` / `systemctl kill user@<UID>`
- `gnome-session-quit`
- `pkill -KILL -u $USER` / `killall -u $USER`
- `dbus-send` / `busctl` calls to `org.gnome.SessionManager.Logout|Shutdown|Reboot`
- `echo X > /sys/power/state`
- `/usr/bin/poweroff`, `/usr/bin/reboot`, `/usr/bin/halt`

**Indirect-pressure clauses:**

1. Do not spawn parallel heavy workloads casually; check `free -h`
   first; keep `user.slice` under 70% of physical RAM.
2. Long-lived background subagents go in `system.slice`. Rootless
   podman containers die with the user manager.
3. Document AI-agent concurrency caps in CLAUDE.md.
4. Never script "log out and back in" recovery flows.

**Defence:** every project ships
`scripts/host-power-management/check-no-session-termination-calls.sh`
(static scanner) and
`challenges/scripts/no_session_termination_calls_challenge.sh`
(challenge wrapper). Both MUST be wired into the project's CI /
`run_all_challenges.sh`.

<!-- END no-session-termination addendum (CONST-036) -->
