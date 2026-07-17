package mcpclient

import (
	"encoding/json"
	"fmt"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func normalizeSchema(value any) map[string]any {
	if schema, ok := value.(map[string]any); ok {
		if _, exists := schema["type"]; !exists {
			schema["type"] = "object"
		}
		if _, exists := schema["properties"]; !exists {
			schema["properties"] = map[string]any{}
		}
		return schema
	}
	return map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": true}
}

func canonicalDefinition(definition contract.ToolDefinition) contract.ToolDefinition {
	payload, _ := json.Marshal(definition)
	var result contract.ToolDefinition
	_ = json.Unmarshal(payload, &result)
	if result.Type == "" {
		result.Type = "function"
	}
	result.Function.Parameters = normalizeSchema(result.Function.Parameters)
	return result
}

func deterministicToolName(base string, used map[string]bool) string {
	if len(base) > 64 {
		base = base[:64]
	}
	if !used[base] {
		used[base] = true
		return base
	}
	for index := 2; ; index++ {
		suffix := fmt.Sprintf("_%d", index)
		name := base[:min(len(base), 64-len(suffix))] + suffix
		if !used[name] {
			used[name] = true
			return name
		}
	}
}

func cloneDefinitionsByName(values map[string]contract.ToolDefinition) map[string]contract.ToolDefinition {
	result := make(map[string]contract.ToolDefinition, len(values))
	for name, definition := range values {
		result[name] = canonicalDefinition(definition)
	}
	return result
}

func definitionsEqual(left, right map[string]contract.ToolDefinition) bool {
	if len(left) != len(right) {
		return false
	}
	for name, definition := range left {
		other, ok := right[name]
		if !ok {
			return false
		}
		a, _ := json.Marshal(definition)
		b, _ := json.Marshal(other)
		if string(a) != string(b) {
			return false
		}
	}
	return true
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
