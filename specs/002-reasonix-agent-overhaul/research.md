# Phase 0 Research: Reasonix Forensic Analysis & Mechanism Inventory

**Feature**: `002-reasonix-agent-overhaul` · **Date**: 2026-07-12
**Reference tree**: `C:\Users\mydwa\Downloads\DeepSeek-Reasonix-main-v2\DeepSeek-Reasonix-main-v2` (all Reasonix `file:line` below are into that tree; MuhiyaCode `file:line` are into this repo at the reviewed working tree)
**Inputs**: two dedicated forensic traces (request-assembly path; agent-loop/context-lifecycle internals), the prior 7-area extraction, `IMPLEMENTATION_REVIEW.md`, and the four verification audits of the current MuhiyaCode tree.

---

## 1. Where the 98–99% actually comes from (the answer to "the trick")

There is no single trick. Reasonix's own design note states the invariant (`REASONIX.md:14-16`): *"the system-prompt prefix (base prompt + tools + memory) must stay byte-stable across turns so DeepSeek's automatic prefix cache stays warm. Never mutate it mid-session — ride the turn tail instead."* Everything else is enforcement. Ranked by measured leverage, with the single most load-bearing source for each:

1. **Append-only prompt with a cache-reset ladder, not a rolling summarizer.** The prefix only grows until a fixed-token trigger; soft-notice (0.5) → snip (0.6) → prune (0.8) → summarize-fold are each deferred as long as possible, and the paid fold is skipped entirely if pruning clears the trigger. `internal/agent/compact.go:100-130` (ladder), `compact.go:19-23` (fixed-token, not fractional, tail budget).
2. **Compose-once prefix, frozen at boot.** System prompt assembled once (`internal/boot/boot.go:258-326`) into `Messages[0]`; tools canonicalized once at registration and rendered alphabetically every turn (`internal/tool/tool.go:177-192, 294-315`). Steady-state request N+1 = request N byte-for-byte + appended turn messages — proven by their own e2e test (`internal/agent/cachehit_e2e_test.go:166-172`, prefix compared with `bytes.Equal`).
3. **Persisted environment/probe snapshots (24 h TTL + flap-merge)** so the environment section inside the prefix renders byte-identically across launches; a transiently failing probe merges against the last good observation instead of rewriting the prefix. `internal/environment/probe.go:96-124`, `snapshot.go:26`, `mergeProbeSnapshot probe.go:114-121`.
4. **Rewrite-version gating: a maintenance pass that changes nothing never bumps the rewrite version** — no-op passes cost zero cache. `internal/agent/prune.go:110-114`, `internal/agent/session.go:174-179`.
5. **Resume reloads persisted bytes** (JSONL transcript, fast-path normalize returns the same backing array for well-formed histories); a differing fresh system prompt only *warns* ("conversation prefix cache will miss"). `internal/agent/save.go:1113-1163`, `normalize.go:20-26`, `desktop/session_prompt.go:27-52`.
6. **All volatility rides the newest user turn as strippable tagged blocks** (`<memory-update>`, `<background-jobs>`, `<hook-context>`, `<active-goal>`, plan marker, language prefs, `<capability-route>`), applied by one `compose()` chokepoint; in steady state the user text is sent with **zero wrappers**. `internal/control/input.go:153-207`, strip set `internal/agent/preview.go:9`.
7. **One-miss-per-steer acceptance**: a mid-turn steer is exactly one injected user message; the single miss is tolerated by design. `internal/agent/agent.go:1113-1118`.
8. **Content-aware stale-result reclamation with per-tool geometry that travels with the tool** (`SnipHinter`, contract-test-enforced) and archival of originals before any rewrite. `internal/agent/prune.go:186-209`, `internal/tool/tool.go:108-122`, `tool/contract_test.go:52-83`.
9. **Compaction geometry that protects meaning and the cache**: pinned system + first user turn (≤ min(1500 tok, 15% window)) + ALL prior digests; fixed-token verbatim tail (16 384, capped at 50% window) aligned off orphan tool results; digests accumulate, never re-summarized; consecutive-compaction latch; mechanical fallback digest so a failed summarizer can't loop. `internal/agent/compact.go:24-36, 377-418, 525-580, 140-146, 687-693`.
10. **Reasoning stored locally, stripped from requests** (except DeepSeek's required empty-key on assistant tool-call turns, emitted uniformly so thinking-mode toggles can't flip settled bytes). `internal/agent/agent.go:1164-1167`, `internal/provider/openai/openai.go:462-477`.
11. **Todos/plan state live host-side, never in the prompt**; plan-approval seeds them via synthetic UI events, and an evidence ledger (not prompt text) gates premature final answers. `internal/agent/agent.go:320-326, 1350-1402`, `control/controller.go:4443-4459`.
12. **Subagents are fully isolated sessions**; only the final answer returns as one tool result; usage events are forwarded for billing only. `internal/agent/task.go:859-883, 932-944`.

