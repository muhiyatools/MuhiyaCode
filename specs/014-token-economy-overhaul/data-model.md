# Data Model: Token Economy Overhaul

**Status**: Implementation contract
**Compatibility**: Additive records first; persistence migrations must be idempotent and preserve existing `usage.jsonl`, history snapshots, and session identities.

## 1. Stability and ownership hierarchy

```text
VisibleSession (one immutable model + upstream affinity)
├── StablePrefixEpoch (system/core tools/project root hash)
├── TaskEpoch 1 (related prompt chain)
│   ├── ExecutionBudget
│   ├── RequestPlan / RequestEconomyRecord *
│   ├── ContextManifest *
│   ├── EvidenceArtifact * -> ObservationCard *
│   └── TaskCapsule (on settlement)
├── TaskEpoch 2
└── ...

ProjectStore
├── Durable memory index/topics
├── Repository/symbol index
└── TaskCapsule index
```

The visible session is a user-facing continuity boundary. A task epoch is a provider-context working-set boundary. Starting a task epoch never changes the session model.

## 2. `TaskEpoch`

Suggested package: `internal/contextengine` or `internal/orchestrator/context_epoch.go` initially; persistence in `internal/state`.

| Field | Type | Rule |
|---|---|---|
| `id` | string | Stable UUID/ULID, unique inside session |
| `sessionID` | string | Existing visible session owner |
| `ordinal` | uint64 | Monotonic per session |
| `modelID` | string | Copied from immutable session model; cannot change |
| `upstreamPin` | string | Same session affinity; epoch is not a routing switch |
| `goal` | string | Sanitized user goal; bounded |
| `class` | TaskClass | Local classification |
| `risk` | []RiskCode | Deterministic risk findings |
| `state` | EpochState | `open`, `settling`, `settled`, `failed`, `aborted` |
| `parentEpochID` | string? | Set for explicit follow-on/fork relationships only |
| `relatedCapsules` | []CapsuleRef | Selected before first request; immutable for request 1 |
| `startedAt` | timestamp | Persistence timestamp, never prompt prefix content |
| `settledAt` | timestamp? | Set transactionally with capsule |
| `contextRevision` | uint64 | Increments on deliberate request-history replacement |
| `prefixRevision` | uint64 | Existing stable-prefix epoch identifier |
| `lastPhase` | ExecutionPhase | Recovery/resume source |

### State transitions

```text
open -> settling -> settled
  |         |
  |         -> failed
  -> failed
  -> aborted
```

- `settling` persists the task capsule and final evidence references.
- A failure during settlement leaves the epoch `open` or `failed` with its previous context intact; it must not silently become `settled`.
- A settled epoch is immutable except for migration metadata and stale-reference annotations.

## 3. `ExecutionBudget`

| Field | Type | Meaning |
|---|---|---|
| `mode` | enum | `off`, `observe`, `balanced`, `aggressive` |
| `class` | TaskClass | Budget family |
| `riskLevel` | enum | `low`, `medium`, `high`, `critical` |
| `mainRequestsSoft/Hard` | int | Warning and absolute governor levels |
| `auxRequestsSoft/Hard` | int | Separate auxiliary allowance |
| `promptTokensSoft/Hard` | int64? | Provider-reported cumulative input; nil means observe-only |
| `cacheMissTokensSoft/Hard` | int64? | Provider-reported or unavailable |
| `outputTokensSoft/Hard` | int64? | Includes thinking when provider reports it as output |
| `inlineObservationSoft/Hard` | int64 | Estimated model-visible result tokens |
| `toolCallsSoft/Hard` | int | Successful + failed dispatched calls; gate rejections separate |
| `wallTimeSoft/Hard` | duration | Does not override user cancellation |
| `remaining*` | derived | Never persisted as authority; recompute from ledger |
| `overrideCount` | int | Count of accepted hard-budget overrides |

### Budget profiles before live tuning

These are initial experiment bands, not defaults to ship without baseline evidence:

| Class | Main requests soft/hard | Output per request default | Cumulative input target |
|---|---:|---:|---:|
| chat | 1/2 | 512 | 8k |
| tiny | 2/4 | 1,200 | 25k |
| small | 4/7 | 2,000 | 60k |
| standard | 7/12 | 3,500 | 160k |
| large | 12/20 | 6,000 | workload-derived |
| epic | milestone-based | 8,000 | milestone-derived |

Hard budgets are bypassable only through a persisted `BudgetOverride`.

## 4. `BudgetOverride`

