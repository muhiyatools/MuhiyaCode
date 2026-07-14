# Feature Specification: DeepSeek API Alignment, Stable User Identity & README Relaunch

**Feature Branch**: `007-deepseek-api-alignment`

**Created**: 2026-07-14

**Status**: Draft

**Input**: User description: "Review the official DeepSeek API documentation (multi-round
chat, chat prefix completion, FIM completion, JSON mode, tool calls, KV cache, rate
limits, create-chat-completion, create-completion). Perform a full audit of both the
Muhiya Go Proxy Gateway (F:\MuhiyaWorkspace\MuhiyaWorkspace) and MuhiyaCode: how
MuhiyaCode connects to the gateway, how the gateway communicates with DeepSeek, and
whether the entire request flow is correctly aligned with DeepSeek's documented behavior.
Pay special attention to caching performance, request isolation, stable user
identification, conversation handling, tool calls, streaming, completion requests, and
all parameters sent to DeepSeek. Audit whether each authenticated Muhiya user should have
a stable and private user identifier forwarded with their requests. Maximize MuhiyaCode's
performance, stability, token efficiency, caching effectiveness, and DeepSeek
compatibility. MuhiyaCode should become aware of DeepSeek's supported capabilities so it
builds valid requests and avoids unsupported behavior. Finally, fully rewrite README.md
as a simple, production-grade GitHub landing page presenting MuhiyaCode's features and
its in-app commands."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Every request reaching DeepSeek is valid and documented (Priority: P1)

A MuhiyaCode user runs long, multi-turn coding sessions. Every request the client builds
and the gateway forwards must contain only parameters DeepSeek documents, with values
DeepSeek accepts, within the limits DeepSeek enforces — so no session ever fails, stalls,
or silently degrades because of a malformed or out-of-spec request. The audit produces a
written conform/diverge verdict for each of the nine documented areas (multi-round chat,
prefix completion, FIM, JSON mode, tool calls, KV cache, rate limits, chat-completion
parameters, completion parameters), and every high-severity divergence is fixed.

**Why this priority**: Correctness is the constitutional first principle; an invalid
parameter, an over-limit token budget, or a mishandled keep-alive risks breaking real
sessions and blocks every other goal (caching, stability, efficiency).

**Independent Test**: Capture the full request/response flow of a representative
multi-turn coding session (tools, edits, reasoning on and off) at the gateway's upstream
boundary; verify every field against the documented parameter tables, and verify the
session survives provider keep-alive and rate-limit behavior. Delivers value alone:
a session that cannot be rejected for request-shape reasons.

**Acceptance Scenarios**:

1. **Given** a multi-turn session with tools and reasoning enabled, **When** every
   upstream request body is captured and compared against the documented parameter
   reference, **Then** every field name, type, and value is documented and valid, and no
   deprecated or unsupported parameter reaches the provider.
2. **Given** the provider holds a request in queue and emits only its documented
   keep-alive signals (comment lines on streaming responses; empty lines on
   non-streaming responses) for several minutes, **When** the client and gateway
   process the wait, **Then** neither side aborts the request before the provider's
   documented 10-minute pre-inference window elapses, and keep-alive traffic is never
   surfaced as an error.
3. **Given** the user's configured effort level is one the provider does not accept
   verbatim, **When** the request is forwarded upstream, **Then** the reasoning controls
   arrive in the provider's documented form only (never a raw unsupported value), for
   both reasoning-capable and non-reasoning target models.
4. **Given** the client's model profile, the gateway's model records, and the provider's
   documented limits, **When** context-window and max-output values are compared,
   **Then** all three agree, and no request can be constructed that exceeds the
   provider's documented limits.
5. **Given** the provider returns a rate-limit rejection under concurrency pressure,
   **When** the client retries, **Then** retry behavior follows the provider's guidance,
   the user sees a clear rate-limit state (not a generic failure), and no duplicate
   billing records are produced.

