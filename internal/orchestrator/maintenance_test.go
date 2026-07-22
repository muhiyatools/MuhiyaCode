package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestPressureInputSwitchesFromEstimateToProviderTokens(t *testing.T) {
	history := NewHistory(HistorySnapshot{Version: 1}, nil)
	history.Append(contract.Message{Role: contract.RoleUser, Content: strings.Repeat("x", 400)})
	bootstrap := history.PressureInput(0, false)
	if !bootstrap.Estimated || bootstrap.Tokens <= 0 {
		t.Fatalf("bootstrap pressure=%+v", bootstrap)
	}
	reported := history.PressureInput(123, true)
	if reported.Estimated || reported.Tokens != 123 {
		t.Fatalf("reported pressure=%+v", reported)
	}
}

// TestTokenCalibration (T038 / REV inventory #26) verifies the estimator
// calibrates its tokens/char ratio from real provider usage, rejects
// out-of-range samples, and stays sane on multi-byte (CJK) content.
func TestTokenCalibration(t *testing.T) {
	h := NewHistory(HistorySnapshot{Version: 1}, nil)
	h.Append(contract.Message{Role: contract.RoleUser, Content: strings.Repeat("a", 400)})
	base := h.EstimatedTokens() // uncalibrated ~0.25/char fallback
	if base <= 0 {
		t.Fatal("uncalibrated estimate should be positive")
	}
	// 200 tokens for 100 system chars + 400 message chars = 500 chars → 0.4/char.
	h.Calibrate(200, 100)
	if calibrated := h.EstimatedTokens(); calibrated <= base {
		t.Fatalf("calibrated estimate (%d) should reflect the higher 0.4 ratio vs the 0.25 fallback (%d)", calibrated, base)
	}
	// An out-of-range ratio must be rejected so a bad sample can't poison it.
	before := h.EstimatedTokens()
	h.Calibrate(1_000_000, 1) // ratio ≫ 2
	if h.EstimatedTokens() != before {
		t.Fatal("out-of-range calibration ratio must be rejected")
	}
	// CJK (multi-byte) content: estimation must not panic and stays positive.
	hc := NewHistory(HistorySnapshot{Version: 1}, nil)
	hc.Append(contract.Message{Role: contract.RoleUser, Content: strings.Repeat("字", 100)})
	if hc.EstimatedTokens() <= 0 {
		t.Fatal("CJK estimate should be positive")
	}
	hc.Calibrate(120, 0)
	if hc.EstimatedTokens() <= 0 {
		t.Fatal("calibrated CJK estimate should be positive")
	}
}

func TestRequestCalibrationExcludesUnsentArchivedHistory(t *testing.T) {
	h := NewHistory(HistorySnapshot{Version: 1}, nil)
	h.Append(contract.Message{Role: contract.RoleUser, Content: strings.Repeat("archived", 100_000)})
	sent := []contract.Message{{Role: contract.RoleSystem, Content: strings.Repeat("s", 200)}}
	h.CalibrateRequest(100, sent, 100)
	if h.tokPerChar <= 0.1 {
		t.Fatalf("calibration was diluted by unsent history: ratio=%f", h.tokPerChar)
	}
}

func TestCombinedMaintenanceUsesOneRewriteAndAntiThrashLatch(t *testing.T) {
	promptTokens := 100_000
	settings := engineSettings()
	history := NewHistory(HistorySnapshot{Version: 1}, nil)
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry(), History: history,
		InitialUsageRecords: []contract.UsageRecord{{Seq: 1, PromptTokens: &promptTokens}},
	})
	if err != nil {
		t.Fatal(err)
	}
	addCompletedToolPayload(history, "c1")
	history.MarkTaskStart()
	before := history.RewriteVersion()
	first, err := engine.runMaintenanceBoundary(context.Background(), Profile(contract.EffortMedium))
	if err != nil || !first.Changed || history.RewriteVersion() != before+1 {
		t.Fatalf("first=%+v version=%d err=%v", first, history.RewriteVersion(), err)
	}
	addCompletedToolPayload(history, "c2")
	history.MarkTaskStart()
	// The first rewrite invalidates the previous provider measurement. Simulate
	// the fresh measurement that a real intervening request would produce.
	engine.taskMu.Lock()
	engine.latestPromptTokens = promptTokens
	engine.latestPromptAvailable = true
	engine.latestPromptRewriteVersion = history.RewriteVersion()
	engine.taskMu.Unlock()
	second, err := engine.runMaintenanceBoundary(context.Background(), Profile(contract.EffortMedium))
	if err != nil || !second.Changed {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if report := engine.ContextReport(); !report.MaintenanceLatched || !report.PressureEstimated {
		t.Fatalf("maintenance diagnostics=%+v", report)
	}
	addCompletedToolPayload(history, "c3")
	history.MarkTaskStart()
	version := history.RewriteVersion()
	third, err := engine.runMaintenanceBoundary(context.Background(), Profile(contract.EffortMedium))
	if err != nil || third.Changed || history.RewriteVersion() != version {
		t.Fatalf("latched pass changed history: result=%+v version=%d err=%v", third, history.RewriteVersion(), err)
	}
}

