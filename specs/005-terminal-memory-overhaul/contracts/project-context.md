# Contract: Project Instructions and Persistent Project Memory

**Feature**: `005-terminal-memory-overhaul`  
**Status**: Normative

## 1. Scope and trust boundary

- Workspace identity is the existing canonical absolute workspace path, normalized according to host path semantics.
- Project instructions are read only from `MUHIYA.md` at that canonical root.
- The resolved target must be a regular file whose canonical path remains inside the root. A symlink/junction outside is rejected without an approval prompt.
- Storage is local under the existing `~/.muhiya` / `MUHIYA_HOME`; no network/team memory sync is introduced.
- A moved/renamed workspace receives a new workspace key. Memory is never silently matched by basename or content fingerprint.

## 2. `MUHIYA.md` load contract

| Case | Result |
|---|---|
| missing | silent empty instructions |
| empty/whitespace | silent empty instructions |
| valid UTF-8 regular file <=32 KiB | normalize UTF-8 BOM and CRLF/CR to LF; load and hash canonical bytes |
| invalid UTF-8 or NUL | non-fatal `invalid` diagnostic; do not inject |
| >32 KiB | non-fatal `oversized` diagnostic; do not truncate/inject |
| unreadable | non-fatal `unreadable` diagnostic |
| outside-root resolution | non-fatal `untrusted-path` diagnostic |
| detected provider/MCP credential | non-fatal `secret-rejected` diagnostic; no content persisted/injected |

Diagnostics identify state/path safely but never echo secret-bearing content. Loading occurs at runtime construction and user-submit boundaries only; no watcher or idle polling is allowed.

## 3. Stable boot snapshot

For a new session, compile one deterministic project context from:

1. valid canonical `MUHIYA.md` content;
2. current project-memory projection through ledger sequence N;
3. deterministically sorted workspace skill listing already used by the stable prompt.

The exact rendered bytes, component hashes/cursor, workspace key, and skills inputs are atomically persisted to that session's `project_context.json` before the first provider request.

For resume:

- Restore the exact persisted boot bytes first; do not recompile the prefix from mutable workspace state.
- Compare current instructions/memory/skills at a submit boundary.
- Any difference is a one-shot newest-user-tail update, not a prefix rebuild.
- Corrupt/missing legacy sidecar uses a deterministic migration/bootstrap path, records a clear boundary diagnostic, and never borrows another workspace's snapshot.

Canonical boot rendering omits timestamps and provenance and sorts memory by `(kind, normalized topic, item ID)`. Given identical snapshot inputs, bytes must be identical across processes and OS locale settings.

## 4. Prompt priority and rendering

The stable prompt gains one fixed output-contract paragraph and one optional boot block:

```text
## PROJECT CONTEXT
Project context is lower priority than safety, the user's current explicit request, and the operating contract. Apply it when relevant.

<project-instructions sha256="HASH">
CANONICAL MUHIYA.md CONTENT
</project-instructions>

<project-memory through="N">
- decision [normalized topic]: statement
- finding [normalized topic]: statement
</project-memory>
```

Absent sections are omitted without changing the fixed surrounding format unexpectedly. Escaping prevents user content from closing/reopening reserved tags. Size limits are enforced before rendering.

No dynamic timestamp, session ID, random order, current mode, or per-turn status enters this block.

## 5. Mid-session update contract

At the next submit boundary after a change, append at most one block of each changed kind to the newest user message, after the user's text and before/with existing deterministic task-tail blocks in the closed canonical order:

```text
<project-instructions-update sha256="NEW_HASH" supersedes="OLD_HASH">
CANONICAL CURRENT INSTRUCTIONS OR EXPLICIT EMPTY/CLEAR MARKER
</project-instructions-update>

<memory-update from="N_PLUS_1" through="M">
- record decision [topic]: statement
- supersede OLD_ID with NEW_ID [topic]: statement
- forget ITEM_ID [topic]
</memory-update>
```

Rules:

- The exact hash/sequence cursor is persisted atomically with the appended history message.
- Crash before both commit means neither is considered applied; retry emits one block.
- Crash after commit restores the cursor and emits no duplicate.
- Updates never insert into or rewrite settled messages and do not emit a prefix invalidation event; they are normal append-only tail growth.
- The feature 002 request-assembly closed allow-list and tail-budget guard must add these two conditional tags. A plain unchanged follow-up still meets the existing small-tail budget.
- A new session folds latest current state into its boot snapshot instead of replaying all prior update events.