---

### User Story 2 - Stable, private per-user identity on every upstream request (Priority: P2)

Each authenticated Muhiya user is represented upstream by one stable, opaque identifier,
sent in the provider's documented user-identification parameter on every request. The
identifier never changes across sessions, devices, or key rotations; it contains no
personal information; and two users can never share one. This gives the provider the
documented end-user identification it supports — including per-user concurrency
attribution on expanded quotas — and gives Muhiya clean per-user isolation of one
user's traffic from another's. (The provider's cache is account-scoped; per-user
attribution of cache activity remains a Muhiya-side responsibility.)

**Why this priority**: The provider documents per-user identification and applies
per-user concurrency limits on expanded quotas; sending nothing today means all users
share one anonymous bucket — the single biggest isolation gap the audit baseline found.
It builds directly on US1's validated request shape.

**Independent Test**: Two distinct authenticated users each run two sessions on
different days; captured upstream traffic shows each user's requests always carry the
same identifier, the two identifiers differ, neither is derivable back to personal data,
and a client attempting to spoof the field in its own request body cannot override the
gateway-assigned value.

**Acceptance Scenarios**:

1. **Given** an authenticated user on any device, **When** they start new sessions on
   different days, **Then** every upstream request carries the same identifier.
2. **Given** two different authenticated users, **When** their traffic is compared,
   **Then** their identifiers are distinct (zero collisions across the test population).
3. **Given** a user rotates or replaces their access key, **When** they make new
   requests, **Then** their identifier is unchanged.
4. **Given** a client sends its own value in the user-identification field, **When** the
   gateway forwards the request, **Then** the gateway-derived identifier replaces it
   (server-authoritative identity).
5. **Given** the identifier format, **When** validated against the provider's documented
   constraints (length and character set), **Then** it always conforms, and it contains
   no email, name, or other personal data in any recoverable form.
6. **Given** a live session whose requests are pinned to one model for cache stability,
   **When** the gateway restarts mid-session, **Then** any resulting model change for
   that session is recorded in gateway logs, and the audit report states the chosen
   durability posture (survives restart or best-effort) with its measured impact.

---

### User Story 3 - MuhiyaCode knows what DeepSeek can and cannot do (Priority: P3)

MuhiyaCode carries an explicit capability profile for the provider behind the gateway:
which parameters exist, which are deprecated, the real context and output limits, how
JSON output must be requested, which beta features (prefix completion, fill-in-middle,
strict tool schemas) exist and whether the gateway exposes them. The client consults this
profile when building requests, so it never emits unsupported behavior, and the profile
is the single place a future capability change lands.

**Why this priority**: Prevents an entire class of future regressions (a new feature
quietly sending an unsupported field) and makes the client's behavior self-documenting;
valuable on its own but less urgent than fixing live divergences (US1) and identity (US2).

**Independent Test**: Review the capability profile against the documented API surface
for completeness; then attempt to configure the client into each unsupported behavior
and observe that the request builder refuses or adapts rather than emitting it.

**Acceptance Scenarios**:

1. **Given** the capability profile, **When** compared with the provider's documented
   parameter tables and limits, **Then** every supported parameter, limit, deprecated
   field, and beta feature is represented accurately.
2. **Given** any client feature that would use JSON-constrained output, **When** it
   builds a request, **Then** the request satisfies the documented JSON-mode rules
   (instruction keyword present, example provided, output budget sized to avoid
   truncation) or JSON mode is not used.
3. **Given** a capability the gateway does not expose (e.g., a beta endpoint), **When**
   the client would otherwise use it, **Then** the client avoids it without user-visible
   failure.

---

### User Story 4 - Caching effectiveness is measured end-to-end and does not regress (Priority: P4)

