package command

import (
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/state"
)

func TestExecutionRecoveryNoticeIsActionable(t *testing.T) {
	if notice := executionRecoveryNotice(state.ExecutionJournalHealth{}); notice != "" {
		t.Fatalf("healthy journal notice = %q", notice)
	}
	notice := executionRecoveryNotice(state.ExecutionJournalHealth{
		IncompleteTools:  []contract.ToolStartedRecord{{ExecutionID: "tool-1"}},
		PendingMutations: []contract.MutationIntent{{IntentID: "mutation-1"}},
	})
	for _, required := range []string{"1 tool dispatch", "1 mutation", "New mutations are blocked"} {
		if !strings.Contains(notice, required) {
			t.Fatalf("notice %q does not contain %q", notice, required)
		}
	}
}
