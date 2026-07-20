package orchestrator

// The completion-honesty disclosure tests (TestCompletionDisclosesIncompleteSteps,
// TestCompletionNoDiscloseWhenComplete, TestCompletionNoDiscloseForNonPlanTask)
// were deleted with the goal/plan lifecycle they were keyed to: they drove the
// disclosure through e.plan + LifecycleImplementing, both of which are gone.
//
// appendCompletionDisclosure is a pass-through stub today (turnhelpers.go). The
// disclosure is rebuilt against the tasks.md checklist in a later phase, and
// these tests return then — rewritten against checklist fixtures instead of
// plan state. See NATIVE_AGENT_PLAN.md §4.
