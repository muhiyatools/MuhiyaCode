# Removal Ledger

Feature 010 US4 (T034-T038; MS-8/MS-9/MS-10/MS-11). Every row: Symbol,
Location, Class (`dead` / `test-only` / `write-only-field` / `injected-unread`
/ `duplicate-merged`), Proof (zero-reference command + deadcode line),
Migration note (mandatory when Class ≠ `dead`), Verification (green-suite
command). Commit rule: proof-empty-output + deadcode-clean, OR migration
note + green suite — never neither.

**Final T038 tally**: `deadcode -test ./...` = 0 (nothing anywhere is
genuinely dead, including test-only code). `deadcode ./...` (no `-test`) went
34 → 4; the remaining 4 are cross-package test-fixture exports that Go's
package-visibility rules make impossible to relocate into a `_test.go` file
without breaking another package's test build — see the "Structurally
irreducible" table under T035 for the full reasoning on each.

## T034 — truly-dead functions (Class = dead)

These 6 functions were verified in research.md R3 as zero-reference by BOTH
`deadcode ./...` (which reports unreachable funcs even with no `-test`
build tag) and a direct repo-wide grep for every caller-shaped reference
(method-call form, qualified-name form, and the bare declaration itself).
Each was deleted outright — no migration needed, nothing depended on it.

| ID | Symbol | Location (pre-removal) | Proof | Verification |
|---|---|---|---|---|
| RL-001 | `Application.Paths` | `internal/command/runtime_build.go:194` | `grep -rn '\.Paths\(\)\|func (a \*Application) Paths' --include=*.go` → only the declaration line; `deadcode ./...` line `internal\command\runtime_build.go:194:23: unreachable func: Application.Paths` | `go build ./... && go vet ./... && go test ./... -count=1` green post-delete |
| RL-002 | `contract.CreditsToUSD` | `internal/contract/credits.go:16` | `grep -rn 'CreditsToUSD' --include=*.go` → only the declaration; `deadcode` line `internal\contract\credits.go:16:6: unreachable func: CreditsToUSD` (sibling `USDToCredits` IS used and stays) | green suite post-delete |
| RL-003 | `Manager.uniqueName` | `internal/mcpclient/schema.go:10` (post-T032 split; pre-split `manager.go:725`) | `grep -rn 'uniqueName' --include=*.go` → only the declaration; `deadcode` line `internal\mcpclient\schema.go:10:19: unreachable func: Manager.uniqueName` (the live dedup path is `deterministicToolName`, a distinct sibling function) | green suite post-delete |
| RL-004 | `orchestrator.assembleRequest` | `internal/orchestrator/history.go:506` | `grep -rn 'assembleRequest\b' --include=*.go` → only its own declaration and its internal call to the *different*, still-live `assembleRequestWithStart`; `deadcode` line `internal\orchestrator\history.go:506:6: unreachable func: assembleRequest` — `assembleRequestWithStart` (the real workhorse, called from `BuildRequestWithMetadata` at history.go:180) is NOT touched | green suite post-delete |
| RL-005 | `state.CheckpointPath` | `internal/state/paths.go:96` | `grep -rn 'CheckpointPath' --include=*.go` → only the declaration; `deadcode` line `internal\state\paths.go:96:6: unreachable func: CheckpointPath` — this is the free helper, NOT the live checkpoint feature (`Sessions` checkpoint methods elsewhere are unaffected and were never in scope) | green suite post-delete |
| RL-006 | `Workspace.Root` | `internal/workspace/types.go:256` | `grep -rn '\.Root\(\)\|func (w \*Workspace) Root' --include=*.go` → only the declaration; `deadcode` line `internal\workspace\types.go:256:21: unreachable func: Workspace.Root` | green suite post-delete |

