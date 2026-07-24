# MuhiyaCode — Terminal-Bench Readiness Audit and Implementation Plan

Audited against `F:\MuhiyaCode Agent Go` HEAD `main` (`2ad5b9d`), Go `go1.26.4 windows/amd64`, module `github.com/muhiya/muhiyacode`. Every finding cites exact code locations verified against the current tree. This plan is research-only output: **no code was changed to produce it.** It is written so another coding agent can execute it phase by phase without rediscovering the architecture.

The audit was conducted by six parallel research agents covering: (1) agent reasoning/execution/termination, (2) prompts/tools/shell/loop, (3) file reading/context/tokens, (4) model selection/routing/recovery, (5) headless/benchmark/container/logging/test-exec/git, (6) security/permissions/cost/logging. Findings were deduplicated and adjudicated. Agent-level defects (tooling, loop, prompts, recovery, context, logging, orchestration) are separated from Model-level limitations (raw reasoning quality, determinism) — the former are in scope for this plan; the latter are noted but out of scope for code changes.

A prior polish plan (`b657ff55`) fixed 22 internal-stability findings (inspect_code/web_search dedup, knowledge eviction, covering-segments, trailing-intent, etc.). Those are DONE and are NOT re-reported here, except where they interact with benchmark readiness.

---

## 1. Executive summary

**Verdict: Not ready for serious Terminal-Bench evaluation.** MuhiyaCode has a genuinely strong, well-tested execution core — cross-platform process-tree shell kill, a five-layer loop-breaker stack with a hard turn ceiling, byte-stable prompt-cache prefix with a fail-fast shape guard, mtime-gated read dedup, provider-reported per-(model,pin) usage accounting with fsync durability, and graceful SIGTERM. The transport resilience layer (retry ladder, stall detection, idle timer) is mature. None of this is the problem.

The problem is that the agent has **no benchmark execution contract** and **one Critical headless blocker** that together make any objective evaluation impossible today:

1. **The workspace trust gate denies every mutation and shell command in a non-TTY container with a fresh state DB** (`internal/workspace/permissions.go:125-128,177-179,211-244`; `internal/command/root.go:630-633`). `ensureTrusted` runs *before* the auto-accept short-circuit and is not mode-aware; the non-TTY `Confirm` callback returns `false`. In a fresh Terminal-Bench container the agent can read files but cannot edit one or run one command. Every code-editing task scores ~0%.
2. **There is no Terminal-Bench/Harbor adapter.** `benchmarks/terminalbench/main.go` is a TUI-memory paging micro-benchmark (it "deliberately does NOT import or drive the TUI," `main.go:8-9`), not an agent adapter. The only agent-driving path is `muhiyacode --print "<task>"` + `MUHIYA_BENCH_JSON=1`, which emits one JSON line carrying only `completed:bool` — no status, no stop reason, no duration, no tool-call count, no trajectory, no model/effort config.
3. **No configurability.** Model, provider, apiKey, effort, timeout, token budget, and cost budget are read from a mutable persisted `~/.muhiya/settings.json` that the runtime itself rewrites mid-run (`runtime_build.go:114-129`). There are no CLI flags or env overrides, so a benchmark cannot hold config constant per run, and a clean container cannot start at all (the `configurationNotice` gate refuses an empty config, `root.go:125-127`).
4. **No enforced final verification.** Success is declared when the model stops emitting tool calls or the turn cap is hit — never when the repo's tests pass. `checksRun` is a counter, not a gate (`turnloop.go:602-604`); `finalize` only reconciles against the model's own `tasks.md` (`turnhelpers.go:138-151`).
5. **No hard wall-clock / token / cost budget.** The only bounds are the turn ceiling (120×effort scale, `engine.go:19`) and per-shell timeout (≤10m, `types.go:25`). A single queued provider request can drip keep-alive past the 14m first-byte deadline indefinitely (`provider.go:282-300`). External SIGKILL loses the bench summary.

Secondary but real pass-rate/cost hits: turn budget escalates only after files change, cutting off exploration-heavy tasks before they edit (`turnloop.go:270-276`); final-turn tool calls are silently dropped (`turnloop.go:498-506`); a do-nothing turn-1 reports `completed:true` (`turnloop.go:515,560`); compaction is lossy and non-deterministic (`maintenance.go:47-101`); `read_file` truncates every line to 500 chars, breaking `edit_file` oldString matches on long lines (`files.go:116-122`); `inspect_code` is Go-only and caps output at 2000 bytes, wasting a turn on every non-Go task (`codeindex.go:566-568,594-597`); `multi_edit` reports partial batches as success (`files.go:266-303`); `rm -rf build/` cleanup is hard-blocked with no in-workspace exception (`risk.go:83-102`); the model advisor/catalog can silently swap the configured model in bench mode (`advisor.go:79-89,125-147`).

**What it would take:** Phase 0–6 below. The single highest-leverage change is Phase 1 Step 1 (headless trust bypass) + Phase 0 Step 1 (the adapter) + Phase 1 Step 2 (status/timeout/budget) — together these take the agent from "cannot be evaluated" to "evaluable and bounded." Phases 2–5 then drive pass rate and cost/success; Phase 6 validates.

---

## 2. Current architecture overview

Entry point: `cmd/muhiyacode/main.go` → `command.Execute` → `NewRootCommand` (`internal/command/root.go:35`). Two run modes:

- **Interactive TUI:** `runInteractive` (`root.go:73`) → `tui.Run`. Not used by benchmarks.
- **One-shot:** `runOneShot` (`root.go:118`) → `OpenApplication` → `app.Runtime().Engine.Run(ctx, AssemblePrompt(...))`. Setting `MUHIYA_BENCH_JSON=1` calls `emitBenchSummary` (`benchjson.go:68`) on stdout as the last line, even on error/cancel (`root.go:132-142`).

Core seam: `internal/app/app.go` (`OpenApplication`, `AssemblePrompt`, `Runtime`). Engine: `internal/orchestrator/engine.go` (`Engine.Run`). Turn loop: `internal/orchestrator/turnloop.go` — a single `for {}` (`:242`) bounded by `hardTurnCeiling` (120 at medium, scaled by effort up to ~168, `engine.go:16-19`, `taskledger.go:30-32`).

