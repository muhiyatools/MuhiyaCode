# V3 — Manual scenario walkthroughs

These five scenarios exercise the interactive flows that cannot be fully
automated without a live gateway and a real terminal. Run each in the TUI
(`muhiyacode` with a valid API key), record the outcome, and check the box.

Prerequisites: a configured API key (`/login` or `muhiyacode config set apiKey`),
a workspace with a small codebase, and at least one MCP server configured if
exercising scenario 3.

## 1. Plan flow

- [ ] `/plan build a small feature` — the agent researches read-only and
      produces a plan. Confirm **zero mutating tool executions** during
      planning (watch for `Blocked:` tool results; at most 1–2 early ones,
      then none after the P4 escalation at violation count 3).
- [ ] Plan completes → a **"Plan is ready"** modal appears with three choices:
      Proceed now / Proceed later / Keep planning.
- [ ] Select **Proceed later** → plan mode turns off, notice reads
      "Plan saved. Say 'proceed' (or 'go ahead') any time to execute it."
- [ ] Send `proceed` → the plan executes (the `[executing saved plan]` block
      is injected into the brief; plan steps are worked through in order).
- [ ] **Restart mid-plan**: close the app during a planning task, reopen the
      same session → notice reads "Plan mode restored" (or
      "Saved plan pending — say 'proceed'" if pendingPlan was set). Verify the
      plan content and mode flag survived the restart via `goal.json` /
      `plan_state.json` sidecars in the session directory.

## 2. Goal flow

- [ ] `/goal <objective>` → the agent works autonomously, ending each reply
      with exactly one `[goal:...]` marker.
- [ ] Let it complete (model emits `[goal:complete]`) → verify the
      "Goal complete: <text>" notice, `/goal` shows no active goal, and the
      next unrelated task's request contains **no `[active-goal]` block**.
- [ ] `/goal x` followed by `/plan` → goal is cleared with a
      "Goal cleared — plan mode is read-only" notice (G3 exclusion); and the
      reverse (`/plan` then `/goal x`) disables plan mode with
      "Plan mode disabled — goal mode is now active."
- [ ] **Restart with active goal**: close the app while a goal is active,
      reopen → notice reads "Restored active goal: <text> (/goal clear to
      drop)". Verify the goal.json sidecar was written and the goal resumes.

## 3. MCP

- [ ] Add an OAuth MCP server (`/mcp add ...`) → Authorize → the `/mcp` modal
      shows the server as `connecting…` (spinner glyph) and **flips to
      connected** without reopening (M2 live refresh).
- [ ] Kill the server process → the next `mcp__<server>__*` tool call marks
      it `error` and the tool returns the H8 unavailable message. A
      subsequent call reconnects once (M8 liveness); a second failure stays
      `error` until a manual Test.
- [ ] Boot a session where every configured server has a cached surface
      (pinned schema on disk) → confirm via the process list that **no child
      processes spawn** until the first `mcp__` tool use (M5 lazy connect).

## 4. Usage footer

- [ ] Run a task → after it completes, the usage summary (duration, effort,
      class, billed, cached, new, tools) stays visible as a persistent footer
      line above the input box.
- [ ] Wait 6+ seconds → the footer is **still present** (no timer clears it).
- [ ] Trigger an unrelated MCP warn notice → the footer **survives** the warn.
- [ ] Send a new prompt → the footer **disappears** (cleared on submit).

## 5. Cancellation

- [ ] Start a task that calls multiple tools in one turn. Press `Ctrl+C` mid-
      batch (while tools are executing).
- [ ] Send a new prompt → the next request assembles without a provider 400
      about unpaired tool calls (H4 cancel-safe pairing: every tool_call in
      the just-appended assistant message has a matching tool result, real or
      synthetic `[cancelled by user before execution]`).