Cache behavior is observable across the whole chain: the provider's cache-hit and
cache-miss token counts are recorded per request in the gateway's usage records and
surfaced to the client, cold-start and steady-state hit rates are reported separately,
and the alignment changes from US1–US3 are verified to keep the steady-state hit rate at
or above today's baseline in a like-for-like session replay.

**Why this priority**: The constitution requires honest measurement and verified
improvements for every cache-affecting change; this story is the proof layer for the
other three. It depends on US1's fixes existing before it can verify them.

**Independent Test**: Run the same scripted multi-turn session before and after the
feature's changes with model, gateway, effort, and workload held constant; compare
provider-reported hit/miss tokens, cost, and latency; confirm miss tokens now appear in
usage records.

**Acceptance Scenarios**:

1. **Given** a completed request whose provider usage reports cache-miss tokens, **When**
   the gateway persists its usage record, **Then** the miss count is stored alongside the
   hit count (today it is discarded).
2. **Given** the before/after benchmark sessions, **When** steady-state hit rates are
   compared, **Then** the after-rate is not lower than the before-rate, and cold-start
   figures are reported separately from steady-state figures.
3. **Given** a session interrupted mid-stream, **When** usage is recorded, **Then**
   estimated figures are labeled as estimates and never presented as provider-reported
   measurements.

---

### User Story 5 - A production-grade README that sells MuhiyaCode in one screen (Priority: P5)

A developer landing on the GitHub repository understands within one scroll what
MuhiyaCode is, what it can do, how to install it, and how to drive it — with the
command reference presenting MuhiyaCode's own in-app slash commands (typed inside the
running agent), not external shell commands. The document reads like the polished
landing pages of leading terminal coding agents: short, visual, confident, and complete
on features while minimal in words.

**Why this priority**: High user-facing value and fully independent of the wire-protocol
work — but it documents the product the other stories improve, so it lands last to
describe the final state.

**Independent Test**: A person who has never used MuhiyaCode reads only the new README,
installs the tool, launches it, and can name its major features and find any in-app
command's purpose — without opening any other document.

**Acceptance Scenarios**:

1. **Given** the current in-app command registry, **When** the README's command section
   is checked against it, **Then** every existing in-app command (including aliases) is
   listed with an accurate one-line description, presented as a command typed inside
   MuhiyaCode, and no nonexistent command is documented.
2. **Given** the rewritten README, **When** its length is compared to the current one,
   **Then** the new document is at most roughly half the current line count while still
   covering: what MuhiyaCode is, key features, installation, first run, in-app commands,
   and where deeper docs live.
3. **Given** a new user with a supported platform, **When** they follow only the README,
   **Then** they can install and launch MuhiyaCode on the first attempt.

---

### Edge Cases

- Provider queues a request under heavy load and sends only keep-alive signals for
  minutes: today both the client and the gateway abort a quiet request after roughly
  1.5–2 minutes unless keep-alive traffic resets their inactivity limits (the audit must
  confirm which); the documented 10-minute pre-inference window must be survivable
  end-to-end.
- Provider closes the connection at the 10-minute pre-inference boundary: the user sees
  a clear, retryable state; billing records are not duplicated on retry.
- A stream dies after tool calls were announced but before completion: the next request
  must still pair every announced tool call with exactly one result, or the provider
  may reject the whole conversation.
- Cache-hit + cache-miss token counts contradict the reported prompt total: the
  contradiction is flagged rather than silently trusted.
- A user's identifier source record is deleted and recreated (same human, new account):
  identifiers are account-scoped by design; the audit must state this boundary
  explicitly.
- The gateway restarts mid-day: today, session-to-model affinity does not survive a
  restart, allowing a silent model swap that wipes the provider's prefix cache for every
  live session; any swap must at minimum be observable, and the durability decision must
  be recorded.
- A client sends deprecated or unknown parameters (old client version, third-party
  client): the gateway's forwarding policy for undocumented fields must be explicit —
  documented pass-through or documented strip — not accidental.