| Field | Type | Rule |
|---|---|---|
| `requestSeq` | uint64 | Request that needs the override |
| `dimension` | enum | requests/input/output/tools/time/observation |
| `reason` | enum | `correctness`, `safety`, `explicit_scope`, `required_verification`, `provider_recovery`, `user_steering`, `migration_compatibility` |
| `evidence` | string | Bounded deterministic fact, not free-form chain of thought |
| `amount` | int64 | Added allowance |
| `approvedBy` | enum | runtime policy/user |
| `at` | timestamp | Ledger only |

`convenience`, `model_preference`, and `more_thorough` are invalid reasons.

## 5. `ExecutionPhaseState`

| Field | Type | Rule |
|---|---|---|
| `phase` | enum | `orient`, `inspect`, `change`, `verify`, `finish`, `recover` |
| `enteredAtRequest` | uint64 | Monotonic |
| `objective` | string | One bounded next outcome |
| `requiredEvidence` | []EvidenceRequirement | Concrete facts needed to leave phase |
| `satisfiedEvidence` | []EvidenceRef | Valid current facts only |
| `attempts` | int | Phase-local retries |
| `lastFailureClass` | string? | Dedupe/recovery input |
| `transitionReason` | enum | evidence_complete/user_change/failure_recovery/scope_escalation/finalize |

### Valid transitions

```text
orient -> finish                      (chat/no-work)
orient -> inspect                     (workspace evidence required)
inspect -> change                     (change target known)
inspect -> verify                     (diagnosis/read-only task)
inspect -> finish                     (answer from evidence)
change -> verify                      (mutation succeeded)
verify -> change                      (proving check found actionable defect; bounded)
verify -> finish                      (proof complete)
any -> recover -> previous/new phase  (bounded failure path)
```

Invalid transitions are programmer errors in tests and runtime failures in production; they are never silently accepted.

## 6. `RequestPlan`

Generated locally immediately before request assembly.

| Field | Type | Rule |
|---|---|---|
| `epochID` | string | Active task epoch |
| `requestSeq` | uint64 | Same identity as usage record |
| `phase` | ExecutionPhase | Current state |
| `reasoningTier` | contract.ReasoningTier | Phase/risk/provider selected |
| `maxOutputTokens` | int | Provider-profile floor <= value <= context allowance |
| `toolPolicy` | enum | `none`, `core`, `broker`, `all_loaded` |
| `expectedResult` | enum | answer/tool_calls/verification/final |
| `allowedRecovery` | RecoveryPolicy | One explicit bounded policy |
| `contextBudget` | SegmentBudget | Input allocation by segment class |
| `stopConditions` | []StopCondition | Evidence and budget conditions |
| `decisionCodes` | []string | Stable reason codes for telemetry |

No LLM call creates `RequestPlan`.

## 7. `ContextManifest`

One manifest describes the exact ordered logical request before provider adaptation.

| Field | Type | Rule |
|---|---|---|
| `requestSeq` | uint64 | Join key |
| `wireHash` | sha256 | Exact final serialized request bytes when capturable |
| `stablePrefixHash` | sha256 | Tools + system stable boundary |
| `messagePrefixHash` | sha256 | Settled message boundary |
| `segments` | []ContextSegment | Ordered, non-overlapping logical pieces |
| `estimatedTokens` | int | Calibrated/tokenizer estimate |
| `providerPromptTokens` | int? | Exact reported total after response |
| `residualTokens` | int? | Provider total minus allocated estimates |
| `measurementKind` | enum | exact-provider / exact-tokenizer / calibrated-estimate |

### `ContextSegment`

| Field | Type | Examples |
|---|---|---|
| `id` | string | deterministic within manifest |
| `kind` | enum | core_tools, deferred_descriptor, system, project_root, current_goal, current_plan, prior_capsule, assistant_reasoning, observation, governor, user, summary |
| `sourceRef` | string | message ID, artifact handle, capsule ID, prompt rule ID |
| `stability` | enum | global/session/epoch/request |
| `byteStart/byteEnd` | int? | Exact when final serializer supports offsets |
| `bytes` | int | Exact logical payload bytes |
| `estimatedTokens` | int | Labelled estimate unless exact tokenizer |
| `fingerprint` | sha256 | Content identity |
| `required` | bool | Protected from selector eviction |
| `relevanceScore` | float? | Only retrieved units |
| `dropReason` | enum? | Filled in candidate manifests/shadow decisions |

Segments reconcile through a residual bucket; estimates are never forced to look exact.

## 8. `RequestEconomyRecord`

Extend or link from existing `contract.UsageRecord` rather than creating a second source of provider truth.

