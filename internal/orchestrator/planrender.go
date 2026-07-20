package orchestrator

import (
	"fmt"
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
