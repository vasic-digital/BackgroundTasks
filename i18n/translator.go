// Package i18n declares BackgroundTasks' hardcoded-content abstraction
// per CONST-046 (round-375 §11.4 anti-bluff sweep, 2026-05-19).
//
// Mirrors the "consumer defines its own Translator interface" pattern
// of every prior CONST-046-migrated package in the HelixCode codebase
// (reference seam: helix_code/internal/approval/i18n/translator.go;
// most recently vasic-digital/LLMProvider round-367).
//
// CONST-051(B) decoupling: this package is project-not-aware. It
// declares only the Translator contract and a loud-echo NoopTranslator
// default. A consuming application wires a real Translator at boot via
// SetTranslator; until then Tr() echoes the message ID verbatim —
// never a silent swallow (which would itself be a §11.4 PASS-bluff at
// the i18n layer).
package i18n

import (
	"context"
	"sync"
)

// Translator is the contract BackgroundTasks uses for every
// CONST-046-migrated user-facing string.
type Translator interface {
	// T resolves messageID against the active locale. templateData
	// supplies named placeholders for go-i18n style interpolation;
	// pass nil when the message has no placeholders.
	T(ctx context.Context, messageID string, templateData map[string]any) (string, error)
}

// NoopTranslator returns the messageID verbatim. SAFETY default for
// unit tests + backward-compat for callers that have not yet wired a
// real Translator. Production paths in a consuming application MUST
// inject a real Translator via SetTranslator.
type NoopTranslator struct{}

// T returns id unchanged (loud echo). Never returns an error.
func (NoopTranslator) T(_ context.Context, id string, _ map[string]any) (string, error) {
	return id, nil
}

var (
	mu      sync.RWMutex
	current Translator = NoopTranslator{}
)

// SetTranslator installs the process-wide Translator. A consuming
// application calls this once at boot with a real (locale-aware)
// implementation. Passing nil resets to NoopTranslator so the loud-echo
// fallback can never be disabled into a silent swallow.
func SetTranslator(t Translator) {
	mu.Lock()
	defer mu.Unlock()
	if t == nil {
		current = NoopTranslator{}
		return
	}
	current = t
}

// CurrentTranslator returns the active Translator (NoopTranslator when
// none wired). Exported for tests that need to assert wiring state.
func CurrentTranslator() Translator {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// Tr resolves msgID against the active Translator using ctx for locale
// selection. data carries named placeholders (nil when none). On any
// translator error Tr falls back to the message ID — a loud, visible
// echo, never an empty string.
func Tr(ctx context.Context, msgID string, data map[string]any) string {
	mu.RLock()
	t := current
	mu.RUnlock()
	out, err := t.T(ctx, msgID, data)
	if err != nil || out == "" {
		return msgID
	}
	return out
}
