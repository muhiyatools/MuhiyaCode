package orchestrator

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/muhiya/muhiyacode/internal/contract"
)

const (
	maxToolArgumentsBytes = 2 << 20
	maxToolArgumentDepth  = 64
	maxToolArgumentTokens = 200_000
	maxToolSchemaBytes    = 1 << 20
	maxCompiledSchemas    = 256
)

var toolSchemaCache = struct {
	sync.Mutex
	entries map[[sha256.Size]byte]*jsonschema.Resolved
}{entries: make(map[[sha256.Size]byte]*jsonschema.Resolved)}

// validateCallArgs is the fail-closed boundary between model output and tool
// dispatch. It validates bounded JSON input against the exact schema advertised
// to the model. Schema compilation is cached because the advertised tool set is
// stable across most turns.
func validateCallArgs(call contract.ToolCall, definitions []contract.ToolDefinition) error {
	raw := call.ArgumentsJSON()
	if raw == "" {
		raw = "{}"
	}
	args, err := decodeBoundedToolArguments([]byte(raw))
	if err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return fmt.Errorf("%s: %s", call.ToolName(), truncationRecoveryBody(call.ToolName()))
		}
		return fmt.Errorf("%s: arguments were not valid bounded JSON (%v)", call.ToolName(), err)
	}
	definition := findDefinition(definitions, call.ToolName())
	if definition == nil {
		return nil // the registry reports unknown tools without dispatching them
	}
	resolved, err := compiledToolSchema(definition.Function.Parameters)
	if err != nil {
		return fmt.Errorf("%s: advertised tool schema is invalid (%v)", call.ToolName(), err)
	}
	if err := resolved.Validate(args); err != nil {
		return fmt.Errorf("%s: arguments do not match the advertised schema (%v)", call.ToolName(), err)
	}
	if err := rejectEmptyRequiredStrings(args, definition.Function.Parameters, ""); err != nil {
		return fmt.Errorf("%s: %w", call.ToolName(), err)
	}
	return nil
}

func decodeBoundedToolArguments(raw []byte) (map[string]any, error) {
	if len(raw) > maxToolArgumentsBytes {
		return nil, fmt.Errorf("arguments exceed %d-byte limit", maxToolArgumentsBytes)
	}
	if err := validateJSONComplexity(raw); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var args map[string]any
	if err := decoder.Decode(&args); err != nil {
		return nil, err
	}
	if args == nil {
		return nil, errors.New("arguments must be a JSON object")
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}
	return args, nil
}

func validateJSONComplexity(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	depth := 0
	tokens := 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			if depth != 0 {
				return io.ErrUnexpectedEOF
			}
			return nil
		}
		if err != nil {
			return normalizeJSONDecodeError(err)
		}
		tokens++
		if tokens > maxToolArgumentTokens {
			return fmt.Errorf("arguments exceed %d-token structural limit", maxToolArgumentTokens)
		}
		delim, ok := token.(json.Delim)
		if !ok {
			continue
		}
		switch delim {
		case '{', '[':
			depth++
			if depth > maxToolArgumentDepth {
				return fmt.Errorf("arguments exceed maximum nesting depth %d", maxToolArgumentDepth)
			}
		case '}', ']':
			depth--
		}
	}
}

func normalizeJSONDecodeError(err error) error {
	if errors.Is(err, io.ErrUnexpectedEOF) || err.Error() == "unexpected EOF" {
		return io.ErrUnexpectedEOF
	}
	return err
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("arguments contain more than one JSON value")
	}
	return err
}

func compiledToolSchema(parameters map[string]any) (*jsonschema.Resolved, error) {
	raw, err := json.Marshal(parameters)
	if err != nil {
		return nil, fmt.Errorf("encode schema: %w", err)
	}
	if len(raw) > maxToolSchemaBytes {
		return nil, fmt.Errorf("schema exceeds %d-byte limit", maxToolSchemaBytes)
	}
	key := sha256.Sum256(raw)
	toolSchemaCache.Lock()
	resolved := toolSchemaCache.entries[key]
	toolSchemaCache.Unlock()
	if resolved != nil {
		return resolved, nil
	}

	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("decode schema: %w", err)
	}
	resolved, err = schema.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("resolve schema: %w", err)
	}
	toolSchemaCache.Lock()
	if len(toolSchemaCache.entries) >= maxCompiledSchemas {
		toolSchemaCache.entries = make(map[[sha256.Size]byte]*jsonschema.Resolved)
	}
	toolSchemaCache.entries[key] = resolved
	toolSchemaCache.Unlock()
	return resolved, nil
}

func rejectEmptyRequiredStrings(value any, schema map[string]any, path string) error {
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	properties, _ := schema["properties"].(map[string]any)
	for _, key := range stringListFromAny(schema["required"]) {
		propertySchema, _ := properties[key].(map[string]any)
		if str, ok := object[key].(string); ok && str == "" && propertySchema["type"] == "string" {
			return fmt.Errorf("required string field %s is empty", joinJSONPath(path, key))
		}
	}
	for key, nested := range object {
		propertySchema, _ := properties[key].(map[string]any)
		switch typed := nested.(type) {
		case map[string]any:
			if err := rejectEmptyRequiredStrings(typed, propertySchema, joinJSONPath(path, key)); err != nil {
				return err
			}
		case []any:
			itemSchema, _ := propertySchema["items"].(map[string]any)
			for index, item := range typed {
				itemPath := fmt.Sprintf("%s[%d]", joinJSONPath(path, key), index)
				if err := rejectEmptyRequiredStrings(item, itemSchema, itemPath); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func joinJSONPath(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}

func findDefinition(definitions []contract.ToolDefinition, name string) *contract.ToolDefinition {
	for index := range definitions {
		if definitions[index].Function.Name == name {
			return &definitions[index]
		}
	}
	return nil
}

func stringListFromAny(value any) []string {
	switch values := value.(type) {
	case []any:
		result := make([]string, 0, len(values))
		for _, item := range values {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result
	case []string:
		return values
	default:
		return nil
	}
}
