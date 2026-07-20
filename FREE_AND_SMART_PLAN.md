# FREE AND SMART — the v1.1.0 field-test polish plan

**Status: PLANNED — awaiting execution approval.**
**Ships inside the still-unreleased v1.1.0** (branch `feature/native-agent-v1.1.0`, HEAD 60ff87f).
Nothing here creates a user-facing cache epoch beyond the one v1.1.0 already records, because
v1.1.0 was never tagged or published — every prefix change below folds into the same release.
**Executor: Claude Opus 4.8.** Per-phase gate: `go build ./... && go vet ./... && go test ./...`
plus staticcheck; goldens regenerated in the same commit as any prefix-byte change
(`go test ./internal/instructions/... -run TestDump -update` and
`go test ./internal/orchestrator/... -run TestWiring_PrefixBytesGolden -update-prefix-golden`).

## What the field test proved

The owner ran v1.1.0 on a real project (an Excel web tool) and hit six defects. The transcript
is the specification — every item below traces to something that actually happened:

1. The `ask_user` answers rendered as a wall of raw JSON in the transcript ("● Question {
   "answers": [ … ] }"). Unreadable.
2. The user typed **"Go"** after a plan was presented, it classified as `chat`, the brief said
   `agents=0`, and — because the role split forbids main-loop edits — the agent could do
   NOTHING. It told the user to send another message so the next turn might have a budget.
   A coding agent asking permission to be allowed to work is the worst possible UX.
3. After the executor reported success, the main model **re-read the changed files and re-ran
   checks anyway**, burning tokens to re-verify what the report already proved.
4. The header buries the workspace path on line 2; the owner wants it on line 1 between the
   version and the context meter.
5. Elapsed time renders as `3m07s`; the owner wants a space: `3m 07s`.
6. tasks.md items and the main↔sub handoffs are not consistently detailed enough for the
   CHEAPER executor model to run them without thinking — which is the entire point of the
   plan/execute split: the smart model's intelligence must be ENCODED into the artifacts.

The direction, in the owner's words: **make the tool free and smart** — free of artificial
caps and ceremony, smart through instructions that carry judgment instead of enforcement
carrying it.

## The root-cause map (why each defect exists)

| Defect | Root cause in code |
|---|---|
| JSON answers wall | `encodeAnswers` (turnsignals.go) is the MODEL-facing payload, and the TUI renders it verbatim: `toolOutcome` falls through to `summarizeTool(tool.output)` (render_tool.go:~138) and the expanded row prints the raw output. No `ask_user` display case exists. |
| "Go" deadlock | `classAgents` (classify.go:160) still exists: `chat:0`. "Go" matched `continuationRE` → inherited a chat-class predecessor → brief said `agents=0 (no run_subagent)` (classify.go:200-202) → `runSubagentInput`'s budget gate refused → role split forbids direct edits → nothing possible. Budgets and the role split are jointly incoherent. |
| Redundant re-verification | Two instructions CONTRADICT: operating contract rule 6 says "Verify to the brief: **run the checks yourself**" (instructions/prompt.go:49) while DELEGATION says "Treat its report as ground truth" (prompt.go:148). The model resolves the conflict by re-verifying. The report format (`ReportFormatImplementation`, subagents.go:115) also has no machine-readable status line, so the main model cannot TELL whether verification already happened. |
| Path placement | render_header.go builds line 1 as brand·version·[update]·context and puts the path on line 2. |
| `3m07s` | `formatDuration` (render.go:449-454): `fmt.Sprintf("%dm%02ds", …)` — no space. |
| Under-detailed tasks | The PLANNING skill (prompt.go PLANNING section) says items should "name their files, about one commit each" — it never says WHO the items are for. Nothing states that tasks must be executable by a cheaper model without design decisions, and nothing guarantees a plan-only request actually writes the file (step 4 says "present it and stop"). |

---

## Phase 0 — TUI polish (no prefix impact; pure display)

### 0a. `ask_user` answers render like a conversation

The JSON stays EXACTLY as the model-facing tool result — `encodeAnswers` is the model
contract and changing it would alter behavior. Only the display changes.

- `internal/tui/render_tool.go`:
  - `toolOutcome`: add `case "ask_user":` → parse the output as the `user_answers` payload
    and return `"N answered"` (fallback to `summarizeTool` on parse failure — never crash on
    unexpected output).
  - Expanded row rendering: when `tool.name == "ask_user"` and the payload parses, render one
    block per answer instead of raw JSON:
    ```
    Q  What kind of Excel online tool do you want?
    →  View & edit uploaded xlsx — A web app that opens .xlsx files…
    ```
    (question faint, arrow+label as the emphasized line, description muted, wrapped). Parse
    failure → existing raw rendering, unchanged.
  - Add a small parser `parseUserAnswers([]byte) ([]answerView, bool)` beside the renderer;
    tolerant of missing fields.
