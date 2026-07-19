# Data Model: Subagent Context Reuse & Cache-First Orchestration

**Feature**: 012-subagent-context-cache · **Phase 1 design artifact**
All entities are additive extensions of existing stores (Constitution VIII); no new storage system. Source-of-truth mapping and validation rules cite the research decisions (research.md R-D*) they implement.

## Entity overview

```text
SubagentContextRecord 1 ──── * ContextLink ──── 1 dispatch (run)
        │                          │
        │ touched-set              │ decision + verification
        ▼                          ▼
   ReadWriteSet               DelegationLedger (per task) ──► TaskStats.Links
                                   ▲
PhaseHandoff (per dispatch) ───────┘
ProviderCacheProfile (per model family, static) — consulted by link decision
```

---

## SubagentContextRecord

The durable artifact of one completed subagent run. Persisted at `~/.muhiya/sessions/<sessionID>/agents/<runID>.json` (R-D3); the session-sidecar allowlist extends additively.

| Field | Type | Rules |
|---|---|---|
| `runID` | string | unique per run; existing run-summary ID reused |
| `kind` | string | explore/plan/review/general |
| `modelID` | string | the resolved subagent model at run time |
| `pin` | string | exact wire pin used (`<sessionID>:sub:<kind>`); continuation MUST reuse verbatim (R-F11) |
| `transcript` | []contract.Message | **verbatim** messages as constructed (raw `ReasoningDetails` preserved; complete tool call/result pairings — validated at save, records failing pairing-completeness are marked non-linkable, never repaired) (R-D1, R-F8) |
| `terminalShape` | enum | `clean-done` / `wrapup-done` / `partial-ceiling` / `failed` / `cancelled` (R-D4) |
| `finalPromptTokens` | int | provider-reported prompt tokens of the last request; overflow predicate input (R-D10) |
| `finalCompletionTokens` | int | provider-reported; sizing/diagnostics |
| `touched` | ReadWriteSet | see below (R-D5) |
| `result` | string | the structured report (existing bounds) |
| `taskLineage` | string | task ID + phase label that dispatched it; feeds eligibility (R-D9) |
| `createdAt` / `completedAt` | timestamps | eviction ordering |

**Lifecycle**: created at dispatch, finalized at completion (end-of-run re-stat, terminal shape stamp), evicted oldest-completed-first under a per-session cap (count + bytes), removed with the session. A record with `terminalShape != clean-done` is retained (digest-fallback source + diagnostics) but never stream-continued.

**Validation**: save-time checks — every assistant `tool_calls` entry has its tool result (R-F8); transcript non-empty; pin matches `<sessionID>:sub:<kind>`. A record failing validation is stored with `linkable=false` and a reason.

## ReadWriteSet

Per-run touched-file evidence with **end-of-run** fingerprints (R-D5 — self-edits are never stale).

| Field | Type | Rules |
|---|---|---|
| `reads` | map[path]→{lineRanges, fingerprint} | path = workspace-relative, slash-normalized, Windows-lowercased (single normalization: the inspection-ledger convention) |
| `writes` | map[path]→{fingerprint} | any mutation by this run |
| `fingerprint` | {mtimeMS float64, size int64} | captured by one re-stat pass at run **completion**, not at read time |

**Staleness computation** (link decision input): for each path in `reads ∪ writes`, current `os.Stat` vs stored fingerprint; changed-fraction = changed ÷ |touched set|. `> 0.5` → continuation declined (Clarification Q5). Changed members ≤ 0.5 → continuation proceeds with mandatory re-read directives named in the successor handoff. mtime+size blindspot documented (R-D5); no content hashing.

## ContextLink

The recorded relationship between a new dispatch and its predecessor. One per dispatch **including declined links** (FR-003, SC-008).

