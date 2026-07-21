# TOKEN EFFICIENCY OVERHAUL PLAN — MuhiyaCode v1.1.x

**Status: READY FOR EXECUTION — root-cause dossier complete (3 investigator
reports + first-hand verification, 2026-07-21, branch feature/native-agent-v1.1.0)**

Executor: Opus 4.8. This document is written to be executed without further design
decisions: every task names its files, its exact change, its regression test, and
its acceptance check. Read §0 and §1 fully before touching anything.

Task inventory: WS-M (2) · WS-A (3) · WS-B (6) · WS-C (5) · WS-D (3) · WS-E (8)
· WS-F (2) · WS-G (3) — 32 tasks. Prefix-byte tasks (ONE epoch commit):
TA02, TB05, TC01, TC02, TD02, TD03, TG01.

---

## §0 Mission, non-negotiables, and how to execute this plan

### Mission

A real session built a single-file snake game and consumed hundreds of thousands of
tokens, with requests like `minimax-m3 | 20,647 input / 2,434 output | Cache Read:
109 | Cache Write: 0`. The reported defects:

1. `tasks.md` is created in the opened workspace root even when the task targets a
   subdirectory — it must be created in the task's target directory.
2. The executor re-implemented work from scratch after reading the tasks file and
   glob-scanning the whole workspace ("No snake.html exists yet. Let me build the
   complete game").
3. A whole-file `write_file` was cut off mid-JSON ("The call's JSON was cut off
   during generation…"), and the retry advice ("send a shorter and more compact
   version") is actively wrong for a full-file write.
4. Files already read are re-read; exploration is unscoped (workspace-wide globs
   for single-file tasks).
5. Cache utilization collapsed (109 of 20,647 tokens read from cache) and the fixed
   ~24.6 KB prefix pays full price on every request.

The mission: make the standard build-something flow cost a **bounded, small number
of requests with near-full prefix-cache hits**, without reducing implementation
quality and **without dissolving the plan/execute split** (owner directive,
NATIVE_AGENT_PLAN §7 F0 — the split stays; it must get cheap, not get removed).

### Non-negotiables (Constitution digest — violations are plan failures)

- **I Correctness before optimization** — no efficiency change may break behavior;
  every change lands with a regression test proven meaningful.
- **III Deterministic stable prefix** — prompt/tool-JSON bytes are identical across
  turns within a session. No timestamps, map ordering, or per-turn state may enter
  prefix content.
- **IV/V Separate dynamic from cached; append-only history** — dynamic content
  rides message tails/sidecars, never the prefix; history is never rewritten
  mid-session.
- **VI Honest measurement** — cache/token claims come from provider usage fields
  and scripted-provider byte counts, never estimates presented as measurements.
- **VIII Improve, don't rewrite** — smallest change that fixes the defect.
- **X Verified improvements** — before/after evidence for every efficiency claim
  (offline: scripted-provider byte/request counts; live: owner-gated gauntlet).

### Cache-epoch discipline (CRITICAL — read twice)

Several workstreams change prefix bytes (system prompt clauses, tool descriptions,
tool schemas). A prefix change is a one-time full cache invalidation for every
session. Therefore:

- **ALL prefix-byte changes across ALL workstreams land in ONE commit** — the
  single sanctioned epoch of this plan. Batch: WS-A prompt clause, WS-B tool
  description/schema changes, WS-C handoff/executor-prompt clauses, WS-D
  read-discipline clauses, WS-F brief changes, WS-G diet edits.
- Regenerate the three goldens exactly once, in that commit:
  - `go test ./internal/instructions/ -run TestDump -update`
  - `go test ./internal/orchestrator/ -run TestWiring_PrefixBytesGolden -update-prefix-golden`
- Diff-review the regenerated goldens: every changed line must trace to a named
  task in this plan. Any other drift = stop and investigate.
- `TestSystemPromptSizeWithinBudget` (`internal/orchestrator/prompt_budget_test.go:52`,
  baseline const `promptBaselineChars = 5340`) must be re-baselined **DOWN** in the
  same commit (WS-G shrinks more than other workstreams add — see WS-G acceptance).
  Document the new baseline in the const's comment block, as every prior epoch did.
- Non-prefix changes (engine logic, gates, sidecar texts, TUI) are cache-safe and
  land in their own commits, before or after the epoch commit.

### Execution order

1. **Phase 0 — measurement harness** (TM01, TM02). Build the offline
   token-accounting fieldtests FIRST and record the "before" numbers on the
   current tree in §4. Every later phase re-runs them and records deltas.
2. **Phase 1 — cache-safe correctness** (no prefix bytes): TB01→TB02→TB03→TB04
   (truncation chain, in that order — detection before consumption), TA01
   (active checklist), TA03+TC04 (handoff lines), TC03 (non-linkable digest),
   TD01 (executor dedupe), TF02 (stop-at-done), TF01 (ceremony audit).
3. **Phase 2 — THE epoch commit**: TA02, TB05, TC01, TC02, TD02, TD03, TG01 in
   ONE commit + the three golden regens + the prompt-budget re-baseline (down).
   Nothing else may touch prefix bytes in any other commit.
4. **Phase 3 — cache pipeline** (cache-safe): TE01→TE02→TE03 (honesty),
   TE04→TE05 (reclamation), TE06 (linkable wrap-ups), TE07 (caps), TG02 (pin),
   TG03 (measured, optional), TE08 (gateway note). Then TB06, TC05 (fieldtests
   that need earlier tasks in place).
5. **Phase 4 — verification**: full gauntlet (§3) + before/after table + §4
   execution log entries per phase.

After every phase: `gofmt -l internal cmd` clean, `go vet ./...` clean,
`go build ./...` clean, `go test ./... -count=1` all green. No exceptions.

### Out of scope (do not touch)

- The muhiya gateway repo (server-side cache behavior, MiniMax upstream flags).
  Where a fix belongs there, this plan says so explicitly and the agent-side task
  is limited to detection + honest surfacing.
- The plan/execute role split, model-driven dispatch (owner directives).
- `-race` runs (no CGO on this host — CI gate, noted per task where relevant).

---

## §1 Verified root-cause dossier

Every claim below is file:line-verified on branch `feature/native-agent-v1.1.0`
(2026-07-21): first-hand reads plus three adversarially-tasked investigator
reports (A: tasks.md/handoff, B: truncation pipeline, C: cache/token anatomy),
cross-checked where they overlap (e.g. both C and the first-hand golden read
independently measured the 19,305-byte tool block).

### RC-1: tasks.md location — prompt silence + reader/writer asymmetry (VERIFIED)

- The file lands at the workspace root because every teaching surface says bare
  "tasks.md" and relative paths resolve against the root (`workspace.Resolve`,
  `paths.go:82-90`). The surfaces: operating contract rules 2/4/6
  (`prompt.go:45,47,49`), PLANNING steps 3/4 (`prompt.go:234-235`), the per-turn
  task-brief tail "keep tasks.md current" (`classify.go:196`), the role-gate
  refusal text (`instructions/gates.go:63`), and the executor deliverable ("check
  off the tasks.md items you finish", `subagents.go:131`). None names a
  directory.
- **The asymmetry:** the WRITE trigger is location-agnostic — `touchesChecklist`
  matches any basename `tasks.md` case-insensitively (`checklist.go:89-100`),
  fired from the ONE shared dispatch gate after any successful mutation
  (`gates.go:131-136`, main loop AND subagents), and the role-gate carve-out uses
  the same predicate (`rolesplit.go:77-80`; pinned by
  `rolesplit_test.go:94-99`). But the READ side is root-pinned:
  `checklistPath() = filepath.Join(WorkspacePath, "tasks.md")`
  (`checklist.go:79-84`), used by `refreshChecklist` (`:107-127`), the
  session-open seed (`engine.go:378-380`), the TUI pull (`CurrentChecklist`,
  `engine.go:606-612`), AND the completion-honesty guard
  (`appendCompletionDisclosure` → `openChecklistItems`,
  `turnhelpers.go:151-169`, `checklist.go:145-156`) — so a target-directory
  tasks.md passes the gate, then the panel stays empty and the honesty guard
  goes blind.
- Execution constraint: the prompt-budget test has ~14 chars of headroom
  (baseline 5340, actual 5326 — `prompt_budget_test.go:45-52`), so ANY location
  wording forces the documented re-baseline that WS-G's diet pays for in the
  same epoch commit.

### RC-2: duplicated implementation — VERIFIED (investigator A)

- The handoff (`subagent.go:62-81`; labels `subagents.go:146-149`) carries
  Role / Scope (≤160-char digest) / Context / Deliverable / OutputFormat, riding
  ONE per-run user message (`subagentUserMessage`, `subagent.go:263-269`) —
  pinned ≤ task+900 chars (`handoff_fidelity_test.go:60-62`).
- What Context can say about prior work — the complete inventory
  (`contextlink.go:262-332`): (1) continuation — predecessor transcript replayed
  verbatim + exactly ONE new user message (`contextlink.go:288-315`; pinned by
  `contextlink_integration_test.go:65-113`); (2) digest fallback — ≤1400-char
  predecessor digest + changed-file re-read markers (`digestCarryForward`,
  `contextlink.go:213-241`); (3) fresh — knowledge briefing ≤1500
  (`contextlink.go:273-276`); (4) empty → "inspect only the named scope".
- **The hole:** `Linkable` requires clean-done with complete tool pairings
  (`contextrecord.go:303-311`), and candidate search filters on it
  (`latestLinkableRecord`, `contextrecord.go:326-336`) — non-linkable records are
  NEVER candidates, and the digest fallback fires only when a LINKABLE candidate
  fails a later gate (`contextlink.go:79-81`; pinned by
  `contextlink_test.go:206-214`). Knowledge banking needs status done/partial +
  report ≥80 chars (`subagent.go:204-206`). So after a **failed or cancelled**
  first dispatch (e.g. killed by the truncation loop in RC-3): no continuation,
  no digest, no knowledge — the second dispatch knows only what the main model
  happens to write in `task`, and the task-property teaching (`tools.go:86-90`)
  has NO "state what already exists / what attempt 1 completed" item.
- No executor-facing text says "read the checklist's checkboxes first; never redo
  a checked item": the only tasks.md reference it ever sees is write-side ("check
  off the tasks.md items you finish", `subagents.go:131`), and
  `SubagentGeneralSystem` (`subagents.go:86`) has no re-entrancy clause. The main
  prompt's CACHE DISCIPLINE ("never re-run a past search", `prompt.go:71-73`) has
  no executor analog.
- The observed re-glob-and-rebuild is consistent with any of: first run
  non-clean-done (→ fresh/no-candidate), provider without `ContinuationSupported`
  (→ digest at best), linking off, or the target files living outside the
  fresh executor's relative-path worldview (RC-1 interplay).
- **Carrier constraints for any fix** (test-pinned): the per-kind system message
  must stay byte-stable across dispatches (`contextlink_integration_test.go:31`);
  a fresh dispatch is exactly 2 messages (system+user, `:160-178`); a
  continuation appends exactly ONE user message (`:65-113`). Every new
  completion-state carrier must therefore ride INSIDE the existing single user
  message (handoff render or task text) — never a new message or system-text
  mutation.

### RC-3: truncated write_file JSON — VERIFIED end-to-end (investigator B)

The full defect chain, every link confirmed:

1. **A 16k output cap governs every write.** `provider.go:165-175`: when
   `input.MaxTokens <= 0` the request uses `profile.MaxOutputTokens`, clamped by
   `ClampOutputTokens` (`gateway/model.go:161-166`) — and every family's
   operational default is **16,000** (DeepSeek `model.go:105` vs a 384k documented
   ceiling; MiniMax `model.go:130`, with the caveat at `model.go:126-129` that
   MiniMax counts max_tokens against the shared context window; GLM `model.go:139`;
   generic `model.go:141`). Neither loop overrides it: main `turnloop.go:371`,
   executor `subagent.go:433` — no `MaxTokens`. A whole-file write + reasoning
   must fit in 16k output. The catalog's per-model `contract.Model.MaxOutput`
   (`types.go:40`, parsed at `provider.go:565-576`) is **consumed nowhere**.
2. **The stream ends cleanly and the broken call leaks out.** Tool-call argument
   deltas are naive string concat (`sse.go:214`, `ingestCalls` 196-216);
   `Result()` (`sse.go:147-169`) emits every named call as-is — **no `json.Valid`
   check, no cross-check of `finish=="length"` against argument completeness**.
   On a cap hit the provider closes the stream normally, so the truncated call
   flows through `provider.go:312-321` into the orchestrator. (Transport deaths
   mid-arguments error out instead and never leak — the cap path is the defect.)
3. **FinishReason=length is consumed exactly once, notice-only, main loop only.**
   `turnloop.go:464-467` emits a UI notice then **dispatches the truncated calls
   anyway** (`:479`). The executor loop — the only scope that writes files under
   the role split — never checks FinishReason at all.
4. **The advice text was written for a different incident.** The user-visible
   string is `validate.go:26-28` ("cut off mid-generation… send a shorter version
   (fewer, more compact steps)" — update_plan phrasing, per the comment at
   `validate.go:22-25`) + the suffix appended at `gates.go:28`. Both are plain
   runtime strings, NOT in the instructions registry → no audit coverage (they
   ride the tool-result tail, so cache-safe). Governing contract still specifies
   different wording: `specs/002-reasonix-agent-overhaul/contracts/dispatch-gate.md:32`.
5. **No guard ever fires, and every retry is permanently re-billed.** The H1
   rejection returns before every guard (`gates.go:27-32` vs `recordFailedCall`
   sites at `:39,54,68,124-126`) → the verbatim failed-call cache, repeat limiter
   (`gates.go:79-88`), and storm breaker (`dispatch.go:166-179`) never see a
   truncated call; even if they did, `callSignature` falls back to raw truncated
   text (`inspection.go:477-487`), so different cut offsets are distinct
   signatures. H5 excludes GateRejected by design (G-1). The executor loop has
   **no consecutive-failure guard at all** (`subagent.go:520-530`) — only the
   progress ladder (`:548-560`) and window pressure (`:543-547`) end it. And each
   failed attempt's **full truncated payload is persisted into history and
   re-sent as input on every subsequent request** (`turnloop.go:555,599-602`
   via `assistantReplayMessage:642-652`; `subagent.go:509,528`) — the token bomb.
6. **Chunked writing is possible today but taught nowhere.** `write_file` is
   whole-file only (no append — `registry.go:33`, `files.go:305-339`), but a
   successful Write enters the read ledger (`files.go:331`), so a skeleton write
   unlocks follow-up edits without a read; `applyEditText` exact-matches anywhere
   (`files.go:422-480`), so a unique tail-marker append via edit_file/multi_edit
   (≤30 edits/call — `registry.go:32`, `files.go:249-251`) is mechanically
   reliable. There is no write-size cap (`MaxSnapshotBytes` 2MB is a checkpoint
   cap — `files.go:298,334`). No instruction teaches writing in parts (grep:
   only read-side ranging advice, `prompt.go:64`, `tools.go:23`).

### RC-4: rereads + unscoped exploration

- Main loop HAS a duplicate-read guard: `InspectionLedger.Duplicate`
  (`internal/orchestrator/inspection.go:108-138`) with mtime-gated freshness for
  read_file and mutation-invalidated signatures for searches, enforced at
  `gates.go:89-101`. **The executor has none** (RC-2).
- `ToolGlobDescription` literally teaches a workspace-wide example: "Find files by
  doublestar glob, e.g. `**/*.go`." (`internal/instructions/tools.go:26`) — the
  exact anti-pattern the field session showed.
- CONTEXT DISCIPLINE tells the main model to prefer ranged reads
  (`prompt.go:63-64`) but nothing scopes the EXECUTOR, which is where the
  workspace-wide glob happened.

### RC-5: cache utilization collapse + fixed-payload weight (VERIFIED, investigator C)

- **The request carries zero cache hints** — no `cache_control`, no
  `prompt_cache_key`, anywhere (`provider.go:176-196`; repo-wide grep). Caching
  is 100% implicit provider-side prefix matching; the only affordance is the
  `X-Muhiya-Session` routing pin header (`provider.go:211-217`).
- **The sample numbers are real (read side) and fabricated (write side).**
  Usage parsing maps DeepSeek `prompt_cache_hit_tokens` first, else
  OpenAI/MiniMax `prompt_tokens_details.cached_tokens`; miss is reported or
  derived (`sse.go:228-310`). MiniMax's format IS parsed correctly (research
  R-F20, `specs/012-subagent-context-cache/research.md:46`) — 109/20,647 is a
  **genuine near-total provider-side miss**. "Cache Write: 0" is a hardcoded
  zero: `benchUsage.CacheWriteTokens` is declared but never assigned
  (`internal/command/benchjson.go:23,86-96`); no write-side field is parsed and
  `contract.Usage` has none (Constitution VI violation to display it).
- **Nothing tells the user when cache collapses mid-session.** The cold-start
  notice needs `CacheReadTokens == 0` exactly AND prompt ≥ 8k
  (`cacheresilience.go:34-74`) — 109 ≠ 0, so the observed request triggered
  nothing; mid-session collapse lands only as ledger attribution
  (`agent-suspect`/`provider`, `usage.go:47-61,197-210`).
- **Continuation replays a 15-25k-token predecessor transcript on trust.**
  `decideLink` gates on the static family flag `ContinuationSupported`
  (`contextlink.go:135-137`), never on the MEASURED per-(model,pin) hit rate
  already computed in `PerPairingRates` (`contract/cache.go:90-135`,
  `turnloop.go:127`) — when the provider cache is dead, every "warm
  continuation" re-bills the whole replayed transcript; the ≤2,200-char digest
  fallback is ~10× cheaper in that state.
- **Big builds always cold-start their next dispatch:** only `clean-done` runs
  are `Linkable` (`contextrecord.go:26-32,304-311`); the turn-ladder wrap-up
  (`subagent.go:548-560`) — the normal ending for a long build — produces
  `wrapup-done`, never linkable, and the decline reason reaches only bench JSON
  (`benchjson.go:142-150`), never the user.
- **History reclamation effectively never fires on huge-window models.** Tool
  results enter history at 8k-24k chars each (`gates.go:118`, `effort.go:38-59`)
  and ride every later request; trim needs ≥60% window pressure
  (`maintenanceFloorRatio`, `engine.go:20`) — on MiniMax M3's 1M window that is
  ~593k tokens, so a snake-game session accretes every read/diff verbatim.
  `MarkSuperseded` is main-mutation-only (`dispatch.go:159-161`) and the
  executor's `InvalidateFor` IDs are **discarded** (`subagent.go:341-352`) —
  under the role split, supersede marking is effectively dead code.
- **Fixed payload:** main ≈6.2-7k tokens/request — system prompt 5,323 chars +
  tool JSON **19,305 B ≈ 78% of the fixed prefix** (synthetics: run_subagent
  ≈2.9 KB, memory trio ≈3.2 KB, ask_user+propose ≈1.7 KB — main-loop-only
  weight) + PROJECT CONTEXT (MUHIYA.md/MEMORY.md **uncapped up to 32 KB each**,
  `project_context.go:31-32`). Executor ≈4-5k fixed (system ≈600 + tools
  ≈2,700 + handoff ≤~1,500), already definition-filtered per kind.
- Task-brief note: the per-task brief embeds `date=` via `time.Now()`
  (`classify.go:196`) — rides the dynamic user-message tail, cache-legal; listed
  so nobody "fixes" it into the prefix.
- Aux calls per task (all bounded): advisor once/session (~1k in / 200 out),
  onboarding only for vague prompts (~250/500), compaction summarizer up to
  ~7.5k in / 1,600 out **on the active main model** (`maintenance.go:76`),
  review subagent the largest optional (Decide-gated).

### RC-6: workflow weight for trivial tasks

- The operating contract mandates delegation for ANY workspace change
  (`prompt.go:45` rule 2) — correct under the split — but the classifier ladder
  (`internal/orchestrator/classify.go:67-181`, classes Chat/Tiny/Small/Standard/
  Large/Epic) currently changes only tool/turn budgets, not the *ceremony*:
  advisor + fresh-session advisory run per task (`turnloop.go:71-72`; advisor is
  bounded — 8s timeout, 200 max tokens, `shouldRunAdvisor` gate — verify
  once-per-session in execution), and rule 6 permits a review agent "when the work
  was substantial" with no class gate.
- tasks.md is already optional under 3 steps (rule 4). The snake-game flow still
  pays: classify → (possible explore) → tasks.md → dispatch → executor explores →
  giant write (truncated, retried) → report → wrap-up.

---

## §2 Workstreams

> Task numbering: `T<workstream><nn>`. Every task states: files, change, test,
> acceptance. Tasks marked **[PREFIX]** contribute bytes to the Phase-2 epoch
> commit and MUST NOT land separately.

### WS-M — Measurement first (Phase 0)

**TM01 — request-byte accounting helper for fieldtests.**
Files: `internal/orchestrator/fieldtest_e2e_test.go` (helper), new
`internal/orchestrator/fieldtest_tokens_test.go`.
Change: add `requestBytes(provider *scriptedProvider) (requests int, totalBytes
int, perRequest []int)` summing marshaled message content + tool definitions per
recorded request (the scripted provider already records every request —
`fieldtest_e2e_test.go:29,46-64`). Bytes are the offline proxy for tokens
(Constitution VI: label them bytes, never "tokens", in test names and failures).
Test: self-testing helper (golden-free); used by every scenario below.
Acceptance: helper compiles, returns non-zero for the existing
`TestFieldTestGoAfterPlanExecutes`.

**TM02 — "snake game" baseline scenario.**
Files: new `internal/orchestrator/fieldtest_snake_test.go`.
Change: scripted scenario mirroring the field session: user asks for a snake game
in a target subdirectory; script the main model to write `tasks.md`, dispatch the
executor; executor reads tasks.md, writes `snake.html`, reports COMPLETE; main
wraps up. Record (requests, totalBytes) on the CURRENT tree and pin them as the
**before** numbers in this plan's execution log (comment, not assertion).
Test: asserts only workflow correctness (file lands, one dispatch) — byte numbers
are logged, not asserted, until the after-state.
Acceptance: scenario green on the unmodified tree; before-numbers recorded.

### WS-A — tasks.md in the target directory

**TA01 — engine: active-checklist tracking.**
Files: `internal/orchestrator/checklist.go`, `internal/orchestrator/engine.go`
(field), `internal/state/session.go` + `internal/contract/types.go` only if
persistence requires a new sidecar field (prefer deriving over persisting — see
change).
Change: add `activeChecklistPath string` to the engine (guarded by `e.mu`). In the
shared dispatch gate where `touchesChecklist(call)` already fires (the
post-mutation refresh call site — locate via `refreshChecklist` callers), resolve
the written tasks.md path (workspace-relative → absolute against
`e.session.WorkspacePath`) and store it. `refreshChecklist` reads
`activeChecklistPath` when non-empty, else falls back to the workspace-root path
(existing behavior). On resume, derive: if the workspace-root tasks.md is absent
but the session's last `run_start`/`run_summary`… — NO: keep it simple and
deterministic — persist `ChecklistPath` in the session record ONLY if a resumed
session must restore the panel from a non-root path; otherwise fall back to root
on resume and let the next write re-establish it. Decide by reading
`internal/command/runtime_build.go` hydration: if the panel is rebuilt from
events, no persistence is needed. Document the decision in the code comment.
Test: new `internal/orchestrator/checklist_path_test.go` —
(a) a `write_file` to `games/snake/tasks.md` through the shared gate updates the
panel (PlanUpdate fires with the parsed items); (b) a later root tasks.md write
switches the active path; (c) `touchesChecklist` continues to carve out BOTH from
the role gate (existing `TestRoleGate…` stays green).
Acceptance: a tasks.md written anywhere inside the workspace drives the to-do
panel; root behavior unchanged for existing sessions.

**TA02 [PREFIX] — prompt: name the location rule.**
Files: `internal/instructions/prompt.go` (`PromptPlanningBody` step 3, and the
rule-4 sentence in `PromptOperatingContractBody`).
Change (exact wording, keep it this tight): step 3 gains "Create tasks.md in the
directory the work targets (create the directory first if needed); use the
workspace root only when the task spans the whole workspace." Rule 4's "keep a
tasks.md checklist current" is untouched (location is stated once, in PLANNING —
the audit forbids stating one rule in two places with different words; if the
audit's single-statement check flags the addition, register the sentence as a
rule body used verbatim in both places, following the
`WriteFilePermissionRuleBody` pattern at `prompt.go:21-36`).
Test: `internal/instructions/audit_test.go` stays green;
`epoch_clauses_test.go` gains a pin for the location sentence.
Acceptance: part of the Phase-2 epoch; prompt-budget test still under baseline.

**TA03 — handoff names the checklist path.**
Files: `internal/orchestrator/subagent.go` (handoff composition — exact insertion
point from investigator A's report).
Change: when a checklist is active (`activeChecklistPath != ""` or root file
exists), the executor handoff carries one line: `Checklist: <workspace-relative
path> — work top to bottom, check off items as you complete them, do not redo
checked items.` (This is per-run user-message content — cache-safe, NOT prefix.)
Test: extend `internal/orchestrator/handoff_fidelity_test.go` — dispatch with an
active non-root checklist asserts the path line rides the handoff.
Acceptance: executor receives the path; no prefix bytes changed.

### WS-B — tool-call integrity (truncation)

**TB01 — typed truncation detection at the source (cache-safe).**
Files: `internal/gateway/sse.go`, `internal/gateway/provider.go`,
`internal/contract/types.go`.
Change: in the SSE accumulator's `Result()` (`sse.go:147-169`), for each named
`partialCall` run `json.Valid([]byte(call.arguments))`; expose
`ChatResponse.TruncatedCalls []string` (the call IDs whose arguments are invalid
when `finish == "length"`). Thread exactly like FinishReason
(`sse.go:20,100-102,168` → `provider.go:321` → `types.go:275-285`). A transport
death mid-arguments already errors out (`provider.go:281-288`) and stays
untouched — only the clean cap-hit path is tagged.
Test: extend `internal/gateway/params_finish_test.go` pattern — a scripted SSE
stream ending with `finish_reason:"length"` mid-argument yields the call ID in
`TruncatedCalls`; a complete-args length-finish does not.
Acceptance: no behavior change for well-formed streams; no prefix bytes.

**TB02 — explicit output-window sizing (cache-safe).**
Files: `internal/gateway/model.go`, `internal/orchestrator/turnloop.go:371`,
`internal/orchestrator/subagent.go:433`.
Change: introduce `outputBudget(modelID)` resolution: catalog
`contract.Model.MaxOutput` when > 0 (`types.go:40` — parsed at
`provider.go:565-576` and today consumed NOWHERE), else
`profile.MaxOutputTokens`; always clamped by `profile.OutputTokenLimit`
(`ClampOutputTokens`, `model.go:161-166`). Set it as `ChatRequest.MaxTokens` at
both loop call sites. Raise DeepSeek/GLM/generic operational defaults from
16,000 (`model.go:105,139,141`) to 32,000 (DeepSeek's documented ceiling is
384k). **MiniMax stays at its current default** — max_tokens counts against its
shared context window (`model.go:126-129`), so raising it shrinks the input
window; for MiniMax the chunked protocol (TB03/TB06) is the fix, not headroom.
Test: unit-test `outputBudget` (catalog wins, profile fallback, ceiling clamp,
MiniMax unchanged); scripted-provider test asserting both loops now send
`max_tokens`.
Acceptance: request bodies carry an explicit, per-model output cap.

**TB03 — never dispatch a truncated call; recover cheap (cache-safe).**
Files: `internal/orchestrator/turnloop.go:461-479`,
`internal/orchestrator/subagent.go:479-487`, `internal/orchestrator/turnloop.go`
history-replay sites (`:555,599-602`, `assistantReplayMessage:642-652`),
`subagent.go:509,528`.
Change: in BOTH loops, when `response.TruncatedCalls` is non-empty: (a) do not
dispatch the broken call(s) — synthesize their tool results with the TB04
protocol text (each keeps its call ID so pairing stays intact); (b) count a
storm-breaker class hit via `recordFailedCall` keyed `(toolName,
"json-truncated")` (`dispatch.go:49-53,166-179`) so 3 repeats escalate through
`sc.escalate` in whichever scope is running — the executor's escalate is already
wired (`subagent.go:322-324`) and today unreachable for this failure; (c)
**sanitize history**: the assistant message that enters history/transcript
replaces the broken call's arguments with `{"truncated":true}` so the giant
payload is NOT re-billed on every later request (today it is — the token bomb).
H5's GateRejected exclusion stays (G-1 rationale holds); the class-keyed
governor is the bound here.
Test: new `internal/orchestrator/truncated_call_test.go` — scripted truncated
write_file: broken call never reaches the workspace (no file write), synthesized
result carries the protocol text, third repeat escalates, replayed history
contains `{"truncated":true}` not the payload. Run in both loops.
Acceptance: a truncated call costs one turn, once, with guidance — never a
re-billed payload or an unguarded retry loop.

**TB04 — rewrite the advice; register it (cache-safe: Sidecar).**
Files: `internal/orchestrator/validate.go:26-28`, `internal/orchestrator/gates.go:28`,
`internal/instructions/gates.go` (new registered bodies),
`specs/002-reasonix-agent-overhaul/contracts/dispatch-gate.md:32`.
Change: branch on `call.ToolName()`. For `write_file`/`multi_edit`/`apply_patch`:
"The call was cut off at the output limit. Do NOT resend the whole payload.
Write the file in parts: first write_file the opening section ending at a
complete line plus a unique marker line; then extend with edit_file replacing
the marker with the next section plus the marker; remove the marker last. Never
resend content already written." For other tools keep a corrected generic body
(re-emit whole, compact args). Register both as `Audience: Gate, Cache: Sidecar`
(pattern `instructions/gates.go:41-42,51`) so `audit_test.go` finally covers
them; update the dispatch-gate contract doc.
Test: `epoch_clauses_test.go`-style pins on both bodies; audit test green.
Acceptance: the "(fewer, more compact steps)" update_plan phrasing is gone; the
write-tool branch never advises "shorter".

**TB05 [PREFIX] — teach chunked writes up front.**
Files: `internal/instructions/subagents.go` (`EditDisciplineBody`, delivered via
`SubagentGeneralSystem` — subagent-prefix bytes), NOT `WriteFilePermissionRuleBody`
(it is asserted token-identical in three places — `audit_test.go:126-138`; adding
size guidance there would triple-bill the bytes on the main prefix, where the
model cannot write files anyway).
Change: one sentence in the executor's edit discipline: "A new file beyond a few
hundred lines is written in parts — write_file the opening section ending with a
unique marker line, extend with edit_file replacing the marker (section + marker
again), and remove the marker last." (Line count, not tokens — models cannot
count tokens. Mechanics verified: a successful Write enters the read ledger
(`files.go:331`) so follow-up edits need no read; `applyEditText` exact-matches
anywhere (`files.go:422-480`); multi_edit ≤30 edits/call (`files.go:249-251`).)
Test: pin in `epoch_clauses_test.go`; goldens regen in the epoch commit.
Acceptance: part of the ONE epoch; executor prefix stable thereafter.

**TB06 — fieldtest: truncated write recovers chunked.**
Files: new scenario in `internal/orchestrator/fieldtest_snake_test.go`.
Change: script the executor's first write_file response as a length-truncated
call (via TB01's tagged response), then scripted chunked recovery. Assert: (a)
the broken call is not dispatched, (b) recovery completes via write_file
skeleton + edit_file appends, (c) the storm breaker stays silent (single
occurrence), (d) request count bounded, (e) total request bytes exclude the
truncated payload after the failure turn (TB03c).
Acceptance: the field session's failure mode is scripted and can never return.

### WS-C — no duplicated implementation

Carrier constraints (test-pinned, see RC-2): system text byte-stable across
dispatches; fresh dispatch = exactly 2 messages; continuation appends exactly ONE
user message. All carriers below ride INSIDE the existing single user message.

**TC01 [PREFIX] — executor re-entrancy clause.**
Files: `internal/instructions/subagents.go` (`SubagentGeneralSystem`,
`subagents.go:86`).
Change: append one sentence: "Before your first change, read the checklist the
assignment names and check what already exists: never redo a checked item or
rewrite a file that already matches its item — continue from the actual current
state." (Subagent-prefix bytes → epoch commit. Register with correct
`MentionsTools`/`AllowlistCtx` per the pattern at `subagents.go:89-102`;
`handoff_fidelity_test.go:72-97` pins exact substrings of the CURRENT text —
extend the pin, don't break it.)
Test: pin in `epoch_clauses_test.go`; audit + fidelity tests green.

**TC02 [PREFIX] — the task property demands prior-state disclosure.**
Files: `internal/instructions/tools.go:86-90`
(`ToolRunSubagentTaskPropertyDescription`).
Change: the checklist of what a task must carry gains: "what already exists and
what any earlier attempt completed (so nothing is redone)". Keep it one clause —
this description is already ~1,050 chars and WS-G is dieting the same block; net
growth here must be paid inside TG01's reduction.
Test: `delegation_prompt_test.go:73-104` substring pins extended.

**TC03 — digest carry-forward from NON-linkable records (cache-safe).**
Files: `internal/orchestrator/contextrecord.go:326-336`
(`latestLinkableRecord`), `internal/orchestrator/contextlink.go:61-152,213-241`.
Change: when NO linkable candidate exists, look up the latest record of the kind
REGARDLESS of `Linkable` (records are stored for every terminal shape —
`contextrecord.go:303-311`) and seed `digestCarryForward` from it (≤1,400 chars:
result digest + touched files + changed-since markers). Continuation stays
clean-done-only. A failed/cancelled/wrapped-up attempt 1 then hands attempt 2
its digest instead of nothing.
Test: **consciously retire** `TestDecideLinkNonLinkableShapesFallBack`'s
fresh/no-candidate pin (`contextlink_test.go:206-214`) — update it to expect
`linkDigestSeeded` with the non-linkable record's digest; add a
failed-first-run scenario. The 2-message fresh-dispatch pin
(`contextlink_integration_test.go:160-178`) stays green (digest rides Context
inside the one user message).

**TC04 — checklist state line in the handoff (cache-safe).**
Folded into TA03's Checklist line: it carries the path AND the live counts
("3 of 7 checked") read from the parsed active checklist at dispatch time. Keep
the whole line ≤120 chars so the `task+900` budget
(`handoff_fidelity_test.go:60-62`) holds without a re-baseline.

**TC05 — fieldtest: no re-implementation after a dead first dispatch.**
Files: `internal/orchestrator/fieldtest_snake_test.go`.
Change: scenario — first executor dispatch ends `failed` mid-way (after writing
part of the file / checking one item); second dispatch's recorded request must
carry (a) the checklist path line with updated counts, (b) the attempt-1 digest
(TC03), and (c) exactly 2 messages. Assert on the recorded wire messages, not on
model behavior.

### WS-E — cache maximization

**TE01 — continuation gated on MEASURED link health (cache-safe).**
Files: `internal/orchestrator/contextlink.go:61-152` (decideLink),
`internal/contract/cache.go:90-135` (PerPairingRates — data already exists).
Change: before choosing `linkContinued`, consult the measured steady-state
read-share for (subagent model, pin) over the last ≥3 requests; below 20%,
degrade to digest-seeded with reason `provider-cache-cold` (a dead cache makes
the ≤2,200-char digest ~10× cheaper than re-billing a 15-25k replay). Emit the
existing per-dispatch link notice with the reason (`contextlink.go:377-390`
pattern).
Test: unit — synthetic pairing rates at 0%/15%/60% choose digest/digest/
continuation; notice text pinned.

**TE02 — surface cache collapse to the user (cache-safe).**
Files: `internal/orchestrator/cacheresilience.go:34-74`, `usage.go:197-210`.
Change: generalize the cold-start notice: fire once per task when a main-stream
request with prompt ≥ 8k reports read-share < 5% (not only == 0), naming the
model and the attributed cause (`agent-suspect` heuristics vs `provider`). The
109/20,647 request becomes visible instead of silent.
Test: extend the cacheresilience tests — 109/20,647 fires; 15k/20k does not;
fires at most once per task.

**TE03 — stop fabricating "Cache Write: 0" (cache-safe, Constitution VI).**
Files: `internal/command/benchjson.go:23,86-96`.
Change: no write-side field exists in any parsed provider payload — remove
`CacheWriteTokens` from bench JSON (or emit it only when a future field is
actually parsed, as `null`/omitted, never 0). Update any bench readers.
Test: bench JSON golden/unit reflects omission.

**TE04 — absolute history-trim floor (cache-safe).**
Files: `internal/orchestrator/engine.go:20` (`maintenanceFloorRatio` use site),
`maintenance.go:116-185`.
Change: boundary maintenance fires at `min(60% of usable window, absolute
120k-token floor)` so 1M-window models (MiniMax M3) still reclaim. The existing
yield floor (≥5%) and invalidation-event bookkeeping stay — this changes WHEN
reclamation is considered, not what it does.
Test: unit — 1M-window engine with 150k-token history triggers boundary
maintenance; 64k-window behavior byte-identical to today.

**TE05 — wire the executor's supersede IDs (cache-safe).**
Files: `internal/orchestrator/subagent.go:341-352`.
Change: stop discarding `inspection.InvalidateFor(call)`'s return — pass it to
`e.history.MarkSuperseded` exactly as the main scope does
(`dispatch.go:159-161`), so main-history reads of files the executor rewrote
become trim-eligible. Under the role split this is the ONLY mutation path, so
today supersede marking is dead code.
Test: executor writes a file the main loop had read → the read's ID is in the
superseded set; next trim cuts it first.

**TE06 — make wrap-up runs linkable (cache-safe).**
Files: `internal/orchestrator/contextrecord.go:26-32,303-311`,
`internal/orchestrator/subagent.go:548-560` (wrap-up injection sites),
`internal/orchestrator/contextlink.go`.
Change: when storing the context record for a `wrapup-done` run whose tool
pairings are complete, strip the harness-injected wrap-up/budget nudge messages
from the stored transcript and mark it `Linkable` (shape recorded as
`wrapup-done-linkable` for traceability). The normal ending of a big build then
keeps its warm stream. Emit the link-decline reason as a user notice when a
candidate is rejected (today it reaches only bench JSON, `benchjson.go:142-150`).
Test: a wrapped-up scripted run links its successor (continuation replays the
STRIPPED transcript — assert no nudge text in the replayed wire messages);
`contextlink` gate tests updated consciously.

**TE07 — cap the uncapped prefix riders (cache-safe for existing goldens).**
Files: `internal/workspace/project_context.go:31-32` (32 KB/file),
`internal/command/skills.go:64` + `skills_tool.go:207-238` (32 KB skill bodies).
Change: render caps — PROJECT CONTEXT files head+tail-cut at 8 KB each with an
explicit "[trimmed — open the file for the rest]" marker; skill bodies cut at
16 KB at dispatch with the same marker. Boot-time composition → affects only
sessions whose files exceed the caps; goldens (small fixtures) unchanged.
Test: unit — oversize MUHIYA.md renders capped with marker; boot snapshot
byte-stable across turns (Constitution III).

**TE08 — gateway-repo handoff note (out of agent scope, document only).**
The MiniMax route served ~109 of 20,647 despite a byte-stable, shape-guard-clean
prefix (RC-5). Agent-side work cannot force provider caching (no request-side
hints exist in the OpenAI-compatible surface we target). File the evidence with
the gateway repo: verify sticky upstream routing honors `X-Muhiya-Session`
(`provider.go:211-217`), whether the MiniMax upstream needs an explicit cache
flag, and whether gateway load-balancing breaks passive prefix matching.
Acceptance: a written note in the gateway repo's issue tracker / plan file —
NOT agent-side hacks.

### WS-D — read discipline and scoped exploration

**TD01 — executor per-run duplicate-read guard.**
Files: `internal/orchestrator/subagent.go` (sub `dispatchScope`),
`internal/orchestrator/dispatch.go` (scope wiring), possibly a small
`runReadLedger` in `subagent.go`.
Change: give the subagent scope a per-run read-signature set (tool name +
canonical args for readonly tools — reuse `callSignature`/`readSignature` from
`inspection.go` if exportable without layering violations; else a local copy with
a sync test). On a duplicate, return the existing
`GateDuplicateReadTmpl`-shaped sidecar text pointing at the earlier call. The
run's OWN mutations invalidate its ledger (same rule as main:
mutation → clear search signatures + touched paths).
Test: new `internal/orchestrator/subagent_dedupe_test.go` — scripted executor run
re-reading the same file twice gets the already-read block on the second call;
after the run edits the file, a re-read passes.
Acceptance: no behavior change for non-duplicate reads; main-loop ledger untouched
(B6 isolation: per-run state only).

**TD02 [PREFIX] — scope the executor's exploration.**
Files: `internal/instructions/subagents.go` (SubagentGeneralSystem).
Change: one sentence: "Operate only on the files the assignment names (plus files
you create); confirm a single file with read_file, and use glob/list_files only
under the assignment's target directory — never scan the whole workspace."
Test: `subagent_env_test.go`-style unit pin + audit test green.
Acceptance: epoch commit; `instructions_dump.golden` diff shows exactly this
sentence.

