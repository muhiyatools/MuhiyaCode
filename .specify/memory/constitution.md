<!--
Sync Impact Report
==================
Version change: (template, unversioned) → 1.0.0
Rationale: Initial ratification. All placeholder tokens replaced with concrete,
project-specific content derived from user-supplied principles.

Modified principles: n/a (initial adoption)

Added sections:
- Core Principles (I–X, all ten newly defined)
- Measurement & Benchmarking Standards
- Development Workflow & Quality Gates
- Governance

Removed sections: none (template placeholders replaced)

Templates requiring updates:
- ✅ .specify/templates/plan-template.md — no change needed; its Constitution Check
  section derives gates from this file at plan time.
- ✅ .specify/templates/spec-template.md — no change needed; mandatory, measurable
  Success Criteria already align with Principles VI and X.
- ✅ .specify/templates/tasks-template.md — updated; "tests are optional" note now
  carries the constitutional exception for improvement claims (Principle X).
- ⚠ .specify/templates/commands/*.md — directory does not exist in this repo; n/a.
- ✅ README.md, docs/agent-design.md, docs/architecture.md, docs/security.md — no
  prior principle references exist (initial adoption); no updates required.

Follow-up TODOs: none. No placeholders deferred.
-->

# MuhiyaCode Constitution

## Core Principles

### I. Correctness Before Optimization

Functional correctness MUST take precedence over every optimization goal, including cache
efficiency, token savings, cost, and latency. An optimization that changes agent behavior,
degrades answer quality, breaks a tool contract, corrupts session state, or weakens the
security model MUST be rejected or reworked regardless of its measured savings. When a
correctness fix and an efficiency gain conflict, correctness wins, and the conflict MUST be
recorded in the change's plan or review notes.

Rationale: a cheap but wrong turn costs more than the tokens it saves — in rework, in user
trust, and in corrupted downstream context.

### II. Cache Efficiency Without Quality Loss

Cache efficiency MUST be improved without reducing agent quality. A change intended to raise
provider prefix-cache hit rates MUST NOT remove, truncate, reorder, or delay content the model
needs to do its job — instructions, tool schemas, or task-relevant context. Every
cache-affecting change MUST state what content moved or disappeared and why quality is
preserved, and MUST be validated under Principle X before merge. A higher hit rate obtained by
starving the model of context is a regression, not an improvement.

### III. Deterministic Stable Prefix

The system prompt, tool schemas, and other stable context MUST remain byte-identical across
requests within a session, and MUST be deterministic across sessions given identical
configuration. Serialization of the stable prefix MUST NOT introduce nondeterminism:
no timestamps, no random or map-iteration-dependent ordering, no locale-dependent formatting,
and no per-request mode switching (e.g., lite/full prompt variants) inside the prefix.
Content that must vary per request MUST NOT be placed in the stable prefix.

Rationale: provider prefix caches match byte-for-byte from position zero; one early byte of
drift invalidates the entire cached prefix for the rest of the session.

### IV. Separation of Dynamic and Cached Content

Dynamic content MUST be kept separate from reusable cached content. Per-turn state — task
classification, budgets, goals, plan-mode instructions, effort hints, timestamps — MUST ride
on the user message, the latest turn, or request parameters (headers/fields), never be
interleaved into the stable prompt, tool schemas, or settled history. New features MUST
default to this placement; putting dynamic material anywhere earlier in the request requires
written justification in the feature plan.

### V. No Redundant Retransmission

Unchanged files, settled conversation history, and completed tool outputs MUST NOT be sent to
the provider repeatedly when avoidable. Settled history MUST grow append-only; rewriting it
(compaction, folding, redaction sweeps) is a cache-busting event and MUST be justified by real
context pressure, not run speculatively. Duplicate and already-covered file reads MUST be
blocked while their source results remain intact in context; stale reads MUST be superseded
rather than accumulated; completed-task tool payloads MUST be folded only at settled
boundaries.

### VI. Honest Measurement

Token usage, cache reads, cache writes, cost, and latency MUST be measured honestly: taken
from provider-reported usage fields on real requests, not from local estimates. Estimates,
when unavoidable, MUST be labeled as estimates and never presented as measurements. Reports
MUST show the whole picture — a change that raises cache-hit rate while growing total tokens
or cost MUST report both numbers. Selective reporting of favorable metrics is a violation
equivalent to having no measurement at all.

### VII. Reference Architecture: DeepSeek Reasonix

DeepSeek Reasonix MUST be studied as a reference for cache-related architecture and prompt
organization before significant cache-affecting design work begins. Findings MUST be captured
in the feature's research notes (e.g., `specs/<feature>/research.md`) with concrete
observations about prefix layout, cache boundaries, and prompt organization. The reference
informs design; it does not mandate imitation. Deviations from the reference approach are
acceptable and MUST be justified against MuhiyaCode's constraints (OpenAI-compatible
gateways, multi-provider compatibility, terminal workflows).

### VIII. Improve, Don't Rewrite

Existing behavior MUST be improved through the smallest change that achieves the measured
goal. Rewriting a working subsystem requires written justification — in the plan's Complexity
Tracking section — demonstrating why incremental improvement is insufficient. Established
compatibility boundaries (the `~/.muhiya` state layout, session formats, the OpenAI-compatible
wire protocol) MUST be preserved unless a migration plan accompanies the change.

### IX. Clean, Maintainable, Secure, Provider-Compatible Code

Code MUST remain clean, maintainable, secure, and provider-compatible:

- MUST pass `go fmt`, `go vet`, and `go test ./... -count=1` before merge; race-enabled tests
  MUST pass on CGO-capable runners.
- MUST follow the existing idioms and structure of the package it touches; new dependencies
  require justification.
- MUST NOT weaken the documented security model (real-path containment, workspace trust,
  mutation approval, secret redaction, destructive-command blocks).
- MUST remain compatible with generic OpenAI-compatible endpoints: provider-specific cache
  behavior MAY be exploited when available but MUST degrade gracefully when the provider does
  not offer it (absent usage/cache fields tolerated, no hard dependence on one vendor).

### X. Verified Improvements

Every improvement claim MUST be verified with realistic before-and-after testing. "Realistic"
means representative multi-turn agent sessions against a real endpoint — not single-shot
prompts or synthetic token counts. The before and after runs MUST hold model, gateway, effort
level, and workload constant, and MUST be measured per Principle VI. Results MUST be recorded
with the feature's artifacts so they can be reproduced. A change whose verification shows
quality or correctness regression is blocked by Principles I and II regardless of its
efficiency gains.

## Measurement & Benchmarking Standards

These standards operationalize Principles VI and X for all performance and caching work:

- Primary metrics: prompt tokens, completion tokens, cache-hit prompt tokens, cache-miss
  prompt tokens (as reported by the provider usage payload), request count, wall-clock
  latency, and computed cost.
- Benchmarks MUST use realistic multi-turn coding sessions exercising tools, file reads,
  edits, and history growth — the workloads the runtime actually serves.
- Cache warm-up MUST be accounted for: first requests write the cache, later requests read
  it. Cold-start and steady-state figures MUST be reported separately when the distinction
  affects conclusions.
- Before/after comparisons MUST vary only the change under test. Model, gateway, effort
  level, and prompt workload stay fixed.
- Benchmark procedures and raw results MUST be stored with the feature
  (`specs/<feature>/`) so any reviewer can rerun them.

## Development Workflow & Quality Gates

- Every plan MUST pass the Constitution Check gate before Phase 0 research and again after
  Phase 1 design; violations MUST be documented in Complexity Tracking with the rejected
  simpler alternative.
- Cache-affecting changes (system prompt composition, tool-schema serialization, history
  management, compaction) MUST include an automated prefix-stability check asserting the
  stable prefix is byte-identical across consecutive turns.
- Improvement claims MUST attach the before/after evidence required by Principle X; reviewers
  MUST verify the evidence exists and covers both efficiency and quality.
- Changes touching the permission layer, path containment, secret handling, or command
  blocking MUST be reviewed against `docs/security.md`.
- `README.md`, `docs/agent-design.md`, and `docs/architecture.md` MUST be updated in the same
  change when token or cache design behavior they describe is altered.

## Governance

This constitution supersedes all other development practices for this repository. Where
guidance elsewhere (README, docs, templates, tooling defaults) conflicts with it, the
constitution wins and the conflicting guidance MUST be corrected.

Amendment procedure: amendments are proposed as a change to this file with a written
rationale, reviewed like any other change, and take effect on merge. Each amendment MUST
update the version below and the Sync Impact Report comment, and MUST propagate any affected
gates into `.specify/templates/` in the same change.

Versioning policy (semantic):

- MAJOR: backward-incompatible governance changes — removing or redefining a principle.
- MINOR: adding a principle or section, or materially expanding mandatory guidance.
- PATCH: clarifications, wording, and non-semantic refinements.

Compliance review: every feature plan re-derives its Constitution Check from the current
version of this file. Reviewers MUST treat unverified improvement claims, nondeterministic
prefix serialization, and unjustified rewrites as blocking findings. Runtime development
guidance lives in `README.md`, `docs/agent-design.md`, `docs/architecture.md`, and
`docs/security.md`.

**Version**: 1.0.0 | **Ratified**: 2026-07-11 | **Last Amended**: 2026-07-11
