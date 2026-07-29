package command

import (
	"fmt"

	"github.com/muhiya/muhiyacode/internal/state"
)

func executionRecoveryNotice(health state.ExecutionJournalHealth) string {
	if len(health.PendingMutations) == 0 && len(health.IncompleteTools) == 0 {
		return ""
	}
	return fmt.Sprintf(
		"Recovery required: %d tool dispatch(es) lack a terminal record and %d mutation(s) lack a confirmed outcome. New mutations are blocked until the session journal is inspected and reconciled.",
		len(health.IncompleteTools),
		len(health.PendingMutations),
	)
}
