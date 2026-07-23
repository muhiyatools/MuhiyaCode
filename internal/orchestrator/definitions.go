package orchestrator

import (
	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/instructions"
)

// sessionDefinitions returns the full, session-stable tool schema set. It never
// varies by task class or effort, so the tool block stays byte-identical across
// turns and stays inside the provider's cached prefix. Per-turn permission comes
// at execution time from the liveness guards, never by adding or removing
// schemas.
func (e *Engine) sessionDefinitions() []contract.ToolDefinition {
	definitions := e.registry.BaseDefinitions(nil)
	definitions = append(definitions, askUserDefinition(), proposeChangesDefinition(), saveMemoryDefinition(), recallMemoryDefinition(), editMemoryDefinition())
	// read_skill is advertised only when the session actually catalogued skills:
	// a tool the model can never use successfully is pure prefix weight, and its
	// presence would invite calls that can only fail.
	if e.skills.Len() > 0 {
		definitions = append(definitions, readSkillDefinition())
	}
	definitions = append(definitions, e.registry.MCPDefinitions(nil)...)
	return definitions
}

// SessionToolNames exposes sessionDefinitions()'s full name set (feature 010
// US5, WI-1/WI-3): the registry's tools plus the synthetic session tools
// (ask_user, propose_changes, the memory trio, and read_skill when skills
// exist) plus any registered mcp__ tools. It exists so the cross-package
// wiring-inventory guard test (internal/tui/wiring_inventory_test.go)
// enumerates the REAL live tool surface instead of a hand-maintained mirror —
// any future change to sessionDefinitions (a tool added, renamed, or
// removed) is reflected here automatically, with no second place to update.
func (e *Engine) SessionToolNames() []string {
	return toolNames(e.sessionDefinitions())
}

func askUserDefinition() contract.ToolDefinition {
	choice := map[string]any{"type": "object", "properties": map[string]any{"label": map[string]any{"type": "string"}, "description": map[string]any{"type": "string"}, "recommended": map[string]any{"type": "boolean"}}, "required": []string{"label"}}
	question := map[string]any{"type": "object", "properties": map[string]any{"question": map[string]any{"type": "string"}, "choices": map[string]any{"type": "array", "minItems": 2, "maxItems": 5, "items": choice}}, "required": []string{"question", "choices"}}
	return definition("ask_user", instructions.ToolAskUserDescription, map[string]any{"questions": map[string]any{"type": "array", "minItems": 1, "maxItems": 3, "items": question}}, []string{"questions"})
}

func proposeChangesDefinition() contract.ToolDefinition {
	file := map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}, "reason": map[string]any{"type": "string"}, "risk": map[string]any{"type": "string", "enum": []string{"low", "medium", "high"}}}, "required": []string{"path", "reason", "risk"}}
	return definition("propose_changes", instructions.ToolProposeChangesDescription, map[string]any{"summary": map[string]any{"type": "string"}, "files": map[string]any{"type": "array", "minItems": 1, "maxItems": 20, "items": file}, "testPlan": map[string]any{"type": "string"}, "estimatedSteps": map[string]any{"type": "integer", "minimum": 1, "maximum": 40}}, []string{"summary", "files", "testPlan", "estimatedSteps"})
}

func definition(name, description string, properties map[string]any, required []string) contract.ToolDefinition {
	parameters := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		parameters["required"] = required
	}
	return contract.ToolDefinition{Type: "function", Function: contract.FunctionDefinition{Name: name, Description: description, Parameters: parameters}}
}

func hasDefinition(definitions []contract.ToolDefinition, name string) bool {
	for _, definition := range definitions {
		if definition.Function.Name == name {
			return true
		}
	}
	return false
}
