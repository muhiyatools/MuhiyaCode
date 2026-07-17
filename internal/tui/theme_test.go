package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// featureLogicFiles lists every non-test TUI source file EXCEPT render.go
// (defines the palette table) and theme.go (builds the Theme) — the files
// that must route color/breakpoints through the Theme rather than embedding
// literals. Discovered dynamically (feature 010 US4 split) so splitting a
// file never silently drops it from the check.
func featureLogicFiles(t *testing.T) []string {
	t.Helper()
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	exempt := map[string]bool{"render.go": true, "theme.go": true}
	var files []string
	for _, f := range matches {
		if strings.HasSuffix(f, "_test.go") || exempt[f] {
			continue
		}
		files = append(files, f)
	}
	sort.Strings(files)
	return files
}

// TestThemePaletteIsSingleSource (US6 T070) proves the feature-logic files route
// all color through the palette rather than embedding raw hex — colors have one
// definition (the palette table), so a theme change is a one-file change.
func TestThemePaletteIsSingleSource(t *testing.T) {
	hexColor := regexp.MustCompile(`#[0-9A-Fa-f]{6}`)
	// render.go defines the palette table; theme.go builds the Theme. Every other
	// TUI source file must be free of raw color literals.
	for _, f := range featureLogicFiles(t) {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if loc := hexColor.FindIndex(src); loc != nil {
			t.Errorf("%s embeds a hard-coded hex color near byte %d — colors belong in the palette", f, loc[0])
		}
	}
}

// TestThemeBreakpointsAreSingleSource (US6 T070) proves the render floor and header
// width breakpoints come from the Theme, not scattered magic literals.
func TestThemeBreakpointsAreSingleSource(t *testing.T) {
	bp := regexp.MustCompile(`m\.width [<>]=? ?(60|80|96)\b|m\.height [<>]=? ?20\b`)
	for _, f := range featureLogicFiles(t) {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if loc := bp.FindIndex(src); loc != nil {
			t.Errorf("%s uses a magic breakpoint literal near byte %d — use m.theme.{FloorWidth,FloorHeight,WideWidth,ExtraWidth}", f, loc[0])
		}
	}
}

// TestThemeCarriesResolvedTables (US6 T006) proves the Theme bundles the resolved
// palette, glyphs, and layout metrics as one value.
func TestThemeCarriesResolvedTables(t *testing.T) {
	th := newTheme(nil)
	if th.FloorWidth != 60 || th.FloorHeight != 20 || th.WideWidth != 80 || th.ExtraWidth != 96 {
		t.Fatalf("theme metrics wrong: %+v", th)
	}
	if th.Gutter != transcriptGutter {
		t.Fatal("theme gutter is not the single transcriptGutter source")
	}
	if len(th.Glyphs.spinner) == 0 {
		t.Fatal("theme did not resolve the glyph table")
	}
}
