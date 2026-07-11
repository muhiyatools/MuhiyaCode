package orchestrator

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestCacheHitGuardStablePrefixScenarios(t *testing.T) {
	tests := []struct {
		name      string
		responses []contract.ChatResponse
		prompts   []string
		tools     []contract.Tool
	}{
		{
			name: "dialogue and goal-style continuation",
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
