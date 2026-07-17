package orchestrator

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func planMarkdown(plan contract.Plan) string {
	return renderExecutionPlan(plan, nil, "")
}

func (e *Engine) executionPlanMarkdown(plan contract.Plan) string {
	var findings []KnowledgeFact
	if e.knowledge != nil {
		findings = e.knowledge.Findings(12)
	}
	e.mu.Lock()
	superseded := e.supersededPlan
	e.mu.Unlock()
	if len(findings) == 0 && e.Lifecycle().IsDegraded(contract.LifecycleResearch) {
		findings = []KnowledgeFact{{Title: "Research degraded", Text: "Delegated research was unavailable; the main agent must ground every plan step through direct read-only investigation."}}
	}
	return renderExecutionPlan(plan, findings, superseded)
}

func renderExecutionPlan(plan contract.Plan, findings []KnowledgeFact, superseded string) string {
	lines := []string{"# Execution Plan", "", "## Research Findings"}
	if len(findings) == 0 {
		lines = append(lines, "- No pipeline research findings attached (legacy/direct plan).")
	} else {
		for i, finding := range findings {
			lines = append(lines, fmt.Sprintf("- [F%d] **%s** — %s", i+1, finding.Title, contract.Digest(finding.Text, 500)))
		}
	}
	lines = append(lines, "", "## Implementation Steps")
	for _, step := range plan.Steps {
		mark := " "
		if step.Status == contract.PlanCompleted {
			mark = "x"
		}
		lines = append(lines, fmt.Sprintf("- [%s] %s (%s)", mark, step.Title, step.Status))
	}
	verification := labeledPlanSection(plan.Note, "verification", "risks")
	if verification == "" {
		verification = "- Run the observable acceptance check named by every implementation step."
	}
	risks := labeledPlanSection(plan.Note, "risks")
	if risks == "" {
		risks = "- No additional risks recorded."
	}
	lines = append(lines, "", "## Verification", verification, "", "## Risks", risks)
	if strings.TrimSpace(superseded) != "" {
		lines = append(lines, "", "## Superseded Prior Plan", "", "The following prior plan was explicitly superseded by this pipeline run:", "", "```markdown", strings.TrimSpace(superseded), "```")
	}
	return strings.Join(lines, "\n") + "\n"
}

func labeledPlanSection(note, label string, stops ...string) string {
	if strings.TrimSpace(note) == "" {
		return ""
	}
	lower := strings.ToLower(note)
	start := strings.Index(lower, strings.ToLower(label)+":")
	if start < 0 {
		return ""
	}
	start += len(label) + 1
	end := len(note)
	for _, stop := range stops {
		if index := strings.Index(strings.ToLower(note[start:]), strings.ToLower(stop)+":"); index >= 0 && start+index < end {
			end = start + index
		}
	}
	value := strings.TrimSpace(note[start:end])
	if value == "" {
		return ""
	}
	return value
}

func encodeAnswers(answers []contract.Answer) string {
	values := make([]map[string]any, 0, len(answers))
	for _, answer := range answers {
		values = append(values, map[string]any{"question": answer.Question, "selected_index": answer.Index, "selected_label": answer.Choice.Label, "selected_description": answer.Choice.Description, "recommended": answer.Choice.Recommended})
	}
	payload, _ := json.MarshalIndent(map[string]any{"type": "user_answers", "answers": values}, "", "  ")
	return string(payload)
}

var doneRE = regexp.MustCompile(`(?i)\bDONE\s*[:=]\s*(.{5,300})`)

func extractDoneCriteria(content string) string {
	match := doneRE.FindStringSubmatch(content)
	if len(match) < 2 {
		return ""
	}
	return contract.TruncateEllipsis(strings.TrimSpace(strings.SplitN(match[1], "\n", 2)[0]), 160)
}

var checkRE = regexp.MustCompile(`(?i)\b(test|typecheck|tsc|lint|build|check|vet|pytest|vitest|jest|mypy|ruff)\b`)

func isCheckCall(call contract.ToolCall) bool {
	if call.ToolName() != "run_shell" {
		return false
	}
	var args struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal([]byte(call.ArgumentsJSON()), &args)
	return checkRE.MatchString(args.Command)
}

// trailingIntentRE matches a final line that ANNOUNCES imminent work ("Let me
// fix:", "Now update the CSS and HTML:", "I'll check the Toolbar:") — the
// DeepSeek narration-drift shape. Two deliberate bounds keep it conservative:
// the text must END with a colon or ellipsis (a completed sentence never does),
// and the last line must carry an explicit first-person/imperative intent verb
// phrase, so ordinary final summaries and lists are never re-prompted.
var trailingIntentRE = regexp.MustCompile(`(?i)\b(let me|let's|now (let me|i'?ll|to)|i'?ll( now)?|next,? (i'?ll|let me)|going to)\b[^.!?\n]*[:…]$`)

// trailingIntent reports whether a no-tool-call turn's text ends by announcing
// the next action instead of delivering a result (feature 008 FR-004b guard).
func trailingIntent(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || (!strings.HasSuffix(text, ":") && !strings.HasSuffix(text, "…")) {
		return false
	}
	lines := strings.Split(text, "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	return trailingIntentRE.MatchString(last)
}