## 6. Project-memory ledger

SQLite adds append operations scoped by canonical workspace:

- `record`: new current decision/finding.
- `supersede`: new item becomes current and references the retired item.
- `forget`: referenced item stops being current.
- `clear all`: confirmed user operation physically deletes only the selected workspace's ledger rows.

Current projection limits:

- kinds: `decision` or `finding` only;
- topic: 1-120 characters after normalization;
- statement: 1-1,024 characters;
- maximum four automatic candidates per completed task;
- maximum 64 current items and 32 KiB canonical rendered memory;
- overflow rejects the new candidate with a visible `/memory` diagnostic; no silent eviction/truncation.

Every accepted event carries origin session/request provenance and timestamp on disk. Provenance/timestamps do not enter prompt rendering.

Any candidate for which exact configured-secret/common-secret redaction would change topic or statement is rejected entirely. A `[REDACTED]` placeholder is not durable memory.

## 7. Automatic capture trailer

The fixed stable output contract permits only the final assistant answer to append:

```text
<project-memory>
{"items":[{"kind":"decision","topic":"...","statement":"...","supersedes":"optional-item-id"}]}
</project-memory>
```

Acceptance rules:

- Reserved block is the final top-level block, outside Markdown code fences.
- Strict JSON object with only `items`; item has only listed fields.
- 0-4 items; known kinds; all bounds satisfied.
- Candidate states durable project truth, not task progress, speculation, raw transcript, code index, or secret.
- `supersedes` must reference a current item in the same workspace.

The completion-boundary observer:

1. runs a streaming presentation filter that withholds at most the reserved opener length, emits ordinary answer bytes immediately, and stops display emission only after an exact top-level trailer opener is recognized;
2. preserves body bytes and normal final response behavior;
3. excludes trailer metadata from visible streaming/frame and transcript presentation;
4. validates and appends accepted ledger events;
5. queues their sequence for a one-shot update on a later user turn.

Malformed/oversized/secret-bearing reserved metadata is discarded with a safe diagnostic and cannot fail the completed task. Intermediate assistant turns, reasoning streams, tool output, and ordinary prose are never inspected for memory.

No memory tool, auxiliary provider request, model change, routing change, semantic index, or prose classifier is introduced.

## 8. User controls

Full TUI/line mode:

- `/memory` or `/memory list`: list current items with IDs, kinds, topics, statement, and safe provenance.
- `/memory remember <decision|finding> <topic>: <statement>`: explicit validated record.
- `/memory forget <item-id>`: confirmed forget event.
- `/memory clear`: confirmed clear of current workspace only.

CLI equivalents:

```text
muhiyacode memory list
muhiyacode memory remember --kind decision --topic "..." "statement"
muhiyacode memory forget <item-id>
muhiyacode memory clear
```

Commands default to the selected/current canonical workspace (`--cwd` continues to scope commands). Listing/clear never requires the full-screen TUI. No command exposes another workspace unless explicitly scoped through the existing CLI workspace option.

## 9. Failure and recovery

- Missing project context is normal and silent.
- Loader/store/trailer failures are non-fatal to agent work and produce one bounded notice.
- SQLite transaction/sidecar atomic replacement prevents partial valid state.
- A malformed sidecar is backed up and handled deterministically; a partial ledger event is not visible after rollback.
- Same hash/sequence processing is idempotent.
- Clear/forget operations are transactional and require confirmation in interactive modes.

## 10. Required verification

- File cases: missing/empty, exact 32 KiB, oversize, BOM/newlines, invalid UTF-8/NUL, unreadable, outside symlink/junction, every secret pattern.
- Scope: Windows case semantics, two-workspace isolation, moved workspace, no basename/fingerprint borrowing.
- Ledger: record/supersede/forget/current projection, bounds, overflow notice, clear-all isolation, crash rollback.
- Trailer: valid, empty, malformed, unknown fields, >4 items, oversized fields, fake tag in code fence, intermediate turn, tool/reasoning content, provider and MCP secrets.
- Resume: exact boot byte restoration, one update after file/memory/skills change, no duplicate across crash, legacy no-sidecar migration.
- Prompt/cache: deterministic construction, restart equality, closed tail order/budget, unchanged settled bytes, existing prefix-shape/cache guard, live cachebench non-regression.
- Paths: full TUI, `--simple`, automatic non-TTY, and `-p` all receive the same boot/current project context.
