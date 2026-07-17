package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func resilienceEngine(t *testing.T) *Engine {
	t.Helper()
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "cr", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{}, Registry: NewRegistry(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func coldMiss(prompt int) contract.Usage {
	zero, miss := 0, prompt
	return contract.Usage{PromptTokens: prompt, PromptTokensAvailable: true, CacheReadTokens: &zero, CacheMissTokens: &miss}
}

// TestColdStartNotice covers C6: a resumed cold miss on a large prefix flags a
// notice; the threshold, the fresh-session exclusion, and a warm hit do not.
func TestColdStartNotice(t *testing.T) {
	engine := resilienceEngine(t)
	one := 5000
	prev := &contract.UsageRecord{PromptTokens: &one}

	// Below the token floor → no notice.
	engine.flagColdStartIfNeeded(prev, coldMiss(coldNoticeMinTokens-1))
	if _, _, ok := engine.takeColdStartNotice(); ok {
		t.Fatal("a sub-threshold cold miss must not notice")
	}
	// At the floor, with a prior request (a resume) and read==0 → notice + fallback cause.
	engine.flagColdStartIfNeeded(prev, coldMiss(coldNoticeMinTokens))
	tokens, cause, ok := engine.takeColdStartNotice()
	if !ok || tokens != coldNoticeMinTokens {
		t.Fatalf("threshold cold miss should notice: ok=%v tokens=%d", ok, tokens)
	}
	if !strings.Contains(cause, "provider cache expired") {
		t.Fatalf("fallback cause wrong: %q", cause)
	}
	// A fresh session (no prior main request) never notices, even on a huge cold read.
	engine.flagColdStartIfNeeded(nil, coldMiss(50000))
	if _, _, ok := engine.takeColdStartNotice(); ok {
		t.Fatal("a fresh session's first cold request must not notice")
	}
	// A warm hit (read>0) never notices.
	read := 40000
	warm := coldMiss(50000)
	warm.CacheReadTokens = &read
	engine.flagColdStartIfNeeded(prev, warm)
	if _, _, ok := engine.takeColdStartNotice(); ok {
		t.Fatal("a warm hit must not notice")
	}
}

// TestCheckResumeDriftAttributesChange covers C3/C4: a system/tools change on the
// first resumed request records the (previously dead) prompt-rebuild / toolset-change
// cause and sets a specific cause; the resume shape is consumed once.
func TestCheckResumeDriftAttributesChange(t *testing.T) {
	ctx := context.Background()
	engine := resilienceEngine(t)
	engine.priorSessionShape = &PrefixShape{SystemHash: "OLD", ToolsHash: "T", ModelID: "m"}
	engine.checkResumeDrift(ctx, PrefixShape{SystemHash: "NEW", ToolsHash: "TX", ModelID: "m"})

	events := engine.invalidations.Events()
	if !hasCause(events, contract.InvalidationPromptRebuild) {
		t.Fatalf("system change must record prompt-rebuild: %+v", events)
	}
	if !hasCause(events, contract.InvalidationToolsetChange) {
		t.Fatalf("tools change must record toolset-change: %+v", events)
	}
	engine.taskMu.Lock()
	cause := engine.lastResumeCause
	engine.taskMu.Unlock()
	if !strings.Contains(cause, "system prompt") || !strings.Contains(cause, "tool set") {
		t.Fatalf("cause should name both changed regions: %q", cause)
	}
	// Consumed once: a second call is a no-op.
	if engine.priorSessionShape != nil {
		t.Fatal("priorSessionShape must be consumed after the first check")
	}
}

// TestCheckResumeDriftIdenticalIsServerSide covers C3: a byte-identical resume
// records NO invalidation and attributes the cold start to the provider (server-side),
// matching the incident's forensic verdict.
func TestCheckResumeDriftIdenticalIsServerSide(t *testing.T) {
	engine := resilienceEngine(t)
	engine.priorSessionShape = &PrefixShape{SystemHash: "S", ToolsHash: "T", ModelID: "m"}
	engine.checkResumeDrift(context.Background(), PrefixShape{SystemHash: "S", ToolsHash: "T", ModelID: "m"})
	if n := len(engine.invalidations.Events()); n != 0 {
		t.Fatalf("an identical resume must record no invalidation, got %d", n)
	}
	engine.taskMu.Lock()
	cause := engine.lastResumeCause
	engine.taskMu.Unlock()
	if !strings.Contains(cause, "provider cache expired") {
		t.Fatalf("identical resume cause should be server-side: %q", cause)
	}
}

// TestStaleResumePruneGating covers C7: a fresh or recent session never prunes, and
// the prune runs at most once per engine.
func TestStaleResumePruneGating(t *testing.T) {
	ctx := context.Background()
	profile := Profile(contract.EffortMedium)

	// Fresh session (no usage records) → no prune.
	fresh := resilienceEngine(t)
	if err := fresh.staleResumePruneIfNeeded(ctx, profile); err != nil {
		t.Fatal(err)
	}
	if n := len(fresh.invalidations.Events()); n != 0 {
		t.Fatalf("a fresh session must not prune, got %d events", n)
	}
	if !fresh.resumePruneDone {
		t.Fatal("the prune check must mark itself done (once per engine)")
	}

	// A recent (not stale) large session → no prune.
	recent := resilienceEngine(t)
	recent.usageRecords = []contract.UsageRecord{{At: time.Now().Add(-1 * time.Minute), Stream: contract.UsageStreamMain}}
	if err := recent.staleResumePruneIfNeeded(ctx, profile); err != nil {
		t.Fatal(err)
	}
	if n := len(recent.invalidations.Events()); n != 0 {
		t.Fatalf("a recently-active session must not prune, got %d events", n)
	}
}

// TestStaleResumeThresholds pins the C7 gate constants so a future edit is deliberate.
func TestStaleResumeThresholds(t *testing.T) {
	if staleResumeAfter != 6*time.Hour {
		t.Errorf("staleResumeAfter drifted: %v", staleResumeAfter)
	}
	if staleResumeMinTokens != 20_000 || coldNoticeMinTokens != 8_000 {
		t.Errorf("token thresholds drifted: stale=%d cold=%d", staleResumeMinTokens, coldNoticeMinTokens)
	}
}

func hasCause(events []contract.InvalidationEvent, cause contract.InvalidationCause) bool {
	for _, e := range events {
		if e.Cause == cause {
			return true
		}
	}
	return false
}
