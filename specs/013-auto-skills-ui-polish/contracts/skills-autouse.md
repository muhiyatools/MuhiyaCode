# Contract: Automatic Skill Use

**Feature**: `013-auto-skills-ui-polish` | Covers FR-001..FR-011 | Supersedes the workspace-only listing rules of feature 003's `skills-delivery.md` §2 (upgrade epoch)

## 1. Discovery & Catalog

- C1.1 The session skill catalog is discovered **once** at session build from, in precedence order (first-wins dedupe by lowercased name): workspace `.agents/skills`, workspace `.codex/skills`, each `MUHIYA_SKILLS_DIR` entry, `CODEX_HOME/skills` (default `~/.codex/skills`), `~/.agents/skills`.
- C1.2 Bound: 40 skills; per-file: only files named `SKILL.md` (case-insensitive), ≤4 directory levels below a root. Unreadable/malformed files are skipped silently (FR-009). Discovery failures of a whole root are non-fatal.
- C1.3 Ordering: stable sort by lowercased name. Identical installed files + configuration ⇒ byte-identical catalog every session (Constitution III).
- C1.4 The catalog is frozen for the session lifetime; installs/removals take effect at the next session (spec edge case).
- C1.5 Startup budget: all-roots discovery must keep session start within SC-003 (≤5% over baseline); the walk bounds above are the mechanism (no content reads at discovery time — frontmatter scan only).

## 2. Prefix Section (`SKILLS`)

- C2.1 Rendered iff the catalog is non-empty, appended to the system prompt exactly where the current section sits.
- C2.2 Byte-fixed format per entry (paths are no longer rendered):

```text
SKILLS
<hint body — see §5>
- <name>: <single-line description ≤200 runes>
- <name>            ← entries with empty descriptions render name-only
```

- C2.3 The section is part of the stable prefix: byte-identical across turns and tasks within a session; deterministic across sessions given identical inputs (verified by §6 checks).

## 3. `read_skill` Tool

- C3.1 Definition (session-static, main-loop toolset only — **not** granted to sub-agents):
  - name: `read_skill`
  - description: reads one installed skill's full instructions by name, from the SKILLS list
  - input schema: `{ "name": { "type": "string" } }`, required `["name"]`
- C3.2 Resolution: case-insensitive catalog lookup. Hit → return the trimmed body loaded via the existing 32 KiB bound. Body oversize/read failure → error string naming the skill and the reason (no paths for non-workspace roots).
- C3.3 Unknown name → error: `unknown skill "<name>" — only skills listed in SKILLS are available` (no filesystem probing, no path disclosure).
- C3.4 Dedupe (FR-008, Constitution V): if the name is in the task's provided-set (manual `<skill name="…">` markers scanned at task start, or a prior successful `read_skill` this task), return exactly: `skill "<name>" was already provided in this task — apply it from where it appears above.` The body is never transmitted twice within a task.
- C3.5 Security invariants (reviewed against `docs/security.md`): the tool accepts **no path input**; it can serve only cataloged SKILL.md bodies from the documented roots; secret-redaction and file-tool containment are untouched; skill bodies pass through the same output pipeline as other tool results.
- C3.6 Result placement: ordinary tool result in the conversation tail — append-only, never folded into the prefix (Constitution IV/V).

## 4. Sub-agent Equipping (`run_subagent.skills`)

- C4.1 `run_subagent` input gains optional `skills: string[]`. Schema is session-static.
- C4.2 Semantics: for each name, resolve per C3.2/C3.4 rules against the same session catalog; render into the delegated brief, after the task text, as the established wrapper: `Apply these skill instructions throughout this assignment.` followed by `<skill name="…">body</skill>` blocks in the given order (deduped, each name at most once per dispatch).
- C4.3 Unknown/unloadable names degrade to a single notice line inside the brief (`skill "<name>" unavailable`) — the dispatch still runs (graceful, FR-009).
- C4.4 Sub-agents receive no catalog, no `read_skill`, and no skill discovery (Clarification Q5). Static per-kind system prompts and Allowed toolsets are unchanged so per-kind cache identity is preserved (feature 012 compatibility).
- C4.5 On a continued stream (feature 012 linking), equipped skills ride the delta brief; a skill already equipped on the SAME stream earlier is not re-sent (engine tracks per-run-stream provided-sets the same way as per-task, Constitution V).

## 5. Prefix Hint (instruction text)

- C5.1 `prompt.skills.hint` body is replaced with guidance that (registry `MentionsTools: ["read_skill", "run_subagent"]`):
  1. names `read_skill` as the single way to load a skill;
  2. instructs: when a task (or a later step of it) falls in a listed skill's territory, read that skill **before** starting the related work — then keep applying it through the rest of the task (FR-004, FR-007);
  3. instructs blending: the skill informs the work; the model keeps its own reasoning and style — never verbatim template-following (FR-005);
  4. permits multiple skills per task and mandates none for unrelated tasks (FR-006);
  5. instructs equipping delegated work via `run_subagent.skills` when the delegated assignment falls in a skill's territory (FR-010);
  6. states manual `<skill>` blocks in the prompt take precedence and must not be re-read (FR-008).
- C5.2 The hint is session-invariant text registered in the instructions registry (Audience MainStatic, Cache Prefix) — the tool-mention drift guard (DC1) must pass.

## 6. Determinism & Cache Verification (workflow gate)

- C6.1 Byte-stability: `SystemPrompt(ctx)` with an all-roots listing is byte-identical across double construction (extend DG-5 test) and across turns within a session (prefix-stability check).
- C6.2 Toolset stability: serialized tool definitions (including `read_skill` and the extended `run_subagent`) are byte-identical across consecutive turns.
- C6.3 Upgrade epoch: first run of the new binary changes `SystemHash` + `ToolsHash` in `prefix_shape.json` → the resume/cold-start notice attributes the one-time invalidation (never a silent cold start).
- C6.4 SC-011 guard: the standard multi-turn no-skill benchmark shows steady-state hit rate within variance of the pre-change baseline (procedure in [quickstart.md](../quickstart.md) §5).
