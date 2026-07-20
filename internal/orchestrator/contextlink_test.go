package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/instructions"
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

// TestFollowUpRelatednessPredicate pins the cross-task relatedness bar. It is
// tested on "explore" because v1.1.0 deliberately exempts the EXECUTION chain
// from it (TestSessionChainContinuesGeneralAcrossTasks): general makes every
// change in the session, so the workspace is its shared subject. Research
// context stays topic-scoped and must still decline unrelated follow-ups.
func TestFollowUpRelatednessPredicate(t *testing.T) {
	e := linkTestEngine(t)
	writeWorkspaceFile(t, e, "billing.go", "package billing")
	bankLinkableRecord(t, e, "prev-task", "explore", 7, []string{"billing.go"}, nil) // lineage != current taskSeq (1)
	related := e.decideLink(subagentInput{Agent: "explore", Task: "now map the billing.go rounding"}, e.subagentSpecs()["explore"], e.subagentModelID())
	if related.Decision != linkContinued {
		t.Fatalf("related follow-up must link, got %+v", related)
	}
	unrelated := e.decideLink(subagentInput{Agent: "explore", Task: "tweak the css theme"}, e.subagentSpecs()["explore"], e.subagentModelID())
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

// TestSessionChainContinuesGeneralAcrossTasks is the owner's sub-agent cache
// directive (v1.1.0): when the user continues a session with a new prompt, the
// execution agent reuses the previous run's stream and context. Under the
// plan/execute split "general" makes every change in the session, so the
// workspace is the shared subject — cross-task continuation needs no topic
// overlap, and the session-frozen executor model keeps stream identity valid.
func TestSessionChainContinuesGeneralAcrossTasks(t *testing.T) {
	e := linkTestEngine(t)
	writeWorkspaceFile(t, e, "exporter.go", "package reporting")
	// A predecessor from an EARLIER task, about unrelated work.
	bankLinkableRecord(t, e, "impl1", "general", 1, []string{"exporter.go"}, []string{"exporter.go"})
	e.taskSeq = 2 // the user came back with a new prompt

	decision := e.decideLink(subagentInput{Agent: "general", Task: "rename the login button label"}, e.subagentSpecs()["general"], e.subagentModelID())
	if decision.Decision != linkContinued {
		t.Fatalf("execution chain did not continue across tasks: %+v", decision)
	}
	if decision.Reason != "session-chain" {
		t.Fatalf("cross-task execution continuation must be attributable in the ledger, got reason %q", decision.Reason)
	}
}

// TestSessionChainDoesNotWidenExploreOrReview: research context is topic
// specific. Carrying an unrelated investigation forward would pollute the
// reasoning rather than save tokens, so those kinds keep the relatedness bar.
func TestSessionChainDoesNotWidenExploreOrReview(t *testing.T) {
	for _, kind := range []string{"explore", "review"} {
		t.Run(kind, func(t *testing.T) {
			e := linkTestEngine(t)
			writeWorkspaceFile(t, e, "exporter.go", "package reporting")
			bankLinkableRecord(t, e, "prior1", kind, 1, []string{"exporter.go"}, nil)
			e.taskSeq = 2

			decision := e.decideLink(subagentInput{Agent: kind, Task: "rename the login button label"}, e.subagentSpecs()[kind], e.subagentModelID())
			if decision.Decision == linkContinued {
				t.Fatalf("%s continued into unrelated work: %+v", kind, decision)
			}
			if decision.Reason != "relatedness-miss" {
				t.Fatalf("expected the relatedness bar to decline, got %q", decision.Reason)
			}
		})
	}
}

// TestRoleNameIsDisplayOnly is the cache-safety half of LLM-named agents: the
// model's role name reaches the chip and the handoff, but the capability
// class — and with it the pin, the stable system message, and the record's
// Kind — stays fixed, so a novel name can never fragment the provider cache.
func TestRoleNameIsDisplayOnly(t *testing.T) {
	named := subagentInput{Agent: "general", Role: "auth-flow-mapper", Title: "auth work", Task: "wire the login handler"}
	if got := displayRole(named); got != "auth-flow-mapper" {
		t.Fatalf("chip identity = %q, want the model's role name", got)
	}
	handoff := handoffFor(named, "")
	if !strings.Contains(handoff.Role, "auth-flow-mapper") || !strings.Contains(handoff.Role, "implement-step") {
		t.Fatalf("handoff Role = %q, want the model's name AND the canonical role", handoff.Role)
	}
	// The report format is chosen by CLASS, never by the free-form name.
	if handoff.OutputFormat != instructions.ReportFormatImplementation {
		t.Fatalf("a role name changed the report format: %q", handoff.OutputFormat)
	}
	// Fallbacks: title, then the class — a bare "general" is the last resort.
	if got := displayRole(subagentInput{Agent: "general", Title: "auth work", Task: "x"}); got != "auth work" {
		t.Fatalf("unnamed dispatch chip = %q, want the title", got)
	}
	if got := displayRole(subagentInput{Agent: "general", Task: "x"}); got != "general" {
		t.Fatalf("last-resort chip = %q", got)
	}
}