**Critical provider fact**: the DeepSeek path (`internal/provider/openai/`) contains **no cache_control/ephemeral markers anywhere** — the Anthropic-adapter breakpoints from the earlier survey do **not** apply to the DeepSeek wire. For MuhiyaCode (DeepSeek-only via gateway) the entire game is byte-prefix stability. Reasonix also sends **no session/sticky header** (it talks to DeepSeek directly); MuhiyaCode's `X-Muhiya-Session` pin is a MuhiyaCode-specific *addition* required by its multi-model gateway, and is kept.

## 2. Reasonix request-assembly anatomy (condensed; full trace in agent reports)

- **System prompt order** (each section `"\n\n"`-joined, built once at `boot.go:258-326`): base prompt (~660 B const, includes frugality + todo discipline) → output style → UserDecisionPolicy (const) → LanguagePolicy (const) → `Current workspace: "<root>"` line → optional token-economy block → `## Environment` (persisted probe snapshot; capped 24 entries/list) → `# Memory` (persisted files; staleness discipline text) → `# Skills` index (≤4000 chars; bodies never in prefix). Typical total 3–8 KB. Boot determinism is guarded by `boot/prompt_stability_test.go:18-59` (two Builds ⇒ byte-identical prompt).
- **Wire builder** (`openai.go:450-569`): re-marshals typed structs; `SanitizeToolPairing` fast-path returns the same backing array for well-formed histories; assistant tool-call turns get `content: null` + uniform `reasoning_content` key for DeepSeek; **no `tool_choice` field exists**; `max_tokens` omitted; temperature session-stable; headers static.
- **Tools**: canonicalized once at `Add` (sorted `required`, map-key ordering via `json.Marshal`), rendered **alphabetically** every turn; MCP placeholders registered at boot from an on-disk schema cache and never swapped mid-session; plan mode never filters the array.
- **Tool results**: raw output, verbatim, **zero wrapper text** (`agent.go:1234-1242`); 32 KB head+tail cap applied at execution (`agent.go:36`).

## 3. Reasonix context-lifecycle anatomy (condensed)

- **Constants** (`compact.go:24-36`): soft 0.5 · snip 0.6 · compact 0.8 · force 0.9 · target 0.5 · tail 16 384 tok · minRecentKeep 2 · pin-first-user ≤ min(1500 tok, 0.15·window) · minPruneBytes 1024 · minFoldTokens 400 · summary timeout 90 s (one retry, then mechanical digest).
- **Staleness** = older than the token-bounded protected tail (16 384 tok), not age-in-turns. Pinned: system, pinnable first user turn, prior digests, error-marked results (KeepErrors), whole tool-call groups.
- **Geometry defaults**: read-only `{head:80, tail:12, headChars:10000, tailChars:2000}` · side-effecting `{head:40, tail:40, headChars:8000, tailChars:8000}`; per-tool overrides (read_file 120/12, grep 80/8, bash 40/40 …) via `SnipHinter` on the tool itself.
- **Archival**: before any rewrite, originals appended to `<archiveDir>/<timestamp>.jsonl`, one full message JSON per line; placeholders embed the archive path and byte counts.
- **Token estimation**: calibrated tokens/char from the last real usage (`PromptTokens / chars`, accepted in (0.05, 2), fallback 0.25); messages estimated with +4/message and +8/tool-call framing.
- **Digest**: summarizer call on the executor's own provider, no tools, fixed heading contract ("Standing facts & constraints / Goal / Decisions & rationale / Files & code / Commands & outcomes / Errors & fixes / Pending & next step"); spliced as ONE `RoleUser` message wrapped in `<compaction-summary>` tags; `Replace` + `IncrementRewrite`.

