package tui

import "github.com/muhiya/muhiyacode/internal/contract"

// Theme is the single source for the TUI visual system (US6 T006/T073): the
// semantic color palette, the glyph table, and the layout metrics that were
// previously scattered as literals across the render code — the render floor, the
// header width breakpoints, and the transcript gutter. It only gathers existing
// values into one auditable place; feature 003's semantics are unchanged.
type Theme struct {
	Palette palette
	Glyphs  glyphs

	FloorWidth  int    // below this width the "too small" screen shows (was 60)
	FloorHeight int    // below this height the "too small" screen shows (was 20)
	WideWidth   int    // header shows the Subagent segment at/above this width (was 80)
	ExtraWidth  int    // header adds the reasoning segment at/above this width (was 96)
	Gutter      string // transcript left gutter
}

// transcriptGutter is the transcript's left margin. It lives here as the single
// definition; Theme mirrors it as Gutter so call sites can read either.
const transcriptGutter = "  "

// newTheme builds the theme from settings, resolving the palette and glyph tables
// once and pairing them with the layout metrics.
func newTheme(settings *contract.Settings) Theme {
	themeName, borderMode := "", ""
	if settings != nil {
		themeName = settings.Theme
		borderMode = settings.UI.BorderMode
	}
	return Theme{
		Palette:     newPalette(themeName),
		Glyphs:      newGlyphs(asciiGlyphs(borderMode)),
		FloorWidth:  60,
		FloorHeight: 20,
		WideWidth:   80,
		ExtraWidth:  96,
		Gutter:      transcriptGutter,
	}
}
