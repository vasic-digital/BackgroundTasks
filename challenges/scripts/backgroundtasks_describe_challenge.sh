#!/usr/bin/env bash
# backgroundtasks_describe_challenge.sh
#
# Round-261 paired-mutation deep-doc challenge for digital.vasic.background.
#
# Validates that:
#   1. The deep-doc ledger (docs/test-coverage.md) lists every exported
#      symbol from interfaces.go, task_queue.go, worker_pool.go,
#      stuck_detector.go, resource_monitor.go, events.go,
#      event_publisher.go, messaging_adapter.go, and metrics.go.
#   2. The multi-locale fixture
#      (tests/fixtures/backgroundtasks/payloads.json) parses and
#      contains at least 3 locales.
#   3. The multi-locale runner (challenges/runner/main.go) builds and
#      runs, byte-preserving non-ASCII task payloads through the real
#      InMemoryTaskQueue + AdaptiveWorkerPool + DefaultStuckDetector
#      + ProcessResourceMonitor + MessagingTaskQueue + EventPublisher.
#   4. The README enumerates the round-261 anti-bluff guarantees.
#
# Paired-mutation invariant (CONST-035 + CONST-050(B)):
#   With --anti-bluff-mutate the script plants a deliberate symbol-rename
#   mutation in the ledger (in a tmp copy: AdaptiveWorkerPool ->
#   AdaptiveWorkerPool_MUTATED), reruns validation, and asserts the
#   gate FAILS with exit 99. This proves the gate actually catches
#   ledger-vs-source drift instead of rubber-stamping it.
#
# Exit codes:
#   0  — gate PASS on clean tree
#   1  — gate FAIL on clean tree (real failure to fix)
#   99 — paired-mutation correctly detected (good — proves anti-bluff)
#   2  — usage / environment error

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODULE_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

MUTATE=0
for arg in "$@"; do
    case "$arg" in
        --anti-bluff-mutate) MUTATE=1 ;;
        --help|-h)
            sed -n '1,32p' "$0"
            exit 0
            ;;
        *)
            echo "unknown argument: $arg" >&2
            exit 2
            ;;
    esac
done

PASS=0
FAIL=0
TOTAL=0

pass() { PASS=$((PASS+1)); TOTAL=$((TOTAL+1)); echo "  PASS: $1"; }
fail() { FAIL=$((FAIL+1)); TOTAL=$((TOTAL+1)); echo "  FAIL: $1"; }

LEDGER="${MODULE_DIR}/docs/test-coverage.md"
FIXTURE="${MODULE_DIR}/tests/fixtures/backgroundtasks/payloads.json"
RUNNER="${MODULE_DIR}/challenges/runner/main.go"
README="${MODULE_DIR}/README.md"

LEDGER_WORK="${LEDGER}"
TMP_LEDGER=""
if [ "${MUTATE}" -eq 1 ]; then
    TMP_LEDGER="$(mktemp)"
    cp "${LEDGER}" "${TMP_LEDGER}"
    # Plant a rename so the symbol no longer matches what the source declares.
    sed -i 's/AdaptiveWorkerPool/AdaptiveWorkerPool_MUTATED/g' "${TMP_LEDGER}"
    LEDGER_WORK="${TMP_LEDGER}"
    echo "=== BackgroundTasks Describe Challenge (anti-bluff-mutate mode) ==="
else
    echo "=== BackgroundTasks Describe Challenge (clean mode) ==="
fi
echo ""

# Section 1: ledger presence and freshness
echo "Section 1: docs/test-coverage.md ledger"
if [ ! -f "${LEDGER_WORK}" ]; then
    fail "ledger missing at ${LEDGER_WORK}"
else
    pass "ledger present"
    if grep -q "round-261" "${LEDGER_WORK}"; then
        pass "ledger marked round-261"
    else
        fail "ledger missing round-261 marker"
    fi
    if grep -q "execution of tests and Challenges MUST guarantee" "${LEDGER_WORK}"; then
        pass "ledger carries Article XI §11.9 mandate"
    else
        fail "ledger missing Article XI §11.9 mandate"
    fi
fi

# Section 2: every exported package symbol appears in ledger.
# We restrict to a hand-picked, stable set of structural symbols that we
# expect to find verbatim in the ledger. (Exhaustive parsing of every
# exported identifier from every Go file would produce too many false
# positives from internal helpers — the ledger is authoritative about
# what counts as part of the public surface.)
echo ""
echo "Section 2: structural symbol cross-reference"

