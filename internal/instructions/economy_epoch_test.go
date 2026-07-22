package instructions

import (
	"strings"
	"testing"
)

func TestLeanPromptRulePreservationMap(t *testing.T) {
	mappings := []struct {
		rule, target, fragment string
	}{
		{"identity", PromptLeanBody, "You are MuhiyaCode"},
		{"priority", PromptLeanBody, "Obey safety, then the user's request"},
		{"workspace containment", PromptLeanBody, "Stay inside the workspace"},
		{"secret protection", PromptLeanBody, "Never expose secrets"},
		{"inspect once/reuse", PromptLeanBody, "reuse valid evidence"},
		{"preserve user work", PromptLeanBody, "Never discard unrelated changes"},
		{"surgical edits", PromptLeanBody, "Use surgical edits"},
		{"structured calls", PromptLeanBody, "Use structured tools"},
		{"deferred broker", PromptLeanBody, "returned schema hash"},
		{"minimal verification", PromptLeanBody, "smallest existing project check"},
		{"observed results", PromptLeanBody, "Report only observed results"},
		{"direct communication", PromptLeanBody, "Be direct and compact"},
		{"immutable model", PromptLeanBody, "model and upstream are fixed"},
		{"exact edit matching", ToolEditFileDescription, "oldString must match exactly once"},
		{"whole-file replacement", ToolWriteFileDescription, "only after reading it"},
		{"failed-call recovery", ToolEditFileDescription, "nearest region"},
		{"memory safety", ToolSaveMemoryDescription, "Never save task progress"},
		{"skill timing", ToolReadSkillDescription, "BEFORE starting work"},
	}
	for _, mapping := range mappings {
		if !strings.Contains(mapping.target, mapping.fragment) {
			t.Errorf("rule %q lost from target: missing %q", mapping.rule, mapping.fragment)
		}
	}
	if len(PromptLeanBody) > 3_000 {
		t.Fatalf("lean universal prompt is %d chars, over 3000", len(PromptLeanBody))
	}
}
