package evidence

import (
	"strings"
	"testing"
)

func TestReduceFilePreservesCoordinatesOmissionsSkippedAndIncompleteNegative(t *testing.T) {
	card := ReduceFile(FileReductionInput{Kind: "search", Source: "a.go", Status: "success", Complete: false, Text: "", StartLine: 10, MaxLines: 2, Skipped: []string{"vendor"}})
	rendered := card.Render()
	if !strings.Contains(rendered, "absence is not proven") || !strings.Contains(rendered, "skipped=vendor") {
		t.Fatalf("rendered=%q", rendered)
	}
	card = ReduceFile(FileReductionInput{Kind: "read", Source: "a.go", Status: "success", Complete: true, Text: "a\nb\nc", StartLine: 10, MaxLines: 2})
	if card.Excerpts[0].StartLine != 10 || card.Excerpts[0].EndLine != 11 || card.Excerpts[0].Text != "a\nb" || card.OmittedLines != 1 {
		t.Fatalf("card=%+v", card)
	}
}

func TestFileListGlobAndSearchKindsRemainExplicit(t *testing.T) {
	for _, kind := range []string{"file", "read", "search", "list", "glob"} {
		rendered := ReduceFile(FileReductionInput{Kind: kind, Source: ".", Status: "success", Complete: true, Text: "x"}).Render()
		if !strings.Contains(rendered, "summary="+kind+" result") {
			t.Fatalf("kind %s lost: %q", kind, rendered)
		}
	}
}
