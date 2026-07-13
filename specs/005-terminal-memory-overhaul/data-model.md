# Data Model: Terminal Experience and Project Memory

**Feature**: `005-terminal-memory-overhaul`  
**Date**: 2026-07-13

The model separates durable transcript/project data from bounded presentation state. Stable identities connect live events, persisted rows, rendered blocks, mouse targets, and scroll anchors without requiring the complete transcript in memory.

## 1. Transcript and Frame Model

### 1.1 TranscriptEvent

One durable transcript entry.

| Field | Type | Rules |
|---|---|---|
| `ID` | signed 64-bit integer | SQLite event ID; positive and monotonically increasing within the database |
| `SessionID` | string | valid existing session ID; immutable |
| `Role` | enum | `user`, `assistant`, `tool`, `agent`, `system` |
| `Kind` | string | existing event type; non-empty |
| `Content` | UTF-8 string | persisted/redacted authoritative content |
| `CreatedAt` | timestamp | persisted metadata; never used in deterministic prompt rendering |
| `LiveKey` | optional string | temporary identity for an uncommitted active stream/tool; reconciled to `ID` after persistence |

**Validation**:

- Durable records are ordered by `(SessionID, ID)`.
- A live event and its persisted replacement may not coexist as two visible entries; reconciliation replaces identity in place.
- Secret redaction remains the persistence boundary used by existing events/transcripts.

### 1.2 TranscriptPage

A keyset-paginated slice returned by the state layer.

| Field | Type | Rules |
|---|---|---|
| `SessionID` | string | page scope |
| `Generation` | unsigned integer | runtime/session generation that requested the page |
| `Entries` | ordered TranscriptEvent list | ascending ID; limited by entry and byte budget |
| `OldestID` / `NewestID` | optional integer | derived from entries |
| `HasOlder` / `HasNewer` | boolean | indicates another keyset page exists |
| `RawBytes` | integer | sum of loaded content bytes; used for cache budget |

**Validation**:

- A result is discarded if its session or generation differs from the active window.
- Page boundaries use `< beforeID` or `> afterID`; offset pagination is prohibited.
- One oversized entry may occupy a page by itself; its visible rows are still windowed.

### 1.3 ViewportAnchor

Logical scroll position independent of a giant line offset.

| Field | Type | Rules |
|---|---|---|
| `EventID` | integer/live key | first anchored visible entry |
| `IntraEntryRow` | non-negative integer | wrapped row within the entry |
| `FollowOutput` | boolean | true only when user follows the newest content |
| `PendingDeltaRows` | integer | temporary compensation while prepending/reflowing |

**Transitions**:

```text
following newest --wheel/page up--> anchored away (FollowOutput=false)
anchored away --explicit end/follow action--> following newest
following newest --new stream rows--> remains at newest
anchored away --new stream rows--> anchor unchanged
any --resize--> same EventID, recomputed IntraEntryRow in one transaction
```

### 1.4 TranscriptWindow

Bounded TUI presentation cache.

| Field | Type | Rules |
|---|---|---|
| `SessionID` / `Generation` | identity | changes atomically on session switch |
| `Entries` | ordered map by stable ID | visible page plus bounded overscan only |
| `Anchor` | ViewportAnchor | always resolves to a retained entry or nearest valid neighbor |
| `OldestCursor` / `NewestCursor` | optional ID | keyset fetch cursors |
| `HasOlder` / `HasNewer` | boolean | paging state |
| `RawByteBudget` | integer | default 8 MiB for retained raw presentation entries |
| `RenderedByteBudget` | integer | default 8 MiB for rendered blocks |
| `EntryBudget` | integer | default 384 retained entries, except one oversized active entry |
| `LoadState` | enum | `ready`, `loading-older`, `loading-newer`, `failed` |

**Eviction**:

- Evict farthest from the viewport, never the anchor, visible entries, or active live entry.
- Eviction removes matching rendered blocks, hit regions, and selection fragments in the same transaction.
- Full durable content remains queryable from SQLite.

