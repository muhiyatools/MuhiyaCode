package orchestrator

import (
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestContextManifestOrderingFingerprintAndLogicalHash(t *testing.T) {
	tools := NewContextSegment("core-tools", SegmentCoreTools, "definitions", StabilitySession, []byte(`[{"name":"read"}]`), 5, true)
	system := NewContextSegment("system", SegmentSystem, "prompt", StabilitySession, []byte("stable rules"), 3, true)
	if tools.Fingerprint != NewContextSegment("copy", SegmentCoreTools, "other", StabilityRequest, []byte(`[{"name":"read"}]`), 99, false).Fingerprint {
		t.Fatal("fingerprint depends on metadata rather than payload")
	}
	manifest := ContextManifest{RequestSeq: 1}
	if err := manifest.Add(tools); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Add(system); err != nil {
		t.Fatal(err)
	}
	if manifest.Segments[0].ID != "core-tools" || manifest.Segments[1].ID != "system" || manifest.EstimatedTokens != 8 {
		t.Fatalf("manifest order/totals changed: %+v", manifest)
	}
	reversed := ContextManifest{RequestSeq: 1}
	_ = reversed.Add(system)
	_ = reversed.Add(tools)
	if manifest.LogicalHash() == reversed.LogicalHash() {
		t.Fatal("segment reordering did not change logical hash")
	}
	if err := manifest.Add(tools); err == nil {
		t.Fatal("duplicate segment id accepted")
	}
}

func TestContextManifestResidualReconciliation(t *testing.T) {
	manifest := ContextManifest{}
	_ = manifest.Add(NewContextSegment("a", SegmentSystem, "a", StabilitySession, []byte("a"), 10, true))
	_ = manifest.Add(NewContextSegment("b", SegmentUser, "b", StabilityRequest, []byte("b"), 15, true))
	provider := 30
	manifest.Reconcile(&provider)
	if manifest.ProviderPromptTokens == nil || *manifest.ProviderPromptTokens != 30 || manifest.ResidualTokens == nil || *manifest.ResidualTokens != 5 {
		t.Fatalf("reconciliation=%+v", manifest)
	}
	if manifest.EstimatedTokens+*manifest.ResidualTokens != *manifest.ProviderPromptTokens || manifest.MeasurementKind != contract.MeasurementProviderExact {
		t.Fatalf("estimate plus residual does not equal provider truth: %+v", manifest)
	}
	manifest.Reconcile(nil)
	if manifest.ProviderPromptTokens != nil || manifest.ResidualTokens != nil || manifest.MeasurementKind != contract.MeasurementCalibratedEstimate {
		t.Fatalf("unavailable provider total was fabricated: %+v", manifest)
	}
}

func TestContextManifestCloneIsIndependentAndDebugRenderHasNoSecrets(t *testing.T) {
	secret := "sk-super-secret-provider-value"
	start, end, relevance := 1, 9, 0.75
	segment := NewContextSegment("user\nsecret", SegmentUser, `C:\private\secret.txt`, StabilityRequest, []byte(secret), 7, true)
	segment.ByteStart, segment.ByteEnd, segment.RelevanceScore = &start, &end, &relevance
	manifest := ContextManifest{RequestSeq: 9, WireHash: "wire"}
	_ = manifest.Add(segment)
	provider := 8
	manifest.Reconcile(&provider)
	clone := manifest.Clone()
	*clone.Segments[0].ByteStart = 99
	*clone.ProviderPromptTokens = 100
	clone.Segments[0].ID = "changed"
	if *manifest.Segments[0].ByteStart != 1 || *manifest.ProviderPromptTokens != 8 || manifest.Segments[0].ID == clone.Segments[0].ID {
		t.Fatalf("clone aliases source: source=%+v clone=%+v", manifest, clone)
	}
	rendered := manifest.DebugRender()
	for _, forbidden := range []string{secret, `C:\private\secret.txt`, "\nsecret"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("debug render exposed %q: %s", forbidden, rendered)
		}
	}
	if !strings.Contains(rendered, segment.Fingerprint) || !strings.Contains(rendered, "bytes=") {
		t.Fatalf("debug render lacks safe attribution: %s", rendered)
	}
}