## 4. MuhiyaCode current-state delta

Verified equivalent already (do not rebuild — evidence from this repo's audits): compose-once system prompt with zero dynamic values (`internal/orchestrator/prompt.go`, untouched by the last round); deterministic tool marshaling and fixed composition order with sorted MCP block (`engine.go sessionDefinitions`, `registry.go:112-127`); MCP pinned surface + boundary-only swaps (`engine.go:1041-1056`); tail-riding goal/plan/brief blocks (`engine.go:575-588`); settled-byte prefix guard, now hash-based (`prefixshape.go:95`) — *stronger* than Reasonix's rewrite-version proxy; raw tool results with no wrapper (`engine.go:689` region); reasoning-replay decoupled from live effort (`gateway/provider.go:207-210`); session routing pin `X-Muhiya-Session` with per-stream suffixes (`provider.go:151-153`, beyond Reasonix); probe persistence with last-good merge for web_search (`state/toolcache.go:105-158`); subagent isolation with usage-only reporting (`subagent.go`); restart determinism test (`restart_determinism_test.go`).

Genuinely missing vs Reasonix (the adoption set): the reclamation **ladder semantics** (soft-notice band, snip-before-compact ordering, prune-then-skip-summarize, estimate-before-mutate — REV A1 is precisely a violation of Reasonix trick #4), per-tool snip geometry + archival, compaction pinning/digest-accumulation geometry, calibrated token estimation, static cache-discipline prompt text, session hit-rate readout, and the steady-state zero-overhead tail guard.

## 5. Key decisions (Decision / Rationale / Alternatives)

- **Decision**: Keep dispatch-time enforcement for plan mode; never filter the tools array by mode.
  **Rationale**: Reasonix does exactly this (`planmode/policy.go:13-14`); the tools array is part of the cached prefix; the review already validated MuhiyaCode's implementation of this pattern.
  **Alternatives considered**: per-mode tool filtering — rejected: guaranteed cache bust per toggle; contradicts Constitution III/IV and the reference.
- **Decision**: Keep MuhiyaCode's per-task classification brief (≈38 tokens) on the task's first turn, i.e. do NOT chase Reasonix's literal zero-wrapper steady state; budget plain follow-ups at ≤ ~50 system tokens.
  **Rationale**: the brief drives MuhiyaCode's effort/budget levers, which Reasonix implements differently (config-level); its cost is bounded and tail-positioned. Recorded as an intentional deviation (FR-008).
  **Alternatives**: dropping the brief (loses effort routing); moving classification into the system prompt (would make the prefix dynamic — forbidden).
- **Decision**: Map MuhiyaCode's existing fold/trim machinery onto the Reasonix ladder (soft 0.5 / reclaim 0.6 / prune≥0.8-before-compact / compact 0.8, force 0.9) rather than building a parallel pruner.
  **Rationale**: Constitution VIII (improve in place); `history.Maintain` already owns rewrites and invalidation events; only its scheduling, geometry, yield-gating, and archival change.
  **Alternatives**: verbatim port of `prune.go` as a new subsystem — rejected: duplicates ownership of history rewrites; two writers to settled bytes is how the A1 class of bug happens.
- **Decision**: Adopt Reasonix's fixed-token protected tail (16 384 tokens, capped at 50% of window) and calibrated tokens/char estimation.
  **Rationale**: fixed-token tails are why huge windows compact rarely while small windows still work (`compact.go:19-23`); calibration self-corrects across languages without a tokenizer dependency.
  **Alternatives**: fraction-of-window tail (re-compaction loops on big windows); embedded tokenizer (new dependency, forbidden).
- **Decision**: Keep `tool_choice:"auto"` as an explicit, session-stable field (do not delete it to mimic Reasonix's absent field).
  **Rationale**: it is byte-stable today and the gateway path is validated with it; deleting saves ~6 tokens once but risks provider-default drift through the gateway chain.
  **Alternatives**: omit the field (Reasonix-identical) — deferred; revisit only with a gateway-verified test.
- **Decision**: Stream-recovery-without-double-append is **investigate-then-adopt** (Phase E7), not a blind port.
  **Rationale**: MuhiyaCode already has retry byte-identity (`provider.go:80-101`) and SSE robustness; the review found no double-append defect. Adopt Reasonix's partial-text/partial-tool recovery contract only if a reproducible gap is demonstrated.
  **Alternatives**: full port now — rejected: touches the most fragile loop code without an evidenced defect (Constitution I).
- **Decision**: Evidence-ledger final-readiness gating is **rejected** for this feature; only its plan-completion-integrity core is adopted (the intercept-once rule, REV B3).
  **Rationale**: MuhiyaCode's governor + plan-continue + intercept-once achieve the user-visible behavior; a receipts ledger is a large new subsystem out of scope (Constitution VIII, spec scope).
- **Decision**: Reasonix mechanisms with no MuhiyaCode counterpart feature (memory files/compiler, hooks, background jobs, capability routing, output styles, language preference blocks) are **not-applicable**, but their shared pattern — every such feature rides the tail as a strippable tagged block from ONE compose chokepoint — is codified in the request-assembly contract's closed tail-tag set.
  **Rationale**: the pattern, not the features, is what protects the cache; future features get a rule instead of a rediscovery.

## 6. Mechanism Inventory (FR-007/FR-008 — the complete decision matrix)

Areas: P=prompt structure · O=orchestration · C=context mgmt · K=caching strategy · T=tool execution · S=session handling · M=memory/steering · E=token efficiency. Decisions: **ADOPT** / **ADAPT** (with deviation) / **REJECT** / **PRESENT** (already equivalent, evidence) / **N/A** (provider or feature difference).

| # | Mechanism (Reasonix source) | Area | Decision | MuhiyaCode landing / evidence |
|---|---|---|---|---|
| 1 | Compose-once boot system prompt, fixed section order (`boot.go:258-326`) | P | PRESENT | `prompt.go` (verified untouched, zero dynamic values); order codified in request-assembly contract |
| 2 | Base-prompt frugality + todo discipline text (`config.go:1517-1526`) | P | ADOPT | static cache-discipline section in `prompt.go` (REV A4.1); one-time upgrade break, then byte-stable |
| 3 | Boot prompt byte-stability test (`boot/prompt_stability_test.go:18-59`) | P | PRESENT (extend) | `prompt_stability_test.go` — extend to cover the new section |
| 4 | Persisted env-probe snapshot, 24 h TTL + flap-merge (`probe.go:96-124`, `snapshot.go:26`) | P/K | PRESENT (pattern) | web_search probe store `state/toolcache.go:105-158` (fingerprint + last-good merge); MuhiyaCode renders no other env probes — audit confirms in Phase C |
| 5 | Memory block folds into prefix at boot; mid-session edits ride `<memory-update>` tail (`boot.go:305-307`, `input.go:179-191`) | M | N/A (pattern codified) | no memory feature; tail-tag rule in request-assembly contract §4 |
| 6 | Skills index in prefix, capped 4000 chars, bodies never in prefix (`index.go:12,55-61`) | P | ADAPT | verify MuhiyaCode skill surfacing renders deterministically + capped; add guard in Phase C audit |
| 7 | Tools canonicalized once at Add; sorted `required`; map-key JSON ordering (`tool.go:177-192`, `schema_canonicalize.go`) | T/K | PRESENT | `registry.go` definitions built from sorted/deterministic maps (marshal-determinism tests); ordering guard test added Phase C |
| 8 | Alphabetical tools rendering every turn (`tool.go:294-315`) | T/K | ADAPT | keep MuhiyaCode's fixed composition order (base → synthetic → subagent → exit_plan_mode → sorted MCP): equally deterministic; alphabetical re-sort would break every existing session's cache once for zero gain — deviation recorded |
| 9 | MCP lazy placeholders from on-disk schema cache at boot; never swapped mid-session (`plugin/lazy.go`, `boot.go:487-517`) | T/K | PRESENT | pinned surface + `TakeBoundaryChange` (`engine.go:1041-1056`); lazy-connect landed (M5), completed by REV D2 |
| 10 | No per-request variance: `max_tokens` omitted, stable temperature, static headers (`agent.go:1835-1839`, `openai.go:797-808`) | K | PRESENT (guard) | steady-state diff conformance test added (contract §6); marshal tests exist |
| 11 | No `tool_choice` field at all (`openai.go:797-808`) | K | REJECT | keep stable `"auto"` (decision above) |
| 12 | DeepSeek `reasoning_content` uniform empty-key on assistant tool-call turns (`openai.go:462-477`) | K | PRESENT | `provider.go:207-210` unconditional emission (F7 fix, review-verified) |
| 13 | Reasoning stored, stripped from requests (`agent.go:1164-1167`) | E | PRESENT | replay strips reasoning; settled bytes effort-independent (marshal test) |
| 14 | `content: null` on pure tool-call assistant turns (`openai.go:484-489`) | K | ADAPT | conform MuhiyaCode wire shape only if it already matches; if it differs, keep current bytes (changing = one-time session-wide bust) — verify in Phase C, document result in contract §3 |
| 15 | Raw tool results, zero wrapper (`agent.go:1234-1242`) | E | PRESENT | `engine.go` tool-result append (audit-verified) |
| 16 | 32 KB per-result head+tail cap at execution (`agent.go:36`, `truncateToolOutput:2892-2907`) | E | ADAPT | align `CapToolOutput` (`registry.go:148-162`) to head+tail with explicit truncation marker if not already; verify geometry Phase C |
| 17 | Append-only session; `Replace`-only rewrites; no in-place message mutation (`session.go:58-72`) | S/K | PRESENT | `history.go` append-only + pressure-gated rewrites (audit-verified) |
| 18 | No-op maintenance never bumps rewrite version (`prune.go:110-114`) | C/K | ADOPT | REV A1: estimate-before-mutate + skip without event/latch — the exact defect fix |
| 19 | Ladder: soft 0.5 notice (latched, zero rewrite) → snip 0.6 → prune ≥0.8 → compact (`compact.go:85-147`) | C | ADOPT | `engine.go` maintenance scheduling + `history.go`; context-lifecycle contract §1 |
| 20 | Prune-before-compact; skip paid summarize if pruning clears trigger (`compact.go:119-130`) | C/E | ADOPT | same landing; force at 0.9 bypasses skip |
| 21 | Fixed-token protected tail 16 384 capped 50% window; boundary aligned off leading RoleTool (`compact.go:525-580`) | C | ADOPT | `history.go` planCompaction-equivalent |
| 22 | Pin system + first user turn ≤ min(1500 tok, 15% window) + all prior digests (`compact.go:377-402`) | C | ADOPT | compaction geometry (REV A4.3) |
| 23 | partitionFold: keep error-marked results, user-marked, small user turns; digests accumulate verbatim (`compact.go:408-466`) | C | ADOPT | same |
| 24 | Digest = single RoleUser `<compaction-summary>` message; fixed heading contract; 90 s + one retry + mechanical fallback; minFoldTokens 400 (`compact.go:55-80, 621-693`) | C | ADAPT | align MuhiyaCode's compaction summary prompt/splice; keep MuhiyaCode's existing invalidation-event emission (richer than Reasonix's version bump) |
| 25 | consecutiveCompacts latch + compactStuck pause (`compact.go:140-146`) | C | PRESENT (verify) | latch exists (review-verified); align reset conditions to "under trigger", REV A1 keeps no-reset-on-dips |
| 26 | Calibrated tokens/char from real usage; (0.05,2) bounds; 0.25 fallback; +4/msg +8/call framing (`compact.go:587-607, 158-189`) | C/E | ADOPT | pressure estimation in `engine.go`/`history.go` |
| 27 | Per-tool `SnipHinter` geometry travelling with the tool; contract-test enforced; defaults RO 80/12/10000/2000, SE 40/40/8000/8000; minPruneBytes 1024 (`prune.go:186-209`, `tool/tool.go:108-122`) | C/E | ADOPT | geometry table on workspace tools (REV A4.2); contract test in Phase E |
| 28 | Archive originals to timestamped `.jsonl` before any rewrite; placeholder embeds path+bytes (`compact.go:742-760`, `prune.go:141-170`) | C | ADOPT | `state/session.go` pruned.jsonl (data-model §4 record) |
| 29 | Cache-shape capture/compare per turn; sorted-schema normalize; log_rewrite reason (`cache_shape.go`) | K | PRESENT (stronger) | `prefixshape.go` hashes actual settled bytes; degraded paths made loud (REV A3/C2) |
| 30 | One-miss-per-steer: single injected user message mid-loop (`agent.go:1113-1118`) | M/O | PRESENT | steering appends tail (`engine.go` steering path, audit-verified) |
| 31 | Stream recovery without double-append; step not consumed (`agent.go:1127-1149`) | O | **REJECT (T045 investigation: no gap)** | `gateway/provider.go:80-101` retries `chatOnce` ONLY when `streamed==false` (no data emitted), so a streamed interruption returns an error rather than re-streaming — no double-append. `engine.go:768-771` returns on a Chat error WITHOUT appending any assistant message, so history is never left with a partial/orphaned turn. No double-append or context-loss corruption is reproducible, so per "adopt only if a gap is demonstrated" no recovery is adopted. Guarded by `TestProviderErrorLeavesNoPartialAssistantMessage`. |
| 32 | maxSteps grace round then pause sentinel (`agent.go:1255-1297`) | O | PRESENT (equiv) | turn governor + isFinal finalize (`engine.go:622-629` region) |
| 33 | Evidence-ledger final-readiness gate (`agent.go:1350-1402`) | O | REJECT (core adopted) | intercept-once plan-integrity rule lands via REV B3 |
| 34 | Host-side canonical todo state; never in prompt; synthetic UI events; rebuild from transcript (`agent.go:320-326, 1457-1608`) | O/S | PRESENT (equiv) | plan steps host-side + `plan.md` sidecar; plan block tail-only in plan mode |
| 35 | Plan-approval seeds todos + synthetic proceed turn (`controller.go:4443-4459`) | O | PRESENT | P2 flow landed (review-verified) + B2 fixes |
| 36 | Plan mode = dispatch-gate refusal, tools array untouched (`planmode/policy.go:13-14`) | T/K | PRESENT | review-verified; P4 read-only-shell passthrough aligns with `decideBash` |
| 37 | Storm breaker keyed (tool, error-class), not args (`agent.go:2213-2327`) | O | PRESENT (complete via REV) | landed H2; B6/B7 complete it (shared gate + all-failed-turns) |
| 38 | Repeat-success guard threshold 2 (`agent.go:2637-2694`) | O | PRESENT | repeat limiter + failed-cache (review-verified; thresholds per dispatch-gate contract) |
| 39 | Subagent = own session/provider stream; only final answer returns; usage forwarded observability-only (`task.go:859-944`) | O/S | PRESENT | `subagent.go` isolation + `recordIsolatedUsage`; B9 adds tokens into parent breaker |
| 40 | Deterministically filtered subagent tool registry; depth-bounded recursion (`task.go:597-619`) | T | PRESENT | allow-list specs + structural no-recursion (audit-verified) |
| 41 | Session cache atomics never reset by compaction; status line "turn hit · avg" (`agent.go:249-256`, `chat_tui.go:2695-2721`) | E | ADOPT | REV A4.5: session rate + turn rate in TUI footer/status from `contract/cache.go` aggregation |
| 42 | Cost formula (hit·CacheHit + miss·Input + completion·Output)/1e6; per-call summation (`provider.go:503-517`, `run_metrics.go:84-88`) | E | ADOPT | readout cost derivation (pricing config) |
| 43 | Usage normalization: top-level DeepSeek fields, nested fallback, derived miss (`openai.go:772-793`) | K | PRESENT | gateway SSE usage parsing (F8 fixed); A2 makes zero/multi-frame tolerant |
| 44 | Resume reloads persisted transcript bytes; fresh-prompt swap warns; compatible-baseline clone (`save.go:1113-1163`, `session_prompt.go:27-52`) | S/K | ADAPT | Phase E6: verify MuhiyaCode resume path replays identical bytes (restart_determinism_test exists); add the "differs ⇒ warn + one-time event" rule on session upgrade |
| 45 | Anthropic cache_control breakpoints (`anthropic.go:306-320`) | K | N/A | provider difference — DeepSeek implicit caching; recorded per spec assumption |
| 46 | Memory compiler, hooks, background jobs, capability routing, output styles, language blocks (`input.go:157-207` et al.) | M/P | N/A (pattern codified) | closed tail-tag allow-list, request-assembly contract §4 |
| 47 | `X-Muhiya-Session` per-stream routing pin | K | — (MuhiyaCode-only, keep) | not in Reasonix (direct-to-DeepSeek); required by gateway; review-verified C1 |

**Coverage check (SC-008)**: all 8 areas covered — P (1-6), O (30-40), C (18-28), K (7-14, 29, 43-45, 47), T (7-9, 27, 36, 40), S (17, 34, 39, 44), M (5, 30, 46), E (13, 15-16, 20, 26-28, 41-43). Every row carries a decision; deviations carry reasons.

## 7. Adopted constants (single source for Phase E)

| Constant | Value | Reasonix source |
|---|---|---|
| soft-notice ratio | 0.5 (latched once, zero rewrite) | `compact.go:24` |
| reclamation (snip) ratio | 0.6 | `compact.go:26` |
| compact ratio / force ratio | 0.8 / 0.9 | `compact.go:27-28` |
| compact target | ≤0.5 window kept | `compact.go:29` |
| protected verbatim tail | 16 384 tokens, capped 50% window, ≥2 messages | `compact.go:31-33` |
| pin first user turn | ≤ min(1500 tok, 15% window) | `compact.go:35-36` |
| minPruneBytes | 1024 | `prune.go:18` |
| minFoldTokens | 400 | `compact.go` (foldEconomics) |
| min maintenance yield (MuhiyaCode addition, REV A1) | 5% of window | — |
| snip geometry defaults | RO 80/12 lines, 10000/2000 chars · SE 40/40, 8000/8000 | `prune.go:186-189` |
| per-tool geometry | read_file 120/12/12000/2000 · grep/glob/ls 80/8/10000/1000 · shell 40/40/8000/8000 | builtin SnipHints |
| tool-result execution cap | 32 KB head+tail | `agent.go:36` |
| tokens/char calibration | usage-derived, bounds (0.05, 2), fallback 0.25; +4/msg +8/tool-call | `compact.go:587-607, 158-189` |
| summary timeout / retry | 90 s / one retry / mechanical fallback | `compact.go:47, 676-682` |

All prior NEEDS CLARIFICATION: none remained (Technical Context had none); the open design questions raised by the spec's assumptions are resolved by decisions §5.
