# Implementation Plan: DeepSeek API Alignment, Stable User Identity & README Relaunch

**Branch**: `007-deepseek-api-alignment` | **Date**: 2026-07-14 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/007-deepseek-api-alignment/spec.md`

## Summary

Align the full request chain — MuhiyaCode (client) → Muhiya Gateway (proxy) → DeepSeek —
with DeepSeek's documented API behavior, then prove it. Five slices: (1) wire-protocol
validity (parameters, limits, keep-alive survival, rate-limit semantics) adjudicated by a
written audit with conform/diverge verdicts across nine documented areas; (2) a
gateway-authoritative stable `user_id` on every upstream request; (3) a client-side
provider capability profile so MuhiyaCode can never emit unsupported behavior;
(4) end-to-end cache observability (record `prompt_cache_miss_tokens`, before/after
benchmark with no steady-state regression); (5) a ≤125-line production-grade README
whose command reference is the in-app slash-command registry. Evidence base:
[audit-baseline.md](audit-baseline.md) (doc captures 2026-07-14 + wire maps of both
repos + divergence candidates D1–D11).

## Technical Context

**Language/Version**: Go 1.25+ (client module `github.com/muhiya/muhiyacode`, toolchain
go1.26.4; gateway module `gateway` at `F:\MuhiyaWorkspace\MuhiyaWorkspace`)

**Primary Dependencies**: Client: stdlib `net/http` + SSE hand-parser
(`internal/gateway/sse.go`), Bubble Tea v2 TUI (README story only touches Markdown).
Gateway: stdlib `net/http`, Postgres (`db/`), optional Redis (rate limiting only).
No new dependencies anticipated (Principle IX).

**Storage**: Gateway: Postgres `request_logs` (gains cache-miss accounting) and model
records; client: `~/.muhiya` session state (unchanged — compat boundary).

**Testing**: `go test ./... -count=1` in both repos; client `benchmarks/cachebench`
(live gateway) for Principle X before/after; new keep-alive simulation harness (spec
assumption) exercising client+gateway timeout paths end-to-end.

**Target Platform**: Client: Windows/macOS/Linux terminals. Gateway: Linux server
(single binary + Postgres).

**Project Type**: Two existing codebases, one feature: CLI/TUI client + HTTP proxy
service. All changes are incremental edits to existing packages (Principle VIII).

**Performance Goals**: Steady-state prefix-cache hit rate ≥ current live baseline
(≈96.5%, feature 001); zero upstream request-shape rejections; queued requests survive
the provider's documented 10-minute pre-inference keep-alive window.

**Constraints**: Byte-stable prefix (Principle III) — nothing added to the stable
prompt/messages region; `user_id` rides as a top-level request field (per-user constant,
Principle IV compliant). Compat boundaries frozen: client↔gateway session/effort
signaling, cost meta-chunk, key auth, `~/.muhiya` layout. Gateway body handling stays
lossless-map based (no typed-struct re-serialization of client bytes). 24 MiB body cap
unchanged.

**Scale/Scope**: Single-tenant gateway deployment, O(10²) users, O(10⁶) requests/month.
Feature touches ~6 client files, ~6 gateway files, 1 DB migration, 1 README.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Principle | Gate for this feature | Pre-Phase-0 | Post-Phase-1 |
|---|-----------|----------------------|-------------|--------------|
| I | Correctness before optimization | Alignment fixes must not change agent behavior/quality; any conflict recorded in this plan | PASS — audit-first design; fixes target validity, not prompt content | PASS — no design item mutates prompt/history content |
| II | Cache efficiency without quality loss | Every cache-affecting change states what moved and why quality holds | PASS — only additive/top-level body fields planned (user_id, stream_options already present) | PASS — contracts pin the body layout; no message-region changes |
| III | Deterministic stable prefix | No per-request variance may enter the stable prefix; gateway body mutation must stay deterministic | PASS — user_id is per-user constant; Go JSON map marshaling is key-sorted (deterministic) | PASS — upstream-request contract asserts byte-stable message region across turns |
| IV | Dynamic vs cached separation | New per-request data must ride on request params, not the prompt | PASS — user_id/thinking are top-level params | PASS |
| V | No redundant retransmission | No new retransmission; history discipline untouched | PASS — out of scope, FR-007 is a regression guard | PASS |
| VI | Honest measurement | Miss tokens recorded from provider usage; estimates labeled; SC-004 before-measurement honest about unverified abort behavior | PASS — spec already hedges; R1 verifies empirically | PASS — usage-record contract carries estimated flag |
| VII | Reference architecture: DeepSeek Reasonix | Reasonix MUST be studied before cache-affecting design; findings in research.md | PASS — R12 carries forward feature 002's 47-row mechanism inventory + fresh wire/timeout/identity pass over the reference repo | PASS — research.md §R12 records findings |
| VIII | Improve, don't rewrite | Smallest change per divergence; compat boundaries preserved | PASS — all fixes are point edits; no subsystem rewrite | PASS — Complexity Tracking empty |
| IX | Clean, maintainable, secure, provider-compatible | fmt/vet/test gates; graceful degradation for non-DeepSeek providers | PASS — capability profile is per-family; unknown providers omit user_id or map to their documented equivalent | PASS — contracts specify degradation |
| X | Verified improvements | Before/after cachebench on the feature-001 canonical workload, cold vs steady separated | PASS — planned as the US4 acceptance gate | PASS — quickstart.md defines the runs |

**Gate result**: PASS (both evaluations). No violations → Complexity Tracking is empty.

## Project Structure

### Documentation (this feature)

```text
specs/007-deepseek-api-alignment/
├── spec.md              # Feature specification (done)
├── audit-baseline.md    # Evidence base: doc captures + wire maps + D1–D11 (done)
├── plan.md              # This file
├── research.md          # Phase 0: decisions R1–R14 (all unknowns resolved)
├── data-model.md        # Phase 1: entities (identity, capability, usage, findings)
├── quickstart.md        # Phase 1: validation scenarios per SC
├── contracts/
│   ├── upstream-request.md    # Gateway→DeepSeek body/header invariants
│   ├── capability-profile.md  # Client-side provider capability contract
│   └── usage-record.md        # Cache-hit/miss + estimated-flag accounting contract
└── tasks.md             # Phase 2 (/speckit-tasks — not created here)
```

### Source Code (repository root)

```text
# Client — F:\MuhiyaCode Agent Go
internal/gateway/
├── provider.go      # request build: keep-alive-aware idle timer, capability consult
├── sse.go           # keep-alive comment/empty-line liveness; usage parsing (exists)
├── model.go         # ModelProfile → capability profile extension; limit reconciliation
internal/contract/
└── types.go         # capability struct (if surfaced to orchestrator); usage fields (exist)
internal/orchestrator/
└── history.go       # context-window guard against reconciled limits (FR-004)
README.md            # US5 rewrite (≤125 lines)
docs/                # receives any README content that moves down

# Gateway — F:\MuhiyaWorkspace\MuhiyaWorkspace
proxy/
├── handler.go       # user_id injection point; param sanitizer; stream keep-alive relay
├── thinking.go      # unconditional reasoning-field sanitization (D3)
├── translator.go    # usage: prompt_cache_miss_tokens plumb-through (exists, unused)
├── stickysession.go # swap observability/logging (D6)
└── limiter.go       # (read-only reference — per-plan limits out of scope)
db/
├── db.go            # request_logs cache_miss_tokens column + model-limit corrections
└── migrations/      # new migration for the column / model rows
main.go              # (routes unchanged)
```

**Structure Decision**: Two existing repos, incremental edits only. The feature's
artifacts and audit report live in the client repo (`specs/007-…`); gateway changes are
executed against `F:\MuhiyaWorkspace\MuhiyaWorkspace` per the established division of
labor (findings docs in the feature dir drive the gateway implementer).

## Complexity Tracking

> No Constitution Check violations — table intentionally empty.
