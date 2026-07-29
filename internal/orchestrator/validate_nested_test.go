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
	if err := validateCallArgs(badItem, definitions); err == nil || !strings.Contains(err.Error(), "/properties/steps/items") {
		t.Fatalf("bad array item not caught: %v", err)
	}
	missingKey := contract.NewToolCall("c3", "demo", `{"options":{"count":1}}`)
	if err := validateCallArgs(missingKey, definitions); err == nil || !strings.Contains(err.Error(), `"mode"`) {
		t.Fatalf("missing nested required key not caught: %v", err)
	}
	badNestedType := contract.NewToolCall("c4", "demo", `{"options":{"mode":"fast","count":"two"}}`)
	if err := validateCallArgs(badNestedType, definitions); err == nil || !strings.Contains(err.Error(), "/options/properties/count") {
		t.Fatalf("bad nested type not caught: %v", err)
	}
}

func TestValidationTightening(t *testing.T) {
	definitions := []contract.ToolDefinition{
		{
			Function: contract.FunctionDefinition{
				Name: "edit_file",
				Parameters: map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []string{"path", "oldString", "newString"},
					"properties": map[string]any{
						"path":      map[string]any{"type": "string"},
						"oldString": map[string]any{"type": "string"},
						"newString": map[string]any{"type": "string"},
					},
				},
			},
		},
	}
	tests := []struct {
		name string
		args string
		err  string
	}{
		{"Unknown property", `{"path": "a", "oldString": "b", "newString": "c", "unknown": "d"}`, `additional properties ["unknown"]`},
		{"Empty string in required field", `{"path": "a", "oldString": "", "newString": "c"}`, `required string field oldString is empty`},
		{"Valid call", `{"path": "a", "oldString": "b", "newString": "c"}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := contract.NewToolCall("1", "edit_file", tt.args)
			err := validateCallArgs(call, definitions)
			if tt.err == "" {
				if err != nil {
					t.Errorf("expected success, got %v", err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Errorf("expected error %q, got %v", tt.err, err)
				}
			}
		})
	}
}
