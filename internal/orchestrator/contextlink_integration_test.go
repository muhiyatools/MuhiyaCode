package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// Integration coverage for the continuation dispatch path (quickstart S1/S3):
// real executeSubagent runs against the scripted provider, asserting replay
// shape on the wire, record banking, verification, and the capability mask.

func integrationEngine(t *testing.T, provider *scriptedProvider) *Engine {
	t.Helper()
	settings := engineSettings()
	settings.Provider.SubagentModelID = "deepseek-v4-flash"
	settings.Provider.Models = append(settings.Provider.Models, contract.Model{ID: "deepseek-v4-flash", Name: "DeepSeek Flash", ContextLimit: 128000})
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "cont", WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	engine.taskSeq = 1
	engine.taskAgentCap = 8
	return engine
}

// TestSubagentSystemMessageStableAcrossDispatches pins PH-1/PH-6: two fresh
// dispatches of one kind put byte-identical system messages and tool arrays
// on the wire — the per-run handoff rides the first USER message.
func TestSubagentSystemMessageStableAcrossDispatches(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{Content: "report one — findings for the first scope with plenty of detail to bank."},
		{Content: "report two — findings for the second scope with plenty of detail to bank."},
	}}
	e := integrationEngine(t, provider)
	e.settings.ContextLinking = "off" // isolate the relocation from linking
	for _, task := range []string{"map the config loader in internal/state", "map the gateway provider in internal/gateway"} {
		result := e.executeSubagent(context.Background(), "r-"+task[:5], subagentInput{Agent: "explore", Task: task}, e.subagentSpecs()["explore"])
		if result.Status != "done" {
			t.Fatalf("dispatch failed: %+v", result)
		}
	}
	if len(provider.requests) != 2 {
		t.Fatalf("want 2 requests, got %d", len(provider.requests))
	}
	first, second := provider.requests[0], provider.requests[1]
	if first.Messages[0].Content != second.Messages[0].Content {
		t.Fatalf("system messages differ across dispatches — per-run content leaked into the stable prefix:\n%q\n%q", first.Messages[0].Content, second.Messages[0].Content)
	}
	if strings.Contains(first.Messages[0].Content, "HANDOFF CONTRACT") {
		t.Fatal("handoff must not live in the system message (PH-1)")
	}
	if !strings.Contains(first.Messages[1].Content, "HANDOFF CONTRACT") || !strings.Contains(first.Messages[1].Content, "TASK:") {
		t.Fatalf("handoff+task must ride the first user message, got %q", first.Messages[1].Content)
	}
	if len(first.Tools) != len(second.Tools) {
		t.Fatalf("tool arrays differ across dispatches: %d vs %d", len(first.Tools), len(second.Tools))
	}
}

// TestContinuationReplaysPredecessorVerbatim pins CL-2: the successor's first
// request replays the predecessor's transcript byte-for-byte and appends
// exactly one user message; pin and record bookkeeping follow.
func TestContinuationReplaysPredecessorVerbatim(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{Content: "phase one complete — implemented the loader changes as assigned in detail."},
		{Content: "phase two complete — continued on top of the inherited context in detail."},
	}}
	e := integrationEngine(t, provider)
	first := e.executeSubagent(context.Background(), "run1", subagentInput{Agent: "general", Task: "implement phase 1 of the loader"}, e.subagentSpecs()["general"])
	if first.Status != "done" || first.TerminalShape != terminalCleanDone {
		t.Fatalf("first run: %+v", first)
	}
	record := e.agentRecords["run1"]
	if record == nil || !record.Linkable {
		t.Fatalf("first run must bank a linkable record, got %+v", record)
	}
	second := e.executeSubagent(context.Background(), "run2", subagentInput{Agent: "general", Task: "implement phase 2 of the loader"}, e.subagentSpecs()["general"])
	if second.Status != "done" {
		t.Fatalf("second run: %+v", second)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("want 2 requests, got %d", len(provider.requests))
	}
	prefixLen := len(record.Transcript)
	continuation := provider.requests[1]
	if len(continuation.Messages) != prefixLen+1 {
		t.Fatalf("continuation must be predecessor transcript + 1 user message: got %d, prefix %d", len(continuation.Messages), prefixLen)
	}
	for index := 0; index < prefixLen; index++ {
		if continuation.Messages[index].Content != record.Transcript[index].Content || continuation.Messages[index].Role != record.Transcript[index].Role {
			t.Fatalf("replayed message %d drifted from the stored transcript", index)
		}
	}
	appended := continuation.Messages[prefixLen]
	if appended.Role != contract.RoleUser || !strings.Contains(appended.Content, "CONTINUATION") || !strings.Contains(appended.Content, "phase 2") {
		t.Fatalf("appended continuation message wrong: %+v", appended)
	}
	if continuation.SessionID != e.session.ID+":sub:general" {
		t.Fatalf("continuation must reuse the predecessor's pin, got %q", continuation.SessionID)
	}
	e.taskMu.Lock()
	links := append([]contract.LinkOutcome(nil), e.taskLinks...)
	e.taskMu.Unlock()
	if len(links) != 2 || links[1].Decision != linkContinued || links[1].Predecessor != "run1" {
		t.Fatalf("link ledger wrong: %+v", links)
	}
	// The successor's record represents the whole stream for ITS successor.
	if e.agentRecords["run2"] == nil || len(e.agentRecords["run2"].Transcript) <= prefixLen {
		t.Fatal("successor record must contain the extended stream")
	}
}

