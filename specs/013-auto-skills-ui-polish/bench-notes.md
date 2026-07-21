# Bench Notes: 013-auto-skills-ui-polish

## T001 — Green baseline (2026-07-20)

Branch point / merge-base commit: **bb8df5a** (`fix(security): a newline no longer launders a destructive command`)

```text
go build ./...         → OK
go test ./... -count=1 → all 14 packages ok
```

## Offline verification (implementation)

Full suite green after every phase. Final run: `go fmt` clean, `go vet` clean,
`go test ./... -count=1` green across all 14 packages.

### New/changed test coverage

| Area | File | What it pins |
|---|---|---|
| Discovery | `internal/workspace/skills_scope_test.go` | all-roots span, workspace-shadows-global precedence, malformed-skip, 40-bound, root order |
| `read_skill` | `internal/orchestrator/skills_tool_test.go` | catalog hit, case-insensitivity, unknown name is path-free, manual-marker dedupe, repeat-call dedupe, per-task reset, oversize refusal, definition gating |
| Sub-agent equipping | `internal/orchestrator/skills_subagent_test.go` | brief carries blocks in order, unknown degrades to a notice, continuation does not re-send, no sub-agent gets `read_skill`, schema carries `skills` |
| Prefix stability (constitutional gate) | `internal/orchestrator/skills_prefix_stability_test.go` | all-roots prompt byte-stable, `read_skill` schema byte-stable, toolset stable across turns, **upgrade epoch attributed not silent**, no epoch within a session, catalog frozen |
| Prompt section | `internal/orchestrator/skills_prompt_test.go` | path-free listing, hint teaches read-before-work via `read_skill` |
| Token format | `internal/contract/fulltokens_test.go` | grouping boundaries 0/999/1000/999999/1e6/2^31-1, negatives, not-abbreviated |
| All-stream rate | `internal/contract/allstream_rate_test.go` | **counts sub-agents+aux (SC-006)**, ignores one-sided records, unavailable states, matches main-only for single-stream sessions |
| Cross-surface agreement | `internal/tui/cross_surface_test.go` | live headline == summary figure, full digits, no friction marker, footer rate == card rate |
| Context card | `internal/tui/context_panel_test.go` | group order, full cache-inclusive numbers, **dropped diagnostics**, ≤14 lines / ≤72 cols, cost honesty, unavailable states |
| Keys | `internal/tui/keys_test.go` | →/← ring both directions, **arrows never steal caret while typing**, no-agent no-op, Tab no longer cycles |
| Footer | `internal/tui/footer_test.go` | per-line 2-col margin, mode always shown, hint line present, auto-accept distinct |
| To-dos | `internal/tui/render_todos_test.go` | visible for the whole active life, ctrl+t cannot hide it |
| Removed commands | `internal/tui/slash_alias_test.go` | `/errors` `/permissions` `/mode` are unknown-command; Shift+Tab still cycles |

### End-to-end smoke (outside unit fixtures)

Ran a throwaway program against real temp directories:

```text
roots discovered: 4
skills found: 2
  - commit-style: House commit style.          (HOME root — previously unreachable)
  - frontend-design: Build distinctive frontends.  (workspace root)
loaded body OK; oversized skill correctly refused: skill huge exceeds 32768 bytes
```

Confirms the headline capability at the file layer: a **home-folder** skill now
reaches the catalog, which is exactly what the old workspace-only discovery
excluded.

### Race detector — NOT RUN (environment)

`go test -race` requires cgo; this host has no C compiler
(`cgo: C compiler "gcc" not found`). Per Constitution IX, race-enabled tests must
pass on CGO-capable runners — **defer to CI or a host with gcc/clang**.

## Guard pass (clean-code-guard) — 6 fixes

Run over the production diff after implementation:

1. `internal/orchestrator/skills_tool.go` — **DRY (duplicated knowledge)**: the
   `<skill name="` wrapper parser existed twice (prompt seeding + transcript
   scanning). Extracted `skillWrapperMarker` + `skillNamesIn`; `subagent.go`'s
   copy is now a 3-line call. The wrapper's shape is encoded once.
2. `internal/orchestrator/skills_tool.go` — **CQS / dead write**: `renderEquippedSkills`
   mutated its `alreadySent` argument although no caller read it back. Removed;
   intra-call dedupe was already covered by the local set.
3. `internal/orchestrator/skills_tool.go` — extracted `equippedSkillSection` so
   `renderEquippedSkills` is a short loop instead of a 35-line function mixing
   iteration, resolution, loading, and formatting.
4. `internal/orchestrator/skills_tool.go` — removed a pointless `_ = ctx`; the
   parameter is now `_ context.Context`.
5. `internal/orchestrator/skills_tool.go` — `fmt.Errorf` with a constant, no-arg
   message → `errors.New`.
6. `internal/orchestrator/skills_tool.go` — documented *why* `Lookup`/`Len` are
   nil-safe (the test suite constructs `&Engine{}` literals) instead of leaving
   them as unexplained defensive guards.

All tests re-run green after the refactor; no observable behavior changed.

### Flagged for the author — not changed

**Pre-existing, adjacent to FR-019.** `headlineTokens`' no-cache fallback returns
`usage.TotalTokens`, and the gateway sets `TotalTokens` **only** when the provider
reports `total_tokens` (it is never derived — `internal/gateway/sse.go:276`). A
provider that reports `prompt_tokens`/`completion_tokens` but omits both
`total_tokens` and all cache fields would therefore display **0 tokens**.

Not changed here because it predates this feature, no current test or provider in
the suite exercises it, and I cannot verify against such a provider offline.
The fix would be small: fall back to `PromptTokens + CompletionTokens` when
`TotalTokens` is 0 and either is available.

## T002 / T014 / T022 (live) / T039 — DEFERRED: owner cost approval

These need live gateway sessions (credits). Implementation does not block on them.

- **T002** (Constitution X baseline): build the pre-feature binary from bb8df5a,
  run the standard multi-turn gauntlet 3× plus 3× session-start timing;
  record steady-state hit rate, token totals, startup times.
- **T014**: quickstart §1 scenarios 1–6 + the two-model style check
  (SC-001 skill-read-before-work ≥90%, SC-002 zero loads when unrelated ≥95%,
  SC-004 no verbatim skill dumps).
- **T022**: the live half of quickstart §2 (a real sub-agent session cross-checked
  against `usage.jsonl` for SC-006 — the offline aggregate test already proves the
  arithmetic).
- **T039**: repeat T002's workload on the feature binary; expect steady-state hit
  rate within variance (SC-011), startup within +5% (SC-003), and zero
  `read_skill` calls on a skills-installed-but-unrelated workload (FR-011).

The **offline** half of the constitutional gates is done and passing: prefix
byte-stability across turns, and the one-time upgrade epoch proven *attributed*
rather than silent (`TestUpgradeEpochIsAttributedNotSilent`).