EXPECTED_SYMBOLS=(
    # interfaces.go
    "TaskExecutor" "ProgressReporter" "TaskQueue" "TaskWaiter" "TaskRepository"
    "ResourceRequirements" "SystemResources" "ResourceMonitor" "StuckDetector"
    "NotificationService" "WebSocketClient" "WorkerPool" "WorkerStatus"
    "TaskEvent" "ExecutionResult"
    # task_queue.go
    "PostgresTaskQueue" "InMemoryTaskQueue" "NewInMemoryTaskQueue" "TaskQueueStats"
    # worker_pool.go
    "WorkerPoolConfig" "DefaultWorkerPoolConfig" "AdaptiveWorkerPool"
    "NewAdaptiveWorkerPool"
    # stuck_detector.go
    "DefaultStuckDetector" "StuckDetectorConfig" "DefaultStuckDetectorConfig"
    "NewDefaultStuckDetector" "StuckAnalysis"
    # resource_monitor.go
    "ProcessResourceMonitor" "NewProcessResourceMonitor"
    # events.go
    "TopicTaskEvents" "TopicTaskCreated" "TopicTaskStarted" "TopicTaskCompleted"
    "TaskEventType" "BackgroundTaskEvent" "NewBackgroundTaskEvent"
    "TaskEventPublisher" "TaskEventPublisherConfig" "DefaultTaskEventPublisherConfig"
    "NewTaskEventPublisher"
    # event_publisher.go
    "EventPublisher" "NoOpEventPublisher" "LoggingEventPublisher" "Logger"
    "NewLoggingEventPublisher"
    # messaging_adapter.go
    "MessagingTaskQueue" "MessagingTaskQueueConfig" "DefaultMessagingTaskQueueConfig"
    "NewMessagingTaskQueue" "MessagingProgressReporter" "NewMessagingProgressReporter"
    "MessagingTaskExecutorWrapper" "NewMessagingTaskExecutorWrapper"
    "SetupMessagingForWorkerPool"
    # metrics.go
    "WorkerPoolMetrics" "NewWorkerPoolMetrics" "GetGlobalMetrics" "SetGlobalMetrics"
)

CHECKED=0
MISSING=0
for sym in "${EXPECTED_SYMBOLS[@]}"; do
    CHECKED=$((CHECKED + 1))
    if grep -qE "\\b${sym}\\b" "${LEDGER_WORK}"; then
        : # found
    else
        fail "ledger missing symbol ${sym}"
        MISSING=$((MISSING + 1))
    fi
done
if [ "${MISSING}" -eq 0 ]; then
    pass "all ${CHECKED} structural symbols cross-referenced in ledger"
fi

# Section 3: multi-locale fixture sanity
echo ""
echo "Section 3: multi-locale fixture"
if [ ! -f "${FIXTURE}" ]; then
    fail "fixture missing at ${FIXTURE}"
else
    pass "fixture present"
    LOCALE_COUNT=$(grep -oE '"locale":\s*"[^"]+"' "${FIXTURE}" | sort -u | wc -l)
    if [ "${LOCALE_COUNT}" -ge 3 ]; then
        pass "fixture covers ${LOCALE_COUNT} locales (>=3)"
    else
        fail "fixture covers only ${LOCALE_COUNT} locales (<3)"
    fi
fi

# Section 4: runner builds + runs against every section
echo ""
echo "Section 4: multi-locale runner build + run (real queue + worker pool + monitor + publisher)"
if [ ! -f "${RUNNER}" ]; then
    fail "runner missing at ${RUNNER}"
