# BackgroundTasks Module - Architecture

**Module:** `digital.vasic.background`
**Version:** 1.0.0
**Last Updated:** March 2026

---

## Design Philosophy

The BackgroundTasks module provides a **complete background task processing system** with persistence, resource awareness, stuck detection, and event publishing. It is designed to:

1. **Persist tasks** -- PostgreSQL-backed queue survives process restarts.
2. **Monitor resources** -- CPU, memory, disk, and I/O tracked per-task.
3. **Detect stuck tasks** -- Multiple heuristics identify frozen, starved, or leaking processes.
4. **Publish events** -- Full task lifecycle events for observability and integration.
5. **Support pause/resume** -- Checkpoint-based pause and resume for long-running tasks.
6. **Scale dynamically** -- Worker pool scales based on utilization thresholds.

---

## High-Level Architecture

```
+-------------------------------------------------------------------+
|                        Application Layer                           |
|                                                                    |
|  HTTP Handlers / Debate Service / CLI Commands                     |
+-------------------------------------------------------------------+
         |                                        |
         v                                        v
+------------------+                    +--------------------+
|   TaskQueue      |                    | TaskEventPublisher |
|  (PostgreSQL or  |                    | (async/sync)       |
|   In-Memory)     |                    +--------------------+
+------------------+                              |
         |                                        v
         v                              +--------------------+
+------------------+                    | EventPublisher     |
|   WorkerPool     |                    | (Kafka, logging,   |
|   (dynamic       |                    |  or no-op)         |
|    scaling)      |                    +--------------------+
+------------------+
    |          |
    v          v
+--------+ +------------------+
|Worker 1| |Worker N          |
|        | |                  |
| TaskExecutor.Execute()      |
|  |                          |
|  +-> ProgressReporter       |
|  +-> Heartbeat              |
|  +-> Checkpoint             |
+--------+ +------------------+
    |          |
    v          v
+------------------+    +------------------+
| ResourceMonitor  |    | StuckDetector    |
| (gopsutil)       |    | (5 heuristics)   |
+------------------+    +------------------+
```

---

## Core Components

### Task Queue

The task queue manages task persistence and atomic claim operations. Two implementations exist:

**PostgresTaskQueue** -- Production implementation backed by PostgreSQL via the `TaskRepository` interface. Features include:
- Priority-based ordering (critical > high > normal > low)
- Atomic dequeue with `SELECT ... FOR UPDATE SKIP LOCKED`
- Resource-aware dequeue (respects CPU/memory requirements)
- Dead letter queue for permanently failed tasks
- In-memory queue depth cache with 5-second TTL

**InMemoryTaskQueue** -- Testing implementation with the same interface. Provides:
- Priority-sorted queue using bubble sort on priority weight
- Scheduled task support (tasks with future `ScheduledAt` are skipped)
- Resource requirement matching during dequeue

### Worker Pool

The worker pool manages a fleet of workers that consume tasks from the queue.

**Scaling Model:**

```
                    ScaleUpThreshold (0.8)
                         |
   [SCALE UP] <----------+
                         |
   utilization = active_tasks / max_workers
                         |
   [SCALE DOWN] <--------+
                         |
                    ScaleDownThreshold (0.2)
```

**Configuration:**

| Parameter | Default | Description |
|-----------|---------|-------------|
| `MinWorkers` | 2 | Minimum workers maintained |
| `MaxWorkers` | 10 | Maximum workers allowed |
| `ScaleUpThreshold` | 0.8 | Scale up when utilization exceeds 80% |
| `ScaleDownThreshold` | 0.2 | Scale down when utilization drops below 20% |
| `ScaleCheckInterval` | 30s | How often to evaluate scaling |
| `MaxTaskDuration` | 1h | Maximum allowed task execution time |
| `EnableStuckDetection` | true | Enable stuck task detection |
| `StuckCheckInterval` | 5m | How often to check for stuck tasks |

**Worker Lifecycle:**

```
  IDLE -----(dequeue task)-----> BUSY
   ^                               |
   |                               v
   +-----(task completes/fails)----+
   |                               |
   +-----(Stop() called)----> STOPPING ---> STOPPED
```

Each worker reports its status via `WorkerStatus`:
- `id` -- Unique worker identifier
- `status` -- Current state (idle, busy, stopping, stopped)
- `current_task` -- Task currently being executed (if busy)
- `tasks_completed` / `tasks_failed` -- Lifetime counters
- `avg_task_duration` -- Rolling average execution time

### Resource Monitor

The resource monitor uses `gopsutil` to track system and per-process resource usage.

**System Resources Tracked:**
- Total and available CPU cores
- CPU load percentage
- Total and available memory (MB)
- Memory and disk usage percentages
- Load averages (1m, 5m, 15m)

**Per-Process Resources (ResourceSnapshot):**
- CPU percent and user/system time
- Memory RSS bytes and percent
- Open file descriptors and thread count
- I/O read/write bytes
- Network connections, bytes sent/received
- Process state

