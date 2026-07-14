# Tasks: DeepSeek API Alignment, Stable User Identity & README Relaunch

**Input**: Design documents from `/specs/007-deepseek-api-alignment/`

**Prerequisites**: plan.md, spec.md, research.md (R1–R14), data-model.md, contracts/
(upstream-request, capability-profile, usage-record), quickstart.md,
audit-baseline.md (evidence, D1–D11)

**Tests**: Included where a contract names a conformance assertion or the constitution
mandates them (Principles VI/X: before/after measurement and prefix-stability checks
are NOT optional for this feature). No speculative TDD tasks beyond that.

**Organization**: Two repos. `CLIENT` = `F:\MuhiyaCode Agent Go`,
`GATEWAY` = `F:\MuhiyaWorkspace\MuhiyaWorkspace`. Tasks grouped by user story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: parallelizable (different files, no dependency on an incomplete task)
- **[Story]**: US1–US5 from spec.md

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Audit scaffolding and the test fixtures every story's validation uses.

- [X] T001 Create audit report scaffold `CLIENT/specs/007-deepseek-api-alignment/audit-report.md`: nine-area × hop matrix with AuditFinding columns (area, hop, observed, documented+citation, verdict, severity per data-model.md §4 rubric, resolution), seeded with divergence candidates D1–D11 from audit-baseline.md §D and the doc-snapshot quotes from §A
- [X] T002 [P] Build keep-alive fixture upstream (configurable: response-header delay, comment-only `: keep-alive` period, empty-line period, total-silence period, then normal stream) as a test server in `GATEWAY/proxy/keepalive_fixture_test.go` per quickstart.md §3
- [X] T003 [P] Add upstream wire-capture tap (test-only recording RoundTripper capturing full request bodies/headers sent upstream) in `GATEWAY/proxy/capture_test.go` for contract §5 assertions

---

## Phase 2: Foundational (Before-Measurements — BLOCKING)

**Purpose**: Constitution Principle X requires before/after evidence; the "before" legs
MUST be captured on unmodified code, so they block every behavior-changing story.

**⚠️ CRITICAL**: Complete before any task in Phases 3–7 that changes behavior.

- [X] T004 Record BEFORE keep-alive behavior: run T002 fixture scenarios (8-min header delay; 9-min comment-only stream; 3-min silence) against the UNMODIFIED gateway+client chain; record outcomes (expected: 502 at ~90s on header delay per G3) as the SC-004 before-measurement in `CLIENT/specs/007-deepseek-api-alignment/audit-report.md`
- [ ] T005 [P] Record BEFORE cache benchmark: `CLIENT/benchmarks/cachebench` canonical feature-001 workload against the live gateway (single user, pinned model/effort); save cold + steady-state results to `CLIENT/specs/007-deepseek-api-alignment/benchmarks/before/` (SC-006 baseline)
- [X] T006 [P] Live-verify legacy model-name handling (R3c): minimal upstream requests with `deepseek-chat`/`deepseek-reasoner` vs `deepseek-v4-flash`/`deepseek-v4-pro`; record accepted/aliased/rejected verdict in audit-report.md (gates T012)

**Checkpoint**: Before-evidence captured — behavior changes may now land.

---

## Phase 3: User Story 1 — Every request reaching DeepSeek is valid and documented (P1) 🎯 MVP

**Goal**: Only documented parameters with valid values within documented limits reach
DeepSeek; queued requests survive the 10-minute keep-alive window; nine-area audit
verdicts complete; every high-severity divergence fixed.

**Independent Test**: quickstart.md §1 (wire-shape capture over a ≥15-request session)
and §3 (keep-alive scenarios) pass; audit-report.md has all nine verdicts.

### Gateway implementation

