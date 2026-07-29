package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func (e *Engine) executeOne(ctx context.Context, call contract.ToolCall, definitions []contract.ToolDefinition) contract.ToolResult {
	name := call.ToolName()
	var output string
	var err error
	switch name {
	case "ask_user":
		output, err = e.askUser(ctx, json.RawMessage(call.ArgumentsJSON()))
	case "propose_changes":
		output, err = e.proposeChanges(ctx, json.RawMessage(call.ArgumentsJSON()))
	case "save_memory":
		// Feature 008 US5 (T029): first-class durable-memory save. The
		// definition, the approval-gate parity, and the append helper live in
		// memory.go.
		output, err = e.saveMemory(ctx, json.RawMessage(call.ArgumentsJSON()))
	case "recall_memory":
		// Experience Overhaul B3: read one topic file from the per-project store.
		// Read-only, so no approval gate; the store dir is engine-managed.
		output, err = e.recallMemory(ctx, json.RawMessage(call.ArgumentsJSON()))
	case "edit_memory":
		// Memory Parity N2: update, delete, or consolidate one saved entry. The
		// definition, matcher, approval-gate parity, and rewrite live in memory.go.
		output, err = e.editMemory(ctx, json.RawMessage(call.ArgumentsJSON()))
	case "read_skill":
		// 013 US1: load one advertised skill's instructions by name. Read-only and
		// catalog-bounded, so no approval gate; skills_tool.go owns the resolution,
		// the size bound, and the already-provided dedupe.
		output, err = e.readSkillTool(ctx, json.RawMessage(call.ArgumentsJSON()))
	case "activate_tools":
		output, err = e.activateTools(ctx, json.RawMessage(call.ArgumentsJSON()))
	case "integration_tools":
		output, err = e.integrationTools(ctx, json.RawMessage(call.ArgumentsJSON()))
	default:
		allowed := make(map[string]bool)
		for _, definition := range definitions {
			allowed[definition.Function.Name] = true
		}
		return e.registry.Execute(ctx, name, json.RawMessage(call.ArgumentsJSON()), allowed)
	}
	return contract.AdaptToolResult(output, err)
}

// repairJSONStringEscapes doubles invalid backslash escapes inside JSON
// string values (\s, \d, \w, Windows paths… — regex/path text the model wrote
// verbatim, a live update_plan failure: "invalid character 's' in string
// escape code"). Applied ONLY after Unmarshal fails; valid JSON is untouched.
func repairJSONStringEscapes(raw []byte) []byte {
	out := make([]byte, 0, len(raw)+8)
	inString := false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if !inString {
			if c == '"' {
				inString = true
			}
			out = append(out, c)
			continue
		}
		if c == '\\' {
			if i+1 < len(raw) {
				next := raw[i+1]
				switch next {
				case '"', '\\', '/', 'b', 'f', 'n', 'r', 't', 'u':
					out = append(out, c, next)
				default:
					// Invalid JSON escape: keep the author's literal backslash.
					out = append(out, '\\', '\\', next)
				}
				i++
				continue
			}
			out = append(out, '\\', '\\')
			continue
		}
		if c == '"' {
			inString = false
		}
		out = append(out, c)
	}
	return out
}

// decodeToolArgs unmarshals tool arguments with a one-shot invalid-escape
// rescue so a regex like \d+ inside a string argument does not hard-fail the
// whole tool call.
func decodeToolArgs(raw json.RawMessage, v any) error {
	err := json.Unmarshal(raw, v)
	if err == nil {
		return nil
	}
	if repaired := repairJSONStringEscapes(raw); json.Unmarshal(repaired, v) == nil {
		return nil
	}
	return err
}

func (e *Engine) askUser(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Questions []contract.Question `json:"questions"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	if len(input.Questions) == 0 || len(input.Questions) > 3 || e.callbacks.Ask == nil {
		return "", errors.New("ask_user requires 1-3 questions and an interactive client")
	}
	// Each question must carry at least two choices (the tool schema requires it,
	// but the payload is model-controlled and smaller models sometimes omit them).
	// Reject a choiceless question with a tool error the model can act on, rather
	// than passing it to the modal where indexing an empty choice slice would panic.
	for i, q := range input.Questions {
		if len(q.Choices) < 2 {
			return "", fmt.Errorf("ask_user question %d (%q) must include at least 2 choices", i+1, q.Question)
		}
	}
	answers, err := e.callbacks.Ask(ctx, input.Questions)
	if err != nil {
		return "", err
	}
	return encodeAnswers(answers), nil
}

func (e *Engine) proposeChanges(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Summary        string `json:"summary"`
		EstimatedSteps int    `json:"estimatedSteps"`
		Files          []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	paths := make([]string, 0, len(input.Files))
	for _, file := range input.Files {
		paths = append(paths, file.Path)
	}
	if e.callbacks.Ask == nil {
		if err := e.setPlanApproval(ctx, false, input.Summary, paths); err != nil {
			return "", err
		}
		return `{"verdict":"rejected","instruction":"Interactive approval is unavailable. Do not mutate; report the proposed scope and wait."}`, nil
	}
	answers, err := e.callbacks.Ask(ctx, []contract.Question{{Question: fmt.Sprintf("Approve this change plan? (%d files, ~%d steps)\n%s", len(input.Files), input.EstimatedSteps, input.Summary), Choices: []contract.QuestionChoice{{Label: "Proceed", Description: "Apply and verify the plan", Recommended: true}, {Label: "Proceed carefully", Description: "Minimize each edit and stop on surprises"}, {Label: "Stop", Description: "Do not edit"}}}})
	if err != nil {
		return "", err
	}
	index := 0
	if len(answers) > 0 {
		index = answers[0].Index
	}
	if index == 2 {
		if err := e.setPlanApproval(ctx, false, input.Summary, paths); err != nil {
			return "", err
		}
		return `{"verdict":"rejected","instruction":"Do not dispatch this change; summarize the plan and wait."}`, nil
	}
	if index == 1 {
		if err := e.setPlanApproval(ctx, true, input.Summary, paths); err != nil {
			return "", err
		}
		return `{"verdict":"approved_with_caution","instruction":"Dispatch the smallest change that works, and check the report for each file."}`, nil
	}
	if err := e.setPlanApproval(ctx, true, input.Summary, paths); err != nil {
		return "", err
	}
	return `{"verdict":"approved","instruction":"Apply only the approved file scope, verify it, and report evidence."}`, nil
}
