package orchestrator

// Gate policy (Stability Overhaul Phase 3, T030). Every harness gate — anything
// that can refuse or redirect a model tool call — MUST satisfy all five clauses:
//
//	(a) STATED IN ADVANCE. The rule the gate enforces is disclosed to the model
//	    before it can be violated (the instructions registry's StatesRule /
//	    EnforcesRule audit links the advance-notice text to the enforcing gate).
//	(b) ONE-STEP-FIXABLE OR NON-BLOCKING. A rejection either names the single
//	    mechanical fix, or the gate is non-blocking (records a degradation and
//	    proceeds). It never demands an open-ended fix.
//	(c) BOUNDED. A gate may reject at most twice for the SAME cause within one
//	    task; then it must accept-with-recorded-degradation or hard-stop into a
//	    user decision. A gate can NEVER loop. (assertRecoveryInvariant is the
//	    machine check: no identical rejection more than 3×.)
//	(d) TELEMETERED. Every rejection records a harness event (Phase 1,
//	    recordHarnessEvent) so friction is visible in /errors and the summary.
//	(e) SYNC-TESTED. The prose the model reads and the code that enforces are
//	    locked together by a test, so they cannot drift (e.g. T034 for the shell
//	    gate).
//
// Compliance inventory (audited T030; deviations were fixed by the cited task):
//
//	Gate                         a  b  c  d  e   notes
//	read-only shell gate         ✓  ✓  ✓  ✓  ✓   arg-position false positives fixed (D1/T031); sync test T034
//	execution role gate          ✓  ✓  ✓  ✓  ✓   v1.1.0: main-loop mutations delegate; escalates after 3 (never repeats one line forever)
//	continuation review mask     ✓  ✓  ✓  ✓  ✓   R-D2: review continuations carry the implementer's tool array but refuse mutations
//	repeat limiter               ✓  ✓  ✓  ✓  ✓   blocks a specific repeated call, never the task (T033)
//	duplicate-read dedupe        ✓  ✓  ✓  n/a ✓   benign successful optimization, NOT telemetered as friction (T033)
//	dispatch arg-validation      ✓  ✓  ✓  ✓  ✓   H1: re-emit with well-formed args
//	failed-call short-circuit    ✓  ✓  ✓  ✓  ✓   H2: change approach; telemetered as gate/repeat-failed-call
//	subagent turn-cap            ✓  ✓  ✓  ✓  ✓   forced wrap-up, never a fatal error (INV-3/T035)
//	H5 failure terminator        ✓  ✓  ✓  ✓  ✓   windowed breaker; clean terminate reason, stats stamped
//
// v1.1.0 DELETED the subagent-budget gate (it stated a remaining allowance and
// escalated to a closed door). A count cap plus the plan/execute split was
// jointly incoherent — the main model could neither edit nor delegate — so
// delegation scale is now the model's judgment. Nothing in this inventory caps
// HOW MANY agents run. What remains bounds LIVENESS only: a task must end.
//
// The duplicate-read guard is deliberately NOT counted as friction (clause d
// "n/a"): it is the cache working correctly (the model is handed the result it
// already has), not a rejection of intended work, so surfacing it in the
// friction marker would mislead. See T033.
