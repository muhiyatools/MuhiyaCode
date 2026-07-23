# Implementation Plan: Automatic Skill Use & Interface Polish

**Branch**: `013-auto-skills-ui-polish` | **Date**: 2026-07-20 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/013-auto-skills-ui-polish/spec.md`

**Note**: This template is filled in by the `/speckit-plan` command. See `.specify/templates/plan-template.md` for the execution workflow.

## Summary

Give the agent genuine, model-driven skill awareness: an all-roots skill catalog (workspace + home + configured folders) advertised in the stable prefix, a new `read_skill` tool that loads any cataloged skill by name (bounded, containment-safe), a strengthened prefix instruction that tells the model to read a relevant skill **before** the related work and keep applying it, and an optional `skills` field on `run_subagent` so the main agent equips delegated work (sub-agents never self-select). Alongside, a truth-and-polish pass over the TUI: cache-inclusive full-digit token figures everywhere, a session cache-hit rate computed over **all** streams (main + sub-agents + aux) from the already-maintained paired sums, a radically simplified `/context` card, removal of `/errors`+harness-friction UI and `/permissions`(+`/mode`), a "Log Out of Account" rename, DeepSeek-mapping text removal, an always-visible permission mode with a "Shift + Tab to cycle" hint, permanently visible to-dos while busy (Ctrl+T removed), and ←/→ agent switching on an empty composer (Tab keeps autocomplete only).

## Technical Context

**Language/Version**: Go 1.25.0 (module `github.com/muhiya/muhiyacode`)

**Primary Dependencies**: Bubble Tea v2 / Lipgloss v2 / Bubbles v2 (TUI), cobra (CLI), modelcontextprotocol/go-sdk (MCP), golang.org/x/text (already vendored; NOT used for number formatting — a small hand-rolled comma-grouping helper keeps the dependency surface unchanged)

**Storage**: `~/.muhiya` state layout (sessions, usage.jsonl append-only records, prefix_shape.json sidecar) — layout unchanged by this feature

**Testing**: `go test ./... -count=1` (offline golden/unit suites: `internal/tui` render goldens, `internal/orchestrator` engine suites, `internal/contract` aggregate tests); race-enabled on CGO-capable runners; live gauntlet sessions for Principle X verification

**Target Platform**: Windows / macOS / Linux terminals (win32 primary dev environment)

**Project Type**: Single Go CLI/TUI application (terminal coding agent against OpenAI-compatible gateways)

**Performance Goals**: Skill discovery adds ≤5% to session startup (SC-003); no added per-task latency for skill-free tasks; steady-state session cache-hit rate not regressed beyond run variance (SC-011)

**Constraints**: Byte-stable system prefix within a session (Constitution III); dynamic content rides the turn tail (IV); provider-reported figures only, unavailable never fabricated (VI); no weakening of real-path containment — home-root skills are readable **only** through the name-keyed `read_skill` tool, never via widened file-tool containment (IX)

**Scale/Scope**: Catalog bounded at 40 skills across all roots (existing bound), 32 KiB per skill body (existing bound); ~19 `HumanTokens` display call sites; 4 UI surfaces changed (footer, todos, palette, context card); 2 tool-contract changes (`read_skill` new, `run_subagent.skills` added) forming one upgrade cache epoch

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Principle | Pre-research | Post-design |
|---|-----------|--------------|-------------|
| I | Correctness Before Optimization | PASS — feature adds capability and corrects misleading displays; no optimization trades against behavior | PASS — no conflicts arose |
| II | Cache Efficiency Without Quality Loss | PASS (plan) — catalog stays in the stable prefix; skill bodies load on demand into the tail; nothing the model needs is removed or delayed | PASS — design adds content (catalog + stronger hint); removal-free; SC-011 guards regression |
| III | Deterministic Stable Prefix | PASS (plan) — catalog is discovered once at session start, sorted, byte-stable; new tool schemas are static | PASS — `read_skill` definition and `run_subagent.skills` are session-invariant; one-time **upgrade epoch** on first run of the new binary (prefix-shape sidecar attributes it; precedent: feature-g4 `ask_user` epoch, commit 488ea14) |
| IV | Separation of Dynamic and Cached Content | PASS (plan) — skill bodies are per-task dynamic content | PASS — bodies arrive as `read_skill` tool results / sub-agent brief sections in the tail, never in the prefix |
| V | No Redundant Retransmission | PASS (plan) — dedupe required by FR-008 | PASS — manual `<skill name="…">` markers are detected at task start; `read_skill` on an already-provided skill returns a short pointer notice, not the body; results append-only |
| VI | Honest Measurement | PASS (plan) — feature strengthens VI | PASS — session-wide rate uses provider-reported paired sums only; unavailable stays "unavailable"; full-digit displays remove abbreviation loss; totals become cache-inclusive |
| VII | Reference Architecture: DeepSeek Reasonix | Required — findings captured | DONE — see [research.md](research.md) §R13 (catalog-pointer prefix + lazy tail loads mirror the Reasonix layout) |
| VIII | Improve, Don't Rewrite | PASS (plan) — every change extends an existing seam | PASS — discovery, prompt section, tool registry, aggregate, and renderers are extended in place; no subsystem rewritten; `~/.muhiya` layout and wire protocol untouched |
| IX | Clean, Maintainable, Secure, Provider-Compatible | PASS (plan) | PASS — no new dependencies; containment not widened (name-keyed tool instead); graceful degradation without cache fields preserved; `go fmt`/`vet`/tests gate in quickstart |
| X | Verified Improvements | Required — verification designed | DONE — quickstart.md defines the before/after live-session procedure (same model/gateway/effort/workload) for SC-003/SC-011 and the skill-use acceptance runs |

**Workflow gates**:

- Cache-affecting change (system prompt composition + toolset) → automated prefix-stability check REQUIRED: extend the existing byte-stability tests (`delegation_prompt_test.go` DG-5 pattern, `cachehit_guard_test.go` scenarios) to cover the all-roots catalog and the new tool set. Planned in [contracts/skills-autouse.md](contracts/skills-autouse.md) §6.
- Permission-layer / containment-adjacent change (`read_skill` reads home-root files) → review against `docs/security.md` REQUIRED. The tool serves **only** session-cataloged `SKILL.md` files under the documented skill roots, size-bounded, secret-redaction untouched; no raw-path input exists. Planned in [contracts/skills-autouse.md](contracts/skills-autouse.md) §4.
- `README.md` / `docs/agent-design.md` / `docs/architecture.md` updates in the same change (token/cache display behavior and skills flow both alter documented behavior).

## Project Structure

### Documentation (this feature)

```text
specs/013-auto-skills-ui-polish/
├── plan.md              # This file (/speckit-plan command output)
├── research.md          # Phase 0 output (/speckit-plan command)
├── data-model.md        # Phase 1 output (/speckit-plan command)
├── quickstart.md        # Phase 1 output (/speckit-plan command)
├── contracts/           # Phase 1 output (/speckit-plan command)
│   ├── skills-autouse.md
│   ├── display-formats.md
│   └── ui-surfaces.md
└── tasks.md             # Phase 2 output (/speckit-tasks command - NOT created by /speckit-plan)
```

### Source Code (repository root)

```text
internal/
├── workspace/
│   └── skills.go            # DiscoverSkills origins + all-roots catalog feed (extend)
├── command/
│   ├── skills.go            # workspaceSkillListings → allRootSkillListings; catalog handoff (extend)
│   ├── runtime_build.go     # PromptContext.Skills now all-roots; catalog → EngineConfig (extend)
│   └── actions.go           # LoadSkill action unchanged; context-report action feeds new card
├── orchestrator/
│   ├── prompt.go            # SkillListing (path dropped from render), renderSkillsSection (extend)
│   ├── skills_tool.go       # NEW: read_skill tool (catalog-keyed, bounded, dedupe-aware)
│   ├── subagent.go          # subagentInput.Skills; brief equipping (extend)
│   ├── contextreport.go     # untouched fields; consumed subset shrinks
│   └── engine.go            # session skill catalog snapshot; manual-marker scan at task start
├── instructions/
│   └── prompt.go            # SkillsHintBody rewrite (read_skill mention + MentionsTools);
│                            # reasoning-level description strings lose DeepSeek text
├── contract/
│   ├── format.go            # NEW FullTokens (comma-grouped); HumanTokens retired from displays
│   └── cache.go             # all-streams display rate helper over Paired sums (extend)
├── tui/
│   ├── keys.go              # ctrl+t removed; tab=autocomplete only; left/right agent switching
│   ├── update.go            # cycleAgent ± direction variants
│   ├── model.go             # commands table: /errors + /permissions rows removed; /logout renamed
│   ├── slash.go             # /errors + /permissions cases removed; reasoning strings cleaned
│   ├── render_header.go     # mode line: hints, permission chip always-on, hint sub-line
│   ├── render_todos.go      # toggle branch removed; visible while busy with items
│   └── format.go            # taskSummaryLine friction marker removed; formatHarnessEvents deleted;
│                            # formatContextReport → compact card; FullTokens call sites
└── app/
    └── app.go               # AssemblePrompt skill-wrapper format unchanged (dedupe marker source)

docs/                        # security.md review; agent-design.md + architecture.md + README updates
```

**Structure Decision**: Single Go project; every change lands inside existing packages along existing seams. One new file (`internal/orchestrator/skills_tool.go`) hosts the `read_skill` tool; everything else is in-place extension, honoring Constitution VIII.

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

No constitutional violations. One noteworthy non-violation, recorded for review transparency:

| Event | Why Needed | Simpler Alternative Rejected Because |
|-------|------------|--------------------------------------|
| One-time upgrade cache epoch (toolset + prefix bytes change on first run of the new binary) | `read_skill` tool + `run_subagent.skills` field + all-roots catalog + stronger hint all change session-stable bytes | Shipping skill auto-use without a tool would require widening file-tool containment to home roots (weakens the security model, Constitution IX) or keyword auto-injection by the harness (violates FR-003 and the standing model-driven-dispatch directive). Upgrade epochs are the established, sidecar-attributed pattern (feature g4 precedent). Within-session determinism is fully preserved. |