- The provider renames or re-tiers its models (docs now describe different model names
  than the gateway's configured targets): model records must be reconciled and alias
  attribution (one alias targeting another model's cache namespace) must be verified.
- Time-variable (off-peak) provider pricing: the audit must confirm whether the provider
  currently operates any time-variable pricing; if it does, the gap against the
  gateway's static price records must be documented so billing stays correct.
- A README reader on a platform without a prebuilt binary: the README must state
  supported platforms honestly.

## Requirements *(mandatory)*

### Functional Requirements

**Audit & wire-protocol alignment (US1)**

- **FR-001**: The feature MUST produce a written audit report giving an explicit
  conform/diverge verdict, with a citation to the provider's documentation, for each of:
  multi-round conversation handling, chat prefix completion, FIM completion, JSON mode,
  tool calls, KV caching, rate limits, chat-completion parameters, and completion
  parameters — covering both the client→gateway hop and the gateway→provider hop.
  Every finding MUST carry a severity, where high severity means the divergence can
  cause request rejection, session failure, billing error, or provider-cache
  invalidation; every high-severity finding MUST be resolved and re-verified before the
  feature completes.
- **FR-002**: Every parameter sent to the provider MUST be a documented parameter with a
  documented-valid value; deprecated parameters MUST NOT be forwarded upstream regardless
  of what a client sends.
- **FR-003**: Reasoning/thinking controls MUST reach the provider only in the provider's
  documented form and only with documented values; a client-side effort setting the
  provider does not accept verbatim MUST be mapped or removed before the request leaves
  the gateway — under every configuration path, including when the client sends no
  explicit effort setting at all.
- **FR-004**: Context-window and maximum-output limits MUST be consistent across the
  client's model profiles, the gateway's model records, and the provider's documented
  limits; the request pipeline MUST NOT be able to emit a request exceeding the
  provider's documented limits.
- **FR-005**: Client and gateway MUST treat the provider's documented keep-alive signals
  (comment lines on streaming responses; empty lines on non-streaming responses) as
  connection liveness, and MUST NOT abort a healthy queued request before the provider's
  documented pre-inference window (10 minutes) has elapsed.
- **FR-006**: On a provider rate-limit rejection, the client MUST retry within the
  provider's guidance (bounded attempts, honoring any server-provided wait), MUST surface
  a distinct rate-limit state to the user, and the chain MUST NOT create duplicate
  billing records for one logical request.
- **FR-007**: Conversation history MUST continue to be resent complete and in
  chronological order on every round (stateless provider), remaining append-only except
  when the next request would otherwise exceed the provider's documented context window
  — and every such history rewrite MUST be recorded in session records; provider-
  generated reasoning output MUST be excluded from replayed history except where the
  provider requires the field's presence.
- **FR-008**: Every tool call announced by the model MUST be answered by exactly one
  matching tool result before the next provider round, including after interrupted
  streams (regression guard on existing behavior).

**Stable user identity & isolation (US2)**

- **FR-009**: The gateway MUST attach a stable per-user identifier, in the provider's
  documented user-identification parameter, to every upstream request made on behalf of
  an authenticated user.
- **FR-010**: The identifier MUST be opaque (no personal data recoverable from it),
  deterministic for the same user account across sessions, devices, and access-key
  rotations, unique per user account, and MUST conform to the provider's documented
  length and character-set constraints.
- **FR-011**: The gateway MUST be authoritative for the identifier: any
  user-identification value arriving from a client MUST be replaced, and the identifier
  MUST be derived from the authenticated account — never from client-supplied input.
- **FR-012**: Per-session affinity decisions that affect provider cache reuse (such as
  pinning a session to one model) MUST NOT silently change mid-session; any change MUST
  be observable in gateway logs, and the chosen durability posture (survives restart or
  best-effort) MUST be recorded with its measured impact in the audit report.

**Capability awareness (US3)**

- **FR-013**: MuhiyaCode MUST maintain a provider capability profile covering supported
  parameters, deprecated parameters, context and output limits, JSON-output rules, and
  beta features with their gateway availability; the request builder MUST consult it so
  no request uses a capability marked unsupported or unavailable.
- **FR-014**: Any use of JSON-constrained output MUST satisfy the provider's documented
  preconditions (instruction keyword present in the prompt, an example of the desired
  shape, and an output budget large enough to avoid truncation).
- **FR-015**: Beta provider features (prefix completion, fill-in-middle, strict tool
  schemas) MUST be represented in the capability profile with an explicit
  adopted/not-adopted status; adopting any of them is in scope only if the audit
  demonstrates a measured improvement in at least one benchmark metric (token cost,
  latency, or cache-hit rate) or removal of a documented correctness divergence, and
  each adoption decision MUST be recorded together with that measurement.

**Cache measurement & accounting (US4)**

- **FR-016**: The gateway MUST record both provider-reported cache-hit and cache-miss
  token counts in its per-request usage records, and cost calculations MUST derive from
  provider-reported usage whenever it is available.
- **FR-017**: Usage figures produced without provider confirmation (estimation after an
  interrupted stream) MUST be labeled as estimates everywhere they are stored or shown.
- **FR-018**: All alignment changes MUST be verified by a like-for-like before/after
  replay of a representative multi-turn session — the canonical benchmark workload
  established in feature 001 (multi-turn coding with tool calls, file reads, edits, and
  reasoning both on and off), with model, gateway, effort, and workload held constant —
  reporting cold-start and steady-state cache figures separately, with no steady-state
  regression.

**README relaunch (US5)**

- **FR-019**: README.md MUST be fully rewritten as a concise product landing document
  covering, in order of a newcomer's needs: what MuhiyaCode is, its key features, how to
  install it, how to launch and first-run it, its in-app command reference, and pointers
  to deeper documentation.
- **FR-020**: The README's command reference MUST present MuhiyaCode's in-app slash
  commands (typed inside the running agent), MUST include every command and alias that
  exists in the current build, MUST NOT document commands that do not exist, and MUST
  clearly separate in-app commands from the few shell-level commands (install, launch).
- **FR-021**: The README MUST be no more than 55% of the current document's line count
  (currently 228 lines as measured on 2026-07-14, so at most 125 lines) while remaining
  complete on the feature list; deep internal design content MUST move to (or remain
  in) the docs directory rather than the README.

### Key Entities

- **Authenticated User**: A Muhiya account holder; owns one or more access keys; the
  subject of identity, isolation, budgets, and per-user provider limits.
- **Access Key (Virtual Key)**: A credential presented by a client; resolves to exactly
  one Authenticated User; rotatable without changing the user's identity.
- **Stable User Identifier**: The opaque, deterministic, provider-conformant value
  representing one Authenticated User on every upstream request; derived server-side;
  no personal data.
- **Session**: One client conversation stream (main, subagent, or auxiliary); carries a
  session marker used for affinity; distinct from user identity.
- **Capability Profile**: The client's authoritative description of what the provider
  (via the gateway) supports: parameters, limits, rules, beta features, adoption status.
- **Audit Finding**: One documented divergence or conformance verdict: area, observed
  behavior, documented behavior, severity, resolution status.
- **Usage Record**: The gateway's per-request accounting row: tokens (prompt, completion,
  cache-hit, cache-miss), cost, estimated-or-measured flag, user and key attribution.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In a captured representative multi-turn session, 100% of upstream request
  fields are documented provider parameters with valid values, and zero requests are
  rejected by the provider for request-shape reasons.
- **SC-002**: The audit report contains an explicit conform/diverge verdict with
  documentation citation for all nine documented areas, and 100% of divergences rated
  high-severity are resolved and re-verified within the feature.
- **SC-003**: Across at least two users, two sessions each, on at least two different
  days: each user's upstream requests carry one identical identifier (100% consistency),
  the users' identifiers are distinct (zero collisions), and no identifier fails the
  provider's documented format constraints.
- **SC-004**: A streamed request that receives only documented keep-alive traffic
  completes successfully after a queue delay of up to the provider's documented
  10-minute window; the audit first records today's actual behavior (client and gateway
  inactivity limits of 90–120 seconds would abort such a request unless keep-alive
  traffic resets them — currently unverified) as the before-measurement.