All six confirmed via a fresh `GOFLAGS=-mod=mod go run golang.org/x/tools/cmd/deadcode@latest ./...`
run before deletion (34 total findings, matching research.md's count) and a
full `go build ./... && go vet ./... && go test ./... -count=1` green sweep
immediately after deletion — no callers existed anywhere in production code,
tests, or benchmarks.

### Post-research additions (found via a fresh `deadcode -test` run)

research.md's R3 inventory predates the completed US3 (T024-T029) instruction-
registry work. A `deadcode -test ./...` run after T034's six deletions surfaced
two more functions dead in BOTH invocations (i.e. `Class = dead`, not
`test-only` — nothing, not even a test, calls them), so they are handled here
rather than deferred to T035.

| ID | Symbol | Location (pre-removal) | Proof | Verification |
|---|---|---|---|---|
| RL-007 | `instructions.ByAudience` | `internal/instructions/types.go:154` | `grep -rn 'ByAudience' --include=*.go` → only the declaration; `deadcode -test ./...` line `internal\instructions\types.go:154:6: unreachable func: ByAudience` — sibling `ByCache` IS used (by `audit_test.go`) and stays | green suite post-delete |
| RL-008 | `instructions.SortedIDs` | `internal/instructions/types.go:178` | `grep -rn 'SortedIDs' --include=*.go` → only the declaration; `deadcode -test ./...` line `internal\instructions\types.go:178:6: unreachable func: SortedIDs` — its doc comment claimed "used by the golden dump" but `dump_test.go` actually does its own inline `sort.Strings(tools)` and never calls this helper; deleting it also dropped the now-unused `"sort"` import from `types.go` | green suite post-delete; `go vet` clean (no unused-import warning) |

## T035 — test-only survivors, resolved WITH their tests

`deadcode ./...` (no `-test`) findings that survive `deadcode -test ./...`
(i.e. only a test reaches them) fall into two groups: (1) genuine orphans
that should be deleted together with the test that exercised them, and
(2) deliberate test-support surface (fakes, round-trip verifications, data-
table checks) that legitimately has no production caller and is kept, same
class as the explicitly-blessed `Manager.connectionForTest` shim.

### Deleted together with their tests

| ID | Symbol | Location (pre-removal) | Class | Migration note | Verification |
|---|---|---|---|---|---|
| RL-009 | `tui.TranscriptWindow` (+ 16 methods: `Ingest`, `AppendLive`, `Reconcile`, `Entries`, `HasOlder`, `HasNewer`, `Len`, `RawBytes`, `Anchor`, `SetAnchor`, `oldestID`, `newestID`, `upsert`, `resortFrom`, `evict`, `NewTranscriptWindow`), plus the co-located `ViewportAnchor` type | `internal/tui/transcript.go` (whole file, 151 lines) | test-only → deleted | Zero production callers anywhere (`grep -rn TranscriptWindow --include=*.go .` matched only `transcript.go` and `transcript_test.go`); paging in the live TUI goes through `paging.go`'s `applyPage`/`applyOlderPage`/`loadInitialPageCmd` instead — this type was a superseded presentation-cache design (005 US1) never wired to the current model. Deleted the whole file (`transcript.go`) and its whole test (`transcript_test.go`, 138 lines) together — no partial content in either was reachable independently. | `go build ./... && go vet ./... && go test ./... -count=1` green post-delete |
| RL-010 | `gateway.ParseOpenAIStream`, `gateway.NewStreamAccumulator` | `internal/gateway/sse.go:59,200` | test-only → deleted (decode logic migrated) | These were a parallel "decode a whole SSE payload in one call" convenience wrapper around the SAME `StreamAccumulator.ConsumeLine` the live streaming provider path uses (`provider.go:190` calls the sibling `NewStreamAccumulatorForProfile`, which `ConsumeLine` also backs) — a duplicate entry point, not a duplicate implementation. The 3 test call sites (`gateway_test.go:24` `TestSSEAccumulator`; `sse_usage_test.go:234` `TestUsageParsingMergesPartialUsageWithProviderPrecedence`; `sse_usage_test.go:285` the `parseUsageGolden` helper, itself used by `TestUsageParsingGoldenProviderShapes` and `TestUsageParsingPreservesLargeIntegersAcrossSSEJSON`) now call a new test-local helper `decodeSSEStream(text, profile)` (`sse_usage_test.go`) that does the identical line-split + `ConsumeLine` loop via the live `NewStreamAccumulatorForProfile` constructor — same JSON fixtures, same assertions, zero test-intent change. | `go test ./internal/gateway/... -run 'TestSSEAccumulator|TestUsageParsing|TestMiniMaxUsage' -v` and the full suite green |

### Relocated to their only caller's test file (T038 — achieves literal deadcode-zero)

`deadcode ./...` (without `-test`) cannot see `_test.go` files at all — they are
excluded from that build the same way `go build` excludes them. A function
whose ONLY callers are tests IN THE SAME PACKAGE therefore has an exact,
zero-cost fix: move its declaration into a `_test.go` file. It stops being
"unreachable production code" and becomes what it always was — test support —
with byte-identical behavior and zero risk, verified by the full suite passing
unchanged after each move.

| ID | Symbol | Old location → new location | Note | Verification |
|---|---|---|---|---|
| RL-011 | `mcpclient.Manager.connectionForTest` | `manager.go:135` → `manager_test.go` | Explicitly named in the feature's own executor notes as a deliberate `*ForTest` shim. Its only callers (`manager_test.go:261,278,378,383`) are in the same package/file already; moving the declaration there was a pure cut-paste, no rewrite. | `go test ./internal/mcpclient/... -count=1` green |
| RL-013 | `state.Sessions.PrunedRecords`, `state.Sessions.Transcript` | `session.go:98,319` → `state_test.go` | The read-half of a persisted write/read pair; the write half (`AppendPruned`) stays in `session.go`, wired in production (`command/runtime_build.go:312`). Only caller of both reads is `state_test.go`'s round-trip proof — moved verbatim, `state_test.go` gained the `"bufio"` import `Transcript` needs. | `go test ./internal/state/... -count=1` green |
| RL-014 | `gateway.ModelProfile.IsDeprecatedParam` | `model.go:141` → `model_capability_test.go` | Verifies contract CP-2 against the `DeprecatedParams` data table; `DeprecatedParams` itself stays live (rendered in `tui/format.go:462`'s capability panel) — the "never sent" guarantee is structural elsewhere, so this predicate only ever existed to check the table, and only `TestDeprecatedParamsNeverSupported` calls it. | `go test ./internal/gateway/... -run TestDeprecatedParamsNeverSupported -v` green |
| RL-015 | `workspace.ProjectInstructions.Loaded` | `project_context.go:66` → `memory_file_test.go` | Only caller is `TestLoadProjectMemoryFile`'s `mem.Loaded()`; production call sites (`command/runtime_build.go:450,522`) consume `.ContentHash`/`.CanonicalContent`/`.State` directly and never needed the coarse boolean. | `go test ./internal/workspace/... -run TestLoadProjectMemoryFile -v` green |
| RL-023 | `instructions.ByID`, `instructions.ByCache`, `instructions.AllExamples` | `types.go`/`examples.go` → `audit_test.go` | Only callers are `audit_test.go`/`dump_test.go` (same package). `All` — which `ByCache` calls internally — stays in `types.go` (see RL-025, cross-package). | `go test ./internal/instructions/... -count=1` green |
| RL-024 | `instructions.ExampleByID` → `instructions.exampleByID` (renamed, unexported) | `examples.go:35` → `audit_test.go` | Could not move under its original exported name: Go's test tooling treats any exported `ExampleXxx` func placed in a `_test.go` file as a runnable doc-example, requiring a niladic, no-return signature — `go vet` rejected this one's real `(id string) (Example, bool)` signature there. Its only caller (`audit_test.go:412`, `TestExampleReferencesResolve`) was updated to the lowercase name in the same commit; it was never genuine public API (zero external callers, confirmed by grep before the rename). | `go vet ./...` clean (no more `should be niladic` warning); `go test ./internal/instructions/... -count=1` green |

### Structurally irreducible (kept in production, Class = test-only)

These 4 CANNOT be relocated into a `_test.go` file: each has at least one
caller in a DIFFERENT package's test file, and Go's package-visibility rules
mean an external package (even its own test files) can only ever see a
package's REGULAR (non-test) build — never another package's `_test.go`
content. Moving them would break the other package's test build. `deadcode
./...` (without `-test`) will therefore always list these as "unreachable"
by construction, for as long as they exist purely to support other packages'
tests — this is the exact same shape as the explicitly-blessed
`connectionForTest` pattern, just crossing a package boundary instead of
staying within one.

| ID | Symbol | Location | Cross-package consumer forcing production placement | Alternative considered and rejected |
|---|---|---|---|---|
| RL-025 | `instructions.All` | `internal/instructions/types.go:133` | `internal/orchestrator/instructions_wiring_test.go:66` (`instructions.All()`) | Duplicating `All`'s registry-walk logic inline in the orchestrator test would reintroduce exactly the kind of divergent-copy problem T036 eliminated elsewhere — worse than one intentionally-shared exported reader. |
| RL-012 | `workspace.NewMemoryTrustStore`, `MemoryTrustStore.IsTrusted`, `MemoryTrustStore.Trust` | `internal/workspace/permissions.go:239,243,250` | `internal/orchestrator/faultinjection_test.go:198` and `internal/orchestrator/instructions_wiring_test.go:41,163` (`workspace.NewMemoryTrustStore()`) — both are mandatory US1/US2/US3 deliverables of THIS feature (the recovery-invariant safety net and the instruction-wiring cross-check), not incidental tests | Rewriting those tests to build a real `state.DB`-backed trust store instead of the in-memory fake would add real SQLite setup cost to tests that deliberately want a fast, isolated fixture — a regression in test quality for zero dead-code benefit. The real implementation (`internal/state.DB`) is already wired in production; this fake exists ONLY so tests don't need it. |

Net result: `deadcode ./...` (no `-test`) went from the original 34 findings
(research.md baseline) to 4 — all four cross-package test-fixture exports
whose only alternative is deleting real test coverage or duplicating logic.
`deadcode -test ./...` is 0, unchanged since T034/the post-research additions —
nothing in the entire module, including every test, is unreachable.

## T036 — duplicate-mission merges (Class = duplicate-merged)

| ID | Cluster | Survivor | Merged/removed | Verification |
|---|---|---|---|---|
| RL-016 | Token formatter: `orchestrator.humanTokens` ≡ `tui.formatTokens` (byte-identical switch bodies) | `contract.HumanTokens` (new, `internal/contract/format.go`) | Both original functions deleted; `orchestrator/maintenance.go`'s `describeFreed` and all 14 `tui` call sites (`format.go`, `slash.go`, `render_tool.go`, `render_header.go`) now call `contract.HumanTokens` — one foundation-layer implementation both packages import (contract is the common ancestor of orchestrator and tui per the layering allow-map) | `go build ./... && go test ./... -count=1` green |
| RL-017 | Char-truncation primitives: `orchestrator.truncateEllipsis`+`digest`, `orchestrator.truncate`, `orchestrator.oneLineGoal`, byte-unsafe `workspace.truncateLine` (sliced `value[:limit]` by BYTE offset — could split a multi-byte UTF-8 rune) | `contract.TruncateEllipsis` (primitive) + `contract.Digest` (whitespace-collapse wrapper), both new in `internal/contract/format.go` | `truncateEllipsis`/`digest` deleted from `orchestrator/knowledge.go`; `truncate` deleted from `orchestrator/history.go`; `oneLineGoal` deleted from `orchestrator/goal.go`; `truncateLine` deleted from `workspace/files.go`. Every call site across `knowledge.go`, `maintenance.go`, `planrender.go`, `subagent.go`, `toolhandlers.go`, `turnhelpers.go`, `history.go`, `goal.go`, `engine.go`, and `workspace/files.go` now calls `contract.TruncateEllipsis`/`contract.Digest`. Behavior note: `truncate`/`oneLineGoal`'s callers previously got a silent hard cut (no truncation marker); they now get `contract.TruncateEllipsis`'s "…" suffix when truncated — a deliberate, sanctioned improvement (consistent truncation signal everywhere) confirmed safe by grep: no test anywhere pins the exact truncated goal-notice/fold-headline text or length. `truncateLine`'s byte-based cut is now rune-safe, fixing a latent multi-byte-UTF-8 corruption bug for non-ASCII anchors/search lines as a byproduct — output is byte-identical for the common ASCII case every existing test exercises | `go test ./internal/orchestrator/... ./internal/workspace/... ./internal/gateway/... -count=1` and the full suite green |
| RL-018 | Display-width engines: uniseg (`tui/width.go`) vs go-runewidth (`tui.truncateMiddle`/`truncateLeft` in `format.go`, `tui.cellToRuneColumn` in `mouse.go`, two width checks in `tui_test.go`) vs x/ansi (`tui.fitLine`, unrelated — ANSI-escape-aware measurement for styled/colored strings, a genuinely different requirement uniseg's plain `StringWidth` cannot satisfy, so `fitLine` and the `x/ansi` uses in `hittest.go`/`selection.go` are NOT part of this cluster and were left untouched) | the uniseg family (`displayWidth`, new shared helper `graphemeClusters` in `width.go`) | `truncateMiddle`/`truncateLeft` rewritten to iterate grapheme clusters (`graphemeClusters`) instead of runes via `go-runewidth.RuneWidth`; `cellToRuneColumn` rewritten the same way; `tui_test.go`'s `ansiWidth`/the wrapped-line-width check switched from `runewidth.StringWidth` to `displayWidth`. Restores `width.go`'s own stated invariant ("All width math funnels through uniseg... correct... in every case", `width.go:9-14`), which go-runewidth usage elsewhere was silently violating (it over-measures Arabic combining marks — the exact bug uniseg was introduced to fix in feature 006). `go mod tidy` demoted `github.com/mattn/go-runewidth` from a direct to an `// indirect` requirement in `go.mod` — no `tui` source file imports it anymore | `go build ./... && go vet ./... && go test ./... -count=1` green; `grep -rn 'go-runewidth\|runewidth\.' internal/tui/*.go` shows only doc-comment mentions, zero live imports |
| RL-019 | Two dead aligners: `tui.padToWidth` (`width.go`), `tui.DisplayLine.AlignedTo` (`bidi.go`) | — (deleted, no survivor) | Confirmed genuinely unused, not merely misplaced: `DisplayLine.Align` (the field) IS read in production (`composer.go:65`, `render.go:205` pick a prefix side by it), but nothing ever calls `AlignedTo`/`padToWidth` to actually pad text for alignment — right-alignment is achieved some other way in the live renderer. Their only callers were smoke-test invocations in `rtl_fuzz_test.go`'s `TestRTLRobustness`/`FuzzRenderForDisplay` ("never panics on adversarial input" sweep) that discarded the result (`_ = dl.AlignedTo(40)`). Deleted both functions and removed the two now-pointless calls from `rtl_fuzz_test.go`; the fuzz test's other panic-safety assertions (`displayWidth`, `truncateToWidth`, `recoverLogical`, `RenderMarkdown`, etc.) are untouched | `go test ./internal/tui/... -run 'TestRTLRobustness|TestRTLLiveSettingChange' -v` and `go test -fuzz=FuzzRenderForDisplay -fuzztime=5s ./internal/tui/` both green; full suite green |

## T037 — write-only / injected-unread state

| ID | Symbol | Location | Class | Proof / migration note | Verification |
|---|---|---|---|---|---|
| RL-020 | `contract.AgentEvent.CallID` | `internal/contract/types.go` (field, `AgentEvent` struct) | write-only-field → removed | Written at `internal/orchestrator/subagent.go:257,260` (the `tool_start`/`tool_end` events); read by neither production consumer: `internal/tui/ingest.go`'s `applyAgent` switches on `event.Kind`/`.Tool`/`.Arguments`/`.Output`/`.Content`/`.Status`/`.Report`/`.Usage`/`.RunID` and never touches `.CallID`, and `benchmarks/delegationbench/audit.go`'s `observeAgent` reads `.Kind`/`.RunID`/`.Phase`/`.Role`/`.Model`/`.Handoff`/`.Status`/`.Tool`/`.Arguments` — also never `.CallID`. Field deleted from the struct; the two write sites' `CallID: call.ID` literals removed. (`InspectionEntry.CallID` is a DIFFERENT, live-read field on a different type — not touched.) | `go build ./...` (would fail on any stray reference) + full suite green |
| RL-021 | `contract.AgentEvent.Turns`, `contract.AgentEvent.ToolCalls` | `internal/contract/types.go` (fields, `AgentEvent` struct) | write-only-field → removed | Written once, at `subagent.go:393` (the "done" event: `Turns: result.Turns, ToolCalls: result.ToolCalls`); same two consumers checked as RL-020 — neither reads `.Turns`/`.ToolCalls` off an `AgentEvent` (the many other `.Turns`/`.ToolCalls` hits across the codebase are `contract.TaskStats`, `contract.TaskBudget`, or benchmark-local `runResult`/`phaseAgentEvent` fields — different types entirely, grep-verified by type context, not touched). Both fields deleted from the struct; the write site's two key-value pairs removed. `.Handoff` on the same struct IS kept — it has a real reader (`audit.go:133`, `handoffCompliant(event.Handoff)`), the canonical false-positive the contract warns about | `go build ./...` + full suite green |
| RL-022 | `buildinfo.Commit`, `buildinfo.Date` | `internal/buildinfo/buildinfo.go` | injected-unread → wired (not removed) | ldflags-set at release time (`.goreleaser.yaml:16-17`: `-X .../buildinfo.Commit={{.FullCommit}}`, `-X .../buildinfo.Date={{.Date}}`) but had zero readers anywhere in the source (`buildinfo.Version` WAS read, at `command/root.go:41,98`; `.Commit`/`.Date` were not). Chose "wire" over "remove with ldflags lines" because the release pipeline already computes and injects real values — deleting them would throw away working, already-wired release metadata for no benefit. Wired into `command/root.go`'s `--version` output: `root.SetVersionTemplate(fmt.Sprintf("MuhiyaCode {{.Version}} (commit %s, built %s)\n", buildinfo.Commit, buildinfo.Date))`. Verified end-to-end: a plain dev build prints `MuhiyaCode 1.0.2 (commit unknown, built unknown)`; a build with `-ldflags "-X .../buildinfo.Commit=abc1234 -X .../buildinfo.Date=2026-07-15T00:00:00Z"` prints `MuhiyaCode 1.0.2 (commit abc1234, built 2026-07-15T00:00:00Z)` | `go build -o muhiyacode ./cmd/muhiyacode && ./muhiyacode --version` manually verified both plain and ldflags-injected forms; full suite green |

## T041 — US5 ghost resolution (WI-5)

| ID | Symbol | Location | Class | Proof / migration note | Verification |
|---|---|---|---|---|---|
| RL-026 | `contract.Settings.UI.Density` | `internal/contract/types.go` (field, `Settings.UI` struct) | write-only-field → removed | research.md's R5 reconciliation flagged this as plumbed-but-effect-free: `state.DefaultSettings` set a default (`"auto"`), `state.ValidateSettings` constrained it to `{auto,compact,comfortable}`, and `state.SetConfig`'s `"uiDensity"` case wrote it — but grep across `internal/tui` found zero renderer consumers (contrast `Settings.UI.BorderMode`, which `tui/theme.go:31`'s `newTheme` reads into `asciiGlyphs`). Spec 003's "Density" concept (`specs/003-tui-ux-overhaul/contracts/visual-system.md` §3) turned out to be a fixed, hardcoded rendering decision (transcript padded, tool lines packed) never wired to this setting at all — there was no existing behavior to preserve. Chose removal over wiring a new effect per research.md's explicit guidance ("Choose removal if there's no clean rendering effect (simpler, honest)"): a real density mode would need to touch transcript/modal spacing across `internal/tui/render_transcript.go`/`render_modal.go`, none of which any test currently parameterizes, so wiring one now would be new scope invented to save a field, not a preserved behavior. Removed: the `Density` field from `contract.Settings.UI`; `state.DefaultSettings`'s `settings.UI.Density = "auto"` line; the density arm of `ValidateSettings`'s `ui settings are invalid` check; the `"uiDensity"` case in `state.SetConfig`. `internal/state/state_test.go`'s `TestLegacySettingsAndSecrets` legacy JSON blob still contains `"density":"auto"` — left as-is deliberately: it proves a settings.json written by a pre-010 build (with the now-unknown field) still loads cleanly (Go's `encoding/json` silently ignores unknown fields), which IS the compat behavior old sidecars need | `go build ./... && go vet ./... && go test ./... -count=1` green; `internal/tui/wiring_inventory_test.go`'s reflection-based Settings-field enumeration no longer contains `ui.density`, matching its removed status in `specs/010-ultimate-consolidation/wiring-inventory-data.md` |
