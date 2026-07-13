# Quickstart: Validate Terminal Experience and Project Memory

**Feature**: `005-terminal-memory-overhaul`  
**Plan**: [plan.md](plan.md)

Run these gates end-to-end. Contracts are normative; this guide links to them instead of duplicating implementation details.

## Prerequisites

- Go 1.25-compatible toolchain and repository dependencies verified with `go mod verify`.
- A clean feature worktree for improved evidence and a clean recorded baseline worktree/commit.
- Windows Terminal on the reference Windows machine; one representative macOS terminal and one Linux terminal.
- A valid generic OpenAI-compatible test endpoint for live agent/cache trials; keep model, gateway, effort, and workload identical between arms.
- A disposable fixture workspace. `benchmarks/terminalbench` uses the existing `benchmarks/cachebench/fixture` as project content and a temporary `MUHIYA_HOME` for state.
- A CGO-capable CI/host for race tests.

Do not use the current dirty development working tree as a performance baseline.

## Gate 0: measurement-first baseline

Before behavior changes, land/build only the deterministic `benchmarks/terminalbench` harness and fixture generator. Record that harness-only commit and run:

```powershell
go run ./benchmarks/terminalbench -scenario input -messages 10,5000 -events 1000 -runs 30 -width 120 -height 40 -build-label baseline -out specs/005-terminal-memory-overhaul/benchmarks/baseline/input
go run ./benchmarks/terminalbench -scenario resume -messages 5000 -runs 30 -inject-first-key -build-label baseline -out specs/005-terminal-memory-overhaul/benchmarks/baseline/resume
go run ./benchmarks/terminalbench -scenario idle -duration 60s -build-label baseline -out specs/005-terminal-memory-overhaul/benchmarks/baseline/idle
go run ./benchmarks/terminalbench -scenario endurance -duration 60m -target-messages 5000 -sample 5s -build-label baseline -out specs/005-terminal-memory-overhaul/benchmarks/baseline/endurance
```

Expected baseline evidence should reproduce the existing defects (permanent timer/full transcript render, missing mouse/block paste/memory), not be forced green. Preserve raw JSON, logs, fixture hash, machine profile, commit, and dirty=false marker.

Capture the live provider/cache baseline from a clean baseline worktree:

```powershell
go run ./benchmarks/cachebench -scenario all -runs 3 -build-label baseline -out specs/005-terminal-memory-overhaul/benchmarks/cache/baseline
```

## Gate 1: automated quality and contract suites

```powershell
gofmt -l .
go vet ./...
go test ./... -count=1
go test ./internal/tui -run 'Input|Transcript|Stream|Idle|Resize|Mouse|Paste|ApplicationState' -count=1 -v
go test ./internal/state ./internal/command -run 'TranscriptPage|LargeResume|ProjectInstructions|ProjectMemory|SimpleLine' -count=1 -v
go test ./internal/orchestrator -run 'PromptStability|CacheHitGuard|RestartDeterminism|ProjectContext|MemoryTrailer|RequestAssembly' -count=1 -v
go test ./internal/gateway -run 'MarshalDeterminism|RetryReplay' -count=1 -v
```

`gofmt -l .` must print nothing. All suites must pass. On the CGO-capable quality runner:

```powershell
go test -race ./... -count=1
```

The repository's existing Windows/macOS/Linux CI test/build matrix must remain green.

## Gate 2: improved terminalbench

Run identical settings on the same reference machine:

```powershell
go run ./benchmarks/terminalbench -scenario input -messages 10,5000 -events 1000 -runs 30 -width 120 -height 40 -build-label improved -out specs/005-terminal-memory-overhaul/benchmarks/improved/input
go run ./benchmarks/terminalbench -scenario resume -messages 5000 -runs 30 -inject-first-key -build-label improved -out specs/005-terminal-memory-overhaul/benchmarks/improved/resume
go run ./benchmarks/terminalbench -scenario idle -duration 60s -build-label improved -out specs/005-terminal-memory-overhaul/benchmarks/improved/idle
go run ./benchmarks/terminalbench -scenario endurance -duration 60m -target-messages 5000 -sample 5s -build-label improved -out specs/005-terminal-memory-overhaul/benchmarks/improved/endurance
go run ./benchmarks/terminalbench -compare specs/005-terminal-memory-overhaul/benchmarks/baseline specs/005-terminal-memory-overhaul/benchmarks/improved
```