func TestUserCompactRecordsInvalidation(t *testing.T) {
	settings := engineSettings()
	provider := &scriptedProvider{responses: []contract.ChatResponse{{Content: "summary", Usage: contract.Usage{PromptTokens: 10, PromptTokensAvailable: true}}}}
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	events := engine.InvalidationEvents()
	if len(events) != 1 || events[0].Cause != contract.InvalidationUserCompact || events[0].RequestSeq != 2 {
		t.Fatalf("events=%+v", events)
	}
}

func TestBootstrapWindowDropUsesBoundaryTriggerBelowPressureFloor(t *testing.T) {
	promptTokens := 100
	history := NewHistory(HistorySnapshot{
		Version: 1, WindowInitialized: true,
		Messages: []contract.Message{
			{Role: contract.RoleUser, Content: strings.Repeat("a", 300_000)},
			{Role: contract.RoleUser, Content: strings.Repeat("b", 300_000)},
			{Role: contract.RoleUser, Content: strings.Repeat("c", 300_000)},
		},
	}, nil)
	settings := engineSettings()
	provider := &scriptedProvider{responses: []contract.ChatResponse{{Content: "done", Usage: contract.Usage{PromptTokens: 100, PromptTokensAvailable: true}}}}
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry(), History: history, InitialUsageRecords: []contract.UsageRecord{{Seq: 1, Stream: contract.UsageStreamMain, PromptTokens: &promptTokens}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.Run(context.Background(), "continue"); err != nil {
		t.Fatal(err)
	}
	events := engine.InvalidationEvents()
	if len(events) != 1 || events[0].Cause != contract.InvalidationWindowDrop || events[0].Trigger != contract.InvalidationBoundary {
		t.Fatalf("events=%+v", events)
	}
}

// TestMaintenanceSkipsBelowYieldFloorWithoutMutationOrEvent (T009 / REV A1)
// verifies the estimate-before-mutate gate: when pressure is in the maintenance
// band [0.60, 0.80) but the reclaimable yield is below the 5% floor, the
// boundary skips ENTIRELY — history is byte-identical (RewriteVersion
// unchanged), no invalidation event is recorded, and no latch slot is consumed.
// This is the fix for the trap where a trim-only Maintain mutated the prefix and
// then returned without an event, hard-failing the next request's shape guard.
func TestMaintenanceSkipsBelowYieldFloorWithoutMutationOrEvent(t *testing.T) {
	// 80_000 / (128000-12000 usable) = ~0.69 → in-band, above the 0.60 floor and
	// below the 0.80 hard-fold, so the skip gate is active.
	promptTokens := 80_000
	settings := engineSettings()
	history := NewHistory(HistorySnapshot{Version: 1}, nil)
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry(), History: history,
		InitialUsageRecords: []contract.UsageRecord{{Seq: 1, Stream: contract.UsageStreamMain, PromptTokens: &promptTokens}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// A completed region whose messages are all small: nothing exceeds the fold
	// keep-size or the trim threshold, so the estimated yield is ~0.
	history.Append(contract.Message{Role: contract.RoleUser, Content: "small task"})
	history.Append(contract.Message{Role: contract.RoleAssistant, Content: "small reply"})
	history.MarkTaskStart()
	beforeVersion := history.RewriteVersion()

	result, err := engine.runMaintenanceBoundary(context.Background(), Profile(contract.EffortMedium))
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed {
		t.Fatalf("low-yield boundary must not mutate: %+v", result)
	}
	if history.RewriteVersion() != beforeVersion {
		t.Fatalf("history rewritten on a skipped pass: version %d != %d", history.RewriteVersion(), beforeVersion)
	}
	if events := engine.InvalidationEvents(); len(events) != 0 {
		t.Fatalf("skipped pass recorded an invalidation event: %+v", events)
	}
	if report := engine.ContextReport(); report.MaintenanceLatched {
		t.Fatal("skipped pass consumed a latch slot")
	}
}

// TestCompactionAccumulatesDigests (T042) verifies a second compaction PRESERVES
// the prior digest byte-identical and appends the new one, rather than lossy
// re-summarization that would drop earlier facts.
func TestCompactionAccumulatesDigests(t *testing.T) {
	h := NewHistory(HistorySnapshot{Version: 1}, nil)
	for _, c := range []string{"u1", "a1", "u2", "a2"} {
		role := contract.RoleUser
		if c[0] == 'a' {
			role = contract.RoleAssistant
		}
		h.Append(contract.Message{Role: role, Content: c})
	}
	h.CompactTo("DIGEST-ONE", 1)
	if first := h.CompactSummary(); first != "DIGEST-ONE" {
		t.Fatalf("first digest wrong: %q", first)
	}
	h.Append(contract.Message{Role: contract.RoleUser, Content: "u3"})
	h.Append(contract.Message{Role: contract.RoleAssistant, Content: "a3"})
	h.CompactTo("DIGEST-TWO", 1)
	second := h.CompactSummary()
	if !strings.Contains(second, "DIGEST-ONE") {
		t.Fatalf("second compaction dropped the first digest (lossy re-summarization): %q", second)
	}
	if !strings.Contains(second, "DIGEST-TWO") {
		t.Fatalf("second digest not accumulated: %q", second)
	}
	if strings.Index(second, "DIGEST-ONE") > strings.Index(second, "DIGEST-TWO") {
		t.Fatalf("digests should accumulate in order (prior first): %q", second)
	}
}

// TestReclamationArchivesOriginals (T041) verifies reclamation archives the
// original of a tool result BEFORE shortening it, so it stays recoverable.
func TestReclamationArchivesOriginals(t *testing.T) {
	var archived []contract.PrunedRecord
	h := NewHistory(HistorySnapshot{Version: 1}, nil)
	h.SetPruneArchive(func(records []contract.PrunedRecord) error {
		archived = append(archived, records...)
		return nil
	})
	h.Append(contract.Message{Role: contract.RoleUser, Content: "task"})
	h.Append(contract.Message{Role: contract.RoleAssistant, ToolCalls: []contract.ToolCall{contract.NewToolCall("c1", "read_file", `{"path":"x"}`)}})
	original := strings.Repeat("payload ", 400) // ~3200 bytes, well over the 1024 floor
	h.Append(contract.Message{Role: contract.RoleTool, ToolCallID: "c1", Content: original})
	h.MarkTaskStart()

	if result := h.Maintain(0, 500, 1); !result.Changed {
		t.Fatalf("expected maintenance to reclaim something: %+v", result)
	}
	found := false
	for _, r := range archived {
		if r.ToolCallID == "c1" && r.ToolName == "read_file" && r.OriginalContent == original {
			found = true
		}
	}
	if !found {
		t.Fatalf("original tool result not archived verbatim before reclamation: %+v", archived)
	}
}

// TestReclamationGeometry (T040) verifies content-aware trim geometry: read-only
// results are front-loaded, side-effecting/unknown are balanced; results under
// the 1024-byte floor and error results are pinned from trimming.
func TestReclamationGeometry(t *testing.T) {
	if h, tl := snipHeadTail("read_file", 800); h <= tl {
		t.Fatalf("read-only geometry should be front-loaded: head=%d tail=%d", h, tl)
	}
	if h, tl := snipHeadTail("write_file", 800); h != tl {
		t.Fatalf("side-effecting geometry should be balanced: head=%d tail=%d", h, tl)
	}
	if h, tl := snipHeadTail("", 800); h != tl {
		t.Fatalf("unknown tool should default to balanced: head=%d tail=%d", h, tl)
	}

	msgs := []contract.Message{
		{Role: contract.RoleAssistant, ToolCalls: []contract.ToolCall{
			contract.NewToolCall("small", "read_file", `{}`),
			contract.NewToolCall("big", "read_file", `{}`),
			contract.NewToolCall("err", "write_file", `{}`),
		}},
		{Role: contract.RoleTool, ToolCallID: "small", Content: strings.Repeat("x", 900)},                                  // < 1024 floor
		{Role: contract.RoleTool, ToolCallID: "big", Content: strings.Repeat("y", 5000)},                                   // trimmable
		{Role: contract.RoleTool, ToolCallID: "err", Content: "Tool write_file failed: boom " + strings.Repeat("z", 5000)}, // error-pinned
	}
	n := trimAgedMessages(msgs, map[string]struct{}{}, 0, 500, 1)
	if n != 1 {
		t.Fatalf("only the big non-error result should be trimmed, got %d", n)
	}
	if !strings.Contains(msgs[2].Content, TrimNote) {
		t.Fatal("the big result should have been trimmed")
	}
	if strings.Contains(msgs[1].Content, TrimNote) {
		t.Fatal("the small result (<1024 bytes) must be pinned by the floor")
	}
	if strings.Contains(msgs[3].Content, TrimNote) {
		t.Fatal("the error result must be pinned from trimming")
	}
}

// TestSoftBandBelowFloorDoesNotMutate (T039) verifies the ladder's soft band:
// at pressure in [0.50, 0.60) — below the reclaim floor — a maintenance boundary
// performs ZERO mutation (no history rewrite, no invalidation event). The soft
// advisory notice is EmitStatus-only, so it cannot mutate by construction.
func TestSoftBandBelowFloorDoesNotMutate(t *testing.T) {
	promptTokens := 64_000 // 64000/116000 usable ≈ 0.55: soft band, below the 0.60 floor
	settings := engineSettings()
	history := NewHistory(HistorySnapshot{Version: 1}, nil)
	addCompletedToolPayload(history, "c1")
	history.MarkTaskStart()
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry(), History: history,
		InitialUsageRecords: []contract.UsageRecord{{Seq: 1, Stream: contract.UsageStreamMain, PromptTokens: &promptTokens}},
	})
	if err != nil {
		t.Fatal(err)
	}
	before := history.RewriteVersion()
	result, err := engine.runMaintenanceBoundary(context.Background(), Profile(contract.EffortMedium))
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || history.RewriteVersion() != before {
		t.Fatalf("soft band (below reclaim floor) must not mutate history: result=%+v", result)
	}
	if events := engine.InvalidationEvents(); len(events) != 0 {
		t.Fatalf("soft band recorded an invalidation event: %+v", events)
	}
}

