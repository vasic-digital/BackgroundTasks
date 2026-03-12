package background

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"digital.vasic.models"
)

// DefaultStuckDetector implements stuck detection algorithms
type DefaultStuckDetector struct {
	logger     *logrus.Logger
	thresholds map[string]time.Duration
	mu         sync.RWMutex
}

// StuckDetectorConfig holds configuration for stuck detection
type StuckDetectorConfig struct {
	DefaultThreshold        time.Duration `yaml:"default_threshold"`
	CPUActivityThreshold    float64       `yaml:"cpu_activity_threshold"`
	MemoryGrowthThreshold   float64       `yaml:"memory_growth_threshold"`
	IOActivityThreshold     int64         `yaml:"io_activity_threshold"`
	MinSnapshotsForAnalysis int           `yaml:"min_snapshots_for_analysis"`
}

// DefaultStuckDetectorConfig returns default configuration
func DefaultStuckDetectorConfig() *StuckDetectorConfig {
	return &StuckDetectorConfig{
		DefaultThreshold:        5 * time.Minute,
		CPUActivityThreshold:    0.1,  // 0.1% CPU
		MemoryGrowthThreshold:   0.5,  // 50% growth rate
		IOActivityThreshold:     1024, // 1KB
		MinSnapshotsForAnalysis: 3,
	}
}

// NewDefaultStuckDetector creates a new stuck detector
func NewDefaultStuckDetector(logger *logrus.Logger) *DefaultStuckDetector {
	return &DefaultStuckDetector{
		logger: logger,
		thresholds: map[string]time.Duration{
			"default":   5 * time.Minute,
			"command":   3 * time.Minute,
			"llm_call":  3 * time.Minute,
			"debate":    10 * time.Minute,
			"embedding": 2 * time.Minute,
			"endless":   0, // No automatic stuck detection for endless tasks
		},
	}
}

// IsStuck determines if a task is stuck based on various criteria
func (d *DefaultStuckDetector) IsStuck(ctx context.Context, task *models.BackgroundTask, snapshots []*models.ResourceSnapshot) (bool, string) {
	if task == nil {
		return false, ""
	}

	// Endless tasks use different detection
	if task.Config.Endless {
		return d.isEndlessTaskStuck(task, snapshots)
	}

	// Check heartbeat timeout
	if reason := d.checkHeartbeatTimeout(task); reason != "" {
		return true, reason
	}

	// Check deadline exceeded
	if task.IsOverdue() {
		return true, "task exceeded deadline"
	}

	// If we have resource snapshots, perform deeper analysis
	if len(snapshots) >= 3 {
		// Check for frozen process (no CPU activity)
		if d.isProcessFrozen(snapshots) {
			return true, "process appears frozen (no CPU activity)"
		}

		// Check for resource exhaustion
		if reason := d.checkResourceExhaustion(snapshots); reason != "" {
			return true, reason
		}

		// Check for I/O starvation
		if d.isIOStarved(snapshots) {
			return true, "process appears I/O starved"
		}

		// Check for network hang
		if d.isNetworkHung(snapshots) {
			return true, "process appears hung on network I/O"
		}

		// Check for memory leak
		if d.hasMemoryLeak(snapshots) {
			return true, "potential memory leak detected"
		}
	}

	return false, ""
}

// GetStuckThreshold returns the stuck detection threshold for a task type
func (d *DefaultStuckDetector) GetStuckThreshold(taskType string) time.Duration {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if threshold, exists := d.thresholds[taskType]; exists {
		return threshold
	}
	return d.thresholds["default"]
}

// SetThreshold sets a custom threshold for a task type
func (d *DefaultStuckDetector) SetThreshold(taskType string, threshold time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.thresholds[taskType] = threshold
}

// checkHeartbeatTimeout checks if the task has exceeded heartbeat timeout
func (d *DefaultStuckDetector) checkHeartbeatTimeout(task *models.BackgroundTask) string {
	threshold := d.GetStuckThreshold(task.TaskType)
	if threshold == 0 {
		return "" // No timeout for endless tasks
	}

	// Use task config threshold if set
	if task.Config.StuckThresholdSecs > 0 {
		threshold = time.Duration(task.Config.StuckThresholdSecs) * time.Second
	}

	if task.HasStaleHeartbeat(threshold) {
		if task.LastHeartbeat == nil {
			return fmt.Sprintf("no heartbeat ever received (threshold: %v)", threshold)
		}
		return fmt.Sprintf("no heartbeat for %v (threshold: %v)", time.Since(*task.LastHeartbeat).Round(time.Second), threshold)
	}

	return ""
}

