package instructions

// Gate/denial/loop-guard texts (Audience: Gate, Cache: Sidecar): returned as
// tool-call results when the engine blocks or denies an action. Templates keep
// their Sprintf verbs and are filled at the call site; this file exists so the
// literal strings are named, registered, and auditable in one place (IS-1).
//
// The continuation-review mask and the execution-role gate were removed with
// the subagent system: the first masked mutations inside a review continuation,
// the second refused workspace changes from the planning session. Neither
// failure can occur when one session does all the work with the full toolset.

// RuleNoRepeatFailedCall / RuleNoDuplicateRead are the terse-gate rule IDs
// covered by the gate-message-quality audit (IS-8).
const (
	RuleNoRepeatFailedCall = "rule.no-repeat-failed-call"
	RuleNoDuplicateRead    = "rule.no-duplicate-read"
)

const (
	// GateRepeatLimiterBody fires after three identical calls. It names the
	// exact trigger (repeated three times) and an affordable next action
	// (change action, or finish) — IS-8.
	GateRepeatLimiterBody = "Blocked: identical call repeated three times; take a different action or finish."
	// GateDuplicateReadTmpl fires when a read's result is already in
	// unchanged context. It names the exact reason (call %s already holds
	// the result) and the affordable next action (use that result) — IS-8.
	GateDuplicateReadTmpl = "Blocked: unchanged result already in context from call %s. Use that result; do not re-read."
)

var (
	_ = Register(Text{ID: "gate.repeat-limiter", Audience: Gate, Cache: Sidecar, Body: GateRepeatLimiterBody, EnforcesRule: RuleNoRepeatFailedCall})
	_ = Register(Text{ID: "gate.duplicate-read", Audience: Gate, Cache: Sidecar, Body: GateDuplicateReadTmpl, EnforcesRule: RuleNoDuplicateRead})
)

// RuleChunkedWrite is the recovery contract for a call cut off at the output
// limit: never resend the whole payload, extend the file in parts instead.
// Stated in advance by the executor's edit discipline, enforced here.
const RuleChunkedWrite = "rule.chunked-write"

const (
	// GateTruncatedWriteBody is returned INSTEAD of dispatching a file-writing
	// call whose arguments were cut off at the output cap. The previous text told
	// the model to "send a shorter version (fewer, more compact steps)" — phrasing
	// inherited from an update_plan incident, and actively wrong for a file write,
	// where "shorter" means dropping the user's features. The recovery that
	// actually works is to extend the file in parts.
	GateTruncatedWriteBody = "Blocked: this call was cut off at the output limit, so its arguments are incomplete. Do NOT resend the whole payload. Write the file in parts: write_file the opening section, ending at a complete line followed by a unique marker line; then extend it with edit_file replacing that marker with the next section plus the marker again; delete the marker in the final edit. Never resend content you have already written."
	// GateTruncatedCallBody is the non-write variant: a smaller, complete call is
	// the right retry when the arguments are not file content.
	GateTruncatedCallBody = "Blocked: this call was cut off at the output limit, so its arguments are incomplete. Re-emit it as one complete call with only the arguments it needs; if it carried a large payload, split the work across several smaller calls."
)

var (
	_ = Register(Text{
		ID: "gate.truncated-write", Audience: Gate, Cache: Sidecar, Body: GateTruncatedWriteBody,
		EnforcesRule: RuleChunkedWrite, MentionsTools: []string{"write_file", "edit_file"}, AllowlistCtx: "main-loop",
	})
	_ = Register(Text{ID: "gate.truncated-call", Audience: Gate, Cache: Sidecar, Body: GateTruncatedCallBody, EnforcesRule: RuleChunkedWrite})
)

// The read-only-agent run_shell rejection and the execution-role gate texts
// were removed with the subagent system. The first refused state-changing
// commands from a read-only delegated run; the second refused workspace edits
// from the planning session and told it to delegate instead — the refusal that,
// in a live session, discarded a fully generated file after the model had
// already paid to produce it.