| Field | Type | Rules |
|---|---|---|
| `dispatchRunID` | string | the successor |
| `predecessorRunID` | string? | nil when no candidate existed |
| `decision` | enum | `continued` / `digest-seeded` / `fresh` |
| `reason` | string | machine-readable: `eligible`, `no-candidate`, `terminal-shape:<shape>`, `stale:<pct>`, `window-overflow`, `model-changed`, `pin-mismatch`, `kind-pair-unsupported`, `relatedness-miss` |
| `form` | enum? | when `continued`: `same-kind` / `review-after-implement` (R-D2) |
| `verification` | {pairedCacheReadShare float64?, reported bool} | first continuation request's provider-reported paired share; nil+reported=false when provider sent no cache fields — **displayed as unavailable, never estimated** (FR-015) |
| `rereadDirectives` | []path | staleness-driven re-reads named in the successor handoff |

**State transitions**: `decided → dispatched → verified` (verification stamped after the first provider response; a link abandoned before dispatch is recorded with its reason).

## DelegationLedger

Per-task accumulation feeding task summaries and the benchmark (R-D13). In-memory during the task; summarized into `TaskStats.Links` at task end; full ledger appended to the session's run records.

| Field | Type |
|---|---|
| `links` | []ContextLink |
| `gateEvents` | []{tool, phase, outcome: denied/waived/exempt-reason} — read-gate telemetry (R-D8) |
| `handoffSizes` | []{dispatchRunID, outboundChars, returnChars} — SC-006 overhead accounting |

## PhaseHandoff (revised contract shape)

Outbound dispatch payload — **moves to the first user message** for all dispatches (R-D6); continuation dispatches append it as the next user message on the replayed stream (R-D1).

| Field | Rules |
|---|---|
| `role` / `scope` / `deliverable` / `outputFormat` | unchanged five-field contract (minus Context, replaced below) |
| `phaseRef` | plan phase identifier + step titles + plan-Note Verification/Risks digest (bounded) (R-D7) |
| `carryForward` | non-discoverable facts only; bounded (existing briefing bounds) |
| `rereadDirectives` | from ContextLink; "these files changed since your predecessor ran — re-read before trusting inherited context" |
| `roleOverride` | continuation-only: e.g. review-after-implement instructions (mutations masked harness-side, R-D2) |

**Invariant**: no file bodies, no plan prose beyond the phase digest, no repeat of static material already in the stable prefix (FR-008, FR-014).

## ProviderCacheProfile (extension of gateway ModelProfile)

Static per-family caching facts consulted by the link decision (FR-010). Additive fields on the existing `ModelProfile`.

| Field | deepseek | minimax | glm/generic |
|---|---|---|---|
| `ContinuationLinking` | supported | supported | digest-only (degrade, R-D12/IX) |
| `CacheMinPromptTokens` | 0 | 512 (existing) | 0 |
| `PrefixIdentity` | byte-identical from token 0, 64-token blocks (R-F19) | tool-list → system → messages (R-F20) | unknown — conservative |
| `CacheReporting` | hit/miss reported | cached_tokens, miss derived (existing MissDerived) | fallback dialect |
| `EffortPinned` | per P3 probe outcome (R-D12) | n/a (reasoning_split constant) | conservative: pin |

## TaskStats / Settings extensions (contract layer)

- `TaskStats.Links []LinkOutcome` — per dispatch: predecessor, decision, form, reason, verification share, inherited-vs-reread counts (FR-015).
- `TaskStats.ReadGate {Denied, Waived, Exempt int}` — role-separation telemetry (R-D8).
- `Settings.ContextLinking string` — `default` / `off` (kill switch; `off` = today's behavior, FR-017). Config key registered in the wiring inventory.
- Bench JSON: `links` array + per-pin spend attribution replacing the T032 placeholder (R-D13).

## Persistence & migration summary

| Store | Change | Migration risk |
|---|---|---|
| `sessions/<id>/agents/*.json` | new sidecar family | additive; absent = no linkable history = today's behavior (FR-017) |
| sidecar allowlist regex | extend | additive |
| `plan_state.json` | none | — |
| SQLite events | run_summary gains `terminalShape` field | additive JSON field, old rows parse unchanged |
| goldens | handoff relocation (R-D6) + `read_plan` tool | one reviewed cache epoch, regenerated once |
