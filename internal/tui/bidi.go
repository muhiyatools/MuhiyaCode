package tui

import (
	"strings"
	"unicode"
)

// Bidirectional run segmentation (feature 006 T006/T007/T023). One logical line is
// split into maximal RTL (Arabic) and LTR (everything else) runs in LOGICAL order.
// The display pass then orders them for the screen and shapes/reverses only RTL
// runs. Grouping ALL non-Arabic into contiguous LTR runs — a coding-oriented
// simplification of UAX #9 — keeps technical tokens (paths, URLs, inline code,
// bracketed calls like f(x), multi-word English) intact, which x/text/bidi
// fragments today (bracket bug Go #72089; weak-separator/number fragmentation).
// Research R5/R8 explicitly recommends this "segment tokens yourself" approach.

// dirRun is one maximal single-direction span of a line, in logical order.
type dirRun struct {
	text string
	rtl  bool
}

// segmentRuns splits logical into directional runs in logical order. A neutral
// character (space/punctuation) attaches to the current run so a space between two
// Arabic words doesn't start a spurious LTR run, and technical punctuation (dots,
// slashes, brackets) stays inside its LTR token.
func segmentRuns(logical string) []dirRun {
	var runs []dirRun
	var b strings.Builder
	curRTL, started := false, false
	flush := func() {
		if b.Len() > 0 {
			runs = append(runs, dirRun{text: b.String(), rtl: curRTL})
			b.Reset()
		}
	}
	for _, r := range logical {
		if started && unicode.IsSpace(r) {
			b.WriteRune(r) // neutral: glue to the current run
			continue
		}
		rtl := isRTLRune(r)
		if !started || rtl != curRTL {
			flush()
			curRTL, started = rtl, true
		}
		b.WriteRune(r)
	}
	flush()
	return runs
}

// dominantRTL reports whether a line is primarily right-to-left, deciding both the
// bidi base direction and (for align=auto) the block alignment. It compares strong
// RTL letters against strong LTR letters; a line with more Arabic than Latin is
// RTL-dominant. Neutral characters (spaces, punctuation, numbers, code) don't vote,
// so a mostly-Arabic line with an English identifier stays RTL-dominant.
func dominantRTL(s string) bool {
	rtl, ltr := 0, 0
	for _, r := range s {
		switch {
		case isRTLRune(r):
			rtl++
		case unicode.IsLetter(r) && r < 0x0590:
			ltr++
		}
	}
	return rtl > 0 && rtl >= ltr
}

// isRTLRune reports whether r is a strong right-to-left character (Arabic, Hebrew,
// and the Arabic presentation-form ranges), matching the detection in rtl.go.
func isRTLRune(r rune) bool {
	return (r >= 0x0590 && r <= 0x08FF) ||
		(r >= 0xFB1D && r <= 0xFDFF) ||
		(r >= 0xFE70 && r <= 0xFEFF)
}

// DisplayLine is the rendered form of one logical line (data-model.md): the visual
// string for the screen, the originating logical text (source of truth for copy),
// and the resolved alignment. Shaping/BiDi is a pure function that never mutates
// the logical text.
type DisplayLine struct {
	Visual  string
	Logical string
	Align   string // "left" | "right"
}

// resolveAlign maps the RTL.Align setting to a concrete side for this line.
func resolveAlign(logical, align string) string {
	switch strings.ToLower(strings.TrimSpace(align)) {
	case "right":
		return "right"
	case "left":
		return "left"
	default: // auto
		if dominantRTL(logical) {
			return "right"
		}
		return "left"
	}
}
