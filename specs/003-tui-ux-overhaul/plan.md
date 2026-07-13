# Implementation Plan: MuhiyaCode TUI & Agent Experience Overhaul

**Branch**: `003-tui-ux-overhaul` | **Date**: 2026-07-12 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/003-tui-ux-overhaul/spec.md`

## Summary

A full polish pass over MuhiyaCode's terminal experience plus the agent's skills delivery, in nine wired-together strands: (1) a semantic-token palette with light/dark adaptivity and no forced background, fixing the Warp rendering class of bugs ([research D4/D5/D13](research.md)); (2) a finished inline markdown renderer replacing the `**`-strip defect at `render.go:80` (D3); (3) a strict collapsed/expanded tool-display contract with `+A −R` outcome measures parsed from existing diff payloads (D6); (4) an ephemeral thinking indicator and a three-metric task summary (credits · tokens · cache %) rendered into the transcript after the final message, fed by the existing per-task usage delta plus a new per-request cost field (D7/D8); (5) per-request credits wired from the gateway's `muhiya_log` SSE chunk, gate extended to `MuhiyaCode` (D1); (6) a new key-authenticated `GET /v1/usage` gateway endpoint behind a `/usage` command, with `/login`//`/logout` palette visibility tied to sign-in state (D2/D11); (7) a regrouped context modal including session credits (D10); (8) header accuracy — full workspace path, "Subagent" label (D9); (9) skills advertised as a deterministic compact listing in the stable prefix with on-demand loading via the existing `read_file` tool, guarded by prefix-stability tests and a live before/after cache benchmark (D12/D14).

## Technical Context

**Language/Version**: Go 1.25.0 (MuhiyaCode, [go.mod](../../go.mod)); Go 1.22 (MuhiyaLLM gateway, `F:\MuhiyaWorkspace\MuhiyaWorkspace`)

**Primary Dependencies**: `charm.land/bubbletea/v2 v2.0.8`, `charm.land/lipgloss/v2 v2.0.5`, `charm.land/bubbles/v2 v2.1.1`, `charmbracelet/x/ansi`, `go-runewidth` (TUI); gateway is stdlib `net/http` + `lib/pq` + `go-redis`. **No new runtime dependencies** (markdown fix stays hand-rolled per research D3). Two **test-only** modules are added for frame goldens — `charmbracelet/x/exp/teatest/v2` + `x/exp/golden` — justified under Constitution IX (standard Charm test harness, test scope only, no runtime linkage); caveat: teatest/v2 upstream targets `github.com/charmbracelet/bubbletea/v2` while this repo uses the `charm.land` module rename — if no compatible release exists, fall back to stdlib golden-file comparison of `View()` output at 80×24/Ascii (zero new modules), keeping the same pinned-frame obligations.

**Storage**: `~/.muhiya` state layout (sessions, secrets user-only-permissions, SQLite via `modernc.org/sqlite`) — unchanged layout, `Usage`/`UsageRecord` gain an optional cost field (additive). Gateway: PostgreSQL `request_logs` ledger (read-only for this feature; one new day-bounded SUM query).

**Testing**: `go fmt` / `go vet` / `go test ./... -count=1` (constitution IX); teatest/v2 + `x/exp/golden` frame goldens pinned to 80×24 + `colorprofile.Ascii`; markdown corpus goldens; extended `prompt_stability_test.go` + existing `cachehit_guard_test.go`; live before/after run on `benchmarks/cachebench` (constitution X). Gateway: `go test` in the gateway repo for the new endpoint + meta-chunk gate.

**Target Platform**: Windows/macOS/Linux terminals — verification matrix: Warp, Windows Terminal, iTerm2, VS Code integrated terminal; truecolor primary, 256/16-color downsampled, `NO_COLOR` honored; 60×20 render floor, 80×24 design baseline.

**Project Type**: Terminal application (Bubble Tea v2 TUI) + companion gateway service change (cross-repo, same owner).

**Performance Goals**: `/usage` result <3 s healthy-path (SC-007); no perceptible render lag from inline tokenizer during streaming (re-render per token is the existing pattern); session cache-hit rate non-regression on realistic multi-turn benchmark (SC-008).

**Constraints**: Constitution I–X; byte-stable prefix (system prompt + tools) — the only permitted prefix change is the deterministic skills listing (D12) with updated stability fixtures; credits shown only from gateway-reported cost, omission over estimation (FR-012/012a); no security-model changes (key stays a bearer header; no admin credentials in the TUI).

**Scale/Scope**: ~10 files in `internal/tui` (view/render/model/actions/bridge/rtl), touchpoints in `internal/contract` (Usage/UsageRecord/TaskStats), `internal/orchestrator` (prompt.go skills section, engine.go stats), `internal/gateway` (provider.go chunk parsing, new usage client), `internal/command` (application.go actions), `internal/workspace` (lazy skill loading); gateway repo: `proxy/handler.go` (gate + endpoint), `db/db.go` (one query), route table. ≤40 skills listed; transcripts thousands of lines (existing viewport virtualization untouched).

## Constitution Check

*GATE: evaluated pre-Phase 0 and re-checked post-Phase 1 design — PASS (no violations requiring Complexity Tracking).*

| # | Principle | Gate result | Evidence / design hook |
|---|---|---|---|
| I | Correctness before optimization | PASS | No behavior-affecting optimization; the one model-input change (skills listing) adds context, never removes it. Quality validated per X before merge (research D14). |
| II | Cache efficiency without quality loss | PASS | No content removed/truncated/delayed. Skills listing *adds* stable content; per-use skill bodies ride the turn. Change statement + validation required in D12/D14. |
| III | Deterministic stable prefix | PASS (guarded) | Skills section is sorted, session-pinned, byte-identical for identical config (002 FR-013 pattern); `prompt_stability_test.go` extended with skills fixtures; no timestamps/randomness; TUI-only changes never touch the prefix ([research A11](research.md)). |
| IV | Dynamic/cached separation | PASS (justified) | Skills *listing* placed in the prefix is static configuration (like the MCP tool surface, 002 precedent), not per-turn dynamic content — written justification: research D12. All per-turn material (selected skill bodies, task brief) stays on the user message. Credits ride the response, not the prompt. |
| V | No redundant retransmission | PASS | Listing sent once as cached prefix (vs. per-turn alternative rejected in D12); skill bodies loaded once via `read_file` with existing duplicate-read blocking; `/skills` eager-load-all becomes lazy. |
| VI | Honest measurement | PASS | Credits = gateway-computed `muhiya_log.cost` (provider-reported usage path); `usage_estimated` surfaced (`~` marker) or omitted; cache % token-weighted from provider fields, omitted when nil; benchmark reports totals *and* hit rate (whole picture). |
| VII | Reasonix reference | PASS | Findings recorded: research A13 cites 001 research Part A + 002 FR-013; D12 designed against them (static prefix / dynamic suffix). No new deviations. |
| VIII | Improve, don't rewrite | PASS | Inline tokenizer added to existing renderer (no glamour); palette refactored in place; footer → transcript entry reuses `TaskStats`; gateway gate is an allowlist edit; `~/.muhiya` layout and wire protocol preserved (additive fields only). |
| IX | Clean, maintainable, secure, provider-compatible | PASS | No new runtime deps (two test-only golden-test modules justified in Technical Context); `go fmt/vet/test` gates in quickstart; security model untouched (no admin creds client-side — D2 rationale; secrets handling unchanged; advertised skills scoped to workspace-resident roots so `read_file` stays inside existing path containment — skills-delivery §1); generic OpenAI endpoints degrade gracefully: no `muhiya_log` → credits omitted, no cache fields → rate omitted (FR-012). |
| X | Verified improvements | PASS (planned) | D14: before/after `benchmarks/cachebench` run (same model/gateway/effort/workload) for the prefix-affecting change; credits cross-check vs `request_logs`; results stored under `specs/003-tui-ux-overhaul/benchmarks/`. |

**Workflow gates** (constitution "Development Workflow & Quality Gates"): prefix-stability check — extended fixtures (D12/D14); before/after evidence — D14 layer 3; security-sensitive review — not triggered (no permission/path/secret/command-blocking changes; gateway endpoint reviewed against `docs/security.md` auth posture anyway); docs sync — `README.md`/`docs/agent-design.md`/`docs/architecture.md` updated for the skills-listing prompt change and credits display in the same change.

## Project Structure

### Documentation (this feature)

```text
specs/003-tui-ux-overhaul/
├── plan.md              # This file
├── spec.md              # Feature spec (clarified 2026-07-12)
├── research.md          # Phase 0 — current-state evidence + decisions D1–D14
├── data-model.md        # Phase 1 — entities, fields, validation, lifecycles
├── quickstart.md        # Phase 1 — end-to-end validation scenarios
├── contracts/
│   ├── usage-api.md     # Gateway wire: muhiya_log chunk + GET /v1/usage
│   ├── task-summary.md  # Metric formulas, omission rules, placement, lifecycle
│   ├── tool-display.md  # Per-tool collapsed/expanded display contract
│   ├── visual-system.md # Semantic tokens, glyph table, breakpoints, degradation
│   └── skills-delivery.md # Listing format, determinism rules, load-on-use protocol
├── benchmarks/          # D14 before/after evidence (created during implementation)
└── tasks.md             # Phase 2 (/speckit-tasks — not created by /speckit-plan)
```

### Source Code (repository root)

```text
F:\MuhiyaCode Agent Go\                    # this repo
├── internal/tui/
│   ├── render.go        # D3 inline tokenizer; D4 semantic palette; table cell inline pass
│   ├── view.go          # D5 glyph table; D6 tool lines; D7 thinking; D8 summary entry;
│   │                    # D9 header; D10 modal groups; D13 breakpoints; footer removal
│   ├── model.go         # D6 verbose flow; D7 state cleanup; D8 summary lifecycle;
│   │                    # D11 palette filtering; placeholder text (FR-014)
│   ├── actions.go       # D10 /context regroup; D11 /usage command + login visibility;
│   │                    # lazy /skills selection
│   ├── bridge.go        # summary + cost plumbing (statsMsg)
│   ├── rtl.go           # unchanged; inline tokenizer must respect RTL wrapping
│   └── *_test.go        # teatest goldens (80×24, Ascii profile), markdown corpus
├── internal/contract/
│   └── types.go, cache.go  # Usage/UsageRecord: +CostUSD *float64, +CostEstimated bool;
│                           # TaskStats: +StopCause (interruption constants) — data-model §1.3
├── internal/orchestrator/
│   ├── prompt.go        # D12 deterministic skills section (stable prefix)
│   ├── engine.go        # cost fields on usage records; task record-range capture for credits;
│   │                    # StopCause stamping on user-stop/error/disconnect paths
│   └── prompt_stability_test.go, cachehit_guard_test.go  # extended fixtures (D14)
├── internal/gateway/
│   ├── provider.go      # D1 muhiya_log chunk parsing (streaming final frames)
│   └── usage.go         # D2 client for GET /v1/usage (new file)
├── internal/command/
│   └── application.go   # Actions.FetchUsage; lazy ListSkills; skills-at-startup wiring
├── internal/workspace/
│   └── skills.go        # session-pinned discovery result; lazy LoadSkillInstructions
└── benchmarks/cachebench/  # D14 before/after harness (exists from 001)

