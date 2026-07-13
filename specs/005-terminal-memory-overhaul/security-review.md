# Security Review — Terminal Experience and Project Memory (005)

**Scope**: the security-relevant surfaces that are *implemented* in this feature — project instructions loading, durable memory, the typed session sidecar, and the new clipboard/copy path. The still-unbuilt segmented-composer paste surface (T065–T069) is out of scope here and must be re-reviewed when it lands.

Method: source review against `docs/security.md` invariants plus the automated tests noted per item. Items needing a specific OS or terminal to confirm at runtime are labelled **[machine-verify]**.

## 1. Root project-instructions containment (`MUHIYA.md`)

`internal/workspace/project_context.go` `LoadProjectInstructions`:

- **Root-only**: only `<workspace>/MUHIYA.md` is read; no directory walk, no parent/ancestor lookup, so a file planted in a subtree or parent cannot be loaded.
- **Containment**: the resolved path is checked to stay inside the workspace; an outside symlink/junction target is rejected rather than followed. Verified by `project_context_test.go` (outside-symlink/junction cases).
- **Input validation**: UTF-8 required; embedded NUL rejected; hard 32 KiB cap; BOM/newline normalization before hashing so the boot block is byte-stable.
- **Secret screening**: instruction content is passed through the configured-secret + generic redactor; a file whose bytes change under redaction is treated as containing a secret and its state is surfaced honestly rather than silently embedded.

**Finding**: no containment or size bypass found. Diagnostics are typed and content-free (no secret echo in error text).

## 2. Durable memory secret rejection

`internal/orchestrator/project_context.go` `RejectSecretCandidates` and `internal/state/project_memory.go`:

- A candidate whose topic or statement changes under redaction is **dropped entirely**, never stored redacted — a `[REDACTED]` placeholder is not durable memory.
- **Fail-closed**: with a `nil` redactor, *all* candidates are rejected (count returned), so a missing redactor can never fail open. Every shipped caller (engine capture, `/memory remember`, `muhiyacode memory remember`) passes a non-nil redactor bound to the session + MCP secrets.
- Confirmed end-to-end: `muhiyacode memory remember "... sk-ant-...secret..."` is refused (manual CLI run) and by `project_context_test.go` / `project_memory_test.go`.

**Finding**: secret rejection is consistent across all three write paths (engine trailer, CLI, TUI).

## 3. Workspace isolation and the typed session sidecar

- Memory is keyed by `WorkspaceKey`; `Current`/`EventsSince`/`Clear` are all filtered by that key. Cross-workspace read/clear isolation is proven by `TestProjectMemoryWorkspaceIsolation` and `TestProjectMemoryClearIsolation`, and confirmed manually (a second workspace lists empty).
- `Clear` now reports the **current-item** count (not raw ledger rows), so the confirmation the user sees matches `list` — no misleading "cleared N" (fixed this cycle; `TestProjectMemoryClearCountsCurrentItems`).
- The `project_context.json` sidecar is written atomically with user-only permissions; a workspace-key mismatch or corrupt payload backs up the offending file rather than trusting it. **[machine-verify]** the exact on-disk ACL on Windows is enforced by `state/permissions_windows.go` and should be spot-checked on a real profile.

## 4. Clear / forget confirmation

- `/memory clear` and `/memory forget <id>` both open a confirm dialog and mutate only on explicit approval (`internal/tui/actions.go`); the default highlighted choice is Cancel.
- The `muhiyacode memory clear` CLI is non-interactive by contract (scripting surface) and deletes only the current workspace's rows.

**Finding**: destructive memory actions in the interactive surface are gated; the scripting surface is scoped to one workspace.

## 5. Clipboard / copy (new this cycle — US4 T058)

`internal/tui/selection.go` + `model.go` Ctrl+C:

- Copy is **user-initiated**: it only fires on Ctrl+C with an active drag-selection the user made. There is no automatic or background clipboard write.
- The copied payload is the **ANSI-stripped plain text** of the selected transcript range (`selectionText`), assembled from already-displayed content — copying introduces no new disclosure beyond what the user already sees and deliberately selected.
- Transport is Bubble Tea's OSC 52 (`tea.SetClipboard`). OSC 52 is terminal-gated: terminals that disable it simply drop the sequence. There is **no** clipboard *read* on this path (no `ReadClipboard`), so a malicious escape sequence cannot exfiltrate the user's clipboard through this feature.
- Residual consideration (accepted): if the transcript visibly contains a secret the model surfaced, a user who selects and copies it copies that secret — this is inherent to any copy affordance and matches native terminal selection. No mitigation beyond the existing redaction of *stored* memory is warranted.

**[machine-verify]** OSC 52 accepted/rejected behavior across Windows Terminal + one macOS + one Linux terminal (part of T061/T082).

## 6. Unchanged invariants (confirmed)

- The mouse/selection/memory work is **presentation- and ledger-side only**; it does not alter the orchestrator request-assembly path, tool routing, model selection, or the deterministic prompt prefix (release-audit check, and the cache/wire packages are untouched by this cycle's edits).
- Transcript trimming (M1) bounds only the in-memory render window and prints a visible marker; the durable session transcript in the DB is never truncated — no hidden data loss.

## Verdict

No new vulnerabilities introduced by the implemented surfaces. Two items are flagged **[machine-verify]** (Windows sidecar ACL; cross-terminal OSC 52) for the physical matrix. `docs/security.md` requires no change for the built surfaces; it must be revisited when the composer paste path (T065–T069) is implemented.