else
    pass "runner source present"
    cd "${MODULE_DIR}"
    if go build -o /tmp/backgroundtasks_round261_runner ./challenges/runner/ 2>/tmp/bgtasks_build.log; then
        pass "runner builds"
        if /tmp/backgroundtasks_round261_runner -fixtures "${FIXTURE}" > /tmp/bgtasks_run.log 2>&1; then
            pass "runner exit 0 across every section + locale"
            if grep -q "PASS: \[Section1\]\[round-trip\]\[sr\]" /tmp/bgtasks_run.log; then
                pass "Section 1 Cyrillic (sr) payload round-trip"
            else
                fail "Section 1 Cyrillic (sr) round-trip missing"
            fi
            if grep -q "PASS: \[Section1\]\[round-trip\]\[ja\]" /tmp/bgtasks_run.log; then
                pass "Section 1 Japanese (ja) payload round-trip"
            else
                fail "Section 1 Japanese (ja) round-trip missing"
            fi
            if grep -q "PASS: \[Section1\]\[round-trip\]\[ar\]" /tmp/bgtasks_run.log; then
                pass "Section 1 Arabic (ar) payload round-trip"
            else
                fail "Section 1 Arabic (ar) round-trip missing"
            fi
            if grep -q "PASS: \[Section1\]\[round-trip\]\[zh-CN\]" /tmp/bgtasks_run.log; then
                pass "Section 1 Han (zh-CN) payload round-trip"
            else
                fail "Section 1 Han (zh-CN) round-trip missing"
            fi
            if grep -q "PASS: \[Section1\]\[executor.Execute\]" /tmp/bgtasks_run.log; then
                pass "Section 1 worker pool executed every locale task end-to-end"
            else
                fail "Section 1 worker pool end-to-end execution missing"
            fi
            if grep -q "PASS: \[Section2\]\[Dequeue\]\[priority-order\]" /tmp/bgtasks_run.log; then
                pass "Section 2 priority-order Dequeue invariant enforced"
            else
                fail "Section 2 priority-order Dequeue missing"
            fi
            if grep -q "PASS: \[Section2\]\[MoveToDeadLetter\]" /tmp/bgtasks_run.log; then
                pass "Section 2 MoveToDeadLetter exercised"
            else
                fail "Section 2 MoveToDeadLetter missing"
            fi
            if grep -q "PASS: \[Section3\]\[IsStuck\]\[stale-heartbeat\]" /tmp/bgtasks_run.log; then
                pass "Section 3 stuck-detector flags stale heartbeat"
            else
                fail "Section 3 stuck-detector stale-heartbeat missing"
            fi
            if grep -q "PASS: \[Section3\]\[IsStuck\]\[fresh-heartbeat\]" /tmp/bgtasks_run.log; then
                pass "Section 3 stuck-detector ignores fresh heartbeat"
            else
                fail "Section 3 stuck-detector fresh-heartbeat missing"
            fi
            if grep -q "PASS: \[Section4\]\[GetSystemResources\]" /tmp/bgtasks_run.log; then
                pass "Section 4 real gopsutil GetSystemResources exercised"
            else
                fail "Section 4 GetSystemResources missing"
            fi
            if grep -q "PASS: \[Section4\]\[GetProcessResources\]\[own-pid=" /tmp/bgtasks_run.log; then
                pass "Section 4 own-PID GetProcessResources exercised"
            else
                fail "Section 4 own-PID GetProcessResources missing"
            fi
            if grep -q "PASS: \[Section5\]\[publisher\]\[task.created\]" /tmp/bgtasks_run.log; then
                pass "Section 5 task.created event fired through MessagingTaskQueue"
            else
                fail "Section 5 task.created event missing"
            fi
            if grep -q "PASS: \[Section5\]\[publisher\]\[task.started\]" /tmp/bgtasks_run.log; then
                pass "Section 5 task.started event fired through MessagingTaskQueue"
            else
                fail "Section 5 task.started event missing"
            fi
            if grep -q "PASS: \[Section5\]\[TaskEventType.Topic\]" /tmp/bgtasks_run.log; then
                pass "Section 5 TaskEventType.Topic routing table complete"
            else
                fail "Section 5 TaskEventType.Topic routing missing"
            fi
        else
            fail "runner exit non-zero — see /tmp/bgtasks_run.log"
            sed -n '1,80p' /tmp/bgtasks_run.log
        fi
    else
        fail "runner build failed — see /tmp/bgtasks_build.log"
        sed -n '1,40p' /tmp/bgtasks_build.log
    fi
    rm -f /tmp/backgroundtasks_round261_runner
fi

# Section 5: README round-261 anti-bluff section
echo ""
echo "Section 5: README round-261 anti-bluff section"
if grep -q "Anti-bluff guarantees" "${README}"; then
    pass "README declares Anti-bluff guarantees"
else
    fail "README missing Anti-bluff guarantees section"
fi
if grep -q "round-261" "${README}"; then
    pass "README marked round-261"
else
    fail "README missing round-261 marker"
fi

# Cleanup mutated ledger if any
if [ -n "${TMP_LEDGER}" ]; then
    rm -f "${TMP_LEDGER}"
fi

echo ""
echo "=== Summary: ${PASS}/${TOTAL} PASS, ${FAIL} FAIL ==="

if [ "${MUTATE}" -eq 1 ]; then
    if [ "${FAIL}" -gt 0 ]; then
        echo "anti-bluff-mutate: gate correctly detected planted mutation (exit 99)"
        exit 99
    else
        echo "anti-bluff-mutate: gate FAILED to detect planted mutation — bluff!"
        exit 1
    fi
fi

if [ "${FAIL}" -gt 0 ]; then
    exit 1
fi
exit 0
