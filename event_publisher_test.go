package background

import (
	"context"
	"fmt"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

// mockLogger implements Logger interface for testing
type mockLogger struct {
	debugMessages []string
	infoMessages  []string
	warnMessages  []string
	errorMessages []string
}

func (m *mockLogger) Debugf(format string, args ...interface{}) {
	// Store both format and formatted message for testing
	m.debugMessages = append(m.debugMessages, fmt.Sprintf(format, args...))
}

func (m *mockLogger) Infof(format string, args ...interface{}) {
	m.infoMessages = append(m.infoMessages, format)
}

func (m *mockLogger) Warnf(format string, args ...interface{}) {
	m.warnMessages = append(m.warnMessages, format)
}

func (m *mockLogger) Errorf(format string, args ...interface{}) {
	m.errorMessages = append(m.errorMessages, format)
}

func TestNoOpEventPublisher(t *testing.T) {
	publisher := &NoOpEventPublisher{}
	event := &BackgroundTaskEvent{
		EventType: TaskEventTypeCreated,
		TaskID:    "test-task",
		Status:    "pending",
	}
	err := publisher.Publish(context.Background(), event)
	assert.NoError(t, err)
}

func TestLoggingEventPublisher(t *testing.T) {
	logger := &mockLogger{}
	publisher := NewLoggingEventPublisher(logger)
	event := &BackgroundTaskEvent{
		EventType: TaskEventTypeStarted,
		TaskID:    "test-task-123",
		Status:    "running",
		Progress:  0.0,
	}
	err := publisher.Publish(context.Background(), event)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(logger.debugMessages))
	assert.Contains(t, logger.debugMessages[0], "Background task event")
	assert.Contains(t, logger.debugMessages[0], "task_id=test-task-123")
}

func TestLoggingEventPublisherWithRealLogger(t *testing.T) {
	// Use logrus as a real logger to ensure compatibility
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	publisher := NewLoggingEventPublisher(logger)
	event := &BackgroundTaskEvent{
		EventType: TaskEventTypeCompleted,
		TaskID:    "test-task-456",
		Status:    "completed",
		Progress:  100.0,
	}
	err := publisher.Publish(context.Background(), event)
	assert.NoError(t, err)
}