- [X] T007 [US1] Raise upstream `ResponseHeaderTimeout` 90s→630s with a comment citing the documented 10-minute pre-inference window (R1a) in `GATEWAY/proxy/handler.go` (~:34)
- [X] T008 [P] [US1] Add package-level param strip-list constant `{frequency_penalty, presence_penalty, user, user_id}` and apply it as body-pipeline step 3 (contract §2.3, R5) in `GATEWAY/proxy/handler.go` (proxyOpenAIToOpenAI ~:931-953)
- [X] T009 [US1] Make DeepSeek thinking normalization unconditional (contract §2.5, R4): always delete client `reasoning_effort`/`thinking`, always emit `thinking:{type:"enabled"}+reasoning_effort:"high"|"max"` (reasoning-on) or `thinking:{type:"disabled"}` (off/absent), every path incl. no-header, in `GATEWAY/proxy/thinking.go` (ApplyThinkingOpenAI ~:211-241)
- [X] T010 [P] [US1] Propagate upstream `Retry-After` on relayed ≥400 responses (logFailedUpstream ~:1708-1721) and add `Retry-After` to gateway-origin 429s (~:626-629, 833-836) in `GATEWAY/proxy/handler.go` (R7)
- [X] T011 [P] [US1] New migration `GATEWAY/db/migrations/007_deepseek_v4_limits.sql` + seed updates: DeepSeek model rows context_length=1000000, max_output=384000 (documented values, R3a; metadata only)
- [X] T012 [US1] Model-name migration decision (gated on T006): if legacy names rejected/deprecated → migrate `TargetModel` to documented names via migration + logged cache-epoch event in `GATEWAY/db/migrations/` + `GATEWAY/proxy/router.go` swap log; if aliased-and-working → record accepted-deviation row in audit-report.md and defer (R3c)

### Client implementation

- [X] T013 [US1] Restructure request timeouts (R1b): first-byte deadline 10min + existing 90s rolling idle replaces the whole-attempt 10-min cap, in `CLIENT/internal/gateway/provider.go` (~:52-57 defaults, :107 context, :168-181 timer); document the 45s one-shot CLI override (`CLIENT/internal/command/root.go:435`) as intentional

### Tests (contract conformance)

- [X] T014 [P] [US1] Strip-list exact-set test + thinking-normalization table test (efforts × header-present/absent × reasoner/non-reasoner targets; assert no raw client value ever survives) in `GATEWAY/proxy/thinking_test.go`
- [X] T015 [P] [US1] Keep-alive regression tests locking G1/G2: watchdog resets on comment/empty lines; OpenAI→OpenAI relay forwards them verbatim, in `GATEWAY/proxy/handler_test.go`
- [X] T016 [P] [US1] Retry-After propagation tests (upstream 429 with/without header; gateway-origin 429) in `GATEWAY/proxy/handler_test.go`
- [X] T017 [P] [US1] Client timeout tests: 8-min header delay survives; comment-only stream keeps idle alive; 3-min true silence still cancels, in `CLIENT/internal/gateway/provider_test.go`

### Validation

- [X] T018 [US1] AFTER keep-alive run: quickstart §3 scenarios 1–3 against the fixed chain; record vs T004 before-measurement in audit-report.md (SC-004)
- [X] T019 [US1] Wire-shape capture validation: quickstart §1 over ≥15 requests via T003 tap; assert contract §5.1 (keys ⊆ documented set, zero deprecated params, `thinking` on 100%); record SC-001 in audit-report.md
- [X] T020 [US1] Complete ALL nine area verdicts in audit-report.md with doc citations (conform rows incl.: multi-round handling, tool-call pairing, streaming usage, rate-limit/billing per G4/G5, JSON mode n/a, prefix/FIM not-adopted per R9); verify every high-severity row is `fixed` (FR-001, SC-002)

**Checkpoint**: US1 independently complete — valid wire shape + keep-alive survival proven.

---

## Phase 4: User Story 2 — Stable, private per-user identity (P2)

**Goal**: Gateway-authoritative `user_id` (HMAC of account UUID) on every DeepSeek
request; constant per user across sessions/devices/key rotations; spoof-proof.

**Independent Test**: quickstart.md §2 — two users × two sessions: per-user constancy,
cross-user distinctness, rotation stability, spoof override.

