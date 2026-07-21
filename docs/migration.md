# Migration from the TypeScript release

The Go rebuild uses the same `~/.muhiya` contract so users can switch binaries without exporting sessions. It does not use the TypeScript source tree or runtime.

## Before switching

1. Exit every running MuhiyaCode process so SQLite and session JSON files are quiescent.
2. Back up the whole Muhiya home directory (`~/.muhiya`, or `MUHIYA_HOME`).
3. Keep the original TypeScript binary available until the Go build has opened the expected sessions and configuration.
4. Run `muhiyacode doctor --offline`, then `muhiyacode doctor` when endpoint access is available.

No explicit conversion command is needed. The Go binary reads and continues to write the v1 files in place.

## Compatibility matrix

| Artifact | Compatibility |
|---|---|
| `settings.json` | Reads v1 provider/model settings and legacy `provider.model`; maps legacy `min`/`ultra` effort aliases onto the four-level dial |
| `secrets.json` | Reads/writes the existing provider API key shape and reapplies user-only permissions |
| `mcp.json` | Reads/writes stdio and HTTP server definitions; missing HTTP OAuth defaults are normalized |
| `mcp-secrets.json` | Preserves OAuth/client/discovery fields and stdio environment secrets; Go adds an absolute token expiry and refresh metadata when available |
| SQLite | Uses the same trust, session, event, and checkpoint tables/columns; adds only indexes |
| `history.json` | Uses the same v1 structured message/tool-call shape |
| `inspection.json` | Uses the same v3 signatures, line coverage, fingerprints, inspected-file, and full-read shape |
| `knowledge.json` | Uses the same v1 facts/files/edit-epoch shape and task-key algorithm |
| Transcript/checkpoints | Keeps the existing file names and formats |
| Plan sidecars | Retired in v1.1.0: the session directory's `plan.md`, `plan_state.json`, `goal.json`, and the session-dir `tasks.md` twin are no longer read or written. Stale copies are ignored, never treated as corruption, and left on disk for you to delete |
| `tasks.md` | A plain markdown checklist maintained by the model with ordinary file tools — usually at the project root, or created beside the work for a task scoped to one part of it. It is not a session artifact and does not move with `~/.muhiya` |
| Subagent sidecars | Retired with the subagent system: the session directory's `agents/` transcript records are no longer read or written. Stale copies are ignored, never treated as corruption, and left on disk for you to delete |

Corrupt JSON is moved aside as a timestamped `.corrupt-*.bak` and replaced with a safe default. This behavior never deletes the backup. Retired sidecars are not corrupt files and are never backed up or rewritten — they are simply not opened.

## Upgrading to v1.1.0

v1.1.0 removes the goal system and the planning pipeline. The agent follows instructions directly:
the main model plans and instructs, and every workspace change is delegated to an execution
subagent.

### Removed commands

`/goal` and the goal auto-continue system are gone; state the work in your prompt instead.
`/model` is gone because a session's models are now fixed for its whole duration (see below).

### Removed tools

`update_plan`, `exit_plan_mode`, and `read_plan` no longer exist. Planning Mode, the
research/planning/approval/implementing/validating pipeline, the 11-state lifecycle, and the
"Proceed with Plan" modal are removed with them. For multi-step work the model keeps a plain
markdown checklist (`- [ ] item` lines) in `tasks.md` at the project root, written with ordinary
file tools — there is no dedicated tool and no approval step. Add `tasks.md` to `.gitignore` if
you do not want it tracked.

### Pinning models

Three roles now carry a session: a main model that plans, an execution model that runs in
subagents, and a cheap utility model for auxiliary calls. On the first prompt of a session an
advisor confirms the pairing; after that the models are frozen, because switching mid-session
cold-starts the prompt cache and breaks the execution agent's context chain. To choose them
yourself:

```
muhiyacode config set model <id>            # main (planning) model
muhiyacode config set subagentModel <id>    # execution model
muhiyacode config set advisor off           # never propose a pairing
```

Either `set model` or `set subagentModel` marks the roles as user-pinned, so the advisor will
propose but never override your choice. `subagentModel` requires an id or name already in the
model catalog.

### One-time cold start

The system prompt and tool schemas changed, so the first request after upgrading cannot read the
previous cache. Resumed sessions pay one cold start, attributed as such in `/context` and the task
summary, then return to normal steady-state rates. The new prefix is smaller than the one it
replaces (system prompt 5777 → 5246 chars, tool JSON 20317 → 18578 bytes).

### Update notice

The header shows `Update available <new> (you have <current>)` when the npm registry has a newer
version — one cached GET per day, silent on failure. Set `MUHIYACODE_NO_UPDATE_CHECK` to disable
it. The "Welcome to MuhiyaCode" first-run cue is gone; an empty session now starts with an empty
transcript.

## Upgrading to the unified session

A later change removes the subagent system entirely. The plan/execute split introduced in
v1.1.0 — a main model that planned and instructed, and an execution sub-agent it dispatched to
do the actual editing — is gone. One model now reads, edits, runs shells, and verifies its own
work in a single session; nothing is delegated and there is no report to hand back.

### Removed tool

`run_subagent` no longer exists. There is nothing to delegate to, so there is no handoff to
compose: the model that used to describe a task for an execution agent now just does it.

### Removed config keys

`subagentModel` (the underlying `subagentModelId` setting) and `contextLinking` are gone —
`muhiyacode config set subagentModel <id>` and `config set contextLinking <value>` both fail with
"unknown config key". If your `settings.json` already has values stored under either key from
before the upgrade, they are silently ignored on load: not an error, and not migrated onto
anything else. They are harmless to leave in the file, or remove them at your convenience.

### Removed keybinding

`←` / `→` no longer switch between the main session and a running agent, and `Alt+1`…`9` no
longer jump to one — there are no agent views left to navigate to. Both keys now belong entirely
to the composer's text cursor.

### One model, chosen per task instead of three roles frozen per session

v1.1.0 froze three roles — main, execution, utility — once at the start of a session. Now there
is one model, and a lightweight advisor call chooses it fresh at the start of *every* task, not
just the session's first one; its choice is then frozen for that task's duration. The advisor
only moves you when the switch is affordable — the conversation is still small, or you are
returning to a model already warm this session — so a long-running session does not silently
eat a full cold start to chase a marginally better model. See
[prompt-caching.md](prompt-caching.md) for the mechanics. `config set model <id>` still pins the
model and `config set advisor off` still disables the advisor; `config set subagentModel` is gone
because there is only one role left to pin.

### Historical data

Usage records already tagged with the old `subagent` stream label (from sessions run before this
upgrade) still display correctly; the client simply never writes that label again.

## Rollback

The storage schemas remain readable by the last TypeScript release, but always restore the backup if you need a byte-for-byte rollback. Do not alternate two running implementations against the same home directory.

## Changed operational requirements

- Bun and Node.js are no longer needed.
- The distributed binary is self-contained and uses a CGO-free SQLite driver.
- `config discover` can populate models and context windows from the endpoint. A manual model still requires `config set contextLimit`.
- Go uses native OS process-tree cancellation and its own TUI/RTL renderer; terminal appearance may differ while behavior remains equivalent.

