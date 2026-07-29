# MuhiyaCode Architecture Decisions

## AD-1 — One session model authority

The session runtime stores the selected immutable catalog record. Every
planning, coding, recovery, verification-selection, and finalization request
uses that model. The gateway may adapt wire syntax and preserve an upstream
provider route, but it cannot choose another model. A response is accepted only
when its model-resolution receipt matches the selected record and target.

## AD-2 — Exact financial authority

Nano-USD integers are authoritative for pricing, admission, reservations,
top-ups, settlement, and ledger entries. Legacy floating-point fields are
display/API compatibility boundaries only. Admission reserves the conservative
maximum before provider contact; settlement and the immutable request log share
one transaction and cannot charge above the reservation.

## AD-3 — Versioned catalog contract

The gateway publishes canonical schema-v2 records tagged `muhiyacode`.
Capabilities, context/output limits, supported parameters, provider family,
adapter version, cache contract, exact pricing, health, and compatibility epoch
travel together. Clients validate a whole document and atomically replace a
separate last-known-good cache. Catalog refresh never changes an active model.

## AD-4 — Cache lineage is per model

Each session keeps an independent lineage for every model: compatibility/cache
epoch, prefix hash, and observed route. Switching A → B → A restores A when its
compatibility epoch still matches. MiniMax through OpenRouter uses gateway-owned
Redis affinity scoped by virtual key, session, and immutable model record.

## AD-5 — Durable execution before mutation

The checksummed execution journal records task/tool start, mutation intent,
terminal certainty, usage, verification evidence, and projection checkpoints.
A mutation intent is durable before dispatch. Startup detects unfinished
dispatches; unresolved mutations block new writes until inspected and
reconciled. Transcript, history, usage, and task graph are projections, not
independent authorities.

## AD-6 — Retry ownership is bounded

The client provider permits one retry by default and retains the same immutable
model. The outer task loop has bounded recovery and failure breakers. OpenRouter
route rebind is allowed once before downstream bytes only. No partial stream is
replayed or rerouted.

## AD-7 — Deterministic maintenance uses no model

Compaction, indexing, catalog sync, checkpointing, verification selection, and
recovery reports are deterministic local operations. They cannot invoke an
alternate or hidden model.

## AD-8 — Verification is evidence keyed by workspace state

Verification commands are selected from project manifests or
`.muhiya/verification.json` and scoped to changed files. A successful command
is reused only when the repository fingerprint is unchanged. This removes
repeated test burn without treating an old result as evidence for new code.

