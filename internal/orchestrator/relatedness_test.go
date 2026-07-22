package orchestrator

import "testing"

func TestRelatednessFixtures(t *testing.T) {
	tests := []struct {
		name string
		in   RelatednessInput
		want RelatednessDecision
	}{
		{"direct followup", RelatednessInput{Prompt: "make that button blue instead", CurrentGoal: "edit login button", CurrentSettled: true}, RelatedContinue},
		{"correction", RelatednessInput{Prompt: "actually use red", CurrentGoal: "use blue", CurrentSettled: true}, RelatedContinue},
		{"pronoun", RelatednessInput{Prompt: "fix it", CurrentGoal: "repair parser", CurrentSettled: true}, RelatedContinue},
		{"same path", RelatednessInput{Prompt: "update ui/button.ts colors", CurrentGoal: "button behavior", KnownPaths: []string{"ui/button.ts"}, CurrentSettled: true}, RelatedContinue},
		{"explicit switch", RelatednessInput{Prompt: "new task: audit payments", CurrentGoal: "button", CurrentSettled: true}, RelatedNewEpoch},
		{"unsettled switch", RelatednessInput{Prompt: "new task: audit payments", CurrentGoal: "button"}, RelatedUncertain},
		{"uncertain", RelatednessInput{Prompt: "investigate database latency today", CurrentGoal: "button colors", CurrentSettled: false}, RelatedUncertain},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ClassifyRelatedness(test.in); got.Decision != test.want {
				t.Fatalf("got=%+v want=%s", got, test.want)
			}
		})
	}
}

func TestRelatednessTwoPromptHysteresis(t *testing.T) {
	var h RelatednessHysteresis
	in := RelatednessInput{Prompt: "database latency benchmark query metrics", CurrentGoal: "button colors", CurrentSettled: true}
	if got := h.Decide(in); got.Decision != RelatedUncertain {
		t.Fatalf("first=%+v", got)
	}
	if got := h.Decide(in); got.Decision != RelatedNewEpoch {
		t.Fatalf("second=%+v", got)
	}
}
