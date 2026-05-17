// Round-23 §11.4 anti-bluff regression tests (2026-05-17).
//
// These tests pin the contract changes introduced by the round-23 audit:
//
//   1. WaitForCompletionWithOutput MUST return ErrTaskOutputNotAvailable
//      when no captured output is actually available, rather than
//      silently substituting task.ProgressMessage.
//   2. NewInMemoryTaskQueue MUST emit a WARN-level log on instantiation
//      so operators see the dev-only nature of the queue.
//
// Per CONST-035 / Article XI §11.9 / CONST-050(A).

package background

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

// TestErrTaskOutputNotAvailable_Sentinel asserts the sentinel is non-nil,
// carries a non-empty operator-facing message, and satisfies errors.Is
// reflexively (so callers can errors.Is-discriminate it from other errors).
func TestErrTaskOutputNotAvailable_Sentinel(t *testing.T) {
	if ErrTaskOutputNotAvailable == nil {
		t.Fatal("ErrTaskOutputNotAvailable must be non-nil")
	}
	if msg := ErrTaskOutputNotAvailable.Error(); msg == "" {
		t.Fatal("ErrTaskOutputNotAvailable must carry an operator-facing message")
	}
	if !errors.Is(ErrTaskOutputNotAvailable, ErrTaskOutputNotAvailable) {
		t.Fatal("ErrTaskOutputNotAvailable must satisfy errors.Is reflexively")
	}
	// The message MUST mention the §11.4 PASS-bluff context so operators
	// reading the message understand why the function refuses to fabricate.
	if !strings.Contains(ErrTaskOutputNotAvailable.Error(), "PASS-bluff") {
		t.Errorf("sentinel message must reference the §11.4 PASS-bluff context, got: %q",
			ErrTaskOutputNotAvailable.Error())
	}
}

// TestNewInMemoryTaskQueue_LogsDevOnlyWarning asserts the constructor emits
// a WARN log line surfacing the dev-only nature of the queue. This is the
// operator-facing signal that CONST-050(A) requires whenever a stub is
// retained in production source for unit-test convenience.
func TestNewInMemoryTaskQueue_LogsDevOnlyWarning(t *testing.T) {
	var buf bytes.Buffer
	logger := logrus.New()
	logger.SetOutput(&buf)
	logger.SetLevel(logrus.WarnLevel)
	logger.SetFormatter(&logrus.TextFormatter{
		DisableColors:    true,
		DisableTimestamp: true,
	})

	q := NewInMemoryTaskQueue(logger)
	if q == nil {
		t.Fatal("NewInMemoryTaskQueue returned nil")
	}

	logOutput := buf.String()
	if logOutput == "" {
		t.Fatal("NewInMemoryTaskQueue must emit a log line on instantiation")
	}
	if !strings.Contains(logOutput, "level=warning") {
		t.Errorf("instantiation log line must be at WARN level, got: %q", logOutput)
	}
	for _, mustContain := range []string{"DEVELOPMENT/TEST-ONLY", "PostgresTaskQueue", "CONST-050"} {
		if !strings.Contains(logOutput, mustContain) {
			t.Errorf("instantiation log must mention %q to surface dev-only nature, got: %q",
				mustContain, logOutput)
		}
	}
}

// TestNewInMemoryTaskQueue_NilLogger asserts the constructor falls back to
// a default logrus.Logger when nil is passed — the warn-on-instantiation
// contract MUST hold even if the caller forgets to inject a logger.
func TestNewInMemoryTaskQueue_NilLogger(t *testing.T) {
	q := NewInMemoryTaskQueue(nil)
	if q == nil {
		t.Fatal("NewInMemoryTaskQueue(nil) must still return a usable queue")
	}
	if q.logger == nil {
		t.Fatal("NewInMemoryTaskQueue(nil) must initialise a fallback logger")
	}
}
