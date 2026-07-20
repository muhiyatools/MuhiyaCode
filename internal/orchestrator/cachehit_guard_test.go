package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// nonNormalizingProvider deliberately does NOT implement wireRequestNormalizer
// (no StableRequestMessages), forcing the engine onto the C2 degraded-guard
// path so we can assert it degrades LOUDLY (records a marker, run still
// succeeds), never silently hashing pre-replay bytes.
type nonNormalizingProvider struct {
	responses []contract.ChatResponse
	n         int
}

func (p *nonNormalizingProvider) Chat(context.Context, contract.ChatRequest) (contract.ChatResponse, error) {
	if p.n < len(p.responses) {
		r := p.responses[p.n]
		p.n++
		return r, nil
	}
	return contract.ChatResponse{Content: "done"}, nil
}

func (p *nonNormalizingProvider) ListModels(context.Context) ([]contract.Model, error) {
	return nil, nil
}

// TestCacheHitGuardDegradesLoudlyWithoutNormalizer (T010 / REV A3.1 / C2): a
// provider that cannot expose its stable wire bytes must make the prefix-shape
// guard degrade visibly — exactly once per session — and the run must still
// succeed rather than silently hashing the wrong bytes.
func TestCacheHitGuardDegradesLoudlyWithoutNormalizer(t *testing.T) {
	// Reset the process-wide once-latch so this test observes the first emission
	// regardless of what other tests in the binary already triggered.
	degradedPrefixGuardLogged.Lock()
	degradedPrefixGuardLogged.seen = map[string]bool{}
	degradedPrefixGuardLogged.Unlock()

	var buf bytes.Buffer
	degradedGuardWriter = func() io.Writer { return &buf }
	defer func() { degradedGuardWriter = nil }()

	provider := &nonNormalizingProvider{responses: []contract.ChatResponse{
		{Content: "one", Usage: guardUsage(0, 100)},
		{Content: "two", Usage: guardUsage(95, 5)},
	}}
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "degraded", WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry(), Prompt: PromptContext{Model: "Test"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, prompt := range []string{"hi", "again"} {
		if _, _, err := engine.Run(context.Background(), prompt); err != nil {
			t.Fatalf("run must still succeed on the degraded path: %v", err)
		}
	}
	out := buf.String()
	if !strings.Contains(out, "prefix-guard degraded") {
		t.Fatalf("degraded guard did not emit a loud marker; got %q", out)
	}
	if got := strings.Count(out, "prefix-guard degraded"); got != 1 {
		t.Fatalf("degraded marker should be logged once per session, got %d:\n%s", got, out)
	}
}

// TestSubagentPrefixShapeGuardDetectsChange (T011 / REV A3.2 / C3) verifies the
// mechanism the subagent loop relies on: within a subagent run the system prompt
// and tools are frozen at run start, so CompareShape must return no reasons for
// an identical shape (no false trip) and MUST flag a system change if one ever
// occurred — which is what makes the subagent.go:149-157 guard a real backstop.
func TestSubagentPrefixShapeGuardDetectsChange(t *testing.T) {
	tools := []contract.ToolDefinition{definition("read_file", "read", map[string]any{"path": map[string]any{"type": "string"}}, nil)}
	base, err := NewPrefixShape("You are the explore subagent. Workspace: /w", tools, 0, "main")
	if err != nil {
		t.Fatal(err)
	}
	same, _ := NewPrefixShape("You are the explore subagent. Workspace: /w", tools, 0, "main")
	if reasons := CompareShape(base, same); len(reasons) != 0 {
		t.Fatalf("identical subagent shape must not report reasons: %v", reasons)
	}
	changed, _ := NewPrefixShape("You are the explore subagent. Workspace: /w\n\nShared session memory:\nNEW FACT", tools, 0, "main")
	reasons := CompareShape(base, changed)
	foundSystem := false
	for _, r := range reasons {
		if r == PrefixReasonSystem {
			foundSystem = true
		}
	}
	if !foundSystem {
		t.Fatalf("a changed subagent system prompt must be detected (system reason), got %v", reasons)
	}
}

// TestSubagentMultiTurnRunDoesNotFalselyTripGuard (T011 / REV A3.2) runs a real
// multi-turn explore subagent (tool call then final report) and asserts the
// per-turn shape guard never falsely fails the run — the realistic risk, since
// the guard compares a frozen shape against itself every turn.
func TestSubagentMultiTurnRunDoesNotFalselyTripGuard(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("s", "run_subagent", `{"agent":"explore","task":"find the config loader"}`)}},
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("r", "read_file", `{"path":"config.go"}`)}}, // subagent turn 1
		{Content: "Findings: config loads from config.go; verified."},                                    // subagent turn 2 -> done
		{Content: "All done."}, // main loop final
	}}
	settings := engineSettings()
	settings.Effort = contract.EffortHigh // grants a non-zero subagent budget
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "sub", WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry(&recordingTool{name: "run_subagent"}, &recordingTool{name: "read_file"}), Prompt: PromptContext{Model: "Test"}})
	if err != nil {
		t.Fatal(err)
	}
	// A "large"-class prompt so the task grants a subagent budget (agents>0):
	// breadth ("across the") plus four named paths are the two independent size
	// signals the classifier requires to escalate.
	if _, _, err := engine.Run(context.Background(), "Implement a new config loader module with validation across the package and verify it: internal/config/loader.go, internal/config/schema.go, internal/config/validate.go, internal/config/loader_test.go"); err != nil {
		t.Fatal(err)
	}
	sawReport := false
	for _, msg := range engine.history.All() {
		if strings.Contains(msg.Content, "stable prefix changed") {
			t.Fatalf("multi-turn subagent falsely tripped the prefix guard: %q", msg.Content)
		}
		if msg.Role == contract.RoleTool && strings.Contains(msg.Content, "Findings") {
			sawReport = true
		}
	}
	if !sawReport {
		t.Fatalf("expected the subagent findings in history (multi-turn run), got %+v", engine.history.All())
	}
}