**Resource-Aware Scheduling:** Before dequeuing a task, the worker pool checks whether the system has sufficient resources to satisfy the task's `ResourceRequirements` (CPU cores, memory MB, disk MB, GPU count).

### Stuck Detector

The stuck detector uses five heuristics to identify tasks that are no longer making progress.

**Heuristic 1: Heartbeat Timeout**
Each task type has a configurable timeout threshold. If no heartbeat is received within the threshold, the task is considered stuck.

| Task Type | Default Threshold |
|-----------|-------------------|
| `default` | 5 minutes |
| `command` | 3 minutes |
| `llm_call` | 3 minutes |
| `debate` | 10 minutes |
| `embedding` | 2 minutes |
| `endless` | Disabled (0) |

**Heuristic 2: Frozen Process**
If CPU usage is below 0.1% across 3+ consecutive snapshots and CPU time has not increased, the process appears frozen.

**Heuristic 3: Resource Exhaustion**
- Memory usage > 95%
- Open file descriptors > 10,000
- Thread count > 1,000

**Heuristic 4: I/O Starvation**
No I/O operations detected across multiple snapshots while CPU activity is low but nonzero, indicating the process may be waiting on a blocked I/O operation.

**Heuristic 5: Network Hang**
Active network connections exist but no bytes are being sent or received, indicating a potential network hang (e.g., waiting for a response that will never come).

**Additional Detection: Memory Leak**
If memory RSS is monotonically increasing across 80%+ of snapshots with >50% growth rate, a potential memory leak is flagged.

**Endless Tasks:** Tasks configured with `Config.Endless = true` use a separate detection path that only flags zombie processes, critical memory exhaustion (>98%), or complete activity halt.

**Detailed Analysis:** The `AnalyzeTask` method provides a comprehensive `StuckAnalysis` struct with heartbeat status, resource status, activity status, and actionable recommendations.

### Event Publishing

The event system provides full task lifecycle observability through 14 event types.

**Event Types:**

| Event Type | Published When |
|------------|----------------|
| `task.created` | Task enqueued |
| `task.started` | Worker claims task |
| `task.progress` | Progress reported |
| `task.heartbeat` | Heartbeat received |
| `task.paused` | Task paused with checkpoint |
| `task.resumed` | Task resumed from checkpoint |
| `task.completed` | Successful execution |
| `task.failed` | Execution error |
| `task.stuck` | Stuck detector triggered |
| `task.cancelled` | User cancellation |
| `task.retrying` | Task requeued for retry |
| `task.deadletter` | Moved to dead letter queue |
| `task.log` | Task log entry |
| `task.resource` | Resource snapshot recorded |

**Publishing Modes:**
- **Synchronous** -- Events published inline (blocks until published)
- **Asynchronous** -- Events buffered in a channel (default buffer: 1000). A background goroutine drains the buffer. Falls back to synchronous when the buffer is full.

**Kafka Topic Routing:** Each event type maps to a specific Kafka topic under `helixagent.events.tasks.*` for fine-grained consumption.

**BackgroundTaskEvent** includes correlation ID and trace ID fields for distributed tracing integration with OpenTelemetry.

---

## Task Lifecycle

```
                                              +---> DEAD_LETTER
                                              |     (permanent failure)
                                              |
  PENDING ----> RUNNING ----> COMPLETED       |
     ^            |                           |
     |            +----> FAILED ----> RETRYING (delay) ---+
     |            |                                       |
     |            +----> STUCK ----> CANCELLED            |
     |            |                                       |
     |            +----> PAUSED ----> RUNNING (resume)    |
     |                                                    |
     +----------------------------------------------------+
           (requeue with delay)
```

---

## Prometheus Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `background_tasks_total` | Counter | Total tasks processed by type and status |
| `background_tasks_duration_seconds` | Histogram | Task execution duration |
| `background_queue_depth` | Gauge | Number of pending tasks by priority |
| `background_workers_active` | Gauge | Number of active workers |
| `background_system_cpu_usage` | Gauge | System CPU usage percentage |
| `background_system_memory_usage` | Gauge | System memory usage percentage |

---

## File Structure

```
BackgroundTasks/
  interfaces.go               -- Core interfaces (TaskExecutor, TaskQueue, WorkerPool, etc.)
  task_queue.go                -- PostgresTaskQueue and InMemoryTaskQueue
  worker_pool.go               -- Worker pool with dynamic scaling
  resource_monitor.go          -- System and process resource monitoring
  stuck_detector.go            -- 5-heuristic stuck detection
  events.go                    -- Event types, TaskEventPublisher
  event_publisher.go           -- EventPublisher interface and implementations
  messaging_adapter.go         -- Messaging system adapter
  metrics.go                   -- Prometheus metrics
  doc.go                       -- Package documentation
  *_test.go                    -- Test files
```