Required verdicts from [terminal-performance.md](contracts/terminal-performance.md):

- Input P95 <=16 ms at 10 and 5,000 events; P95 gap <=2 ms.
- Stream P95 large-history gap <=5 ms.
- Resume input-ready P95 <=2 seconds over 30 trials; first key P95 <=16 ms.
- 60-minute run completes with zero crash, no latency slope, WorkingSet/RSS <=300 MiB, and <=10% final-15-minute growth.
- 60-second unchanged idle averages <=1% of one core, with no application timer/full-frame revision.
- No missing/duplicate events, stale generation application, or completed-row hash violation.

## Scenario 1: physical input and resume

On Windows Terminal, repeat 30 trials with the generated 5,000-event session:

1. Start `muhiyacode resume <fixture-session-id>` with terminal at 120x40.
2. The harness/recorder injects a known key after process start and timestamps receipt plus first captured frame containing it.
3. Type/edit continuously as initial transcript pages hydrate.
4. Verify the draft remains editable; an early Enter waits visibly until runtime-ready and submits once.
5. Switch/resume sessions during a delayed page result and verify no stale entries appear.

Pass the same latency gates as Gate 2. Model-only latency is supporting evidence; the physical terminal trial is the acceptance evidence.

## Scenario 2: streaming, scroll anchor, resize, and flicker

1. Start both 10-event and 5,000-event fixtures.
2. Stream a fixed long Markdown/tool response through the deterministic provider fixture.
3. While streaming, scroll upward; verify viewport stops following and never jumps down until End/follow is invoked.
4. Resize continuously across wide/narrow and below/above 60x20.
5. Expand a large tool entry, stream more content, then collapse it.
6. Record normalized frames and a 60 fps video in Windows Terminal; repeat in one macOS and Linux terminal.

Pass:

- Completed row hashes never change during unrelated stream chunks.
- No torn, stale, duplicate, or full-screen flash frames.
- One coherent frame per resize event; below floor shows only a bounded 60x20 guidance message.
- Draft, paste, modal, selection, and stable transcript anchor survive reflow.

## Scenario 3: idle lifecycle

Exercise each state and then return to unchanged idle:

- first-run/empty;
- task complete;
- notice expired;
- MCP modal closed;
- tool/subagent terminal;
- stream buffer empty.

For each, observe 60 seconds. There must be no pending application tick, pane invalidation, transcript render, or frame-sequence increment. Process CPU meets the one-core formula in the performance contract. Terminal-owned cursor behavior is recorded separately.

## Scenario 4: mouse and keyboard fallback

Run the [mouse interaction matrix](contracts/mouse-interaction.md) in Windows Terminal and the macOS/Linux terminals:

1. Wheel inside/outside transcript.
2. Click input at ASCII, wrapped, tab, CJK, combining, emoji, and RTL/mixed cells.
3. Click every visible slash-command row.
4. Click individual tool and subagent chips; verify only the target toggles.
5. Click every modal button and verify press/release causes one action.
6. Drag-select transcript text and copy through OSC52; repeat with clipboard denied and use terminal-native fallback.
7. Resize after rendering but before an injected click; stale map must be ignored.
8. Disable mouse reporting and complete every task via the keyboard-equivalence table.

Pass = 100% target/action match, no double invocation, no caret inside a grapheme/paste atom, successful copy/fallback, and full keyboard completion.

## Scenario 5: paste fidelity

Feed the exact cases in [input-draft.md](contracts/input-draft.md):

- 1,023/1,024 code points and 5/6 logical lines;
- empty, one huge line, LF/CRLF/lone-CR/mixed/trailing breaks;
- tabs, NUL, Arabic, CJK, emoji, combining sequences;
- >32 KiB content and multiple blocks;
- typed text before/between/after blocks;
- expand/collapse/remove/clear/abandon/busy queue;
- click and keyboard navigation around blocks.

Capture the engine's received user message and compare byte-for-byte with the concatenation oracle. Pass = all boundaries classified correctly, collapsed blocks <=2 rows, no overflow/truncation/placeholder bytes, and no released block leaks into a later draft.

## Scenario 6: project instructions and memory

Use disposable workspaces A and B plus separate sessions.

### Instructions file