- Tests: `internal/tui/tooldisplay_test.go` — collapsed shows "2 answered"; expanded shows the
  question and the CHOSEN label, never `selected_index` or a `{`; malformed payload falls back
  to raw; RTL-safe via the existing `m.rtl` shaping helpers.

### 0b. Header: workspace path on line 1, after version, before context

- `internal/tui/render_header.go`:
  - Line 1 becomes: `✳ MuhiyaCode v1.1.0 · <workspace path> · [Update available …] · context 12%`.
    The path is middle-truncated with the existing `truncateLeft` (leading ellipsis keeps the
    most-specific segments) to whatever width remains AFTER the fixed segments — the context
    meter must never be pushed off; the path is the flexible segment.
  - Line 2 keeps ONLY the agent-view cluster (`· view <agent>…` / `· N agent(s) running`) and
    renders empty otherwise — `fitLine` already tolerates an empty line; verify the layout
    height math (`layout.go` counts rendered lines) stays correct when line 2 is blank.
- Tests: update `TestHeaderIsModelFreeAndNoticeStaysOutOfTranscript` (tui_test.go) to assert
  the path appears on the same line as the version and before "context"; resize test at narrow
  width asserts the context meter survives and the path truncates.

### 0c. Elapsed time: `3m 07s`

- `internal/tui/render.go:449` — `formatDuration`: `fmt.Sprintf("%dm %02ds", …)`.
  Under-a-minute stays `42.1s` (unchanged).
- Tests: extend the footer/format test with `90s → "1m 30s"`, `61s → "1m 01s"`, `3599s → "59m 59s"`.

---

## Phase 1 — FREE: remove every subagent budget

There is no cap on run_subagent, ever. Judgment (Phase 2/3 instructions) replaces enforcement.
What is NOT removed, deliberately (this is the "balanced" line the owner asked for):

- **One agent at a time, strictly serial** — stays. It is a cache/correctness directive
  (continuations need a single chain), not a budget.
- **The plan/execute role split** — stays. It is the architecture, not a restriction.
- **Liveness guards** — stay: `hardTurnCeiling`, the H5 distinct-failure terminator, the
  repeat limiter, the B7 all-failed detector, subagent per-run `MaxTurns` with the INV-3
  wrap-up protocol. These stop infinite loops, not work.
- **The review gate** — stays advisory (it decides when a review is WORTH it; it never blocks).

### Deletions (each with its blast radius)

- `internal/orchestrator/classify.go`: `classAgents` map (160), `MaxAgentRuns` from `Budget`
  (36) and from `BudgetFor` (175), the whole `agents := …` / `agents=0 (no run_subagent)`
  brief fragment (196-202) — the brief drops the agents field entirely. Reword the comment
  block that explains the zero-budget prohibition.
- `internal/orchestrator/effort.go`: `MaxAgentRuns` field + values in both profiles (the
  per-effort agent ceiling dies with the per-class one). `AgentTurnScale`, `AgentReasoning`,
  `AutoReview` stay.
- `internal/orchestrator/engine.go`: `taskAgentCap`, `taskAgentDenied` fields; their resets in
  `resetTaskState`. `taskAgentRuns`/`taskAgentReused` STAY (stats, not caps).
- `internal/orchestrator/subagent.go`: the entire budget gate in `runSubagentInput`
  (the `limit/used` check through the escalating denial), and the "N subagent run(s)
  remaining" / "budget now exhausted" note appended to every report (the note is budget
  vocabulary; the report ends clean).
