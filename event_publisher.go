// Package background provides event publishing abstraction for background tasks.
package background

import (
	"context"
)

// EventPublisher defines the interface for publishing background task events.
// Implementations can forward events to messaging systems, log aggregators, etc.
type EventPublisher interface {
	// Publish publishes a background task event.
	Publish(ctx context.Context, event *BackgroundTaskEvent) error
}

// NoOpEventPublisher is a no-operation event publisher that discards all events.
// Useful for testing or when event publishing is disabled.
type NoOpEventPublisher struct{}

// Publish discards the event and returns nil.
func (n *NoOpEventPublisher) Publish(ctx context.Context, event *BackgroundTaskEvent) error {
	return nil
}

// LoggingEventPublisher logs events using a logger.
type LoggingEventPublisher struct {
	logger Logger
}

// Logger is a simple logging interface.
type Logger interface {
	Debugf(format string, args ...interface{})
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
}

// NewLoggingEventPublisher creates a new logging event publisher.
func NewLoggingEventPublisher(logger Logger) *LoggingEventPublisher {
	return &LoggingEventPublisher{logger: logger}
}

// Publish logs the event at debug level.
func (l *LoggingEventPublisher) Publish(ctx context.Context, event *BackgroundTaskEvent) error {
	l.logger.Debugf("Background task event: type=%s task_id=%s status=%s progress=%.1f",
		event.EventType, event.TaskID, event.Status, event.Progress)
	return nil
}