// TestReviewContinuationMasksMutations pins R-D2: a review continuation keeps
// the implementer's wire tool array but the harness refuses mutating tools.
func TestReviewContinuationMasksMutations(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{Content: "implementation done — files edited and verified as assigned, in detail."},
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("m1", "write_file", `{"path":"x.go","content":"nope"}`)}},
		{Content: "Verified findings: none. Checks performed: read the diff. VERDICT: PASS. Remaining concerns: none."},
	}}
	e := integrationEngine(t, provider)
	writeTool := &recordingTool{name: "write_file"}
	e.registry.Add(writeTool)
	first := e.executeSubagent(context.Background(), "impl", subagentInput{Agent: "general", Task: "implement the fix in x.go"}, e.subagentSpecs()["general"])
	if first.TerminalShape != terminalCleanDone {
		t.Fatalf("implementer: %+v", first)
	}
	review := e.executeSubagent(context.Background(), "rev", subagentInput{Agent: "review", Task: "review the fix in x.go"}, e.subagentSpecs()["review"])
	if review.Status != "done" {
		t.Fatalf("review continuation: %+v", review)
	}
	if writeTool.calls != 0 {
		t.Fatal("mutation must be masked in a review continuation — write_file executed")
	}
	e.taskMu.Lock()
	links := append([]contract.LinkOutcome(nil), e.taskLinks...)
	e.taskMu.Unlock()
	if links[len(links)-1].Form != linkFormReviewChain {
		t.Fatalf("want review-after-implement link, got %+v", links[len(links)-1])
	}
	// The refusal text reached the model as the tool result.
	refused := false
	for _, request := range provider.requests {
		for _, message := range request.Messages {
			if message.Role == contract.RoleTool && strings.Contains(message.Content, "review-only") {
				refused = true
			}
		}
	}
	if !refused {
		t.Fatal("mutation refusal must be delivered as the tool result")
	}
}

// TestLinkingOffRestoresLegacyDispatch pins FR-017 (quickstart S7): with
// contextLinking=off, no records bank, no link ledger accrues, and every
// dispatch is a fresh two-message conversation.
func TestLinkingOffRestoresLegacyDispatch(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{Content: "report one with enough length to be a bankable knowledge entry here."},
		{Content: "report two with enough length to be a bankable knowledge entry here."},
	}}
	e := integrationEngine(t, provider)
	e.settings.ContextLinking = "off"
	e.executeSubagent(context.Background(), "off1", subagentInput{Agent: "general", Task: "implement part one"}, e.subagentSpecs()["general"])
	e.executeSubagent(context.Background(), "off2", subagentInput{Agent: "general", Task: "implement part two"}, e.subagentSpecs()["general"])
	e.taskMu.Lock()
	records, links := len(e.agentRecords), len(e.taskLinks)
	e.taskMu.Unlock()
	if records != 0 || links != 0 {
		t.Fatalf("off must bank nothing: records=%d links=%d", records, links)
	}
	if len(provider.requests[1].Messages) != 2 {
		t.Fatalf("off must dispatch fresh two-message conversations, got %d messages", len(provider.requests[1].Messages))
	}
}

// TestContinuationVerificationStampsCacheShare pins CL-4: provider-reported
// cache fields on the continuation's first response become the verified share;
// their absence renders unavailable, never an estimate.
func TestContinuationVerificationStampsCacheShare(t *testing.T) {
	read, miss := 900, 100
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{Content: "phase one complete — detailed enough to bank as a linkable record here."},
		{Content: "phase two complete — detailed enough to bank as a linkable record here.", Usage: contract.Usage{PromptTokens: 1000, CacheReadTokens: &read, CacheMissTokens: &miss}},
	}}
	e := integrationEngine(t, provider)
	e.executeSubagent(context.Background(), "p1", subagentInput{Agent: "general", Task: "implement phase 1"}, e.subagentSpecs()["general"])
	e.executeSubagent(context.Background(), "p2", subagentInput{Agent: "general", Task: "implement phase 2"}, e.subagentSpecs()["general"])
	e.taskMu.Lock()
	links := append([]contract.LinkOutcome(nil), e.taskLinks...)
	e.taskMu.Unlock()
	verified := links[1]
	if verified.CacheShare == nil || *verified.CacheShare != 0.9 || !verified.CacheReported {
		t.Fatalf("want verified 90%% share, got %+v", verified)
	}
	if links[0].CacheShare != nil || links[0].CacheReported {
		t.Fatalf("no cache fields → unavailable, got %+v", links[0])
	}
}
