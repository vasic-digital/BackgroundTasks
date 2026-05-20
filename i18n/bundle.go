// bundle.go provides a YAML-backed Translator implementation for
// BackgroundTasks' CONST-046 i18n seam (round-375 §11.4).
//
// CONST-051(B): the loader is project-not-aware. It accepts an
// io.Reader or a []byte so the consuming binary owns the file path
// — the package never reaches into a parent project's tree.
package i18n

import (
	"context"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// BundleTranslator resolves message IDs against a flat
// messageID -> template map loaded from a YAML bundle. Placeholder
// interpolation replaces occurrences of "{{name}}" in the template
// with the corresponding templateData value.
type BundleTranslator struct {
	messages map[string]string
}

// NewBundleTranslatorFromBytes parses a YAML message bundle into a
// BundleTranslator. Accepts both the flat dialect (messageID:
// "template") and the go-i18n nested dialect (messageID:\n
// other: "template") so bundles authored in either form load
// without loss.
func NewBundleTranslatorFromBytes(data []byte) (*BundleTranslator, error) {
	var generic map[string]any
	if err := yaml.Unmarshal(data, &generic); err != nil {
		return nil, fmt.Errorf("i18n: parse bundle: %w", err)
	}
	raw := make(map[string]string, len(generic))
	for id, val := range generic {
		switch v := val.(type) {
		case string:
			raw[id] = v
		case map[string]any:
			if other, ok := v["other"].(string); ok {
				raw[id] = other
				continue
			}
			return nil, fmt.Errorf("i18n: message id %q: nested entry missing string \"other\" key", id)
		case nil:
			raw[id] = ""
		default:
			return nil, fmt.Errorf("i18n: message id %q: unsupported value type %T", id, val)
		}
	}
	return &BundleTranslator{messages: raw}, nil
}

// NewBundleTranslatorFromReader reads a YAML map from r and parses it
// into a BundleTranslator.
func NewBundleTranslatorFromReader(r io.Reader) (*BundleTranslator, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("i18n: read bundle: %w", err)
	}
	return NewBundleTranslatorFromBytes(data)
}

// T resolves messageID against the loaded bundle. An unknown
// messageID returns an error so the caller can fall back to the
// loud NoopTranslator echo via Tr.
func (b *BundleTranslator) T(_ context.Context, messageID string, templateData map[string]any) (string, error) {
	tmpl, ok := b.messages[messageID]
	if !ok {
		return "", fmt.Errorf("i18n: unknown message id %q", messageID)
	}
	return interpolate(tmpl, templateData), nil
}

// interpolate replaces "{{key}}" occurrences in tmpl with the
// stringified templateData values. Keys absent from templateData
// are left untouched so a missing placeholder is visible.
func interpolate(tmpl string, templateData map[string]any) string {
	if len(templateData) == 0 {
		return tmpl
	}
	out := tmpl
	for k, v := range templateData {
		out = strings.ReplaceAll(out, "{{"+k+"}}", fmt.Sprintf("%v", v))
	}
	return out
}