- `internal/orchestrator/turnloop.go`: the class-escalation cap raise; the AutoReview nudge
  drops its `e.taskAgentRuns < e.taskAgentCap` condition (fires on the gate's decision alone).
- `internal/instructions/gates.go`: `RuleSubagentBudget`, all four `GateSubagentBudget*`
  texts and registrations.
- `internal/instructions/prompt.go` DELEGATION: delete both budget sentences ("The brief's
  'agents<=N' is this task's allowance; 'agents=0' means conversation only").
- `internal/instructions/tools.go` run_subagent description: delete "The task brief's
  agents<=N is your allowance."
- `internal/orchestrator/rolesplit.go`: `TestEveryMutatingClassCanAffordAnExecutionAgent`'s
  reason evaporates — the deadlock class cannot exist without budgets. Replace with a pin
  that PROVES the fix: a chat-classified turn can still dispatch (see tests).
- Docs/goldens/wiring: prefix goldens regenerate; `specs/010…/wiring-inventory-data.md` rows
  for the budget gate texts flip to removed; audit realAllowlists lose nothing (run_subagent
  itself stays).

### Tests

- REWRITE `subagent_budget_test.go` → `subagent_dispatch_test.go`: budget-denial tests die;
  keep/reshape the report-format and serial-chain pins. New pins:
  - `TestChatClassTurnCanDispatch` — the transcript regression: classify "Go" (chat), run a
    task, the model calls run_subagent, IT WORKS.
  - `TestNoBudgetVocabularyInBriefOrPrompt` — greps the rendered brief and system prompt for
    `agents<=`, `agents=0`, "budget" — all absent (tombstone).
- `delegation_regression_test.go`: the class-caps test dies; `TestAutoReviewNudge*`
  re-fixtured without cap fields.
- `faultinjection_test.go`: budget-exhaustion rows die; add row
  `plan-presented-then-go-executes`: scripted plan turn → "Go" → dispatch succeeds →
  recovery invariant `guidedSuccess`.
- Gauntlet `forbiddenStrings` gains: `"agents=0"`, `"budget exhausted"`,
  `"can't dispatch agents"` — the transcript's exact failure text can never render again.

---

## Phase 2 — SMART: the main↔sub communication protocol, and verification becomes pull

### 2a. One consistent trust story (kills the re-read defect)

The contradiction is resolved in ONE direction: **the report is the verification** — the main
model re-verifies only when the report asks it to, or fails to prove itself.

- `internal/instructions/prompt.go` operating contract rule 6 REWRITTEN:
  - Old: "Verify to the brief: run the checks yourself, send failures back to the agent, then stop."
  - New: **"Trust the report: an agent report whose Verification section shows the check and
    its result is final — do not re-read its files or re-run its checks. Re-verify ONLY when
    the report says NEEDS-VERIFY, says BLOCKED, or shows no verification. Then check off
    tasks.md and give the final answer."**
- DELEGATION keeps "treat its report as ground truth" — now consistent instead of contradicted.
- CACHE DISCIPLINE gains half a sentence: re-reading a file the report already covers is the
  same offense as re-reading unchanged context.
- Wrap-up is named as a sequence so it closes flat, never open-ended: report arrives →
  (re-verify only if pulled) → tick tasks.md → final answer. No "let me double-check" loop.

### 2b. The report protocol (sub→main) — machine-readable status

- `internal/instructions/subagents.go`:
  - `ReportFormatImplementation` → `"Changes made (file:line); Verification: each check run
    with its result; STATUS: COMPLETE | NEEDS-VERIFY: <exact thing to check> | BLOCKED: <why>;
    Remaining concerns."`
  - `SubagentGeneralSystem` gains one sentence: end every report with the STATUS line; claim
    COMPLETE only when you ran the stated check and it passed — if you could not run it, say
    NEEDS-VERIFY and name the exact command.
  - `HandoffDeliverableImplementation` already instructs ticking tasks.md — keep.
- `internal/orchestrator/subagent.go`: `parseReportTrailer` (contextrecord.go) already parses
  trailers — extend or add `parseReportStatus(report)` returning
  `complete|needs-verify|blocked|unknown`, stored on `subagentResult` and surfaced in the
  run_summary event (observability only; NO harness auto-behavior — the standing directive
  stands: the model decides, the instructions guide it).
- These are Sidecar-class per-kind system bytes: one cold write per kind inside the unreleased
  epoch. Fine.

### 2c. The handoff protocol (main→sub) — written for a cheaper model

- `internal/instructions/tools.go` `ToolRunSubagentTaskPropertyDescription` rewritten:
  `"Write the task so a cheaper model executes it without design decisions: the goal, the
  exact files and symbols, the specific change per file, the constraints, and the exact
  command that proves it worked. It cannot see this conversation or ask you anything."` (the
  report-format cross-references stay).
- DELEGATION gains the complexity-awareness guidance that REPLACES budgets (the owner's
  "burns itself on a simple project" point):
  `"Scale delegation to the real work: a small fix is ONE short dispatch; never delegate to
  look thorough. There is no run cap — your judgment is the cap, and chained follow-ups to
  the same agent are the cheap path."`
- Tests: `TestDelegationSectionContent` re-pins the new sentences;
  `TestPlanningSkillContent`-style pin for the task-property text; handoff render test
  asserts a `Verify:`-bearing task flows through to the sub's user message.

---

## Phase 3 — SMART: plans are files, tasks are executor-ready

### 3a. Every plan lands in tasks.md — always

- PLANNING section (instructions/prompt.go) step 3-4 rewritten:
  - Step 3: `"Write the plan INTO tasks.md: each item names its exact files and the specific
    change, carries the check that proves it, and is executable by the execution agent
    without further design decisions — put the thinking in the item, not in your head. Add a
    short Notes section for constraints and risks."`
  - Step 4: `"If the user asked for a PLAN, write tasks.md FIRST, then present the summary
    and stop — the file is the deliverable. Otherwise start executing."`
- This is also the answer to "plans in an md file": tasks.md IS the plan file, written
  unconditionally — presenting a plan without writing it becomes an instruction violation.
- `ParseChecklist` already tolerates prose sections (they become the Note) — the Notes
  section needs no parser change. Verify with a fixture containing `## Notes`.

### 3b. tasks.md detail standard (the "cheaper model" contract)

- The checklist convention in the operating contract (rule 4) gains the same standard in
  compressed form: items name files + change + check.
- The completion disclosure and the panel are unchanged — they already read the file.
- Tests: extend `checklist_test.go` with a realistic detailed-item fixture (path + change +
  "— Verify: …" suffix) proving round-trip through `ParseChecklist`; a PLANNING-section pin
  asserting "INTO tasks.md" and "without further design decisions".

---

## Phase 4 — Stability: the logic audit and the field-test gauntlet

The owner asked for "the stablest version ever by doing logic checking". Concretely:

1. **Contradiction sweep**: with rule 6 rewritten, grep-audit every registered instruction
   text pair that mentions verification, delegation, or reports for residual conflicts (the
   audit suite checks structure; this pass checks SEMANTICS — done by reading
   `instructions_dump.golden` end to end once, as a review artifact).
2. **Transcript-replay E2E** (new gauntlet scenario): plan presented → user: "Go" →
   dispatch executes 11 scripted steps → executor reports COMPLETE with verification →
   main does NOT issue a read_file for a reported file → ticks tasks.md → finalizes. Assert:
   zero re-reads of reported files, zero budget vocabulary, one serial agent.
3. **NEEDS-VERIFY path E2E**: executor reports NEEDS-VERIFY with a named command → main runs
   exactly that check itself (the role gate's read-only-shell carve-out already allows it) →
   finalizes. Assert the check ran and nothing else was re-read.
4. **Fault rows**: `report-blocked-main-recovers` (BLOCKED report → main re-dispatches with a
   corrected handoff, bounded); `go-after-plan-executes` (Phase 1's row).
5. Full guard suite: build, vet, staticcheck, gofmt, all tests, 800-line budget (new files
   must fit), layering, wiring inventory, goldens consolidated, prompt-budget ratchet
   re-measured (rule-6/DELEGATION/PLANNING rewrites change prompt size — re-baseline tight,
   in whichever direction it lands).
6. Docs touch-up: agent-design.md (verification-is-pull, no budgets), prompt-caching.md
   (brief no longer carries agents field), CHANGELOG 1.1.0 entry amended (it ships in the
   same release). README delegation paragraph.
7. **Build for test**: rebuild `muhiyacode.exe` with ldflags, swap the vendored npm exe,
   `doctor` green — same hand-off as the last round.

---

## Sequencing

| Phase | Content | Risk |
|---|---|---|
| 0 | ask_user render, header path, duration space | none — display only |
| 1 | budget removal end to end | medium — touches dispatch; the serial rule and liveness guards must visibly survive (tests pin them) |
| 2 | trust-the-report + protocol texts | low code / high text — goldens same-commit |
| 3 | plans-to-file + executor-ready tasks | low |
| 4 | audit, E2Es, docs, build | gate |

Each phase is one commit, fully green, goldens regenerated in-commit where prefix bytes moved.

## Balance ledger (what deliberately does NOT change)

- Serial one-agent-at-a-time; the plan/execute role split and its carve-outs; the review
  gate's advisory decisions; classification (still sizes turn caps and review tiers — just no
  agent math); H5/B7/repeat/turn-ceiling liveness guards; the session advisor and frozen
  models; the whole feature-012 linker; the checklist feed. The tool gets FREER (no caps, no
  dead-ends) and SMARTER (protocol, detail standards, trust rules) without getting less safe.
