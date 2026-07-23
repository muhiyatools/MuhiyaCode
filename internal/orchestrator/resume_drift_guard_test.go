package orchestrator

import (
	"context"
	"sync"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// countChangingNormalizer is a provider whose StableRequestMessages inserts one
// fixed synthetic message right after the system message. The insert is
// deterministic and append-only-preserving (turn N's normalized history stays a
// byte-identical prefix of turn N+1's), yet it makes the post-normalization wire
// count differ from len(request.Messages) — exactly what the gateway's
// repairToolMessageSequence does when it heals a dangling assistant tool call on
// a resumed session.
type countChangingNormalizer struct {
	mu        sync.Mutex
	responses []contract.ChatResponse
	requests  []contract.ChatRequest
}

func (p *countChangingNormalizer) Chat(_ context.Context, request contract.ChatRequest) (contract.ChatResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, request)
	if len(p.responses) == 0 {
		return contract.ChatResponse{Content: "done"}, nil
	}
	response := p.responses[0]
	p.responses = p.responses[1:]
	return response, nil
}

func (p *countChangingNormalizer) StableRequestMessages(request contract.ChatRequest) ([]contract.Message, error) {
	if len(request.Messages) == 0 {
		return nil, nil
	}
	synthetic := contract.Message{Role: contract.RoleTool, Content: "<synthetic wire repair>", ToolCallID: "wire-repair"}
	out := make([]contract.Message, 0, len(request.Messages)+1)
	out = append(out, request.Messages[0])
	out = append(out, synthetic)
	out = append(out, request.Messages[1:]...)
	return out, nil
}

func (p *countChangingNormalizer) ListModels(context.Context) ([]contract.Model, error) {
	return nil, nil
}

// TestResumeNormalizationDoesNotFalselyAbort pins A-1. When the provider's wire
// normalization changes the message count, an append-only second turn must NOT
// trip the "stable request prefix changed" guard. The guard must size the
// settled window from the POST-normalization count it actually hashed
// (shape.MessageCount), not the pre-normalization len(messages). With the old
// pre-normalization count, turn 2 slices the settled window one message short,
// the settled hash no longer matches the prior turn's history hash, and the
// engine wrongly reports a history drift with no invalidation event — bricking
// the (typically resumed) session.
func TestResumeNormalizationDoesNotFalselyAbort(t *testing.T) {
	provider := &countChangingNormalizer{responses: []contract.ChatResponse{
		{Content: "Hi there!", Usage: contract.Usage{PromptTokens: 100, CompletionTokens: 5, TotalTokens: 105}},
		{Content: "Hello again!", Usage: contract.Usage{PromptTokens: 120, CompletionTokens: 6, TotalTokens: 126}},
	}}
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{
		Settings: &settings,
		Session:  contract.Session{ID: "s", WorkspacePath: t.TempDir()},
		Provider: provider,
		Registry: NewRegistry(&recordingTool{name: "read_file"}),
		Prompt:   PromptContext{Model: "deepseek-v4-pro"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.Run(context.Background(), "hi"); err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	if _, _, err := engine.Run(context.Background(), "hi"); err != nil {
		t.Fatalf("turn 2 must not abort on an append-only, count-changing normalization: %v", err)
	}
	if engine.lastShape == nil {
		t.Fatal("lastShape not recorded")
	}
	if engine.lastSentMessageCount != engine.lastShape.MessageCount {
		t.Fatalf("lastSentMessageCount=%d must equal the post-normalization shape.MessageCount=%d",
			engine.lastSentMessageCount, engine.lastShape.MessageCount)
	}
}
