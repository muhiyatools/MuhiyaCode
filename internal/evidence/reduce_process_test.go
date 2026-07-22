package evidence

import (
	"strings"
	"testing"
)

func TestReduceProcessProtectsFailuresAndNormalizesHostileOutput(t *testing.T) {
	raw := "\x1b[31mFAIL\x1b[0m a_test.go:12: expected 2 got 3\n" + strings.Repeat("noise\n", 500) + string([]byte{0xff})
	card := ReduceProcess(ProcessReductionInput{Adapter: "go-test", Status: "failure", ExitCode: 1, Complete: true, Text: raw})
	rendered := card.Render()
	if !strings.Contains(rendered, "FAIL a_test.go:12: expected 2 got 3") || strings.Contains(rendered, "\x1b") || card.MaxTokens != 3000 || card.OmittedLines == 0 {
		t.Fatalf("card=%+v rendered=%q", card, rendered)
	}
}

func TestReduceProcessPreservesTimeoutAndCancellation(t *testing.T) {
	card := ReduceProcess(ProcessReductionInput{Adapter: "shell", Status: "timeout", ExitCode: -1, TimedOut: true, Cancelled: true, Complete: false})
	rendered := card.Render()
	if !strings.Contains(rendered, "timed_out=true") || !strings.Contains(rendered, "cancelled=true") || strings.Contains(rendered, "complete=true") {
		t.Fatalf("rendered=%q", rendered)
	}
}

func TestExtraProcessAdaptersUseErrorPriorityReducer(t *testing.T) {
	for name, card := range map[string]ObservationCard{
		"npm":    ReduceNPM(ProcessReductionInput{Status: "failure", ExitCode: 1, Text: "npm ERR! broken", Complete: true}),
		"pytest": ReducePytest(ProcessReductionInput{Status: "failure", ExitCode: 1, Text: "FAILED test_x.py:3 expected true", Complete: true}),
	} {
		if !strings.Contains(card.Render(), name) || len(card.Excerpts) == 0 || !card.Excerpts[0].Protected {
			t.Fatalf("adapter %s card=%+v", name, card)
		}
	}
}
