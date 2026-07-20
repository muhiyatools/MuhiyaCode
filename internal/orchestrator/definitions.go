package orchestrator

import (
	"sort"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/instructions"
)

// sessionDefinitions returns the full, session-stable tool schema set. It never
// varies by task class or effort, so the tool block stays byte-identical across
// turns and remains inside DeepSeek's cached prefix. Per-turn permission comes
// from the task brief (e.g. agents<=0) and is enforced at execution time, not
// by adding or removing schemas.
func (e *Engine) sessionDefinitions() []contract.ToolDefinition {
	definitions := e.registry.BaseDefinitions(nil)
	definitions = append(definitions, askUserDefinition(), proposeChangesDefinition(), saveMemoryDefinition(), recallMemoryDefinition(), editMemoryDefinition())
	definitions = append(definitions, runSubagentDefinition(e.subagentSpecs()))
	definitions = append(definitions, e.registry.MCPDefinitions(nil)...)
	return definitions
}

// SessionToolNames exposes sessionDefinitions()'s full name set (feature 010
// US5, WI-1/WI-3): the registry's tools plus all eight synthetic main-loop
// tools (incl. recall_memory and edit_memory) plus any registered mcp__ tools. It exists so the cross-package
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

// runSubagentDefinition builds the run_subagent tool the model sees. The
// description is deliberately rich (feature 008 DG-4, Reasonix task-tool pattern):
// tool descriptions are the strongest delegation-inducement surface, so it carries
// when-to-use criteria, a worked example, and per-kind selection guidance. The text
// is deterministic (sorted enum, constant strings) so the tools serialization stays
// byte-stable across turns (one recorded upgrade epoch, contract DG-5). Feature 010
// US3 fix (c): the taskDescription now cites the SAME three canonical report-format
// field lists the subagent's own handoff contract renders, instead of a paraphrase.
func runSubagentDefinition(specs map[string]subagentSpec) contract.ToolDefinition {
	names := make([]string, 0, len(specs))
	for name := range specs {
		names = append(names, name)
	}
	sort.Strings(names)
	return definition("run_subagent", instructions.ToolRunSubagentDescription, map[string]any{
		"agent": map[string]any{"type": "string", "enum": names, "description": "explore | plan | review | general — pick by the work's shape, not by habit."},
		"title": map[string]any{"type": "string", "description": "Short human-readable label shown in the transcript."},
		"task":  map[string]any{"type": "string", "description": instructions.ToolRunSubagentTaskPropertyDescription},
	}, []string{"agent", "task"})
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