### 1.5 RenderedBlock

Cached presentation of one entry.

| Field | Type | Rules |
|---|---|---|
| `EventKey` | stable ID/live key | cache owner |
| `Width` | columns | part of cache key |
| `ThemeVersion` | string/integer | part of cache key |
| `DetailState` | collapsed/expanded | part of cache key for tool/agent items |
| `ContentVersion` | monotonically increasing integer | changes only when that entry changes |
| `StyledRows` | list of strings | bounded rendered rows |
| `PlainRows` | list of strings | ANSI-free copy/selection text |

Width, theme, detail, or content changes invalidate only affected retained blocks. Completed immutable entries never gain a new content version.

### 1.6 FrameState

Cached complete visible frame and its event dependencies.

| Field | Type | Rules |
|---|---|---|
| `PaneContent` | map of pane to rendered string | header, transcript, command menu, notice/activity, input, mode line, modal |
| `DirtyPanes` | set | empty when the current frame is reusable |
| `Layout` | LayoutSnapshot | actual terminal geometry |
| `Interactions` | InteractionMap | produced from the same layout/content |
| `FrameSequence` | integer | increments only when visible state changes |
| `CompletedRowHashes` | optional list | benchmark/test observation only |

**Invalidation rules**:

- Input/key edit: input and possibly command menu only.
- Stream/tool chunk: active transcript block and activity only.
- Status/notice: status pane only unless height changes.
- Resize/theme/detail: layout plus retained visible blocks.
- Idle/no event: no dirty pane and no new frame sequence.

## 2. Interaction Model

### 2.1 InteractionTarget

| Field | Type | Rules |
|---|---|---|
| `Kind` | enum | `modal-button`, `command-item`, `input-caret`, `paste-block`, `tool-chip`, `agent-chip`, `transcript-cell` |
| `StableID` | string/integer | command name, event ID, agent run ID, paste ID, modal index, or caret token |
| `Rect` | x, y, width, height | zero-based, clipped to actual terminal dimensions |
| `Priority` | integer | modal > command > input/paste > chip > transcript |
| `Action` | semantic action ID | same action used by keyboard path |
| `PlainTextRef` | optional event/row span | only for transcript selection |

An interaction map belongs to exactly one frame sequence. A click against a stale map is ignored after resize/session generation change.

### 2.2 TranscriptSelection

| Field | Type | Rules |
|---|---|---|
| `Start` / `End` | event ID + plain-row cell | normalized before copy |
| `Active` | boolean | true between press/drag/release or keyboard selection |
| `FrameGeneration` | integer | protects against stale row geometry |
| `PlainText` | derived string | ANSI-free, respects visual row/newline order |

Selection copy is attempted through OSC52; failure leaves the selection intact and exposes the native-terminal fallback hint.

## 3. Input Draft Model

### 3.1 InputDraft

| Field | Type | Rules |
|---|---|---|
| `Parts` | ordered TextPart/PastePart list | adjacent text parts merge |
| `Cursor` | part index + grapheme offset/boundary | never points inside a paste part |
| `FocusedPasteID` | optional integer | one paste block at most |
| `NextPasteID` | positive integer | monotonically increases within draft |
| `Revision` | integer | increments on semantic draft mutation |

An empty draft has one empty TextPart. Sending/clearing/abandoning replaces the draft with that empty state and releases all paste references.

### 3.2 TextPart

| Field | Type | Rules |
|---|---|---|
| `Text` | UTF-8 string | editable typed or small-paste content |
| `GraphemeIndex` | derived boundaries | used for caret navigation/click mapping |

Tabs, NUL, CR, LF, combining sequences, and wide characters are stored as received/typed; unsafe control display uses visible-safe rendering without changing stored text.

### 3.3 PastePart

