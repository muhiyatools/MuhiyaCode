package tui

import (
	"strings"
	"testing"
)

// TestRenderForDisplayShapesReordersAligns (006 T009): Arabic is shaped, reordered,
// and right-aligned; identical inputs are byte-identical (purity).
func TestRenderForDisplayShapesReordersAligns(t *testing.T) {
	a := renderForDisplay(corpusPlain, "visual", "auto")
	b := renderForDisplay(corpusPlain, "visual", "auto")
	if a != b {
		t.Fatal("renderForDisplay not deterministic")
	}
	if a.Visual == a.Logical {
		t.Fatalf("Arabic should transform for display: %q", a.Visual)
	}
	if a.Align != "right" {
		t.Fatalf("Arabic-dominant align = %q, want right", a.Align)
	}
	if a.Logical != corpusPlain {
		t.Fatal("logical source must be preserved unchanged")
	}
}

// TestRenderForDisplayLTRNoop (006 T032/SC-008): pure-LTR is byte-identical & left.
func TestRenderForDisplayLTRNoop(t *testing.T) {
	dl := renderForDisplay(corpusLTROnly, "visual", "auto")
	if dl.Visual != corpusLTROnly || dl.Align != "left" {
		t.Fatalf("pure-LTR must be byte-identical & left-aligned: %+v", dl)
	}
}

// TestRenderForDisplayOffAndNative (006 T032): off = identity; native emits logical
// (terminal reorders) but still right-aligns dominant-RTL.
func TestRenderForDisplayOffAndNative(t *testing.T) {
	if dl := renderForDisplay(corpusPlain, "off", "auto"); dl.Visual != corpusPlain {
		t.Fatalf("off mode must be identity: %q", dl.Visual)
	}
	dl := renderForDisplay(corpusPlain, "native", "auto")
	if dl.Visual != corpusPlain {
		t.Fatalf("native must emit logical text: %q", dl.Visual)
	}
	if dl.Align != "right" {
		t.Fatalf("native still right-aligns dominant-RTL, got %q", dl.Align)
	}
}

// TestRenderForDisplayMixedKeepsLTR (006 T022): LTR tokens stay intact in visual.
func TestRenderForDisplayMixedKeepsLTR(t *testing.T) {
	for _, c := range []struct{ in, token string }{
		{corpusMixedFile, "main.go"},
		{corpusMixedNum, "42"},
		{corpusMixedURL, "https://muhiya.com"},
	} {
		if dl := renderForDisplay(c.in, "visual", "auto"); !strings.Contains(dl.Visual, c.token) {
			t.Fatalf("LTR token %q not preserved in visual of %q: %q", c.token, c.in, dl.Visual)
		}
	}
}

// TestShapeLamAlefLigature (006 T004): LAM+ALEF collapses to one ligature glyph.
func TestShapeLamAlefLigature(t *testing.T) {
	shaped := shapeArabic("لا")
	rs := []rune(shaped)
	if len(rs) != 1 {
		t.Fatalf("LAM-ALEF should be one ligature glyph, got %d runes: %q", len(rs), shaped)
	}
	if rs[0] != 'ﻻ' && rs[0] != 'ﻼ' {
		t.Fatalf("LAM-ALEF ligature wrong: %U", rs[0])
	}
}

// TestEngDominantLeftAligned (006 T024): an English-dominant line with one Arabic
// word is not force-flipped (base direction stays LTR / left).
func TestEngDominantLeftAligned(t *testing.T) {
	if dl := renderForDisplay(corpusEngDom, "visual", "auto"); dl.Align != "left" {
		t.Fatalf("English-dominant line align = %q, want left", dl.Align)
	}
}