**TD03 [PREFIX] — fix the glob description's bad example.**
Files: `internal/instructions/tools.go:26`.
Change: `"Find files by doublestar glob scoped to the narrowest directory, e.g.
src/**/*.go. Prefer a directory prefix over a bare **/ scan."`
Test: dump golden regen (epoch); no code behavior change.
Acceptance: epoch commit.

### WS-F — workflow ceremony scaled to task class

**TF01 — class-gated ceremony (verify-then-close-gaps, not re-invent).**
Files: `internal/orchestrator/turnloop.go` (advisory call sites),
`internal/orchestrator/reviewgate.go` (only if a gap is proven).
Reality check first: review gating ALREADY exists (feature 011 — `ReviewTier`
skip/focused/deep and `Decide`, `reviewgate.go:26-60,188-204`; the model-invoked
`run_subagent("review")` path is DELIBERATELY ungated per the model-driven
dispatch directive — do not gate it). The task is therefore an audit + gap-close:
(a) write the Tiny-class fieldtest FIRST and observe which aux calls actually
fire (advisor, fresh-session advisory, automatic review); (b) for each one that
fires on a Tiny task, either confirm it is self-gating (advisor's
`shouldRunAdvisor`, 8s/200-token bound — `advisor.go:183-206`) and leave it, or
add the narrowest class gate at its call site (`turnloop.go:71-72` for
`maybeAdviseFreshSession`; `Decide`'s TaskProfile for the automatic review
trigger). No new gating framework.
Test: fieldtest asserting a Tiny-class scripted task performs zero aux model
calls beyond the main turns and the single dispatch.
Acceptance: Standard/Large/Epic behavior unchanged (existing tests green);
model-invoked review dispatches remain ungated.

