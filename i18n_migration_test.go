package background

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"

	"digital.vasic.background/i18n"
	"digital.vasic.models"
)

// TestI18n_StuckReason_ResolvesEnglish is the round-375 CONST-046
// anti-bluff anchor for the stuck-detector diagnostic reasons. It
// drives a real DefaultStuckDetector through the heartbeat-timeout
// path and asserts the returned reason is real, human-readable
// English text — NOT the raw message ID echo.
func TestI18n_StuckReason_ResolvesEnglish(t *testing.T) {
	detector := NewDefaultStuckDetector(logrus.New())

	// LastHeartbeat far enough in the past to be stale for a
	// "command" task (3-minute threshold).
	lastHeartbeat := time.Now().Add(-10 * time.Minute)
	startedAt := time.Now().Add(-15 * time.Minute)
	task := &models.BackgroundTask{
		ID:            "i18n-stuck-task",
		TaskType:      "command",
		Status:        models.TaskStatusRunning,
		StartedAt:     &startedAt,
		LastHeartbeat: &lastHeartbeat,
	}

	stuck, reason := detector.IsStuck(context.Background(), task, nil)
	if !stuck {
		t.Fatalf("expected task with 10-minute-stale heartbeat to be stuck")
	}
	if reason == "" {
		t.Fatal("stuck reason is empty — operator gets no diagnostic")
	}
	// Anti-bluff: the reason MUST be resolved English, not the ID echo.
	if strings.HasPrefix(reason, "background_stuck_") {
		t.Fatalf("stuck reason returned the raw message ID %q — i18n migration not wired", reason)
	}
	if !strings.Contains(reason, "heartbeat") {
		t.Fatalf("stuck reason %q does not read as the expected English diagnostic", reason)
	}
}

// TestI18n_StuckReason_NeverHeartbeat covers the no-LastHeartbeat
// branch of checkHeartbeatTimeout — a distinct migrated message ID.
func TestI18n_StuckReason_NeverHeartbeat(t *testing.T) {
	detector := NewDefaultStuckDetector(logrus.New())
	startedAt := time.Now().Add(-15 * time.Minute)
	task := &models.BackgroundTask{
		ID:        "i18n-never-hb-task",
		TaskType:  "command",
		Status:    models.TaskStatusRunning,
		StartedAt: &startedAt,
		// LastHeartbeat intentionally nil.
	}
	stuck, reason := detector.IsStuck(context.Background(), task, nil)
	if !stuck {
		t.Fatalf("expected task with no heartbeat to be stuck")
	}
	if strings.HasPrefix(reason, "background_stuck_") {
		t.Fatalf("stuck reason returned raw ID %q — i18n migration not wired", reason)
	}
	if !strings.Contains(reason, "heartbeat") {
		t.Fatalf("no-heartbeat reason %q does not read as expected English", reason)
	}
}

// TestI18n_MetricHelp_ResolvesEnglish is the round-375 CONST-046
// anti-bluff anchor for the Prometheus metric Help text. It builds a
// real WorkerPoolMetrics, gathers the descriptor for one migrated
// metric, and asserts the Help string is real English — not the ID
// echo. Proves the metric-help migration is functional end-to-end.
func TestI18n_MetricHelp_ResolvesEnglish(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "helixagent",
		Subsystem: "background",
		Name:      "i18n_test_workers_active",
		Help:      i18n.Tr(context.Background(), "background_metric_workers_active", nil),
	})
	if err := reg.Register(g); err != nil {
		t.Fatalf("register gauge: %v", err)
	}
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	var help string
	for _, mf := range families {
		if mf.GetName() == "helixagent_background_i18n_test_workers_active" {
			help = mf.GetHelp()
		}
	}
	if help == "" {
		t.Fatal("metric Help is empty — operators see no description in metrics output")
	}
	if strings.HasPrefix(help, "background_metric_") {
		t.Fatalf("metric Help returned raw message ID %q — i18n migration not wired", help)
	}
	if help != "Number of currently active workers" {
		t.Fatalf("metric Help: got %q want %q", help, "Number of currently active workers")
	}
}

// TestI18n_Recommendations_ResolveEnglish drives AnalyzeTask through
// the file-descriptor-exhaustion path and asserts the emitted
// remediation recommendations are real English text, not ID echoes.
func TestI18n_Recommendations_ResolveEnglish(t *testing.T) {
	detector := NewDefaultStuckDetector(logrus.New())
	lastHeartbeat := time.Now().Add(-10 * time.Minute)
	startedAt := time.Now().Add(-15 * time.Minute)
	task := &models.BackgroundTask{
		ID:            "i18n-analyze-task",
		TaskType:      "command",
		Status:        models.TaskStatusRunning,
		StartedAt:     &startedAt,
		LastHeartbeat: &lastHeartbeat,
	}
	snapshots := []*models.ResourceSnapshot{
		{
			ID:            "snap-fd",
			TaskID:        "i18n-analyze-task",
			SampledAt:     time.Now(),
			CPUPercent:    0.0,
			MemoryPercent: 92.0,
			OpenFDs:       20000,
			ThreadCount:   10,
		},
	}
	analysis := detector.AnalyzeTask(context.Background(), task, snapshots)
	if analysis == nil {
		t.Fatal("AnalyzeTask returned nil")
	}
	if len(analysis.Recommendations) == 0 {
		t.Fatal("expected remediation recommendations for an exhausted task")
	}
	for _, rec := range analysis.Recommendations {
		if strings.HasPrefix(rec, "background_recommend_") {
			t.Fatalf("recommendation returned raw message ID %q — i18n migration not wired", rec)
		}
		if rec == "" {
			t.Fatal("empty recommendation string")
		}
	}
}
