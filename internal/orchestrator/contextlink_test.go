package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// linkTestEngine builds an engine whose subagent model resolves to the
// DeepSeek family (ContinuationLinking supported) with a known window.
func linkTestEngine(t *testing.T) *Engine {
	t.Helper()
	settings := engineSettings()
	settings.Provider.SubagentModelID = "deepseek-v4-flash"
	settings.Provider.Models = append(settings.Provider.Models, contract.Model{ID: "deepseek-v4-flash", Name: "DeepSeek Flash", ContextLimit: 128000})
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "link", WorkspacePath: t.TempDir()}, Provider: &scriptedProvider{}, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	engine.taskSeq = 1
	return engine
}

// bankLinkableRecord inserts a clean-done record for the engine's subagent
// model on the given kind and lineage, with touched fingerprints taken from
// the CURRENT state of the named workspace files (i.e., end-of-run capture).
func bankLinkableRecord(t *testing.T, e *Engine, runID, kind string, lineage int, reads []string, writes []string) *SubagentContextRecord {
	t.Helper()
	stat := func(path string) FileFingerprint { return statFingerprint(e.session.WorkspacePath, path) }
	record := &SubagentContextRecord{
		RunID: runID, Kind: kind, ModelID: e.subagentModelID(),
		Pin: e.session.ID + ":sub:" + kind,
		Transcript: []contract.Message{
			{Role: contract.RoleSystem, Content: "sys"},
			{Role: contract.RoleUser, Content: "task"},
			{Role: contract.RoleAssistant, Content: "report"},
		},
		TerminalShape: terminalCleanDone, Linkable: true,
		FinalPromptTokens: 5000, Result: "did the work in " + strings.Join(append(append([]string{}, reads...), writes...), ", "),
		TaskLineage: lineage, CompletedAt: time.Now().UnixMilli(),
	}
	record.Touched.Reads = map[string]FileFingerprint{}
	for _, path := range reads {
		record.Touched.Reads[path] = stat(path)
	}
	if len(writes) > 0 {
		record.Touched.Writes = map[string]FileFingerprint{}
		for _, path := range writes {
			record.Touched.Writes[path] = stat(path)
		}
	}
	e.taskMu.Lock()
	if e.agentRecords == nil {
		e.agentRecords = map[string]*SubagentContextRecord{}
	}
	e.agentRecords[runID] = record
	e.agentRecordOrder = append(e.agentRecordOrder, runID)
	e.taskMu.Unlock()
	return record
}

