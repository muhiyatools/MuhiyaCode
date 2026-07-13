package orchestrator

import (
	"encoding/json"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestPrefixShapeDeterministicAndRegionAware(t *testing.T) {
	tools := []contract.ToolDefinition{{Type: "function", Function: contract.FunctionDefinition{Name: "one", Parameters: map[string]any{"type": "object", "properties": map[string]any{"z": map[string]any{"type": "string"}, "a": map[string]any{"type": "integer"}}}}}}
	first, err := NewPrefixShape("system", tools, 1, "model")
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewPrefixShape("system", tools, 1, "model")
	if err != nil {
		t.Fatal(err)
	}
	if reasons := CompareShape(first, second); len(reasons) != 0 {
		t.Fatalf("identical shape changed: %v", reasons)
	}
	second.ModelID = "other"
	reasons := CompareShape(first, second)
	if len(reasons) != 1 || reasons[0] != PrefixReasonModel {
		t.Fatalf("unexpected reasons: %v", reasons)
	}
	// RewriteVersion is diagnostic only; actual history bytes are the detector.
	second = first
	second.RewriteVersion++
	if reasons := CompareShape(first, second); len(reasons) != 0 {
		t.Fatalf("rewrite label changed shape: %v", reasons)
	}
}

func TestPrefixShapeHashesWireRepresentationsAndToolOrder(t *testing.T) {
	firstTool := contract.ToolDefinition{Type: "function", Function: contract.FunctionDefinition{Name: "one", Parameters: map[string]any{"type": "object"}}}
	secondTool := contract.ToolDefinition{Type: "function", Function: contract.FunctionDefinition{Name: "two", Parameters: map[string]any{"type": "object"}}}
	tools := []contract.ToolDefinition{firstTool, secondTool}

	shape, err := NewPrefixShape("quoted: \"value\"", tools, 0, "model")
	if err != nil {
		t.Fatal(err)
	}
	systemBytes, err := json.Marshal(contract.Message{Role: contract.RoleSystem, Content: "quoted: \"value\""})
	if err != nil {
		t.Fatal(err)
	}
	toolBytes, err := json.Marshal(struct {
		Tools      []contract.ToolDefinition `json:"tools"`
		ToolChoice string                    `json:"tool_choice"`
	}{tools, "auto"})
	if err != nil {
		t.Fatal(err)
	}
	if shape.SystemHash != hashBytes(systemBytes) || shape.ToolsHash != hashBytes(toolBytes) {
		t.Fatalf("shape did not hash wire bytes: %+v", shape)
	}

	reordered, err := NewPrefixShape("quoted: \"value\"", []contract.ToolDefinition{secondTool, firstTool}, 0, "model")
	if err != nil {
		t.Fatal(err)
	}
	reasons := CompareShape(shape, reordered)
	if len(reasons) != 1 || reasons[0] != PrefixReasonTools {
		t.Fatalf("tool wire-order change reasons = %v", reasons)
	}
}

func TestWirePrefixShapeDetectsSettledByteAndToolChoiceChanges(t *testing.T) {
	request := contract.ChatRequest{
		ModelID: "model", ToolChoice: "auto",
		Messages: []contract.Message{{Role: contract.RoleSystem, Content: "system"}, {Role: contract.RoleUser, Content: "settled"}},
		Tools:    []contract.ToolDefinition{{Function: contract.FunctionDefinition{Name: "tool"}}},
	}
	previous, err := NewWirePrefixShape(request, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	mutated := request
	mutated.Messages = append([]contract.Message(nil), request.Messages...)
	mutated.Messages[1].Content = "settleD"
	current, err := NewWirePrefixShape(mutated, len(request.Messages), 1)
	if err != nil {
		t.Fatal(err)
	}
	if reasons := CompareShape(previous, current); len(reasons) != 1 || reasons[0] != PrefixReasonHistory {
		t.Fatalf("settled mutation reasons=%v", reasons)
	}
	mutated = request
	mutated.ToolChoice = "none"
	current, err = NewWirePrefixShape(mutated, len(request.Messages), 1)
	if err != nil {
		t.Fatal(err)
	}
	if reasons := CompareShape(previous, current); len(reasons) != 1 || reasons[0] != PrefixReasonTools {
		t.Fatalf("tool-choice reasons=%v", reasons)
	}
}

func TestPrefixShapeCanonicalMapOrderIsStable(t *testing.T) {
	leftParameters := map[string]any{}
	leftParameters["z"] = map[string]any{"type": "string"}
	leftParameters["a"] = map[string]any{"type": "integer"}
	rightParameters := map[string]any{}
	rightParameters["a"] = map[string]any{"type": "integer"}
	rightParameters["z"] = map[string]any{"type": "string"}

	left, err := NewPrefixShape("system", []contract.ToolDefinition{{Function: contract.FunctionDefinition{Name: "tool", Parameters: leftParameters}}}, 0, "model")
	if err != nil {
		t.Fatal(err)
	}
	right, err := NewPrefixShape("system", []contract.ToolDefinition{{Function: contract.FunctionDefinition{Name: "tool", Parameters: rightParameters}}}, 0, "model")
	if err != nil {
		t.Fatal(err)
	}
	if reasons := CompareShape(left, right); len(reasons) != 0 {
		t.Fatalf("map insertion order changed shape: %v", reasons)
	}
}
