# Contract: Evidence Store and Observation Reducers

## 1. Store-before-reduce protocol

```text
tool executes
-> capture bounded/live stream under existing safety cap
-> finalize raw result and completeness metadata
-> redact/block secrets according to current rules
-> commit content-addressed artifact
-> run typed reducer
-> validate observation invariants
-> append observation card to model history
```

If artifact commit fails, the tool may return the existing safe bounded result with `[artifact unavailable]`; it may not emit a fake handle.

## 2. Artifact handle grammar

```text
artifact://<session-short>/<sha256>?lines=<start>-<end>
```

The URI is model-facing only. Resolution always checks the current session, workspace, artifact metadata, permissions, and bounds. The raw filesystem path is never accepted from the model.

## 3. Fetch contract

Stable broker operation:

```json
{
  "name": "fetch_artifact",
  "args": {
    "id": "<sha256>",
    "startLine": 1,
    "maxLines": 120,
    "match": "optional literal/regex",
    "maxMatches": 20
  }
}
```

The result is itself an observation card and cannot exceed the active inline evidence budget.

## 4. Reducer invariants

All reducers preserve:

- success/failure/partial/cancelled status
- exit code and timeout/cancellation for processes
- skipped/incomplete/truncated indicators
- failure identifiers and source locations
- changed paths and applied/not-applied state
- exact excerpts as raw subsequences, never paraphrased
- artifact handle when available

Reducers use deterministic sorting and stable formatting.

## 5. Typed reducers

### File/source

- exact requested lines and coordinates
- syntax/AST outline when available
- total lines and remaining ranges
- source fingerprint
- no duplicate line payload already intact in active context

### Search/glob/list

- total matches/entries
- top results diversified by file/directory
- query and scope
- truncation/skipped files
- handle for full result

### Edit/patch/diff

- applied boolean
- changed paths
- lines added/removed
- compact hunks around modifications under cap
- skipped/mismatched edits with nearest exact region
- before/after fingerprints

### Test/build/lint/typecheck

- detected runner and command hash
- exit status/duration
- package/suite/test counts where parser supports them
- every failure name and primary location
- bounded diagnostic blocks
- warnings summarized by class/count
- generic marker if runner parser did not recognize format

### Generic shell

- exit/duration/status
- prioritize lines matching error/fail/panic/exception and platform equivalents
- bounded head and tail
- byte/line omission counts
- never infer test success from exit 0 if command was interrupted/partial

### JSON/MCP/web

- preserve typed scalar fields and item count
- select query-relevant items under budget
- retain schema/type and full artifact
- flag invalid JSON or transport truncation

## 6. Inline budgets

Initial experimental caps:

| Result | Normal cap | Failure cap |
|---|---:|---:|
| source/search | 1,200 tokens | 1,800 |
| edit/diff | 1,500 | 2,000 |
| test/build | 1,000 | 3,000 |
| generic shell | 800 | 2,000 |
| MCP/web/JSON | 1,200 | 2,000 |

Caps are calibrated estimates. A protected failure fact may exceed the soft cap but never the task hard observation cap without an override.

## 7. Retention and garbage collection

- Task artifacts: retain through epoch settlement plus rollback window.
- Capsule-referenced artifacts: retain with capsule or materialize required exact facts into durable project evidence.
- Secret-blocked raw values: never store.
- Large raw artifacts: optional compression at rest; content hash covers canonical uncompressed bytes and metadata records encoding.
- GC is mark-and-sweep from active epochs, capsules, transcript references, and pinned debug records.

## 8. Tests

- Golden reducers for every supported runner and platform.
- Fuzz invalid UTF-8, huge lines, ANSI escapes, JSON depth, binary, and secret patterns.
- Artifact ownership/containment/security tests.
- Exact excerpt subsequence property.
- Failure card cannot say success.
- Omission counts match raw input.
- GC retains every referenced artifact and removes only unreachable blobs.

