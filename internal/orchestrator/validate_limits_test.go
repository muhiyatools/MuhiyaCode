package orchestrator

import (
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestValidateCallArgsEnforcesBoundsAndFullSchemaKeywords(t *testing.T) {
	definitions := []contract.ToolDefinition{{Type: "function", Function: contract.FunctionDefinition{
		Name: "bounded",
		Parameters: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"count", "names"},
			"properties": map[string]any{
				"count": map[string]any{"type": "integer", "minimum": 1.0, "maximum": 3.0},
				"names": map[string]any{
					"type":     "array",
					"minItems": 1.0,
					"maxItems": 2.0,
					"items":    map[string]any{"type": "string", "minLength": 2.0},
				},
			},
		},
	}}}
	tests := []struct {
		name string
		raw  string
	}{
		{"fractional integer", `{"count":1.5,"names":["ok"]}`},
		{"below minimum", `{"count":0,"names":["ok"]}`},
		{"too many items", `{"count":1,"names":["ok","yes","no"]}`},
		{"short string", `{"count":1,"names":["x"]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			call := contract.NewToolCall("call", "bounded", test.raw)
			if err := validateCallArgs(call, definitions); err == nil {
				t.Fatalf("expected schema rejection for %s", test.raw)
			}
		})
	}
	valid := contract.NewToolCall("call", "bounded", `{"count":2,"names":["ok","yes"]}`)
	if err := validateCallArgs(valid, definitions); err != nil {
		t.Fatalf("valid bounded call rejected: %v", err)
	}
}

func TestValidateCallArgsRejectsResourceExhaustionInputs(t *testing.T) {
	definition := []contract.ToolDefinition{{Function: contract.FunctionDefinition{
		Name:       "bounded",
		Parameters: map[string]any{"type": "object"},
	}}}
	oversized := `{"value":"` + strings.Repeat("x", maxToolArgumentsBytes) + `"}`
	if err := validateCallArgs(contract.NewToolCall("large", "bounded", oversized), definition); err == nil ||
		!strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("oversized arguments not rejected safely: %v", err)
	}
	deep := `{"value":` + strings.Repeat("[", maxToolArgumentDepth) + `0` +
		strings.Repeat("]", maxToolArgumentDepth) + `}`
	if err := validateCallArgs(contract.NewToolCall("deep", "bounded", deep), definition); err == nil ||
		!strings.Contains(err.Error(), "nesting depth") {
		t.Fatalf("deep arguments not rejected safely: %v", err)
	}
}

func TestValidateCallArgsFailsClosedOnInvalidAdvertisedSchema(t *testing.T) {
	definition := []contract.ToolDefinition{{Function: contract.FunctionDefinition{
		Name: "invalid",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"value": map[string]any{"type": 7}},
		},
	}}}
	err := validateCallArgs(contract.NewToolCall("call", "invalid", `{"value":"x"}`), definition)
	if err == nil || !strings.Contains(err.Error(), "advertised tool schema is invalid") {
		t.Fatalf("invalid schema should fail closed before dispatch: %v", err)
	}
}

func FuzzValidateCallArgsNeverPanics(f *testing.F) {
	definitions := []contract.ToolDefinition{{Function: contract.FunctionDefinition{
		Name: "fuzz",
		Parameters: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"value": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}},
			},
		},
	}}}
	for _, seed := range []string{`{}`, `{"value":[1,2]}`, `{"value":[`, `null`, `[]`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > maxToolArgumentsBytes+1 {
			t.Skip()
		}
		_ = validateCallArgs(contract.NewToolCall("fuzz-call", "fuzz", raw), definitions)
	})
}
