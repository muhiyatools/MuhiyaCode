# Quickstart: Validating the TUI & Agent Experience Overhaul

**Feature**: `003-tui-ux-overhaul` | **Plan**: [plan.md](plan.md)
Run these scenarios end-to-end to prove the feature works. Contracts referenced instead of duplicated.

## Prerequisites

- Go 1.25 toolchain; this repo on branch `003-tui-ux-overhaul`.
- Gateway repo `F:\MuhiyaWorkspace\MuhiyaWorkspace` with the D1/D2 changes deployed (locally or api.muhiya.com) — gateway ships first ([usage-api.md §3](contracts/usage-api.md)).
- A valid virtual key (`sk-virt-…`) with a plan + at least one top-up (for `/usage` data richness).
- Terminals installed for the matrix: **Warp (required — SC-001 names it)**, plus at least two of {Windows Terminal, iTerm2, VS Code integrated terminal}.
- Fixture skills: copy `specs/003-tui-ux-overhaul/fixtures/skills/` (created during implementation) into `<workspace>/.agents/skills/`.

## Gate 0 — automated suites (must pass before any manual scenario)

```powershell
go fmt ./... ; go vet ./... ; go test ./... -count=1        # constitution IX
go test ./internal/orchestrator -run 'PromptStability|SystemPrompt|CacheHitGuard|RestartDeterminism' -count=1 -v   # D12 guards
go test ./internal/tui -run 'Golden|Markdown|Summary|ToolDisplay' -count=1         # D14 layer 1
```

In the gateway repo:

```powershell
go test ./... -count=1        # meta-chunk gate, usage_estimated, /v1/usage handler
```

Expected: all green; TUI goldens pinned at 80×24 + Ascii profile ([visual-system.md §5](contracts/visual-system.md)).

## Scenario 1 — rendering matrix + header (SC-001, SC-002; US1/US7)

1. Launch `muhiyacode` in each matrix terminal at 80, 120, and 200 columns **from a deeply nested workspace (≥5 path segments, long enough not to fit at 80 cols)**; repeat one terminal in a light theme and once with `NO_COLOR=1`.
   - At ≥120 cols: header line 2 shows the **complete workspace path**, not the basename (FR-021).
   - Narrow until the path no longer fits: left truncation with a leading `…`, rightmost segments preserved.
   - At ≥80 cols: the delegated-model label reads exactly `Subagent` (FR-022 — check at ≥80 cols; the 60–80 breakpoint drops that segment).
2. Ask: *"Show me a markdown demo: a heading, **bold**, *italic*, `inline code`, a list, and a 4-column table comparing Go TUI libraries."*
3. Verify against [visual-system.md](contracts/visual-system.md): styled bold/italic/code (no raw `**`/`*`/`_` anywhere — SC-002), aligned table with styled header + inline styling inside cells, no clipped/overflowing header or seams (Warp especially), coherent palette, readable light-theme + NO_COLOR output.
4. Resize continuously while a response streams: no torn lines; shrink below 60×20: clean "terminal too small" pane; restore: layout returns.

## Scenario 2 — tool display contract (SC-005; US3)

1. Ask for a task touching every class: *"Create demo/util.go with a helper, then read it, grep for the function name, run `go vet ./demo`, and list the demo directory."*
2. Collapsed: each entry is one line `marker · label · target · outcome` (`+A −R` for the write/edit, `N lines` read, `N matches` grep, `exit 0` shell) — no content lines ([tool-display.md §1-2](contracts/tool-display.md)).
3. Ctrl+O: all entries expand simultaneously to the uniform detail block (full diff, output boxes); Ctrl+O again: all collapse. No mixed states.
4. Force a failure (`go vet ./nonexistent`): `×` marker + first error line collapsed; full output expanded.

## Scenario 3 — thinking + task summary (SC-003, SC-004, SC-006; US2)

1. Run a multi-turn task with reasoning enabled. During: one `▏ thinking…` line with elapsed time; after completion: **zero** thinking residue.
2. After the final message: one summary line `credits C · T tokens · cache H%` ([task-summary.md §3](contracts/task-summary.md)); nothing above the input box.
3. SC-006 sweep of the whole transcript + status area: no "thought for", no effort label, no "chat", no tool/agent counts, no cached/new breakdown, no session %.
4. Press Esc mid-task: partial summary with `interrupted` marker.
5. Point MuhiyaCode at a generic OpenAI endpoint (no `muhiya_log`): summary shows tokens + cache only (credits omitted, no error).
6. **Accuracy check (SC-004)**: run the D14 benchmark session; export gateway rows (`GET /api/logs?virtual_key_id=…`, admin) and assert Σ TUI credits == Σ `request_logs.cost` × 100 for the matching `log_id` set. Record in `benchmarks/`.