| Field | Type | Rules |
|---|---|---|
| `ID` | positive integer | unique in draft |
| `RawContent` | UTF-8 string | exact `PasteMsg.Content`; never replaced by label text |
| `CodePointCount` | non-negative integer | threshold measurement |
| `LogicalLineCount` | non-negative integer | CRLF counts as one break; lone CR/LF each count |
| `State` | enum | `collapsed`, `expanded`, `removed` |

**Creation rule**: a paste is large when `CodePointCount >= 1024 OR LogicalLineCount >= 6`; otherwise it merges into TextPart.

**Transitions**:

```text
created --> collapsed
collapsed <--> expanded
collapsed/expanded --> removed --> released
collapsed/expanded --send--> assembled in place --> released
collapsed/expanded --clear/abandon--> released
```

Collapsed display occupies at most two input rows and includes the exact logical line count. Assembly concatenates all parts once in order; placeholder labels never enter the message.

## 4. Theme and Application State

### 4.1 Theme

One immutable startup-resolved value containing:

- Existing semantic foreground/background/status/diff styles.
- Existing Unicode/ASCII glyph table.
- Spacing scale and pane padding.
- Border styles.
- Minimum terminal size (60x20) and design baseline (80x24).
- Header/input/mode-line row metrics and layout breakpoints.
- Interaction focus/selection styles that remain distinguishable without color.

No render function owns independent color, spacing, border, or breakpoint literals after migration.

### 4.2 ApplicationState

| State | Entry condition | Exit condition | Required non-color cue |
|---|---|---|---|
| `first-run/empty` | no transcript and runtime ready | first submitted/persisted event | start guidance text |
| `loading` | shell visible, runtime/page hydration pending | runtime-ready or error | explicit loading label |
| `idle` | runtime ready, no task | submit/load/error | ready label |
| `busy` | task accepted, before first stream | stream/result/error | spinner + text |
| `streaming` | assistant/reasoning chunks arriving | result/error | stream label + active entry |
| `tool-running` | active tool | tool terminal event | tool marker + label |
| `subagent-running` | active subagent | subagent terminal event | agent marker + label |
| `error` | non-fatal/fatal operation failure | dismissal/recovery | error marker + message |
| `too-small` | width <60 or height <20 | valid resize | minimum-size guidance |

Tool/subagent states may coexist with streaming; priority and combined presentation are defined in the visual-state contract.

## 5. Project Context Model

### 5.1 ProjectInstructions

| Field | Type | Rules |
|---|---|---|
| `WorkspaceKey` | canonical path string | case-normalized where platform semantics require |
| `SourcePath` | absolute canonical path | exactly root `MUHIYA.md`; must remain inside root |
| `State` | enum | `missing`, `empty`, `loaded`, `invalid`, `unreadable`, `oversized`, `untrusted-path`, `secret-rejected` |
| `RawSize` | integer | <=32 KiB when loaded |
| `CanonicalContent` | UTF-8 string | BOM removed, CRLF/CR normalized to LF, otherwise preserved |
| `ContentHash` | SHA-256 hex | hash of canonical content |
| `Diagnostic` | optional string/code | non-fatal; content/secret not exposed |

Missing/empty produces no prompt block or warning. Invalid states preserve session usability and expose one safe diagnostic.

### 5.2 ProjectContextSnapshot (`project_context.json`)

Typed per-session sidecar.

| Field | Type | Rules |
|---|---|---|
| `Version` | integer | initial value 1 |
| `WorkspaceKey` | canonical path | must match resumed session |
| `RenderedBootContext` | string | exact bytes reused for prompt reconstruction |
| `InstructionsHash` / `InstructionsState` | values | observed at boot |
| `BaseMemorySequence` | integer | highest memory ledger sequence folded into boot context |
| `SkillsSnapshot` | sorted list | exact stable listing inputs used at boot |
| `AppliedInstructionHash` | hash | latest one-shot instruction update appended to history |
| `AppliedMemorySequence` | integer | latest memory update sequence appended to history |