func TestCacheHitGuardStablePrefixScenarios(t *testing.T) {
	tests := []struct {
		name      string
		responses []contract.ChatResponse
		prompts   []string
		tools     []contract.Tool
	}{
		{
			name: "dialogue and continuation",
			responses: []contract.ChatResponse{
				{Content: "one", Usage: guardUsage(0, 100)},
				{Content: "two", Usage: guardUsage(95, 5)},
				{Content: "three", Usage: guardUsage(98, 2)},
				{Content: "four", Usage: guardUsage(99, 1)},
			},
			prompts: []string{"hi", "continue", "keep going", "finish"},
		},
		{
			name: "tool loop",
			responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("c1", "read_file", `{"path":"a"}`)}, Usage: guardUsage(0, 100)},
				{Content: "tool complete", Usage: guardUsage(96, 4)},
				{Content: "follow-up", Usage: guardUsage(98, 2)},
			},
			prompts: []string{"inspect a", "summarize"}, tools: []contract.Tool{&recordingTool{name: "read_file"}},
		},
		{
			name: "pinned MCP block",
			responses: []contract.ChatResponse{
				{Content: "one", Usage: guardUsage(0, 100)},
				{Content: "two", Usage: guardUsage(99, 1)},
				{Content: "three", Usage: guardUsage(99, 1)},
			},
			prompts: []string{"hi", "again", "done"}, tools: []contract.Tool{&recordingTool{name: "mcp__demo__z"}, &recordingTool{name: "mcp__demo__a"}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &scriptedProvider{responses: append([]contract.ChatResponse(nil), test.responses...)}
			settings := engineSettings()
			engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "guard", WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry(test.tools...), Prompt: PromptContext{Model: "Test"}})
			if err != nil {
				t.Fatal(err)
			}
			for _, prompt := range test.prompts {
				if _, _, err := engine.Run(context.Background(), prompt); err != nil {
					t.Fatal(err)
				}
			}
			assertAppendOnlyWireRequests(t, provider.requests)
			if rate := conceptualPrefixHitRate(t, provider.requests); rate < 0.90 {
				t.Fatalf("tail-average prefix hit rate %.2f%% is below guard", rate*100)
			}
			for _, record := range engine.UsageRecords()[1:] {
				if record.PrefixChanged && len(engine.invalidations.EventsForRequest(record.Seq)) == 0 {
					t.Fatalf("shape changed without ledger event: %+v", record)
				}
			}
		})
	}
}

func TestCacheHitGuardLandingKeepsToolChoiceStable(t *testing.T) {
	responses := make([]contract.ChatResponse, 6)
	for index := range responses {
		responses[index] = contract.ChatResponse{
			Content:   "landing",
			ToolCalls: []contract.ToolCall{contract.NewToolCall(fmt.Sprintf("c%d", index), "read_file", `{"path":"a"}`)},
			Usage:     guardUsage(index*100, 100),
		}
	}
	provider := &scriptedProvider{responses: responses}
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "landing", WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry(&recordingTool{name: "read_file"}), Prompt: PromptContext{Model: "Test"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.Run(context.Background(), "help"); err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) < 6 {
		t.Fatalf("landing path was not reached: %d requests", len(provider.requests))
	}
	for index, request := range provider.requests {
		if request.ToolChoice != "auto" {
			t.Fatalf("request %d tool_choice=%q", index+1, request.ToolChoice)
		}
	}
	assertAppendOnlyWireRequests(t, provider.requests)
}

