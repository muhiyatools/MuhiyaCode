package gateway

import (
	"strings"
	"testing"
)

// TestWebSearchNeverLeaksProvider (003 T028/FR-016) asserts the formatted
// web-search output exposes only the query, numbered titles, URLs, and snippets
// — never the upstream search provider/engine name.
func TestWebSearchNeverLeaksProvider(t *testing.T) {
	payload := []byte(`{
		"query": "go generics",
		"provider": "tavily",
		"engine": "brave",
		"sources": [
			{"index": 1, "title": "Go generics guide", "url": "https://go.dev/generics", "snippet": "An intro."},
			{"index": 2, "title": "Type params", "url": "https://example.com/tp", "snippet": "Details."}
		]
	}`)
	out := formatSearch(payload, "fallback")
	for _, banned := range []string{"tavily", "Tavily", "brave", "Brave", "serper", "Serper", "google", "Google", "bing", "Bing", "provider", "engine"} {
		if strings.Contains(out, banned) {
			t.Fatalf("web search output leaked provider identifier %q:\n%s", banned, out)
		}
	}
	if !strings.Contains(out, "Go generics guide") || !strings.Contains(out, "[2]") {
		t.Fatalf("web search output missing expected result content:\n%s", out)
	}
}

// TestMuhiyaLogChunkParsing (003 T009/D1) verifies the muhiya_log cost chunk is
// parsed when present, and that absence/malformed lines yield no cost (never a
// fabricated value).
func TestMuhiyaLogChunkParsing(t *testing.T) {
	line := `data: {"id":"x","object":"chat.completion.chunk","model":"m","usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15},"muhiya_log":{"cost":0.0242,"log_id":"log-99","usage_estimated":true}}`
	meta, ok := muhiyaLogFromSSELine(line)
	if !ok || meta == nil {
		t.Fatal("expected muhiya_log chunk to parse")
	}
	if meta.cost == nil || *meta.cost != 0.0242 {
		t.Fatalf("cost = %v, want 0.0242", meta.cost)
	}
	if meta.logID != "log-99" || !meta.estimated {
		t.Fatalf("logID/estimated = %q/%v, want log-99/true", meta.logID, meta.estimated)
	}
	// Absence: a normal usage frame carries no muhiya_log.
	if _, ok := muhiyaLogFromSSELine(`data: {"usage":{"prompt_tokens":1}}`); ok {
		t.Fatal("a plain usage frame must not be read as a cost chunk")
	}
	// Malformed muhiya_log yields ok=false (no fabricated cost).
	if _, ok := muhiyaLogFromSSELine(`data: {"muhiya_log": "not-an-object"}`); ok {
		t.Fatal("malformed muhiya_log must not parse")
	}
	if _, ok := muhiyaLogFromSSELine(`data: [DONE]`); ok {
		t.Fatal("[DONE] must not parse as a cost chunk")
	}
}
