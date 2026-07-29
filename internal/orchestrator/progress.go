package orchestrator

// progressEvidenceTracker measures durable new evidence, not lines changed.
// Read-only audits, repository exploration, and verification can therefore
// make progress without manufacturing edits, while identical repeated calls
// do not buy more runway.
type progressEvidenceTracker struct {
	seen     map[string]struct{}
	revision int
}

func newProgressEvidenceTracker() *progressEvidenceTracker {
	return &progressEvidenceTracker{seen: make(map[string]struct{})}
}

func (tracker *progressEvidenceTracker) Observe(outcomes []toolOutcome) bool {
	advanced := false
	for _, outcome := range outcomes {
		if !outcome.Succeeded() {
			continue
		}
		key := callSignature(outcome.Call) + "\x00" + hashExecutionValue(outcome.Output)
		if _, exists := tracker.seen[key]; exists {
			continue
		}
		tracker.seen[key] = struct{}{}
		tracker.revision++
		advanced = true
	}
	return advanced
}

func (tracker *progressEvidenceTracker) Revision() int {
	if tracker == nil {
		return 0
	}
	return tracker.revision
}