func TestCacheHitGuardMultiTaskWorkloadWithBoundaryToolChange(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("read", "read_file", `{"path":"a"}`)}, Usage: guardUsage(0, 100)},
		{Content: "read complete", Usage: guardUsage(96, 4)},
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("write", "write_file", `{"path":"a","content":"updated"}`)}, Usage: guardUsage(98, 2)},
		{Content: "edit complete", Usage: guardUsage(98, 2)},
		{Content: "re-read complete", Usage: guardUsage(99, 1)},
	}}
	boundaryCalls := 0
	boundaryTools := func() (BoundaryToolChange, bool, error) {
		boundaryCalls++
		if boundaryCalls != 2 {
			return BoundaryToolChange{}, false, nil
		}
		return BoundaryToolChange{Tools: []contract.Tool{&recordingTool{name: "mcp__demo__lookup"}}, Scope: "add demo MCP tools"}, true, nil
	}
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "workload", WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry(&recordingTool{name: "read_file"}, &recordingTool{name: "write_file"}), BoundaryTools: boundaryTools, Prompt: PromptContext{Model: "Test"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, prompt := range []string{"read the fixture", "edit the fixture", "re-read and summarize"} {
		if _, _, err := engine.Run(context.Background(), prompt); err != nil {
			t.Fatal(err)
		}
	}
	assertWireRequestsRespectLedger(t, provider.requests, engine.InvalidationEvents())
}

func assertAppendOnlyWireRequests(t *testing.T, requests []contract.ChatRequest) {
	t.Helper()
	for index := 1; index < len(requests); index++ {
		previous, current := requests[index-1], requests[index]
		if len(current.Messages) < len(previous.Messages) {
			t.Fatalf("request %d shrank from %d to %d messages", index+1, len(previous.Messages), len(current.Messages))
		}
		previousBytes, _ := json.Marshal(previous.Messages)
		settledBytes, _ := json.Marshal(current.Messages[:len(previous.Messages)])
		if string(previousBytes) != string(settledBytes) {
			t.Fatalf("request %d rewrote settled message bytes", index+1)
		}
		previousPrefix, _ := json.Marshal(struct {
			Model      string                    `json:"model"`
			Tools      []contract.ToolDefinition `json:"tools"`
			ToolChoice string                    `json:"tool_choice"`
		}{previous.ModelID, previous.Tools, previous.ToolChoice})
		currentPrefix, _ := json.Marshal(struct {
			Model      string                    `json:"model"`
			Tools      []contract.ToolDefinition `json:"tools"`
			ToolChoice string                    `json:"tool_choice"`
		}{current.ModelID, current.Tools, current.ToolChoice})
		if string(previousPrefix) != string(currentPrefix) {
			t.Fatalf("request %d rewrote stable params", index+1)
		}
	}
}

func assertWireRequestsRespectLedger(t *testing.T, requests []contract.ChatRequest, events []contract.InvalidationEvent) {
	t.Helper()
	eventRequests := make(map[int]bool, len(events))
	for _, event := range events {
		eventRequests[event.RequestSeq] = true
	}
	for index := 1; index < len(requests); index++ {
		if eventRequests[index+1] {
			continue
		}
		assertAppendOnlyWireRequests(t, requests[index-1:index+1])
	}
}

func conceptualPrefixHitRate(t *testing.T, requests []contract.ChatRequest) float64 {
	t.Helper()
	if len(requests) < 2 {
		return 1
	}
	var total, hit int
	for index := 1; index < len(requests); index++ {
		previous, current := requests[index-1], requests[index]
		if len(current.Messages) < len(previous.Messages) {
			continue
		}
		eligible, _ := json.Marshal(struct {
			Tools    []contract.ToolDefinition `json:"tools"`
			Messages []contract.Message        `json:"messages"`
		}{previous.Tools, previous.Messages})
		candidate, _ := json.Marshal(struct {
			Tools    []contract.ToolDefinition `json:"tools"`
			Messages []contract.Message        `json:"messages"`
		}{current.Tools, current.Messages[:len(previous.Messages)]})
		total += len(eligible)
		hit += commonPrefixLength(eligible, candidate)
	}
	if total == 0 {
		return 1
	}
	return float64(hit) / float64(total)
}

func commonPrefixLength(left, right []byte) int {
	limit := min(len(left), len(right))
	for index := 0; index < limit; index++ {
		if left[index] != right[index] {
			return index
		}
	}
	return limit
}

func guardUsage(read, miss int) contract.Usage {
	return contract.Usage{PromptTokens: read + miss, PromptTokensAvailable: true, CacheReadTokens: &read, CacheMissTokens: &miss, CachedTokens: read}
}
