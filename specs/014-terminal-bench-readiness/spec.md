# Feature Specification: Terminal-Bench Readiness

**Feature Branch**: `014-terminal-bench-readiness`  
**Created**: 2026-07-24  
**Status**: In progress  
**Authoritative research**: [`docs/MUHIYACODE_TERMINAL_BENCH_READINESS_PLAN.md`](../../docs/MUHIYACODE_TERMINAL_BENCH_READINESS_PLAN.md)

## Problem Statement

MuhiyaCode has a capable interactive and one-shot execution core, but it cannot yet be evaluated fairly as a serious coding agent in a fresh, non-interactive Terminal-Bench-style container. The current runtime blocks mutations through workspace trust, requires mutable persisted configuration, lacks a Harbor-style result contract, and does not reliably verify, bound, or fully describe a benchmark run.

## User Stories

### User Story 1 — Run one isolated benchmark task (Priority: P1)

A benchmark harness starts MuhiyaCode in a pristine, non-TTY container, supplies one task and inline configuration, and receives a machine-readable result with a durable trajectory link and an unambiguous exit code. The run never requires an existing `~/.muhiya` directory or interactive approval.

**Independent test**: A fresh temporary `MUHIYA_HOME` with inline configuration can complete a trivial write-and-test fixture in a non-TTY subprocess; the output is valid JSON, links to a trajectory, and exits with the documented code.

### User Story 2 — Execute safely and reliably in a sandbox (Priority: P2)

The agent can read, edit, test, clean build artifacts, and recover from malformed calls inside the task workspace while preserving containment, sensitive-root protection, and the prohibition on repository publication actions.

**Independent test**: Focused workspace and orchestrator suites prove long-line edits, atomic multi-edits, safe in-workspace cleanup, strict argument validation, and tool-call rescue; negative tests prove out-of-root and sensitive targets remain blocked.

### User Story 3 — Use context and tokens efficiently without losing correctness (Priority: P3)

The agent retains important read content through compaction, invalidates only stale inspections, normalizes equivalent searches, and reports honest fallback cost estimates when provider pricing is absent.

**Independent test**: Deterministic compaction and scoped-invalidation tests pass, while stable-prefix tests remain byte-identical across requests.

### User Story 4 — Verify work and terminate truthfully (Priority: P4)

The agent runs relevant project-marker tests after code changes, executes final-turn tool calls, re-prompts after a no-op first turn, gives exploration tasks bounded runway, and terminates stalled or provider-unavailable work with an explicit reason.

**Independent test**: Fault-injection and fixture tests demonstrate verification, bounded recovery, distinct `blocked`/`timeout`/`budget` outcomes, and no false clean completion.

### User Story 5 — Compare model configurations reproducibly (Priority: P5)

Fixed-model runs cannot silently substitute another model. Routed runs explicitly opt in and record every model and reason. Repeated benchmark runs report result, usage, cost, timing, checks, and trajectory data under an identical harness contract.

**Independent test**: A catalog fixture proves fixed-model pinning and missing-model blocking; a routed fixture emits model-switch entries; the harvester aggregates repeated records without task-specific knowledge.

## Functional Requirements

- **FR-001**: `muhiyacode bench` must accept a task from a file or standard input and emit exactly one structured final record to stdout and, when requested, an output file.
- **FR-002**: A run record must include status, termination information, session and trajectory identity, durations, usage/cost attribution, tool/check counts, changed files, configuration, errors, and verification outcome.
- **FR-003**: The adapter must distinguish `pass`, `fail`, `timeout`, `blocked`, and `error` with documented process exit codes; `completed` remains loop hygiene, not an objective grader result.
- **FR-004**: Benchmark runs must support in-memory CLI/environment overrides for model, provider URL, API key, effort, context limit, timeout, token budget, cost budget, advisor mode, and seed without writing settings.
- **FR-005**: Auto-accept benchmark mode may trust only the configured workspace automatically; all normal-mode confirmations, sensitive-root blocks, and real-path containment must remain fail-closed.
- **FR-006**: A task must have hard wall-clock, token, and cost bounds, each reflected in a final record even when cancellation occurs while dispatching.
- **FR-007**: Code-task runs with changed files and a discoverable generic project test marker must perform one bounded verification stage and record the actual outcome.
- **FR-008**: Benchmark mode must avoid onboarding, persistent-state mutation, startup discovery/probes, silent catalog substitution, and `git add`/`commit`/`push`.
- **FR-009**: Adapter behavior and verification discovery must be generic: no Terminal-Bench task names, fixture names, hidden tests, or gold-patch knowledge may influence execution.
- **FR-010**: All changes must retain existing cross-platform process cleanup, provider compatibility, durable JSONL history, prefix-shape safety, and current permission protections.

## Success Criteria

- **SC-001**: A Linux non-TTY container smoke run with a fresh state directory performs a workspace mutation, runs a check, and produces a valid run record without manual setup.
- **SC-002**: Every ended adapter run emits an interpretable status and trajectory path; a timeout exits promptly and never relies on an external hard kill to report its outcome.
- **SC-003**: Fixed-model benchmark runs use only the requested model or end `blocked`; routed runs identify all model changes.
- **SC-004**: The adapter and fault-injection suites cover timeout, budget, block, provider failure, no-op response, final-turn tool call, and verification behavior.
- **SC-005**: Tool reliability, compaction, and prompt/cache regressions are covered by targeted tests and preserve existing repository-wide quality gates.
- **SC-006**: The repeated-run harvester produces metrics defined in the authoritative research document without task-specific hardcoding.

## Non-Goals

- This feature does not claim objective task pass/fail in place of the benchmark grader.
- This feature does not embed any benchmark fixture or hidden-test knowledge in prompts, tools, or control flow.
- This feature does not replace the interactive TUI or weaken normal-mode workspace approval.
