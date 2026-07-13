# Contract: Request Construction Invariants

**Feature**: `001-prompt-cache-optimization` | Consumers: `internal/orchestrator`,
`internal/gateway`, `internal/mcpclient`, `internal/command`, tests in the cache-guard suite.

This contract defines the request regions and the invariants every request-building code path
(live turn, retry, resume, subagent) MUST satisfy. The offline cache-guard suite enforces it
byte-for-byte.

## Request regions

Every chat-completions request body is conceptually partitioned:

| Region | Content | Stability class |
|---|---|---|
| R1 | System message (agent instructions, environment, model addendum) | **Session-stable** |
| R2 | `tools` array plus `tool_choice` (workspace tools → engine built-ins → pinned MCP block) | **Session-stable** |
| R3 | Settled history (all messages already transmitted in a prior request) | **Append-frozen** |
| R4 | Active tail (newest user message incl. task brief/goal/plan blocks; current-turn assistant/tool messages) | Dynamic |
| R5 | Non-rendering body params (`temperature`, `top_p`, `max_tokens`, `stream`, `reasoning_effort`) + HTTP headers (`X-Muhiya-Effort`, auth) | Dynamic, non-prefix |

## Invariants

**W1. Byte identity of R1+R2+R3.** For request *n* (n>1) in a session, the serialized bytes of
R1, R2, and R3 MUST equal their serialization in request *n−1*, except when an
InvalidationEvent was recorded between the two (see
[invalidation-events.md](invalidation-events.md)). Corollary: request *n−1*'s full message
array is a strict prefix of request *n*'s.

**W2. R1 composition.** R1 MUST be a pure function of session-stable inputs: workspace path,
OS/shell identity, model profile, persisted feature availability (ProbeSnapshot), prompt
template version. R1 MUST NOT contain: wall-clock values of any precision, probe results taken
live at build time, file listings, token counts, task state, or anything derived from
`time.Now()`/randomness. The current date is delivered in the R4 task brief instead.

**W3. R2 composition and order.** `tools` MUST serialize as: fixed-order workspace tools,
fixed-order engine built-ins, then the pinned MCP block sorted by namespaced tool name. Each
schema is canonicalized exactly once at registration. The array MUST NOT change between
requests except via a recorded `toolset-change` event applied at a task boundary. Read-only
MCP operations (list, test) MUST NOT modify the registry. Live MCP handshakes MUST NOT modify
the in-session surface; they update only the persisted ToolSurfaceSnapshot.

**W4. R3 append-freeze.** Settled messages are rewritten only by a maintenance pass or
compaction that (a) fires at `pressure ≥ 0.60` computed from provider-reported prompt tokens
(estimator only before the first usage arrives), (b) runs at a task/turn boundary as one
consolidated pass — never a per-turn trickle, (c) respects the anti-thrash latch (two
consecutive passes pause further automatic rewrites until pressure genuinely recedes or
compaction resolves), and (d) records exactly one InvalidationEvent. Tool-call/result pairing
MUST survive every rewrite.

**W5. R4 dynamic placement.** All per-turn dynamic content — task classification brief,
budgets, current date, goal block, plan-mode block, steering, governor notices, language or
mode preferences — MUST attach to the newest user message or later. Nothing in R4 may modify
R1–R3.

**W6. Render-affecting parameters.** `tools` and `tool_choice` alter the provider-rendered
prompt and are R2: both are session-constant absent a recorded `toolset-change` event.
Reasoning effort, sampling params, and client identity ride body params or HTTP headers, never
messages. Changing effort MUST NOT rewrite replayed R1–R3 bytes.

**W7. Serialization determinism.** Identical logical request state MUST marshal to identical
bytes: map-based envelopes rely on Go's sorted-key marshaling (locked by golden tests);
message and tool arrays are ordered slices; no locale-, time-, or map-iteration-dependent
formatting anywhere in the request path.

**W8. Retry/reconnect replay.** A retried request MUST serialize byte-identically to the
attempt it retries (same input state, same marshal path).

**W9. Restart/resume replay.** Rebuilding a session after process restart MUST reproduce R1,
R2, and R3 byte-identically to the pre-restart serialization, given unchanged configuration
and toolset. Divergence sources fixed by this feature (build-time date, probe flap, MCP
arrival order) are regression-tested.

**W10. Reasoning replay minimum.** Assistant reasoning content MUST NOT be replayed in R3.
The provider-required empty `reasoning_content` key on DeepSeek assistant `tool_calls` turns is
derived solely from the settled message and model family, never from the current effort.

**W11. Subagent conformance.** Subagent request streams satisfy W1–W10 within their own
context (own R1/R2, own history). Subagent traffic MUST NOT mutate the main session's regions.

**W12. Model pinning.** `model` MUST stay constant within a session absent an explicit user
switch, which records a `model-switch` event and refreshes R1's model fields in the same
boundary.

## Verification hooks

- Golden tests: R1 bytes across double-build; R2 bytes across double-build and registration
  orders; full-body marshal determinism.
- Restart determinism test: build → persist → reload → byte-compare (W9 / SC-002).
- Mock-endpoint cache-guard: prefix-overlap measurement across scenario suites (W1, W4, W8).
- Shape check: PrefixShape comparison before every send; non-empty diff without a matching
  InvalidationEvent fails tests (W1/W4 enforcement).
