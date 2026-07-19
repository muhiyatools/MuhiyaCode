package tui

import (
	"github.com/muhiya/muhiyacode/internal/gateway"
)

// friendlyTaskError renders a task-ending error as actionable guidance instead
// of a raw HTTP status or Go network error (Stability Overhaul T051, defect
// D10). The mapping itself lives in gateway.FriendlyRequestError (feature 014)
// so subagent reports and notices share the exact same phrasing.
func friendlyTaskError(err error) string {
	return gateway.FriendlyRequestError(err)
}
