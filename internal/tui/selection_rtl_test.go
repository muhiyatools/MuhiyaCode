package tui

import (
	"testing"

	"golang.org/x/text/unicode/norm"
)

// TestCopyLogicalRoundTrip (006 T025/FR-015): copying shaped/reordered Arabic
// recovers logical text — no presentation forms, correct order — and LTR is
// unchanged.
func TestCopyLogicalRoundTrip(t *testing.T) {
	// Pure LTR: byte-identical.
	if v := renderForDisplay(corpusLTROnly, "visual", "auto").Visual; recoverLogical(v) != corpusLTROnly {
		t.Fatalf("LTR copy changed: %q", recoverLogical(v))
	}
	// Pure Arabic (no ligature): exact logical recovery.
	if v := renderForDisplay(corpusPlain, "visual", "auto").Visual; recoverLogical(v) != corpusPlain {
		t.Fatalf("Arabic copy round-trip: got %q want %q", recoverLogical(v), corpusPlain)
	}
	// Vocalized Arabic: base letters + marks recovered (NFC-equal).
	if v := renderForDisplay(corpusVocalized, "visual", "auto").Visual; norm.NFC.String(recoverLogical(v)) != norm.NFC.String(corpusVocalized) {
		t.Fatalf("vocalized copy round-trip: got %q want %q", recoverLogical(v), corpusVocalized)
	}
	// No presentation forms ever reach the clipboard, for any corpus sample.
	for _, c := range rtlCorpus {
		if got := recoverLogical(renderForDisplay(c, "visual", "auto").Visual); hasPresentationForms(got) {
			t.Fatalf("presentation forms leaked into copy of %q: %q", c, got)
		}
	}
}

// TestNFCComposesArabic (006 T026/FR-013): NFC composes decomposed Arabic and
// never introduces presentation forms — what the model receives at submit.
func TestNFCComposesArabic(t *testing.T) {
	decomposed := "آ" // ALEF + COMBINING MADDA ABOVE
	got := norm.NFC.String(decomposed)
	if got != "آ" { // ALEF WITH MADDA ABOVE (composed)
		t.Fatalf("NFC did not compose alef-madda: %q", got)
	}
	if hasPresentationForms(norm.NFC.String(corpusVocalized)) {
		t.Fatal("NFC must not introduce presentation forms")
	}
}