| Field | Type | Rule |
|---|---|---|
| `usageSeq` | uint64 | Existing usage row identity |
| `sessionID/epochID` | string | Scope |
| `phase` | ExecutionPhase | Next-decision category |
| `transport` | enum | openai-compatible/anthropic-compatible/etc. |
| `providerProfileVersion` | string | Reproducibility |
| `promptTokens` | int? | Provider reported |
| `outputTokens` | int? | Provider reported |
| `cacheReadTokens` | int? | Provider reported |
| `cacheWriteTokens` | int? | Provider reported |
| `uncachedInputTokens` | int? | Provider reported/contract-derived with derivation tag |
| `creditCharge` | decimal? | Provider/gateway reported only |
| `costUSD` | decimal? | Existing honest pricing rules |
| `finishReason` | string | Provider response |
| `durationMS` | int64? | Measured |
| `retryOf` | uint64? | Same logical step retry |
| `manifestHash` | sha256 | Links exact composition |
| `budgetDecision` | enum | within/soft-warning/override/stopped |
| `attributionConfidence` | enum | provider-exact/tokenizer-exact/calibrated/unknown |

Derived fields such as replay amplification remain report projections, not persisted truth.

## 9. `EvidenceArtifact`

Storage candidate: `~/.muhiya/sessions/<id>/artifacts/` with content-addressed blobs and a journaled metadata index. Security permissions match existing session state.

| Field | Type | Rule |
|---|---|---|
| `id` | sha256 | Hash of canonical raw bytes + type domain separator |
| `sessionID/epochID` | string | Authorization owner |
| `workspaceID` | string | Prevent cross-workspace fetch |
| `toolName` | string | Original audited tool identity |
| `toolCallID` | string | Join to transcript |
| `mediaType/encoding` | string | text/plain, application/json, binary; utf-8/base64/etc. |
| `rawBytes` | int64 | Exact size |
| `createdAt` | timestamp | Retention |
| `workspaceFingerprint` | string? | Source validity |
| `sourcePath` | string? | Redacted/relative, containment-validated |
| `secretClass` | enum | public/workspace-sensitive/secret-blocked |
| `complete` | bool | False for truncation, skipped inputs, cancellation |
| `status` | enum | success/failure/partial/cancelled |
| `reducerID/version` | string | Reproducibility |
| `retentionClass` | enum | task/session/project/short-lived |
| `expiresAt` | timestamp? | Never silently delete referenced durable evidence |

Artifact creation happens before model-visible reduction. If storage fails, return the existing bounded raw-output behavior and record degraded mode; do not claim retrievability.

## 10. `ObservationCard`

| Field | Type | Rule |
|---|---|---|
| `artifactID` | sha256? | Nil only in degraded no-store mode |
| `tool` | string | Original name |
| `status` | enum | success/failure/partial/cancelled |
| `summary` | string | Deterministic, bounded |
| `facts` | map/list | Sorted stable keys, schema-specific |
| `excerpts` | []SourceExcerpt | Exact raw subsequences with coordinates |
| `omittedBytes/Lines/Items` | int | Truthful truncation |
| `skipped` | []SkippedInput | Truthful incompleteness |
| `fetchHint` | ArtifactRangeHint | How to request more |
| `estimatedTokens` | int | Cap enforcement |

An observation may summarize but cannot contradict raw status. Failure identifiers and source coordinates are protected fields.

## 11. `TaskCapsule`

Target cap: 400-800 tokens for tiny/small, 1,200 for standard, 2,000 per completed milestone for large/epic.

| Field | Type | Rule |
|---|---|---|
| `id` | sha256 | Canonical capsule content |
| `epochID/sessionID/workspaceID` | string | Ownership |
| `goal` | string | Bounded normalized goal |
| `outcome` | enum + string | completed/partial/failed/aborted + facts |
| `changedFiles` | []FileDeltaRef | Relative path, before/after fingerprint, line counts |
| `checks` | []CheckResultRef | Command/check identity, status, artifact handle |
| `decisions` | []DecisionFact | Durable factual decisions only |
| `unresolved` | []RiskOrBlocker | Genuine remaining work |
| `symbols/paths/terms` | []string | Retrieval index |
| `evidenceRefs` | []ArtifactRef | Bounded |
| `dependsOnCapsules` | []string | DAG edges |
| `createdAt` | timestamp | Not rendered unless needed |
| `schemaVersion` | int | Migration |
| `signature` | sha256/HMAC | Detect corruption/tampering according to state model |

Capsule construction is deterministic from the task ledger, change ledger, verification records, and selected final answer facts. A model may propose narrative text, but exact paths, hashes, statuses, and evidence links come from runtime state.

## 12. `RetrievalCandidate` and selection

