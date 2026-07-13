# Contract: Context Lifecycle (Reclamation Ladder, Compaction, Archival, Invalidation)

**Feature**: `002-reasonix-agent-overhaul` · **Owner**: `internal/orchestrator` (`engine.go` scheduling, `history.go` operations) + `internal/state` (archive I/O) · **Reference**: research.md §3, §7 (adopted constants); Reasonix `compact.go` / `prune.go`

Settled history is rewritten as rarely and as late as possible; every rewrite is estimated first, archived first, event-paired always. This contract fixes the ladder, the geometry, and the bookkeeping.

## 1. The pressure ladder (evaluated at task boundaries / post-turn, in this order)

Pressure = estimated prompt tokens ÷ model context window, using calibrated estimation (§5).

| Band | Action | Rewrite? | Bookkeeping |
|---|---|---|---|
| < 0.5 | nothing; clear compact-stuck state when under trigger | no | — |
| [0.5, 0.6) | ONE soft advisory notice per session (latched) | **NO — zero history mutation (test-guarded)** | latch flag only |
| [0.6, 0.8) | Reclamation Tier 1 (snip): estimate yield FIRST; if estimated yield < 5% of window → skip entirely (no mutation, no event, no latch slot). Else: archive originals → apply per-kind head/tail geometry to stale eligible tool results → ONE InvalidationRecord | only if estimate passed | maintenance latch: max 2 applied passes; latch resets ONLY on successful compaction or explicit /compact — never on transient pressure dips |
| [0.8, 0.9) | Reclamation Tier 2 (prune) FIRST: archive → elide stale eligible results (upgrade already-snipped) → if resulting pressure < 0.8 → STOP (paid compaction skipped). Else → Compact | yes (event per applied pass) | consecutive-compaction latch |
| ≥ 0.9 (force) | Tier 2 then Compact unconditionally | yes | same |

Rules: (a) an estimate-only pass that skips MUST leave history byte-identical (settled-hash test); (b) mutation and its InvalidationRecord are inseparable — no return path between them (REV A1 defect class is a contract violation); (c) no operation here ever touches the system prompt or tools array.

## 2. Staleness, eligibility, pinning

- **Stale** = older than the protected verbatim tail: a fixed **16 384-token** budget (capped at 50% of window, minimum 2 messages), boundary walked newest→oldest and aligned so the tail never starts with an orphan tool result whose assistant `tool_calls` were rewritten away.
- **Eligible** (Tier 1): `role:tool` messages ≥ **1024 bytes**, not already snipped. (Tier 2): already-snipped (upgrade) or ≥ 1024 bytes.
- **Pinned (never rewritten by any tier or compaction)**: system prompt (not in history); first user turn if ≤ min(1500 tokens, 15% of window); ALL prior compaction digests (verbatim, accumulate — never re-summarized); error-marked tool results (content starting `error:`/`blocked:`); the current unsettled task's messages; the protected tail; tool-call/result pairing (never snip a result away from its assistant call — group-preserving).

## 3. Reclamation geometry (per tool-result kind; constants from research.md §7)

| Kind | Head lines / Tail lines | Head chars / Tail chars |
|---|---|---|
| Read-style results (file reads, searches, listings, diffs, status) default | 80 / 12 | 10 000 / 2 000 |
| file-read results (override) | 120 / 12 | 12 000 / 2 000 |
| search/glob/list results (override) | 80 / 8 | 10 000 / 1 000 |
| Side-effecting results (shell, mutations, MCP) default | 40 / 40 | 8 000 / 8 000 |

Geometry is declared WITH the tool (hint method on the tool definition, defaulted by read-only classification; unknown/detached MCP tools → read-only default) and enforced by a contract test: every built-in tool either declares a hint or is explicitly listed as accepting the default. Single-giant-line results: keep head `min(headChars, len/2)` + tail `min(tailChars, len/4)`.

**Placeholders** (exact, model-facing):
- Tier 1: `[snipped tool result — <name>, <N> bytes archived to <path>; showing first <H> lines and last <T> lines]` + head + `[... <omitted> lines omitted ...]` + tail
- Tier 2: `[elided tool result — <name>, <N> bytes archived to <path>; re-run the tool if the data is needed again]`
- Tier-2 upgrade preserves the ORIGINAL byte count and archive path parsed from the Tier-1 marker.

## 4. Archival (before ANY rewrite)

Append the original messages to the session-local archive `pruned.jsonl` (data-model §4 record shape: task index, turn, tool name, original content, reduced-to, tier, pressure) BEFORE mutating history. Already-snipped messages are not re-archived on upgrade. Compaction archives its folded region the same way. Archives are append-only, session-scoped, and referenced by the placeholder paths.

## 5. Token estimation (no tokenizer dependency)

Tokens/char calibrated from the most recent real usage: `promptTokens / charsOf(messages)` accepted only within (0.05, 2); fallback 0.25 before calibration. Reasoning content excluded from char counts (it is not re-sent). Message framing estimated at +4 tokens/message, +8/tool-call. Text estimation = max(bytes/4-ish, rune count) to stay sane on CJK.

## 6. Compaction

- Fold region = everything between the pinned prefix and the protected tail, minus kept items (§2 pinned + policy-kept), minimum fold economics **400 tokens** (skip below, unless force).
- Digest generation: summarizer call on the session's own provider, no tools, session temperature, **90 s timeout, one retry on non-timeout failure, then a deterministic mechanical digest marker** (region is already archived) so a failing summarizer can NEVER loop or block reclamation.
- Digest content contract (fixed headings): Standing facts & constraints / Goal / Decisions & rationale / Files & code / Commands & outcomes / Errors & fixes / Pending & next step. Identifiers, paths, numbers preserved exactly; nothing invented.
- Splice: `pinned prefix + kept + ONE user-role digest message wrapped in <compaction-summary> tags + verbatim tail`, applied as a whole-history replace, ONE InvalidationRecord.
- Second-and-later compactions MUST leave earlier digest messages byte-identical (accumulation test).
- Consecutive-compaction latch: two compactions without an intervening under-trigger turn ⇒ compaction pauses with a one-time notice (window too small); cleared when pressure lands under trigger.

## 7. Interaction rules

- The prefix-shape guard treats every ladder mutation as explained ONLY via its paired record; the no-op estimate path must be invisible to the guard.
- Reclamation/compaction run only between turns of the owning stream; never concurrently with an in-flight request; never on subagent streams (subagent runs are short-lived isolated sessions).
- The efficiency readout continues to aggregate across rewrites (session counters never reset on compaction).

## 8. Conformance tests (minimum set)

1. Soft band performs zero mutation (settled hash unchanged, no event, notice at most once per session).
2. Low-yield estimate skips: byte-identical history, no event, no latch consumption (REV A1 regression trap included: trim-only band change ⇒ next Run does NOT fail the shape guard).
3. Oscillation across 0.6: at most 2 applied passes; every applied pass has exactly one record.
4. Tier-2-clears-trigger: compaction summarizer NOT invoked; force 0.9 invokes it regardless.
5. Geometry: read-style vs side-effecting shapes; 1024-byte floor untouched; single-giant-line split; pairing preserved; error-marked results reach compaction verbatim.
6. Archival: pruned originals recoverable verbatim from `pruned.jsonl`; placeholder paths resolve; upgrade preserves original byte count.
7. Compaction: pinned first user turn survives; digests accumulate byte-identically; mechanical fallback on summarizer timeout; latch pauses after 2 consecutive.
8. Calibration: estimation within sane bounds on ASCII and CJK fixtures; fallback used before first usage.
