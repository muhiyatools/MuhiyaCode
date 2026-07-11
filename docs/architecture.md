# MuhiyaCode Go architecture

MuhiyaCode is rebuilt as a Go product, not a transliteration of the Bun source.
The only compatibility boundary is the user's durable data in `~/.muhiya` (or
`MUHIYA_HOME`). No Go package imports, executes, or ships TypeScript.

## Layer map

- `internal/contract`: dependency-free values and ports shared across layers.
- `internal/state`: atomic configuration/secrets, SQLite sessions/events/trust,
  compatible session bundles, transcripts, plans, memory, and checkpoints.
- `internal/gateway`: OpenAI-compatible streaming, model discovery, usage
  accounting, retries/timeouts, tool-call rescue, and gateway web search.
- `internal/workspace`: symlink-aware path policy, file/search/edit/diff tools,
  shell execution, approvals, risk checks, checkpoints, and skills discovery.
- `internal/mcpclient`: lifecycle-managed stdio and Streamable HTTP MCP clients,
  namespaced tool schemas, OAuth, deadlines, and failure isolation.
- `internal/orchestrator`: task classification, effort budgets, prompt compiler,
  cache-stable history, inspection/knowledge ledgers, subagents, steering,
  compaction, plan completion, and the bounded landing protocol.
- `internal/tui`: Bubble Tea v2 presentation. It consumes runtime events and
  owns keyboard input, menus, responsive layout, RTL display, and accessibility.
- `internal/command`: Cobra command graph and application composition.
- `cmd/muhiyacode`: a minimal executable entry point.

Dependencies point inward toward `contract`; presentation and commands compose
concrete implementations at the edge. Long-running operations take a
`context.Context`. Background work is owned by an explicit lifecycle and must
terminate on cancellation; no package-global worker goroutines are allowed.

## Product invariants

1. Existing v1 settings, secrets, MCP config, SQLite rows, histories,
   checkpoints, and transcripts remain readable in place.
2. Secrets never enter normal settings, logs, persisted model-visible history,
   or terminal diagnostics. Secret files receive user-only permissions.
3. Workspace containment is checked against real paths/nearest existing
   ancestors, including symlinks and Windows junctions.
4. Every assistant tool call has exactly one ordered tool result, including
   cancellation and failure paths.
5. Within a task, model request prefixes are byte-stable. Folding, trimming,
   and compaction occur only at deliberate boundaries.
6. Chat-class turns use the lite prompt and no local tool schemas. Toolful
   prompts and schemas have regression budgets enforced in tests.
7. Independent subagents may run concurrently, but edits and ordinary tools
   stay ordered. Read-only subagents have physically restricted tool registries.
8. A task never silently dies at a turn cap: it escalates once when warranted,
   receives a convergence warning, then lands with tools disabled and a factual
   final report.
9. The TUI remains usable at narrow widths, without color, and in non-TTY line
   mode. Meaning is never conveyed by color alone.
10. Releases are static Go binaries for Windows, Linux, and macOS on amd64 and
    arm64, with one linker-injected version and checksumed archives.
