# MuhiyaCode Remediation Plan — Audit Wave (feature/native-agent-v1.1.0)

## Executive Summary

**44 of 45 findings survived** adversarial verification (1 refuted). Of these, **19 are CONFIRMED** (code-traced, failure reproduced) and **25 are PLAUSIBLE** (low/polish, "verify-then-fix"). **Four are high-severity after re-grading**: `idle-timeout-becomes-noncancelable-abort`, `rm-backstop-anchor-bypass`, `grep-list-glob-bypass-sensitive-roots`, `patch-delete-dashdash-line-fails`.

**Highest-leverage themes (fix these first, they each cover multiple findings):**

1. **The unified-diff `-- `/`++ ` prefix-collision cluster** — one root cause, three surfaces: the *applier silently rejects a valid patch* (C-1, HIGH), the *diff line counter miscounts* (E-1), and *target extraction injects a phantom file* (E-3). Fix all three with count-bounded / hunk-aware parsing so they cannot drift.
2. **Destructive-command backstop laundering** — `/bin/rm`, `\rm`, `(rm`, and abbreviated PowerShell `ri -r -fo` all bypass `ClassifyShell` (I-2 HIGH, I-3). This is the *identical class* as the newline bypass just closed in `bb8df5a`. The correct tokenizer already exists in `orchestrator/shellclassify.go`; share it.
3. **Credential-containment holes in read/search paths** — `grep`/`list_files`/`glob` traverse into `~/.ssh`, `~/.aws`, `~/.muhiya` when pointed at a non-sensitive ancestor (I-1 HIGH); a symlinked `SKILL.md` reads arbitrary files (I-5). Enforce sensitive-root denial on every *traversed* path, not just the authorized root.
4. **Timeout misclassified as user cancel** — the CLI idle/lifetime timeout surfaces as `context.Canceled`, so the very stall-recovery the timeout ordering was designed for never fires and the task shows "stopped." (B-1 HIGH).
5. **Over-aggressive / false cache invalidation** — the prefix-drift guard manufactures a false positive and *aborts valid resumed tasks* (A-1); `apply_patch` wipes the entire inspection ledger (A-2). The guard meant to protect the cache is busting it.
6. **The executor agent is stripped of environment and safety context** — the one agent that runs every mutation is never told its OS/shell, cwd model, or "preserve user changes / avoid destructive commands" (D-1, D-2, D-3). Batch these prompt edits into **one deliberate cache epoch**.
7. **H5 failure terminator counts recoverable gate rejections** — a single turn-1 batch of 8 role-gated edits force-finalizes the whole task with a misleading blocker (G-1).

**Single most important fix:** **I-1 (`grep`/`glob`/`list_files` credential-root bypass).** It silently exfiltrates SSH keys, AWS credentials, and MuhiyaCode's own secrets into model context and the transcript, defeating the strongest documented containment guarantee, and is reachable with one approved outside-workspace search of an ancestor directory. The most impactful *correctness* item is the C-1/E-1/E-3 patch-parser cluster.

**Cache-epoch discipline:** Only Workstream D changes prompt/prefix bytes. All four D tasks land in **one commit / one epoch** with a single golden regeneration. Every other task in this plan is cache-safe (runtime, display, control-flow, or read-side recovery) and touches no prefix bytes.

---

## Execution Log — Phase 0 COMPLETE (2026-07-21)

All eight Phase-0 P0 items shipped (uncommitted working tree), each with a regression test proving the defect and its fix. Gates: `gofmt` clean, `go vet` clean, `go build ./...` clean, `go test ./...` green across all packages; the prefix-stability goldens (`prefix_bytes`, `prefix_bytes_wire`, `instructions_dump`) are **byte-identical** — Phase 0 changed zero prefix bytes.

| Task | What shipped | Tests added |
|---|---|---|
| A-1 | `turnloop.go` records the post-normalization wire message count (`shape.MessageCount`) for both the drift-guard settled window and `suspiciousCacheMiss` | `resume_drift_guard_test.go` (proven: fails on old code with the exact "history" abort) |
| I-2/I-3 | New stdlib leaf `internal/shellsafe` (shared command tokenizer/normalizer); `ClassifyShell` rewritten to tokenize command position → closes `/bin/rm`,`\rm`,`(rm`,`RM.EXE`,subshell (I-2) and abbreviated PowerShell `-r -fo` (I-3); `IsReadOnlyShell` now consumes the shared leaf so the two gates cannot drift; arch layering updated | `risk_launder_test.go`, `shellsafe_test.go`; all prior `ClassifyShell`/`IsReadOnlyShell` tests preserved |
| I-1 | `Guard.IsSensitive`; `grep`/`list_files`/`glob` + `collectFiles` skip any traversed dir/file inside a sensitive root | `sensitive_traversal_test.go` (proven: fails on old code — leaks `id_rsa`) |
| C-1/E-1/E-3/C-5 | `patch.go` count-bounded hunk consumption + no-newline-marker terminal newline; `diffcount.go` hunk-aware counting; `tooltarget.go` `@@`-anchored header detection | `patch_cluster_test.go`, `diff_cluster_test.go` |
| B-1 | `context.WithCancelCause` + typed `StalledError` (retryable) at all three chatOnce exit sites; `isNetworkError`/`Recoverable`/`FriendlyRequestError` special-case it | `stall_retry_test.go` (stall-then-succeed proves the retry fires) |
| G-1 | `toolOutcome.GateRejected` set on the six pre-dispatch gate refusals; H5 terminator skips them | `TestRoleGateRejectionsDoNotForceFinalize` + two existing H5 tests corrected to exercise genuine (not role-gated) failures |
| F-1 | `SaveSettings` no longer self-copies the shared struct (the widest race); `effortMu`→`liveSettingsMu` now also guards `PermissionMode` via `SetPermissionMode`/`permissionMode`, and the TUI routes through it | `settings_race_test.go` (concurrent setters during `engine.Run`; `-race` gate runs on CI) |

**Residual for a CGO-capable runner:** `go test -race ./...` (this host has no C compiler). The F-1 fix is reasoned byte-for-byte but its acceptance gate is `-race`; run it on CI. Narrower settings reads inside `tea.Cmd` goroutines (`state.SaveSettings`/`UpdateConfig` reading the whole struct) are lower-severity and best confirmed under `-race`.

## Execution Log — Phases 1–3 COMPLETE (2026-07-21)

Phases 1, 2, and 3 shipped after Phase 0, each item with a regression test where testable. Full gate green after every phase: `gofmt` clean, `go vet` clean, `go build ./...` clean, `go test ./...` green across all packages.

**Phase 1 (P1 correctness, zero prefix change):** A-2 (apply_patch invalidates only its touched files), C-2 (byte-preserving edit splice + dominant-ending patch), C-3 (rune-boundary read truncation), C-4 (run_shell surfaces timeout/cancel with a failure-recognized prefix), F-2 (JSONL resume tolerates a truncated trailing line), I-4 (nested MCP OAuth tokens redacted), E-2 (context-in-use unified on the request-based figure), H-1 (context card fits at 80×24). Tests: `inspection_test.go`, `edit_endings_test.go`, `read_truncate_test.go`, `shell_result_test.go`, `resume_tolerant_test.go`, `mcp_redact_test.go`, `context_inuse_test.go`, `context_modal_fit_test.go`.

