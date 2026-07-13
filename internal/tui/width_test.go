package tui

import "testing"

// TestDisplayWidthHarakatZero (006 T010): combining marks are zero width, so
// vocalized Arabic measures the same as its base letters — the bug the old
// per-rune runewidth loops had.
func TestDisplayWidthHarakatZero(t *testing.T) {
	if got, want := displayWidth(corpusVocalized), displayWidth("مرحبا"); got != want {
		t.Fatalf("vocalized width %d != base width %d", got, want)
	}
	if w := displayWidth("َ"); w != 0 {
		t.Fatalf("lone harakat width = %d, want 0", w)
	}
	if w := displayWidth("main.go"); w != 7 {
		t.Fatalf("pure-LTR width = %d, want 7", w)
	}
}

// TestReverseGraphemesKeepsHarakat (006 T005): reversal keeps each base letter
// with its combining marks (the bug bidi.ReverseString had, Go #50633).
func TestReverseGraphemesKeepsHarakat(t *testing.T) {
	if got := reverseGraphemes("بَ"); got != "بَ" {
		t.Fatalf("single cluster reversed wrong: %q", got)
	}
	if got, want := reverseGraphemes("بَتُ"), "تُبَ"; got != want {
		t.Fatalf("reverseGraphemes = %q, want %q", got, want)
	}
}

// TestTruncateToWidthClusterSafe (006 T003): truncation never splits a cluster
// and measures by display width.
func TestTruncateToWidthClusterSafe(t *testing.T) {
	if got := truncateToWidth("main.go", 4); got != "main" {
		t.Fatalf("truncateToWidth(main.go,4) = %q, want main", got)
	}
	// A vocalized cluster is width 1, so width 3 keeps the first 3 base letters + marks.
	if w := displayWidth(truncateToWidth(corpusVocalized, 3)); w > 3 {
		t.Fatalf("truncated width %d exceeds 3", w)
	}
}
