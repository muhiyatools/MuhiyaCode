# MuhiyaCode Go architecture

MuhiyaCode is rebuilt as a Go product, not a transliteration of the Bun source.
The only compatibility boundary is the user's durable data in `~/.muhiya` (or
`MUHIYA_HOME`). No Go package imports, executes, or ships TypeScript.

## Layer map

- `internal/contract`: dependency-free values and ports shared across layers.
- `internal/state`: atomic configuration/secrets, SQLite sessions/events/trust,
  compatible session bundles, transcripts, memory, and checkpoints. The
  `plan.md` / `plan_state.json` sidecars are gone with v1.1.0's pipeline
  removal; the model's checklist is an ordinary workspace file.
- `internal/gateway`: OpenAI-compatible streaming, model discovery, usage
  accounting, retries/timeouts, tool-call rescue, and gateway web search.
- `internal/updatecheck`: one cached npm-registry GET per day behind a 24-hour
  TTL, feeding the header's "update available" line. Silent on any failure;
  `MUHIYACODE_NO_UPDATE_CHECK` opts out.
- `internal/workspace`: symlink-aware path policy, file/search/edit/diff tools,
  shell execution, approvals, risk checks, checkpoints, and skills discovery.
- `internal/mcpclient`: lifecycle-managed stdio and Streamable HTTP MCP clients,
  namespaced tool schemas, OAuth, deadlines, and failure isolation.
