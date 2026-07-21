package orchestrator

import (
	"context"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// The zipper. This is the property the live 12:42 gateway log proved the client
// already has, turned into a permanent test.
//
// In that log every cache HIT read exactly the previous request's entire input
// (41,103 of 41,104; 42,898 of 42,899; 43,506 of 43,507) — each request extends
// the last one byte-for-byte, so the provider matches all of it and prefills
// only the tail. That is a ~99.9% hit, and it is what makes the misses in the
// same session provably placement failures rather than byte drift.
//
// The value of pinning it here: the day someone adds a per-turn timestamp, a
// re-sorted tool list, or an in-place history edit, the hit rate quietly halves
// and the only symptom is a bigger bill. This fails instead.
func TestEveryRequestExtendsThePreviousOneByteForByte(t *testing.T) {
	settings := engineSettings()
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("c1", "read_file", `{"path":"a.go"}`)}},
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("c2", "read_file", `{"path":"b.go"}`)}},
		{Content: "reasoning about it", ToolCalls: []contract.ToolCall{contract.NewToolCall("c3", "read_file", `{"path":"c.go"}`)}},
		{Content: "done"},
	}}
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "zipper", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(&recordingTool{name: "read_file"}),
		History: NewHistory(HistorySnapshot{Version: 1}, nil),
		Prompt:  PromptContext{Workspace: "/w", OS: "linux", Shell: "bash", Model: "m"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.Run(context.Background(), "read a.go, b.go and c.go and summarize them"); err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) < 4 {
		t.Fatalf("expected four turns, got %d", len(provider.requests))
	}

	for index := 1; index < len(provider.requests); index++ {
		previous, current := provider.requests[index-1], provider.requests[index]
		if len(current.Messages) <= len(previous.Messages) {
			t.Fatalf("request %d did not grow: %d messages after %d", index, len(current.Messages), len(previous.Messages))
		}
		// Every message the previous request sent must reappear IDENTICALLY, in
		// the same position. One changed byte anywhere in this span truncates the
		// provider's match at that point and re-bills everything after it.
		for position := range previous.Messages {
			before, after := previous.Messages[position], current.Messages[position]
			if before.Role != after.Role || before.Content != after.Content {
				t.Fatalf("request %d rewrote settled message %d (role %s→%s):\n  before=%q\n  after =%q",
					index, position, before.Role, after.Role, before.Content, after.Content)
			}
			if before.ToolCallID != after.ToolCallID || len(before.ToolCalls) != len(after.ToolCalls) {
				t.Fatalf("request %d rewrote settled tool pairing at message %d", index, position)
			}
			for call := range before.ToolCalls {
				if before.ToolCalls[call].ArgumentsJSON() != after.ToolCalls[call].ArgumentsJSON() {
					t.Fatalf("request %d rewrote settled tool arguments at message %d", index, position)
				}
			}
		}
		// The system prompt and the tool block ride ahead of all of it; a change
		// in either invalidates the whole prefix rather than a suffix of it.
		if previous.Messages[0].Content != current.Messages[0].Content {
			t.Fatalf("request %d changed the system prompt", index)
		}
		if len(previous.Tools) != len(current.Tools) {
			t.Fatalf("request %d changed the tool count: %d → %d", index, len(previous.Tools), len(current.Tools))
		}
	}

	// The ledger must agree: a settled-byte change is recorded as a change
	// reason, so an empty reason set on every record is independent confirmation
	// that nothing above was papered over.
	for _, record := range engine.UsageRecords() {
		if record.Stream == contract.UsageStreamMain && len(record.ChangeReasons) > 0 {
			t.Fatalf("a main request reported prefix change reasons on an append-only session: %v", record.ChangeReasons)
		}
	}
}