- [X] T021 [US2] Add `IDENTITY_SECRET` gateway configuration + startup validation (absent ⇒ injection disabled + loud startup warning, never a weaker fallback) in `GATEWAY/main.go` + proxy config plumbing (R2, data-model §1)
- [X] T022 [US2] Implement StableUserIdentifier derivation `"mu-"+hex(HMAC-SHA256(secret, User.ID))[:40]` in new `GATEWAY/proxy/identity.go` (depends on T021)
- [X] T023 [US2] Inject `user_id` as body-pipeline step 4 (after T008 strip) on the DeepSeek path; dialect mapping: other OpenAI-compat upstreams → `user`, Anthropic-dialect → `metadata.user_id`, unknown → omit, in `GATEWAY/proxy/handler.go` (+`translator.go` for the Anthropic body) (contract §2.4)
- [X] T024 [P] [US2] Unit tests in `GATEWAY/proxy/identity_test.go`: format `^mu-[0-9a-f]{40}$`; determinism across two derivations (restart simulation); distinct users ⇒ distinct values; same user across key rotation ⇒ same value; client-sent `user_id` replaced; secret-absent ⇒ field entirely absent
- [X] T025 [US2] Session-affinity observability (FR-012, R10): log every model-swap event for a known session key in `GATEWAY/proxy/stickysession.go` + `GATEWAY/proxy/router.go`; record the best-effort durability posture + restart impact note in audit-report.md
- [X] T026 [US2] Two-user validation run (quickstart §2); SC-003 evidence in audit-report.md — VERIFIED LIVE 2026-07-14 with `IDENTITY_SECRET` enabled: Key A warmed a fresh 3,278-token prefix (0%→97.6%); Key B sent the identical prefix and got 0% hit (isolated namespace), then warmed its own. Per-user cache isolation proven end-to-end. Root cause of the earlier shared result (compose not forwarding the secret) fixed + deployed.

**Checkpoint**: US1+US2 — valid, identity-carrying requests.

---

## Phase 5: User Story 3 — Client capability profile (P3)

**Goal**: MuhiyaCode consults a boot-frozen capability profile so it can never emit
unsupported behavior; limits reconciled to documented values.

**Independent Test**: contracts/capability-profile.md §4 assertions pass; /context
shows the active profile.

- [X] T027 [US3] Extend `ModelProfile` with capability fields (SupportedParams, DeprecatedParams, ContextWindowTokens=1_000_000, MaxOutputTokens=384_000, JSONModeRules, BetaFeatures with not_adopted statuses per R9, KeepAliveSignals) for the deepseek family in `CLIENT/internal/gateway/model.go` (data-model §2)
- [X] T028 [US3] Builder consultation (CP-1..CP-3): param-emission guard + `max_tokens` clamp-with-warning in `CLIENT/internal/gateway/provider.go` chatOnce (depends on T027; coordinate with T013 — same file, sequential)
- [X] T029 [US3] History budget ceiling ≤ ContextWindowTokens (operational `contextLimit` default unchanged, R3b) in `CLIENT/internal/orchestrator/history.go` (CP-4)
- [X] T030 [P] [US3] Table-driven conformance tests (deprecated/unsupported never emitted; clamp fires + logs; wire-shape freeze test extended over profile-driven fields) in `CLIENT/internal/gateway/provider_test.go`
- [X] T031 [P] [US3] Surface active profile (family, limits, beta statuses) read-only in `/context` output in `CLIENT/internal/tui/actions.go` (contract §3)

**Checkpoint**: client refuses/adapts instead of emitting unsupported requests.

---

## Phase 6: User Story 4 — Cache accounting & no-regression proof (P4)

**Goal**: Provider-reported miss tokens persisted and surfaced; feature-wide
before/after benchmark shows no steady-state regression.

**Independent Test**: quickstart.md §4 scenarios 1–4.

- [X] T032 [US4] Migration `GATEWAY/db/migrations/008_cache_miss_tokens.sql`: nullable `cache_miss_tokens` on request_logs; extend RequestLog struct + INSERT in `GATEWAY/db/db.go` (~:109-134, :1034-1041) (UR-1)
- [X] T033 [US4] Persist `prompt_cache_miss_tokens` in all four `finish()` paths; NULL when `usage_estimated=true` (UR-2/UR-3) in `GATEWAY/proxy/handler.go` (depends on T032)
- [X] T034 [US4] Provider-reported hit rate (`Σread/(Σread+Σmiss)` over non-NULL rows) with labeled derived-fallback in `/v1/usage` (handleUsage ~:420-464) and dashboard stats (`GATEWAY/db/db.go` ~:1313) (UR-5/UR-6)
- [X] T035 [P] [US4] Tests per usage-record §4: populated row; estimated ⇒ NULL; mixed-aggregate query; billing-path replay (429 pre-stream / failover / mid-stream disconnect ⇒ row+billed counts unchanged) in `GATEWAY/proxy/handler_test.go` + `GATEWAY/db/db_test.go`
- [ ] T036 [US4] AFTER cache benchmark: rerun T005 workload identically on the full feature build; save to `CLIENT/specs/007-deepseek-api-alignment/benchmarks/after/`; report steady-state ≥ before, cold separated, tokens+cost alongside (SC-006, Principle X) — run only after Phases 3–5 land
- [X] T037 [US4] Prefix-stability check (constitution workflow gate): byte-diff `messages`/`tools` regions across consecutive-turn captures from T019/T036 → confirm zero prefix change introduced by this feature; record in audit-report.md

