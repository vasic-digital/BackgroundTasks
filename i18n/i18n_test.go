package i18n

import (
	"context"
	"strings"
	"testing"
)

// TestNoopTranslator_EchoesID verifies the safety default echoes a
// message ID verbatim — a loud failure mode, never a silent swallow.
func TestNoopTranslator_EchoesID(t *testing.T) {
	got, err := NoopTranslator{}.T(context.Background(), "background_metric_workers_active", nil)
	if err != nil {
		t.Fatalf("NoopTranslator.T returned error: %v", err)
	}
	if got != "background_metric_workers_active" {
		t.Fatalf("NoopTranslator echo: got %q want %q", got, "background_metric_workers_active")
	}
}

// TestDefaultTranslator_ResolvesMetricHelp verifies the embedded en
// bundle resolves a Prometheus metric Help message ID to real
// English text — proof the CONST-046 migration is functional, not a
// PASS-bluff echo.
func TestDefaultTranslator_ResolvesMetricHelp(t *testing.T) {
	tr := MustDefaultTranslator()
	got, err := tr.T(context.Background(), "background_metric_workers_active", nil)
	if err != nil {
		t.Fatalf("DefaultTranslator.T error: %v", err)
	}
	if got != "Number of currently active workers" {
		t.Fatalf("metric help: got %q want %q", got, "Number of currently active workers")
	}
	// Anti-bluff: the resolved value MUST differ from the message ID.
	if got == "background_metric_workers_active" {
		t.Fatal("DefaultTranslator returned the message ID verbatim — bundle not loaded")
	}
}

// TestDefaultTranslator_InterpolatesStuckReason verifies placeholder
// interpolation works for the stuck-detector diagnostic reasons.
func TestDefaultTranslator_InterpolatesStuckReason(t *testing.T) {
	tr := MustDefaultTranslator()
	got, err := tr.T(context.Background(), "background_stuck_no_heartbeat_for", map[string]any{
		"elapsed":   "2m30s",
		"threshold": "5m0s",
	})
	if err != nil {
		t.Fatalf("DefaultTranslator.T error: %v", err)
	}
	want := "no heartbeat for 2m30s (threshold: 5m0s)"
	if got != want {
		t.Fatalf("stuck reason: got %q want %q", got, want)
	}
}

// TestDefaultTranslator_ResolvesAllMigratedIDs is the paired-mutation
// anchor: every message ID the round-375 migration introduced MUST
// resolve to a non-empty, non-echo string. If a bundle entry is
// deleted or renamed (the mutation), this test FAILs — proving the
// bundle is load-bearing, not decorative.
func TestDefaultTranslator_ResolvesAllMigratedIDs(t *testing.T) {
	tr := MustDefaultTranslator()
	ids := []string{
		"background_metric_workers_active",
		"background_metric_workers_total",
		"background_metric_scaling_events",
		"background_metric_tasks_total",
		"background_metric_tasks_in_queue",
		"background_metric_task_duration",
		"background_metric_task_retries",
		"background_metric_stuck_tasks",
		"background_metric_dead_letter_tasks",
		"background_metric_task_cpu_percent",
		"background_metric_task_memory_bytes",
		"background_metric_task_io_read_bytes",
		"background_metric_task_io_write_bytes",
		"background_metric_task_net_bytes_sent",
		"background_metric_task_net_bytes_recv",
		"background_metric_notifications_sent",
		"background_metric_notification_errors",
		"background_metric_notification_latency",
		"background_metric_queue_depth",
		"background_metric_dequeue_latency",
		"background_metric_enqueue_latency",
		"background_stuck_no_heartbeat_ever",
		"background_stuck_no_heartbeat_for",
		"background_stuck_memory_exhaustion",
		"background_stuck_fd_exhaustion",
		"background_stuck_excessive_threads",
		"background_recommend_increase_memory",
		"background_recommend_check_fd_leaks",
		"background_recommend_cancel_restart",
		"background_recommend_check_deadlock",
	}
	for _, id := range ids {
		got, err := tr.T(context.Background(), id, nil)
		if err != nil {
			t.Errorf("message ID %q failed to resolve: %v", id, err)
			continue
		}
		if got == "" {
			t.Errorf("message ID %q resolved to empty string", id)
		}
		if got == id {
			t.Errorf("message ID %q resolved to itself — bundle entry missing", id)
		}
	}
}

// TestBundleTranslator_UnknownIDErrors verifies an unknown message ID
// returns an error so Tr can fall back to the loud echo.
func TestBundleTranslator_UnknownIDErrors(t *testing.T) {
	tr := MustDefaultTranslator()
	_, err := tr.T(context.Background(), "background_nonexistent_id", nil)
	if err == nil {
		t.Fatal("expected error for unknown message ID, got nil")
	}
}

// TestTr_FallsBackToEchoOnUnknownID verifies Tr returns the loud
// message-ID echo when the active translator cannot resolve an ID.
func TestTr_FallsBackToEchoOnUnknownID(t *testing.T) {
	got := Tr(context.Background(), "background_unknown_xyz", nil)
	if got != "background_unknown_xyz" {
		t.Fatalf("Tr fallback: got %q want loud echo of the ID", got)
	}
}

// TestBundleTranslator_AcceptsNestedDialect verifies the loader
// accepts the go-i18n nested dialect (id:\n other: "...").
func TestBundleTranslator_AcceptsNestedDialect(t *testing.T) {
	yaml := "greeting:\n  other: \"hello {{name}}\"\n"
	tr, err := NewBundleTranslatorFromBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("nested dialect parse failed: %v", err)
	}
	got, err := tr.T(context.Background(), "greeting", map[string]any{"name": "ops"})
	if err != nil {
		t.Fatalf("T error: %v", err)
	}
	if !strings.Contains(got, "hello ops") {
		t.Fatalf("nested dialect: got %q want contains %q", got, "hello ops")
	}
}
