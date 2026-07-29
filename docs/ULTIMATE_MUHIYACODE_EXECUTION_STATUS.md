# Ultimate MuhiyaCode Execution Status

- Executor: GLM 5.2 in OpenCode
- Active session model: GLM 5.2
- Delegation/alternate models: prohibited
- Active phase: Phase 7 external validation
- Active work package: staging fault drills and multi-replica evidence
- Last verified state: both repositories pass `go test ./... -count=1`, `go build ./...`, and `git diff --check`; benchmark manifest validation and generated-doc checks also pass
- Current blocker: Elest.io staging credentials and deployment access are required for live fault-injection evidence

### Phase 0 gate status

- ✅ No live automatic model chooser, `muhiya-ai-router`, cross-model fallback, `run_subagent`, subagent usage stream, or alternate-model maintenance call
- ✅ Legacy router requests fail explicitly (400 migration error)
- ✅ Auto Accept survives restart/resume of the same session; does not leak into a new session
- ✅ No logical model turn produces more than two upstream attempts (MaxRetries=1)
- ✅ Production boot fails with missing/invalid provider-key encryption
- ✅ Every request traceable client → gateway → provider → settlement by one request ID (`X-Muhiya-Request-ID`)
- ✅ Existing unit suites remain green (both repos, all packages)

### Phase 0 complete — all gate items satisfied.

| Work package | Status | Files/migrations | Focused tests | Gate evidence | Remaining risk |
|---|---|---|---|---|---|
| P0-W1 (gateway) | complete | deleted proxy/router.go, proxy/stickysession.go, proxy/router_test.go, proxy/stickysession_test.go; edited proxy/handler.go, proxy/agent.go, proxy/media.go (moved modelMatchesVision), proxy/media_test.go (removed 5 routing tests) | `go build ./...` clean; `go test ./...` green (gateway, admin, db, proxy) | no live router/fallback/sticky code (rg confirms only `routerModelDeprecated` const); legacy `muhiya-ai-router` → 400 migration error; one model/one attempt per request | DB `RoutingTier` column + admin tier validation left inert — removed in P2 with migration |
| P0-W1 (client) | complete | internal/orchestrator/maintenance.go (deleted model-based compaction call → mechanical fallback); internal/contract/types.go (removed `PinUpstream`, `RequestPurposeCompaction`/`ExplicitReview`); internal/contract/cache.go (removed `UsageStreamSubagent`, `SubagentRequests`, `:sub:*` pin docs); internal/orchestrator/usage.go (removed the unused auxiliary request writer); tests: allstream_rate_test.go, cache_test.go, upstream_test.go, maintenance_test.go, cross_surface_test.go, usage_display_test.go | `go build ./...` clean; `go test ./...` green (all packages) | no live `PinUpstream`/`UsageStreamSubagent`/`SubagentRequests`/`RequestPurposeCompaction`/`ExplicitReview`; no model call in maintenance.go; every accepted response validates the immutable requested-model receipt | historical `aux` usage values remain readable for old session accounting but no production path emits them |
| P0-W2 | complete | internal/state/session_runtime.go (v2 + PermissionMode field, v1 compat reader); internal/contract/types.go (Session.PermissionMode); internal/command/runtime_build.go (hydrateSessionRuntime defaults from global, buildRuntime reads session authority, WriteSessionModel preserves mode, defaultPermissionMode helper); internal/command/actions.go (SetPermission writes runtime record not global settings); internal/command/root.go (removed resetInteractivePermission); internal/command/callbacks_test.go (replaced reset test with session-scoped matrix) | `go build ./...` clean; `go test ./...` green (all packages) | `resetInteractivePermission` deleted; Shift+Tab writes runtime record; resume restores saved mode; new session inherits global default; WriteSessionModel preserves permission; footer displays the session mode | one-shot `--unsafe-full-access` path remains intentionally separate |
| P0-W3 | complete | internal/gateway/provider.go (MaxRetries default 3→1) | `go build ./...` clean; `go test ./...` green (all 16 packages) | default 1 retry = max 2 upstream calls per logical turn (gate met); tests with explicit MaxRetries values unaffected | formal `AttemptPolicy` type + retry-cause/bytes/deadline recording not yet added (deeper P0-W3 item); outer turn-loop retry (bounded 1/task + 3-in-15 breaker) kept as turn-level recovery |
| P0-W4 | complete | db/db.go (Open fails closed on missing/invalid PROVIDER_KEY_ENCRYPTION_KEY unless DEV_MODE=1; added devModeEnabled helper) | `go build ./...` clean; `go test ./...` green (gateway, admin, db, proxy) | production boot fails with clear error; DEV_MODE is the explicit development-only escape | tests use a non-Open constructor or set DEV_MODE (suite green) |
| P0-W5 | complete | internal/contract/types.go (`RequestID` field); internal/gateway/provider.go (sends `X-Muhiya-Request-ID`); internal/orchestrator/turnloop.go (generates UUID per turn attempt); gateway proxy/handler.go (`correlationID(r)` helper reading header; request-log writes use it) | `go build ./...` clean (both repos); `go test ./...` green (both repos) | one ID ties client turn → gateway attempt → settlement; `/requests` exposes request, prefix, model, upstream, cache, token, and latency diagnostics | first-byte latency is still provider-dependent when an upstream does not report timing detail |
| P0-W6 | complete | proxy/wire_goldens_test.go + proxy/testdata/minimax_wire_request.golden.json + proxy/testdata/deepseek_wire_request.golden.json | `go test ./proxy/ -run "WireRequestIsFrozen"` green; full gateway suite green | exact wire bytes frozen for MiniMax M3 and DeepSeek (always-on thinking + transform pipeline); `-update-goldens` flag for intentional drift | client-side `prefix_bytes_wire.golden` preserved (P0-W6 directive) |

