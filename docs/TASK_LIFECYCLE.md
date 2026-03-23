# BackgroundTasks Module - Task Lifecycle

**Module:** `digital.vasic.background`
**Last Updated:** March 2026

---

## Overview

Every background task follows a well-defined lifecycle from creation to completion (or failure). This document describes all task states, transitions, persistence, and the events published at each stage.

---

## Task States

| State | Description |
|-------|-------------|
| `pending` | Task is in the queue, waiting to be claimed by a worker. |
| `running` | Task has been claimed by a worker and is actively executing. |
| `completed` | Task finished successfully. |
| `failed` | Task encountered an error during execution. |
| `cancelled` | Task was cancelled by a user or system action. |
| `paused` | Task was paused and a checkpoint was saved. |
| `stuck` | Task was detected as stuck by the stuck detector. |
| `retrying` | Task is being requeued after a failure (transient state). |
| `dead_letter` | Task permanently failed and was moved to the dead letter queue. |

---

## State Transition Diagram

```
                          +---> COMPLETED
                          |
  PENDING ---> RUNNING ---+---> FAILED ---> RETRYING ---> PENDING
     ^            |       |                     |
     |            |       +---> STUCK           +---> DEAD_LETTER
     |            |       |
     |            |       +---> CANCELLED
     |            |
     |            +---> PAUSED ---> RUNNING (resume)
     |                      |
     |                      +---> CANCELLED
     |
     +--- (requeue from RETRYING with delay)
```

---

## Detailed State Transitions

### PENDING -> RUNNING

**Trigger:** A worker calls `TaskQueue.Dequeue()`, which atomically claims the task.

**Database Update:**
- `status` = `running`
- `worker_id` = claiming worker's ID
- `started_at` = current timestamp
- `last_heartbeat` = current timestamp

**Event Published:** `task.started`

**Conditions:**
- Task's `scheduled_at` must be in the past (or now)
- Worker must have sufficient resources for the task's `ResourceRequirements`
- Task must be in `pending` status

### RUNNING -> COMPLETED

**Trigger:** `TaskExecutor.Execute()` returns `nil` (no error).

**Database Update:**
- `status` = `completed`
- `completed_at` = current timestamp
- `progress` = 100.0
- `output_data` = serialized result (if any)

**Event Published:** `task.completed` (includes result metadata)

### RUNNING -> FAILED

**Trigger:** `TaskExecutor.Execute()` returns a non-nil error.

**Database Update:**
- `status` = `failed`
- `last_error` = error message
- `completed_at` = current timestamp

**Event Published:** `task.failed` (includes error details)

**Next Step:** The worker pool checks whether the task should be retried or moved to dead letter.

### FAILED -> RETRYING

**Trigger:** Task's `retry_count` < `max_retries` from task config.

**Database Update:**
- `status` = `pending`
- `worker_id` = NULL
- `started_at` = NULL
- `last_heartbeat` = NULL
- `retry_count` = incremented
- `scheduled_at` = current time + retry delay

**Event Published:** `task.retrying` (includes retry delay and next attempt time)

**Retry Delay Calculation:**
```
delay = base_delay * 2^(retry_count - 1)
```
The delay increases exponentially with each retry to avoid thundering herd effects.

### FAILED -> DEAD_LETTER

**Trigger:** Task's `retry_count` >= `max_retries`, or the error is classified as permanent.

**Database Update:**
- `status` = `dead_letter`
- `last_error` = reason for dead lettering

**Event Published:** `task.deadletter` (includes reason)

Dead letter tasks are retained for debugging and analysis. They can be manually requeued by an operator if the root cause is resolved.

### RUNNING -> PAUSED

**Trigger:** User or system requests pause, and `TaskExecutor.CanPause()` returns `true`.

**Process:**
1. `TaskExecutor.Pause()` is called, which returns checkpoint data.
2. Checkpoint is saved to the database.
3. Task status is updated.

**Database Update:**
- `status` = `paused`
- `checkpoint_data` = serialized checkpoint bytes
- `worker_id` = NULL

**Event Published:** `task.paused`

### PAUSED -> RUNNING (Resume)

**Trigger:** User or system requests resume.

**Process:**
1. Task is dequeued by a worker.
2. `TaskExecutor.Resume()` is called with the saved checkpoint.
3. Execution continues from the checkpoint.

**Database Update:**
- `status` = `running`
- `worker_id` = new worker's ID
- `started_at` = current timestamp

**Event Published:** `task.resumed`

### RUNNING -> STUCK

**Trigger:** The `StuckDetector.IsStuck()` returns `true` based on one or more heuristics.

**Detection Heuristics:**
1. No heartbeat received within the threshold period
2. Process appears frozen (zero CPU activity)
3. Resource exhaustion (memory > 95%, FDs > 10,000, threads > 1,000)
4. I/O starvation (no I/O operations despite low CPU)
5. Network hang (connections open but no data transfer)

**Event Published:** `task.stuck` (includes reason from detector)