- **SC-005**: 100% of usage records for provider responses that report cache-miss tokens
  include the miss count (today: 0%).
- **SC-006**: In the before/after replay (workload held constant), steady-state
  cache-hit rate is greater than or equal to the pre-change baseline, and total cost and
  token figures are reported alongside hit rate (whole-picture reporting).
- **SC-007**: A first-time reader can install and launch MuhiyaCode using only the new
  README, and the README's command reference matches the in-app registry exactly (every
  existing command listed, zero nonexistent commands).
- **SC-008**: The rewritten README is no more than 55% of the current line count
  (at most 125 of the current 228 lines) while covering all sections named in FR-019.

## Out of Scope

- Muhiya's own per-plan rate limits, budgets, and credits: unchanged by this feature.
- Shipping user-facing beta provider functionality (prefix completion, fill-in-middle,
  strict tool schemas) without the measured benefit required by FR-015 — the decision
  and its record are in scope; speculative delivery is not.
- Implementing time-variable (off-peak) pricing in billing: the audit records whether a
  gap exists; closing it is a separate feature if confirmed.
- Changes to the client↔gateway protocol beyond the compatibility boundaries preserved
  in Assumptions.
- Provider-account administration (quota expansion requests, account-level settings).

## Assumptions

- The provider documentation fetched on 2026-07-14 (model names, `user_id` parameter,
  thinking controls, keep-alive and 10-minute pre-inference behavior, deprecated
  penalty parameters, cache usage fields) is the authoritative reference for this
  feature; the audit report snapshots the referenced doc content so later doc changes
  don't invalidate the verdicts. The raw evidence base is recorded in this feature's
  `audit-baseline.md`; the create-completion parameter surface is currently evidenced
  only via the FIM guide capture and will be snapshotted in full during the audit.