// isProcessFrozen checks if CPU activity has stopped
func (d *DefaultStuckDetector) isProcessFrozen(snapshots []*models.ResourceSnapshot) bool {
	if len(snapshots) < 3 {
		return false
	}

	// Check if CPU is consistently at 0 across recent snapshots
	zeroCount := 0
	for i := 0; i < min3(5, len(snapshots)); i++ {
		if snapshots[i].CPUPercent < 0.1 {
			zeroCount++
		}
	}

	if zeroCount < 3 {
		return false
	}

	// Also check if CPU time has increased
	if len(snapshots) >= 2 {
		latest := snapshots[0]
		older := snapshots[len(snapshots)-1]
		cpuTimeIncrease := (latest.CPUUserTime + latest.CPUSystemTime) -
			(older.CPUUserTime + older.CPUSystemTime)
		if cpuTimeIncrease > 0.01 {
			return false
		}
	}

	return true
}

// checkResourceExhaustion checks for memory issues
func (d *DefaultStuckDetector) checkResourceExhaustion(snapshots []*models.ResourceSnapshot) string {
	if len(snapshots) == 0 {
		return ""
	}

	latest := snapshots[0]

	// Check for memory exhaustion (> 95% memory)
	if latest.MemoryPercent > 95 {
		return fmt.Sprintf("memory exhaustion: %.1f%% used", latest.MemoryPercent)
	}

	// Check for file descriptor exhaustion
	if latest.OpenFDs > 10000 {
		return fmt.Sprintf("file descriptor exhaustion: %d open", latest.OpenFDs)
	}

	// Check for excessive thread count
	if latest.ThreadCount > 1000 {
		return fmt.Sprintf("excessive threads: %d", latest.ThreadCount)
	}

	return ""
}

// isIOStarved checks for I/O starvation patterns
func (d *DefaultStuckDetector) isIOStarved(snapshots []*models.ResourceSnapshot) bool {
	if len(snapshots) < 3 {
		return false
	}

	latest := snapshots[0]
	oldest := snapshots[min3(3, len(snapshots)-1)]

	// If there are no I/O operations but the process is supposedly doing something
	ioActivity := (latest.IOReadBytes - oldest.IOReadBytes) +
		(latest.IOWriteBytes - oldest.IOWriteBytes)

	// If CPU is active but no I/O for multiple samples, might be stuck
	if ioActivity == 0 && latest.CPUPercent < 1.0 && latest.CPUPercent > 0 {
		return true
	}

	return false
}

// isNetworkHung checks for network hang patterns
func (d *DefaultStuckDetector) isNetworkHung(snapshots []*models.ResourceSnapshot) bool {
	if len(snapshots) < 3 {
		return false
	}

	latest := snapshots[0]
	oldest := snapshots[min3(3, len(snapshots)-1)]

	// Check for active connections with no data transfer
	if latest.NetConnections > 0 {
		netActivity := (latest.NetBytesSent - oldest.NetBytesSent) +
			(latest.NetBytesRecv - oldest.NetBytesRecv)

		if netActivity == 0 && latest.CPUPercent < 1.0 {
			return true
		}
	}

	return false
}

// hasMemoryLeak detects potential memory leaks
func (d *DefaultStuckDetector) hasMemoryLeak(snapshots []*models.ResourceSnapshot) bool {
	if len(snapshots) < 5 {
		return false
	}

	// Check if memory is monotonically increasing
	increasing := 0
	for i := 0; i < len(snapshots)-1; i++ {
		if snapshots[i].MemoryRSSBytes > snapshots[i+1].MemoryRSSBytes {
			increasing++
		}
	}

	// If memory increased in most samples, potential leak
	if float64(increasing)/float64(len(snapshots)-1) > 0.8 {
		// Check growth rate
		oldest := snapshots[len(snapshots)-1]
		latest := snapshots[0]

		if oldest.MemoryRSSBytes > 0 {
			growthRate := float64(latest.MemoryRSSBytes-oldest.MemoryRSSBytes) / float64(oldest.MemoryRSSBytes)
			if growthRate > 0.5 { // 50% growth
				return true
			}
		}
	}

	return false
}

// isEndlessTaskStuck uses different criteria for endless tasks
func (d *DefaultStuckDetector) isEndlessTaskStuck(task *models.BackgroundTask, snapshots []*models.ResourceSnapshot) (bool, string) {
	// For endless tasks, only check for:
	// 1. Process death (zombie state)
	// 2. Severe resource exhaustion
	// 3. Complete hang (no activity at all)

	if len(snapshots) == 0 {
		return false, ""
	}

	latest := snapshots[0]

	// Check process state
	if latest.ProcessState == "zombie" || latest.ProcessState == "Z" {
		return true, "process is in zombie state"
	}

	// Check severe memory exhaustion
	if latest.MemoryPercent > 98 {
		return true, "critical memory exhaustion (>98%)"
	}

	// Check complete activity halt (needs multiple samples)
	if len(snapshots) >= 5 {
		activityDetected := false
		for i := 0; i < 5; i++ {
			if snapshots[i].CPUPercent > 0 ||
				(i > 0 && (snapshots[i].IOReadBytes != snapshots[i-1].IOReadBytes ||
					snapshots[i].IOWriteBytes != snapshots[i-1].IOWriteBytes)) {
				activityDetected = true
				break
			}
		}

		if !activityDetected {
			return true, "endless process has no activity"
		}
	}

	return false, ""
}

