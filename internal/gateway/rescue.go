package gateway

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/muhiya/muhiyacode/internal/contract"
)

var (
	invokeRE    = regexp.MustCompile(`(?is)invoke\s+name\s*=\s*["']([\w.-]+)["'](.*?)</[^>]*invoke\s*>`)
	parameterRE = regexp.MustCompile(`(?is)parameter\s+name\s*=\s*["']([\w.-]+)["'][^>]*?>(.*?)</[^>]*parameter\s*>`)
	wrapperRE   = regexp.MustCompile(`(?is)</?[^>]*tool_calls\s*>`)
	rescueID    atomic.Uint64
)

var stringKeys = map[string]bool{"path": true, "oldString": true, "newString": true, "content": true, "command": true, "pattern": true, "query": true, "title": true, "task": true, "agent": true, "url": true}
var boolKeys = map[string]bool{"replaceAll": true, "recursive": true}
var numberKeys = map[string]bool{"offset": true, "limit": true, "maxResults": true, "maxEntries": true, "timeoutMs": true, "estimatedSteps": true}

func RescueToolCalls(content string, knownTools []string) ([]contract.ToolCall, string) {
	known := make(map[string]bool, len(knownTools))
	for _, tool := range knownTools {
		known[tool] = true
	}
	var calls []contract.ToolCall
	for _, match := range invokeRE.FindAllStringSubmatch(content, -1) {
		if len(match) < 3 || !known[match[1]] {
			continue
		}
		args := make(map[string]any)
		for _, parameter := range parameterRE.FindAllStringSubmatch(match[2], -1) {
			if len(parameter) < 3 {
				continue
			}
			key := camelCase(parameter[1])
			args[key] = coerceParameter(key, parameter[2])
		}
		encoded, _ := json.Marshal(args)
		id := fmt.Sprintf("rescued_%d_%s", rescueID.Add(1), match[1])
		calls = append(calls, contract.NewToolCall(id, match[1], string(encoded)))
	}
	if len(calls) > 0 {
		cleaned := invokeRE.ReplaceAllString(content, "")
		cleaned = wrapperRE.ReplaceAllString(cleaned, "")
		return calls, strings.TrimSpace(cleaned)
	}
	if calls, ok := rescueJSON(content, known); ok {
		return calls, ""
	}
	return nil, content
}

func rescueJSON(content string, known map[string]bool) ([]contract.ToolCall, bool) {
	text := strings.TrimSpace(content)
	if strings.HasPrefix(text, "```") && strings.HasSuffix(text, "```") {
		lines := strings.Split(text, "\n")
		if len(lines) >= 3 {
			text = strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
		}
	}
	if strings.HasPrefix(text, "[") {
		var arr []json.RawMessage
		if json.Unmarshal([]byte(text), &arr) == nil {
			return extractCalls(arr, known)
		}
	}
	var raw map[string]json.RawMessage
	if !strings.HasPrefix(text, "{") || json.Unmarshal([]byte(text), &raw) != nil {
		return nil, false
	}
	for _, key := range []string{"tool_calls", "calls"} {
		if len(raw[key]) > 0 {
			var arr []json.RawMessage
			if json.Unmarshal(raw[key], &arr) == nil {
				if calls, ok := extractCalls(arr, known); ok {
					return calls, true
				}
			}
		}
	}
	if call, ok := extractSingleCall(raw, known); ok {
		return []contract.ToolCall{call}, true
	}
	return nil, false
}

func extractCalls(arr []json.RawMessage, known map[string]bool) ([]contract.ToolCall, bool) {
	var calls []contract.ToolCall
	for _, rawItem := range arr {
		var obj map[string]json.RawMessage
		if json.Unmarshal(rawItem, &obj) == nil {
			if call, ok := extractSingleCall(obj, known); ok {
				calls = append(calls, call)
				if len(calls) == 10 {
					break
				}
			}
		}
	}
	if len(calls) > 0 {
		return calls, true
	}
	return nil, false // Note: Empty array yields no rescued calls.
}

func extractSingleCall(raw map[string]json.RawMessage, known map[string]bool) (contract.ToolCall, bool) {
	var name string
	for _, key := range []string{"name", "tool"} {
		if value := raw[key]; len(value) > 0 && json.Unmarshal(value, &name) == nil && name != "" {
			break
		}
	}
	if !known[name] {
		return contract.ToolCall{}, false
	}
	args := json.RawMessage(`{}`)
	for _, key := range []string{"arguments", "parameters", "args"} {
		if len(raw[key]) > 0 {
			args = raw[key]
			var encoded string
			if json.Unmarshal(args, &encoded) == nil {
				args = json.RawMessage(encoded)
			}
			break
		}
	}
	return contract.NewToolCall(fmt.Sprintf("rescued_%d", rescueID.Add(1)), name, string(args)), true
}

func camelCase(value string) string {
	parts := strings.Split(value, "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

func coerceParameter(key, raw string) any {
	if stringKeys[key] {
		return raw
	}
	value := strings.TrimSpace(raw)
	if boolKeys[key] {
		if parsed, err := strconv.ParseBool(value); err == nil {
			return parsed
		}
	}
	if numberKeys[key] {
		if parsed, err := strconv.ParseFloat(value, 64); err == nil {
			return parsed
		}
	}
	if strings.HasPrefix(value, "[") || strings.HasPrefix(value, "{") {
		var parsed any
		if json.Unmarshal([]byte(value), &parsed) == nil {
			return parsed
		}
	}
	return raw
}
