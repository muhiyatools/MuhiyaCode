# Specification Quality Checklist: Harness Reliability & Clarity Overhaul

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-07-14
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

## Notes

- Validated 2026-07-14 applying the failure modes caught by feature 007's
  adversarial review pass as a rubric:
  - No hedged thresholds: SC-001 (≥2 subagent runs, ≥25% smaller main
    conversation), SC-002 (≤+10% billed tokens, no steady-state regression),
    SC-006 (±1 point rounding) are exact and decidable.
  - Current-behavior claims are evidenced: the 32-request/0-subagent max-effort
    session and the single flash→pro invalidation come from the user's real
    session report (screenshot + /context output, 2026-07-14).
  - Cross-consistency: every FR maps to a story (FR-001–007→US1, 008–010→US2,
    011–013→US3, 014–017→US4); every SC is backed by FRs; the no-token-ceiling
    guarantee (FR-005) encodes the freshly-removed cap staying removed.
  - Domain terms that name the product's own surfaces (/model, /context, prompt
    prefix, cache epoch) are the feature's subject matter, consistent with the
    constitution's vocabulary, and were judged acceptable under the same rule as
    feature 007.
  - Explicit Out of Scope section bounds gateway work, token ceilings, effort
    numbers, new subagent kinds, and verbatim UI copying.
- Zero [NEEDS CLARIFICATION] markers: definitional choices (billed-token figure,
  API vs active time, lines +/− semantics, per-model cost honesty) are resolved
  as documented defaults in Assumptions.
- Evidence expectations for planning: research phase must inventory the current
  orchestration (delegation prompt text, task classification, subagent budget
  flow, failure terminator) and consult the Reasonix reference per constitution
  Principle VII before prompt changes.
