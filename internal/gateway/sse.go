package gateway

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
)

type StreamResult struct {
	Content      string
	Reasoning    string
	ToolCalls    []contract.ToolCall
	Usage        contract.Usage
	FinishReason string
}

type partialCall struct {
	id, name, arguments string
}

type StreamAccumulator struct {
	content, reasoning, finish string
	calls                      map[int]*partialCall
	usage                      contract.Usage
	malformed                  int
	received                   bool
	onToken, onReasoning       func(string)
}

func NewStreamAccumulator(onToken, onReasoning func(string)) *StreamAccumulator {
	return &StreamAccumulator{calls: make(map[int]*partialCall), onToken: onToken, onReasoning: onReasoning}
}

func (a *StreamAccumulator) ConsumeLine(raw string) error {
	line := strings.TrimSpace(raw)
	if !strings.HasPrefix(line, "data:") {
		return nil
	}
	data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if data == "" || data == "[DONE]" {
		return nil
	}
	var chunk map[string]any
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		a.malformed++
		if a.malformed > 20 {
			return fmt.Errorf("provider stream is not valid SSE JSON: %w", err)
		}
		return nil
	}
	a.received = true
	a.mergeUsage(chunk["usage"])
	choices, _ := chunk["choices"].([]any)
	for _, rawChoice := range choices {
		choice, _ := rawChoice.(map[string]any)
		if finish, ok := choice["finish_reason"].(string); ok && finish != "" {
			a.finish = finish
		}
		delta, _ := choice["delta"].(map[string]any)
		message, _ := choice["message"].(map[string]any)
		reasoning := stringValue(delta["reasoning_content"])
		if reasoning == "" {
			reasoning = stringValue(message["reasoning_content"])
		}
		if reasoning != "" {
			a.reasoning += reasoning
			if a.onReasoning != nil {
				a.onReasoning(reasoning)
			}
		}
		token := stringValue(delta["content"])
		if token == "" && delta["content"] == nil {
			token = stringValue(message["content"])
		}
		if token != "" {
			a.content += token
			if a.onToken != nil {
				a.onToken(token)
			}
		}
		toolCalls := delta["tool_calls"]
		if toolCalls == nil {
			toolCalls = message["tool_calls"]
		}
		a.ingestCalls(toolCalls)
	}
	return nil
}

func (a *StreamAccumulator) ReceivedData() bool { return a.received }

func (a *StreamAccumulator) Result() StreamResult {
	indexes := make([]int, 0, len(a.calls))
	for index := range a.calls {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	calls := make([]contract.ToolCall, 0, len(indexes))
	for _, index := range indexes {
		call := a.calls[index]
		if call.name != "" {
			id := call.id
			if id == "" {
				id = fmt.Sprintf("tool_%d", index)
			}
			calls = append(calls, contract.NewToolCall(id, call.name, call.arguments))
		}
	}
	return StreamResult{Content: a.content, Reasoning: a.reasoning, ToolCalls: calls, Usage: a.usage, FinishReason: a.finish}
}

func ParseOpenAIStream(text string) (StreamResult, error) {
	a := NewStreamAccumulator(nil, nil)
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if err := a.ConsumeLine(line); err != nil {
			return StreamResult{}, err
		}
	}
	return a.Result(), nil
}

func (a *StreamAccumulator) ingestCalls(value any) {
	values, _ := value.([]any)
	for position, raw := range values {
		item, _ := raw.(map[string]any)
		index := position
		if number, ok := numberValue(item["index"]); ok {
			index = number
		}
		call := a.calls[index]
		if call == nil {
			call = &partialCall{}
			a.calls[index] = call
		}
		if id := stringValue(item["id"]); id != "" {
			call.id = id
		}
		function, _ := item["function"].(map[string]any)
		call.name += stringValue(function["name"])
		call.arguments += stringValue(function["arguments"])
	}
}

func (a *StreamAccumulator) mergeUsage(value any) {
	raw, _ := value.(map[string]any)
	if raw == nil {
		return
	}
	if value, ok := numberValue(raw["prompt_tokens"]); ok {
		a.usage.PromptTokens = value
	}
	if value, ok := numberValue(raw["completion_tokens"]); ok {
		a.usage.CompletionTokens = value
	}
	if value, ok := numberValue(raw["total_tokens"]); ok {
		a.usage.TotalTokens = value
	}
	cached := 0
	if details, ok := raw["prompt_tokens_details"].(map[string]any); ok {
		cached, _ = numberValue(details["cached_tokens"])
	}
	if deepseek, ok := numberValue(raw["prompt_cache_hit_tokens"]); ok && deepseek > cached {
		cached = deepseek
	}
	if cached > 0 {
		a.usage.CachedTokens = cached
	}
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func numberValue(value any) (int, bool) {
	switch number := value.(type) {
	case float64:
		return int(number), true
	case json.Number:
		value, err := number.Int64()
		return int(value), err == nil
	case int:
		return number, true
	default:
		return 0, false
	}
}
