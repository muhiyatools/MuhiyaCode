package evidence

import (
	"strings"
	"testing"
)

func TestObservationCardIsDeterministicTruthfulBoundedAndExact(t *testing.T) {
	card := ObservationCard{Status: "failure", Complete: false, Artifact: "artifact_123", Summary: "tests failed", Facts: []string{"z", "a"}, MaxTokens: 120, Excerpts: []Excerpt{
		{Source: "z.go", StartLine: 9, EndLine: 9, Text: "exact z"},
		{Source: "a.go", StartLine: 2, EndLine: 2, Text: "expected 2 got 3", Protected: true},
	}, Skipped: []string{"vendor", "node_modules"}}
	first, second := card.Render(), card.Render()
	if first != second || !strings.Contains(first, "status=failure complete=false") || !strings.Contains(first, "expected 2 got 3") || strings.Index(first, "fact=a") > strings.Index(first, "fact=z") {
		t.Fatalf("render=%q", first)
	}
	if len(first) > card.MaxTokens*4+3 {
		t.Fatalf("card exceeded token proxy: %d", len(first))
	}
}

func TestObservationCardMarksDegradedNoStore(t *testing.T) {
	got := (ObservationCard{Status: "success", Complete: true, Degraded: true}).Render()
	if !strings.Contains(got, "artifact=unavailable") || !strings.Contains(got, "degraded") {
		t.Fatalf("degraded marker=%q", got)
	}
}
