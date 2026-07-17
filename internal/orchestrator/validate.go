package orchestrator

import (
	"encoding/json"
	"fmt"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// validateCallArgs (H1) checks the supplied raw JSON against the matching
// tool definition's parameter schema. Implements the four checks the
// plan requires (object parse, required keys, primitive type, enum) without
// pulling in a JSON-Schema library. ~80 lines covers the four cases.
func validateCallArgs(call contract.ToolCall, definitions []contract.ToolDefinition) error {
	raw := call.ArgumentsJSON()
	if raw == "" {
		raw = "{}"
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return fmt.Errorf("%s: arguments were not valid JSON (%v)", call.ToolName(), err)
	}
	schema := findDefinition(definitions, call.ToolName())
	if schema == nil {
		return nil // unknown to the orchestrator; the registry will surface it
	}
	parameters, _ := schema.Function.Parameters["properties"].(map[string]any)
	required := stringListFromAny(schema.Function.Parameters["required"])
	for _, key := range required {
		if _, present := args[key]; !present {
			return fmt.Errorf("%s: missing required field %q (per definition)", call.ToolName(), key)
		}
	}
	for propName, propSchemaAny := range parameters {
		propSchema, ok := propSchemaAny.(map[string]any)
		if !ok {
			continue
		}
		if value, present := args[propName]; present {
			if err := checkPrimitiveType(call.ToolName(), propName, value, propSchema); err != nil {
				return err
			}
		}
	}
	return nil
}

func findDefinition(definitions []contract.ToolDefinition, name string) *contract.ToolDefinition {
	for index, definition := range definitions {
		if definition.Function.Name == name {
			return &definitions[index]
		}
	}
	return nil
}

func stringListFromAny(value any) []string {
	if values, ok := value.([]any); ok {
		result := make([]string, 0, len(values))
		for _, item := range values {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result
	}
	if values, ok := value.([]string); ok {
		return values
	}
	return nil
}

func checkPrimitiveType(tool, key string, value any, schema map[string]any) error {
	want := schema["type"]
	if allowed, ok := schema["enum"].([]any); ok {
		for _, candidate := range allowed {
			if enumEqual(candidate, value) {
				return nil
			}
		}
		return fmt.Errorf("%s: field %q must be one of %v", tool, key, allowed)
	}
	if want == nil {
		return nil
	}
	switch want {
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s: field %q must be a string, got %T", tool, key, value)
		}
	case "number", "integer":
		if _, ok := value.(float64); !ok {
			return fmt.Errorf("%s: field %q must be a number, got %T", tool, key, value)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s: field %q must be a boolean, got %T", tool, key, value)
		}
	case "array":
		if _, ok := value.([]any); !ok {
			return fmt.Errorf("%s: field %q must be an array, got %T", tool, key, value)
		}
	case "object":
		if _, ok := value.(map[string]any); !ok {
			return fmt.Errorf("%s: field %q must be an object, got %T", tool, key, value)
		}
	}
	return nil
}

// enumEqual (B8/T028) compares a JSON enum element against a decoded argument
// value BY TYPE: numbers as float64, strings as strings, bools as bools. Both
// sides of a JSON number decode to float64, so a numeric enum like {1,2,3}
// matches a numeric argument. The previous code stringified non-strings and
// compared against the raw element (a float64), so numeric/boolean enums
// rejected every value — including valid ones.
func enumEqual(candidate, value any) bool {
	switch c := candidate.(type) {
	case string:
		v, ok := value.(string)
		return ok && v == c
	case float64:
		v, ok := value.(float64)
		return ok && v == c
	case bool:
		v, ok := value.(bool)
		return ok && v == c
	default:
		return candidate == value
	}
}
