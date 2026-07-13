# US3 Checkpoint — Return to a Project with Its Context Intact

**Status**: Core complete and independently verified. `/memory` TUI modal (T048) and early-loading TUI submission tests (T040 TUI half) remain, gated on the US1 two-stage-startup seam (T021).

## What was built (T037–T047, T049, T050, T039)

| Area | Files | Verification |
|---|---|---|
| Root `MUHIYA.md` loader (containment, UTF-8/NUL/size, BOM/CRLF, hash, secret reject) | `internal/workspace/project_context.go` | `internal/workspace/project_context_test.go` — all load cases green |
| Append-only workspace memory ledger + current projection + bounds | `internal/state/project_memory.go`, `internal/state/db.go` | `internal/state/project_memory_test.go` — record/supersede/forget/clear, 64-item & 32 KiB bounds, workspace isolation, rollback |
| Typed `project_context.json` sidecar (atomic, mismatch/corrupt backup) | `internal/state/project_context.go` | `internal/state/project_context_test.go` |
| Deterministic boot/update renderers, strict trailer parser, streaming filter, secret-candidate rejection | `internal/orchestrator/project_context.go` | `internal/orchestrator/project_context_test.go`, `memory_trailer_test.go` |
| Byte-stable memory-output contract + optional boot block in prompt | `internal/orchestrator/prompt.go` | `project_context_test.go` (byte-identical, date-free, gated) |
| Engine auto-capture: streaming trailer filter, completion-boundary capture, one-shot tail updates, cursor persistence | `internal/orchestrator/engine.go` | `memory_capture_test.go` — capture+strip, secret rejection, one-shot-then-suppressed |
| Application composition: boot snapshot compose/restore, probe/apply closures, secret set, Prompt fields | `internal/command/application.go` | full `internal/command` suite green |
| CLI `muhiyacode memory list/remember/forget/clear` | `internal/command/root.go` | `internal/command/memory_test.go` — lifecycle, secret reject, workspace isolation |
| Closed-tail contract update | `specs/002-.../contracts/request-assembly.md` | `request_assembly_test.go` banned-substring guard extended |
| Resume boot-byte reuse (cache safety) | — | `restart_determinism_test.go::TestProjectContextResumeMustReusePersistedBootBytes` |

## Independent adversarial verification

A 4-lens adversarial review (cache-safety, secret/trailer-leak, one-shot/determinism, concurrency) was run against the wiring. Result:

- **Cache-safety lens: 6/6 attacks refuted** — the one-shot tail rides the append-only newest user message (excluded from `SettledHash` by `NewWirePrefixShape`), the boot block is restored verbatim on resume, and `ProjectMemory` adds no per-turn variance. No cache-unsafety defect.
- **7 real defects found and FIXED in this checkpoint:**
  1. [HIGH] Transient sidecar read error no longer overwrites a valid snapshot (would have forced a cold cache on Windows AV interference). Now only bootstraps+writes when the read genuinely returns absent/corrupt/foreign.
  2. [MED] `ExtractMemoryTrailer` now strips a mis-placed `<project-memory>` block from persisted/returned text (removal decoupled from acceptance) — no raw JSON leaks to transcript/history.
  3. [MED] Memory cursor no longer over-advances: `finalize` records only (contract §7.5 surfaces on the next turn); tail-inject advances only after actually emitting; probe propagates `EventsSince` errors.
  4–7. [LOW] Fixed: `ApplyMemory` surfaces per-candidate errors (warning path now reachable); CLI screens candidates with the full MCP secret set; `RejectSecretCandidates` fails closed on a nil redactor.

## Quality gate at checkpoint

`go build ./...` ✅ · `go vet ./...` ✅ · `go test ./... -count=1` ✅ (every package green).

## Known limitations (documented, not defects)

- Two concurrent sessions on the SAME workspace surface each other's mid-session memory writes only at the next session boot (single-watermark cursor cannot both suppress self-echo and surface a lower-sequence concurrent event). Rare; no data loss.
- Crash strictly between the history append and the cursor sidecar write re-emits one identical `<memory-update>` on resume (at-least-once, ledger never double-mutated).
- A model that violates the output contract by emitting a trailer on a non-final (tool-call) turn has it withheld from live display but persisted un-stripped (contract §7 scopes capture to final answers).

## Not yet run on this machine

- `--simple` / non-TTY / `-p` paths share `buildRuntime`'s project-context construction (single funnel confirmed by code), but a direct simple-line memory test is pending.
- `/memory` TUI modal (T048) and its click/keyboard matrix — deferred to land with the US1/US2 TUI overhaul that reworks the same panes.