// StuckAnalysis provides detailed analysis of why a task might be stuck
type StuckAnalysis struct {
	IsStuck         bool            `json:"is_stuck"`
	Reason          string          `json:"reason,omitempty"`
	HeartbeatStatus HeartbeatStatus `json:"heartbeat_status"`
	ResourceStatus  ResourceStatus  `json:"resource_status"`
	ActivityStatus  ActivityStatus  `json:"activity_status"`
	Recommendations []string        `json:"recommendations,omitempty"`
}

type HeartbeatStatus struct {
	LastHeartbeat      *time.Time    `json:"last_heartbeat,omitempty"`
	TimeSinceHeartbeat time.Duration `json:"time_since_heartbeat,omitempty"`
	Threshold          time.Duration `json:"threshold"`
	IsStale            bool          `json:"is_stale"`
}

type ResourceStatus struct {
	CPUPercent    float64 `json:"cpu_percent"`
	MemoryPercent float64 `json:"memory_percent"`
	MemoryBytes   int64   `json:"memory_bytes"`
	OpenFDs       int     `json:"open_fds"`
	ThreadCount   int     `json:"thread_count"`
	IsExhausted   bool    `json:"is_exhausted"`
}

type ActivityStatus struct {
	HasCPUActivity bool  `json:"has_cpu_activity"`
	HasIOActivity  bool  `json:"has_io_activity"`
	HasNetActivity bool  `json:"has_net_activity"`
	IOReadBytes    int64 `json:"io_read_bytes"`
	IOWriteBytes   int64 `json:"io_write_bytes"`
	NetConnections int   `json:"net_connections"`
}

// AnalyzeTask performs detailed stuck analysis on a task
func (d *DefaultStuckDetector) AnalyzeTask(ctx context.Context, task *models.BackgroundTask, snapshots []*models.ResourceSnapshot) *StuckAnalysis {
	analysis := &StuckAnalysis{
		Recommendations: make([]string, 0),
	}

	// Check heartbeat
	threshold := d.GetStuckThreshold(task.TaskType)
	analysis.HeartbeatStatus = HeartbeatStatus{
		LastHeartbeat: task.LastHeartbeat,
		Threshold:     threshold,
	}

	if task.LastHeartbeat != nil {
		analysis.HeartbeatStatus.TimeSinceHeartbeat = time.Since(*task.LastHeartbeat)
		analysis.HeartbeatStatus.IsStale = task.HasStaleHeartbeat(threshold)
	} else {
		analysis.HeartbeatStatus.IsStale = true
	}

	// Analyze resources if we have snapshots
	if len(snapshots) > 0 {
		latest := snapshots[0]
		analysis.ResourceStatus = ResourceStatus{
			CPUPercent:    latest.CPUPercent,
			MemoryPercent: latest.MemoryPercent,
			MemoryBytes:   latest.MemoryRSSBytes,
			OpenFDs:       latest.OpenFDs,
			ThreadCount:   latest.ThreadCount,
		}

		// Check for exhaustion
		if latest.MemoryPercent > 90 || latest.OpenFDs > 10000 {
			analysis.ResourceStatus.IsExhausted = true
			if latest.MemoryPercent > 90 {
				analysis.Recommendations = append(analysis.Recommendations, "Consider increasing memory limits")
			}
			if latest.OpenFDs > 10000 {
				analysis.Recommendations = append(analysis.Recommendations, "Check for file descriptor leaks")
			}
		}

		// Analyze activity
		analysis.ActivityStatus = ActivityStatus{
			HasCPUActivity: latest.CPUPercent > 0.1,
			IOReadBytes:    latest.IOReadBytes,
			IOWriteBytes:   latest.IOWriteBytes,
			NetConnections: latest.NetConnections,
		}

		if len(snapshots) >= 2 {
			prev := snapshots[1]
			analysis.ActivityStatus.HasIOActivity = latest.IOReadBytes != prev.IOReadBytes ||
				latest.IOWriteBytes != prev.IOWriteBytes
			analysis.ActivityStatus.HasNetActivity = latest.NetBytesSent != prev.NetBytesSent ||
				latest.NetBytesRecv != prev.NetBytesRecv
		}
	}

	// Determine if stuck
	isStuck, reason := d.IsStuck(ctx, task, snapshots)
	analysis.IsStuck = isStuck
	analysis.Reason = reason

	// Add recommendations
	if isStuck {
		if analysis.HeartbeatStatus.IsStale {
			analysis.Recommendations = append(analysis.Recommendations, "Task may need to be cancelled and restarted")
		}
		if !analysis.ActivityStatus.HasCPUActivity && !analysis.ActivityStatus.HasIOActivity {
			analysis.Recommendations = append(analysis.Recommendations, "Process appears completely idle - check for deadlock")
		}
	}

	return analysis
}

func min3(a, b int) int {
	if a < b {
		return a
	}
	return b
}
