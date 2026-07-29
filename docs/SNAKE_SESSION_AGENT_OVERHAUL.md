# MuhiyaCode Simple-Task Agent Overhaul

## Incident

The session `49de8c3e030a` received:

> Make for me a simple snake game in a single html file

The requested deliverable was successfully written early in the task, and its
embedded JavaScript passed a syntax check. The agent nevertheless created a
temporary `_harness.js`, repeatedly rewrote and debugged it, attempted to edit
it after deleting it, and ran a 19-assertion logic suite that the user never
requested.

Measured from the durable session records:

| Metric | Observed |
|---|---:|
| Model requests | 19 |
| Tool calls | 15 |
| Prompt tokens | 316,370 |
| Completion tokens | 15,489 |
| Cache-read tokens | 285,090 |
| Cache-miss tokens | 31,280 |
| Model latency | 174,425 ms |
| Cost | $0.0450762 |

An 87% cache hit rate reduced the price of the waste; it did not make the
workflow efficient. Every unnecessary turn still replayed a growing transcript.
The temporary harness itself added a large tool-call payload that was billed
again on later requests.

## Failure timeline

1. The classifier labeled the task `small`.
2. High session effort multiplied its advisory tool allowance to approximately
   20 and sent High reasoning to MiniMax.
3. The model listed the repository and created a directory.
4. It wrote `snake-game/index.html`.
5. It ran a relevant JavaScript syntax check, which passed.
6. No completion rule stopped the task after that sufficient evidence.
7. The model invented a headless browser/DOM harness.
8. Harness failures produced new, unique tool evidence, so the progress tracker
   treated the loop as productive.
9. The tool budget only emitted a convergence message after it was exceeded; it
   never denied another tool call.
10. The turn governor could increase the task class and runway merely because a
    file had changed.
11. A missing `_harness.js` and an `oldString` mismatch were classified as
    indeterminate mutations even though no mutation began, encouraging extra
    inspection and recovery.

## Root causes

### 1. Complexity classification did not understand bounded artifacts

`internal/orchestrator/classify.go` recognized words such as `typo` and
`one-line` as tiny, but did not recognize “simple ... in a single HTML file.”
The generic short-request fallback labeled it `small`.

### 2. Effort was treated as permission to overspend

`BudgetFor` multiplied tool allowance by the session effort and
`mainChatRequest` sent `ReasoningForEffort` directly. Setting the client to High
therefore forced High reasoning even when the task itself was trivial.

### 3. Budgets were descriptions, not enforcement

The tail said `tools~20`; exceeding it only produced another model message.
There was no hard per-task token ceiling, verification-execution ceiling, or
pre-dispatch tool-call ceiling.

### 4. Successful verification was not a completion signal

The phase controller observed a check but did not use a successful check plus a
completed artifact to enter finalization. The model was free to reinterpret
“targeted verification” as increasingly elaborate testing.

### 5. Runway escalated from activity rather than user scope

The turn loop promoted a task when files had changed. Any scratch file therefore
became evidence that the task was larger, granting the behavior that caused the
growth more runway.

### 6. Single-file scope was only natural-language guidance

Nothing prevented a second `.js` file or a mutating shell command from being
introduced into an explicitly single-HTML-file task.

### 7. No-op edit failures overstated uncertainty

The workspace adapter marked ordinary precondition failures as
`ToolExecutionIndeterminate`. “File does not exist” and “oldString not found”
are known not-started outcomes and must not trigger mutation-recovery behavior.

## Implemented architecture

### Task-adaptive budgets

Each class now has hard caps for:

- model turns;
- admitted tool calls;
- actual verification executions;
- cumulative task tokens;
- per-request output tokens;
- reasoning tier.

High or Max session effort may improve a complex task, but cannot raise a chat
or tiny task above Low reasoning or a small task above Medium reasoning.

### Explicit single-artifact scope

The classifier extracts an extension when the user asks for one/single/
standalone file. For the incident prompt it records `.html`.

During such a task:

- mutating shell commands are rejected;
- a non-HTML write is rejected;
- a mutation with no file target is rejected;
- multiple target files are rejected;
- after the deliverable exists, a different artifact is rejected.

`write_file` already creates parent directories, so a preliminary `mkdir`
command is unnecessary.

### Verification completion

Tiny and explicit single-artifact tasks receive one cheap successful check.
When a changed artifact and that check both exist, the engine enters a typed
finalizing state. The next model turn is final-only. Tool calls attempted during
that turn are rejected before execution.

One failed verification retry is tolerated. Successful cached verification does
not consume another execution allowance.

### Hard liveness enforcement

The old automatic class/runway escalation was removed. A task cannot purchase
more turns by generating edits. User steering may grant only two turns of
bounded response runway and does not silently widen tool or token limits.

At the task token ceiling the engine stops without buying another generation.
Tool and verification limits are checked before dispatch.

### Correct mutation certainty

Atomic edit mismatches and edits of a deleted target now wrap the workspace
invalid-argument sentinel. The tool adapter records them as:

`ToolExecutionNotStarted`

They no longer create false unresolved-mutation recovery work.

## Incident-specific new behavior

For the exact Snake prompt under High session effort:

| Control | New value |
|---|---:|
| Class | tiny |
| Reasoning | low |
| Turn cap | 5 |
| Tool cap | 5 |
| Successful checks | 1 |
| Failed-check retries | 1 |
| Task-token ceiling | 45,000 |
| Output cap per request | 8,000 |
| Writable artifact scope | one `.html` file |

The regression scenario deliberately makes the model attempt the old sequence:
list, `mkdir`, HTML write, syntax check, scratch harness. The directory command
is rejected as unnecessary, the HTML write and syntax check execute, and the
scratch harness never executes.

## Files changed

- `internal/orchestrator/classify.go`
- `internal/orchestrator/effort.go`
- `internal/orchestrator/engine.go`
- `internal/orchestrator/gates.go`
- `internal/orchestrator/request_tokens.go`
- `internal/orchestrator/task_policy.go`
- `internal/orchestrator/turnloop.go`
- `internal/instructions/prompt.go`
- `internal/workspace/files.go`
- `internal/workspace/registry.go`

## Regression coverage

- Exact Snake-prompt classification and limits.
- High session effort is capped to Low reasoning for tiny tasks.
- Single-artifact scope rejects a mutating `mkdir`.
- One HTML write executes.
- One syntax check executes.
- A subsequent `_harness.js` write is denied.
- The task ends in five or fewer model requests.
- Missing-file and old-string edit failures are not indeterminate.
- Prompt and wire goldens record the intentional one-time prefix-cache epoch.

## Expected impact

For the captured failure shape, requests are bounded at five rather than 19 and
task tokens at 45,000 rather than 331,859 total input/output tokens. A
well-behaved model should normally use three or four requests: inspect if
necessary, write, cheap check, final. The hard limits exist for models that do
not follow the guidance.

These controls intentionally optimize simple tasks without weakening complex
work. Standard, large, and epic tasks retain progressively larger limits, but
all limits are now real enforcement rather than prompt decoration.