Writes are atomic. A corrupt sidecar is backed up and yields a non-fatal deterministic empty/bootstrap path with an explicit restart boundary diagnostic; it never silently borrows another workspace's context.

### 5.3 ProjectMemoryEvent

Append-oriented SQLite row.

| Field | Type | Rules |
|---|---|---|
| `Sequence` | integer primary order | monotonically increasing |
| `WorkspaceKey` | canonical path | indexed with sequence |
| `ItemID` | stable random/derived ID | valid local identifier |
| `Operation` | enum | `record`, `supersede`, `forget` |
| `Kind` | enum | `decision`, `finding` |
| `Topic` | normalized string | 1-120 characters; deterministic whitespace/case normalization |
| `Statement` | UTF-8 string | 1-1,024 characters; secret-free |
| `SupersedesID` | optional item ID | required for supersede |
| `OriginSessionID` | session ID | provenance only; omitted from prompt rendering |
| `OriginRequestSequence` | non-negative integer | provenance |
| `CreatedAt` | timestamp | persistence/audit only; omitted from prompt rendering |

The current projection folds events by sequence: `record` adds, `supersede` retires referenced item and adds the new item, `forget` retires referenced item. Clear-all physically deletes only rows matching the selected workspace after confirmation.

### 5.4 CurrentProjectMemory

| Field | Type | Rules |
|---|---|---|
| `WorkspaceKey` | canonical path | single project |
| `ThroughSequence` | integer | ledger cursor |
| `Items` | current ProjectMemoryItem list | deterministically sorted by kind/topic/ID for rendering |
| `RenderedBytes` | integer | <=32 KiB |

Maximum 64 current items. When count/bytes would exceed the bound, the candidate is rejected with an inspect/clear diagnostic; no current item is silently evicted.

### 5.5 MemoryCandidateTrailer

Reserved final-answer metadata:

```json
{
  "items": [
    {
      "kind": "decision",
      "topic": "testing strategy",
      "statement": "Use fixed-seed terminalbench fixtures for performance evidence.",
      "supersedes": "optional-item-id"
    }
  ]
}
```

Rules:

- Must be a final top-level `<project-memory>...</project-memory>` block outside code fences.
- Maximum four items; unknown fields rejected.
- Only final assistant completion metadata is observed.
- Any candidate changed by configured provider/MCP secret redaction is rejected entirely.
- Valid metadata is excluded from user-visible response/transcript body and converted to ledger events; invalid reserved metadata is discarded with a safe diagnostic.

### 5.6 ProjectContextUpdate

One-shot append-only model context change.

| Field | Type | Rules |
|---|---|---|
| `Kind` | enum | `instructions`, `memory` |
| `SequenceOrHash` | deterministic cursor | prevents duplicates |
| `RenderedBlock` | canonical string | fixed tag format; no timestamps |
| `Applied` | boolean | cursor persisted atomically with history append |

```text
boot snapshot --file/memory unchanged--> no update
boot snapshot --change detected at submit boundary--> pending update
pending update --append newest user tail + persist cursor--> applied
applied --same sequence/hash observed--> no-op
new session --compile latest current state--> folded into new boot snapshot
```

## 6. Relationships

```text
Workspace 1 ── 0..1 ProjectInstructions
Workspace 1 ── * ProjectMemoryEvent ──folds──> CurrentProjectMemory
Session   1 ── 1 ProjectContextSnapshot
Session   1 ── * TranscriptEvent ──pages──> TranscriptWindow
TranscriptWindow 1 ── * RenderedBlock ──composes──> FrameState
FrameState 1 ── 1 InteractionMap ──targets──> Transcript/Input/Commands/Chips/Modals
InputDraft 1 ── * TextPart/PastePart ──assembles──> one submitted user message
Final assistant response 0..1 ── MemoryCandidateTrailer ──validates──> ProjectMemoryEvent
CurrentProjectMemory/ProjectInstructions change ──> ProjectContextUpdate ──appends──> newest user turn
```