## Scenario 4 — input area, commands, auth (SC-006, SC-007; US4/US5)

1. Idle input shows placeholder `Type a request · / for commands` (exact string, [visual-system.md §3](contracts/visual-system.md)); no "Enter send"/"Ctrl+P" hints; mode line keeps mode/badges. Ctrl+P still opens the palette (binding unchanged).
2. Send a message: user band has background only, no `> ` prefix.
3. Signed in: palette hides `/login`, shows `/usage` + `/logout`. `/usage` → grouped modal (plan windows w/ remaining + reset, extra credits, spend today/billing period/session) in <3 s.
4. Failure drills (FR-020): stop the gateway → friendly unreachable state; use a revoked key → friendly invalid-key state; UI responsive throughout.
5. `/logout` → `/login` reappears; `/usage` hidden; log back in with the key; `/usage` works again.
6. Trigger a web search task: no provider name anywhere (FR-016).

## Scenario 5 — context modal (US6)

`/context` mid-session: grouped sections (Context / This session / Streams / Cache health) per [visual-system.md §3](contracts/visual-system.md); every pre-overhaul datum still present (compare against `formatContextReport` field list in [research A9](research.md)); session credits line present (or honest "credits unavailable (N/M requests priced)").

## Scenario 6 — skills trial (SC-008, SC-009; US8)

1. With fixture skills installed, start a fresh session. Confirm via request logging/debug dump that the system prompt contains the sorted `## Skills` listing ([skills-delivery.md §2](contracts/skills-delivery.md)) and that two consecutive turns have byte-identical prefixes.
2. Run the 10 matching tasks (fixture manifest): agent reads the right SKILL.md (visible Read entry) and follows it in ≥9/10.
3. Run the 5 non-matching tasks: zero skill reads/mentions.
4. `/skills` modal still lists all skills; selecting one injects it for that turn only; opening the modal is instant (lazy loading — no 40-file read burst).
5. Place one fixture skill in an external root (e.g. `MUHIYA_SKILLS_DIR`): it appears in the `/skills` modal but **not** in the system-prompt listing (workspace-only advertisement, [skills-delivery.md §1](contracts/skills-delivery.md)); `/resume` the session after editing a workspace SKILL.md: the listing (and prefix) is unchanged until `/new`.

## Scenario 7 — cache benchmark (FR-027, SC-008; constitution X)

Real cachebench interface (mirrors the canonical 001 invocation, `specs/001-prompt-cache-optimization/quickstart.md`); identical fixture workload, model, gateway, and effort on both runs:

```powershell
# on main:
go run ./benchmarks/cachebench -scenario all -runs 3 -build-label baseline -out "specs/003-tui-ux-overhaul/benchmarks/baseline"
# on this branch:
go run ./benchmarks/cachebench -scenario all -runs 3 -build-label improved -out "specs/003-tui-ux-overhaul/benchmarks/improved"
# comparison:
go run ./benchmarks/cachebench -compare "specs/003-tui-ux-overhaul/benchmarks/baseline" "specs/003-tui-ux-overhaul/benchmarks/improved"
```

Assert: session hit rate non-regression; prompt-token growth ≤ skills-listing bytes; report cold-start and steady-state separately; store `comparison.md` + raw JSON in `specs/003-tui-ux-overhaul/benchmarks/`.

## Scenario 8 — qualitative review (SC-010)

1. Recruit ≥5 testers; each runs a fixed walkthrough script (Scenarios 1–5 tasks verbatim) on at least one matrix terminal.
2. Each answers one 5-point item: *"The interface feels polished and trustworthy."*
3. Pass = ≥80% answer 4 or 5 (top-two-box) **and** zero testers reproduce a pre-overhaul complaint (cluttered stats, broken bold, misaligned top bar).
4. Record responses in `specs/003-tui-ux-overhaul/benchmarks/qualitative/`.

## Definition of done

- [ ] Gate 0 suites green (both repos)
- [ ] Scenarios 1–6 pass on the full terminal matrix, incl. light theme + NO_COLOR
- [ ] Scenario 7 evidence stored and non-regressive
- [ ] Scenario 8 qualitative review passes (≥80% top-two-box, zero complaint recurrence)
- [ ] Docs synced (`README.md`, `docs/agent-design.md`, `docs/architecture.md`) for skills listing + credits display (constitution workflow gate)
