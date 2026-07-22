package evidence

import (
	"strings"
	"testing"
)

func TestReduceDiffPreservesPathsHunksAndFingerprints(t *testing.T) {
	raw := "--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-old\n+new"
	card := ReduceDiff(DiffReductionInput{Status: "applied", Complete: true, Text: raw, Artifact: "artifact_x", BeforeFingerprint: "old", AfterFingerprint: "new"})
	rendered := card.Render()
	if !strings.Contains(rendered, "changed_paths=a.go") || !strings.Contains(rendered, "fingerprint=old->new") || !strings.Contains(rendered, "+new") {
		t.Fatalf("rendered=%q", rendered)
	}
}

func TestReduceDiffPreservesOutcomeClassesAndModes(t *testing.T) {
	for _, test := range []DiffReductionInput{
		{Status: "partial", Partial: true}, {Status: "mismatch", Mismatch: true}, {Status: "idempotent", Idempotent: true}, {Status: "applied", Applied: true, BeforeMode: "0644", AfterMode: "0755"},
	} {
		rendered := ReduceDiff(test).Render()
		if !strings.Contains(rendered, "status="+test.Status) {
			t.Fatalf("status lost: %q", rendered)
		}
		if test.BeforeMode != "" && !strings.Contains(rendered, "mode=0644->0755") {
			t.Fatalf("mode lost: %q", rendered)
		}
	}
}
