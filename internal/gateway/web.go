package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/instructions"
)

type WebSearch struct {
	BaseURL string
	APIKey  string
	Client  *http.Client
}

type WebSearchProbeResult string

const (
	WebSearchProbeSupported   WebSearchProbeResult = "supported"
	WebSearchProbeUnsupported WebSearchProbeResult = "unsupported"
)

// WebSearchTool exposes gateway-hosted search through the common agent tool
// boundary. The gateway remains responsible for retrieval and source policy.
type WebSearchTool struct{ Searcher WebSearch }

func (t WebSearchTool) Definition() contract.ToolDefinition {
	return contract.ToolDefinition{Type: "function", Function: contract.FunctionDefinition{
		Name:        "web_search",
		Description: instructions.ToolWebSearchDescription,
		Parameters: map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{
				"query":      map[string]any{"type": "string", "description": "Focused search query"},
				"maxResults": map[string]any{"type": "integer", "minimum": 1, "maximum": 10},
			},
			"required": []string{"query"},
		},
	}}
}

func (t WebSearchTool) Execute(ctx context.Context, raw json.RawMessage) contract.ToolResult {
	var arguments map[string]any
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return contract.AdaptToolResult("", contract.ToolNotStarted(fmt.Errorf("web_search: invalid arguments: %w", err)))
	}
	output, err := t.Searcher.Search(ctx, arguments)
	return contract.AdaptToolResult(output, err)
}

func (w WebSearch) Supported(ctx context.Context) bool {
	result, definitive, _ := w.Probe(ctx)
	return definitive && result == WebSearchProbeSupported
}

// Probe distinguishes definitive capability results from transient transport
// failures so callers can preserve a persisted last-good snapshot.
func (w WebSearch) Probe(ctx context.Context) (WebSearchProbeResult, bool, error) {
	if w.Client == nil {
		w.Client = &http.Client{}
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	for _, base := range w.bases() {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/capabilities", nil)
		w.headers(req)
		response, err := w.Client.Do(req)
		if err != nil {
			return "", false, err
		}
		var payload map[string]any
		_ = json.NewDecoder(response.Body).Decode(&payload)
		response.Body.Close()
		if response.StatusCode == 404 || response.StatusCode == 405 {
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return "", false, fmt.Errorf("capabilities probe returned %d", response.StatusCode)
		}
		if features, ok := payload["features"].(map[string]any); ok && features["web_search"] == true {
			return WebSearchProbeSupported, true, nil
		}
		if tools, ok := payload["tools"].([]any); ok {
			for _, raw := range tools {
				tool, _ := raw.(map[string]any)
				if tool["name"] == "web_search" && tool["available"] != false {
					return WebSearchProbeSupported, true, nil
				}
			}
		}
		return WebSearchProbeUnsupported, true, nil
	}
	return WebSearchProbeUnsupported, true, nil
}

func (w WebSearch) Search(ctx context.Context, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	if strings.TrimSpace(query) == "" {
		return "", contract.ToolNotStarted(fmt.Errorf("web_search: query is required"))
	}
	if w.Client == nil {
		w.Client = &http.Client{}
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	body, _ := json.Marshal(args)
	var last string
	for _, base := range w.bases() {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, base+"/tools/web_search", bytes.NewReader(body))
		w.headers(req)
		response, err := w.Client.Do(req)
		if err != nil {
			return "", err
		}
		payload, _ := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024))
		response.Body.Close()
		if response.StatusCode == 404 || response.StatusCode == 405 {
			last = strings.TrimSpace(string(payload))
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return "", fmt.Errorf("web_search failed (%d): %s", response.StatusCode, strings.TrimSpace(string(payload)))
		}
		return formatSearch(payload, query), nil
	}
	return "", fmt.Errorf("web_search failed: %s", last)
}

func (w WebSearch) bases() []string {
	base := strings.TrimRight(w.BaseURL, "/")
	base = strings.TrimSuffix(base, "/chat/completions")
	values := []string{base}
	if strings.HasSuffix(base, "/v1") {
		values = append(values, strings.TrimSuffix(base, "/v1"))
	}
	return unique(values)
}

func (w WebSearch) headers(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+w.APIKey)
	req.Header.Set("X-Client-App", "MuhiyaCode")
}

func formatSearch(payload []byte, fallback string) string {
	var raw map[string]any
	if json.Unmarshal(payload, &raw) != nil {
		return string(payload)
	}
	if content := stringValue(raw["content"]); content != "" {
		return content
	}
	query := stringValue(raw["query"])
	if query == "" {
		query = fallback
	}
	lines := []string{fmt.Sprintf("Web search results for %q:", query)}
	if answer := stringValue(raw["answer"]); answer != "" {
		lines = append(lines, "[Answer] "+answer)
	}
	if sources, ok := raw["sources"].([]any); ok {
		for i, entry := range sources {
			source, _ := entry.(map[string]any)
			index, ok := numberValue(source["index"])
			if !ok {
				index = i + 1
			}
			lines = append(lines, fmt.Sprintf("[%d] %s - %s", index, stringValue(source["title"]), stringValue(source["url"])))
			if snippet := stringValue(source["snippet"]); snippet != "" {
				lines = append(lines, snippet)
			}
		}
	}
	lines = append(lines, "Cite these sources inline as [n].")
	return strings.Join(lines, "\n")
}