// TestMaintenanceMutationAlwaysRecordsInvalidation (T009 / REV A1) locks in the
// invariant that closes the trap: whenever a maintenance boundary bumps the
// RewriteVersion (settled bytes changed), exactly one invalidation event is
// recorded in the same pass — mutation and its explanation are inseparable, so
// the next request's prefix-shape guard always finds the change explained.
func TestMaintenanceMutationAlwaysRecordsInvalidation(t *testing.T) {
	promptTokens := 100_000 // above hard-fold: the boundary always applies
	settings := engineSettings()
	history := NewHistory(HistorySnapshot{Version: 1}, nil)
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry(), History: history,
		InitialUsageRecords: []contract.UsageRecord{{Seq: 1, Stream: contract.UsageStreamMain, PromptTokens: &promptTokens}},
	})
	if err != nil {
		t.Fatal(err)
	}
	addCompletedToolPayload(history, "c1")
	history.MarkTaskStart()
	beforeVersion := history.RewriteVersion()
	beforeEvents := len(engine.InvalidationEvents())

	result, err := engine.runMaintenanceBoundary(context.Background(), Profile(contract.EffortMedium))
	if err != nil {
		t.Fatal(err)
	}
	mutated := history.RewriteVersion() != beforeVersion
	recorded := len(engine.InvalidationEvents()) - beforeEvents
	if result.Changed != mutated {
		t.Fatalf("result.Changed=%v disagrees with history mutation=%v", result.Changed, mutated)
	}
	if mutated && recorded != 1 {
		t.Fatalf("mutation without exactly one paired invalidation event: recorded=%d", recorded)
	}
	if !mutated && recorded != 0 {
		t.Fatalf("no mutation but %d invalidation event(s) recorded", recorded)
	}
}

func addCompletedToolPayload(history *History, id string) {
	history.Append(contract.Message{Role: contract.RoleUser, Content: "task"})
	history.Append(contract.Message{Role: contract.RoleAssistant, ToolCalls: []contract.ToolCall{contract.NewToolCall(id, "read_file", `{"path":"file"}`)}})
	history.Append(contract.Message{Role: contract.RoleTool, ToolCallID: id, Content: "file\n" + strings.Repeat("payload", 300)})
}
