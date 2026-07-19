package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/instructions"
)

// readPlanTool (feature 012 R-D7, contracts/phase-handoff.md PH-3) is the
// read-only registry tool that returns the session's rendered execution plan.
// Served by the harness from engine state — never a filesystem read — so a
// subagent reaches its assigned phase without the outside-workspace approval
// prompt that ~/.muhiya/sessions/<id>/plan.md would trigger.
type readPlanTool struct {
	engine *Engine
}

func (t readPlanTool) Definition() contract.ToolDefinition {
	return contract.ToolDefinition{Type: "function", Function: contract.FunctionDefinition{
		Name:        "read_plan",
		Description: instructions.ToolReadPlanDescription,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"section": map[string]any{"type": "string", "description": "Optional: 'steps' returns only the implementation steps"},
			},
			"additionalProperties": false,
		},
	}}
}

func (t readPlanTool) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Section string `json:"section"`
	}
	_ = json.Unmarshal(raw, &input)
	plan := t.engine.CurrentPlan()
	if len(plan.Steps) == 0 && strings.TrimSpace(plan.Note) == "" {
		return "No plan exists yet for this session.", nil
	}
	if strings.EqualFold(strings.TrimSpace(input.Section), "steps") {
		lines := []string{"Implementation steps:"}
		for _, step := range plan.Steps {
			mark := " "
			if step.Status == contract.PlanCompleted {
				mark = "x"
			}
			lines = append(lines, fmt.Sprintf("- [%s] %s (%s)", mark, step.Title, step.Status))
		}
		return strings.Join(lines, "\n"), nil
	}
	return t.engine.executionPlanMarkdown(plan), nil
}
