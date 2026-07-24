package orchestrator

import (
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// Feature 011 T044 (audit F12): nested object/array item schemas are validated
// pre-dispatch instead of passing and failing at execution.
func TestValidateCallArgsNestedSchemas(t *testing.T) {
	definitions := []contract.ToolDefinition{{Type: "function", Function: contract.FunctionDefinition{
		Name: "demo",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"steps": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"options": map[string]any{
					"type":       "object",
					"required":   []any{"mode"},
					"properties": map[string]any{"mode": map[string]any{"type": "string"}, "count": map[string]any{"type": "number"}},
				},
			},
		},
	}}}
	ok := contract.NewToolCall("c1", "demo", `{"steps":["a","b"],"options":{"mode":"fast","count":2}}`)
	if err := validateCallArgs(ok, definitions); err != nil {
		t.Fatalf("valid nested args rejected: %v", err)
	}
	badItem := contract.NewToolCall("c2", "demo", `{"steps":["a",3]}`)
	if err := validateCallArgs(badItem, definitions); err == nil || !strings.Contains(err.Error(), "steps[1]") {
		t.Fatalf("bad array item not caught: %v", err)
	}
	missingKey := contract.NewToolCall("c3", "demo", `{"options":{"count":1}}`)
	if err := validateCallArgs(missingKey, definitions); err == nil || !strings.Contains(err.Error(), `required key "mode"`) {
		t.Fatalf("missing nested required key not caught: %v", err)
	}
	badNestedType := contract.NewToolCall("c4", "demo", `{"options":{"mode":"fast","count":"two"}}`)
	if err := validateCallArgs(badNestedType, definitions); err == nil || !strings.Contains(err.Error(), "options.count") {
		t.Fatalf("bad nested type not caught: %v", err)
	}
}
