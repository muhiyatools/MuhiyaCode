# Contract: Client Capability Profile (MuhiyaCode)

Feature 007. The client-side single source of truth for what the provider (via the
gateway) supports. Extends the existing per-family `ModelProfile`
(internal/gateway/model.go). Values in [data-model.md](../data-model.md) §2.

## 1. Shape & lifecycle

- One profile per model family, compile-time constant, resolved once at boot from the
  configured model ID (existing `profileFor` path). **No network fetch, no mid-session
  mutation** — the emitted wire shape for a session is frozen (Reasonix X5 pattern).
- The request builder (`chatOnce`) MUST consult the profile for: parameter emission
  (only `SupportedParams`), `max_tokens` ≤ `MaxOutputTokens`, and deprecated-param
  suppression. It already consults temperature/top_p/max_tokens; this contract extends
  the same mechanism, not a new layer.

## 2. Behavioral requirements

| ID | Requirement |
|---|---|
| CP-1 | A request MUST NOT contain any parameter absent from `SupportedParams` for the active family (FR-013) |
| CP-2 | `DeprecatedParams` MUST never be emitted regardless of configuration (FR-002 client half) |
| CP-3 | Configured/derived `max_tokens` MUST be validated ≤ `MaxOutputTokens` at request build; violation clamps + logs (FR-004) |
| CP-4 | The history budget ceiling MUST NOT exceed `ContextWindowTokens`; the operational budget (`contextLimit`, default 128k) stays user-configurable below it (R3b) |
| CP-5 | Any future `response_format: json_object` use MUST satisfy `JSONModeRules` (keyword + example + output headroom) or be refused at build time (FR-014) |
| CP-6 | `BetaFeatures` entries carry `adopted | not_adopted | not_adopted_reevaluate` + a rationale string; the builder MUST refuse a request needing a `not_adopted*` feature (FR-015, R9) |
| CP-7 | Families without DeepSeek-specific data degrade gracefully: absent capability data ⇒ current permissive behavior (Principle IX; no hard vendor dependence) |
| CP-8 | Profile content changes are code changes reviewed against the provider-docs snapshot in the audit report — never runtime data |

## 3. Surfacing

`/context` (or doctor diagnostics) MAY render the active profile (family, limits,
beta statuses) for user visibility; rendering is read-only.

## 4. Conformance assertions

1. Unit: builder given a config attempting each deprecated/unsupported param ⇒ request
   bytes contain none of them (table-driven over the profile).
2. Unit: `max_tokens` above limit ⇒ clamped + warning event.
3. Marshal-determinism: profile-driven request shape byte-stable across two identical
   builds (extends existing wire-shape freeze test from feature 002).
