package orchestrator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// erroringProvider always fails Chat, simulating a streamed interruption that
// the gateway does not retry.
type erroringProvider struct{}

func (erroringProvider) Chat(context.Context, contract.ChatRequest) (contract.ChatResponse, error) {
	return contract.ChatResponse{}, errTestProvider
}
func (erroringProvider) ListModels(context.Context) ([]contract.Model, error) { return nil, nil }
func (erroringProvider) StableRequestMessages(r contract.ChatRequest) ([]contract.Message, error) {
	return append([]contract.Message(nil), r.Messages...), nil
}

var errTestProvider = errTest("provider stream interrupted")

type errTest string

func (e errTest) Error() string { return string(e) }

// TestProviderErrorLeavesNoPartialAssistantMessage (T045 / REV E7) documents the
// investigation conclusion: a provider error surfaces to the caller and leaves
// NO orphaned/partial assistant message in history, so there is no
// double-append or context-loss gap that would warrant a stream-recovery port.
func TestProviderErrorLeavesNoPartialAssistantMessage(t *testing.T) {
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "err", WorkspacePath: t.TempDir()}, Provider: erroringProvider{}, Registry: NewRegistry(), Prompt: PromptContext{Model: "Test"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, runErr := engine.Run(context.Background(), "hi"); runErr == nil {
		t.Fatal("expected the provider error to surface from Run")
	}
	for _, m := range engine.history.All() {
		if m.Role == contract.RoleAssistant {
			t.Fatalf("provider error left an orphaned assistant message in history: %+v", m)
		}
	}
}

// TestValidateCallArgs (T028 / REV B8 / dispatch-gate contract §2) exercises the
// pre-dispatch argument validator across every failure class, including the B8
// regression: numeric and boolean enums must accept valid members (they used to
// reject everything because non-string values were stringified before compare).
func TestValidateCallArgs(t *testing.T) {
	defs := []contract.ToolDefinition{definition("cfg", "test", map[string]any{
		"path":  map[string]any{"type": "string"},
		"level": map[string]any{"type": "integer", "enum": []any{1.0, 2.0, 3.0}},
		"mode":  map[string]any{"type": "string", "enum": []any{"a", "b"}},
		"flag":  map[string]any{"type": "boolean"},
	}, []string{"path"})}
	call := func(args string) contract.ToolCall { return contract.NewToolCall("c", "cfg", args) }

	cases := []struct {
		name    string
		args    string
		wantErr bool
		substr  string
	}{
		{"valid minimal", `{"path":"x"}`, false, ""},
		{"valid full incl numeric+string enum", `{"path":"x","level":2,"mode":"a","flag":true}`, false, ""},
		{"unparseable json", `{"path":`, true, "cut off mid-generation"},
		{"missing required", `{"mode":"a"}`, true, "missing required field"},
		{"wrong primitive type", `{"path":123}`, true, "must be a string"},
		{"numeric enum violation", `{"path":"x","level":9}`, true, "must be one of"},
		{"string enum violation", `{"path":"x","mode":"z"}`, true, "must be one of"},
		{"numeric enum valid (B8 regression)", `{"path":"x","level":3}`, false, ""},
		{"extra unknown field ignored", `{"path":"x","surprise":true}`, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCallArgs(call(tc.args), defs)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %q", tc.args)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.args, err)
			}
			if tc.substr != "" && (err == nil || !strings.Contains(err.Error(), tc.substr)) {
				t.Fatalf("error %v missing substring %q", err, tc.substr)
			}
		})
	}
}

// TestPlainFollowUpTailStaysWithinBudget (T013 / contracts/request-assembly.md §4, SC-004)
// asserts that a plain turn with no active goal/plan/pending/steer adds only the
// small task-classification brief to the user text — bounded well under ~50
// system tokens — and carries none of the heavier dynamic blocks. This keeps the
// per-turn uncached tail minimal, which is what the whole cache design protects.
func TestPlainFollowUpTailStaysWithinBudget(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{{Content: "an answer", Usage: guardUsage(0, 50)}}}
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "tail", WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry(), Prompt: PromptContext{Model: "Test"}})
	if err != nil {
		t.Fatal(err)
	}
	raw := "what does this helper function do"
	if _, _, err := engine.Run(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	req := provider.requests[len(provider.requests)-1]
	last := req.Messages[len(req.Messages)-1]
	if last.Role != contract.RoleUser {
		t.Fatalf("last message should be the user turn, got role %s", last.Role)
	}
	if !strings.HasPrefix(last.Content, raw) {
		t.Fatalf("user prompt must lead the tail, got %q", last.Content)
	}
	added := strings.TrimSpace(last.Content[len(raw):])
	if tokens := EstimateTokens(added); tokens > 50 {
		t.Fatalf("plain follow-up added %d system tokens (>50 budget): %q", tokens, added)
	}
	// No goal/plan/steer/project-context-update blocks may appear on a plain turn.
	for _, banned := range []string{"active-goal", "plan-mode", "executing saved plan", "project-instructions-update", "memory-update"} {
		if strings.Contains(added, banned) {
			t.Fatalf("unexpected dynamic block %q on a plain turn: %q", banned, added)
		}
	}
}