**Recovery Options:**
- Automatic cancellation and requeue
- Manual operator intervention
- Escalation via notification system

### RUNNING -> CANCELLED / PAUSED -> CANCELLED

**Trigger:** User sends a cancel request, or the system decides to cancel (e.g., after stuck detection).

**Process:**
1. `TaskExecutor.Cancel()` is called for graceful cleanup.
2. The task's context is cancelled.
3. Status is updated.

**Database Update:**
- `status` = `cancelled`
- `completed_at` = current timestamp

**Event Published:** `task.cancelled`

---

## Progress Reporting

During execution, tasks report progress through the `ProgressReporter` interface.

### ReportProgress

```go
reporter.ReportProgress(50.0, "Processing batch 5 of 10")
```

Updates the task's `progress` (0-100) and `progress_message` fields. Publishes a `task.progress` event.

### ReportHeartbeat

```go
reporter.ReportHeartbeat()
```

Updates the task's `last_heartbeat` timestamp. This prevents the stuck detector from flagging the task. Should be called periodically (recommended every 30-60 seconds for long-running tasks).

### ReportCheckpoint

```go
checkpoint, _ := json.Marshal(currentState)
reporter.ReportCheckpoint(checkpoint)
```

Saves a checkpoint that can be used for pause/resume. Useful for tasks that process items in batches -- the checkpoint stores the last processed item.

### ReportMetrics

```go
reporter.ReportMetrics(map[string]interface{}{
    "items_processed": 500,
    "errors_skipped":  3,
    "cache_hit_rate":  0.85,
})
```

Reports custom metrics from the task. These are stored in the task's metadata and included in events for monitoring dashboards.

### ReportLog

```go
reporter.ReportLog("info", "Starting phase 2", map[string]interface{}{
    "phase": 2,
    "items": 250,
})
```

Reports a structured log entry from within the task. Published as a `task.log` event.

---

## Persistence Model

### Database Schema

```sql
CREATE TABLE background_tasks (
    id                 TEXT PRIMARY KEY,
    task_type          TEXT NOT NULL,
    task_name          TEXT NOT NULL,
    status             TEXT NOT NULL DEFAULT 'pending',
    priority           TEXT NOT NULL DEFAULT 'normal',
    input_data         BYTEA,
    output_data        BYTEA,
    checkpoint_data    BYTEA,
    progress           FLOAT DEFAULT 0,
    progress_message   TEXT,
    last_error         TEXT,
    worker_id          TEXT,
    retry_count        INT DEFAULT 0,
    required_cpu_cores INT DEFAULT 0,
    required_memory_mb INT DEFAULT 0,
    scheduled_at       TIMESTAMPTZ NOT NULL,
    started_at         TIMESTAMPTZ,
    completed_at       TIMESTAMPTZ,
    last_heartbeat     TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    correlation_id     TEXT,
    config             JSONB DEFAULT '{}'
);

CREATE INDEX idx_tasks_status_priority ON background_tasks (status, priority DESC, scheduled_at);
CREATE INDEX idx_tasks_worker ON background_tasks (worker_id) WHERE worker_id IS NOT NULL;
CREATE INDEX idx_tasks_type ON background_tasks (task_type);

CREATE TABLE task_execution_history (
    id         BIGSERIAL PRIMARY KEY,
    task_id    TEXT NOT NULL REFERENCES background_tasks(id),
    event_type TEXT NOT NULL,
    data       JSONB,
    worker_id  TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE resource_snapshots (
    id               BIGSERIAL PRIMARY KEY,
    task_id          TEXT NOT NULL REFERENCES background_tasks(id),
    cpu_percent      FLOAT,
    memory_rss_bytes BIGINT,
    memory_percent   FLOAT,
    io_read_bytes    BIGINT,
    io_write_bytes   BIGINT,
    thread_count     INT,
    open_fds         INT,
    net_connections  INT,
    process_state    TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### Priority Weights

| Priority | Weight | Description |
|----------|--------|-------------|
| `critical` | 4 | System-critical tasks, dequeued first |
| `high` | 3 | User-facing tasks |
| `normal` | 2 | Standard background work |
| `low` | 1 | Deferred, non-urgent tasks |

---

## Event Flow Example

A complete lifecycle for a task that fails once and succeeds on retry:

```
1. Enqueue          -> task.created     (status: pending)
2. Worker claims    -> task.started     (status: running)
3. Progress 25%     -> task.progress    (status: running)
4. Heartbeat        -> task.heartbeat   (status: running)
5. Progress 50%     -> task.progress    (status: running)
6. API error        -> task.failed      (status: failed)
7. Retry decision   -> task.retrying    (status: pending, retry_count: 1)
8. Worker claims    -> task.started     (status: running)
9. Progress 50%     -> task.progress    (status: running)
10. Progress 100%   -> task.progress    (status: running)
11. Success         -> task.completed   (status: completed)
```

Each event includes the task ID, worker ID, timestamp, and relevant metadata, enabling full reconstruction of the task's execution history.
