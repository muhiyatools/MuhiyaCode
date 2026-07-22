package orchestrator

import (
	"regexp"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/evidence"
)

func TestBalancedVirtualizesVerboseOutputAndRawArtifactRoundTrips(t *testing.T) {
	store, _ := evidence.NewStore(t.TempDir(), 1<<20)
	settings := engineSettings()
	settings.TokenEconomyMode = "balanced"
	engine := &Engine{settings: &settings, session: contract.Session{ID: "s"}, artifactStore: store, workspaceID: "w"}
	raw := strings.Repeat("line with complete raw evidence\n", 500)
	outcome := engine.virtualizeToolOutcome(toolOutcome{Call: contract.NewToolCall("r", "read_file", `{"path":"a.go"}`), Output: raw})
	if outcome.Output == raw || len(outcome.Output) >= len(raw) || !strings.Contains(outcome.Output, "artifact=") {
		t.Fatalf("output was not virtualized: %d -> %d", len(raw), len(outcome.Output))
	}
	t.Logf("prompt-growth bytes raw=%d card=%d reduction=%.2f%%", len(raw), len(outcome.Output), 100*(1-float64(len(outcome.Output))/float64(len(raw))))
	handle := regexp.MustCompile(`artifact_[a-f0-9]{24}`).FindString(outcome.Output)
	fetched, _, err := store.Fetch(handle, "s", "w", 0, int64(len(raw)))
	if err != nil || string(fetched) != raw {
		t.Fatalf("raw artifact did not round-trip: bytes=%d err=%v", len(fetched), err)
	}
}

func TestObserveStoresButDoesNotRewriteVerboseOutput(t *testing.T) {
	store, _ := evidence.NewStore(t.TempDir(), 1<<20)
	settings := engineSettings()
	settings.TokenEconomyMode = "observe"
	engine := &Engine{settings: &settings, session: contract.Session{ID: "s"}, artifactStore: store, workspaceID: "w"}
	raw := strings.Repeat("verbose\n", 1000)
	outcome := engine.virtualizeToolOutcome(toolOutcome{Call: contract.NewToolCall("r", "read_file", `{"path":"a.go"}`), Output: raw})
	if outcome.Output != raw {
		t.Fatal("observe mode changed model-visible tool output")
	}
}

func TestInspectionDeduplicatesAgainstIntactArtifactAfterHistoryEviction(t *testing.T) {
	store, _ := evidence.NewStore(t.TempDir(), 1<<20)
	settings := engineSettings()
	settings.TokenEconomyMode = "balanced"
	inspection := NewInspection(InspectionSnapshot{Version: 3}, nil)
	engine := &Engine{settings: &settings, session: contract.Session{ID: "s"}, artifactStore: store, workspaceID: "w", inspection: inspection}
	inspection.SetEvidenceIntact(func(handle, hash string) bool {
		metadata, err := store.Metadata(handle)
		return err == nil && metadata.ContentHash == hash
	})
	call := contract.NewToolCall("r", "grep", `{"pattern":"needle"}`)
	raw := strings.Repeat("a.go:1:needle\n", 500)
	inspection.Record(call, raw)
	engine.virtualizeToolOutcome(toolOutcome{Call: call, Output: raw})
	if _, duplicate := inspection.Duplicate(call, func(string) bool { return false }); !duplicate {
		t.Fatal("intact artifact did not preserve duplicate evidence after history eviction")
	}
}
