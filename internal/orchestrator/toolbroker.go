package orchestrator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
)

type ToolDescriptor struct {
	CanonicalName     string   `json:"name"`
	Namespace         string   `json:"namespace"`
	OneLine           string   `json:"description"`
	Keywords          []string `json:"keywords,omitempty"`
	Risk              string   `json:"risk"`
	ReadOnly          bool     `json:"readOnly"`
	SchemaHash        string   `json:"schemaHash"`
	Availability      string   `json:"availability"`
	ServerFingerprint string   `json:"serverFingerprint,omitempty"`
}

func (e *Engine) brokerDescriptors(query string, limit int) []ToolDescriptor {
	if limit <= 0 || limit > 20 {
		limit = 12
	}
	queryTerms := tokenizeTerms(query)
	type scored struct {
		descriptor ToolDescriptor
		score      int
	}
	rows := []scored{}
	for _, definition := range e.brokerableDefinitions() {
		name := definition.Function.Name
		if name == "discover_tools" || name == "invoke_tool" || hasDefinition(e.sessionDefinitions(), name) {
			continue
		}
		descriptor := descriptorFor(definition, e.registry.DeclaresReadOnly(name))
		if fingerprint, availability := e.registry.ToolIdentity(name); fingerprint != "" || availability != "" {
			descriptor.ServerFingerprint = fingerprint
			if availability != "" {
				descriptor.Availability = availability
			}
		}
		haystack := strings.ToLower(name + " " + definition.Function.Description)
		score := 0
		for _, term := range queryTerms {
			if strings.Contains(strings.ToLower(name), term) {
				score += 4
			} else if strings.Contains(haystack, term) {
				score++
			}
		}
		if len(queryTerms) == 0 || score > 0 {
			rows = append(rows, scored{descriptor, score})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].score != rows[j].score {
			return rows[i].score > rows[j].score
		}
		return rows[i].descriptor.CanonicalName < rows[j].descriptor.CanonicalName
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	result := make([]ToolDescriptor, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.descriptor)
	}
	return result
}

func descriptorFor(definition contract.ToolDefinition, declaredReadOnly bool) ToolDescriptor {
	name := definition.Function.Name
	readOnly := declaredReadOnly || readonlyTools[name]
	risk := "read"
	if isMutation(name) {
		risk = "mutation"
	} else if name == "run_shell" || strings.Contains(name, "shell") {
		risk = "process"
	}
	description := strings.Join(strings.Fields(definition.Function.Description), " ")
	if len(description) > 160 {
		description = description[:160]
	}
	namespace := "builtin"
	if strings.HasPrefix(name, "mcp__") {
		namespace = "mcp"
	}
	if strings.Contains(name, "memory") {
		namespace = "memory"
	}
	if name == "read_skill" {
		namespace = "skill"
	}
	return ToolDescriptor{CanonicalName: name, Namespace: namespace, OneLine: description,
		Keywords: sortedUnique(tokenizeTerms(strings.ReplaceAll(name, "_", " ")), 8),
		Risk:     risk, ReadOnly: readOnly, SchemaHash: toolSchemaHash(definition), Availability: "ready"}
}

func toolSchemaHash(definition contract.ToolDefinition) string {
	encoded, _ := json.Marshal(definition)
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

func (e *Engine) discoverTools(raw json.RawMessage) (string, error) {
	var input struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := decodeToolArgs(raw, &input); err != nil {
		return "", err
	}
	descriptors := e.brokerDescriptors(input.Query, input.Limit)
	encoded, err := json.Marshal(map[string]any{"tools": descriptors, "count": len(descriptors)})
	return string(encoded), err
}

func (e *Engine) resolveBrokerCall(source contract.ToolCall) (contract.ToolCall, []contract.ToolDefinition, error) {
	var input struct {
		Name              string          `json:"name"`
		SchemaHash        string          `json:"schemaHash"`
		ServerFingerprint string          `json:"serverFingerprint,omitempty"`
		Arguments         json.RawMessage `json:"arguments"`
	}
	if err := decodeToolArgs(json.RawMessage(source.ArgumentsJSON()), &input); err != nil {
		return contract.ToolCall{}, nil, err
	}
	if input.Name == "invoke_tool" || input.Name == "discover_tools" {
		return contract.ToolCall{}, nil, errors.New("broker recursion is not allowed")
	}
	full := e.brokerableDefinitions()
	var matched *contract.ToolDefinition
	for i := range full {
		if full[i].Function.Name == input.Name {
			matched = &full[i]
			break
		}
	}
	if matched == nil {
		return contract.ToolCall{}, nil, fmt.Errorf("deferred tool %s is unknown or offline", input.Name)
	}
	if input.SchemaHash == "" || input.SchemaHash != toolSchemaHash(*matched) {
		return contract.ToolCall{}, nil, fmt.Errorf("deferred tool %s schema is stale; discover it again", input.Name)
	}
	if fingerprint, _ := e.registry.ToolIdentity(input.Name); fingerprint != "" && input.ServerFingerprint != fingerprint {
		return contract.ToolCall{}, nil, fmt.Errorf("deferred tool %s server identity is stale; discover it again", input.Name)
	}
	if len(input.Arguments) == 0 {
		input.Arguments = json.RawMessage("{}")
	}
	call := contract.NewToolCall(source.ID, input.Name, string(input.Arguments))
	return call, full, nil
}
