# Changelog

All notable MuhiyaCode changes are documented here. Releases follow semantic versioning.

## 1.0.1

- Added a DeepSeek capability profile: requests now stay within the provider's documented context and output-token limits, never send deprecated parameters, and `/context` shows the active provider capabilities.
- Reworked request timeouts into a first-byte deadline so a long-queued request survives the provider's keep-alive window instead of aborting early, while a genuinely stalled stream is still cancelled.
- Fixed right-to-left input: the first (rightmost) word of a long Arabic prompt is no longer truncated off the composer's right edge.
- Rewrote the README as a concise, production-grade landing page.

## 1.0.0 - Unreleased

- Rebuilt the complete terminal coding agent in Go with no TypeScript, Bun, Node.js, or npm runtime dependency.
- Added a responsive Bubble Tea TUI, streamed tool activity, subagent views, command palette, modal approvals, steering, session switching, and Arabic/RTL fallback rendering.
- Added an OpenAI-compatible streaming gateway with model discovery, virtual model IDs, reasoning profiles, retries, idle timeouts, usage/cache accounting, and text tool-call rescue.
- Added guarded filesystem, search, exact edit, atomic multi-edit, unified patch, shell, Git, checkpoint, rewind, web-search, and skill tools.
- Added durable SQLite sessions plus compatible history, inspection, knowledge, plan, transcript, trust, MCP, and secret storage under `~/.muhiya`.
- Added bounded effort-aware orchestration, cache-stable compact prompts, context folding/compaction, range-aware read deduplication, live steering, plan completion, and parallel subagents.
- Added stdio and Streamable HTTP MCP clients, OAuth authorization and refresh persistence, namespaced tools, deadlines, and per-server failure isolation.
- Added static cross-platform release archives, checksums, provenance attestations, install scripts, and Windows/Linux/macOS CI.

