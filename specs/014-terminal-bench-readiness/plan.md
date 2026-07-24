# Implementation Plan: Terminal-Bench Readiness

**Branch**: `014-terminal-bench-readiness` | **Date**: 2026-07-24 | **Spec**: [spec.md](spec.md)

## Summary

Implement the benchmark execution contract described by the research roadmap while preserving the agent's established safety and reliability controls. The implementation is intentionally incremental: establish the adapter and result data model, make headless execution bounded and configurable, improve tool/context/termination behavior, then validate it with reproducible harness runs.

## Technical Context

- **Language**: Go, repository module `github.com/muhiya/muhiyacode`
- **Entry surface**: `cmd/muhiyacode/main.go` → `internal/command/root.go`
- **Runtime seams**: `internal/command`, `internal/app`, `internal/orchestrator`, `internal/workspace`, `internal/gateway`, and `internal/state`
- **Persistence**: session transcript and usage JSONL plus persisted settings; benchmark mode must use an isolated temporary state root and avoid configuration writes
- **Primary tests**: package-level Go tests, adapter subprocess fixtures, gateway fault injection, and Linux container smoke coverage

## Constitution Check

- **Correctness and security**: Permission changes are gated to benchmark auto-accept and tested against sensitive/out-of-root targets; no normal-mode bypass is introduced.
- **Stable prefix and dynamic separation**: Budget, status, and per-run configuration remain runtime/tail data. Any prompt or schema change retains prefix-shape and deterministic-stability tests.
- **Honest measurement**: Provider-reported usage remains authoritative. Price-table results are marked estimated, and repeated-run results preserve raw run records.
- **Improve, do not rewrite**: The adapter reuses the existing application and engine path; targeted changes extend existing state, telemetry, dispatch, and workspace seams.
- **Compatibility**: Generic OpenAI-compatible providers continue to work when optional usage, cost, seed, or reasoning fields are absent.

## Implementation Phases

1. **Foundation and adapter**: Add the isolated `bench` command, generic result schema, exit mapping, and per-task harvester scaffold.
2. **Headless correctness**: Scope auto-trust to auto-accept, apply inline configuration in memory, skip benchmark startup mutation, enforce timeout/budget/status semantics, and cap post-header streams.
3. **Tool and container reliability**: Address long-line reads, Go-only inspection guidance, atomic multi-edits, safe sandbox cleanup, bench repository protections, strict call validation, and rescue decoding.
4. **Context and cost efficiency**: Make compaction recoverable/deterministic, scope invalidation, normalize search keys, annotate stale knowledge, and compute labelled fallback estimates.
5. **Verification and recovery**: Force generic verification where appropriate, preserve final-turn calls, avoid no-op success, detect stalled work, permit bounded exploration, and make provider recovery explicit.
6. **Model integrity and validation**: Pin fixed-model runs, record routing, conditionally send provider fields, add seed support, execute repeatable benchmark and anti-leakage validation, and complete repository gates.

## Key Safety Decisions

- A bench run must not claim objective pass solely from model text; external grading remains authoritative.
- Auto-trust must be limited to the canonical configured workspace under auto-accept, and must never admit sensitive or path-escaping targets.
- Destructive shell exceptions must resolve canonical operands first; only contained non-sensitive paths are eligible.
- Verification commands are selected only from generic project markers, never task-specific names.
- The adapter will operate one process per task and set an isolated `MUHIYA_HOME` to prevent cross-task state transfer.

## Checkpoint Policy

[`tasks.md`](tasks.md) is the continuation source of truth. Mark a task `[X]` only after its implementation and specified focused test are complete. If execution stops, resume at the first unchecked task, review its dependencies, and retain all existing working-tree changes.