### Phase 1 gate status

- Complete: checked `NanoUSD` is authoritative for admission, budget reads, top-up consumption, ledger entries, request cost, and settlement.
- Complete: MiniMax long-context pricing is evaluated by the pure `pricing` package from database-backed exact tiers.
- Complete: OpenAI, Anthropic, and transcription requests reserve a conservative maximum before contacting an upstream.
- Complete: concurrent requests serialize per user and active reservations count against every enabled budget window.
- Complete: request log, top-up consumption, account ledger debit, and reservation settlement commit in one PostgreSQL transaction.
- Complete: a customer charge cannot exceed the admitted reservation, including excess provider usage.
- Complete: Redis fails closed at runtime when `REQUIRE_REDIS=1`; fallback is explicitly single-instance only.
- Complete: TPM admission reserves maximum output instead of adding output after completion.
- Complete: expired/indeterminate reservations are conservatively settled by a supervised reconciler.
- Verified: gateway `go test ./...` and `go build ./...` pass.

| Work package | Status | Principal implementation | Remaining deployment evidence |
|---|---|---|---|
| P1-W1 | complete | `money/money.go`; migrations 023–024; exact compatibility boundary in `db/db.go` | run migration/parity queries against Elest.io staging |
| P1-W2 | complete | `pricing/pricing.go`; migration 025; catalog-loaded tiers | validate production MiniMax tier rows |
| P1-W3 | complete | conservative whole-request input bound and normalized output cap | compare actual usage with bounds under live traffic |
| P1-W4 | complete | `db.ReserveBudget`, affordable-output search, advisory lock | 100-way staging concurrency test |
| P1-W5 | complete | `db.SettleReservationAndLog`, exact top-up order, ledger | staged crash/fault injection |
| P1-W6 | complete | fail-closed Redis and maximum-output TPM admission | Elest.io Redis disconnect drill |
| P1-W7 | complete | `ReconcileExpiredReservations` and `budget-reconciler` | forced-process-kill drill |

### Phase 2 gate status

- Complete: catalog v2 publishes immutable model records, capabilities, context limits, pricing metadata, hashes, and compatibility epochs.
- Complete: the client stores a separate last-known-good catalog, refreshes at safe task boundaries, and never changes the active model implicitly.
- Complete: provider adapters translate wire dialects but have no model-selection authority.
- Complete: every accepted response is checked against the requested immutable model receipt.

### Phase 3 gate status

- Complete: model A → B → A restores A's prior lineage instead of silently creating a new cache epoch.
- Complete: OpenRouter provider affinity is gateway-owned, scoped by virtual key/session/model record, and permits one unpinned retry only before response bytes.
- Complete: request diagnostics expose prefix identity, cache attribution, resolved record, upstream, request ID, tokens, and latency.

