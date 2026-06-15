package background

import (
	"context"
	"io"
	"sync"
	"testing"

	"digital.vasic.models"
	"github.com/sirupsen/logrus"
)

// TestInMemoryTaskQueue_ConcurrentEnqueue_UniqueIDs is the §11.4.115 RED-first
// regression guard for the InMemoryTaskQueue.Enqueue duplicate-ID bug.
//
// Bug (RED on pre-fix artifact): Enqueue assigned
//
//	task.ID = fmt.Sprintf("task-%d", time.Now().UnixNano())
//
// Two concurrent enqueues that land in the same nanosecond tick produce the
// SAME id. The queue stores tasks in `map[string]*models.BackgroundTask`, so
// the second write OVERWRITES the first under the same key — a silently lost
// task. (The mutex serialises the map write but does NOT separate the two
// time.Now().UnixNano() reads enough to guarantee distinct values; coarse
// clocks and same-tick reads collide.)
//
// Fix: append a per-Enqueue monotonic atomic counter to the id, guaranteeing
// uniqueness regardless of clock resolution.
//
// Polarity switch per §11.4.115: this same source is BOTH the bug-reproducer
// (on the broken artifact it FAILs, demonstrating the duplicate) AND the
// standing GREEN regression guard (on the fixed artifact every id is unique).
// There is no RED_MODE env flag needed — the assertion is identical in both
// roles: "all N enqueued ids are unique AND all N tasks are retrievable".
func TestInMemoryTaskQueue_ConcurrentEnqueue_UniqueIDs(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	q := NewInMemoryTaskQueue(logger)

	const n = 1000

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			// task.ID left empty so Enqueue generates it — this is the path
			// under test. A non-empty ID would bypass the generator.
			task := &models.BackgroundTask{
				TaskType: "unit-test",
				Priority: models.TaskPriorityNormal,
			}
			if err := q.Enqueue(context.Background(), task); err != nil {
				t.Errorf("Enqueue returned error: %v", err)
			}
		}()
	}
	wg.Wait()

	// 1) Every generated ID must be unique. On the broken artifact, same-tick
	//    collisions make len(seen) < n.
	pending, err := q.Peek(context.Background(), n)
	if err != nil {
		t.Fatalf("Peek returned error: %v", err)
	}

	seen := make(map[string]int, n)
	for _, task := range pending {
		seen[task.ID]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Errorf("duplicate task ID generated %d times: %q", count, id)
		}
	}

	// 2) The map-overwrite symptom: a lost task. The internal map MUST hold all
	//    n distinct tasks. On the broken artifact, colliding ids overwrite each
	//    other and the queue depth drops below n.
	got, err := q.GetPendingCount(context.Background())
	if err != nil {
		t.Fatalf("GetPendingCount returned error: %v", err)
	}
	if int(got) != n {
		t.Errorf("expected %d pending tasks, got %d — %d task(s) lost to ID collision/overwrite",
			n, got, n-int(got))
	}
	if len(seen) != n {
		t.Errorf("expected %d unique IDs, got %d — %d collision(s)", n, len(seen), n-len(seen))
	}
}