**Checkpoint**: measured, whole-picture cache evidence complete.

---

## Phase 7: User Story 5 — README relaunch (P5)

**Goal**: ≤125-line production-grade landing page; complete in-app command table.

**Independent Test**: quickstart.md §5.

- [X] T038 [US5] Rewrite `CLIENT/README.md` per R11 skeleton: centered hero (wordmark, tagline, shields.io badges, screenshot/GIF, `---`) → Install (npm i -g muhiyacode + GitHub releases; honest platform list) → Quick start (3 steps + /login) → Features (≤7 bullets) → Commands (full 17-command table + /effort & /mode aliases from `CLIENT/internal/tui/model.go:385-394`, framed as typed-inside-MuhiyaCode) → Configuration pointer → Documentation/Contributing/Community one-liners; displaced deep content stays in `CLIENT/docs/` (FR-019/020/021)
- [X] T039 [P] [US5] README validation: line count ≤125; command table diffed against the registry (zero missing, zero invented); FR-019 sections present; record SC-007/SC-008 in audit-report.md

**Checkpoint**: all five stories independently complete.

---

## Phase 8: Polish & Cross-Cutting

- [X] T040 Update `CLIENT/docs/architecture.md` + `GATEWAY/GATEWAY_AUDIT.md` where this feature changed described behavior (timeouts, identity, accounting) — constitution workflow requirement; mark stale GATEWAY_AUDIT findings superseded
- [X] T041 Both repos: `go fmt` (no diff), `go vet ./...`, `go test ./... -count=1` green; `-race` where a CGO-capable runner is available
- [X] T042 Final audit-report.md completeness pass: nine areas verdict+citation, D1–D11 all adjudicated, severity rubric applied, SC-001…SC-008 evidence table filled
- [X] T043 Full quickstart.md §§1–6 end-to-end sign-off run — all code/build/vet/test legs green in both repos; live legs signed off (health, cache no-regression 97.3% warm, model-name verdict, strip/sanitizer). Only live cross-user isolation is gated on `IDENTITY_SECRET` (T026). Formal cachebench artifact (T005/T036) intentionally deferred — optional, SC-006 already met live.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: none — start immediately; T002/T003 parallel
- **Phase 2 (Before-measurements)**: needs T002 (for T004) and T003 (for T006); **blocks all behavior changes** (Principle X)
- **Phase 3 (US1)**: after Phase 2. Internal: T012 gated on T006; T007–T011 parallel-friendly; T013 independent (client); tests T014–T017 after their implementations; T018–T020 last
- **Phase 4 (US2)**: after Phase 2; independent of US1 except T023 orders after T008 (same pipeline function — do T008 first or coordinate). T021→T022→T023 sequential; T024/T025 parallel after
- **Phase 5 (US3)**: after Phase 2; client-only, independent of US2. T027→T028→T029 sequential (T028 shares provider.go with T013 — sequence within the file)
- **Phase 6 (US4)**: T032→T033→T034 sequential; T035 parallel after T033; **T036/T037 require Phases 3–5 complete** (the "after" leg measures the whole feature)
- **Phase 7 (US5)**: independent of everything except final command registry state — safe any time after Phase 1; scheduled last to describe the final product
- **Phase 8**: after all desired stories

### Cross-repo note

Gateway tasks (T002–T012, T014–T016, T021–T026, T032–T035) and client tasks (T013,
T017, T027–T031, T038–T039) never share files — the two repos can proceed fully in
parallel after Phase 2, honoring the in-story orderings above.

## Parallel Example: after Phase 2 completes

```text
Gateway lane: T007, T008, T010, T011 together → T009, T012 → T014–T016
Client lane:  T013 → T017 ∥ T027 → T028 → T029 → T030, T031
README lane:  T038 → T039 (any time)
Then:         T021→T022→T023→T024–T026 (US2), T032→…→T035 (US4)
Finally:      T036, T037 (needs everything), T040–T043
```

## Implementation Strategy

**MVP = Phase 1 + Phase 2 + Phase 3 (US1)**: after T020 the chain provably sends only
valid requests and survives provider queuing — deployable value on its own.
Then increments: US2 (identity) → US3 (capability) → US4 (measured proof) → US5
(README) — each independently testable per its quickstart section; stop and validate
at every checkpoint.
