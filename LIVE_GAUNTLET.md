# MuhiyaCode Live Acceptance Gauntlet

The human-driven final check for the Stability Overhaul (T045/T062). The offline
gauntlet (`go test ./internal/orchestrator/ -run TestGauntlet`) proves the harness
in isolation; this proves it on your real machine, live. ~30 minutes.

## 0. Build & verify (once)

```powershell
.\scripts\swap-global.ps1        # gate → stamped build → overwrite the npm vendor exe
muhiyacode doctor                 # must be all "ok" — especially the launch/build lines
```

Then **fully close and reopen your terminal** so the new binary is what runs.
Confirm: `muhiyacode --version` shows a real commit + date (not "unknown"), and
`muhiyacode doctor` reports `launch: the muhiyacode command matches THIS build`.

## 1. The runs

Run each prompt in a **fresh, empty folder** unless it says "existing repo".
After the whole session, run `/errors` — the **pass bar is ZERO gate/ui events**.

| # | Prompt / action | What must happen |
|---|-----------------|------------------|
| 1 | *"Build a small Go CLI called taskflow with add/list/done, saved to JSON. Include a test."* | Research **skipped** (no subagents on an empty folder); plan accepted on the **first** try; **Proceed now** does NOT freeze. |
| 2 | In an **existing repo**: *"Add a --version flag and a test for it."* | Research runs and **scales** (1–3 agents by repo size); implements; validates. |
| 3 | Start a long task, press **Esc** mid-run, then type *"proceed"* | Stops cleanly ("interrupted"), then **resumes** — no lost work. |
| 4 | *"Audit auth/, billing/, and reports/ for missing error handling."* | One research agent **per named directory** (per-dir fan-out). |
| 5 | During any subagent run, watch the shell commands | `go build`, `dir /s /b 2>nul`, `grep format x.go`, `go env` all **run** — never "Blocked". |
| 6 | After a task that edited files: `/rewind` | The edits are **restored** (undo works). |
| 7 | `/errors` | Shows the session's harness friction, or "clean run". |
| 8 | `/context` | Full-session credits (main + subagents) + per-model breakdown. |
| 9 | Turn off Wi‑Fi, send any prompt | A **friendly** error (not a raw HTTP/stack dump); session survives. |
| 10 | `muhiyacode doctor` again | Still all "ok". |

## 1a. Experience Overhaul — Part A (TUI)

| # | Prompt / action | What must happen |
|---|-----------------|------------------|
| 11 | Cycle `/reasoning` through low → medium → high → max | The chip at the **bottom-right, under the input box** reads only `Low`/`Medium`/`High`/`Max` (never "Reasoning: …"), in a **different color per level**; the header no longer shows a reasoning segment. |
| 12 | Run a multi-step task and watch the zone above the input | One status line `✻ <verb> · <Ns> · <tokens> · cache N%` with breathing room; **no "Thinking" text anywhere**; a **checkbox to-do list** (`☒`/`▸`/`☐`) ticks off live as steps complete; `Ctrl+T` hides/shows it; it disappears when the task ends. The transcript row reads `To-dos updated: N/M done`. |
| 13 | Interrupt a plan mid-execution (Esc), then restart the session | Idle shows `☐ N to-dos remaining — say 'proceed' to resume`; typing `proceed` resumes it. |

## 1b. Experience Overhaul — Part B (agent memory)

| # | Prompt / action | What must happen |
|---|-----------------|------------------|
| 14 | Tell the agent a durable fact (e.g. *"remember that this project uses pnpm, not npm"*) | A **Memory** row appears; in normal mode the approval prompt names a path under `~/.muhiya/projects/<id>/memory/` (the store), **not** the repo. For a bigger cluster, the agent may file it under a topic (row shows the topic). |
| 15 | Restart the session, then ask something a saved fact answers, then something a topic answers | The index fact is used directly; the topic is fetched via a **`Memory · <topic>` `recalled`** row before answering. |
| 16 | If this workspace had a `MEMORY.md` before this build: check the repo and the store | The old `./MEMORY.md` is **untouched** (never deleted), and its content was **copied once** into `~/.muhiya/projects/<id>/memory/MEMORY.md` with a "migrated from …" comment. The repo file is no longer loaded. |

## 2. Pass criteria

- **Zero** `gate` or `ui` events in `/errors` across the whole session.
- **Zero** terminal freezes.
- **Zero** raw stack dumps or `commit unknown`.
- Every failure message is actionable (tells you what to do).
- The footer's `credits N (session)` covers subagent spend.

If any run fails, the crash report (if any) is under your temp dir as
`muhiyacode-crash-<pid>.log`, and `/errors` has the harness trace — attach both.