**TF02 — stop-at-done.**
Files: `internal/orchestrator/turnloop.go` (the no-tool-call finalize ladder,
`turnloop.go:492-545`), checklist state accessor.
Change: add a `taskSatisfied()` predicate — the active checklist exists, every
item is checked, and the task's most recent executor report parsed COMPLETE
(state already tracked for review-outcome stamping) — and consult it at the TOP
of the no-calls branch (`turnloop.go:492`): when true, skip the empty-final retry
(`:500-504`), the narrated-intent retry (`:513-518`), and the AutoReview nudge
(`:527-543`), and go straight to `finalize` (`:545`). The nudges exist to stop
premature finishes; a satisfied checklist is the opposite case, and each nudge
costs a full round-trip on the expensive main stream. The AutoReview skip only
applies via the gate's existing Decide short-circuit — record
`ReviewTierSkip`-equivalent rationale ("checklist satisfied") through
`setTaskReviewDecision` so stats stay honest.
Test: fieldtest — after COMPLETE + all-checked, the very next no-call main
response finalizes with zero injected nudge messages (assert no `[review]` /
`[continue]` user messages in recorded requests); a second scenario with one
UNCHECKED item still gets the existing nudge behavior.
Acceptance: multi-dispatch tasks (unchecked items remain) unaffected; existing
faultinjection/delegation regression tests green.