| Field | Type |
|---|---|
| `ref` | capsule/artifact/range/memory/symbol reference |
| `kind` | enum |
| `estimatedTokens` | int |
| `lexicalScore` | float |
| `pathSymbolScore` | float |
| `dependencyScore` | float |
| `recencyScore` | float |
| `validityScore` | float |
| `pinnedScore` | float |
| `combinedScore` | float |
| `similarityToSelected` | float |
| `selected` | bool |
| `exclusionReason` | enum? |

Selection uses hard validity filters, then MMR/token-budget selection. Store candidate decisions in debug/benchmark mode only; ordinary sessions need only selected refs and summary counts.

## 13. `ToolDescriptor` and broker state

| Field | Type | Rule |
|---|---|---|
| `canonicalName` | string | Exact callable identity |
| `namespace` | builtin/mcp/skill/memory/integration |
| `oneLine` | string | Strict character/token cap |
| `keywords` | []string | Sorted, bounded |
| `risk` | enum | Permission path |
| `readOnly` | bool | Existing contract |
| `schemaHash` | sha256 | Exact full schema version |
| `serverFingerprint` | string? | MCP identity/account |
| `availability` | enum | ready/auth-required/offline/disabled |

`discover_tools(query, limit)` returns descriptors only. `invoke_tool(name,args)` resolves the exact schema by hash, validates args, runs normal gates, and records the original canonical tool name in all audit logs.

## 14. `ProviderCacheProfile`

| Field | Type | Examples |
|---|---|---|
| `family` | string | minimax/deepseek/generic |
| `transport` | enum | openai/anthropic |
| `mode` | enum | none/automatic/explicit |
| `prefixOrder` | []enum | tools, system, messages |
| `minCacheableTokens` | int? | Provider documented |
| `maxBreakpoints` | int? | Explicit controls only |
| `lookbackBlocks` | int? | Capability, not assumption |
| `ttls` | []duration | Verified supported values |
| `readWeight/writeWeight/missWeight/outputWeight` | decimal? | Price/credit schedule versioned |
| `usageMapping` | struct | Raw -> normalized field paths |
| `requiresFullAssistantReplay` | bool | MiniMax tool chains true |
| `toolMutationInvalidates` | bool | Usually true at earliest tier |
| `reasoningParamInvalidatesMessages` | bool? | Only if documented/verified |
| `supportsDeferredToolReference` | bool | Capability-tested |
| `version/sourceDate` | string/date | Auditability |

Unknown capability is false/unsupported, not optimistic.

## 15. `EconomyDecision`

| Field | Type |
|---|---|
| `kind` | keep_context/new_epoch/evict_observation/compact/load_tool/escalate_reasoning/stop |
| `requestSeq` | uint64 |
| `mode` | observe/enforce |
| `inputs` | bounded numeric map |
| `predictedKeepCost` | decimal? |
| `predictedChangeCost` | decimal? |
| `predictedSavings` | decimal? |
| `dependencyConfidence` | float? |
| `qualityGate` | pass/fail/unknown |
| `decision` | apply/skip |
| `reasonCode` | enum |
| `actualSavings` | decimal? after completion |

Observe-mode decisions are never injected into model context.

## 16. Persistence and migration rules

1. Add new economy fields to `UsageRecord` as nullable, backward-compatible JSON.
2. Store task epochs/capsules/artifact metadata through the canonical session journal when that migration exists; until then use atomic sidecars with explicit versioning and retryable persistence errors.
3. Write artifact blob to a temporary file, fsync where supported, atomically rename, then journal metadata. Orphan blobs are safe and garbage-collectable; metadata may never reference a missing committed blob.
4. Store relative workspace paths after real-path containment and secret checks.
5. Never garbage-collect an artifact referenced by an unsettled epoch, a retained capsule, a failure report, or an active resume point.
6. Rebuild retrieval indexes from capsules/artifact metadata; the index is a projection, never canonical truth.
7. A migration rollback disables new request assembly but preserves readable new records.

## 17. Invariants

- One visible session -> one immutable main model.
- Every provider request -> at most one provider-truth usage row; retries have distinct rows and `retryOf`.
- Every model-visible reduced observation -> either a valid retrievable artifact handle or an explicit degraded marker.
- Every context reset -> durable capsule/checkpoint persisted first.
- Every cache-affecting change -> explicit `EconomyDecision`/invalidation reason and prefix revision.
- Exact provider totals are never overwritten by estimates.
- Selected context never includes secret-blocked artifacts.
- Tool broker invocation never bypasses original schema, permission, or audit identity.
- Budget stopping never overrides mandatory safety/correctness actions without a recorded valid override.