Key subsystems and their benchmark-relevant files:
- **Tool registry & dispatch:** `internal/workspace/registry.go` (13 workspace tools), `internal/orchestrator/registry.go`, `internal/orchestrator/toolhandlers.go` (`decodeToolArgs`), `internal/orchestrator/gates.go` (gate H1–H5 + repeat limiter + dedup + storm breaker), `internal/orchestrator/dispatch.go` (`postDispatch`, `recordFailedCall`).
- **Shell:** `internal/workspace/shell.go` (timeout + process-tree kill), `process_unix.go` (Setpgid + negative-PID SIGKILL), `process_windows.go` (Job Objects), `risk.go` (`ClassifyShell`), `internal/shellsafe/*`, `internal/orchestrator/shellclassify.go`.
- **Files:** `internal/workspace/files.go` (read/write/edit, `canOverwrite`, 500-char line cap, 5MB hard limit), `patch.go`, `codeindex.go`/`codeindex_exec.go` (Go-only `inspect_code`).
- **Permissions/trust:** `internal/workspace/permissions.go` (`Guard`, `ApprovePath`/`ApproveShell`/`ensureTrusted`, `MemoryTrustStore`), `paths.go`.
- **Context/dedup/cache:** `internal/orchestrator/inspection.go` (read dedup ledger, signatures), `knowledge.go` (notes/briefing), `memory.go`/`maintenance.go` (compaction), `history.go` (transcript, fold/trim), `prefixshape.go` (cache-shape guard), `cacheresilience.go`, `truncation.go`, `contextreport.go`.
- **Model/provider:** `internal/gateway/provider.go` (OpenAI-compatible, retries, timeouts), `sse.go` (streaming), `rescue.go` (prose→tool-call rescue), `model.go` (profiles/capabilities), `upstreampin.go`, `usage.go`/`web.go`. `internal/orchestrator/advisor.go` (per-task model advisor), `effort.go`/`switchcost.go`.
- **State/logging:** `internal/state/{config,session,db,paths,toolcache}.go` (persisted settings, SQLite transcript/events, fsync'd JSONL), `internal/orchestrator/telemetry.go`/`usage.go`, `taskledger.go`.
- **Bench surface:** `internal/command/benchjson.go`, `scripts/bench_011.{sh,ps1}`, `specs/011-competitive-agent-audit/contracts/benchmark-run.md`. `benchmarks/terminalbench/main.go` is NOT an agent adapter (see §1).

Configurability today: only `--cwd`, `--print`, `--simple`, `--new`, `--no-mcp` (`root.go:59-63`). Everything else comes from `~/.muhiya/settings.json` (`state.LoadSettings`) and `~/.muhiya/secrets.json`. `MUHIYA_HOME` relocates the whole state tree (`state/paths.go:14,36-49`) — this is the existing isolation hook.

Preserved strengths (do not regress): process-tree shell kill; 5-layer loop breaker + hard turn ceiling; byte-stable prefix + fail-fast shape guard; mtime-gated read dedup with compaction-aware intactness; provider-reported usage with per-(model,pin) attribution and fsync; non-TTY fail-closed confirms (no stdin hangs); closed rm-laundering; sensitive-root traversal pruning; graceful SIGTERM; bounded re-prompts (empty-final ≤2, trailing-intent ≤2, AutoReview once); H5 distinct-failure terminator; upstream-pin rejection latch.

---

## 3. Benchmark-readiness gap analysis

Mapped against the 10 Terminal-Bench requirements in the task:

| # | Requirement | Status | Evidence |
|---|---|---|---|
| 1 | Reliable headless mode: one task, no interaction, clear final status | **Blocked** | `runOneShot` works but the trust gate denies all mutations in non-TTY (`permissions.go:125-128,211-244` + `root.go:630-633`); no discrete status emitted (only `completed:bool`, `benchjson.go:105`); `ask_user`/onboarding auto-answer in headless (`root.go:649-664`). |
| 2 | Harbor-compatible custom-agent adapter | **Missing** | No adapter; `benchmarks/terminalbench/main.go:8-9` is a paging micro-benchmark. |
| 3 | Correct operation inside isolated containers | **Partial** | `MUHIYA_HOME` relocatable; Linux process-tree kill correct; BUT `configurationNotice` refuses empty config (`root.go:125-127`), discovery writes back to shared settings (`runtime_build.go:114-129`), web/MCP startup probes add latency and can fail. |
| 4 | Configurable model/provider/effort/timeout/token/cost budget | **Missing** | Only 5 CLI flags; no model/effort/timeout/budget flags or env (`root.go:59-63`); persisted-file dependency. |
| 5 | Fixed-model AND auto-routing modes, every model reported | **Partial** | Routing exists (`advisor.go`) but can silently swap the configured model in one-shot mode (`advisor.go:79-89,125-147`); routing not disabled under `MUHIYA_BENCH_JSON=1`; switch event/reason not in bench JSON. |
| 6 | Complete trajectory + usage logging per run | **Partial** | Durable fsync'd `transcript.jsonl`/`usage.jsonl` exist (`session.go:51-122`) but are NOT in the bench output; no `session_id` link; no per-tool-call duration (`turnloop.go:620-624`). |
| 7 | Reliable termination on done/impossible/timeout/blocked | **Partial** | Liveness-safe (won't hang) but no timeout/budget cause; timeout collapses to `StopCauseUserStop` (`turnloop.go:143-148`); no impossible/blocked detection; do-nothing turn-1 false success. |
| 8 | Protection against shell loops / repeated actions / retries / token runaway | **Partial** | 5-layer breaker + ceiling (strong) BUT repeat limiter evaded by trivial arg variation; no successful-loop detection; no cost/token abort; compaction can re-bill tokens unbounded. |
| 9 | Final verification stage that runs tests and checks repo state | **Missing** | `checksRun` is a counter not a gate (`turnloop.go:602-604`); `finalize` only reconciles `tasks.md` (`turnhelpers.go:138-151`); final-turn tool calls dropped (`turnloop.go:498-506`). |
| 10 | Reproducible runs, no hardcoding/leakage | **Partial** | No task-specific hardcoding found; BUT onboarding auto-injects synthetic clarifications; catalog substitution is non-deterministic; no seeded mode; cross-task state leakage if a batch reuses one engine/session. |

Summary: requirements 1, 2, 4, 9 are hard blockers; 3, 5, 6, 7, 8, 10 are partial with specific gaps.

---

## 4. Detailed findings

Findings are consolidated and deduplicated across the six auditors. IDs are `B-<n>` (Benchmark). Severity, Layer (Agent/Model), and the exact metric each fix should move are on every finding.

### Critical

#### [B-1] Headless trust gate denies ALL mutations and shell commands in a non-TTY fresh container
- **Severity**: Critical
- **Layer**: Agent-level
- **Subsystem**: Permissions / workspace trust / headless runtime
- **Affected locations**: `internal/workspace/permissions.go:125-128` (`ApprovePath` calls `ensureTrusted` before auto-accept), `:177-179` (`ApproveShell` same), `:211-244` (`ensureTrusted` not mode-aware, calls `confirm`), `:246-251`; `internal/command/root.go:630-633` (`Confirm` returns false when `!readerIsTerminal`), `:118-128` (`runOneShot` uses these callbacks); `internal/command/runtime_build.go:346-351` (approver delegates to `callbacks.Confirm`); `internal/state/db.go:109-121` (trust only via interactive confirm into `trusted_workspaces`).
- **Current behavior**: `ApprovePath`/`ApproveShell` call `ensureTrusted` *before* the `Mode()==PermissionAutoAccept` short-circuit. `ensureTrusted` calls `g.confirm` when the workspace is not trusted; the one-shot non-TTY `Confirm` returns `false,nil`. There is no `--trust` flag, no env bypass, and no auto-trust path. Even `PermissionAutoAccept` does not bypass trust.
- **Why it harms benchmark performance**: In an isolated container (no TTY, fresh DB) the first `edit_file`/`write_file`/`apply_patch`/`run_shell` returns `ErrPermissionDenied` before executing. The agent burns its turn budget on denied calls and finalizes with no work. Every mutating task scores ~0%.
- **Example failure scenario**: Harbor spawns `muhiyacode -p "fix the failing test in foo.py"`. The agent reads `foo.py`, calls `edit_file` → `ErrPermissionDenied`; calls `run_shell pytest` → same. Repeats to the turn ceiling. Grader sees no changes.
- **Recommended solution**: Make `ensureTrusted` auto-grant when `g.Mode()==contract.PermissionAutoAccept` (skip `confirm`, call `g.trust.Trust(ctx,g.root)` directly), AND/OR add a `MUHIYA_TRUST_WORKSPACE=1` env / `--trust` flag that pre-trusts the workspace root at application open. The benchmark adapter sets `permission=auto-accept` + the trust env so a fresh container workspace is trusted without interaction. Keep `Normal` mode's interactive trust prompt unchanged.
- **Implementation complexity**: S
- **Dependencies/risks**: Touches the central authorization seam. The auto-accept auto-trust must stay off in `Normal` mode. `internal/workspace/workspace_test.go` and permissions suites assert current deny-on-untrusted; add cases for auto-accept/headless.
- **How to test the fix**: Unit: `NewGuard` with `Mode=auto-accept`, untrusted `MemoryTrustStore`, a `Confirm` returning false → assert `ApprovePath(ActionEdit, inside-root)` and `ApproveShell("echo hi")` succeed and `IsTrusted` becomes true. E2E: `muhiyacode -p "create foo.txt with content bar"` with stdin `/dev/null` in a fresh temp dir → assert `foo.txt` written. Negative: a `~/.ssh` target still denied.
- **Metric expected to improve**: task pass rate, first-attempt success rate, human-intervention rate

#### [B-2] No Terminal-Bench / Harbor-style agent adapter
- **Severity**: Critical
- **Layer**: Agent-level
- **Subsystem**: Harness adapter
- **Affected locations**: `benchmarks/terminalbench/main.go:1-12,135-205` (paging micro-benchmark, "does NOT import or drive the TUI"), `scripts/bench_011.sh:138-184` (on-host runner, no container contract).
- **Current behavior**: Nothing conforms to a spawn-in-container harness contract. The only agent-driving path is `runOneShot` + `MUHIYA_BENCH_JSON=1`, which has no defined result schema beyond `completed:bool` and no trajectory output.
- **Why it harms benchmark performance**: A Harbor harness that does `docker run muhiyacode <task>` expects a defined result + trajectory on stdout/files. Nothing produces it; the run is ungradeable without the operator writing the whole adapter.
- **Example failure scenario**: Harbor expects `result.json` with `status`/`trajectory`/`usage`; `muhiyacode -p` prints prose + one `muhiya_bench` line with `completed:true` and no trajectory → ungradeable.
- **Recommended solution**: Add a `muhiyacode bench` subcommand (or `cmd/terminalbench-agent`) that: (a) reads the task from `--task <file>` or stdin; (b) runs `Engine.Run` under `context.WithTimeout`; (c) emits one self-contained JSON result to stdout (and/or a `--out` path) with `status` ∈ {pass,fail,timeout,blocked,error}, `session_id`, `models_used`, `usage`, `cost`, `tool_calls`, `duration_ms`, `checks_run`, `trajectory_path`, `errors`; (d) exits with distinguishable codes (0 pass / 2 fail / 3 timeout / 4 blocked). Reuse the existing one-shot path, do not reinvent it.
- **Implementation complexity**: L
- **Dependencies/risks**: Touches `cmd/`, `internal/command`, `internal/orchestrator`. Requires B-1, B-3, B-4, B-5, B-9 to be useful. Must not change the interactive TUI path.
- **How to test the fix**: Spawn the adapter in a fresh container on a trivial Go task → exit 0, valid JSON, `status:pass`, readable trajectory; a timeout task → exit 3.
- **Metric expected to improve**: human-intervention rate (enables automated runs at all)

### High

#### [B-3] No hard wall-clock task timeout; only a turn ceiling
- **Severity**: High
- **Layer**: Agent-level
- **Subsystem**: Task timeout / termination
- **Affected locations**: `internal/orchestrator/turnloop.go:30` (`context.WithCancel`, not `WithTimeout`), `:244-246` (only `ctx.Err()` check), `internal/command/root.go:59-63,118-128` (no `--timeout`), `cmd/muhiyacode/main.go:30` (`signal.NotifyContext`, no deadline), `internal/orchestrator/engine.go:16-19` (ceiling only).
- **Current behavior**: `Engine.Run` wraps the parent context with `WithCancel`; no deadline. The only bound is the turn ceiling (120×effort). A task with slow turns can run far past any benchmark budget; external SIGKILL loses the bench summary.
- **Why it harms benchmark performance**: Benchmarks enforce a per-task wall budget. Without an internal deadline the agent relies on external kill, which (for SIGKILL) emits no summary and (for SIGTERM) is mislabeled `StopCauseUserStop` (B-4).
- **Example failure scenario**: 600s budget; agent runs 40 slow turns; harness SIGKILL at 600s; no summary; run recorded `completed:false,reported:false`.
- **Recommended solution**: Add `--task-timeout`/`MUHIYA_TASK_TIMEOUT` that wraps the `Engine.Run` context in `context.WithTimeout` in `runOneShot`; on expiry finalize with a distinct timeout status (B-4). Move `emitBenchSummary` into a `defer` so it always runs.
- **Implementation complexity**: M
- **Dependencies/risks**: Coordinate with the `turnloop.go` defer so stats still compute on timeout; `Close()` wait must not exceed remaining budget.
- **How to test the fix**: `--task-timeout 5s` against a prompt that sleeps 30s in `run_shell` → exits ~6s with `status:timeout` and a valid summary.
- **Metric expected to improve**: timeout rate, P95 time

#### [B-4] Bench summary omits status, stop-cause, duration, tool-call count, checks-run, files-changed, model/effort config
- **Severity**: High
- **Layer**: Agent-level
- **Subsystem**: Benchmark output contract
- **Affected locations**: `internal/command/benchjson.go:48-64` (schema), `:103-130` (emission), `:105` (`Completed` only), `internal/contract/types.go:387-453` (TaskStats HAS `DurationMS`,`StopCause`,`TerminatedReason`,`ToolCalls`,`ChecksRun`,`FilesChanged`,`HarnessEvents`,`Invalidations` — none emitted), `internal/command/root.go:128-143`.
- **Current behavior**: `benchSummary` emits `task_class,completed,turns,usage,cost_usd,cost_estimated,review,violations,per_pairing,read_gate`. `Completed = runErr==nil && StopCause=="" && TerminatedReason==""`. The reason/duration/tool-count/checks/config are dropped. `StopCause` has no `timeout` value (`types.go:455-460`: only user-stop/error/disconnect) and the defer sets `StopCauseUserStop` for any `ctx.Err()` (`turnloop.go:143-148`), so a timeout is mislabeled "user stop."
- **Why it harms benchmark performance**: A harness cannot distinguish pass/fail/timeout/blocked/error, cannot report per-run timing or whether verification ran, cannot attribute the model used. Failure analysis and metric segmentation are impossible.
- **Example failure scenario**: Two tasks both `completed:false` — one timed out, one hit the H5 breaker — indistinguishable.
- **Recommended solution**: Extend `benchSummary` with `Status` (derived from `StopCause`/`TerminatedReason`/`runErr`), `StopCause`, `TerminatedReason`, `DurationMS`, `ToolCalls`, `ChecksRun`, `FilesChanged`, `HarnessEvents`, `Invalidations`, `SessionID`, `Effort`, `ModelsUsed`, and `Errors`. Add `StopCauseTimeout="timeout"`; in `runOneShot` use `context.WithCancelCause` + a typed `ErrTaskTimeout` and have the defer detect `errors.Is(ctx.Err(), ErrTaskTimeout)` → `StopCauseTimeout`. Distinguish `context.DeadlineExceeded` (timeout) from `context.Canceled` (user stop).
- **Implementation complexity**: S (fields) / M (timeout cause plumbing)
- **Dependencies/risks**: `bench_011.sh` jq tolerates new fields; keep `Completed` semantics stable for back-compat.
- **How to test the fix**: Drive `emitBenchSummary` with a timeout `runErr`, a breaker `TerminatedReason`, and a 401 → assert distinct `status` values; assert `durationMs`/`toolCalls` present.
- **Metric expected to improve**: timeout rate (observable), test-execution rate (via `checksRun`), human-intervention rate

#### [B-5] No CLI/env overrides for model/provider/apiKey/effort/timeout/budget; config locked to mutable persisted file
- **Severity**: High
- **Layer**: Agent-level
- **Subsystem**: Configurability / container startup
- **Affected locations**: `internal/command/root.go:59-63` (only 5 flags), `internal/command/runtime_build.go:81-88,107` (`LoadSettings`/`LoadSecrets` from disk), `:114-129` (discovery + `SaveSettings` writes back), `internal/state/config.go:35-65,228-288` (`SetConfig` only via `config set` subcommand which writes to disk).
- **Current behavior**: No `--model`/`--base-url`/`--api-key`/`--effort`/`--task-timeout`/`--token-budget`/`--cost-budget` flags or env. The runner must pre-write `~/.muhiya/settings.json`; the runtime mutates it mid-run via discovery.
- **Why it harms benchmark performance**: Cannot hold config constant per run or vary it across configurations without editing a shared mutable file (breaks container isolation and reproducibility); an API key cannot be injected per run without persisting; "single" vs "mixed" config labels in the existing runner are cosmetic.
- **Example failure scenario**: "mixed" config run but `settings.json` still holds the prior "single" model → every "mixed" task silently uses the wrong model.
- **Recommended solution**: Add CLI flags and env vars (`MUHIYA_MODEL`, `MUHIYA_API_KEY`, `MUHIYA_BASE_URL`, `MUHIYA_EFFORT`, `MUHIYA_TASK_TIMEOUT`, `MUHIYA_TOKEN_BUDGET`, `MUHIYA_COST_BUDGET`, `MUHIYA_CONTEXT_LIMIT`) that override loaded settings in-memory only (never write back). Apply in `openApplicationCore` after `LoadSettings`/before `NewEngine`. In bench mode, skip discovery and the `SaveSettings` write-back.
- **Implementation complexity**: M
- **Dependencies/risks**: The advisor/capability paths read `settings.Provider.ActiveModelID`; keep the interactive path unchanged.
- **How to test the fix**: `muhiyacode -p "x" --model foo --effort low --api-key k` in an empty `MUHIYA_HOME` → uses model `foo`, does not create `settings.json`.
- **Metric expected to improve**: reproducibility, human-intervention rate

#### [B-6] One-shot refuses to run without pre-baked ~/.muhiya config
- **Severity**: High
- **Layer**: Agent-level
- **Subsystem**: Headless mode / container startup
- **Affected locations**: `internal/command/root.go:125-127` (`configurationNotice` gate before `Engine.Run`), `:573-585` (`configurationNotice`), `internal/command/runtime_build.go:81-88`.
- **Current behavior**: If `secrets.ProviderAPIKey==""`, no active model, or `model.ContextLimit<=0`, `runOneShot` returns a setup error before running. A clean container has none of these.
- **Why it harms benchmark performance**: A pristine container (exactly what Terminal-Bench spawns) errors out with a setup message; every task scores 0 unless the image bakes a config in.
- **Example failure scenario**: `docker run --rm muhiyacode -p "fix the bug"` → prints "Setup required: run `muhiyacode login`..." and exits non-zero.
- **Recommended solution**: When inline config flags/env (B-5) supply the missing fields, skip the `configurationNotice` gate for those fields. Accept `MUHIYA_API_KEY`+`MUHIYA_MODEL`+`MUHIYA_CONTEXT_LIMIT` to satisfy the gate with no on-disk file.
- **Implementation complexity**: S (after B-5)
- **Dependencies/risks**: Must still surface the notice when config is genuinely incomplete (no flags, no file).
- **How to test the fix**: One-shot in an empty `MUHIYA_HOME` with `MUHIYA_API_KEY`/`MUHIYA_MODEL` set → engine runs instead of returning the setup error.
- **Metric expected to improve**: task pass rate, human-intervention rate

#### [B-7] No enforced final verification stage; success declared on "model stopped / cap hit," not on tests passing
- **Severity**: High
- **Layer**: Agent-level
- **Subsystem**: Verification / pass-fail semantics
- **Affected locations**: `internal/orchestrator/turnloop.go:498-560` (finalize paths), `:602-604` (`checksRun` counter only), `internal/orchestrator/turnhelpers.go:115-151` (`finalize`→`appendCompletionDisclosure` only reconciles `tasks.md`), `internal/orchestrator/turnsignals.go:35-46` (`isCheckCall`), `internal/instructions/prompt.go:48` (verification is prose), `internal/command/benchjson.go:105`.
- **Current behavior**: `finalize` returns whenever the model stops emitting tool calls or the cap is hit. `checksRun` is informational. `AutoReview` only fires at max effort with ≥2 files and asks to re-read, not run tests (`turnloop.go:534-559`, `effort.go:55-61`). Nothing runs the repo's tests or inspects repo state before declaring success.
- **Why it harms benchmark performance**: Terminal-Bench grades on objective repo state. The agent can declare "DONE: tests pass" without running them; the harness accepts it; pass rate depends entirely on the model volunteering to run tests.
- **Example failure scenario**: "make `pytest` pass" — model edits, writes "DONE: tests pass" without invoking `run_shell` → `len(calls)==0` → `finalize` → `completed:true`; grader's `pytest` fails.
- **Recommended solution**: Add a verification gate before `finalize` for code-task classes: when `len(filesChanged)>0` and `checksRun==0` and a test runner is discoverable from project markers (`go.mod`→`go test ./...`, `package.json`→`npm test`, `Makefile`→`make test`, `Cargo.toml`→`cargo test`, `pytest`/`tox.ini`/`pyproject.toml`→`pytest`), inject one bounded forced verification turn (counted against the ceiling, non-re-entrant) instructing the model to run the check and report the real result. Record `VerificationRan`/`VerificationResult` in `TaskStats`. At minimum, surface `checksRun` in the bench summary (B-4) so `completed:true && checksRun==0` is flagged suspect.
- **Implementation complexity**: L
- **Dependencies/risks**: Use only project markers, never task-specific test names (anti-leakage). The forced turn must respect the timeout/ceiling and be skipped for read-only/chat classes. `Classify` already gates by class.
- **How to test the fix**: A `go.mod` fixture where the model edits but never checks → assert a `go test` turn is forced and `VerificationRan=true`; assert `completed:false` when the forced test fails.
- **Metric expected to improve**: test-execution rate, first-attempt success rate, task pass rate

#### [B-8] No cost-budget or token-budget abort mid-task
- **Severity**: High
- **Layer**: Agent-level
- **Subsystem**: Budget enforcement
- **Affected locations**: `internal/orchestrator/turnloop.go:654-660` (tool-budget is advisory only), `internal/orchestrator/classify.go:31-39` (Budget has no USD/token cap), `internal/orchestrator/engine.go:534-543` (`outputBudget` caps per-request output only), `internal/contract/cache.go:179-205` (`SumCreditsUSD`), `internal/command/benchjson.go:114-117`.
- **Current behavior**: `budget.ToolCalls` overrun only sets `taskOverBudget` and appends a governor notice — it does not stop the loop. No USD or token cap is checked. A "progress"-making task runs to the turn ceiling regardless of cost.
- **Why it harms benchmark performance**: Cost-controlled benchmarks need the agent to self-bound spend; without it a runaway task blows cost/run and cost/success, and external kill loses the summary.
- **Example failure scenario**: A task re-reads large files every turn, 100k prompt tokens/turn for 48 turns at max effort — several dollars — no stop.
- **Recommended solution**: Add `CostBudgetUSD`/`TokenBudget` to the run config (B-5); in the turn-loop prologue (alongside `ctx.Err()` at `turnloop.go:244`) compare `SumCreditsUSD(e.usageRecords)`/cumulative tokens against the budget and, on breach, set `TerminatedReason="cost/token budget exceeded"` and `return e.finalize(...)`. Treat `nil` cost as "unknown, do not abort."
- **Implementation complexity**: M
- **Dependencies/risks**: `SumCreditsUSD` returns nil if any priced member lacks `CostUSD` (member-set honesty) — guard against false aborts. Check after `recordMainUsage` so the latest spend counts.
- **How to test the fix**: Scripted provider reporting escalating `CostUSD`; set `CostBudgetUSD` just above the first record → assert finalize with `TerminatedReason` set and `CreditsUSD ≤ budget`.
- **Metric expected to improve**: cost/run, cost/success

#### [B-9] Trajectory not in bench output; transcript orphaned with no session link; no per-tool-call duration
- **Severity**: High
- **Layer**: Agent-level
- **Subsystem**: Trajectory & usage logging
- **Affected locations**: `internal/command/benchjson.go:48-64` (no `session_id`/trajectory), `internal/state/session.go:51-75` (`AppendTranscript` fsync'd, no duration), `:77-79` (`usage.jsonl`), `internal/orchestrator/turnloop.go:616-624` (transcript entry has `createdAt` only), `internal/contract/types.go:470-472` (`ToolStart`/`ToolEnd` callbacks exist but unused for timing).
- **Current behavior**: Every tool call + result + target + `createdAt` is persisted to `~/.muhiya/sessions/<id>/transcript.jsonl` with per-line `Sync()`, and usage to `usage.jsonl`. But `emitBenchSummary` emits none of this and no `session_id`. Per-tool-call duration is not recorded anywhere.
- **Why it harms benchmark performance**: The harness receives a single summary line with no link to the trajectory and no per-tool timing; it must discover the session dir and open the SQLite/JSONL internals. P50/P95 tool-latency is unmeasurable.
- **Example failure scenario**: Harness captures the `muhiya_bench` line; to diagnose a failure it must mount `~/.muhiya` and scrape `sessions/*/transcript.jsonl`, possibly picking the wrong session when the dir is shared.
- **Recommended solution**: Add `session_id` and `trajectory_path` to the bench summary (B-4). In the adapter (B-2), emit/copy the transcript+usage JSONL alongside the result, or print the trajectory path. Record per-tool-call `durationMs` by capturing `time.Now()` before `executeBatch` per call and storing elapsed in the transcript map and a `TaskStats` trajectory field.
- **Implementation complexity**: M
- **Dependencies/risks**: Transcript entries are append-only JSONL (additive). `executeBatch` may run calls in parallel — measure per-call, not per-batch. Apply redaction (`turnhelpers.go:153-158`).
- **How to test the fix**: One-shot task → assert summary's `session_id` resolves to a `transcript.jsonl` whose entries include `durationMs`.
- **Metric expected to improve**: P50/P95 time (per-tool), tool-call count observability, human-intervention rate

#### [B-10] Model advisor / catalog reconciliation can silently switch the configured model in one-shot/bench mode
- **Severity**: High
- **Layer**: Agent-level
- **Subsystem**: Automatic model routing / fixed-model integrity
- **Affected locations**: `internal/orchestrator/advisor.go:79-89` (`shouldRunAdvisor` true unless off/pinned), `:125-147` (`reconcileCatalog` substitutes a missing model), `:156-169` (`substituteFor` = largest context), `:176-252` (`runTaskAdvisor` may `applyModelSwitch`), `internal/orchestrator/engine.go:479,484-488`; `internal/command/root.go:118-128` (no pin under `MUHIYA_BENCH_JSON=1`).
- **Current behavior**: In one-shot mode the advisor is not disabled and `RolesPinned` is not set, so the advisor can reroute the task to a different model at the task boundary (notice-only, lost in headless). `reconcileCatalog` runs independently of the advisor and silently substitutes the largest-window model if the configured one is missing — also notice-only.
- **Why it harms benchmark performance**: A "fixed-model" run can be silently scored under the wrong model, breaking reproducibility and fixed-model integrity; the switch event/reason is absent from the bench JSON.
- **Example failure scenario**: Fixed-model run targeting `deepseek-chat`; advisor judges `minimax-m3` a better fit and switches; the run is scored under the wrong model.
- **Recommended solution**: In bench mode, force `RolesPinned=true`/`Advisor="off"` unless the operator explicitly requests routing (`--advisor routed`). Make a missing configured model a HARD failure (non-zero exit, `status:blocked`) in bench mode rather than a silent substitution. Add `model_switches` (from/to/reason/turn) and `invalidations` to the bench summary (B-4).
- **Implementation complexity**: S
- **Dependencies/risks**: Operators who want routed runs need an explicit opt-in; document both modes. Keep interactive substitution UX.
- **How to test the fix**: One-shot with `MUHIYA_BENCH_JSON=1` and a multi-model catalog → assert `ActiveModelID` unchanged after `Engine.Run` and every request used it; seed a catalog without the configured model → assert a clear error, not a silent swap.
- **Metric expected to improve**: reproducibility, human-intervention rate

#### [B-11] No post-headers request lifetime cap; keep-alive drip can hang one request unbounded
- **Severity**: High
- **Layer**: Agent-level
- **Subsystem**: Provider timeouts / SSE
- **Affected locations**: `internal/gateway/provider.go:282-288` (`RequestLifetime` is first-byte only, `lifetime.Stop()` on headers), `:300` (idle timer reset on every `scanner.Scan()` line including `: keep-alive`), `:75-79,178-179`.
- **Current behavior**: After headers, only the rolling `IdleTimeout` (110s) governs, and it resets on keep-alive comment lines. A provider dripping `: keep-alive` every <110s keeps the request alive indefinitely.
- **Why it harms benchmark performance**: One queued/slow request can consume the entire wall budget on a single turn with zero progress.
- **Example failure scenario**: A queued upstream sends headers, then `: keep-alive` every 90s while generating; the request runs 14m+ on one turn; the task times out with no tool calls.
- **Recommended solution**: Add a `StreamLifetime` (separate from first-byte `RequestLifetime`) bounding total post-headers streaming, NOT reset by keep-alive comments — only by real data/payload frames. Keep a shorter liveness timer for keep-alive. Make it generous (10–15m) and configurable.
- **Implementation complexity**: M
- **Dependencies/risks**: Must not cut off legitimately long single answers; bound generously.
- **How to test the fix**: Extend `provider_timeout_test.go` with a server dripping keep-alive past `IdleTimeout` and `StreamLifetime` → assert failure at the stream lifetime.
- **Metric expected to improve**: timeout rate, P95 time

#### [B-12] Turn budget escalates only after files change; exploration-heavy tasks are cut off before editing
- **Severity**: High
- **Layer**: Agent-level
- **Subsystem**: Task interpretation / budgeting
- **Affected locations**: `internal/orchestrator/classify.go:67-197` (regex classification, `classTurns` small=16/standard=26), `internal/orchestrator/turnloop.go:270-276` (ladder requires `len(filesChanged)>0`), `internal/orchestrator/effort.go:33-62`.
- **Current behavior**: `BudgetFor` sets `MaxTurns=min(effort.MaxTurns, classTurns[class])`. The runway ladder fires only when `turns>=turnCap && len(filesChanged)>0`. During pure exploration (reads/inspects, no writes) `filesChanged` is empty, so the ladder never escalates and `isFinal` force-finalizes at the small cap mid-exploration.
- **Why it harms benchmark performance**: Many tasks need substantial exploration before the first edit; the agent is stopped before writing anything → automatic failure.
- **Example failure scenario**: "find and fix the off-by-one in the parser" in a 30-file repo; 16 turns of reading/grepping, no edit, hits cap, returns a text answer with zero edits → fail.
- **Recommended solution**: Decouple exploration runway from edit state: allow one or two bounded ladder escalations when `turns>=turnCap && sawToolCall && len(filesChanged)==0` (cap no-edit escalations to avoid pure-read loops). Optionally base the budget on repo size rather than prompt length. Keep H5/ceiling as backstops.
- **Implementation complexity**: M
- **Dependencies/risks**: The no-edit escalation must stay bounded so pure-read loops still terminate.
- **How to test the fix**: Fake provider that only reads for N turns then edits → assert at least one escalation before finalizing when `sawToolCall && filesChanged==0`.
- **Metric expected to improve**: task pass rate, first-attempt success rate

#### [B-13] Final-turn tool calls are silently dropped, not executed or recorded
- **Severity**: High
- **Layer**: Agent-level
- **Subsystem**: Termination / verification
- **Affected locations**: `internal/orchestrator/turnloop.go:498-506`.
- **Current behavior**: When `isFinal`, the loop returns `e.finalize(...)` immediately after the provider response, before `assistantReplayMessage`/`executeBatch`. Any `response.ToolCalls` on the final turn are never dispatched and never appended.
- **Why it harms benchmark performance**: A model's last-turn verification command is dropped; the answer is whatever prose it emitted. This undermines final verification and can mask a failing task as completed.
- **Example failure scenario**: On the cap turn the model emits `run_shell: pytest` + "Running final tests..." → call dropped, "Running final tests..." becomes the answer, `completed:true`, tests never ran.
- **Recommended solution**: On the final turn, if `len(calls)>0`, execute the batch first (as the terminal turn, no recursion) then finalize with the post-execution text; or at minimum record the dropped calls in `stats` and surface a notice so it is not silent.
- **Implementation complexity**: S
- **Dependencies/risks**: Executing on the final turn extends wall-clock by one dispatch; respect `ctx.Err()`. Record-only alternative has no risk.
- **How to test the fix**: Fake provider returns a tool call on the `isFinal` turn → assert the call is executed (or recorded) and the answer reflects its result.
- **Metric expected to improve**: test-execution rate, first-attempt success rate

#### [B-14] Do-nothing turn-1 (no tool calls, empty/no text) finalizes immediately as "Done." with completed:true
- **Severity**: High
- **Layer**: Agent-level
- **Subsystem**: Empty-response handling / finalization
- **Affected locations**: `internal/orchestrator/turnloop.go:507-560,515`, `internal/orchestrator/turnhelpers.go:235-243`, `internal/command/benchjson.go:105`.
- **Current behavior**: On a non-final turn with `len(calls)==0`, the empty-final re-prompt and trailing-intent guard both require `sawToolCall` (true only after a tool ran). On turn 1 `sawToolCall` is false, so a no-tool/no-text response skips both and reaches `finalize` → `fallbackAnswer("")` returns "Done." → `completed:true`.
- **Why it harms benchmark performance**: An overloaded/empty turn-1 causes zero work and a false success; the agent never re-prompts to force action. Tanks pass rate on flaky-model days and misleads harnesses that trust `Completed`.
- **Example failure scenario**: First request returns `finish_reason=stop`, empty content, no tool calls → agent prints "Done." and exits `completed:true` having touched nothing.
- **Recommended solution**: On a non-final turn with no tool calls and no/empty text when `!sawToolCall`, inject at least one bounded re-prompt ("No work done yet and no tool called; take a concrete action or state the genuine blocker") before finalizing. Bound the retry count.
- **Implementation complexity**: S
- **Dependencies/risks**: Must not loop on a model that truly has nothing to do; bound retries.
- **How to test the fix**: Script a provider returning an empty turn-1 → assert at least one re-prompt rather than finalizing with "Done."
- **Metric expected to improve**: task pass rate, first-attempt success rate

#### [B-15] `read_file` truncates every line to 500 chars — breaks `edit_file` oldString on long lines
- **Severity**: High
- **Layer**: Agent-level
- **Subsystem**: File reading
- **Affected locations**: `internal/workspace/files.go:116-122` (`TruncateEllipsis(line,500)`), `internal/workspace/registry.go:73-97` (no raw mode), `internal/contract/format.go:82-91`.
- **Current behavior**: Every returned line is rune-sliced at 499 + "…". `edit_file`/`multi_edit` exact-match against real bytes, but the model builds `oldString` from the truncated view → never matches for >500-char lines (long URLs, minified lines, big literals, generated configs).
- **Why it harms benchmark performance**: The edit fails with "oldString not found"; the model flails (re-reads, retries truncation guesses) burning turns/tokens, or uses `write_file` to rewrite the whole file risking the unseen remainder.
- **Example failure scenario**: "Fix the API base URL in config.py" where the line is a >500-char URL+query string → truncated read → `oldString` never matches.
- **Recommended solution**: Add an opt-in `fullLines`/`raw` param to `read_file` that disables per-line truncation, and/or auto-disable truncation when `limit` is small (≤50 lines). Keep the 500-char default for large scans. Alternatively, have `edit_file`'s "not found" error include the actual (untruncated) closest lines so the model can copy exact bytes.
- **Implementation complexity**: S
- **Dependencies/risks**: Increases output size when used; bound unbounded reads. Schema change is session-stable only if tool definition order/shape is preserved (prefix-shape guard).
- **How to test the fix**: Read a 600-char line with the new mode → full line returned; a subsequent `edit_file` with that exact line as oldString succeeds first attempt.
- **Metric expected to improve**: tool-error rate, first-attempt success rate, tool-call count

#### [B-16] `inspect_code` is Go-only and caps output at 2000 bytes — wastes a turn on every non-Go task
- **Severity**: High
- **Layer**: Agent-level
- **Subsystem**: Code inspection
- **Affected locations**: `internal/workspace/codeindex.go:566-568` (rejects non-`.go`), `:594-597`, `:17` (`maxOutputBytes=2000`), `internal/workspace/codeindex_exec.go:47-52,155-160`, `internal/workspace/registry.go:38` (advertised unconditionally, no language gate in description).
- **Current behavior**: Non-Go files hard-error "inspect_code only supports Go source files (.go)". Output is capped at 2000 bytes, so a directory outline is silently incomplete. The tool is advertised unconditionally with no "Go only" hint.
- **Why it harms benchmark performance**: On any Python/JS/TS/Rust/C++ task the model tries `inspect_code`, wastes a turn on a hard error, then falls back to grep/read. Even on Go, large outlines truncate and miss the target symbol.
- **Example failure scenario**: A Python refactor task → `inspect_code mode=outline path=.` → hard error → re-plan with grep/glob.
- **Recommended solution**: (a) Gate advertisement on the workspace containing Go files (or make it multi-language via Tree-sitter); (b) make the tool description explicitly state "Go only"; (c) raise/paginate `maxOutputBytes` so outlines aren't silently incomplete; (d) at minimum return a guidance-rich error ("Go only; use grep/glob for other languages") so the model falls back immediately.
- **Implementation complexity**: S (description/error) / M (multi-language)
- **Dependencies/risks**: A description change busts the cached prefix once (handled by the toolset-change invalidation path).
- **How to test the fix**: `inspect_code` on a `.py` file → error names the Go-only limit and suggests grep; a Go dir with >50 symbols → no silent truncation (or a continuation marker).
- **Metric expected to improve**: tool-error rate, tool-call count, first-attempt success rate

#### [B-17] `multi_edit` commits a partial batch as success when some edits apply and others skip
- **Severity**: High
- **Layer**: Agent-level
- **Subsystem**: File editing
- **Affected locations**: `internal/workspace/files.go:266-303` (applies sequentially; skipped edit appended, loop continues; writes if `current!=before`), `internal/workspace/registry.go:179-195,222-232` (`editMismatchError` returns nil when `result.Diff!=""`).
- **Current behavior**: If edit #1 applies but edit #2's `oldString` is not found, #2 is skipped and the file is still written; the call returns success with a buried "edit 2: …" note. The model may not notice the skip and proceeds assuming the whole batch applied.
- **Why it harms benchmark performance**: A coordinated multi-edit (e.g. "rename X to Y in 3 places") leaves the task half-done; the skipped edit is not auto-retried; the verbatim-failed cache is not populated (call "succeeded").
- **Example failure scenario**: 3-edit rename: #1 and #3 match, #2 has a subtle whitespace diff and skips → success returned → one call site unrenamed → build fails or passes if untested.
- **Recommended solution**: Make `multi_edit` atomic: if any edit is skipped (other than the legitimate "already present"/"identical" idempotent skip), do NOT write and return a failure listing all skipped edits with closest-region hints. Or, if partial application is intentional, return a non-nil error whenever `len(Skipped)>0 && AppliedEdits>0` so loop guards engage and the model must reconcile.
- **Implementation complexity**: S
- **Dependencies/risks**: Existing callers/tests relying on partial success need review; keep the "already present" idempotent skip as success.
- **How to test the fix**: `multi_edit` where #1 matches and #2 does not → file NOT written, call returns failure naming both applied and skipped.
- **Metric expected to improve**: first-attempt success rate, tool-error rate, human-intervention rate

#### [B-18] `rm -rf` hard-blocked everywhere with no in-workspace exception
- **Severity**: High
- **Layer**: Agent-level
- **Subsystem**: Safe command execution
- **Affected locations**: `internal/workspace/risk.go:83-102` (`destructiveDeleteRisk` blocks any rm with recursion+force, no path reasoning), `:125-155` (`deleteSwitches` only flags, never operand), `internal/workspace/permissions.go:167-172` (hard error on `Blocked` even in auto-accept), `internal/workspace/files.go:341-348`.
- **Current behavior**: `rm -rf build`, `rm -rf node_modules`, `rm -rf dist` are refused outright regardless of target.
- **Why it harms benchmark performance**: Many build/setup tasks need to clean a generated directory before reinstall/rebuild; the agent cannot perform the canonical cleanup, so it abandons, works around with slower per-file deletes, or fails the build on stale artifacts.
- **Example failure scenario**: "the build is broken because of stale cache; fix it" → correct fix is `rm -rf node_modules && npm install` → `ClassifyShell` blocks it → rebuild fails on stale artifacts.
- **Recommended solution**: When `ClassifyShell` flags a recursive-force delete, resolve the path operands (reuse `shellsafe.Segments`) and allow the command when every operand canonicalizes inside the workspace root and is not sensitive. Keep the hard block for `/`, `..`, `~`, absolute paths outside root, operand-free `rm -rf /`. Gate the permissive branch on `Mode==auto-accept` (or a benchmark flag) so interactive users keep the blanket block.
- **Implementation complexity**: M
- **Dependencies/risks**: Operand resolution must use `CanonicalPath` and reject unresolvable/symlink-escaping targets. Keep the laundering protection (command-position tokenizer stays).
- **How to test the fix**: Auto-accept in `t.TempDir()` → `ApproveShell("rm -rf "+root+"/build")` succeeds; `ApproveShell("rm -rf /")` and `rm -rf ~/.ssh` still denied.
- **Metric expected to improve**: task pass rate, tool-error rate

### Medium

#### [B-19] Compaction is lossy and non-deterministic — drops file contents and prior decisions
- **Severity**: High (rated Medium by some auditors; conservatively High for pass rate)
- **Layer**: Agent-level
- **Subsystem**: Context / compaction
- **Affected locations**: `internal/orchestrator/maintenance.go:47-101` (last 60 messages, `Digest(content,500)` truncate, model summarize at temp 0.1), `internal/orchestrator/history.go:462-488` (`CompactTo(summary,2)`), `internal/contract/format.go:131-133`.
- **Current behavior**: Compaction digests each message to 500 chars, asks the model to summarize under structured headings, keeps only the last 2 conversation units. File contents read earlier are gone from the message log; they survive only as 500-char digests plus whatever the model retained. Non-deterministic (temp 0.1).
- **Why it harms benchmark performance**: A multi-file task that reads files early, then compacts, loses the exact bytes needed for a later edit → re-read (re-paying tokens) or edits against a misremembered value. Non-determinism breaks run-to-run reproducibility.
- **Example failure scenario**: "Refactor X in auth.py and update its two callers" — reads auth.py + 2 callers; compaction fires; summary drops exact signatures; model edits auth.py then reconstructs caller signatures from the summary and gets them wrong.
- **Recommended solution**: Before folding, persist a compact file-content cache keyed by path+mtime for any `read_file` result being dropped (extend the inspection ledger's coverage, or archive via `SetPruneArchive` at `history.go:89`). Have the compaction prompt enumerate "FILES read this session with current mtime" so the model knows what it can re-read cheaply. Use `temperature:0` + fixed seed where supported; fall back to the mechanical digest list deterministically.
- **Implementation complexity**: L
- **Dependencies/risks**: Touches `maintenance.go`, `history.go`, `inspection.go`. Adds storage; stay secret-screened.
- **How to test the fix**: Read a 2000-line file, force compaction → assert the model can recover an exact line range without token penalty (re-read deduped against retained coverage); run twice → byte-identical summaries.
- **Metric expected to improve**: first-attempt success rate, cost/success, reproducibility

#### [B-20] No impossible/blocked detection; successful-but-unproductive loops run to the 120–168-turn ceiling
- **Severity**: Medium
- **Layer**: Agent-level
- **Subsystem**: Termination / self-correction
- **Affected locations**: `internal/orchestrator/turnloop.go:631-660`, `internal/orchestrator/dispatch.go:134-144`, `internal/orchestrator/engine.go:31-36`.
- **Current behavior**: The only force-terminators are H5 (8 failures/6 turns), the ceiling, one provider-error retry, and external cancel. The B7 all-failed guard and storm breaker only nudge. No detection for "impossible/blocked"; no `blocked` status. A task with successful-but-useless actions (re-reading, re-running a passing build) never hits H5 and runs to the ceiling.
- **Why it harms benchmark performance**: Wastes the full turn/cost envelope and returns a low-quality answer; cannot distinguish "genuinely blocked" from "ran out of runway."
- **Example failure scenario**: A task needing an absent dependency: the model `grep`s/`read_file`s distinct paths (all succeed), never edits, never fails, for 120 turns → "I could not find X." Cost maximal; could have been declared in 5 turns.
- **Recommended solution**: Add a "stalled progress" terminator: if N consecutive turns pass with `filesChanged` unchanged, no check-call, and no new distinct tool signature, force-finalize with `terminateReason="no measurable progress in N turns"` (N≈6–8). Combine with a distinct `blocked` status (B-4).
- **Implementation complexity**: M
- **Dependencies/risks**: Must not fire on legitimate long exploration (gate on `sawToolCall` + unchanged file set + no checks).
- **How to test the fix**: Fake provider emitting only successful `read_file` on distinct paths for 10 turns → assert finalize with no-progress reason before the ceiling.
- **Metric expected to improve**: cost/run, P50/P95 time, timeout rate

#### [B-21] Repeat limiter evaded by trivial argument variation; no successful-loop detection
- **Severity**: Medium
- **Layer**: Agent-level
- **Subsystem**: Dispatch gate / loop protection
- **Affected locations**: `internal/orchestrator/gates.go:33-51` (repeat limiter keys exact signature), `internal/orchestrator/inspection.go:496-498` (`callSignature`), `internal/orchestrator/dispatch.go:134-144` (storm breaker counts failures only).
- **Current behavior**: The limiter blocks the 4th *identical* call; a trivially different command (`pytest`→`pytest -v`→`pytest tests/test_foo.py`) is a new signature. The storm breaker only counts *failed* classes. Successful-but-repetitive loops evade both, bounded only by the ceiling.
- **Why it harms benchmark performance**: A model stuck "trying slightly different commands" burns turns/cost until the ceiling with no early stop.
- **Example failure scenario**: `make`, `make clean`, `make`, `make -j4`, `make V=1` — five successful distinct-signature builds, no dedup, loops variants to the ceiling.
- **Recommended solution**: Add a tool-name frequency limiter: track per-tool-name call counts per sliding window and nudge/terminate when one tool exceeds a threshold (e.g. >12 `run_shell` in 10 turns) regardless of arg variation. Gate termination on lack of `filesChanged` since the last such call. Keep the ceiling as backstop.
- **Implementation complexity**: S
- **Dependencies/risks**: Must not block legitimate repeated builds after edits; gate on count + no progress.
- **How to test the fix**: Fake provider emits many distinct-arg `run_shell` calls → assert the per-tool limiter fires before the ceiling.
- **Metric expected to improve**: shell-loop rate, tool-call count, cost/run

#### [B-22] `ask_user` and onboarding auto-answer in headless mode, injecting synthetic clarifications
- **Severity**: Medium
- **Layer**: Agent-level
- **Subsystem**: Headless interaction / reproducibility
- **Affected locations**: `internal/command/root.go:649-664` (`Ask` returns recommended choice with no TTY check, unlike `Confirm`), `internal/orchestrator/toolhandlers.go:100-124`, `internal/orchestrator/definitions.go:15,38-42` (`ask_user` always advertised), `internal/orchestrator/turnloop.go:74-95`, `internal/orchestrator/onboarding.go:12-26,84-97`.
- **Current behavior**: In one-shot mode `callbacks.Ask` is non-nil and auto-selects the recommended choice (no `readerIsTerminal` guard). `ask_user` is always advertised as "blocking." Onboarding runs for short/vague prompts (common bench task shape) and injects auto-answered clarifications + an aux model call.
- **Why it harms benchmark performance**: A benchmark task must be executed as-is with no interaction. Auto-answering fabricates user decisions (the recommended default, not task-informed) that can steer the task wrong; onboarding adds non-deterministic prompt mutation + cost; `ask_user` wastes a turn.
- **Example failure scenario**: "Refactor X using approach A or B" → `ask_user` auto-returns "A" → model implements A, hidden tests expect B.
- **Recommended solution**: In one-shot/bench mode: (a) drop `ask_user` from `sessionDefinitions` (or make `askUser` return a tool error "unavailable in non-interactive mode; decide and proceed"); (b) disable onboarding (pass `Ask==nil` or a `--no-onboarding`/`MUHIYA_BENCH_NO_ONBOARDING=1` flag, which already short-circuits the onboarding branch). Default both off for the benchmark adapter.
- **Implementation complexity**: S
- **Dependencies/risks**: Must preserve interactive onboarding/ask for TUI; update wiring-inventory expectations for headless.
- **How to test the fix**: Non-TTY one-shot on an ambiguous prompt → assert no onboarding answers injected, no aux call, `ask_user` not advertised.
- **Metric expected to improve**: reproducibility, cost/run, first-attempt success rate

#### [B-23] Agent may `git commit`/`git push` to the task repo in auto-accept mode
- **Severity**: Medium
- **Layer**: Agent-level
- **Subsystem**: Git handling / task isolation
- **Affected locations**: `internal/workspace/registry.go:24-39` (only `git_status`/`git_diff` tools), `internal/instructions/prompt.go:87-92` (forbids destructive git but NOT commit/push), `internal/instructions/readonlyshell.go:13` (lists git mutating subcommands but only used in read-only/plan mode), `internal/workspace/risk.go` (`ClassifyShell` does not block `git commit`).
- **Current behavior**: In `PermissionAutoAccept` (required for headless), `ClassifyShell` does not block `git commit`/`git push`/`git add`, so the model can commit to the task repo via `run_shell`. The prompt only forbids destructive git, not commits.
- **Why it harms benchmark performance**: Graders typically inspect the working tree or run tests against it. A commit can make `git diff` show nothing or interfere with the harness's git state → false failures on correct work.
- **Example failure scenario**: Model runs `git add -A && git commit -m "fix"`; grader's `git diff HEAD~1` expects working-tree changes → task scored fail.
- **Recommended solution**: In benchmark/headless mode, add `git commit`/`git push`/`git add` to the auto-accept refusal set in `ClassifyShell` (an enforceable gate), and/or add a prompt clause "Do not commit or push; leave changes in the working tree" to the benchmark system prompt. Prefer the gate.
- **Implementation complexity**: S
- **Dependencies/risks**: `risk.go` is not part of the cached prefix, so blocking more commands is cache-safe. Gate to benchmark mode so interactive use is unaffected.
- **How to test the fix**: Auto-accept, model attempts `git commit` → refused, working tree stays uncommitted.
- **Metric expected to improve**: task pass rate (grader alignment)

#### [B-24] Cost not computed when the gateway omits `muhiya_log`; no local price fallback
- **Severity**: Medium
- **Layer**: Agent-level
- **Subsystem**: Cost accounting
- **Affected locations**: `internal/gateway/provider.go:292-296,340-367` (cost only from non-standard `muhiya_log`), `internal/command/benchjson.go:114-117` (`CostUSD` nil when unpriced).
- **Current behavior**: Cost is taken solely from the gateway's `muhiya_log` meta chunk; absence is "normal, never estimated locally." Against a direct/non-Muhiya gateway, `cost_usd` is 0/missing for every run.
- **Why it harms benchmark performance**: cost/run and cost/success are uncomputable against direct providers — a required benchmark dimension.
- **Example failure scenario**: A benchmark against a direct OpenAI-compatible endpoint reports `cost_usd:0` for all tasks → cost-optimal model comparison impossible.
- **Recommended solution**: Add an optional local price table (per-model input/output $/Mtok) used as a fallback when `muhiya_log` is absent, flagged `cost_estimated:true` (field exists). Let the bench adapter supply prices.
- **Implementation complexity**: M
- **Dependencies/risks**: Estimated cost must be clearly flagged to avoid conflating with gateway-reported cost.
- **How to test the fix**: Request against a server emitting no `muhiya_log` → `CostUSD>0` and `CostEstimated=true` from the fallback.
- **Metric expected to improve**: cost/run, cost/success

#### [B-25] `reasoning_effort` sent in the request body to unprofiled/generic models can 400 a strict provider
- **Severity**: Medium
- **Layer**: Agent-level
- **Subsystem**: Per-request effort transmission
- **Affected locations**: `internal/gateway/provider.go:236-243,378-388`, `internal/gateway/model.go:146` (generic profile has no `SupportedParams`).
- **Current behavior**: `reasoning_effort` is added to the body when `profileSupportsParam` is true, which returns TRUE when `SupportedParams` is empty (permissive fallback). The generic profile has no `SupportedParams`, so `reasoning_effort` IS emitted for any non-deepseek/minimax/glm model. The `X-Muhiya-Effort` header is always set.
- **Why it harms benchmark performance**: A strict OpenAI-compatible endpoint that rejects unknown body fields 400s every request; a 400 is non-retryable and fails the task.
- **Example failure scenario**: A self-hosted vLLM server with strict schema validation → every request 400s on `reasoning_effort` → 100% task failure.
- **Recommended solution**: For the generic/permissive case, omit `reasoning_effort` from the body and rely on the header alone. Only emit the body field for families that explicitly list it.
- **Implementation complexity**: S
- **Dependencies/risks**: None — the header already carries effort; the body field is redundant for generic models.
- **How to test the fix**: Assert the marshalled body for `ResolveModelProfile("unknown-model")` has no `reasoning_effort` key while DeepSeek's does.
- **Metric expected to improve**: tool-error rate, first-attempt success rate

#### [B-26] Cross-task state leakage if a benchmark batch reuses one engine/session
- **Severity**: Medium
- **Layer**: Agent-level
- **Subsystem**: Context/memory isolation
- **Affected locations**: `internal/orchestrator/engine.go:393-412` (`resetTaskState` clears per-task counters but NOT `history`/`inspection`/`knowledge`), `internal/orchestrator/turnloop.go:96`, `internal/command/root.go:118-144`.
- **Current behavior**: `resetTaskState` does not reset `e.history`, `e.inspection`, or `e.knowledge`. `runOneShot` calls `Run` once per process (safe for Harbor's one-process-per-task), but nothing prevents a batch harness from calling `Run` multiple times on one engine — history/ledger/knowledge bleed across tasks.
- **Why it harms benchmark performance**: Task N starts with task N-1's conversation, inspected-files map, and knowledge facts → cross-task contamination (benchmark leakage) and non-reproducibility.
- **Example failure scenario**: Batch runner processes task A then task B in one process; task B starts with task A's 30 turns of history and inspected-files → biased approach, inflated cost.
- **Recommended solution**: Add an explicit `ResetForNewTask()` (or a batch `--fresh` equivalent) that clears `history`/`inspection`/`knowledge` between tasks. Document that the supported benchmark mode is one-process-per-task (Harbor's model) and assert in `runOneShot` that only one `Run` occurs. At minimum, gate `Briefing`/`BriefingForScope` to the current epoch and ensure `inspection.Known()` does not suppress reads whose prior result is no longer in history.
- **Implementation complexity**: M
- **Dependencies/risks**: A history reset conflicts with conversational resume; make it opt-in for batch mode only.
- **How to test the fix**: Two unrelated tasks on one engine → assert task 2's first request has no task 1 history, knowledge facts, or inspected-file entries.
- **Metric expected to improve**: reproducibility, cost/run, first-attempt success rate

#### [B-27] Any workspace mutation invalidates ALL search-kind dedup entries — re-searches after every edit
- **Severity**: Medium
- **Layer**: Agent-level
- **Subsystem**: Read dedup / token waste
- **Affected locations**: `internal/orchestrator/inspection.go:225-267,339-354`.
- **Current behavior**: `InvalidateFor` wipes all search-kind signatures on any edit/write/patch/mutation-shell. After an edit, every prior grep/glob/list_files/inspect_code result is invalidated; an identical search re-runs fully.
- **Why it harms benchmark performance**: A read→search→edit→search-again-to-verify workflow re-runs and re-emits the full search result each cycle, multiplying search token cost. Conservative (an edit could touch any file) but wasteful for non-overlapping paths.
- **Example failure scenario**: "Add logging to every handler" — grep `def handler` (80 matches), edit handler1.py, grep `def handler` again → re-runs all 80 matches.
- **Recommended solution**: Refine search invalidation to be path-scoped where the search root is known and does not overlap the edited file. Keep the blanket wipe for searches rooted at `.` and for `run_shell`/`mcp__*` (unbounded). Store the search root in `InspectionEntry` and compare against the edited path.
- **Implementation complexity**: M
- **Dependencies/risks**: Could miss a glob that spanned the edited file; needs careful overlap logic. Keep the conservative wipe for unbounded mutations.
- **How to test the fix**: grep `src/utils`, edit `src/handlers/foo.py`, re-grep `src/utils` → second deduped (served from cache).
- **Metric expected to improve**: cost/run, tool-call count, repeated-read rate

#### [B-28] `env`/`printenv`/`set` and `chmod 777` hard-blocked — denylist tuned for interactive safety, not container permissiveness
- **Severity**: Medium
- **Layer**: Agent-level
- **Subsystem**: Safe command execution
- **Affected locations**: `internal/workspace/risk.go:33,41,42,62-72`, `internal/workspace/permissions.go:167-172`.
- **Current behavior**: `ClassifyShell` hard-blocks bare `env`/`printenv`/`set`, `cat env:`, and `chmod …777` even in auto-accept. Patterns exist to stop credential enumeration by a human-facing agent but also refuse legitimate inspection/permission commands.
- **Why it harms benchmark performance**: A task that needs to inspect env vars or fix permissions is blocked; the agent works around it (slower) or fails.
- **Example failure scenario**: "debug the build by checking NODE_ENV/PATH" → `printenv | grep NODE` blocked → falls back to guessing.
- **Recommended solution**: Gate the env-enumeration block behind non-auto-accept/interactive mode, or relax it to block only when a sensitive root is named in the same command. Allow `chmod 777` on in-workspace paths in auto-accept (mirror B-18's operand check). Keep the block for piping to a shell.
- **Implementation complexity**: M
- **Dependencies/risks**: Relaxing env enumeration weakens a credential-exfiltration guard; scope to auto-accept/headless. Coordinate with B-18's operand helper.
- **How to test the fix**: Auto-accept in-workspace → `printenv` and `chmod 777 ./run` succeed; Normal mode → `printenv` still blocked.
- **Metric expected to improve**: task pass rate, tool-error rate

#### [B-29] Startup network calls can hang/abort a one-shot run and mutate persisted settings
- **Severity**: Medium
- **Layer**: Agent-level
- **Subsystem**: Container / startup reliability
- **Affected locations**: `internal/command/runtime_build.go:114-129` (discovery `ListModels` 5s + `SaveSettings` write-back), `:387-399` (web probe 1.5s), `:427-430` (MCP refresh).
- **Current behavior**: `openApplicationCore` does model discovery + writes the catalog back to disk, a web-search probe, and an MCP refresh — all on the one-shot path before `Engine.Run`. In a restricted-network container these add latency and can fail/abort; the write-back mutates shared `~/.muhiya`.
- **Why it harms benchmark performance**: Adds seconds of startup latency per task (×200 tasks = minutes); a flaky network can abort before the task starts; the catalog drifts between tasks (B-26).
- **Example failure scenario**: Container with no outbound except the model endpoint → web probe fails → `web_search` silently disabled for tasks that need it; discovery's 5s timeout adds 5s × 200.
- **Recommended solution**: In benchmark/headless mode, skip discovery and the web probe (or make them best-effort non-fatal and never write to disk), relying on inline config (B-5) for the model and a pre-probed `web_search` capability flag. Gate the `SaveSettings` at `runtime_build.go:121` behind `!benchmarkMode`.
- **Implementation complexity**: S
- **Dependencies/risks**: Must not disable discovery for interactive users. The web probe already degrades gracefully when `BaseURL`/`apiKey` are empty.
- **How to test the fix**: One-shot with a mock endpoint and no general network → startup <1s, no `settings.json` write, `web_search` capability per the flag.
- **Metric expected to improve**: P50/P95 time, timeout rate

#### [B-30] No provider-failure circuit breaker; turn-level retry is a single per-task latch
- **Severity**: Medium
- **Layer**: Agent-level
- **Subsystem**: Provider-failure recovery
- **Affected locations**: `internal/orchestrator/turnloop.go:241,440-451`, `internal/orchestrator/turnhelpers.go:42-48`, `internal/orchestrator/engine.go:31-35` (H5 gates tool failures only).
- **Current behavior**: `turnRecovered` is set once per `Run` on the first recoverable provider error and never reset; the SECOND provider error anywhere in the task returns the error. No across-turn provider breaker.
- **Why it harms benchmark performance**: A long task that survives one blip is killed by the next blip 50 turns later — too aggressive for multi-hour runs. Conversely no breaker trips after N provider failures with a clear "provider unavailable" status.
- **Example failure scenario**: 60-turn task: 503 on turn 10 (recovered), 503 on turn 40 → task aborts despite 30 successful intervening turns.
- **Recommended solution**: Make `turnRecovered` a sliding window (N recoveries per M turns) and/or add a provider-failure breaker that force-finalizes with `TerminatedReason="provider unavailable"` after e.g. 3 provider errors in 10 turns.
- **Implementation complexity**: M
- **Dependencies/risks**: Must not reintroduce hammering a down provider; keep backoff.
- **How to test the fix**: Provider fails on turns 5 and 40 with successes between → task continues past the second failure.
- **Metric expected to improve**: task pass rate, timeout rate

#### [B-31] 429 throughput-throttling aborts after 3 retries; `Retry-After` capped at 30s
- **Severity**: Medium
- **Layer**: Agent-level
- **Subsystem**: Rate-limit recovery
- **Affected locations**: `internal/gateway/provider.go:668-673,278`, `internal/gateway/friendly.go:91`, `internal/orchestrator/turnloop.go:436-451`.
- **Current behavior**: A 429 is gateway-retryable up to `MaxRetries=3` with `Retry-After` capped at 30s. After gateway retries exhaust, `Recoverable(429)` is false for throttling → the task returns the error.
- **Why it harms benchmark performance**: A 60s rate-limit window gets only 30s, re-429s, and after ~37s the task dies — a longer wait would have succeeded.
- **Example failure scenario**: A benchmark burst triggers a 60s window; every task under it aborts after 37s instead of waiting.
- **Recommended solution**: Raise/remove the `Retry-After` cap for 429 (respect the server up to a generous bound, e.g. 120s); consider one turn-level wait-and-retry for throttling 429s (distinct from budget exhaustion, which correctly aborts).
- **Implementation complexity**: S
- **Dependencies/risks**: Keep the budget-exhaustion abort path so a permanent budget condition still stops.
- **How to test the fix**: Script a 429 with `Retry-After: 60` then success → request waits ~60s and succeeds.
- **Metric expected to improve**: task pass rate, timeout rate

#### [B-32] `bench_011.sh` runner has no per-task timeout and no per-task state isolation
- **Severity**: Medium (High for the existing runner's usefulness)
- **Layer**: Agent-level
- **Subsystem**: Benchmark runner
- **Affected locations**: `scripts/bench_011.sh:129-184` (no `timeout` wrapper, no `MUHIYA_HOME` per task), `:163-164`, `:182`.
- **Current behavior**: Each task is invoked with no `timeout`; a hung process blocks the whole run. `MUHIYA_HOME` is never set, so all tasks share `~/.muhiya` (sessions, DB, memory, knowledge accumulate); discovery writes back mid-run; one-shot defaults to resume not fresh.
- **Why it harms benchmark performance**: One hung task stalls the entire run; cross-task state leaks; a reused workspace path resumes stale history.
- **Example failure scenario**: Task 3 of 20 hangs on stdin-reading test → runner blocks at task 3; tasks 4–20 never run.
- **Recommended solution**: Wrap invocations with `timeout --preserve-status -s TERM -k 5s "$TASK_TIMEOUT" ...` (124 ⇒ `status:timeout`); set `MUHIYA_HOME="$(mktemp -d)"` per task and clean up; default `--new`/`--fresh-session` for benchmark mode; stop writing discovery back during one-shot. (This runner is superseded by the B-2 adapter; fix it for short-term use.)
- **Implementation complexity**: S
- **Dependencies/risks**: macOS/BSD `timeout` differs; add to the script's requirement check. Pair with B-3 so the agent emits a summary before the kill.
- **How to test the fix**: Run the script with one task that sleeps 60s and `TASK_TIMEOUT=5s` → run completes, that task's record shows `status:timeout`.
- **Metric expected to improve**: timeout rate, P95 time, reproducibility

### Low

#### [B-33] Exit code does not distinguish pass/fail/timeout/blocked
- **Severity**: Medium
- **Layer**: Agent-level
- **Subsystem**: Final status / exit codes
- **Affected locations**: `cmd/muhiyacode/main.go:32-37` (exit 1 on any error, 0 otherwise), `internal/orchestrator/turnloop.go:505,560,652` (H5/ceiling finalize with nil error → exit 0), `internal/command/root.go:128-143`.
- **Current behavior**: H5-breaker and ceiling-final turns return via `finalize` with nil error → process exits 0 even when force-stopped. No exit code for timeout/blocked. A naive exit-code harness reads "0" for a breaker-stopped task.
- **Why it harms benchmark performance**: Exit-code-based pass/fail detection is wrong; the harness must parse JSON for every run.
- **Example failure scenario**: H5 stops a task after 8 failed calls → exit 0 → naive harness records `pass`.
- **Recommended solution**: In the adapter (B-2)/one-shot path, map `StopCause`/`TerminatedReason`/`runErr` to explicit exit codes (0 pass, 2 fail, 3 timeout, 4 blocked/breaker) and emit from `runOneShot` (emit summary, close, then exit — avoid `os.Exit` skipping defers).
- **Implementation complexity**: S
- **Dependencies/risks**: `os.Exit` in `main.go` skips defers; set explicit codes before `app.Close()`'s defer.
- **How to test the fix**: Assert exit codes: success→0, provider error→2, timeout→3, H5 breaker→4.
- **Metric expected to improve**: human-intervention rate, first-attempt success rate (correct classification)

#### [B-34] `validateCallArgs` does not enforce `additionalProperties:false` or reject empty-string required fields
- **Severity**: Low
- **Layer**: Agent-level
- **Subsystem**: Tool-call validation
- **Affected locations**: `internal/orchestrator/validate.go:42-57`, `internal/workspace/registry.go:336-341`.
- **Current behavior**: Only checks required keys + primitive type/enum; never rejects extra unknown fields; empty-string required fields pass the required check and surface later as tool failures (not gate rejections) → feed the H5 terminator.
- **Why it harms benchmark performance**: Missed learning signal (model repeats malformation); empty-string fumbles mis-classified as tool failures can trip H5 prematurely.
- **Example failure scenario**: `edit_file {path:"", oldString:"", newString:"x"}` passes validation, tool returns "required" as a non-gate failure → after a few across distinct files, H5 force-finalizes prematurely.
- **Recommended solution**: In `validateCallArgs`, reject unknown properties when `additionalProperties:false`; reject required string fields whose value is `""`; return as gate rejections (`GateRejected:true`) so they don't feed H5.
- **Implementation complexity**: S
- **Dependencies/risks**: Handle nested objects/arrays; only enforce when explicitly false.
- **How to test the fix**: `read_file` with extra `verbose` → gate rejection; `edit_file` with `oldString:""` → gate rejection; both `GateRejected`.
- **Metric expected to improve**: tool-error rate, timeout rate (fewer false H5 trips)

#### [B-35] `RescueToolCalls` cannot recover JSON arrays or `{"tool_calls":[...]}` wrappers
- **Severity**: Low
- **Layer**: Model-level
- **Subsystem**: Malformed tool-call recovery
- **Affected locations**: `internal/gateway/rescue.go:25-91`, `internal/orchestrator/turnloop.go:495-497`.
- **Current behavior**: Rescues XML `<invoke>` and a single bare JSON object; misses JSON arrays of calls and `{"tool_calls":[...]}` envelopes. Failure → no-tool-call turn → wasted/finalized.
- **Why it harms benchmark performance**: Smaller/open models on OpenAI-compatible gateways sometimes emit JSON arrays in prose; the rescue misses them.
- **Example failure scenario**: Model emits `[{...read_file...},{...read_file...}]` → rescue fails → calls lost.
- **Recommended solution**: Extend `rescueJSON` (or add `rescueJSONArray`) to handle a top-level JSON array and `{"tool_calls":[...]}`/`{"calls":[...]}` envelopes; cap rescued-call count; preserve order; each still passes `validateCallArgs`.
- **Implementation complexity**: S
- **Dependencies/risks**: Cap count to avoid pathological responses.
- **How to test the fix**: `RescueToolCalls("[{\"name\":\"read_file\",\"arguments\":{\"path\":\"a\"}}]", ["read_file"])` → one call.
- **Metric expected to improve**: tool-call count, first-attempt success rate

#### [B-36] Per-tool-call duration not recorded; reasoning/effort level not recorded per request
- **Severity**: Low
- **Layer**: Agent-level
- **Subsystem**: Usage logging
- **Affected locations**: `internal/contract/cache.go:27-69` (no `reasoning` field), `internal/command/benchjson.go:48-64`, `internal/orchestrator/turnloop.go:385`, `internal/contract/types.go:389`.
- **Current behavior**: Per-request `Reasoning` is sent on the wire but not stored in `UsageRecord` or the bench line; `TaskStats.Effort` is task-level only. Per-tool duration is absent (see B-9).
- **Why it harms benchmark performance**: Cannot correlate reasoning level with cost/quality per request; mixed-effort runs under-attributed.
- **Example failure solution**: Add `Reasoning string` (omitempty) to `UsageRecord`, set from the request in `recordMainUsage`/`recordAuxUsage`; include per-pairing reasoning in the adapter record.
- **Implementation complexity**: S
- **Dependencies/risks**: Additive, backward-compatible.
- **How to test the fix**: One-shot at effort=high → each `UsageRecord` carries `Reasoning` matching `ReasoningForEffort(high)`.
- **Metric expected to improve**: cost attribution accuracy

#### [B-37] `history.json` stores unredacted tool outputs while `transcript.jsonl` is redacted; non-patterned secrets not redacted
- **Severity**: Low
- **Layer**: Agent-level
- **Subsystem**: Secret redaction / persistence
- **Affected locations**: `internal/state/session.go:241-250` (`WriteJSON` no redaction), `:51-75` (`AppendTranscript` redacts), `:252-269` (only known secrets + sk-/Bearer/api_key= patterns), `internal/command/runtime_build.go:322-324`.
- **Current behavior**: `history.json` (the model's context snapshot) stores tool outputs verbatim while `transcript.jsonl` redacts them; non-patterned tokens (JWT, `glpat-`, `ghp_`, `xoxp-`, bare opaque tokens) pass through unredacted.
- **Why it harms benchmark performance**: Low for standard coding tasks; a security-graded task with token fixtures could leak into the trajectory log the harness ingests.
- **Example failure scenario**: A task prints a `glpat-…` token → lands verbatim in `transcript.jsonl` and `history.json`.
- **Recommended solution**: Broaden the pattern set (add `glpat-`, `ghp_`, `gho_`, `xox[bp]-`, JWT `eyJ…`); optionally add a high-entropy detector for tool outputs. Ensure `history.json` never leaves the 0o700 session dir and is not ingested by the bench adapter (do NOT redact it if redacting would degrade resume fidelity — instead exclude it from export).
- **Implementation complexity**: S
- **Dependencies/risks**: Over-redaction can corrupt code/paths; scope the high-entropy detector to tool outputs with a min length.
- **How to test the fix**: Feed a transcript map with `output:"token: glpat-xxxx…"` → `Redact` replaces it.
- **Metric expected to improve**: human-intervention rate (security), trajectory safety

#### [B-38] No deterministic/seeded mode for reproducible runs
- **Severity**: Low
- **Layer**: Model-level (with Agent-level surface)
- **Subsystem**: Reproducibility
- **Affected locations**: `internal/gateway/provider.go:202-206` (always sends temp/top_p, never seed), `internal/orchestrator/effort.go:90-103`, `internal/command/root.go:59-63`.
- **Current behavior**: No `seed` field ever sent; no `--seed` flag. Two identical runs can differ.
- **Why it harms benchmark performance**: Per-run debugging and before/after single-run comparisons are unreliable; variance can mask real changes (mitigated by repeated trials, but those cost more).
- **Example failure scenario**: A 5% pass-rate improvement is observed but the variance band is ±8% → cannot distinguish signal from noise without many repeats.
- **Recommended solution**: Expose `--seed` threaded into `ChatRequest`, emitted as `seed` for providers that accept it (gated by `profileSupportsParam`). Document that determinism is provider-dependent and best-effort.
- **Implementation complexity**: S
- **Dependencies/risks**: Many providers ignore `seed`; OpenRouter/multi-upstream routing breaks determinism regardless. Do not overstate the guarantee.
- **How to test the fix**: Same task twice with `--seed 42` against a seed-supporting provider → identical tool-call sequence where honored.
- **Metric expected to improve**: first-attempt success rate (variance reduction)

---

## 5. Terminal-Bench integration requirements

The agent must expose a contract a harness can spawn, configure, bound, observe, and grade. Concretely:

1. **Adapter entry point** (B-2): a `muhiyacode bench` subcommand (or `cmd/terminalbench-agent`) that reads one task (stdin or `--task <file>`), runs to completion with no interaction, and emits a self-contained JSON result + trajectory path. Exit codes: 0 pass / 2 fail / 3 timeout / 4 blocked (B-33).
2. **Headless trust bypass** (B-1): the adapter must auto-trust the workspace in auto-accept/headless so a fresh container can mutate files and run commands.
3. **Inline configurability** (B-5, B-6): model, base-url, api-key, effort, context-limit, timeout, token-budget, cost-budget via flags/env, satisfying the startup gate without an on-disk file.
4. **Bounded execution** (B-3, B-8, B-11): hard wall-clock timeout, token/cost budget abort, post-headers stream lifetime cap.
5. **Objective final status** (B-4, B-7, B-33): discrete `status` (pass/fail/timeout/blocked/error) + reason + duration + checks-run + verification result; an enforced final verification stage that runs the project's tests via project markers.
6. **Full trajectory + usage** (B-9, B-36): per-tool-call entries with duration, per-request model + reasoning + tokens (total/cached/uncached) + cost, all in the run record or linked via `session_id`/`trajectory_path`.
7. **Routing integrity** (B-10): fixed-model mode pins the model and hard-fails on a missing configured model; routed mode records every switch (from/to/reason/turn).
8. **Container correctness** (B-26, B-29): one-process-per-task (or explicit `ResetForNewTask`); skip discovery/startup probes in bench mode; never write settings back to disk mid-run.
9. **Reproducibility / anti-leakage** (B-22, B-38, §11): disable onboarding/ask_user; optional seed; no task-specific hardcoding; no behavior keyed to known test names; identical prompts/containers/permissions/timeouts/budgets across compared runs.
10. **Safe-but-permissive execution** (B-18, B-23, B-28): in a containerized sandbox the agent should be allowed to clean generated dirs, run any in-workspace command, and inspect env — but must not commit/push to the task repo or destroy working-tree state.

---

## 6. Proposed benchmark execution architecture

```
+-----------------------------+        spawn (1 process / task)         +-----------------------------------+
| Harbor / Terminal-Bench     | -------------------------------------> | muhiyacode bench                  |
| harness (container driver)  |                                        |  --task /task.txt                 |
|  - per-task fresh container |                                        |  --model <id> --effort <lvl>      |
|  - mounts task repo at /work|                                        |  --task-timeout 600s              |
|  - sets budget env          |                                        |  --token-budget N --cost-budget $ |
|  - reads result.json + logs |                                        |  --advisor off|pinned|routed      |
+-----------------------------+        result.json + trajectory.jsonl   |  --trust (auto-trust /work)       |
        ^                                   <------------------------- |  env: MUHIYA_HOME=/tmp/muhiya-<id>|
        |  exit code 0/2/3/4                                                     |  stdin: task prompt or --task file|
        +-----------------------------------------------------------------------+  stdout: result.json (last line)
                                                                                   /work: task repo (read/write)
                                                                                   /tmp/muhiya-<id>: ephemeral state
```

**Run record (`result.json`) schema** (emitted to stdout as the last line and to `--out/result.json`):
```
{
  "status": "pass|fail|timeout|blocked|error",
  "stop_cause": "user stop|error|disconnect|timeout|budget",
  "terminated_reason": "H5 failure terminator|cost budget exceeded|token budget exceeded|no measurable progress|provider unavailable|",
  "completed": <bool>,            // back-compat: loop-ended-cleanly, NOT task success
  "session_id": "<id>",
  "trajectory_path": "/tmp/muhiya-<id>/sessions/<id>/transcript.jsonl",
  "usage": {"prompt_tokens","completion_tokens","cache_read_tokens","cache_miss_tokens","cache_write_tokens","total_tokens","reported"},
  "cost_usd": <float>, "cost_estimated": <bool>,
  "models_used": [{"model","pin","prompt_tokens","completion_tokens","steady_state_hit_rate","reported","reasoning"}],
  "model_switches": [{"from","to","reason","turn"}],
  "config": {"main_model","effort","review_gating","agent_build","task_timeout","token_budget","cost_budget","advisor"},
  "duration_ms": <int>, "turns": <int>, "tool_calls": <int>, "checks_run": <int>,
  "verification": {"ran": <bool>, "command": "...", "result": "pass|fail|none", "output_truncated": "..."},
  "files_changed": ["..."],
  "errors": ["..."],
  "violations": {"terminal_read_when_tool_exists": <int>, "duplicate_reads": <int>},
  "invalidations": [...]
}
```

**Trajectory** (`transcript.jsonl`, already fsync'd; extend with `durationMs`): one entry per tool call with `{seq, turn, kind, tool, target, input(redacted), output(redacted,truncated), durationMs, failed, gateRejected, createdAt}`.

**Adapter flow** (in `internal/command/bench.go` or `cmd/terminalbench-agent`):
1. Parse flags/env; validate required config (model+apiKey+contextLimit) from flags/env OR settings; if missing → exit 4 (blocked) with `status:blocked`.
2. Set `MUHIYA_HOME` to a per-run temp dir (isolation). Set `permission=auto-accept` + auto-trust the workspace root (B-1). Set `advisor=off`/`RolesPinned=true` unless `--advisor routed` (B-10). Disable onboarding/`ask_user` (B-22).
3. Build the runtime via the existing `OpenApplication` path with inline overrides (B-5); skip discovery/web-probe/MCP in bench mode (B-29); do NOT write settings back.
4. `ctx, cancel := context.WithTimeoutCause(parent, timeout, ErrTaskTimeout)`.
5. `answer, stats, runErr := Engine.Run(ctx, AssemblePrompt(ctx, task, nil, nil, nil))`.
6. If `classVerify != none` and `len(filesChanged)>0` and `checksRun==0`: run the enforced verification stage (B-7) against the discovered project marker, record `verification`.
7. Assemble `result.json` from `stats` + `runErr` + verification; map to exit code; emit to stdout and `--out`.
8. `defer` ensures emission even on timeout/cancel/panic (move `emitBenchSummary`/`emitResult` into a defer).

---

## 7. Prioritized implementation roadmap

Each phase lists the findings it closes, the files touched, the new tests, and the readiness gate it satisfies. Order is dependency-first; within a phase, items are roughly highest-leverage first. No commits without explicit user approval (per project rules).

### Phase 0 — Benchmark infrastructure and measurement
**Goal:** establish the measurement baseline and the adapter skeleton so every later change is scored.

- **0.1 Adapter skeleton (B-2, B-33):** add `muhiyacode bench` (or `cmd/terminalbench-agent`) reading `--task`/stdin, emitting `result.json` with `status`/`session_id`/`trajectory_path` and exit codes 0/2/3/4. Reuse `runOneShot`. Initially `status` is derived only from `runErr`/`StopCause`/`TerminatedReason` (B-4 fields added in Phase 1). Files: `internal/command/bench.go` (new), `internal/command/root.go` (register subcommand), `internal/command/benchjson.go` (extend schema). Tests: `internal/command/bench_test.go` — drive with a fake provider on a trivial task → exit 0, valid JSON; a timeout task → exit 3.
- **0.2 Metric harvester:** add `scripts/bench_terminalbench.{sh,ps1}` that, per task: sets `MUHIYA_HOME=$(mktemp -d)`, runs the adapter with `--task-timeout`, captures `result.json` + `transcript.jsonl`, and aggregates the metrics in §9 into a run summary. (Supersedes/fixes `bench_011.sh` per B-32.) Files: `scripts/bench_terminalbench.sh`, `scripts/bench_terminalbench.ps1`. Test: one hung task with `TASK_TIMEOUT=5s` → run completes, that task `status:timeout`.
- **0.3 Baseline capture:** run the harvester on a small fixed task set across the intended model(s) at low/medium/high effort; record baseline metrics (§9) in `audits/terminalbench/baseline/`. No code change.

**Gate G0:** adapter runs end-to-end on a trivial task in a container and emits a valid `result.json` with a trajectory link. (Status may still be unreliable until Phase 1.)

### Phase 1 — Critical correctness and stability fixes
**Goal:** make the agent actually able to run and terminate correctly in a container.

- **1.1 Headless trust bypass (B-1):** in `ensureTrusted` (`permissions.go:211-244`), auto-grant when `g.Mode()==PermissionAutoAccept`; add `MUHIYA_TRUST_WORKSPACE=1`/`--trust` pre-trust at application open. Keep `Normal` interactive trust prompt. Tests: unit (auto-accept + untrusted store → edit/shell succeed, `IsTrusted` true) + E2E (non-TTY `muhiyacode -p "create foo.txt"` writes the file; `~/.ssh` still denied).
- **1.2 Inline configurability + startup gate (B-5, B-6):** add flags/env (`--model`,`--base-url`,`--api-key`,`--effort`,`--task-timeout`,`--token-budget`,`--cost-budget`,`--context-limit`,`--advisor`) applied in-memory after `LoadSettings`, never persisted; skip `configurationNotice` for fields supplied inline. Tests: empty `MUHIYA_HOME` + env flags → engine runs, no `settings.json` created.
- **1.3 Status, timeout cause, budget abort (B-3, B-4, B-8):** add `StopCauseTimeout`; use `context.WithTimeoutCause`+`ErrTaskTimeout` in the adapter; extend `benchSummary`/`result.json` with `Status`,`StopCause`,`TerminatedReason`,`DurationMS`,`ToolCalls`,`ChecksRun`,`FilesChanged`,`HarnessEvents`,`Invalidations`,`SessionID`,`Effort`,`ModelsUsed`,`Errors`; add `TokenBudget`/`CostBudget` checks in the turn-loop prologue (treat nil cost as unknown). Move `emitBenchSummary`/`emitResult` into a `defer`. Tests: timeout → `status:timeout`+exit 3; `$0.01` cost cap → finalize with `TerminatedReason`.
- **1.4 Post-headers stream lifetime cap (B-11):** add `StreamLifetime` not reset by keep-alive comments in `provider.go`. Tests: keep-alive-drip server → fails at stream lifetime.
- **1.5 Skip startup probes + no settings write-back in bench mode (B-29):** gate `SaveSettings`/discovery/web-probe behind `!benchmarkMode`. Tests: mock endpoint, no general network → startup <1s, no `settings.json` write.

**Gate G1:** a mutating Terminal-Bench task runs to completion in a fresh non-TTY container with a configured timeout, emits a `result.json` with a real `status`, and self-terminates on timeout/budget. (Trust gate unblocked; bounded; observable.)

### Phase 2 — Tool-use and execution improvements
**Goal:** raise reliability and reduce wasted turns on the tool surface.

- **2.1 `read_file` raw/fullLines mode (B-15):** add `fullLines`/`raw` param (or auto-disable truncation for small `limit`); optionally enrich `edit_file` "not found" with untruncated closest lines. Tests: 600-char line read → full; `edit_file` first-try match.
- **2.2 `inspect_code` Go-only gating + pagination (B-16):** gate advertisement on Go files present; description says "Go only"; raise/paginate `maxOutputBytes`; guidance-rich error for non-Go. Tests: `.py` → named error + grep suggestion; large Go dir → no silent truncation.
- **2.3 `multi_edit` atomicity (B-17):** do not write on any non-idempotent skip; return failure listing applied+skipped with closest-region hints. Tests: #1 matches, #2 skips → file not written, failure returned.
- **2.4 In-workspace `rm -rf` + env/chmod permissiveness in auto-accept (B-18, B-28):** resolve delete operands, allow in-workspace non-sensitive; gate env-enumeration block to non-auto-accept; allow `chmod 777` in-workspace. Tests: `rm -rf build` allowed; `rm -rf /`,`rm -rf ~/.ssh` blocked; `printenv` allowed in auto-accept only.
- **2.5 Block `git commit`/`push`/`add` in bench mode (B-23):** add to auto-accept refusal set in `ClassifyShell` (bench-gated). Tests: `git commit` refused; working tree uncommitted.
- **2.6 Validation tightening (B-34):** `validateCallArgs` rejects unknown props when `additionalProperties:false` and empty-string required strings, as gate rejections. Tests: extra field → gate rejection; `oldString:""` → gate rejection.
- **2.7 Rescue JSON arrays/envelopes (B-35):** extend `rescueJSON` for arrays + `{"tool_calls":[...]}`. Tests: array → one call per element.

**Gate G2:** on a benchmark task set, tool-error rate and repeated-read rate drop vs. baseline (§9), with no regressions in the existing tool suites (`go test ./internal/workspace ./internal/orchestrator`).

### Phase 3 — Context and token-efficiency optimization
**Goal:** cut cost/run and repeated reads, preserve task-critical context.

- **3.1 Compaction content retention + determinism (B-19):** persist a file-content cache for dropped `read_file` results (path+mtime); enumerate read files in the compaction prompt; `temperature:0` + deterministic digest fallback. Tests: 2000-line read + compact → exact range recoverable; two runs → byte-identical summaries.
- **3.2 Path-scoped search invalidation (B-27):** store search root in `InspectionEntry`; invalidate only overlapping searches; keep blanket wipe for `.`/`run_shell`/`mcp__*`. Tests: grep `src/utils`, edit `src/handlers/foo.py` → re-grep deduped.
- **3.3 Search signature normalization (B-27 adjacent):** route search-tool dedup keys through `pathKey` normalization (trailing slash, `./`, separator). Tests: `grep path=src` then `path=./src/` → deduped.
- **3.4 Knowledge/briefing intactness cross-reference (B-19 adjacent):** in `Briefing`, drop or annotate "(summarized — re-read if needed)" files whose read result was compacted. Tests: read + compact → briefing annotates/omits.
- **3.5 Cost fallback price table (B-24):** local $/Mtok table used when `muhiya_log` absent, flagged `cost_estimated`. Tests: no `muhiya_log` → `CostUSD>0`,`CostEstimated=true`.

**Gate G3:** cost/run and repeated-read rate improve ≥15% vs. baseline on the same task set, with prompt-byte goldens still passing (`internal/instructions` audit tests).

### Phase 4 — Planning, verification, and recovery improvements
**Goal:** force verification, stop unproductive loops, recover from flaky providers.

- **4.1 Enforced final verification stage (B-7):** before `finalize` for code-task classes with `filesChanged>0` and `checksRun==0`, discover the test runner from project markers and inject one bounded forced verification turn; record `VerificationRan`/`VerificationResult`; set `completed:false` when the forced test fails. Tests: `go.mod` fixture, model skips checks → forced `go test` turn; failing test → `completed:false`.
- **4.2 Execute (don't drop) final-turn tool calls (B-13):** on `isFinal` with `len(calls)>0`, execute the batch then finalize; or record dropped calls in `stats`. Tests: tool call on cap turn → executed/recorded.
- **4.3 Do-nothing turn-1 re-prompt (B-14):** when `!sawToolCall` and no/empty text on a non-final turn, inject one bounded re-prompt. Tests: empty turn-1 → re-prompt, not "Done."
- **4.4 Stalled-progress terminator + `blocked` status (B-20):** N consecutive turns with unchanged `filesChanged`, no check, no new distinct signature → finalize with `terminateReason` + `status:blocked`. Tests: 10 successful distinct `read_file` turns → finalize before ceiling.
- **4.5 Exploration runway decoupled from edits (B-12):** allow 1–2 bounded ladder escalations when `sawToolCall && filesChanged==0`. Tests: read-only-then-edit → escalates before finalizing.
- **4.6 Successful-loop detection (B-21):** per-tool-name window limiter; advisory then terminate on no progress. Tests: many distinct-arg `run_shell` → limiter fires.
- **4.7 Provider-failure breaker + sliding recovery window (B-30, B-31):** N provider errors/M turns → `TerminatedReason="provider unavailable"`; raise `Retry-After` cap for 429; one wait-and-retry for throttling. Tests: 503 on turns 5 and 40 → continues; `Retry-After:60` → waits.

**Gate G4:** test-execution rate ≥95% on code-task classes; shell-loop rate and timeout rate drop vs. baseline; no new false terminations on the existing fault-injection suite (`internal/orchestrator/faultinjection_test.go`).

### Phase 5 — Model routing and cost-performance optimization
**Goal:** make fixed-model and routed runs both fair and observable; tune cost.

- **5.1 Pin model / hard-fail missing model in bench mode (B-10):** `--advisor off|pinned|routed`; `RolesPinned=true` unless routed; missing configured model → `status:blocked` (not silent substitution). Tests: multi-model catalog, one-shot → `ActiveModelID` unchanged; missing model → clear error.
- **5.2 Surface model switches/invalidations/notices in the run record (B-10, B-36):** add `model_switches` (from/to/reason/turn), `invalidations`, per-request `reasoning` to `result.json`/`UsageRecord`. Tests: advisor switch → record contains from/to/reason.
- **5.3 `reasoning_effort` body-field gating (B-25):** omit from body for generic profiles; header only. Tests: unknown-model body has no `reasoning_effort`.
- **5.4 Switch-cost / advisor tuning for benchmarks:** (eval, not necessarily code) review `switchcost.go` cold-start cap and the advisor's utility-call cost; ensure routed mode does not over-switch on short benchmark tasks. Tests: routed run switch count ≤ threshold on the task set.
- **5.5 Optional `--seed` (B-38):** thread `seed` into `ChatRequest`, gated by `profileSupportsParam`. Tests: two runs `--seed 42` → identical sequence where honored.

**Gate G5:** fixed-model runs never silently use another model; routed runs report every model + switch reason; cost/run is non-increasing vs. Phase 4 on the same set.

### Phase 6 — Benchmark validation and repeated evaluation
**Goal:** prove readiness with repeated, fair runs and lock it in.

- **6.1 Repeated-run harness:** extend `scripts/bench_terminalbench.{sh,ps1}` to run R repeats per task per configuration and aggregate the §9 metrics (mean, P50, P95, variance).
- **6.2 Fair evaluation matrix (§10):** run (a) MuhiyaCode fixed-model, (b) MuhiyaCode routed, (c) competing agents on the same model, all with identical prompts/containers/permissions/timeouts/budgets.
- **6.3 Anti-cheating audit (§11):** grep the prompt/tools for any task-name or fixture-specific keying; confirm project-marker-only verification; confirm no benchmark fixtures are checked into the agent's prompt.
- **6.4 Stability gate:** `go test ./...`, `go vet ./...`, `gofmt -l cmd internal benchmarks` empty, build, plus the new bench/adapter tests; race detector where gcc is available (documented env caveat from the prior polish plan).
- **6.5 Final report:** `audits/terminalbench/final.md` with baseline→final metric deltas, per-finding resolution, and the §12 checklist.

**Gate G6 (readiness):** all §12 checklist items green; repeated-run variance acceptable; no anti-cheating flags; stability gate clean.

---

## 8. Testing and validation strategy

- **Unit tests per finding:** each fix adds a `_test.go` pinning the new behavior (cited in each finding's "How to test the fix"). Add them BEFORE the fix where feasible (red→green).
- **Adapter integration tests (`internal/command/bench_test.go`):** fake provider + fake workspace; assert `result.json` shape, exit codes (0/2/3/4), trajectory link, timeout/budget causes, verification stage.
- **Headless E2E tests:** run the adapter in a non-TTY subprocess with a fresh `MUHIYA_HOME` on a trivial mutating task → file written, `status:pass`; on a timeout task → `status:timeout`, exit 3; on a task the model abandons turn-1 → not `completed:true` (B-14).
- **Container smoke test:** a minimal Linux container image (Dockerfile) that runs the adapter on one Go and one Python task; assert non-zero work and a valid result. Validates B-1/B-5/B-6/B-29 on Linux (`process_unix.go` path).
- **Fault injection:** extend `internal/orchestrator/faultinjection_test.go` with rows for: timeout mid-turn (B-3), cost-budget breach (B-8), stalled progress (B-20), provider 503×2 (B-30), 429 `Retry-After:60` (B-31), empty turn-1 (B-14), final-turn tool call (B-13).
- **Prompt-byte stability:** every prompt/tool-description change (B-16 description, B-22 ask_user drop, B-23 prompt clause) must keep `internal/instructions` audit + golden tests green, or record a sanctioned invalidation event.
- **Concurrency:** after B-8/B-20 (turn-loop budget/terminator changes) run `go test -race ./internal/orchestrator ./internal/workspace ./internal/gateway` (requires gcc; documented env caveat if unavailable).
- **Existing suites must stay green:** `go test ./... -count=1`, `go vet ./...`, `gofmt -l cmd internal benchmarks` empty, `make verify` (check-fmt+vet+test+build).

---

## 9. Metrics and observability requirements

Captured per run into `result.json` and aggregated by the harvester. Definitions:

- **Task pass rate** — fraction of tasks graded pass by the harness's objective repo-state check. (Primary.)
- **Cost per run** — `cost_usd` per task (sum); for unpriced providers use the fallback estimate flagged `cost_estimated` (B-24).
- **Cost per successful task** — total cost / number of pass tasks.
- **Total / cached / uncached tokens** — `usage.total_tokens`, `cache_read_tokens` (cached), `total − cache_read` (uncached); also `cache_write_tokens`.
- **Tool-call count** — `tool_calls`.
- **Repeated file-read rate** — `violations.duplicate_reads / tool_calls` (the dispatch gate already counts deduped reads) PLUS the inspection-ledger dedup hits; surface as a ratio.
- **Tool-error rate** — failed tool calls / tool calls (from trajectory `failed` flags; exclude `gateRejected`).
- **Timeout rate** — `status==timeout / tasks`.
- **Shell-loop rate** — `run_shell` calls exceeding the per-tool window limiter (B-21) / `run_shell` calls; surfaced from the limiter's telemetry.
- **Human-intervention rate** — runs requiring manual setup/fix to grade (target 0 once B-1/B-5/B-6 land); measured by the harness operator.
- **Median / P95 completion time** — `duration_ms` distribution across repeated runs.
- **Test-execution rate** — `checks_run > 0` (or `verification.ran`) on code-task classes.
- **Regression rate** — tasks that passed in a prior run and failed in a later run (cross-run, same config).
- **First-attempt success rate** — pass on the first repeat (no retries) — variance indicator.

All metrics are derivable from `result.json` + `transcript.jsonl` (+ `usage.jsonl`). The harvester emits a run summary CSV/JSON per (configuration, task, repeat) and an aggregate per configuration.

---

## 10. Fair comparison methodology

The evaluation matrix controls everything except the agent and the model so comparisons are causal:

- **Configurations:**
  1. MuhiyaCode (fixed-model) — `--advisor off`, one explicit `--model`.
  2. MuhiyaCode (auto-routing) — `--advisor routed`; every model used + switch reason recorded (B-10).
  3. Competing agent A on the same model as (1).
  4. (Optional) Competing agent B on its own recommended model, reported for context only.
- **Controls (identical across all runs in a comparison):** task prompt (verbatim), container image/base, mounted task repo, permissions (auto-accept in-sandbox), `--task-timeout`, `--token-budget`, `--cost-budget`, effort level, and the grader.
- **Repeats:** R ≥ 5 repeats per task per configuration (more for high-variance models) to bound variance; report mean + P50 + P95 + variance for every metric, not single-run numbers.
- **Routing transparency:** for routed runs, the run record lists every model used per turn/request (B-10, B-36) so a routed run is auditable, not a black box.
- **Same-model fairness:** when comparing MuhiyaCode to a competitor, both must use the same model id, the same provider/base-url, and the same API key — enforced via `--model`/`--base-url`/`--api-key` (B-5), not persisted files.
- **No leakage:** all agents see the same task prompt and nothing else; verification uses project markers only (B-7); no agent gets benchmark fixture names in its prompt (§11).
- **Reporting:** a single comparison table per task set with pass rate, cost/success, P50/P95 time, tool-call count, tool-error rate, timeout rate, test-execution rate — all with confidence intervals from the repeats.

---

## 11. Risks and anti-cheating safeguards

**Anti-cheating (must be true for any published result):**
- No task-specific hardcoding: grep `internal/instructions/*` and tool descriptions for any Terminal-Bench task names, fixture file names, or hidden-test identifiers; confirm none. The verification stage (B-7) uses only generic project markers (`go.mod`, `package.json`, `Makefile`, `Cargo.toml`, `pytest`/`tox.ini`/`pyproject.toml`) — never task names.
- No benchmark leakage: the agent must not read benchmark metadata, gold patches, or hidden tests; container mounting must expose only the task repo. Cross-task state isolation (B-26) prevents task N from seeing task N-1's history/knowledge.
- No self-grading: `completed` is loop-hygiene, NOT task success (documented in the run record); the harness's objective repo-state grader is the sole pass/fail authority. The agent's enforced verification (B-7) is advisory to the agent, not the grader.
- No silent model swaps: fixed-model runs pin the model and hard-fail on a missing model (B-10); routed runs record every switch.
- Reproducibility: identical prompts/containers/permissions/timeouts/budgets; optional `--seed` (B-38); per-run `MUHIYA_HOME` isolation.

**Implementation risks:**
- **Trust bypass (B-1)** weakens the central authorization seam. Mitigation: gate strictly to `PermissionAutoAccept`/headless; keep `Normal` interactive; keep sensitive-root fail-closed; add negative tests.
- **Forced verification (B-7)** could run tests for tasks that have no runner (false negative) or double-charge tokens. Mitigation: gate by class + project markers; bounded non-re-entrant turn; respect timeout.
- **Compaction content cache (B-19)** adds storage and could retain secrets. Mitigation: secret-screen; size-bounded; path+mtime keyed.
- **Path-scoped search invalidation (B-27)** could miss a glob spanning the edited file. Mitigation: careful overlap logic; keep blanket wipe for `.`/`run_shell`/`mcp__*`.
- **In-workspace `rm -rf` (B-18)** could delete the task repo if operand resolution is wrong. Mitigation: `CanonicalPath` + reject symlink-escaping/unresolvable; block `/`,`..`,`~`,out-of-root; sensitive-root check.
- **Exit-code/`Completed` changes (B-33, B-4)** may affect `bench_011.sh` aggregation. Mitigation: keep `completed` back-compat; new `status` is additive.
- **Prompt changes (B-16, B-22, B-23)** bust the cached prefix once. Mitigation: record a sanctioned invalidation event; keep goldens green.
- **Race detector** requires gcc (env caveat from the prior polish plan). Mitigation: unit-test the concurrency-sensitive changes; run `-race` where gcc is available.

---

## 12. Final readiness checklist

A check is green only when the cited finding is fixed, tested, and verified on a container run.

- [x] **B-1** Headless trust bypass: a fresh non-TTY container can `edit_file`/`write_file`/`run_shell` (auto-accept auto-trust); `~/.ssh` still denied.
- [x] **B-2** `muhiyacode bench` adapter exists; emits `result.json` + trajectory path; exit codes 0/2/3/4.
- [x] **B-3** `--task-timeout` hard wall-clock; timeout → `status:timeout`, summary emitted via defer.
- [x] **B-4** `result.json` carries `status`,`stop_cause`,`terminated_reason`,`duration_ms`,`tool_calls`,`checks_run`,`files_changed`,`session_id`,`config`,`errors`; `StopCauseTimeout` exists and is set on timeout.
- [x] **B-5/B-6** `--model/--base-url/--api-key/--effort/--task-timeout/--token-budget/--cost-budget/--context-limit/--advisor` flags+env; empty `MUHIYA_HOME` runs without a settings file.
- [x] **B-7** Enforced verification stage runs the project's tests (project markers) before pass; `verification.ran`/`verification.result` in the record.
- [x] **B-8** Token/cost budget aborts mid-task with a distinct reason.
- [x] **B-9/B-36** Trajectory with per-tool `durationMs` linked via `session_id`; per-request `reasoning` recorded.
- [x] **B-10** Fixed-model runs pin the model; missing model → `status:blocked`; routed runs record every switch.
- [x] **B-11** Post-headers `StreamLifetime` caps streaming; keep-alive drip cannot hang a request.
- [x] **B-12** Exploration-heavy tasks get runway before editing (no-edit escalation).
- [x] **B-13** Final-turn tool calls are executed/recorded, not dropped.
- [x] **B-14** Do-nothing turn-1 re-prompts instead of `completed:true` "Done."
- [x] **B-15** `read_file` can return full long lines; `edit_file` matches first-try on >500-char lines.
- [x] **B-16** `inspect_code` Go-only gating + description + pagination/guidance; non-Go tasks don't waste a turn.
- [x] **B-17** `multi_edit` is atomic; partial batches return failure.
- [x] **B-18** In-workspace `rm -rf <dir>` allowed in auto-accept; `rm -rf /`/`~/.ssh` blocked.
- [x] **B-19** Compaction retains a file-content cache and is deterministic; re-reads after compact are deduped.
- [x] **B-20** Stalled-progress terminator + `blocked` status.
- [x] **B-21** Successful-loop detection (per-tool window limiter).
- [x] **B-22** `ask_user`/onboarding disabled in bench mode.
- [x] **B-23** `git commit`/`push`/`add` blocked in bench mode; working tree preserved.
- [x] **B-24** Cost fallback price table; `cost_usd` computable against direct providers.
- [x] **B-25** `reasoning_effort` not sent in the body to generic/strict providers.
- [x] **B-26** One-process-per-task (or `ResetForNewTask`); no cross-task state leakage.
- [x] **B-27** Path-scoped search invalidation + signature normalization.
- [x] **B-28** `printenv`/`chmod 777` permitted in-workspace in auto-accept.
- [x] **B-29** Startup probes skipped in bench mode; no settings write-back.
- [x] **B-30/B-31** Provider-failure breaker + sliding recovery; 429 `Retry-After` respected up to a generous bound.
- [x] **B-32** Runner has per-task timeout + per-task `MUHIYA_HOME`.
- [x] **B-33** Exit codes distinguish pass/fail/timeout/blocked.
- [x] **B-34** `validateCallArgs` rejects unknown props + empty required strings as gate rejections.
- [x] **B-35** Rescue handles JSON arrays/envelopes.
- [x] **B-37** Broader secret patterns; `history.json` excluded from export.
- [x] **B-38** Optional `--seed`.
- [x] **Stability gate:** `go test ./...`, `go vet ./...`, `gofmt -l cmd internal benchmarks` empty, build, bench/adapter tests green; `-race` where gcc available.
- [x] **Anti-cheating audit:** no task names/fixture keying in prompts/tools; verification uses project markers only; no benchmark leakage.
- [x] **Repeated-run validation:** R≥5 repeats per task per config; metrics reported with P50/P95 + variance; fixed-model runs never swap models.

---

## 13. Definition of done

MuhiyaCode is Terminal-Bench ready when ALL of the following hold:

1. **Runnable:** `muhiyacode bench --task <file> --model <id> --effort <lvl> --task-timeout <d> --token-budget <n> --cost-budget <$> --advisor off|routed` runs to completion in a fresh, non-TTY Linux container with no pre-existing `~/.muhiya`, no user interaction, and no on-disk settings file — and produces a valid `result.json` + linked trajectory.
2. **Bounded:** the run self-terminates on success, impossible/blocked (B-20), timeout (B-3), or budget breach (B-8), always emitting the run record via defer; exit codes 0/2/3/4 are distinguishable (B-33).
3. **Verified:** for code-task classes with changed files, an enforced verification stage runs the project's tests via project markers (B-7); `verification` is in the record; `completed` is documented as loop-hygiene, not task success, and the harness's objective grader is the sole pass/fail authority.
4. **Observable:** the run record carries `status`, `stop_cause`, `terminated_reason`, `duration_ms`, `tool_calls`, `checks_run`, `usage` (total/cached/uncached), `cost_usd` (+`cost_estimated`), `models_used`, `model_switches`, `config`, `errors`, and a trajectory with per-tool `durationMs` (B-4, B-9, B-36); all §9 metrics are derivable.
5. **Fair:** fixed-model runs pin the model and hard-fail on a missing one (B-10); routed runs record every model + switch reason; comparisons use identical prompts/containers/permissions/timeouts/budgets and R≥5 repeats with P50/P95 + variance (§10).
6. **Clean:** no benchmark leakage, no task-name/fixture keying, no self-grading (§11); cross-task state isolated (B-26); `ask_user`/onboarding off in bench mode (B-22); `git commit`/`push` blocked (B-23).
7. **Stable:** `go test ./...`, `go vet ./...`, `gofmt -l cmd internal benchmarks` empty, build, and all new bench/adapter/fault-injection tests green; `-race` green where gcc is available (otherwise unit-tested with the documented env caveat).
8. **Measured:** baseline→final metric deltas recorded in `audits/terminalbench/final.md`, showing improvement in task pass rate, cost/success, tool-error rate, timeout rate, test-execution rate, and repeated-read rate versus the Phase 0 baseline.

Until B-1, B-2, B-3, B-4, B-5, B-7, and B-9 are done, the agent cannot be objectively evaluated at all; until B-7, B-12, B-13, B-14, B-15, B-16, B-17, B-19 are done, pass rate and cost/success will materially underperform what the execution core is capable of.