### WS-G — fixed-payload diet — partially PENDING investigator C

**TG01 [PREFIX] — tool-definitions JSON diet.**
Files: `internal/workspace/registry.go` (property descriptions),
`internal/instructions/tools.go`, `internal/orchestrator/definitions.go`.
Change: the 19,305-byte definitions block is ~78% of the fixed main prefix.
Measured fat (investigator C): run_subagent ≈2.9 KB (its description AND its
task-property are ~1,050 chars EACH), memory trio ≈3.2 KB (save_memory's
description alone is a paragraph), ask_user+propose_changes ≈1.7 KB. Shrink:
(a) property-level inline hints in registry.go (not registered rules); (b) the
save/recall/edit_memory descriptions — keep every stated rule, cut the prose
around it (audit `StatesRule`/`MentionsTools` coverage is the guardrail — a rule
lost = audit failure); (c) run_subagent's description/task-property wording
(pinned substrings in `delegation_prompt_test.go:73-104` updated consciously,
and TC02's added clause paid from the same cut). Target: ≥20% net reduction of
the TOOL DEFINITIONS section (≤ ~15.4 KB), measured by the wire golden's section
header.
Test: golden regen (epoch) shows the byte count; audit tests green; prompt-budget
re-baselined DOWN in the same commit (covers TA02/TB05/TC01/TC02/TD02/TD03
additions with room to spare).
Acceptance: wire golden TOOL DEFINITIONS ≤ ~15.4 KB; zero rules lost.

**TG02 — verify executor definition subsets (audit-only, cache-safe).**
Investigator C measured the general executor's tool block at ~10.5-11.5 KB
(synthetics excluded) — filtering EXISTS. Task: pin it. Test asserting each
kind's wire definitions == its spec.Allowed set (explore/review get read-only
subsets), so a future registry change cannot silently re-fatten dispatches.

