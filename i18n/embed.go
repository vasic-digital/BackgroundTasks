// embed.go ships the active English bundle inside the i18n package so
// BackgroundTasks resolves its CONST-046-migrated literals correctly
// even before a consuming application wires a locale-aware Translator.
//
// CONST-051(B) decoupling: the embedded bundle carries only this
// module's own message IDs — it is project-not-aware. A consumer that
// wants additional locales calls SetTranslator with its own
// locale-aware implementation; the embedded en bundle remains the
// safe default rather than the loud message-ID echo.
package i18n

import (
	_ "embed"
)

//go:embed bundles/active.en.yaml
var embeddedEnBundle []byte

// DefaultTranslator returns a BundleTranslator loaded from the
// embedded active English bundle. It is the recommended default for
// consuming applications that do not need multi-locale support.
func DefaultTranslator() (*BundleTranslator, error) {
	return NewBundleTranslatorFromBytes(embeddedEnBundle)
}

// MustDefaultTranslator is DefaultTranslator with a panic on failure.
// The embedded bundle is compiled in, so a failure here indicates a
// build-time corruption — fail loud rather than ship a silent echo.
func MustDefaultTranslator() *BundleTranslator {
	t, err := DefaultTranslator()
	if err != nil {
		panic("i18n: embedded en bundle failed to load: " + err.Error())
	}
	return t
}

// init installs the embedded English BundleTranslator as the
// process-wide default so BackgroundTasks resolves its
// CONST-046-migrated literals to real English text out of the box.
// A consuming application that needs other locales overrides this by
// calling SetTranslator at boot with its own locale-aware Translator.
func init() {
	SetTranslator(MustDefaultTranslator())
}
