package tui

import (
	"strings"

	"github.com/rivo/uniseg"
)

// Grapheme-cluster width helpers (feature 006 T003). Arabic combining marks
// (harakat) must count as zero width, but per-rune go-runewidth.RuneWidth returns
// 1 for them (its combining table has no U+0600–U+06FF entries), so summing it
// over runes over-measures vocalized Arabic. All width math funnels through
// uniseg, which measures by grapheme cluster and is correct for harakat, CJK, and
// emoji in every case (including defective/leading combining marks).

// displayWidth returns the terminal column width of s, measured by grapheme
// cluster (harakat = 0, base+marks = 1, CJK/emoji = 2).
func displayWidth(s string) int { return uniseg.StringWidth(s) }

// truncateToWidth returns the longest prefix of s whose display width is <= width,
// never splitting a grapheme cluster.
func truncateToWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if uniseg.StringWidth(s) <= width {
		return s
	}
	var b strings.Builder
	used := 0
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		w := g.Width()
		if used+w > width {
			break
		}
		b.WriteString(string(g.Runes()))
		used += w
	}
	return b.String()
}

// truncateToWidthRight returns the longest SUFFIX of s whose display width is
// <= width, never splitting a grapheme cluster. It is the RTL-correct counterpart
// of truncateToWidth: on a right-aligned Arabic line the visual string's rightmost
// clusters are the logical FIRST word, so an overflowing line must be cut from the
// LEFT (dropping later words) to keep the sentence start at the right edge — cutting
// the prefix instead would drop the first word off the right (006 follow-up).
func truncateToWidthRight(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if uniseg.StringWidth(s) <= width {
		return s
	}
	var clusters []string
	var widths []int
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		clusters = append(clusters, string(g.Runes()))
		widths = append(widths, g.Width())
	}
	used, start := 0, len(clusters)
	for i := len(clusters) - 1; i >= 0; i-- {
		if used+widths[i] > width {
			break
		}
		used += widths[i]
		start = i
	}
	return strings.Join(clusters[start:], "")
}

// reverseGraphemes reverses the order of grapheme clusters in s while keeping each
// cluster's internal order intact — so an Arabic base letter stays with its
// combining marks (harakat). This replaces bidi.ReverseString, which mis-
// associates combining marks after reversal (Go issue #50633).
func reverseGraphemes(s string) string {
	clusters := make([]string, 0, len(s))
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		clusters = append(clusters, string(g.Runes()))
	}
	var b strings.Builder
	for i := len(clusters) - 1; i >= 0; i-- {
		b.WriteString(clusters[i])
	}
	return b.String()
}

// padToWidth left- or right-aligns visual to width by adding plain spaces,
// measuring with grapheme-cluster width. A visual already at/over width is
// returned unchanged (callers truncate separately when needed).
func padToWidth(visual string, width int, align string) string {
	pad := width - displayWidth(visual)
	if pad <= 0 || width <= 0 {
		return visual
	}
	spaces := strings.Repeat(" ", pad)
	if align == "right" {
		return spaces + visual
	}
	return visual
}