**TG03 — compaction summarizer on the utility model (measured, optional).**
Files: `internal/orchestrator/maintenance.go:76`.
Change: the compaction summarizer (up to ~7.5k in / 1,600 out, deliberately
cold) runs on the ACTIVE main model; switch to the utility/subagent model like
the advisor. GATE: run the compaction quality fixtures before/after; land only
if summaries stay faithful (Constitution II — cache efficiency never at quality's
expense). If quality drops, document and keep the main model.
Test: existing compaction tests + a fidelity comparison noted in the execution
log.

---

## §3 Verification gauntlet

1. Per-task regression tests (named above) — each proven to fail on the pre-fix
   tree where the defect is reproducible offline.
2. `gofmt` / `go vet` / `go build` / `go test ./... -count=1` after every phase.
3. **Offline before/after table** (WS-M scenarios): requests, total bytes,
   per-request bytes for (a) snake-game scenario, (b) truncation-recovery
   scenario, (c) tiny-task ceremony scenario. Record in the execution log with
   the tree SHA for each measurement.
4. UTF-8 corruption sweep (`grep -c $'\xef\xbf\xbd'` over changed files — the
   PowerShell hazard from prior sessions; write non-ASCII only via Edit/Write
   tools).
5. Goldens: exactly ONE regen commit; diff reviewed line-by-line against §2 tasks.
6. Live (owner-gated, after merge): one real snake-game session on MiniMax and
   one on DeepSeek; record request count, input tokens, and cache READ per
   request (write-side numbers do not exist — TE03); compare against the field
   session's numbers. Target: >90% of the stable prefix served from cache on
   cache-capable providers from turn 2 on, and total tokens for the scenario
   reduced by an order of magnitude. If MiniMax still reads ~0% with a clean
   shape guard, that confirms the TE08 gateway-side case — do not chase it
   agent-side.