- The gateway codebase at `F:\MuhiyaWorkspace\MuhiyaWorkspace` is in scope for changes;
  this feature's artifacts live in the MuhiyaCode repository, and gateway changes ship
  through the gateway's own workflow.
- The stable identifier derives from the gateway's authenticated user account record
  (not from access keys, client input, or personal attributes), so key rotation
  preserves identity and account deletion retires the identifier.
- Existing compatibility boundaries are preserved: the client↔gateway session and
  effort signaling, the gateway's per-request cost reporting to the client, key-based
  authentication, and the client's on-disk state layout all remain unchanged
  (constitution Principle VIII).
- Adopting provider beta features is decision-scoped, not delivery-scoped: the feature
  must adjudicate and record each (adopt / don't adopt), but user-facing beta
  functionality ships only where the audit demonstrates concrete benefit.
- Rate-limit handling targets the provider's documented concurrency semantics; Muhiya's
  own per-plan rate limits remain unchanged and out of scope.
- The README rewrite targets the repository's GitHub landing audience (developers
  evaluating or installing MuhiyaCode); deep architecture, security, and design
  material remains in `docs/`.
- Benchmark verification uses the constitution's Measurement & Benchmarking Standards
  (provider-reported usage, multi-turn realistic sessions, cold vs steady-state
  separation) as already practiced in feature 001.
- Keep-alive survival (SC-004) may be verified against a simulated upstream that emits
  the provider's documented keep-alive signals for a configurable duration, since a
  genuine multi-minute provider queue delay cannot be produced on demand; the client
  and gateway must be exercised end-to-end through that simulation.
