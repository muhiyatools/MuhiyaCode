# Quickstart: Validating Automatic Skill Use & Interface Polish

**Feature**: `013-auto-skills-ui-polish` | References: [spec.md](spec.md), [contracts/](contracts/)

This is the runnable proof that the feature works end-to-end. Scenarios map to the spec's user stories and success criteria; contracts define the exact expected shapes.

## 0. Prerequisites

```powershell
# From the repo root, on the feature branch
go fmt ./... ; go vet ./...
go build -o muhiyacode.exe ./cmd/muhiyacode        # adjust output name per platform
go test ./... -count=1                              # full offline suite must be green
```

Live scenarios additionally need: a configured gateway API key (or OAuth login), and the note in memory that a globally installed `muhiyacode` may shadow the fresh build — run the repo binary explicitly or set `MUHIYACODE_BINARY`.

## 1. US1 — Automatic skill use (SC-001, SC-002, SC-004)

Setup — install a distinctive skill in each root class:

```powershell
# Workspace skill
New-Item -ItemType Directory -Force .agents\skills\frontend-design | Out-Null
@'
---
name: frontend-design
description: Opinionated guidance for building distinctive, non-generic web frontends.
---
Always use a warm editorial palette, oversized display type, and no default browser blue.
'@ | Out-File -Encoding utf8 .agents\skills\frontend-design\SKILL.md

# Home skill (external root — previously unreachable automatically)
New-Item -ItemType Directory -Force $env:USERPROFILE\.agents\skills\commit-style | Out-Null
@'
---
name: commit-style
description: House style for git commit messages.
---
Write commit subjects as plain sentences describing the user-visible effect.
'@ | Out-File -Encoding utf8 $env:USERPROFILE\.agents\skills\commit-style\SKILL.md
```

Run the TUI and validate:

| # | Do | Expect |
|---|----|--------|
| 1 | Start a session; prompt: "build me a small landing page in this folder" (never name the skill) | Transcript shows a `read_skill` call for `frontend-design` **before** any page file is written; the output visibly follows the skill (acceptance #1) |
| 2 | Same session, prompt: "what's 2+2?" then "list the files here" | Zero `read_skill` calls; no skill text in context (acceptance #3, SC-002) |
| 3 | Prompt work that drifts into commit territory mid-task ("…and commit the result") | `read_skill` for `commit-style` may fire mid-task, before the commit step (acceptance #6) |
| 4 | Queue a skill manually via `/skills`, then submit a matching prompt | The manual `<skill>` block appears once; a `read_skill` call for the same name returns the already-provided notice, not a second body (acceptance #5) |
| 5 | Prompt a delegation-worthy task in skill territory (e.g., "explore this repo's frontend and propose a redesign, use an agent") | The `run_subagent` call carries `skills: ["frontend-design"]`; the sub-agent brief contains the skill block; the sub-agent makes no `read_skill`/discovery calls (acceptance #8) |
| 6 | Corrupt a skill (`echo garbage > .agents\skills\frontend-design\SKILL.md` with no frontmatter) and restart | Session starts cleanly; the malformed skill is absent from SKILLS; nothing errors (FR-009) |

Skill-quality check (SC-004): run scenario 1 against two different session models; outputs must differ in voice while both honoring the skill — no verbatim skill-text dumps.

## 2. US2 — Token & cache truth (SC-005, SC-006)

| # | Do | Expect |
|---|----|--------|
| 1 | Run any task; watch the live activity line | Token figure is full digits with commas (e.g., `41,382 tokens`) and equals cache-read + uncached + output for the task ([display-formats.md](contracts/display-formats.md) §2) |
| 2 | Let the task finish | Summary line token figure **equals** the final live figure (same helper, FR-024); no `⚠ N harness` marker anywhere |
| 3 | Run a task that spawns sub-agents; open `/context` | `Hit rate:` reflects main + sub-agent + aux traffic (all-stream rate); with mixed cache behavior it differs from what a main-only computation would give (SC-006 — cross-check against `usage.jsonl` records) |
| 4 | `/context` layout | Exactly three groups — Context / Session / Models — ≤14 content lines, full-digit numbers, no per-model rows, no category table, no invalidations, no pressure/time/lines rows ([display-formats.md](contracts/display-formats.md) §4) |
| 5 | Run against an endpoint that reports no cache fields (or inspect a fresh session before first response) | Hit rate renders `unavailable` — never `0%` (FR-022) |
| 6 | Resume a session that previously ran sub-agents | Context-card totals and hit rate include pre-resume traffic (rebuilt from `usage.jsonl`) |

## 3. US3 — Command surface (SC-007, SC-009)

| # | Do | Expect |
|---|----|--------|
| 1 | Type `/` and browse the palette | `/logout` reads "Log Out of Account"; `/errors` and `/permissions` rows absent |
| 2 | Submit `/errors`, `/permissions`, `/mode` | Standard unknown-command notice each time; no crash (FR-018) |
| 3 | Open `/reasoning` | Subtitle "How hard the model thinks."; no level description mentions DeepSeek (FR-016) |
| 4 | Look at the footer in a fresh session (default mode) | `normal` chip visible before the effort chip; `Shift + Tab to cycle` rendered beneath the cluster (Clarification Q4) |
| 5 | Press Shift+Tab twice | Chip flips `normal → auto-accept → normal`; hint stays; a user relying only on the screen can switch modes (SC-009) |
| 6 | Shrink the terminal below ~60 columns | Hint line degrades/drops first; mode line never corrupts (edge case) |

Orphan sweep (SC-007): `grep -ri` for `harness friction`, `/errors`, `/permissions`, `Clear API key`, `DeepSeek maps`, `Ctrl+T`, `Tab agents` across `internal/` and `docs/` — zero hits in user-visible strings (telemetry identifiers exempt, [ui-surfaces.md](contracts/ui-surfaces.md) §C4.3).

## 4. US4 — To-dos & arrow keys (SC-010)

| # | Do | Expect |
|---|----|--------|
| 1 | Start a multi-step task that produces a checklist | Panel appears and stays for the task's whole active life; Ctrl+T does nothing |
| 2 | Task completes / goes idle | Panel retires exactly as before (no lingering) |
| 3 | With ≥2 agents running and an **empty** composer, press → repeatedly, then ← | View cycles main → A1 → … → main forward, exact inverse backward |
| 4 | Type text into the composer, press ←/→ | Caret moves within the text; view never switches (Clarification Q1) |
| 5 | Press Tab with text and no leading `/` | Nothing happens (agent branch removed); with `/` prefix, autocomplete works as before |
| 6 | Footer hints | `Esc stop · ←/→ agents · / commands` — no Ctrl+T, no "Tab agents" |

## 5. Constitution X — Before/After Verification (SC-003, SC-011)

Hold model, gateway, effort, and workload constant; only the binary changes.

1. **Baseline** (pre-feature binary): run the standard multi-turn coding gauntlet session (no skills installed). Record from `usage.jsonl` / bench notes: steady-state hit rate, total prompt/completion tokens, session start wall-clock (3 runs, note variance).
2. **After** (feature binary, same workspace, no skills installed): repeat identically. Expect: steady-state hit rate within baseline variance (SC-011 — first request is the attributed upgrade epoch; exclude it as cold start per the measurement standards); token totals within variance; session start within +5% (SC-003).
3. **After, skills installed** in all roots (per §1 setup): repeat the no-skill-relevant workload. Expect: zero `read_skill` calls, same cache profile (FR-011 — the catalog block is prefix-cached after the first request).
4. **Prefix stability**: run the automated prefix-stability suite (byte-identical system prompt + toolset across consecutive turns) — required workflow gate; plus one manual check that `prefix_shape.json` attributes the upgrade epoch on first run rather than logging a silent cold start.
5. Record all raw results under `specs/013-auto-skills-ui-polish/` (bench notes) so the comparison is reproducible.

## 6. Done Bar

- All §0 gates green (fmt, vet, full test suite; race-enabled on CGO runners).
- Every table row above observed as expected; §5 evidence recorded.
- Docs updated (`README.md`, `docs/agent-design.md`, `docs/architecture.md`, `docs/security.md` review note for `read_skill`).
