# Contract: Skills Delivery

**Feature**: `003-tui-ux-overhaul` | Serves FR-023..025, FR-027, SC-008/009 | Research D12/A10/A13; constitution III/IV/V; feature 002 FR-013 determinism

## 1. Discovery snapshot (session-pinned, workspace-scoped advertisement)

- `workspace.DiscoverSkills` runs **once at new-session creation** (wired in `internal/command/application.go`); the result is the session's immutable skill set. No per-turn or per-prompt re-discovery.
- **The advertised listing (§2) includes workspace-resident skills only** — roots `<workspace>/.agents/skills` and `<workspace>/.codex/skills`. Rationale: the model loads skills via `read_file`, and the permission guard (`Guard.ApprovePath`, `internal/workspace/permissions.go:109-124`) makes every out-of-workspace read interactive (or denied when headless) — advertising unreadable paths would either spam approval prompts or fail SC-009. External roots (`extraRoots`, `MUHIYA_SKILLS_DIR`, `CODEX_HOME/skills`, `~/.agents/skills`) remain discoverable for the **manual `/skills` flow only**, which already loads instructions outside the guard (`LoadSkillInstructions`/`os.Open`, `skills.go:21-38`). No security-model change (plan.md Constitution IX holds).
- Existing semantics otherwise kept: first-root-wins dedupe by lowercased name, cap 40, stable sort by lowercased name (`skills.go:40-92`).
- Same machine + same config + same files ⇒ identical snapshot ⇒ byte-identical listing (constitution III "deterministic given identical configuration").
- **Resume/restart rule**: the snapshot is **persisted with the session** (sidecar, same pattern as 001's ProbeSnapshot — `application.go:550-564`) and restored verbatim on `/resume` and process restart, so the `## Skills` section — and thus the cached prefix — is byte-identical across save/load even if skill files changed on disk. Re-discovery happens only when a new session is created (`/new`, first launch).

## 2. The listing (stable-prefix content)

Appended to `SystemPrompt` (`internal/orchestrator/prompt.go`) as a final section, only when the snapshot is non-empty:

```
## Skills

Workspace skills you can apply. When a task matches a skill's purpose, read its file with read_file and follow its instructions. Otherwise ignore skills entirely.

- code-review (.agents/skills/code-review/SKILL.md): Review changed code for correctness and style before committing.
- release-notes (.agents/skills/release-notes/SKILL.md): Draft release notes from merged PRs and the changelog.
```

Format rules (normative):
- One `- <name> (<path>): <description>` line per skill; sorted by lowercased name. A skill with an **empty description** (frontmatter-less SKILL.md — `readSkill` falls back to the directory basename for the name and leaves description empty) renders the fixed form `- <name> (<path>)` — no trailing colon, deterministic.
- `path`: **workspace-relative** (guaranteed by §1's workspace-only scope); forward slashes on all platforms (determinism + model ergonomics); points at the `SKILL.md` file readable by the existing `read_file` tool within the containment boundary.
- `description`: frontmatter description, newlines collapsed to spaces, hard-truncated at 200 chars with `…`.
- Exactly two fixed guidance sentences (above), byte-frozen — they are part of the stable prefix.
- Empty snapshot ⇒ the entire section (heading included) is absent.
- **Prohibited in the section**: timestamps, counts, environment paths that vary per launch (e.g. no `~` expansion differences), skill bodies, dynamic ordering.

## 3. On-demand loading (dynamic content)

- The model reads a skill's `SKILL.md` via the existing `read_file` tool when — and only when — the task matches. The read is an ordinary tool call: visible in the transcript (rendered per [tool-display.md](tool-display.md)), subject to the duplicate/covered-read blocking (constitution V), and its content rides the turn, never the prefix (constitution IV).
- `/skills` manual selection remains as an explicit override with its current `<skill name="…">…</skill>` user-turn injection (`model.go:560-576`). Change: `listSkills` (`application.go:744-761`) stops eager-loading all instruction bodies on modal open; bodies load lazily for **selected** skills at submit time only (32 KiB cap unchanged).
- No new tools, no auto-injection of skill bodies, no per-turn skill hints (FR-024's "nothing more").

## 4. Cache-safety obligations

- The listing changes the prefix **only between sessions** (snapshot immutability + resume persistence, §1); within a session — including across `/resume` and process restart — the prefix stays byte-identical → no invalidation events attributable to skills.
- `prompt_stability_test.go` gains fixtures: (a) `SystemPrompt` byte-identical across constructions with a non-empty skill set; (b) listing order independent of discovery/registration order; (c) empty-set produces byte-identical prompt to today's (modulo intended section absence); (d) path separators normalized on Windows; (e) a frontmatter-less SKILL.md renders the fixed no-description form.
- `restart_determinism_test.go` gains a skills fixture: persisted snapshot ⇒ `SystemPrompt` byte-identical across save/reload even when a SKILL.md changed on disk between runs (the test's existing "no filesystem-dependent prompt inputs" premise comment must be updated — the snapshot, not the filesystem, is the input).
- `cachehit_guard_test.go` scenarios re-run green with skills fixtures present.
- Prefix growth budget: listing ≤ 40 lines + header ≈ ≤ 4 KB — the FR-024/SC-008 bound ("no more than the compact skill listing").

## 5. Behavioral acceptance (SC-009)

Trial protocol (quickstart scenario 6): 10 tasks matching a fixture skill's stated purpose + 5 unrelated tasks, fresh session each.
- Matching: agent `read_file`s the right SKILL.md and observably follows it in ≥9/10.
- Non-matching: zero skill reads, zero skill mentions in output (FR-025).
- Cache: before/after benchmark (D14) shows session hit-rate non-regression and prompt-token growth ≤ listing size (FR-027).

## 6. Failure modes

| Case | Behavior |
|---|---|
| SKILL.md deleted mid-session | `read_file` fails normally; agent proceeds without the skill; listing unchanged until next new session (prefix immutability wins — documented, acceptable) |
| Skill read denied / approval declined | agent proceeds without the skill (ordinary tool failure); cannot occur for advertised skills under §1's workspace-only scope — guard prompts apply only to manual out-of-workspace flows |
| External-root skills (env/home dirs) | never advertised in the prefix; available via the `/skills` modal only (§1) |
| >40 skills | deterministic first-40 by root order + name sort (existing cap); count not shown to user — `/skills` modal remains the full-visibility surface |
| Malformed or missing frontmatter | skill is **still discovered** (`readSkill`, `skills.go:122-129`, falls back to the directory basename when `name` is absent; description may be empty); renders the deterministic no-description form per §2 — never a malformed listing line |
| Skill description contains `read_file`-bait or contradictory instructions | out of scope for this contract beyond truncation; skills are user-installed local files (same trust tier as workspace CLAUDE.md-style config) |