- `internal/orchestrator`: task sizing, effort budgets, prompt compiler,
  cache-stable history, inspection/knowledge ledgers, subagents, steering,
  compaction, the workspace checklist, and the bounded landing protocol.
  Feature 012 adds subagent context linking (`contextlink.go`/
  `contextrecord.go`: continuation-first stream reuse with verbatim transcript
  records under the session's `agents/` sidecars). v1.1.0 adds the plan/execute
  role gate (`rolesplit.go`), the `tasks.md` checklist observer
  (`checklist.go`), and the once-per-session model advisor (`advisor.go`).
- `internal/app`: the frontend-neutral core seam. It holds the types a frontend
  needs to drive a session — `Runtime`, `Actions`, `Skill`, `UsageData`, and the
  MCP view models — plus `app.AssemblePrompt`, the single prompt-assembly path
  every frontend calls. The `internal/tui` names are now aliases of these.
- `internal/tui`: Bubble Tea v2 presentation. It consumes runtime events and
  owns keyboard input, menus, responsive layout, RTL display, and accessibility.
- `internal/command`: Cobra command graph and application composition. It also
  refreshes the gateway model catalog on a 24-hour TTL, so the role defaults and
  the session advisor resolve against current entries.
- `cmd/muhiyacode`: a minimal executable entry point.

Dependencies point inward toward `contract`; presentation and commands compose
concrete implementations at the edge. `internal/app` sits above `orchestrator`
and below the frontends: it may import `orchestrator`, and it must never import
`internal/tui` or any other frontend. Those types previously lived in
`internal/tui` and were implemented by `internal/command`, which made the
composition root depend on the terminal renderer just to name its own return
values; extracting them is groundwork for the planned desktop app. Long-running
operations take a `context.Context`. Background work is owned by an explicit
lifecycle and must terminate on cancellation; no package-global worker
goroutines are allowed.

## Product invariants

1. Existing v1 settings, secrets, MCP config, SQLite rows, histories,
   checkpoints, and transcripts remain readable in place.
2. Secrets never enter normal settings, logs, persisted model-visible history,
   or terminal diagnostics. Secret files receive user-only permissions.
3. Workspace containment is checked against real paths/nearest existing
   ancestors, including symlinks and Windows junctions.
4. Every assistant tool call has exactly one ordered tool result, including
   cancellation and failure paths.
5. Across a session, model request prefixes are byte-stable. Folding, trimming,
   compaction, and tool-surface changes occur only at deliberate, attributable
   boundaries. The session's models are chosen once — by configuration, a pin,
   or the first-prompt advisor — and then frozen: after the first request no
   path switches the main or execution model, because a switch would cold-start
   the main prefix and break the execution agent's continuation chain. The user
   does not manage models; there is no `/model`.
6. All task classes use one stable full prompt and pinned tool surface. Task
   sizing sets budgets and turn caps only — it never selects a different
   execution path or prompt. Prompt and schema regression budgets are enforced
   in tests.
7. Independent subagents may run concurrently, but edits and ordinary tools
   stay ordered. Read-only subagents have physically restricted tool registries.
   There are exactly three capability classes — `explore`, `general`, `review` —
   and only `general` can change the workspace. Each kind carries its own
   provider cache pin (`:sub:<kind>`), an optional provider-reported token
   ceiling, and — for the review kind — a deterministic dispatch gate
   (feature 011) with a user-visible rationale. A run's LLM-chosen `role` name
   is display and handoff only: pin, system message, and context-record kind
   stay keyed on the fixed class, so a novel name cannot fragment the cache.
   The main model plans and instructs; the execution subagent changes the
   workspace. The dispatch gate refuses main-loop file writes, patches,
   mutating shell, and MCP calls with an actionable, escalating message, with
   exactly four carve-outs: the `tasks.md` checklist, read-only shell, memory
   writes, and every subagent scope. Per-class agent budgets (`0/1/1/2/5/8`)
   guarantee that any class able to change files can afford its executor.
8. A task never silently dies at a turn cap: it escalates once when warranted,
   receives a convergence warning, then lands with tools disabled and a factual
   final report.
9. The TUI remains usable at narrow widths, without color, and in non-TTY line
   mode. Meaning is never conveyed by color alone. Colors resolve from a semantic
   token palette (dark/light, `NO_COLOR`-aware); the terminal's own background is
   respected rather than force-filled, and all non-ASCII glyphs route through a
   single table with an ASCII fallback. Below a 60×20 floor the UI shows one
   clean "terminal too small" message instead of a collapsed layout.
10. Per-task cost is measured, never estimated: credits shown in the task summary
    and `/context` come only from the gateway's per-request `muhiya_log` cost
    field (USD, ×100 = credits), summed over the task's own usage records; any
    turn without a reported cost makes that task's credits unavailable rather
    than guessed. The `/usage` command reads account usage from a key-
    authenticated gateway endpoint (no admin credentials client-side).
11. Releases are static Go binaries for Windows, Linux, and macOS on amd64 and
    arm64, with one linker-injected version and checksumed archives.
12. The TUI is mouse-native: an immutable per-frame InteractionMap resolves every
    click/hover to the same action as its keyboard equivalent (command rows,
    modal/MCP choices, tool/subagent chips, the composer), and dragging over
    transcript text selects it with Ctrl+C copying via OSC 52. All of this is
    presentation-only — it never reaches the request-assembly path or the prefix
    cache. On very long sessions the in-memory transcript window is bounded (with a
    visible trim marker); the durable session transcript is never truncated.

## DeepSeek API alignment (feature 007)

The request chain (MuhiyaCode → Muhiya Gateway → DeepSeek) is aligned to DeepSeek's
documented behavior; full audit in `specs/007-deepseek-api-alignment/`.

13. **Timeouts follow DeepSeek's keep-alive contract.** `RequestLifetime` is now a
    first-byte deadline (connect + header wait, up to the documented ~10-minute
    pre-inference window); once the stream flows the rolling idle timeout alone
    governs, and `: keep-alive` comment / empty lines are liveness (they reset the
    idle timer), so a long answer after a long queue is never cut off. The gateway's
    upstream `ResponseHeaderTimeout` is likewise 630s.
14. **Stable, private per-user identity.** The gateway stamps a documented `user_id`
    on every DeepSeek request: `"mu-" + HMAC-SHA256(secret, accountID)`, opaque and
    server-authoritative (inbound `user`/`user_id` are stripped first). It gives the
    provider its documented per-user KVCache isolation without leaking account data;
    it is a top-level parameter, never part of the tokenized prefix.
15. **Valid parameters only.** Reasoning controls reach DeepSeek exclusively as the
    documented `thinking` object + `reasoning_effort: high|max` (no raw client value
    on any path); deprecated `frequency_penalty`/`presence_penalty` are never
    forwarded; a client-side capability profile (limits, deprecated set, beta
    adoption) keeps requests in range, surfaced read-only in `/context`.
16. **Cache-miss observability.** `request_logs.cache_miss_tokens` records the
    provider-reported miss count alongside the hit count; estimated rows leave it
    null (honest measurement). Cache accounting is otherwise unchanged.