// TestToolsArrayStableAcrossModeToggles (T014 / contracts/request-assembly.md §1, FR-015)
// asserts the prime invariant: the tools array is byte-identical across a whole
// multi-task session even as plan mode and goal mode are toggled between tasks.
// Mode restrictions are enforced at dispatch time, never by changing the schema
// the model sees — so a toggle never busts the cached prefix.
func TestToolsArrayStableAcrossModeToggles(t *testing.T) {
	responses := make([]contract.ChatResponse, 8)
	for i := range responses {
		responses[i] = contract.ChatResponse{Content: "ok", Usage: guardUsage(i*10, 10)}
	}
	provider := &scriptedProvider{responses: responses}
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "modes", WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry(&recordingTool{name: "read_file"}, &recordingTool{name: "write_file"}), Prompt: PromptContext{Model: "Test"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.Run(context.Background(), "first task"); err != nil {
		t.Fatal(err)
	}
	engine.SetLifecycleState(contract.LifecyclePlanning)
	if _, _, err := engine.Run(context.Background(), "second task in plan mode"); err != nil {
		t.Fatal(err)
	}
	engine.SetGoal("achieve the objective") // G3: also clears plan mode
	if _, _, err := engine.Run(context.Background(), "third task under a goal"); err != nil {
		t.Fatal(err)
	}
	engine.ClearGoal()
	if _, _, err := engine.Run(context.Background(), "fourth task"); err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) < 4 {
		t.Fatalf("expected at least 4 requests, got %d", len(provider.requests))
	}
	first, _ := json.Marshal(provider.requests[0].Tools)
	for i, req := range provider.requests {
		cur, _ := json.Marshal(req.Tools)
		if string(cur) != string(first) {
			t.Fatalf("request %d tools array changed under mode toggles (cache bust):\nfirst=%s\ncur=%s", i+1, first, cur)
		}
	}
}

// TestSteadyStateRequestIsPriorPlusAppendedTurn (T012 / contracts/request-assembly.md §2)
// asserts the headline cache guarantee explicitly: for consecutive main-stream
// requests N, N+1 with no invalidation between them, request N+1 is byte-for-byte
// request N plus the turn's appended messages, and every session-stable request
// parameter (model, tools, tool_choice) is unchanged. This is the invariant
// DeepSeek's implicit byte-prefix cache depends on.
func TestSteadyStateRequestIsPriorPlusAppendedTurn(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{Content: "one", Usage: guardUsage(0, 100)},
		{Content: "two", Usage: guardUsage(95, 5)},
		{Content: "three", Usage: guardUsage(98, 2)},
	}}
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "steady", WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry(), Prompt: PromptContext{Model: "Test"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, prompt := range []string{"hello", "keep going", "and finish"} {
		if _, _, err := engine.Run(context.Background(), prompt); err != nil {
			t.Fatal(err)
		}
	}
	if len(provider.requests) < 3 {
		t.Fatalf("expected at least 3 requests, got %d", len(provider.requests))
	}
	for i := 1; i < len(provider.requests); i++ {
		prev, cur := provider.requests[i-1], provider.requests[i]

		// (1) N+1 must be a strict superset that begins with all of N's messages.
		if len(cur.Messages) <= len(prev.Messages) {
			t.Fatalf("request %d did not grow append-only: %d -> %d messages", i+1, len(prev.Messages), len(cur.Messages))
		}
		prevBytes, _ := json.Marshal(prev.Messages)
		sharedBytes, _ := json.Marshal(cur.Messages[:len(prev.Messages)])
		if string(prevBytes) != string(sharedBytes) {
			t.Fatalf("request %d rewrote settled message bytes (prefix cache would miss)", i+1)
		}

		// (2) Reconstruct N+1 = N + appended-tail, byte-for-byte.
		appended := cur.Messages[len(prev.Messages):]
		reconstructed := append(append([]contract.Message(nil), prev.Messages...), appended...)
		reBytes, _ := json.Marshal(reconstructed)
		curBytes, _ := json.Marshal(cur.Messages)
		if string(reBytes) != string(curBytes) {
			t.Fatalf("request %d is not exactly prior + appended tail", i+1)
		}

		// (3) Session-stable request parameters must not vary.
		if cur.ModelID != prev.ModelID {
			t.Fatalf("request %d changed model: %q -> %q", i+1, prev.ModelID, cur.ModelID)
		}
		if cur.ToolChoice != prev.ToolChoice {
			t.Fatalf("request %d changed tool_choice: %q -> %q", i+1, prev.ToolChoice, cur.ToolChoice)
		}
		prevTools, _ := json.Marshal(prev.Tools)
		curTools, _ := json.Marshal(cur.Tools)
		if string(prevTools) != string(curTools) {
			t.Fatalf("request %d changed the tools array", i+1)
		}
	}
}