F:\MuhiyaWorkspace\MuhiyaWorkspace\        # gateway repo (companion change)
├── proxy/handler.go     # D1 sendMuhiyaMetaChunk allowlist + usage_estimated field;
│                        # D2 GET /v1/usage handler (authenticateVirtualKey)
├── db/db.go             # D2 day-bounded SUM(cost) query (GetUserSpendingToday)
└── main.go              # route registration for /v1/usage
```

**Structure Decision**: Two-repo change with a strict dependency direction: the gateway ships first (D1 gate + D2 endpoint are backward-compatible additions — clients that ignore `muhiya_log` and never call `/v1/usage` are unaffected), then MuhiyaCode consumes them. Within MuhiyaCode, all visual work is confined to `internal/tui` (prefix-safe by construction, research A11); the only prompt-affecting change is isolated to `prompt.go` + its stability fixtures so it can be benchmarked and reverted independently of the visual overhaul.

## Complexity Tracking

No constitution violations to justify. Two scope notes recorded for reviewer awareness (not violations):

| Item | Why needed | Simpler alternative rejected because |
|---|---|---|
| Cross-repo gateway change (D1/D2) | Credits/usage data exists only server-side; FR-012a forbids client-side estimation | Client-side price tables violate honest measurement (VI) and clarified FR-012a; admin-credential reuse violates least-privilege (IX) |
| Stable-prefix growth by the skills listing (D12) | FR-023 requires the model to recognize applicable skills; listing must be visible every turn to work | Per-turn listing retransmits identical bytes every request (violates V); manual-only `/skills` fails FR-023/FR-025 |
