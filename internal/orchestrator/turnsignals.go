package orchestrator

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// This file holds the small turn-analysis helpers the main loop needs: the
// ask_user answer encoding, the DONE-criteria stat, the check-call counter, and
// the narration-drift guard. They lived in the plan renderer until the planning
// pipeline was removed; none of them were ever plan machinery.

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

// trailingIntentMinPhraseLen is the minimum length AFTER the intent verb for a
// trailing-intent match (Phase V F12 hardening): the regex alone matches
// user-quoted colon fragments (e.g. "note: remember to wire X") which the
// model often produces when reading helpful docs. Excluding short fragments
// keeps the reset nudge focused on real narration drift without false
// positives on quoted text.
const trailingIntentMinPhraseLen = 8

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
