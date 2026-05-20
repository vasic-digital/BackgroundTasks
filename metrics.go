package background

import (
	"context"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"digital.vasic.background/i18n"
)

// WorkerPoolMetrics holds Prometheus metrics for the worker pool
type WorkerPoolMetrics struct {
	// Worker metrics
	WorkersActive prometheus.Gauge
	WorkersTotal  prometheus.Gauge
	ScalingEvents *prometheus.CounterVec

	// Task metrics
	TasksTotal      *prometheus.CounterVec
	TasksInQueue    *prometheus.GaugeVec
	TaskDuration    *prometheus.HistogramVec
	TaskRetries     *prometheus.CounterVec
	StuckTasks      prometheus.Counter
	DeadLetterTasks prometheus.Counter

	// Resource metrics
	TaskCPUPercent   *prometheus.GaugeVec
	TaskMemoryBytes  *prometheus.GaugeVec
	TaskIOReadBytes  *prometheus.GaugeVec
	TaskIOWriteBytes *prometheus.GaugeVec
	TaskNetBytesSent *prometheus.GaugeVec
	TaskNetBytesRecv *prometheus.GaugeVec

	// Notification metrics
	NotificationsSent   *prometheus.CounterVec
	NotificationErrors  *prometheus.CounterVec
	NotificationLatency *prometheus.HistogramVec

	// Queue metrics
	QueueDepth     *prometheus.GaugeVec
	DequeueLatency prometheus.Histogram
	EnqueueLatency prometheus.Histogram
}

// NewWorkerPoolMetrics creates a new WorkerPoolMetrics with registered metrics
func NewWorkerPoolMetrics() *WorkerPoolMetrics {
	return &WorkerPoolMetrics{
		// Worker metrics
		WorkersActive: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: "helixagent",
			Subsystem: "background",
			Name:      "workers_active",
			Help:      i18n.Tr(context.Background(), "background_metric_workers_active", nil),
		}),

		WorkersTotal: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: "helixagent",
			Subsystem: "background",
			Name:      "workers_total",
			Help:      i18n.Tr(context.Background(), "background_metric_workers_total", nil),
		}),

		ScalingEvents: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "helixagent",
			Subsystem: "background",
			Name:      "scaling_events_total",
			Help:      i18n.Tr(context.Background(), "background_metric_scaling_events", nil),
		}, []string{"direction"}), // direction: up, down

		// Task metrics
		TasksTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "helixagent",
			Subsystem: "background",
			Name:      "tasks_total",
			Help:      i18n.Tr(context.Background(), "background_metric_tasks_total", nil),
		}, []string{"task_type", "status"}), // status: completed, failed, cancelled

		TasksInQueue: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "helixagent",
			Subsystem: "background",
			Name:      "tasks_in_queue",
			Help:      i18n.Tr(context.Background(), "background_metric_tasks_in_queue", nil),
		}, []string{"priority"}),

		TaskDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "helixagent",
			Subsystem: "background",
			Name:      "task_duration_seconds",
			Help:      i18n.Tr(context.Background(), "background_metric_task_duration", nil),
			Buckets:   []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 300, 600, 1800, 3600},
		}, []string{"task_type"}),

		TaskRetries: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "helixagent",
			Subsystem: "background",
			Name:      "task_retries_total",
			Help:      i18n.Tr(context.Background(), "background_metric_task_retries", nil),
		}, []string{"task_type"}),

		StuckTasks: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: "helixagent",
			Subsystem: "background",
			Name:      "stuck_tasks_total",
			Help:      i18n.Tr(context.Background(), "background_metric_stuck_tasks", nil),
		}),

		DeadLetterTasks: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: "helixagent",
			Subsystem: "background",
			Name:      "dead_letter_tasks_total",
			Help:      i18n.Tr(context.Background(), "background_metric_dead_letter_tasks", nil),
		}),

		// Resource metrics
		TaskCPUPercent: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "helixagent",
			Subsystem: "background",
			Name:      "task_cpu_percent",
			Help:      i18n.Tr(context.Background(), "background_metric_task_cpu_percent", nil),
		}, []string{"task_id"}),

		TaskMemoryBytes: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "helixagent",
			Subsystem: "background",
			Name:      "task_memory_bytes",
			Help:      i18n.Tr(context.Background(), "background_metric_task_memory_bytes", nil),
		}, []string{"task_id"}),

		TaskIOReadBytes: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "helixagent",
			Subsystem: "background",
			Name:      "task_io_read_bytes",
			Help:      i18n.Tr(context.Background(), "background_metric_task_io_read_bytes", nil),
		}, []string{"task_id"}),

		TaskIOWriteBytes: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "helixagent",
			Subsystem: "background",
			Name:      "task_io_write_bytes",
			Help:      i18n.Tr(context.Background(), "background_metric_task_io_write_bytes", nil),
		}, []string{"task_id"}),

		TaskNetBytesSent: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "helixagent",
			Subsystem: "background",
			Name:      "task_net_bytes_sent",
			Help:      i18n.Tr(context.Background(), "background_metric_task_net_bytes_sent", nil),
		}, []string{"task_id"}),

		TaskNetBytesRecv: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "helixagent",
			Subsystem: "background",
			Name:      "task_net_bytes_recv",
			Help:      i18n.Tr(context.Background(), "background_metric_task_net_bytes_recv", nil),
		}, []string{"task_id"}),

		// Notification metrics
		NotificationsSent: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "helixagent",
			Subsystem: "background",
			Name:      "notifications_sent_total",
			Help:      i18n.Tr(context.Background(), "background_metric_notifications_sent", nil),
		}, []string{"type", "event"}), // type: webhook, sse, websocket, polling

		NotificationErrors: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "helixagent",
			Subsystem: "background",
			Name:      "notification_errors_total",
			Help:      i18n.Tr(context.Background(), "background_metric_notification_errors", nil),
		}, []string{"type"}),

		NotificationLatency: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "helixagent",
			Subsystem: "background",
			Name:      "notification_latency_seconds",
			Help:      i18n.Tr(context.Background(), "background_metric_notification_latency", nil),
			Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"type"}),

		// Queue metrics
		QueueDepth: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "helixagent",
			Subsystem: "background",
			Name:      "queue_depth",
			Help:      i18n.Tr(context.Background(), "background_metric_queue_depth", nil),
		}, []string{"priority"}),

		DequeueLatency: promauto.NewHistogram(prometheus.HistogramOpts{
			Namespace: "helixagent",
			Subsystem: "background",
			Name:      "dequeue_latency_seconds",
			Help:      i18n.Tr(context.Background(), "background_metric_dequeue_latency", nil),
			Buckets:   []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5},
		}),

		EnqueueLatency: promauto.NewHistogram(prometheus.HistogramOpts{
			Namespace: "helixagent",
			Subsystem: "background",
			Name:      "enqueue_latency_seconds",
			Help:      i18n.Tr(context.Background(), "background_metric_enqueue_latency", nil),
			Buckets:   []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5},
		}),
	}
}

