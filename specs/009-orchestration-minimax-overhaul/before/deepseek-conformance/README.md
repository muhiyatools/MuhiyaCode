# DeepSeek-only BEFORE conformance snapshot

Captured 2026-07-14 from gateway revision
`0b4ab0407f8b01a0efe4e7b086d21b0da31e4e33`, before any feature-009 gateway
production change.

The capture executes the existing gateway body-conditioning path in its production
order: replace the client model with the resolved target, strip `web_search`, apply
DeepSeek thinking normalization, add `stream_options.include_usage`, sanitize
identity and unsupported sampling fields, then serialize with `encoding/json`.
The canonical body includes user, assistant text, assistant tool-call, and tool
result messages plus the OpenAI function-tool schema. Secrets are never recorded.

## Fixtures

| File | Scenario | SHA-256 |
|---|---|---|
| `reasoner-max.json` | reasoning model, max effort, server-derived identity | `3755f23ef7301dfd906e5c249103e115e1b9a72accb809d55ff592adb71fd708` |
| `chat-absent.json` | non-reasoning model, no effort, server-derived identity | `182724327f4ad0508472cd3147bb36a958223ae438e0465b48077c13768ccf47` |
| `reasoner-no-identity.json` | reasoning model, medium effort, identity injection disabled | `5f8ff7d8d1af6c44f574b45d0d808f48fe18c7588e8e658a897a57a0b025492b` |

Each `.meta.json` sidecar freezes the method, path, content type, redacted auth
shape, and resolved thinking result. The pre-feature regression matrices also pass:

- Gateway: `TestDeepSeekThinkingNormalizationAndStripList` and
  `TestSanitizeUpstreamIdentityNoSecretDropsIdentity`.
- Client: `TestWireMessageShapesFrozen` and
  `TestMarshalDeterminismForIdenticalLogicalRequest`.

T049 must regenerate the same scenarios from the final gateway build and compare
the JSON bytes and metadata byte-for-byte. Any difference is an FR-018 regression.