**Phase 2 (THE cache epoch — one commit, three goldens regenerated once):** D-1 (executor told to preserve user work / avoid destructive commands / verify targets), D-2 (executor told its OS + shell), D-3 (run_shell cwd non-persistence), D-4 (edit exactness/recovery contract). `instructions_dump.golden`, `prefix_bytes.golden`, `prefix_bytes_wire.golden` regenerated exactly once; diff reviewed to be only these four clauses (plus the pre-existing uncommitted 013 delta). Tests: `epoch_clauses_test.go`, `subagent_env_test.go`.

**Phase 3 (P2 polish / verify-then-fix, all cache-safe):** I-5 (read_skill rejects a symlinked SKILL.md escaping to a sensitive root), A-6 (no zero-usage record on a failed subagent Chat), A-7 (PrefixStabilityRate ≤ 100%), A-3 (read_skill not re-sent when its body is already in settled history — result now carries a `<skill name>` marker; provided-set seeded from actual history), A-4 (documented: search dedup is mutation-triggered by design), A-8 (resume-drift tool-set label names skills too, not just MCP), B-2 (reasoning_effort gated by SupportedParams), B-3 (finish_reason threaded through; `length` truncation surfaced), B-4 (cache-only usage payloads render honestly), C-6/C-7 (oversize-change / whole-file-overwrite warnings), E-4 (trim-marker reports the actual count), G-2 (subagent progress ladder counts apply_patch / mutating run_shell), H-2 (permission/reasoning nil-Settings guards), H-3 (modal sized to View's real reserve — no bottom shear), H-4 (Shift+Tab hint aligned under the permission chip), H-5 (corrected the `lastStats` comment).

**Deferred (2 of Phase 3, documented, not started):**
- **A-5** (aux summarizer usage advances `requestSeq`): the fix reshapes load-bearing `requestSeq` semantics that drive invalidation attribution engine-wide and would ripple across many exact-Seq test assertions, for a rare telemetry off-by-one (compaction + aux same turn). Needs careful seq redesign + the live gauntlet to verify.
- **F-3** (`taskMu` held across the usage fsync): moving the fsync off the lock is a usage-hot-path concurrency reordering whose disk-ordering safety needs `go test -race` (no C compiler on this host). P2 jank, not correctness.

**Live Constitution-X before/after runs remain owner-cost-gated** (steady-state hit rate, startup timing, cross-provider behavior on DeepSeek + MiniMax/GLM), plus the one-time Phase-2 epoch verification (exactly one cache-miss turn, then steady-state ≥ prior plateau).

---

## Workstream A — Caching to ~100%

### A-1 · Prefix-drift guard aborts valid resumed tasks — P0 · CONFIRMED · medium
- **Problem:** `turnloop.go:444` stores `e.lastSentMessageCount = len(messages)` from the *pre-normalization* list (`built.Messages`, line 367), but the shape it is compared against hashes the *post-normalization* list (`shapeRequest.Messages`, line 372-387; `prefixshape.go:80`). When `repairToolMessageSequence` (gateway `provider.go:373-410`) adds/drops a synthetic tool result on a resume-after-interrupted-tool-call, `M' != M`. Next turn `CompareShape` slices to the wrong settled window → `HistoryHash != SettledHash` on a purely append-only turn → `turnloop.go:399-401` returns the fatal "stable request prefix changed without an invalidation event: history" and bricks the resumed session.
- **Fix:** At `turnloop.go:444` set `e.lastSentMessageCount = shape.MessageCount` (== `len(shapeRequest.Messages)`). One-line diagnostic-count change; on the no-normalizer branch it is a strict no-op.
- **Constitution:** III, IV.
- **Cache-safety:** Cache-safe. Changes only the guard's bookkeeping counter; the bytes sent to the provider come from `request`, not `shapeRequest`.
- **Acceptance:** New unit test: persist an assistant `tool_calls` message with a dangling (result-less) call, resume, run two turns; assert no `stable request prefix changed` abort and that `lastSentMessageCount == shape.MessageCount`. Update any guard test asserting the old count.

### A-2 · `apply_patch` wipes the entire inspection ledger — P1 · CONFIRMED · medium
- **Problem:** `inspection.go:215` takes the global-wipe branch for `apply_patch` (alongside `run_shell`/`mcp__*`), clearing `l.signatures`+`l.coverage` and superseding *every* covered read — including files the patch never touched. Downstream `dispatch.go:160` → `MarkSuperseded`, and `history.go:242` trims a superseded result even inside the `keepFull` recent window. `apply_patch`'s touched set is deterministic and already parseable via `contract.ToolTargetPaths → PatchTargetFiles` (`tooltarget.go:87`).
- **Fix:** In `InvalidateFor`, route `apply_patch` like the path-bearing edit tools: derive targets with `contract.ToolTargetPaths(name, []byte(call.ArgumentsJSON()))` and `invalidatePathLocked` each (union of dropped IDs). Keep the blanket wipe only for `run_shell` and `mcp__*`.
- **Constitution:** V, IX.
- **Cache-safety:** Cache-safe and *strictly more conservative* — preserves more settled bytes. **Sequencing:** depends on E-3 (fix `PatchTargetFiles` phantom-file bug first so derived targets are clean).
- **Acceptance:** Unit test: read file B, `apply_patch` on file A only; assert B's read is NOT superseded and B stays duplicate-guarded. Confirm `TestInspectionFreshnessAndInvalidation` still passes.

### A-3 · `read_skill` re-sends a body already in settled history — P2 · PLAUSIBLE · low · verify-then-fix
- **Problem:** `taskSkillsProvided` is cleared per task (`engine.go:376`) and seeded only from the current prompt's `<skill>` markers (`skills_tool.go:177-182`). A second-task `read_skill` of a name already read in task 1 re-loads and re-appends the full body (`skills_tool.go:142-153` → `turnloop.go:574`) — redundant tail retransmission of settled content.
- **Fix:** Seed `taskSkillsProvided` from skills already present in the settled history at task start (scan prior `RoleTool` results via `skillNamesIn`), or make the provided-set session-scoped.
- **Constitution:** V.
- **Cache-safety:** Cache-safe (tail-only; prefix untouched).
- **Acceptance:** Two-task session test: `read_skill("x")` in task 1, then task 2 without a marker; assert the second call returns the "already provided" short-circuit, not the full body.

### A-4 · Search dedup has no staleness gate — P2 · PLAUSIBLE · low · verify-then-fix
- **Problem:** `Duplicate()` for `grep`/`glob`/`list_files` hits purely on `l.signatures` + `intact()` (`inspection.go:114-116`), with no mtime/generation check (unlike `read_file`'s `coverageStaleLocked`). An external edit between turns is not detected; an identical re-search serves a stale "already ran" block.
- **Fix:** (a) drop all `search`-kind signatures when a coarse workspace-change generation counter (`knowledge.MarkWorkspaceChanged`) advances, or (b) document that search dedup relies solely on mutation-triggered invalidation and shorten search-signature lifetime.
- **Constitution:** V, I.
- **Cache-safety:** Cache-safe (read-side only).
- **Acceptance:** Test: record a grep, bump the workspace generation, re-run identical grep; assert it re-executes rather than returning the stale block.

### A-5 · Aux summarizer usage orphans a same-turn fold event's `RequestSeq` — P2 · PLAUSIBLE · low · verify-then-fix
- **Problem:** `appendUsageLocked` bumps `e.requestSeq` for *all* streams including the compaction summarizer (`usage.go:144,153`; `maintenance.go:82-86`). When maintenance folds and compaction also fires, the Fold event lands one seq below the request whose prefix it changed, so `EventsForRequest` mis-attributes the fold to the aux request.
- **Fix:** Give aux/summarizer usage a non-advancing seq (reuse current `requestSeq` or a separate aux counter), or record the fold after compaction at the same `nextRequestSeq()`.
- **Constitution:** VI.
- **Cache-safety:** Cache-safe (telemetry seq only).
- **Acceptance:** Test the maintenance+compaction-same-turn path; assert the Fold event's `RequestSeq` equals the main request's.

### A-6 · Subagent records a zero-usage record on a failed Chat — P2 · PLAUSIBLE · low · verify-then-fix
- **Problem:** `executeSubagent` calls `recordIsolatedUsage` unconditionally before the `if err != nil` handling (`subagent.go:413-420`); on transport error it appends an empty record and bumps `requestSeq`, and again on the retry. The main loop records nothing on a Chat error — the two paths disagree.
- **Fix:** Guard the record with `err == nil` (or "response carries any usage/cache field"), mirroring the main loop.
- **Constitution:** VI.
- **Cache-safety:** Cache-safe.
- **Acceptance:** Test a failing subagent Chat; assert no usage record is appended and `requestSeq` is unchanged.

### A-7 · `PrefixStabilityRate` can exceed 100% — P2 · PLAUSIBLE · low · verify-then-fix
- **Problem:** In `cache.go:366`, `PrefixStableRead += *CacheReadTokens` runs unconditionally while `eligible` clamps to 0 when `NewTailTokens >= PromptTokens`. A degenerate record inflates the numerator with reads whose denominator contribution was clamped → ratio > 1.0.
- **Fix:** `if eligible > 0 { PrefixStableRead += *CacheReadTokens; PrefixStableEligible += eligible; prefixStabilityAvailable = true }`.
- **Constitution:** VI.
- **Cache-safety:** Cache-safe (contract accounting).
- **Acceptance:** Unit test with one clamped + one normal record; assert rate ≤ 100%.

### A-8 · Resume-drift attributes a skills-driven tool-set change to "MCP servers" — P2 · PLAUSIBLE · low · verify-then-fix
- **Problem:** `checkResumeDrift` hardcodes `Scope: "...tool set changed... (MCP servers)"` (`cacheresilience.go:97-103`), but the tool set also flips when `read_skill` appears/disappears (`definitions.go:22-24`). Installing/removing the first/last skill fires this event mislabeled as MCP.
- **Fix:** Drop the parenthetical or compute it (diff the tool-name sets to name what actually appeared/disappeared).
- **Constitution:** VI.
- **Cache-safety:** Cache-safe (telemetry string).
- **Acceptance:** `skills_prefix_stability_test.go` extended to assert the scope string reflects a skill change, not MCP.

---

## Workstream B — Multi-model gateway consistency

### B-1 · Idle/lifetime timeout becomes a non-recoverable `context.Canceled` abort — P0 · CONFIRMED · high
- **Problem:** `chatOnce` builds `ctx` via `context.WithCancel(parent)` and both the lifetime timer (`provider.go:159`) and the idle timer (`provider.go:232`) call the same plain `cancel`. On a silent stall, `scanner.Err()` returns raw `context.Canceled` (streamed=true), returned at `provider.go:273-275`. `isNetworkError` excludes `Canceled`, so Chat's once-only stream retry (line 126) is skipped; `gateway.Recoverable` also returns false; `FriendlyRequestError` renders "stopped." The carefully designed CLI-idle-below-gateway ordering (`provider.go:62-73`) is structurally defeated for exactly the half-open-TCP stall it targets.
- **Fix:** Distinguish a timeout-cancel from a user-cancel (atomic flag set in the AfterFunc before `cancel`, or cause tracking). When `parent.Err()==nil`, return a **typed `StalledError`** (not raw `Canceled`) that **both** `isNetworkError` **and** `Recoverable` treat as retryable — merely wrapping `DeadlineExceeded` fixes only the turnloop gate, not the Chat stream-retry gate. Apply at the `scanner.Err()` path (273-275) and the pre-header `Do` path (216-218), not only the in-loop select.
- **Constitution:** I, IX.
- **Cache-safety:** Cache-safe — only error classification changes; the retry re-sends the byte-identical warm-prefix request.
- **Acceptance:** `httptest` SSE server that emits one frame then stalls; assert the stall yields a retryable error, the stream retry fires, and the task does not render "stopped." Keep `TestDiedStreamIsRetriedOnce` green.

### B-2 · Request builder ignores `profile.SupportedParams` — P2 · PLAUSIBLE · low · verify-then-fix
- **Problem:** `chatOnce` emits a fixed body (`provider.go:176-194`) and never consults `SupportedParams` (no production consumer; grep-confirmed). `reasoning_effort` is emitted for every model including MiniMax (whose profile lists `reasoning_split`, `model.go:136`) and its value can be `"max"` (`effort.go:98`). Harm requires a non-gateway strict endpoint (the gateway maps `X-Muhiya-Effort` per provider), so severity was downgraded — but the `model.go` "single source of truth / never emits an unsupported param" docstring is unenforced.
- **Fix:** Gate optional params by `SupportedParams` when non-empty (empty == permissive), **or** downgrade the docstring to state param sanitization is delegated to the gateway. Either is acceptable and cache-safe.
- **Constitution:** III, IX.
- **Cache-safety:** Cache-safe — `SupportedParams` is boot-frozen/session-constant, so the wire shape stays session-stable.
- **Acceptance:** Test that a MiniMax request omits `reasoning_effort` from the body (header still carries effort); DeepSeek unchanged.

### B-3 · `finish_reason` parsed but never consumed — P2 · PLAUSIBLE · low
- **Problem:** `sse.go:168` returns `StreamResult.FinishReason`, but `chatOnce` (`provider.go:298-307`) drops it and `ChatResponse` has no such field. `finish_reason="length"` (MiniMax/GLM at 16k) truncation is indistinguishable from natural completion.
- **Fix:** Thread `FinishReason` through `ChatResponse`; on `"length"` emit a truncation notice/telemetry (optionally auto-continue).
- **Constitution:** I, VI.
- **Cache-safety:** Cache-safe (read-only use of an already-parsed field).
- **Acceptance:** Test a `finish_reason:"length"` stream; assert a truncation signal is surfaced.

### B-4 · Total-only / cache-only usage payloads under-report on live surfaces — P2 · PLAUSIBLE · low
- **Problem (two related findings):** (a) `render_header.go:116/52` and `render_tool.go:306` gate the entire live token+cache segment on derived `TotalTokens>0`; a payload with cache/total present but prompt/completion absent renders nothing. (b) `usageFromAggregate` (`usage.go:234`) sets `Total = SumPrompt+SumCompletion` and discards a provider's own `total_tokens`, so a total-only provider shows 0. The originally-flagged `format.go:175` 0-token bug is *defused* by `usage.go:234` re-derivation and needs no change beyond harmless defense.
- **Fix:** (a) Gate on presence of any reportable usage: `if m.usage.TotalTokens > 0 || m.usage.CacheReadTokens != nil` (all three sites). (b) In `usageFromAggregate`, prefer a provider total when it exceeds the derived sum: `max(providerTotal, SumPrompt+SumCompletion)`. (c) Optional: make `headlineTokens` fallback self-consistent.
- **Constitution:** IX, VI.
- **Cache-safety:** Cache-safe (display/accounting only).
- **Acceptance:** Test cache-only and total-only usage payloads; assert the cache tag / token figure renders honestly instead of vanishing.

---

## Workstream C — Shell / Edit / Write execution reliability & prerequisite verification

> **Cluster note:** C-1 shares a root cause with E-1 and E-3 (the `-- `/`++ ` unified-diff prefix collision). Fix the parser cluster together.

### C-1 · `apply_patch` rejects any patch that DELETES a `-- ` line — P0 · CONFIRMED · high
- **Problem:** `parseUnifiedPatch` bounds the hunk body with `!HasPrefix(line,"--- ")` (`patch.go:135`) and the outer loop stops on `--- ` (line 126); a deleted `-- comment` line renders as `--- comment` and is misread as a file header demanding a following `+++ ` → `missing +++ header after "..."` and the whole patch is rejected, applying nothing. Hits SQL/Lua/Haskell/Ada/Elm/VHDL comments (this repo does Supabase SQL).
- **Fix:** Make hunk-body consumption **count-bounded**: extend `hunkHeader`'s regex to capture the `@@ -a,b +c,d @@` counts and consume exactly `b`/`d` body lines (retaining the existing `\ No newline at end of file` skip without decrementing). Then a `--- content` deletion inside the counted body is body, and header detection resumes only after the counted lines.
- **Constitution:** I, IX.
- **Cache-safety:** Cache-safe (workspace parsing; `ToolApplyPatchDescription` byte-identical).
- **Acceptance:** Test applying a patch that deletes `-- deprecated`; assert success and correct on-disk result. Add `-- `/`++ ` deletion+addition fixtures.

### C-2 · Edit/patch blanket re-encodes every line ending on any CRLF — P1 · CONFIRMED · medium
- **Problem:** `applyEditText` (`files.go:378-405`) sets CRLF mode if the file contains *any* `\r\n`, normalizes all to `\n`, edits, then `ReplaceAll(current,"\n","\r\n")` — flipping bare LFs that were never CRLF. `ApplyPatch` (`patch.go:58-65`) has the identical pattern. A one-line edit to a mostly-LF file with one CRLF rewrites every line (git shows the whole file changed; busts the cached file body).
- **Fix:** Reconstruct per-line endings: split retaining each line's own terminator, replace within the matched region, rejoin — untouched lines keep exact bytes. Or only re-normalize when the file is uniform / matches the dominant ending.
- **Constitution:** I, V.
- **Cache-safety:** Cache-safe (on-disk file bytes, not prefix).
- **Acceptance:** Test a mixed-ending file, single-line edit; assert only the edited line's ending region changes and all other bytes are byte-identical.

### C-3 · `read_file` truncates long lines at 500 **bytes**, splitting UTF-8 — P1 · CONFIRMED · medium
- **Problem:** `files.go:110-114` does `line[:500]` (byte slice); byte 500 mid-rune (em-dash/arrow/§) emits an incomplete sequence → U+FFFD mojibake on JSON marshal. Also, the 500-byte cap is unconditional, so a >500-byte line can never be retrieved in full → the model cannot build an exact `oldString` for `edit_file` and loops on "oldString not found". Grep (`files.go:173`) already uses rune-aware `contract.TruncateEllipsis`.
- **Fix:** Replace the byte slice with `contract.TruncateEllipsis(line, 500)` (rune boundaries), matching Grep. Consider raising the per-line cap or exposing a full-line fetch so long lines stay editable.
- **Constitution:** I, IX.
- **Cache-safety:** Cache-safe (tool output; may require updating a read golden for the `…` delimiter).
- **Acceptance:** Test a line with a multi-byte rune straddling byte 500; assert valid UTF-8 output.

### C-4 · `run_shell` discards `TimedOut`/`Cancelled` — P1 · CONFIRMED · medium
- **Problem:** `shell.go:123` computes `TimedOut`/`Cancelled`/`Duration`; `execShell` (`registry.go:279-283`) emits only `exit code: N` + output. Both fields are never read anywhere. A killed/timed-out command is indistinguishable from a real non-zero exit; `IsToolFailure` also doesn't flag it (err nil, output starts `exit code:`), so no loop guard engages — the model blindly retries the hang.
- **Fix:** In `execShell`, prepend a clear marker on `TimedOut` (and `Cancelled`) naming the elapsed duration and that the process tree was killed, before the exit-code line. Consider giving it a recognized failure prefix so `IsToolFailure`/loop-guard engages.
- **Constitution:** I.
- **Cache-safety:** Cache-safe (per-turn tool-result text).
- **Acceptance:** Test a command that exceeds the deadline; assert the result contains the timeout marker + duration.

### C-5 · `apply_patch` appends a trailing newline to files that had none — P2 · PLAUSIBLE · low
- **Problem:** `patch.go:197` `if hadNewline || len(hunks) > 0 { result += "\n" }`; any file with ≥1 hunk gains a trailing newline. The `\ No newline at end of file` marker is parsed then discarded (`patch.go:137-140`).
- **Fix:** Honor `hadNewline` for the tail; consult the parsed no-newline marker to suppress the terminator.
- **Constitution:** I, V.
- **Cache-safety:** Cache-safe. **Sequencing:** land with C-1 (same parser).
- **Acceptance:** Test patching a no-trailing-newline file; assert no `\n` added.

### C-6 · Partial read satisfies the read-before-overwrite guard — P2 · PLAUSIBLE · low
- **Problem:** `canOverwrite` (`files.go:364-372`) returns true if the ledger `has(target)`, but Read marks the whole file read after any partial read (`files.go:115`; `inspection.go:173/240`). Reading lines 1-50 of a 5000-line file unlocks a full `write_file` overwrite of the unseen remainder.
- **Fix:** For `write_file` on an existing file, require evidence the model saw the whole file (a full-read segment covering total lines), or warn/require an explicit overwrite acknowledgement.
- **Constitution:** I.
- **Cache-safety:** Cache-safe.
- **Acceptance:** Test: partial read then `write_file`; assert a guard warning/block rather than silent full overwrite.

### C-7 · 2–5 MB files are editable but never checkpointed — P2 · PLAUSIBLE · low
- **Problem:** `checkpoint.Create` skips files > `MaxSnapshotBytes` (2 MB) (`checkpoint.go:67-69`), but edit/write accept up to `MaxReadBytes` (5 MB, `types.go:21`). A wrong-but-complete edit to a 3 MB file has no rollback.
- **Fix:** Align the ceilings (lower editable cap to snapshot cap or raise snapshot cap), **or** emit an explicit "no checkpoint (file too large) — change is irreversible" warning in the tool result.
- **Constitution:** I.
- **Cache-safety:** Cache-safe.
- **Acceptance:** Test editing a 3 MB file; assert either a checkpoint exists or the irreversibility warning is present.

---

## Workstream D — Agent instruction clarity  *(ONE deliberate cache epoch)*

> All four tasks change prompt/prefix bytes. **Land them in a single commit, regenerate `instructions_dump.golden`, `prefix_bytes.golden`, and `prefix_bytes_wire.golden` once**, and treat it as one epoch bump (D-1/D-2 = general-subagent Sidecar prefix; D-3/D-4 = main-loop Prefix). Do not interleave with other prefix edits.

### D-1 · Executor is never told to preserve user work / avoid destructive commands / verify targets — P0 · CONFIRMED · medium
- **Problem:** The general executor's system message is `SubagentGeneralSystem` (`subagents.go:86`) = `EditDisciplineBody` + "inspect before editing…" — the "Preserve user changes. Avoid destructive commands." clauses live only in the main-loop Prefix (`prompt.go:88,100-101`), which subagents never inherit. The runtime gate does NOT block `git reset --hard`, `git checkout -- .`, `git stash drop`, plain `rm file`, `mv`, or `>` truncation; in auto-accept these run unmediated (`permissions.go:160-162`).
- **Fix:** Add to `SubagentGeneralSystem`: "Preserve the user's uncommitted work — never run a command that discards changes (`git reset --hard`, `git checkout --`, `git stash drop`) or deletes/overwrites files you did not create for this task. Before any shell command, confirm its target exists and you are acting on the right path."
- **Constitution:** IX; audit-priority #2.
- **Cache-safety:** **Epoch (Sidecar).** New epoch for the general kind only; main Prefix byte-identical. Pairs with runtime hardening I-2/I-3.
- **Acceptance:** `instructions_dump.golden` regenerated; assert the clause is present in the general subagent system message and absent from the main Prefix hash change beyond expectation.

### D-2 · Executor never told its OS / shell / cwd model — P1 · CONFIRMED · medium
- **Problem:** `subagentSystemMessage` (`subagent.go:238`) = identity + `spec.System` + workspace path + capabilities — no OS, no shell. On Windows the executor runs PowerShell (`ChooseShell`, `shell.go:34-64`) but is never told, so it emits `mkdir -p`/`touch`/`grep`/`2>/dev/null` which error. Weaker sub-models fail hardest (audit priority #3).
- **Fix:** Include session-fixed OS and shell in `subagentSystemMessage` for the general kind, mirroring the main-loop ENVIRONMENT block.
- **Constitution:** IX; priorities #2, #3.
- **Cache-safety:** **Epoch (Sidecar).** OS/shell/root are session-invariant → per-kind prefix stays byte-stable across dispatches within a session. Regenerate the subagent-message goldens.
- **Acceptance:** Golden shows the ENVIRONMENT line; a same-session dispatch test confirms byte-identical general-kind prefix across two dispatches.

### D-3 · No instruction that `run_shell` cwd does not persist — P1 · CONFIRMED · low
- **Problem:** Every `run_shell` runs a fresh process at the workspace root (`shell.go:99-100`, `files.go:324`); `cd` only affects its own command. No prompt text says so; `ReadOnlyShellAllowlistBody` shows `cd path && go build` (correct pattern) but nothing states cross-call non-persistence.
- **Fix:** Add one clause to `ToolRunShellDescription` (`tools.go:36`) and/or the ENVIRONMENT section: "Each run_shell call starts fresh at the workspace root; a `cd` only lasts within that one command — combine `cd` and the command in a single call."
- **Constitution:** I; audit-priority #2.
- **Cache-safety:** **Epoch (main Prefix).** Batch here. Executor benefits (shares the `run_shell` description).
- **Acceptance:** `prefix_bytes.golden` regenerated; assert the clause present.

### D-4 · Edit-tool descriptions omit failure/recovery contract — P2 · PLAUSIBLE · low
- **Problem:** `ToolEditFileDescription`/`ToolApplyPatchDescription` (`tools.go:27,33`) state no failure modes; the exactness/recovery discipline lives only in `EditDisciplineBody` (Sidecar), which the main model — now editing `tasks.md` under the plan/execute split — no longer receives.
- **Fix:** Add a short clause to `ToolEditFileDescription`: "oldString must match exactly once; if not found the result names the nearest region — retry from that, and add surrounding context to disambiguate a repeated match."
- **Constitution:** I.
- **Cache-safety:** **Epoch (main Prefix).** Batch here.
- **Acceptance:** Golden regenerated; assert the clause present.

---

## Workstream E — Bugs & incorrect calculations

### E-1 · `DiffCounts` miscounts `-- `/`++ ` body lines as headers — P1 · CONFIRMED · low
- **Problem:** `diffcount.go:35` skips any `+++ `/`--- ` line as a file header. go-difflib renders a removed `-- x` as `--- x` and added `++ y` as `+++ y`, both skipped → under-reported add/remove counts shown per tool row and persisted into `TaskStats` and benchmark data (`turnloop.go:560`).
- **Fix:** Make counting hunk-aware: set `inHunk` on any `@@ … @@` line and count `+`/`-` body lines only while `inHunk` (file headers always precede the first `@@`). Drop the fragile space heuristic.
- **Constitution:** I, VI.
- **Cache-safety:** Cache-safe (contract; TUI/stats only). **Cluster:** land with C-1/E-3.
- **Acceptance:** Existing `diffcount_test.go` stays green; add a `-- `/`++ ` fixture asserting correct counts.

### E-2 · "Context in use" computed two different ways; /context under-reports — P1 · CONFIRMED · medium
- **Problem:** `ContextReport.Percent`/`HistoryTokens` (`contextreport.go:56,77`) use `history.EstimatedTokens()` which excludes system prompt + tool schemas (`history.go:616-617`). The live footer uses `max(history, lastRequest)` where `lastRequest = response.Usage.PromptTokens` — includes system+tools. Both write the same `m.context.Percent`. Opening /context or resuming drops the footer from request-based (~9.4%) to history-only (~1.6%), disagreeing with the engine's own compaction trigger (`maintenance.go:107-113`).
- **Fix:** Make "context in use" request-based everywhere: derive `HistoryTokens`/`Percent` from `latestPromptTokens` (the source `contextPressure` uses), with the history estimate only as a no-provider-figure fallback; compute Free from that. Both surfaces then read one figure.
- **Constitution:** VI; priority #1.
- **Cache-safety:** Cache-safe (display/telemetry). **Coordinate with H-1** (both touch /context).
- **Acceptance:** Test that footer and /context modal report the same percent, and that it matches `contextPressure`'s basis. Update `context_panel`/`actions` tests asserting history-derived percent.

### E-3 · `PatchTargetFiles` injects a phantom file from adjacent `-- `/`++ ` — P2 · PLAUSIBLE · low
- **Problem:** `tooltarget.go:66-67` treats any `--- ` immediately followed by `+++ ` as a header pair with no hunk awareness; a modification deleting `-- old` / adding `++ new` on adjacent body lines is misread as a new file section → spurious target in the tool row and `ToolTargetPaths`.
- **Fix:** Reset header detection at each `@@` boundary — accept a `--- `/`+++ ` pair as a header only before the first `@@` of a section.
- **Constitution:** I.
- **Cache-safety:** Cache-safe. **Sequencing:** land before/with A-2 (which consumes `PatchTargetFiles`), and with the C-1/E-1 cluster.
- **Acceptance:** Test a hunk changing a `-- ` comment; assert only the real file is returned.

### E-4 · `TruncateMiddle` marker understates chars trimmed — P2 · PLAUSIBLE · polish
- **Problem (two duplicate findings merged):** `format.go:113` computes `dropped = len(runes) - maxChars`, but the kept region is only `budget = maxChars - len(marker)` runes, so real removed = `dropped + len(marker)`. The label is low by the marker length (~40-50 chars). Output length itself is correct.
- **Fix:** After computing `budget`, use `dropped := len(runes) - budget` for the marker text (or reword as approximate).
- **Constitution:** VI.
- **Cache-safety:** Cache-safe (display).
- **Acceptance:** Unit test asserting the announced count equals actual removed runes.

---

## Workstream F — Concurrency & state correctness

### F-1 · Shared `*contract.Settings` written unsynchronized during a live task — P0 · PLAUSIBLE · medium · verify-then-fix
- **Problem:** `e.settings == a.settings == m.runtime.Settings` (aliased at `engine.go:310`, `runtime_build.go:490,583`). The task goroutine reads `Provider.ActiveModelID` (`turnloop.go:371`) and ranges `Provider.Models` (`contextLimit`, `engine.go:549`) with no lock; `SetEffort` uses `effortMu`, `applyModelSwitch` uses `e.mu`. The TUI `SaveSettings` action does `*a.settings = *settings` (`actions.go:27`) on a `tea.Cmd` goroutine under neither lock, reachable mid-task via un-gated `/effort`/`/permission` (`keys.go:97`, `slash.go:237,250`). **Verifier caveat:** all reachable TUI callers pass `m.runtime.Settings` (== `a.settings`), so the write is a value-identical self-copy — no torn-header panic and no wrong-model in the current codebase; `DiscoverModels` (the only true mutation of `Provider.Models`) is CLI-only. So this is a genuine `-race` data race with no demonstrated corruption today, but latent the moment any path builds a distinct Settings.
- **Fix:** (1) At `Run` start, snapshot the task-dependent model config (`ActiveModelID`, subagent model, matching `ContextLimit`) into task-local immutables (models are already session-frozen); route `Effort` exclusively through `effortMu` everywhere. (2) In `SaveSettings`, stop whole-struct-copying the shared struct while a task is live — operate on and persist a copy; let the engine re-read config at the next task boundary.
- **Constitution:** I, IX.
- **Cache-safety:** Cache-safe — model config is session-frozen; Effort rides request params, not the prefix.
- **Acceptance:** `go test -race` on a test that runs `engine.Run` while firing `/effort`; assert no race report.

### F-2 · A crash mid-JSONL-append bricks session resume — P1 · CONFIRMED · medium
- **Problem:** `appendJSONLine`/`AppendTranscript` (`session.go:99-122,51-75`) use bare `O_APPEND` + `fsync`, not the atomic temp-replace path. `readSessionJSONLines` (`session.go:124-151`) returns `nil, error` for the *whole file* on any `json.Unmarshal` failure — so one truncated trailing line loses the entire usage/invalidation ledger. Resume callers hard-fail (`runtime_build.go:314-321`). The sibling JSON readers already recover (move-aside + fallback).
- **Fix:** Make `readSessionJSONLines` tolerant of a malformed **trailing** line (return records parsed so far, or truncate/move-aside like `ReadJSON`) — under append-only writes only the last line can be partial. Alternatively/additionally degrade the `runtime_build.go` callers to an empty ledger.
- **Constitution:** VIII, IX.
- **Cache-safety:** Cache-safe (read-side recovery).
- **Acceptance:** Test a usage JSONL with a truncated final line; assert prior records load and resume succeeds.

### F-3 · `taskMu` held across the usage fsync — P2 · PLAUSIBLE · low
- **Problem:** `appendUsageLocked` runs under `taskMu` and calls `AppendUsage` → `fsync` (`usage.go:148-150`, `session.go:113-121`) while holding it. TUI read accessors (`Usage()`, `UsageRecords()`, `ContextReport()`) all take `taskMu`, so each blocks for the fsync duration — visible jank on slow/contended storage. The codebase already moves other sidecar writes off the hot path via `writeMu`.
- **Fix:** Update the in-memory ledger under `taskMu`, release it, then perform `AppendUsage` outside the lock (serialized by a dedicated mutex like `writeMu` for ordering).
- **Constitution:** I.
- **Cache-safety:** Cache-safe (lock scope reorder).
- **Acceptance:** `-race` clean; assert read accessors do not call into disk I/O under `taskMu` (structural test or review).

---

## Workstream G — Turn-loop / dispatch / workflow logic

### G-1 · H5 failure terminator counts recoverable gate rejections — P0 · CONFIRMED · medium
- **Problem:** Every outcome with `Failed==true` calls `recordTaskFailure` (`turnloop.go:548`) with no filter; `taskFailureWindowCount >= 8` force-finalizes the task (`turnloop.go:598-602`). But the shared gate returns `Failed:true` for *recoverable* refusals: execution-role gate (`rolesplit.go:93`), H1 arg-validation, H2 verbatim, repeat-limiter, read-only-shell, continuation-review mask (`gates.go`). A turn-1 batch of 8 role-gated `edit_file` calls trips the breaker on turn 1 with a misleading "8 failures / genuine blocker" report — before the model can act on a single "delegate" message.
- **Fix:** Add a `GateRejected` bool to `toolOutcome`, set by `gatedExecute`'s pre-dispatch refusals, and skip `recordTaskFailure` for those. (Do **not** use "only when `outcome.Err != nil`" — `IsToolFailure` classifies real failures from output text with nil err.) The role gate's own escalation ladder plus the B7 all-failed-turn guard and `hardTurnCeiling` remain the liveness backstops.
- **Constitution:** I, IX.
- **Cache-safety:** Cache-safe (turn-loop control flow; no golden change).
- **Acceptance:** Test a turn-1 batch of 8 role-gated edits; assert the task is NOT force-finalized and the role-gate messages are delivered.

### G-2 · Subagent turn-extension ladder is blind to `apply_patch`/`run_shell` progress — P2 · PLAUSIBLE · low · verify-then-fix
- **Problem:** The ladder extends while `capture.touchedCount()` grows (`subagent.go:522-535`), but `touchedCount` only records `read_file`/`edit_file`/`multi_edit`/`write_file` (`contextrecord.go:114-131`); `apply_patch` and `run_shell` are excluded. A continuation seeded with the predecessor's touched paths that then applies changes via `apply_patch` on known files sees `touched == progressMark` → premature `wrapUp`. **Verifier caveat:** the finding overstates the harm — a wrapped-up run still returns status `done`, not `partial`, so no forced continuation; and any single new `read_file`/`edit_file` extends the ladder, so the trigger is narrow.
- **Fix:** Count path-less mutations toward progress: in `postDispatch`, tick a dedicated progress counter for every successful mutation (`isMutation(name) && !IsReadOnlyShell`), or record `apply_patch` targets via `PatchTargetFiles`. Keep the staleness fingerprint set separate.
- **Constitution:** II, X.
- **Cache-safety:** Cache-safe (control flow).
- **Acceptance:** Test an `apply_patch`-only continuation at `turnCap`; assert the ladder extends while patches land.

---

## Workstream H — TUI correctness & polish

### H-1 · /context (and /usage) modal silently truncates its card at 80×24 — P1 · CONFIRMED · medium
- **Problem:** `renderModal` hard-truncates the message (`render_modal.go:53-56`); for an info modal at 80×24, `limit=11` but `formatContextReport` emits 13 lines, so `Main:`/`Sub-agent:` are dropped and `Models …` shows. Info modals cannot scroll (`modals.go:234-241`, `mouse.go:268-277`) — the rows are unreachable. At 80×20 it loses Cache/Hit rate/Cost/Models. Defeats 013 FR-023.
- **Fix:** Give choiceless modals a scroll window (up/down + wheel over the message, with an "N more" affordance), or shrink the card / widen the modal budget so 13 lines fit at 24 rows.
- **Constitution:** I, VI.
- **Cache-safety:** Cache-safe (presentation). **Coordinate with E-2.**
- **Acceptance:** Render test at 80×24; assert Main/Sub-agent rows are reachable (scroll) or visible.

### H-2 · `cyclePermission`/`setPermission`/`/reasoning` deref `Settings` without the nil guard — P2 · PLAUSIBLE · low
- **Problem:** `slash.go:257,245,89` read/write `m.runtime.Settings.*` unguarded, while `permissionChip`/effort chip/`sessionModelNames` all guard `!= nil`. Shift+Tab routes straight to `cyclePermission` (`keys.go:78`); `Update` has no recover around `handleKey`. Latent panic for any runtime built without Settings.
- **Fix:** Add `if m.runtime.Settings == nil { return nil }` to `cyclePermission`, `setPermission`, and the `/reasoning` handler.
- **Constitution:** IX.
- **Cache-safety:** Cache-safe.
- **Acceptance:** Test Shift+Tab on a Model with nil Settings; assert no panic.

### H-3 · `renderModal` budget vs `View()` reserve mismatch shears bottom frame — P2 · PLAUSIBLE · low
- **Problem:** `renderModal` budgets `m.height-4` (`render_modal.go:23`); `View()` reserves `m.height-6` (header 4 + mode line 2) then clamps (`view.go:36,49-50`). A budget-filling modal loses its bottom pad+border. Cosmetic.
- **Fix:** Compute `renderModal`'s budget from the same quantity `View()` reserves (`m.height - lineCount(header) - lineCount(modeLine)`), or pass it in.
- **Constitution:** I.
- **Cache-safety:** Cache-safe.
- **Acceptance:** Render test of a height-filling modal; assert the bottom border is present.

### H-4 · "Shift + Tab to cycle" hint anchored under the effort chip, not permission — P2 · PLAUSIBLE · low · polish
- **Problem:** The right cluster order is `[skills?, permissionChip, effortChip]` (`render_header.go:183-188`), so the effort chip is rightmost; the hint right-aligns to the same width and lands under effort, contradicting FR-015 ("beneath the permission chip").
- **Fix:** Right-align the hint to the permission chip's column (pad to end just before the effort chip), or reorder so permission is rightmost.
- **Constitution:** I.
- **Cache-safety:** Cache-safe.
- **Acceptance:** Render test asserting the hint's right edge aligns with the permission chip.

### H-5 · `lastStats` comment claims a persistent footer that isn't rendered — P2 · PLAUSIBLE · polish
- **Problem:** `model.go:218-221` documents `lastStats` as a "persistent usage footer … must remain visible," but it only seeds a transcript `summary` item (`update.go:237-238`) and is never read by any render path.
- **Fix:** Correct the comment to describe the one-shot summary carrier, or actually render a persistent footer if desired.
- **Constitution:** I.
- **Cache-safety:** Cache-safe (comment).
- **Acceptance:** Comment matches behavior; reviewer check.

---

## Workstream I — Security-model integrity

### I-1 · `grep`/`list_files`/`glob` read protected credential roots via a non-sensitive ancestor — P0 · CONFIRMED · high
- **Problem:** `ApprovePath` denies only targets *inside* a sensitive root (`permissions.go:103-108`). The traversal tools authorize the single root once (`files.go:361`) then enumerate children (`collectFiles`+`readTextFile`, `WalkDir`) with **no per-file sensitive check**. `grep{path:"C:\\Users\\me", pattern:"PRIVATE KEY"}` (home is not a sensitive root) issues one outside-workspace confirm; on approval it reads `~/.ssh/id_rsa`, `~/.aws/credentials`, `~/.muhiya/*` into model context and the transcript — while direct `grep{path:"~/.ssh"}` IS blocked. Defeats `docs/security.md:8`.
- **Fix:** Enforce sensitive-root denial on every traversed path: thread the guard's `g.sensitive` set (or a `Workspace.isSensitive` helper) into `collectFiles`, the Grep read loop, and the List/Glob `WalkDir` callbacks — `SkipDir` when a directory `IsInside` a sensitive root, skip any file `IsInside` one.
- **Constitution:** IX.
- **Cache-safety:** Cache-safe (runtime traversal).
- **Acceptance:** Test `grep`/`list`/`glob` of an ancestor dir containing a sensitive root; assert the sensitive files are skipped and their contents never appear. Existing direct-target block tests stay green.

### I-2 · `rm` backstop bypassed by `/bin/rm`, `\rm`, `(rm` — P0 · CONFIRMED · high
- **Problem:** `ClassifyShell`'s `rmCommand` (`risk.go:64`) anchors on start-of-string or `; & |`/whitespace; `/bin/rm -rf /`, `\rm -rf /`, `(rm -rf /)`, `; (rm -rf .)` all return not-blocked. This is the only backstop under auto-accept and for the general executor. The correct normalization already exists in `orchestrator/shellclassify.go` (`normalizeCommandWord` strips `/bin/`, leading `\`, `.exe`; `(` is a command boundary) — just not shared.
- **Fix:** Normalize/tokenize before the `rm` check by sharing the `shellclassify.go` command-position tokenizer/normalizer, so the two gates cannot drift. Preserve the existing quoted-as-data behavior (`TestClassifyShellDoesNotBlockQuotedRmAsData`).
- **Constitution:** IX.
- **Cache-safety:** Cache-safe (`risk.go` is not prefix). Pairs with D-1.
- **Acceptance:** Add `/bin/rm -rf /`, `/usr/bin/rm -rf x`, `\rm -rf /`, `(rm -rf /)`, `; (rm -rf .)` to `risk_newline_test.go`; assert all blocked; assert `git log --grep "rm -rf"` stays unblocked.

### I-3 · PowerShell alias abbreviations `ri -r -fo` bypass the recursive-force-delete block — P0 · CONFIRMED · medium
- **Problem:** The alias branch (`risk.go:91`) requires the literal substrings `-recurse` AND `-force`; the abbreviation-tolerant regex applies only to `remove-item`. `ri -r -fo <path>`, `rd -r -fo /` (valid: `-r`→Recurse, `-fo`→Force) return not-blocked and, in auto-accept, run unconfirmed (`permissions.go:157-160`).
- **Fix:** Apply the same `[a-z]*r[a-z]*` / `[a-z]*f[a-z]*` abbreviation-tolerant recurse/force detection to alias forms (`ri`/`del`/`erase`/`rd`/`rmdir`).
- **Constitution:** IX.
- **Cache-safety:** Cache-safe. **Land with I-2** (same file/tokenizer).
- **Acceptance:** Test `ri -r -fo x`, `rd -r -fo /`; assert blocked.

### I-4 · MCP OAuth tokens excluded from exact-match redaction — P1 · CONFIRMED · medium
- **Problem:** `mcpSecretValues` (`command/root.go:537-544`) collects only top-level string values from each OAuth map; the real secrets are nested (`entry["tokens"]` = access/refresh, `entry["clientInformation"]` = client_secret), so they never enter the exact set — only harmless URLs do. Transcript persistence passes no extra values (`session.go:61`). A bare opaque token (`ya29.a0Af…`) matches no generic pattern and is written verbatim to memory/harness events/events DB.
- **Fix:** Recurse into nested maps/slices in `mcpSecretValues`, collecting every string leaf ≥ 8 chars. Also pass the full MCP secret set into transcript redaction.
- **Constitution:** IX.
- **Cache-safety:** Cache-safe (redaction is off-prefix).
- **Acceptance:** Test: an OAuth token echoed in a tool result / memory candidate is redacted in transcript, harness event, and memory file.

### I-5 · `read_skill` via a symlinked `SKILL.md` reads arbitrary files — P2 · PLAUSIBLE · low
- **Problem:** `LoadSkillInstructions` does `os.Open(skill.Path)` + `LimitReader` (`skills.go:25-30`) with no `EvalSymlinks` and no sensitive-root check; a `SKILL.md` symlinked to `~/.ssh/id_rsa` is catalogued (name falls back to dir basename) and its first 32 KiB returned. Precondition: local write to a trusted skill root (hence low).
- **Fix:** `EvalSymlinks(skill.Path)` before reading; reject/skip any `SKILL.md` whose real path resolves inside a sensitive root (optionally reject symlinked `SKILL.md` entirely).
- **Constitution:** IX.
- **Cache-safety:** Cache-safe (does not alter legitimate catalog bytes).
- **Acceptance:** Test discovery/read of a symlinked SKILL.md pointing into a sensitive root; assert it is skipped.

---

## Conflicts & Sequencing

1. **Unified-diff parser cluster (C-1, E-1, E-3):** same `-- `/`++ ` root cause across `patch.go`, `diffcount.go`, `tooltarget.go`. Fix as one changeset (count-bounded / hunk-aware parsing) so the three cannot diverge again. **C-5** (trailing newline) rides the same `patch.go` change.
2. **E-3 before A-2:** A-2 derives `apply_patch` targets via `PatchTargetFiles`; land the E-3 phantom-file fix first so A-2 invalidates only real paths.
3. **Shell gate hardening (I-2, I-3) ↔ executor instruction (D-1):** complementary defenses for the same auto-accept destructive-delete exposure. Ship the runtime gates (I-2/I-3, cache-safe, immediate) in Phase 0; ship D-1 in the prompt epoch. The runtime gate is the enforceable backstop; the instruction reduces attempts.
4. **E-2 ↔ H-1:** both touch /context. E-2 fixes *which number*; H-1 fixes *whether all rows show*. Land E-2 first (single source figure), then H-1 (scroll/fit) so the modal displays the corrected number in full.
5. **F-1 ↔ effort handling:** F-1's Effort-through-`effortMu` change must not conflict with any D-workstream effort text; F-1 is runtime-only, D is prompt-only — no byte overlap, but land F-1 before touching effort call sites.
6. **A-1 depends on nothing** but protects every resume path the C-2/F-2 fixes also touch; land it early.

---

## Recommended Execution Order (phased)

**Phase 0 — P0 correctness & security, zero prefix change (ship first, independently):**
`I-1`, `I-2`, `I-3` (credential + destructive backstops) · `B-1` (idle-timeout recovery) · `C-1`+`E-1`+`E-3`+`C-5` (patch-parser cluster) · `A-1` (prefix-drift false abort) · `G-1` (H5 gate) · `F-1` (settings race).

**Phase 1 — P1 correctness, zero prefix change:**
`A-2` (apply_patch ledger) · `C-2` (CRLF) · `C-3` (UTF-8 read) · `C-4` (shell timeout) · `F-2` (JSONL resume) · `I-4` (MCP redaction) · `E-2` (context calc) · `H-1` (context modal).

**Phase 2 — THE cache epoch (one commit, one golden regen):**
`D-1`, `D-2`, `D-3`, `D-4` together. Regenerate `instructions_dump.golden`, `prefix_bytes.golden`, `prefix_bytes_wire.golden`. No other prefix-touching change may share or straddle this commit.

**Phase 3 — P2 polish & verify-then-fix (all cache-safe):**
`A-3`–`A-8`, `B-2`, `B-3`, `B-4`, `C-6`, `C-7`, `E-4`, `F-3`, `G-2`, `H-2`, `H-3`, `H-4`, `H-5`, `I-5`. Confirm each PLAUSIBLE finding's trigger before applying.

---

## Verification Gauntlet

**Offline gates (must pass on every phase):**
- `go fmt ./... && go vet ./...` clean; `staticcheck ./...` at 0 (per the production-grade baseline).
- `go build ./...` clean.
- `go test ./...` green; **`go test -race ./...`** green (F-1, F-3 specifically; CI must run `-race`).
- **Prefix-stability gates:** Phases 0/1/3 must leave `prefix_bytes.golden`, `prefix_bytes_wire.golden`, and `instructions_dump.golden` **byte-identical** (any diff outside Phase 2 is a regression). Phase 2 regenerates all three exactly once; diff-review the regeneration to confirm only the four intended clauses changed.
- Per-task acceptance tests above, each an offline unit/render/parser test.
- Targeted regression fixtures: the five shell spellings (I-2), the two PowerShell aliases (I-3), the `-- `/`++ ` diff fixtures (C-1/E-1/E-3), the multi-byte read line (C-3), the mixed-ending edit (C-2), the truncated JSONL tail (F-2), the interrupted-tool-call resume (A-1).

**Live gates (Constitution X — owner cost approval required):**
- Realistic multi-turn before/after with model + gateway + effort + workload held constant, on **DeepSeek Reasonix (reference)** plus at least one of **MiniMax/GLM** to validate B-1/B-2/B-4 cross-provider behavior.
- Confirm the Phase-2 epoch produces exactly **one** cache-miss turn, then steady-state cache hit ≥ prior plateau (~96.5%) — report the whole picture (total tokens *and* hit rate) per Constitution VI.
- Provider-usage honesty spot-check: verify /context and the live footer now agree (E-2) against provider `prompt_tokens`.
- A silent-stall live probe (half-open connection) to confirm B-1 yields a retried turn rather than "stopped."

**Provenance note:** 19 findings are CONFIRMED and code-traced; 25 are PLAUSIBLE (low/polish) and carry a `verify-then-fix` acceptance step — reproduce the trigger before landing. Two high-severity re-grades were applied by verifiers (`shared-settings` high→medium, `executor-destructive` high→medium); `idle-timeout`, `rm-backstop`, `grep-list-glob`, and `patch-delete-dashdash` remain high.