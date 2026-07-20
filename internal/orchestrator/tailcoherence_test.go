package orchestrator

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The instruction-registry audit only sees REGISTERED texts. A dozen
// model-facing strings live as inline literals in the orchestrator — the
// governor riders, the review nudge, the subagent wrap-up prompts, the
// propose_changes verdicts, the continuation notices. A semantic audit found
// every one of them still addressing a main model that edits files: the
// plan/execute split had been applied to the cached prefix and the gate, but
// not to the dynamic tail the model reads LAST.
//
// This test closes that blind spot mechanically: it greps the orchestrator's
// own source for model-facing literals and fails on tail text that instructs
// the main loop to edit, or that speaks in the deleted budget vocabulary.

// mainLoopTailLiterals are the files that compose per-turn user messages the
// MAIN model receives.
var mainLoopTailLiterals = []string{"turnloop.go", "toolhandlers.go", "dispatch.go"}

// tailInstructionRE finds the injected user-message bodies: the bracketed
// harness riders ("[governor] …", "[review] …", "[loop guard] …") in ordinary
// quoted strings, AND the propose_changes verdicts, which are backtick raw
// strings — an earlier version of this pattern matched only quoted strings and
// passed vacuously over the verdicts, which is exactly the blind spot this
// test exists to close.
var tailInstructionRE = regexp.MustCompile(
	`"(\[(?:governor|review|loop guard|continue|validation gate)\][^"]*)"` +
		"|`([^`]*verdict[^`]*)`")

// forbiddenInMainTail are phrases that contradict the plan/execute split or
// resurrect the deleted budgets. Each maps to why it is wrong.
var forbiddenInMainTail = map[string]string{
	"finish edits":                "tells the main model to edit; the role gate refuses it",
	"finish the edits":            "tells the main model to edit; the role gate refuses it",
	"continue it directly":        "tells the main model to do work the role split forbids",
	"Execute and verify the plan": "instructs the main model to execute",
	"verify each change yourself": "instructs the main model to execute and re-verify",
	"agents<=":                    "deleted budget vocabulary",
	"agents=0":                    "deleted budget vocabulary",
	"budget exhausted":            "deleted budget vocabulary",
	"run(s) remaining":            "deleted budget vocabulary",
}

func TestMainLoopTailDoesNotContradictTheRoleSplit(t *testing.T) {
	for _, name := range mainLoopTailLiterals {
		data, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, match := range tailInstructionRE.FindAllStringSubmatch(string(data), -1) {
			literal := match[1] + match[2]
			for phrase, why := range forbiddenInMainTail {
				if strings.Contains(strings.ToLower(literal), strings.ToLower(phrase)) {
					t.Errorf("%s: a per-turn instruction to the MAIN model contains %q — %s:\n  %s", name, phrase, why, literal)
				}
			}
		}
	}
}

// TestSubagentTailCarriesTheReportContract: the wrap-up prompts override the
// report shape at exactly the moment a run is being cut short, which is when a
// missing STATUS line is most damaging — the caller then reads "no verification
// shown" with nothing to act on.
func TestSubagentTailCarriesTheReportContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(".", "subagent.go"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, wrapUp := range []string{"Turn budget reached", "Context window nearly full"} {
		index := strings.Index(source, wrapUp)
		if index < 0 {
			t.Fatalf("wrap-up prompt %q not found — did it move?", wrapUp)
		}
		// The STATUS requirement must appear inside the same string literal.
		end := strings.Index(source[index:], "\"}")
		if end < 0 {
			end = 400
		}
		if !strings.Contains(source[index:index+end], "STATUS") {
			t.Errorf("the %q wrap-up prompt does not restate the STATUS requirement, so a cut-short run reports without one", wrapUp)
		}
	}
	// The harness-synthesized partial report must itself carry a status.
	if got := parseReportStatus(turnBudgetPartialReport("")); got != ReportStatusBlocked {
		t.Errorf("turnBudgetPartialReport status = %q, want blocked — a synthetic report with no status strands the caller", got)
	}
}