// RecordResourceSnapshot records resource usage metrics for a task
func (m *WorkerPoolMetrics) RecordResourceSnapshot(taskID string, cpuPercent float64, memoryBytes int64, ioReadBytes, ioWriteBytes, netBytesSent, netBytesRecv int64) {
	m.TaskCPUPercent.WithLabelValues(taskID).Set(cpuPercent)
	m.TaskMemoryBytes.WithLabelValues(taskID).Set(float64(memoryBytes))
	m.TaskIOReadBytes.WithLabelValues(taskID).Set(float64(ioReadBytes))
	m.TaskIOWriteBytes.WithLabelValues(taskID).Set(float64(ioWriteBytes))
	m.TaskNetBytesSent.WithLabelValues(taskID).Set(float64(netBytesSent))
	m.TaskNetBytesRecv.WithLabelValues(taskID).Set(float64(netBytesRecv))
}

// CleanupTaskMetrics removes metrics for a completed/failed task
func (m *WorkerPoolMetrics) CleanupTaskMetrics(taskID string) {
	m.TaskCPUPercent.DeleteLabelValues(taskID)
	m.TaskMemoryBytes.DeleteLabelValues(taskID)
	m.TaskIOReadBytes.DeleteLabelValues(taskID)
	m.TaskIOWriteBytes.DeleteLabelValues(taskID)
	m.TaskNetBytesSent.DeleteLabelValues(taskID)
	m.TaskNetBytesRecv.DeleteLabelValues(taskID)
}

// UpdateQueueDepth updates queue depth metrics from a map
func (m *WorkerPoolMetrics) UpdateQueueDepth(depths map[string]int64) {
	for priority, count := range depths {
		m.QueueDepth.WithLabelValues(priority).Set(float64(count))
	}
}

// Global metrics instance for packages that don't have access to WorkerPool
var (
	globalMetrics     *WorkerPoolMetrics
	globalMetricsOnce sync.Once
)

// GetGlobalMetrics returns the global metrics instance, creating if necessary
func GetGlobalMetrics() *WorkerPoolMetrics {
	globalMetricsOnce.Do(func() {
		if globalMetrics == nil {
			globalMetrics = NewWorkerPoolMetrics()
		}
	})
	return globalMetrics
}

// SetGlobalMetrics sets the global metrics instance (must be called before GetGlobalMetrics)
func SetGlobalMetrics(metrics *WorkerPoolMetrics) {
	globalMetrics = metrics
}