Test missing, empty, valid, exact 32 KiB, oversized, BOM/newlines, invalid UTF-8/NUL, unreadable, and outside-root symlink/junction `MUHIYA.md`. Include secret fixtures (`sk-...`, `Bearer ...`, `api_key=...`, and configured MCP secret).

Pass:

- Valid instructions affect the first turn in full TUI, `--simple`, and `-p`.
- Missing/empty is silent.
- Invalid cases are non-fatal and safe; no rejected content reaches disk/wire.
- Workspace B never receives A's context.

### Durable memory

1. Establish an explicit project decision and a durable finding; verify the hidden final trailer is absent from visible output.
2. Complete another turn; verify one `<memory-update>` appears at the newest tail and not again.
3. Start a new session in A; both items are in its boot snapshot and correctly recalled.
4. Supersede the decision; only the new statement is current after restart.
5. `/memory list`, explicit remember, forget, and clear; repeat CLI/line-mode equivalents.
6. Attempt malformed/oversized/>4-item/intermediate/tool/reasoning/code-fence trailers and secret-bearing candidates.
7. Corrupt/interrupt sidecar and ledger writes; restart.
8. Move/rename A and confirm no unrelated path inherits its memory.

Pass follows [project-context.md](contracts/project-context.md): correct recall/isolation, exact current projection, no secrets, no duplicate updates, atomic recovery, inspect/clear control, and no task failure from memory metadata rejection.

## Scenario 7: deterministic prompt and live cache evidence

Run exact stability suites from Gate 1, then the live cache arm:

```powershell
go run ./benchmarks/cachebench -scenario all -runs 3 -build-label improved -out specs/005-terminal-memory-overhaul/benchmarks/cache/improved
go run ./benchmarks/cachebench -compare specs/005-terminal-memory-overhaul/benchmarks/cache/baseline specs/005-terminal-memory-overhaul/benchmarks/cache/improved
```

Verify:

- Identical project-context snapshots produce byte-identical system/tools/settled-history prefixes across turns and restart.
- The fixed memory output contract and boot block cause only the documented upgrade-boundary change.
- Changed instructions/memory/skills produce one deterministic tail block, no settled-history rewrite, and no false invalidation event.
- Plain unchanged follow-ups retain the existing small system-tail budget.
- Provider-reported prompt/completion/cache tokens, request count, cost, cold start, and steady state are all reported; cache efficiency and reviewed task quality do not regress.

## Scenario 8: simple/non-interactive regression

On Windows, macOS, and Linux CI plus a local TTY:

```powershell
muhiyacode --simple
muhiyacode resume <session-id> --simple
"prompt" | muhiyacode
muhiyacode -p "prompt"
muhiyacode memory list
```

Verify input, modal fallback, `/context`, `/compact`, exit/EOF, project context, message submission, answer output, and memory commands. No path requires full-screen transcript/composer/mouse types.

## Scenario 9: visual states and accessibility

Capture every state in [visual-state-system.md](contracts/visual-state-system.md) at 80x24 and 120x40, dark/light, `NO_COLOR=1`, Unicode and ASCII glyph modes. Repeat around 59/60 columns and 19/20 rows.

Pass:

- One Theme owns all comparable color/spacing/glyph/border/breakpoint values.
- Every state is identifiable without color alone.
- First-run/loading/error/too-small provide an actionable next step.
- Hit targets match the same frame geometry.
- Accessibility reviewers identify all required states correctly.

## Definition of done

- [ ] Measurement-first baseline and improved raw terminalbench evidence stored from clean recorded commits.
- [ ] Automated quality, cross-platform CI, and race suites green.
- [ ] All performance/resource gates pass, including 60-minute run and 30 resume trials.
- [ ] Frame recordings show no flicker/artifacts in Windows Terminal and representative macOS/Linux terminals.
- [ ] Mouse/copy matrix and keyboard-only fallback pass completely.
- [ ] Paste fidelity oracle passes all boundary/fuzz cases.
- [ ] Project instructions/memory recall, isolation, supersession, security, inspect/clear, and recovery pass in all run modes.
- [ ] Prompt stability/cachebench show no unexplained drift or quality/cache regression and report all token/cost metrics honestly.
- [ ] `README.md`, architecture/agent/cache/security docs, and feature 002 tail contract are synchronized.