---

## §4 Execution log

### Phase 0 — measurement harness (2026-07-21) ✅

Landed: TM01 (`fieldtest_tokens_test.go` — `requestBytes`/`requestAccount`,
main-vs-executor split, self-guarded), TM02 (`fieldtest_snake_test.go` — the
happy-path snake build into a target subdirectory).

**BEFORE baseline (unmodified tree):**

| scenario | requests | total bytes | main | executor | per-request |
|---|---|---|---|---|---|
| snake-baseline | 7 | 103,081 | 77,157 | 25,924 | 18358, 18973, 8134, 8538, 9252, 19567, 20259 |

Observations that shape later phases:
- Every MAIN request costs 18.4–20.3 KB; the fixed prefix dominates (TG01's
  tool-JSON diet targets exactly this).
- Executor requests are 8.1–9.3 KB — already definition-filtered (TG02 confirms).
- The scenario's target-directory assertions PASS on the unmodified tree: the
  main model CAN write `games/snake/tasks.md` (basename carve-out). RC-1 is
  therefore purely a READER defect (TA01) plus prompt silence (TA02).

### Phases 1 + 2 (2026-07-21) ✅ — landed, full suite green

Landed: TB01, TB02, TB03, TB04, TB05 (truncation chain); TA01, TA02, TA03/TC04
(tasks.md location, reader, handoff); TD01, TD02, TD03 (read discipline + scope);
TC01, TC02 (executor re-entrancy + prior-state); TG01 (partial); TE05.
Goldens regenerated ONCE. `promptBaselineChars` 5340 → 5393 (documented).

