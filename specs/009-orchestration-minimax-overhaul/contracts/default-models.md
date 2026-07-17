# Contract: Fresh-Install Default Models

Feature 009 (US4/FR-022; clarification #3). The first-run model pairing. Evidence:
[research.md](../research.md) R7 (and the AutoAssign gotcha it found).

## 1. Rules

| ID | Requirement |
|---|---|
| DM-1 | On first run (no `ActiveModelID`/`SubagentModelID` configured), when discovery reports BOTH a MiniMax M3 model AND a DeepSeek V4 Pro model, the defaults MUST be set explicitly: `ActiveModelID` = MiniMax-M3, `SubagentModelID` = DeepSeek V4 Pro |
| DM-2 | This MUST be an explicit pair rule, NOT reliance on `AutoAssignModels` score heuristics — verified: `V4Pro` matches the `"pro"` main-preference substring and M3's 1M context scores highest, so the score rules would NOT deterministically produce M3-main/V4Pro-sub |
| DM-3 | When MiniMax is NOT available through the gateway, defaults MUST fall back to today's `AutoAssignModels` DeepSeek result, byte-unchanged (graceful fallback) |
| DM-4 | An already-configured model setting (user-pinned, or set by a prior session) MUST NEVER be altered by an upgrade or by this rule (existing user-pin guard) |
| DM-5 | The rule applies only to the discovery/assign path (addDiscoveredModels → AutoAssignModels); it adds a targeted pre-step gated on the specific model pair and otherwise defers to the unchanged score-based assignment for all other deployments |

## 2. Acceptance (maps to spec)

- FR-022: a fresh install with both providers discovered → M3 main + V4Pro sub;
  a fresh install with only DeepSeek → today's DeepSeek defaults unchanged; an
  existing configured install → settings untouched after upgrade.
- Unit tests (extend `config_defaults_test.go`): both-providers fresh → the pair;
  DeepSeek-only fresh → unchanged AutoAssign result; pre-pinned roles → untouched;
  MiniMax-only (no V4Pro) → falls back to score-based (documented behavior).