func writeWorkspaceFile(t *testing.T, e *Engine, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(e.session.WorkspacePath, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDecideLinkDisabled(t *testing.T) {
	e := linkTestEngine(t)
	e.settings.ContextLinking = "off"
	bankLinkableRecord(t, e, "r1", "general", 1, nil, nil)
	decision := e.decideLink(subagentInput{Agent: "general", Task: "continue"}, e.subagentSpecs()["general"], e.subagentModelID())
	if decision.Decision != linkFresh || decision.Reason != "disabled" {
		t.Fatalf("off switch must yield fresh/disabled, got %+v", decision)
	}
}

func TestDecideLinkNoCandidate(t *testing.T) {
	e := linkTestEngine(t)
	decision := e.decideLink(subagentInput{Agent: "general", Task: "anything"}, e.subagentSpecs()["general"], e.subagentModelID())
	if decision.Decision != linkFresh || decision.Reason != "no-candidate" {
		t.Fatalf("want fresh/no-candidate, got %+v", decision)
	}
}

func TestDecideLinkContinuedSameKindAndReviewChain(t *testing.T) {
	e := linkTestEngine(t)
	writeWorkspaceFile(t, e, "a.go", "package a")
	bankLinkableRecord(t, e, "impl1", "general", 1, []string{"a.go"}, []string{"a.go"})
	same := e.decideLink(subagentInput{Agent: "general", Task: "phase 2"}, e.subagentSpecs()["general"], e.subagentModelID())
	if same.Decision != linkContinued || same.Form != linkFormSameKind || same.Reason != "eligible" {
		t.Fatalf("same-kind continuation expected, got %+v", same)
	}
	review := e.decideLink(subagentInput{Agent: "review", Task: "review the change"}, e.subagentSpecs()["review"], e.subagentModelID())
	if review.Decision != linkContinued || review.Form != linkFormReviewChain {
		t.Fatalf("review-after-implement continuation expected, got %+v", review)
	}
	explore := e.decideLink(subagentInput{Agent: "explore", Task: "explore"}, e.subagentSpecs()["explore"], e.subagentModelID())
	if explore.Decision != linkDigestSeeded || explore.Reason != "kind-pair-unsupported" {
		t.Fatalf("explore after general must digest-seed, got %+v", explore)
	}
}

func TestDecideLinkModelChangedAndWindowOverflow(t *testing.T) {
	e := linkTestEngine(t)
	record := bankLinkableRecord(t, e, "impl1", "general", 1, nil, nil)
	record.ModelID = "some-other-model"
	decision := e.decideLink(subagentInput{Agent: "general", Task: "next"}, e.subagentSpecs()["general"], e.subagentModelID())
	if decision.Decision != linkDigestSeeded || decision.Reason != "model-changed" {
		t.Fatalf("want digest/model-changed, got %+v", decision)
	}
	record.ModelID = e.subagentModelID()
	record.FinalPromptTokens = 120_000 // 128k window * 0.8 headroom = 102.4k
	decision = e.decideLink(subagentInput{Agent: "general", Task: "next"}, e.subagentSpecs()["general"], e.subagentModelID())
	if decision.Decision != linkDigestSeeded || decision.Reason != "window-overflow" {
		t.Fatalf("want digest/window-overflow, got %+v", decision)
	}
}

// TestSelfEditChainNotStale is the R-D5 regression: a predecessor that edited
// files it read is NOT stale for its successor, because fingerprints are
// captured at run end (after its edits).
func TestSelfEditChainNotStale(t *testing.T) {
	e := linkTestEngine(t)
	for _, name := range []string{"f1.go", "f2.go", "f3.go", "f4.go"} {
		writeWorkspaceFile(t, e, name, "edited content "+name)
	}
	// End-of-run capture: fingerprints reflect the post-edit state.
	bankLinkableRecord(t, e, "impl1", "general", 1, []string{"f1.go", "f2.go", "f3.go", "f4.go"}, []string{"f1.go", "f2.go", "f3.go", "f4.go"})
	decision := e.decideLink(subagentInput{Agent: "general", Task: "phase 2"}, e.subagentSpecs()["general"], e.subagentModelID())
	if decision.Decision != linkContinued {
		t.Fatalf("self-edit chain must continue (changed-fraction 0), got %+v", decision)
	}
	if len(decision.Reread) != 0 || decision.Inherited != 4 {
		t.Fatalf("want 4 inherited, 0 re-reads, got inherited=%d reread=%v", decision.Inherited, decision.Reread)
	}
}

// TestExternalEditMajorityDeclines: >50% of the touched set changed after the
// run ended → decline continuation with the stale:NN% reason (Clarification Q5).
func TestExternalEditMajorityDeclines(t *testing.T) {
	e := linkTestEngine(t)
	names := []string{"g1.go", "g2.go", "g3.go", "g4.go", "g5.go"}
	for _, name := range names {
		writeWorkspaceFile(t, e, name, "original "+name)
	}
	bankLinkableRecord(t, e, "impl1", "general", 1, names, nil)
	// External edits to 3 of 5 (60%) AFTER the run ended.
	time.Sleep(20 * time.Millisecond) // ensure mtime moves even on coarse filesystems
	for _, name := range names[:3] {
		writeWorkspaceFile(t, e, name, "changed after the run "+name)
	}
	decision := e.decideLink(subagentInput{Agent: "general", Task: "phase 2"}, e.subagentSpecs()["general"], e.subagentModelID())
	if decision.Decision != linkDigestSeeded || !strings.HasPrefix(decision.Reason, "stale:") {
		t.Fatalf("want digest-seeded stale:NN%%, got %+v", decision)
	}
	if decision.CarryForward == "" || !strings.Contains(decision.CarryForward, "Predecessor general result") {
		t.Fatalf("digest fallback must carry the predecessor digest, got %q", decision.CarryForward)
	}
}

// TestMinorityStalenessRereads: ≤50% changed → continue with re-read directives.
func TestMinorityStalenessRereads(t *testing.T) {
	e := linkTestEngine(t)
	names := []string{"h1.go", "h2.go", "h3.go", "h4.go", "h5.go"}
	for _, name := range names {
		writeWorkspaceFile(t, e, name, "original "+name)
	}
	bankLinkableRecord(t, e, "impl1", "general", 1, names, nil)
	time.Sleep(20 * time.Millisecond)
	writeWorkspaceFile(t, e, "h1.go", "changed after the run — longer content")
	decision := e.decideLink(subagentInput{Agent: "general", Task: "phase 2"}, e.subagentSpecs()["general"], e.subagentModelID())
	if decision.Decision != linkContinued {
		t.Fatalf("minority staleness must continue, got %+v", decision)
	}
	if len(decision.Reread) != 1 || decision.Reread[0] != "h1.go" {
		t.Fatalf("want re-read directive for h1.go, got %v", decision.Reread)
	}
}

func TestFollowUpRelatednessPredicate(t *testing.T) {
	e := linkTestEngine(t)
	writeWorkspaceFile(t, e, "billing.go", "package billing")
	bankLinkableRecord(t, e, "prev-task", "general", 7, []string{"billing.go"}, nil) // lineage != current taskSeq (1)
	related := e.decideLink(subagentInput{Agent: "general", Task: "now fix the billing.go rounding"}, e.subagentSpecs()["general"], e.subagentModelID())
	if related.Decision != linkContinued {
		t.Fatalf("related follow-up must link, got %+v", related)
	}
	unrelated := e.decideLink(subagentInput{Agent: "general", Task: "tweak the css theme"}, e.subagentSpecs()["general"], e.subagentModelID())
	if unrelated.Decision != linkFresh || unrelated.Reason != "relatedness-miss" {
		t.Fatalf("unrelated follow-up must be fresh/relatedness-miss, got %+v", unrelated)
	}
}

func TestDecideLinkNonLinkableShapesFallBack(t *testing.T) {
	e := linkTestEngine(t)
	record := bankLinkableRecord(t, e, "impl1", "general", 1, nil, nil)
	record.Linkable = false // e.g. wrapup-done / failed / incomplete pairings
	decision := e.decideLink(subagentInput{Agent: "general", Task: "next"}, e.subagentSpecs()["general"], e.subagentModelID())
	if decision.Decision != linkFresh || decision.Reason != "no-candidate" {
		t.Fatalf("non-linkable records are never candidates, got %+v", decision)
	}
}

func TestTerminalShapeFor(t *testing.T) {
	cases := []struct {
		status          string
		wrapUp, ceiling bool
		want            string
	}{
		{"done", false, false, terminalCleanDone},
		{"done", true, false, terminalWrapupDone},
		{"partial (token ceiling)", true, true, terminalPartialCeiling},
		{"failed", true, false, terminalFailed},
		{"cancelled", false, false, terminalCancelled},
	}
	for _, tc := range cases {
		if got := terminalShapeFor(tc.status, tc.wrapUp, tc.ceiling); got != tc.want {
			t.Fatalf("terminalShapeFor(%q,%v,%v) = %q, want %q", tc.status, tc.wrapUp, tc.ceiling, got, tc.want)
		}
	}
}

func TestTranscriptPairingsComplete(t *testing.T) {
	complete := []contract.Message{
		{Role: contract.RoleSystem, Content: "s"},
		{Role: contract.RoleAssistant, ToolCalls: []contract.ToolCall{contract.NewToolCall("c1", "read_file", `{}`)}},
		{Role: contract.RoleTool, ToolCallID: "c1", Content: "result"},
	}
	if !transcriptPairingsComplete(complete) {
		t.Fatal("complete pairing judged incomplete")
	}
	missing := complete[:2]
	if transcriptPairingsComplete(missing) {
		t.Fatal("missing tool result judged complete — replay would inject repair bytes (R-F8)")
	}
}

func TestPairedCacheShare(t *testing.T) {
	read, miss := 800, 200
	usage := contract.Usage{CacheReadTokens: &read, CacheMissTokens: &miss}
	if share := pairedCacheShare(usage); share == nil || *share != 0.8 {
		t.Fatalf("want 0.8, got %v", share)
	}
	if share := pairedCacheShare(contract.Usage{CacheReadTokens: &read}); share != nil {
		t.Fatal("one-sided usage must yield nil (unavailable), never an estimate")
	}
}

func TestReadPlanTool(t *testing.T) {
	e := linkTestEngine(t)
	tool := readPlanTool{engine: e}
	if out, err := tool.Execute(context.Background(), []byte(`{}`)); err != nil || !strings.Contains(out, "No plan exists yet") {
		t.Fatalf("empty plan: %q err=%v", out, err)
	}
	e.plan = contract.Plan{Steps: []contract.PlanStep{{Title: "step one", Status: contract.PlanPending}}, Note: "Verification: run go test"}
	full, err := tool.Execute(context.Background(), []byte(`{}`))
	if err != nil || !strings.Contains(full, "step one") {
		t.Fatalf("full plan render missing step: %q err=%v", full, err)
	}
	steps, err := tool.Execute(context.Background(), []byte(`{"section":"steps"}`))
	if err != nil || !strings.Contains(steps, "- [ ] step one") || strings.Contains(steps, "Verification") {
		t.Fatalf("steps section wrong: %q err=%v", steps, err)
	}
}

// --- role gate (contracts/role-gate.md RG-6 matrix) ---

func roleGateEngine(t *testing.T) *Engine {
	e := linkTestEngine(t)
	e.lifecycle = Lifecycle{State: contract.LifecycleImplementing, Depth: PipelineDepthFull, ResearchCompleted: true, PlanWritten: true, Approved: true}
	return e
}

func gateCall(name, args string) contract.ToolCall { return contract.NewToolCall("g1", name, args) }

func TestPhaseReadGateDeniesTwiceThenWaives(t *testing.T) {
	e := roleGateEngine(t)
	sc := dispatchScope{counters: newCallCounters(), trackStats: true}
	call := gateCall("read_file", `{"path":"impl.go"}`)
	for attempt := 1; attempt <= 2; attempt++ {
		outcome, blocked := e.phaseReadGate(context.Background(), sc, call, "read_file")
		if !blocked || !outcome.Failed || !strings.Contains(outcome.Output, "belongs to the implementation subagents") {
			t.Fatalf("attempt %d must deny with guidance, got blocked=%v %+v", attempt, blocked, outcome)
		}
	}
	if _, blocked := e.phaseReadGate(context.Background(), sc, call, "read_file"); blocked {
		t.Fatal("third attempt must proceed with a recorded waiver (RG-3) — a gate can never loop")
	}
	var stats contract.TaskStats
	e.finalizeLinkStats(&stats)
	if stats.ReadGate.Denied != 2 || stats.ReadGate.Waived != 1 {
		t.Fatalf("stats want denied=2 waived=1, got %+v", stats.ReadGate)
	}
}

func TestPhaseReadGateShellReadDeniedGrepPasses(t *testing.T) {
	e := roleGateEngine(t)
	sc := dispatchScope{counters: newCallCounters(), trackStats: true}
	if _, blocked := e.phaseReadGate(context.Background(), sc, gateCall("run_shell", `{"command":"cat impl.go"}`), "run_shell"); !blocked {
		t.Fatal("shell cat bypass must be denied (RG-2)")
	}
	if _, blocked := e.phaseReadGate(context.Background(), sc, gateCall("run_shell", `{"command":"go test ./..."}`), "run_shell"); blocked {
		t.Fatal("non-read shell commands must pass")
	}
	if _, blocked := e.phaseReadGate(context.Background(), sc, gateCall("grep", `{"pattern":"x"}`), "grep"); blocked {
		t.Fatal("discovery tools must never be gated")
	}
}

func TestPhaseReadGateExemptions(t *testing.T) {
	// Light depth: never gated.
	e := roleGateEngine(t)
	e.lifecycle.Depth = "light"
	sc := dispatchScope{counters: newCallCounters(), trackStats: true}
	if _, blocked := e.phaseReadGate(context.Background(), sc, gateCall("read_file", `{"path":"a"}`), "read_file"); blocked {
		t.Fatal("light depth must never gate")
	}
	// Degraded phase: exempt.
	e = roleGateEngine(t)
	e.lifecycle.Degradations = []LifecycleDegradation{{State: contract.LifecycleImplementing, Reason: "x"}}
	if _, blocked := e.phaseReadGate(context.Background(), sc, gateCall("read_file", `{"path":"a"}`), "read_file"); blocked {
		t.Fatal("degraded phases must be exempt (advance-notice texts promise reads)")
	}
	// Post-failure diagnosis: exempt after a failed dispatch.
	e = roleGateEngine(t)
	e.markImplementFailureDiagnosis(context.Background())
	if _, blocked := e.phaseReadGate(context.Background(), sc, gateCall("read_file", `{"path":"a"}`), "read_file"); blocked {
		t.Fatal("post-failure diagnosis must be exempt (RG-4)")
	}
	// Subagent scopes: never gated.
	e = roleGateEngine(t)
	if _, blocked := e.phaseReadGate(context.Background(), dispatchScope{counters: newCallCounters()}, gateCall("read_file", `{"path":"a"}`), "read_file"); blocked {
		t.Fatal("subagent scopes must never be gated")
	}
	// Kill switch: gate disabled with linking off.
	e = roleGateEngine(t)
	e.settings.ContextLinking = "off"
	if _, blocked := e.phaseReadGate(context.Background(), sc, gateCall("read_file", `{"path":"a"}`), "read_file"); blocked {
		t.Fatal("contextLinking=off must disable the gate (FR-017)")
	}
}

func TestParseReportTrailer(t *testing.T) {
	report := "Changes made: stuff.\nChanged: a.go, b/c.go , none\nVerified: go test ./... green\nCarryForward: the loader now caches\ntrailing prose"
	changed, verified, carry := parseReportTrailer(report)
	if len(changed) != 2 || changed[0] != "a.go" || changed[1] != "b/c.go" {
		t.Fatalf("changed wrong: %v", changed)
	}
	if verified != "go test ./... green" || carry != "the loader now caches" {
		t.Fatalf("verified/carry wrong: %q %q", verified, carry)
	}
	if c, v, cf := parseReportTrailer("plain prose report"); len(c) != 0 || v != "" || cf != "" {
		t.Fatal("absent trailer must degrade to nothing, never error")
	}
}

// TestResearchAutoTransitionsWithoutDispatch pins feature 013: entering an
// orchestrated pipeline NEVER fans out harness subagents — research passes
// straight into planning with the investigate-then-plan instruction, and the
// provider sees zero requests.
func TestResearchAutoTransitionsWithoutDispatch(t *testing.T) {
	provider := &scriptedProvider{}
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "brief", WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	engine.lifecycle = Lifecycle{State: contract.LifecycleResearch, Depth: PipelineDepthFull}
	prelude, err := engine.preparePipelinePhase(context.Background(), "extend the calc package with a Multiply function", Budget{MaxAgentRuns: 2}, Profile(contract.EffortMedium))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prelude, "[pipeline planning]") {
		t.Fatalf("research must hand the model the planning instruction, got %q", prelude)
	}
	if engine.LifecycleState() != contract.LifecyclePlanning {
		t.Fatalf("research must transition to planning, got %s", engine.LifecycleState())
	}
	if len(provider.requests) != 0 {
		t.Fatalf("the harness must not launch subagents on its own — %d provider requests fired", len(provider.requests))
	}
}

func TestLinkNoticeLine(t *testing.T) {
	share := 0.85
	continued := contract.LinkOutcome{Decision: linkContinued, Form: linkFormSameKind, CacheShare: &share}
	if line := linkNoticeLine(continued); !strings.Contains(line, "85% reused") {
		t.Fatalf("continued notice wrong: %q", line)
	}
	unavailable := contract.LinkOutcome{Decision: linkContinued, Form: linkFormSameKind}
	if line := linkNoticeLine(unavailable); !strings.Contains(line, "unavailable") {
		t.Fatalf("unavailable must render honestly: %q", line)
	}
	digest := contract.LinkOutcome{Decision: linkDigestSeeded, Reason: "stale:60%"}
	if line := linkNoticeLine(digest); !strings.Contains(line, "digest-seeded") || strings.Contains(line, "continued") {
		t.Fatalf("digest must never render as linked: %q", line)
	}
	if line := linkNoticeLine(contract.LinkOutcome{Decision: linkFresh}); line != "" {
		t.Fatalf("fresh dispatches emit no link notice: %q", line)
	}
}