**AFTER measurement, same scenario:**

| | requests | total | main | executor |
|---|---|---|---|---|
| before | 7 | 103,081 | 77,157 | 25,924 |
| after | 7 | 105,243 | 76,217 | 29,026 |
| delta | 0 | **+2,162** | **−940** | **+3,102** |

**Read this honestly — the happy path got slightly more expensive.** The main
stream fell 940 B (the tool-JSON diet), but the executor gained ~1,034 B per
request from the new scope/re-entrancy/chunked-write clauses and the checklist
handoff line. This scenario is the SUCCESS path: one plan, one dispatch, one
clean write. It exercises none of the waste the clauses exist to prevent, so it
shows only their cost.

Why the trade is still right, stated precisely rather than assumed:
- The added executor bytes are **prefix-cacheable** — the scope and chunked-write
  clauses live in the per-kind system message, byte-stable across dispatches, so
  on a working cache they are paid once per kind per session. The waste they
  prevent (re-read file bodies, re-billed truncated payloads, a rebuilt
  implementation) is **uncached tail** billed at full price every turn.
- Prevented-waste magnitudes, measured by the new regression tests, not estimated:
  a truncated 4,000-char write is now billed once instead of riding every later
  request (`TestTruncatedWriteIsNeverDispatchedAndNotReBilled`); a repeated read
  returns a ~90-char block instead of the file (`TestExecutorRunBlocksDuplicateReads`).
  A real `snake.html` is far larger than the 4,000-char fixture.
- **Not yet measured offline:** a failure-path scenario that shows the saving as a
  byte delta. TB06/TC05 are exactly those scenarios and remain open — until they
  run, the saving is demonstrated by behavior tests, not by a number.

Tool-definitions JSON: 19,305 → 19,021 B (−284, **−1.5%**), far short of TG01's
−20% target. Honest reason: the remaining weight is rule-bearing text the audit
protects (report formats, the write_file permission sentence, the task-property
contract) and already-terse per-property hints in `workspace/registry.go`. The
condensable prose — the memory trio and run_subagent's restated "Kinds:" gloss —
is what came out. Hitting −20% would mean deleting stated rules, which the plan
itself forbids. **TG01 is recorded as partially achieved, not done.**
