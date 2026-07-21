# Specification Quality Checklist: Automatic Skill Use & Interface Polish

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-07-20
**Feature**: [spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs)
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain
- [x] Requirements are testable and unambiguous
- [x] Success criteria are measurable
- [x] Success criteria are technology-agnostic (no implementation details)
- [x] All acceptance scenarios are defined
- [x] Edge cases are identified
- [x] Scope is clearly bounded
- [x] Dependencies and assumptions identified

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification

## Post-Implementation Re-Validation (2026-07-20)

Re-checked against the delivered code, not just the spec. All 16 items still pass.

| Requirement group | Delivered | Verified by |
|---|---|---|
| FR-001..FR-011 (auto skills) | all-roots catalog, `read_skill`, rewritten prefix hint, `run_subagent.skills` | `skills_tool_test.go`, `skills_subagent_test.go`, `skills_scope_test.go`, `skills_prompt_test.go`, end-to-end smoke |
| FR-012..FR-018 (command surface) | `/logout` renamed, `/errors` + friction UI removed, `/permissions`+`/mode` removed, always-on mode chip + hint, DeepSeek text gone | `slash_alias_test.go`, `footer_test.go`, palette goldens, orphan sweep |
| FR-019..FR-024 (token/cache truth) | `FullTokens` everywhere, cache-inclusive headline, `AllStreamHitRate`, essentials card | `fulltokens_test.go`, `allstream_rate_test.go`, `cross_surface_test.go`, `context_panel_test.go` |
| FR-025..FR-029 (interaction) | Ctrl+T gone, todos permanent, ←/→ ring, Tab autocomplete-only, hints updated | `render_todos_test.go`, `keys_test.go`, `footer_test.go` |
| FR-030 (integration) | wiring inventory, docs, security note all updated in the same change | `wiring_inventory_test.go`, README/agent-design/prompt-caching/security |

**Deviation from the plan worth noting**: `read_skill` loads bodies through an
injected `SkillLoader` rather than importing `internal/workspace` directly — the
architecture layering guard (`internal/arch`) forbids that edge. This follows the
existing `EngineConfig` dependency-injection pattern (`Rescue`, `Redact`) and
keeps the 32 KiB bound in one place (`command.SkillBodyLimit`).

**Success criteria status**: SC-005, SC-006, SC-007, SC-008, SC-010 are verified
offline. SC-001, SC-002, SC-003, SC-004, SC-009, SC-011 need the live/interactive
runs recorded as deferred in [bench-notes.md](../bench-notes.md).

## Notes

- Validation performed 2026-07-20 against the full checklist; all items pass on the first iteration.
- Zero [NEEDS CLARIFICATION] markers were needed: every open point had a reasonable default, recorded in the spec's Assumptions section (model-driven selection, manual-flow precedence, all-folder scope, always-visible permission mode as the hint's anchor, alias removal with `/permissions`, bounded meaning of "polish", to-do "active" definition, arrow-key conflict resolution deferred to design with a hard no-regression constraint, internal-diagnostics removal deferred to design, cache-discipline compliance, existing per-skill size bound retained).
- Command names (`/errors`, `/permissions`), shortcut names (Shift+Tab, Ctrl+T, arrows), and the "DeepSeek maps to" wording appear in requirements because they are user-visible product surfaces being renamed or removed — they are the subject of the requirements, not implementation choices.
- Constitution alignment noted for planning: FR-011/SC-003/SC-011 carry Principles II, VI, and X (cache efficiency without quality loss, honest measurement, verified improvements) into this feature; the skill-catalog presentation must respect Principles III–IV (deterministic stable prefix, dynamic/cached separation). FR-021 (session-wide, sub-agent-inclusive hit rate) implements Principle VI's whole-picture reporting.
