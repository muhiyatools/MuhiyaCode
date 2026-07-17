package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
)

type StreamResult struct {
	Content          string
	Reasoning        string
	ReasoningDetails json.RawMessage
	ToolCalls        []contract.ToolCall
	Usage            contract.Usage
	FinishReason     string
}

type partialCall struct {
	id, name, arguments string
}

type usageNumberState uint8

const (
	usageNumberMissing usageNumberState = iota
	usageNumberNull
	usageNumberValid
	usageNumberMalformed
)

type usageNumber struct {
	state usageNumberState
	value int
}

type StreamAccumulator struct {
	content, reasoning, finish string
	reasoningDetails           []json.RawMessage
	calls                      map[int]*partialCall
	usage                      contract.Usage
	promptTokens               usageNumber
	completionTokens           usageNumber
	totalTokens                usageNumber
	deepSeekCacheRead          usageNumber
	deepSeekCacheMiss          usageNumber
	openAICacheRead            usageNumber
	usageDiagnostics           []string
	malformed                  int
	received                   bool
	profile                    ModelProfile
	onToken, onReasoning       func(string)
}

func NewStreamAccumulatorForProfile(profile ModelProfile, onToken, onReasoning func(string)) *StreamAccumulator {
	return &StreamAccumulator{calls: make(map[int]*partialCall), profile: profile, onToken: onToken, onReasoning: onReasoning}
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
	decoder := json.NewDecoder(strings.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&chunk); err != nil {
		a.malformed++
		if a.malformed > 20 {
			return fmt.Errorf("provider stream is not valid SSE JSON: %w", err)
		}
		return nil
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		a.malformed++
		if a.malformed > 20 {
			if err == nil {
				err = fmt.Errorf("multiple JSON values")
			}
			return fmt.Errorf("provider stream is not valid SSE JSON: %w", err)
		}
		return nil
	}
	a.received = true
	if usage, ok := chunk["usage"]; ok {
		a.mergeUsage(usage)
	}
	choices, _ := chunk["choices"].([]any)
	for _, rawChoice := range choices {
		choice, _ := rawChoice.(map[string]any)
		if finish, ok := choice["finish_reason"].(string); ok && finish != "" {
			a.finish = finish
		}
		delta, _ := choice["delta"].(map[string]any)
		message, _ := choice["message"].(map[string]any)
		detailValue := delta["reasoning_details"]
		if detailValue == nil {
			detailValue = message["reasoning_details"]
		}
		detailText, details := parseReasoningDetails(detailValue)
		if a.profile.ParsesReasoning {
			a.reasoningDetails = append(a.reasoningDetails, details...)
		}
		reasoning := stringValue(delta["reasoning_content"])
		if reasoning == "" {
			reasoning = stringValue(message["reasoning_content"])
		}
		if reasoning == "" && a.profile.ParsesReasoning {
			reasoning = detailText
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
	var reasoningDetails json.RawMessage
	if len(a.reasoningDetails) > 0 {
		reasoningDetails, _ = json.Marshal(a.reasoningDetails)
	}
	return StreamResult{Content: a.content, Reasoning: a.reasoning, ReasoningDetails: reasoningDetails, ToolCalls: calls, Usage: a.usage, FinishReason: a.finish}
}

func parseReasoningDetails(value any) (string, []json.RawMessage) {
	values, ok := value.([]any)
	if !ok {
		if value == nil {
			return "", nil
		}
		values = []any{value}
	}
	var textParts []string
	var raw []json.RawMessage
	for _, item := range values {
		if detail, ok := item.(map[string]any); ok {
			if text := stringValue(detail["text"]); text != "" {
				textParts = append(textParts, text)
			} else if text := stringValue(detail["content"]); text != "" {
				textParts = append(textParts, text)
			}
		}
		if encoded, err := json.Marshal(item); err == nil {
			raw = append(raw, encoded)
		}
	}
	return strings.Join(textParts, ""), raw
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
	if value == nil {
		return
	}
	raw, ok := value.(map[string]any)
	if !ok {
		a.addUsageDiagnostic("usage must be an object")
		a.rebuildUsage()
		return
	}
	a.mergeUsageNumber(&a.promptTokens, raw, "prompt_tokens", "usage.prompt_tokens")
	a.mergeUsageNumber(&a.completionTokens, raw, "completion_tokens", "usage.completion_tokens")
	a.mergeUsageNumber(&a.totalTokens, raw, "total_tokens", "usage.total_tokens")
	a.mergeUsageNumber(&a.deepSeekCacheRead, raw, "prompt_cache_hit_tokens", "usage.prompt_cache_hit_tokens")
	a.mergeUsageNumber(&a.deepSeekCacheMiss, raw, "prompt_cache_miss_tokens", "usage.prompt_cache_miss_tokens")

	if detailsValue, exists := raw["prompt_tokens_details"]; exists {
		switch details := detailsValue.(type) {
		case nil:
			a.openAICacheRead = usageNumber{state: usageNumberNull}
		case map[string]any:
			a.mergeUsageNumber(&a.openAICacheRead, details, "cached_tokens", "usage.prompt_tokens_details.cached_tokens")
		default:
			a.openAICacheRead = usageNumber{state: usageNumberMalformed}
			a.addUsageDiagnostic("usage.prompt_tokens_details must be an object")
		}
	}
	a.rebuildUsage()
}

func (a *StreamAccumulator) mergeUsageNumber(target *usageNumber, raw map[string]any, key, path string) {
	value, exists := raw[key]
	if !exists {
		return
	}
	if value == nil {
		*target = usageNumber{state: usageNumberNull}
		return
	}
	number, ok := numberValue(value)
	if !ok {
		*target = usageNumber{state: usageNumberMalformed}
		a.addUsageDiagnostic(path + " must be an integer")
		return
	}
	*target = usageNumber{state: usageNumberValid, value: number}
}

func (a *StreamAccumulator) rebuildUsage() {
	usage := contract.Usage{}
	if a.promptTokens.state == usageNumberValid {
		usage.PromptTokens = a.promptTokens.value
		usage.PromptTokensAvailable = true
	}
	if a.completionTokens.state == usageNumberValid {
		usage.CompletionTokens = a.completionTokens.value
		usage.CompletionTokensAvailable = true
	}
	if a.totalTokens.state == usageNumberValid {
		usage.TotalTokens = a.totalTokens.value
	}

	readBlocked := false
	switch a.deepSeekCacheRead.state {
	case usageNumberValid:
		setCacheRead(&usage, a.deepSeekCacheRead.value)
	case usageNumberMalformed:
		readBlocked = true
	}
	if usage.CacheReadTokens == nil && !readBlocked && a.openAICacheRead.state == usageNumberValid {
		setCacheRead(&usage, a.openAICacheRead.value)
	}
	if a.profile.CacheMinPromptTokens > 0 && a.promptTokens.state == usageNumberValid && a.promptTokens.value < a.profile.CacheMinPromptTokens && a.deepSeekCacheRead.state != usageNumberValid {
		usage.CacheReadTokens = nil
		usage.CachedTokens = 0
	}

	switch a.deepSeekCacheMiss.state {
	case usageNumberValid:
		miss := a.deepSeekCacheMiss.value
		usage.CacheMissTokens = &miss
	case usageNumberMissing, usageNumberNull:
		if a.openAICacheRead.state == usageNumberValid && usage.PromptTokensAvailable && usage.CacheReadTokens != nil {
			miss, ok := subtractInt(usage.PromptTokens, *usage.CacheReadTokens)
			if ok {
				usage.CacheMissTokens = &miss
				usage.MissDerived = true
			} else {
				usage.Contradictory = true
				a.addUsageDiagnostic("derived cache miss exceeds the integer range")
			}
		}
	}

	if a.promptTokens.state == usageNumberValid && a.deepSeekCacheRead.state == usageNumberValid && a.deepSeekCacheMiss.state == usageNumberValid &&
		!sumEquals(a.deepSeekCacheRead.value, a.deepSeekCacheMiss.value, a.promptTokens.value) {
		usage.Contradictory = true
	}
	if a.promptTokens.state == usageNumberValid && a.openAICacheRead.state == usageNumberValid && a.openAICacheRead.value > a.promptTokens.value {
		usage.Contradictory = true
	}
	if usage.PromptTokensAvailable && usage.PromptTokens < 0 || usage.CacheReadTokens != nil && *usage.CacheReadTokens < 0 || usage.CacheMissTokens != nil && *usage.CacheMissTokens < 0 {
		usage.Contradictory = true
	}
	usage.Diagnostic = strings.Join(a.usageDiagnostics, "; ")
	a.usage = usage
}

func (a *StreamAccumulator) addUsageDiagnostic(message string) {
	for _, existing := range a.usageDiagnostics {
		if existing == message {
			return
		}
	}
	a.usageDiagnostics = append(a.usageDiagnostics, message)
}

func setCacheRead(usage *contract.Usage, value int) {
	read := value
	usage.CacheReadTokens = &read
	usage.CachedTokens = read
}

func subtractInt(left, right int) (int, bool) {
	if right > 0 && left < minInt+right || right < 0 && left > maxInt+right {
		return 0, false
	}
	return left - right, true
}

func sumEquals(left, right, expected int) bool {
	if right > 0 && left > maxInt-right || right < 0 && left < minInt-right {
		return false
	}
	return left+right == expected
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func numberValue(value any) (int, bool) {
	switch number := value.(type) {
	case float64:
		if math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number || number < float64(minInt) || number >= -float64(minInt) {
			return 0, false
		}
		return int(number), true
	case float32:
		return numberValue(float64(number))
	case json.Number:
		value, err := number.Int64()
		if err != nil || int64(int(value)) != value {
			return 0, false
		}
		return int(value), true
	case int:
		return number, true
	case int8:
		return int(number), true
	case int16:
		return int(number), true
	case int32:
		return int(number), true
	case int64:
		if int64(int(number)) != number {
			return 0, false
		}
		return int(number), true
	case uint:
		if uint64(number) > uint64(maxInt) {
			return 0, false
		}
		return int(number), true
	case uint8:
		return int(number), true
	case uint16:
		return int(number), true
	case uint32:
		if uint64(number) > uint64(maxInt) {
			return 0, false
		}
		return int(number), true
	case uint64:
		if number > uint64(maxInt) {
			return 0, false
		}
		return int(number), true
	default:
		return 0, false
	}
}

const maxInt = int(^uint(0) >> 1)
const minInt = -maxInt - 1