### Phase 4 gate status

- Complete: versioned, checksummed execution events record task, tool, approval, mutation intent/outcome, and terminal transitions.
- Complete: replay rebuilds task and tool projections and retains compatibility with legacy session history.
- Complete: unresolved prior mutations block new mutations after restart while preserving read-only diagnosis.
- Complete: checkpoints support list, restore, and fork flows.

### Phase 5 gate status

- Complete: the task loop uses typed explore/execute/verify/finalize phases.
- Complete: progress is based on unique successful tool evidence rather than line-count churn.
- Complete: repeated identical evidence does not extend the loop.
- Complete: the fixed 96-turn emergency cap cannot be multiplied by effort.
- Complete: task briefs no longer inject the wall-clock date, mandatory `DONE` text, or unconditional `tasks.md` ceremony.
- Complete: the stable system prompt is guarded by a 1,500-character budget.

### Phase 6 gate status

- Complete: Auto Accept remains session-scoped and visibly represented in the TUI.
- Complete: `/requests` exposes per-request model, purpose, cache, token, latency, upstream, prefix, and request-ID diagnostics.
- Complete: successful verification output can be reused only when the exact command and workspace fingerprint are unchanged.
- Complete: verification fingerprints include Git HEAD, staged/unstaged changes, and untracked file contents, with a bounded non-Git source fallback.

### Phase 7 gate status

- Complete in code: gateway CI provisions PostgreSQL and Redis and runs format, vet, unit, live-database, race, and build gates.
- Complete in code: the live database test proves two concurrent $0.04 reservations cannot both pass a $0.05 window.
- Complete in code: `REQUIRE_REDIS=1` fails closed without terminating library callers.
- Complete in code: benchmark reports include typed terminal status, turns, tools, checks, token/cache/cost metrics, and workspace digest.
- Complete in code: production runbook and architecture-decision record cover deployment order, stop conditions, incident recovery, and release evidence.
- Pending external evidence: Elest.io migrations, Redis disconnect, process-kill reconciliation, two-replica concurrency, provider affinity, catalog ETag, and canary drills.

### Simple-task incident remediation

- Complete: the exact one-file Snake request is classified `tiny`, uses Low reasoning, and carries an explicit `.html` single-artifact scope.
- Complete: task-class limits are enforced before dispatch instead of producing advisory warnings after overspend.
- Complete: tiny work is capped at 5 turns, 5 admitted tool calls, 1 planned verification plus at most one failed-check retry, 8,000 output tokens per request, and 45,000 total task tokens.
- Complete: single-artifact tasks reject mutating shell setup, scratch harnesses, secondary files, and mutations outside the requested extension.
- Complete: a changed artifact plus sufficient successful verification becomes a completion transition; file churn cannot expand the task class or runway.
- Complete: missing edit targets and atomic `oldString` mismatches are classified as not-started mutations, preventing unnecessary indeterminate-mutation recovery.
- Complete: the live prompt explicitly forbids scratch test artifacts for small work, and the obsolete automatic-model-selection prompt block was removed.
- Verified: a deterministic regression reproducing the original request sequence stops the harness attempt, pins Low reasoning, limits output, and completes in five provider turns.
- Evidence and architecture details: `docs/SNAKE_SESSION_AGENT_OVERHAUL.md`.

## Decisions

- Preserve all unrelated dirty worktree changes in both repos. Do not reset/overwrite.
- Honor plan Section 0.3 rule 5: no subagents, no delegated LLM tasks. Use direct read/search/patch/shell tools only.
- Local implementation is complete through Phase 7. Deployment-only gates remain open until an authorized Elest.io staging environment is available.
- Stray untracked `%SystemDrive%/` entry in client repo is unrelated; left untouched.

## Compatibility migrations

- (none yet)

## Failed/indeterminate actions

- (none yet)

## Next exact action

Run the Phase 7 Elest.io staging sequence in `docs/MUHIYACODE_PRODUCTION_RUNBOOK.md`, attach the resulting SQL, request-trace, cache, concurrency, and recovery evidence, then execute the canary gates.
