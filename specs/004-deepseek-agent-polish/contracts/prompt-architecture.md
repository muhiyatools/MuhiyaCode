# Contract: Prompt Architecture

**Feature**: `004-deepseek-agent-polish` | Decisions: [research D4, D10](../research.md) | Findings: B5, B8

Governs the system prompt's structure, size, and the single permitted prefix-byte change of this feature. Requirements FR-018, FR-021; constitution II/III/IV.

## 1. Section order (fixed — reordering is out of contract)

1. Identity line
2. `OPERATING CONTRACT` (numbered rules + **new single priority rule**)
3. `CONTEXT AND EDIT DISCIPLINE`
4. `CACHE DISCIPLINE` (002 — byte-frozen except via this contract's bump procedure)
5. `TOOLS AND RECOVERY`
6. `COMMUNICATION`
7. `SAFETY`
8. `ENVIRONMENT` (workspace/OS/shell/model, web + agents capability lines, model addendum)
9. `SKILLS` (003 — deterministic listing, only when skills exist)

Rationale: B5's position-sensitivity evidence concerns system-vs-user weighting, which MuhiyaCode already exploits via per-turn riders; no T1 evidence supports intra-prompt reordering, so order churn is rejected (X).

## 2. The tightening pass (exact scope)

| Edit | Section(s) | Direction |
|---|---|---|
| Add one priority rule: "When rules conflict: safety, then the user's explicit request, then this contract, then style." | OPERATING CONTRACT | +1 line |
| Deduplicate verify-then-stop / final-answer phrasing (currently stated in both OPERATING CONTRACT and COMMUNICATION) | OPERATING CONTRACT ↔ COMMUNICATION | −duplication (single home: OPERATING CONTRACT) |
| Consolidate the re-read prohibition (currently split across CACHE DISCIPLINE and TOOLS AND RECOVERY) | CACHE DISCIPLINE ↔ TOOLS AND RECOVERY | −duplication (single home: CACHE DISCIPLINE) |
| DeepSeek addendum +1 sentence: "Report only work actually performed; if steps remain, say so." | ENVIRONMENT (model addendum, `gateway/model.go`) | +1 sentence, DeepSeek-family only |
| Tool-description quality pass (D10b): each description leads with when-to-use / when-not / key argument | tool schema block (not the prompt text; same prefix region) | reworded, no schema shape changes |

**Size budget**: net estimated tokens of the composed system prompt ≤ the pre-change value (~1,900 est.), asserted by a test that composes both fixture prompts and compares `EstimateTokens` — growth fails the build. Ambiguity/conflict removal is reviewed against the FR-018 checklist in code review.

## 3. Placement rules (restate of constitution III/IV as testable invariants)

- Prefix carries only session-invariant content; identical config ⇒ byte-identical prompt across sessions (`restart_determinism_test.go`).
- Dynamic content (task class, budgets, plan/goal state, capability changes, recovery nudges) rides the newest user message or request params — **never** the prefix. New rider classes must not exceed 002 SC-004's ≤~50-token system-added tail on plain follow-up turns.
- No per-mode or per-request prompt variants; the model addendum varies only by session-static model family.
- Subagent capability statements are mission-message content ([mode-capability §1](mode-capability.md)), not prefix.

## 4. Bump procedure (the one permitted prefix-byte change)

1. All §2 edits land in **one commit** (prompt text + addendum + tool descriptions) — exactly one cache-invalidation event for the feature.
2. Same commit updates every stability fixture: `prompt_stability_test.go`, `restart_determinism_test.go`, `cachehit_guard_test.go` (and the size-budget test of §2).
3. The commit message records the II justification: what moved/was deduplicated, why quality is preserved (references D4).
4. D9's after-benchmark runs against this commit; hit-rate ≥ baseline and scripted-suite green are merge gates ([quickstart §4](../quickstart.md)).
5. In-flight sessions: old sessions keep their prompt until restart (per-session byte stability is never violated); new sessions prime the new prefix.

## 5. Test obligations

Size-budget comparison test; fixture updates in-commit (a bump without fixture updates must fail CI); no-date/no-randomness assertions stay green; MCP-order determinism unchanged; addendum change covered by a family-resolution test (DeepSeek gets the sentence, generic does not).
