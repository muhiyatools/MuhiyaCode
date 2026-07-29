package orchestrator

import (
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestClassificationAndBudgets(t *testing.T) {
	tests := []struct {
		prompt string
		want   TaskClass
	}{
		{"hi", ClassChat},
		{"fix typo in README.md", ClassTiny},
		{"Make for me a simple snake game in a single html file", ClassTiny},
		{"add a user management system", ClassStandard},
		{"fully migrate the entire project from TypeScript to Go with a new architecture", ClassEpic},
	}
	for _, test := range tests {
		if got := Classify(test.prompt, "").Class; got != test.want {
			t.Errorf("Classify(%q) = %s, want %s", test.prompt, got, test.want)
		}
	}
	budget := BudgetFor(Classify("hi", ""), Profile(contract.EffortMax))
	if budget.Reasoning != contract.ReasoningLow {
		t.Fatalf("chat reasoning must stay cheap even when the session is set to max: %+v", budget)
	}
	if BudgetFor(Classify("hi", ""), Profile(contract.EffortLow)).Reasoning != contract.ReasoningLow {
		t.Fatal("low effort should send low reasoning")
	}
	snake := BudgetFor(Classify("Make for me a simple snake game in a single html file", ""), Profile(contract.EffortHigh))
	if snake.ToolCalls != 5 || snake.MaxChecks != 1 || snake.MaxTurns != 5 ||
		snake.Reasoning != contract.ReasoningLow || snake.SingleArtifactExtension != ".html" {
		t.Fatalf("simple single-file budget is not bounded: %+v", snake)
	}
}

func TestSystemPromptBudgetAndStability(t *testing.T) {
	ctx := PromptContext{Workspace: `F:\work`, OS: "Windows", Shell: "pwsh", Model: "deepseek-v4-pro", ModelAddendum: ""}
	first, second := SystemPrompt(ctx), SystemPrompt(ctx)
	if first != second {
		t.Fatal("system prompt is not byte-stable")
	}
	if tokens := EstimateTokens(first); tokens > 1900 {
		t.Fatalf("system prompt is too large: %d tokens", tokens)
	}
	// The prompt must not embed per-task state (an effort section or a concrete
	// class value), which would drift it between turns and bust the cache. It
	// may still *reference* the [task-brief] as the stable place that state
	// lives.
	for _, forbidden := range []string{"EFFORT ", "class=chat", "class=standard"} {
		if strings.Contains(first, forbidden) {
			t.Fatalf("system prompt embeds per-task state %q — breaks prefix stability", forbidden)
		}
	}
}

func TestHistoryFoldAndIntactness(t *testing.T) {
	h := NewHistory(HistorySnapshot{Version: 1}, nil)
	h.Append(contract.Message{Role: contract.RoleUser, Content: "task one"})
	h.Append(contract.Message{Role: contract.RoleAssistant, ToolCalls: []contract.ToolCall{contract.NewToolCall("c1", "read_file", `{"path":"big.go","padding":"`+strings.Repeat("x", 400)+`"}`)}})
	h.Append(contract.Message{Role: contract.RoleTool, ToolCallID: "c1", Content: "read big.go\n" + strings.Repeat("a", 1000)})
	h.MarkTaskStart()
	h.Append(contract.Message{Role: contract.RoleUser, Content: "task two"})
	if removed := h.FoldCompletedTasks(); removed <= 0 {
		t.Fatal("expected folded tokens")
	}
	if h.IsToolResultIntact("c1") {
		t.Fatal("folded result reported intact")
	}
}

// TestPrefixStableAcrossTurnsAndClasses is the core cache guarantee: the shared
// message prefix of successive requests in a session must be byte-identical, so
// DeepSeek's implicit prefix cache keeps hitting. This is what raises the hit
// rate from ~73% toward Reasonix's ~99%.
func TestPrefixStableAcrossTurnsAndClasses(t *testing.T) {
	system := SystemPrompt(PromptContext{Workspace: `F:\work`, OS: "windows", Shell: "pwsh", Model: "deepseek-v4-pro"})
	h := NewHistory(HistorySnapshot{Version: 1}, nil)

	// Turn 1: a chat greeting.
	h.Append(contract.Message{Role: contract.RoleUser, Content: "hi\n\n" + BudgetFor(Classify("hi", ""), Profile(contract.EffortLow)).Brief})
	req1 := h.BuildRequest(system, 128000, 12000)

	// Turn 2: a coding request in the same session.
	h.Append(contract.Message{Role: contract.RoleAssistant, Content: "Hello!"})
	h.Append(contract.Message{Role: contract.RoleUser, Content: "fix the bug in main.go\n\n" + BudgetFor(Classify("fix the bug in main.go", ""), Profile(contract.EffortLow)).Brief})
	req2 := h.BuildRequest(system, 128000, 12000)

	// req2 must start with req1's messages, byte-for-byte (same system prompt,
	// same earlier user turn) — no lite/full switch, no history rewrite.
	if len(req2) < len(req1) {
		t.Fatalf("second request lost prefix messages: %d < %d", len(req2), len(req1))
	}
	for i := range req1 {
		if req1[i].Role != req2[i].Role || req1[i].Content != req2[i].Content {
			t.Fatalf("prefix message %d diverged between turns:\n  turn1=%q\n  turn2=%q", i, req1[i].Content, req2[i].Content)
		}
	}
}
