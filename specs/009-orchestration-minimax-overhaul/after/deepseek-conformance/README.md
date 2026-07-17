# DeepSeek-only AFTER conformance snapshot

Regenerated 2026-07-14 from the finished feature-009 gateway working tree with
`TestFeature009DeepSeekCaptureMatchesPinnedBefore`. The test ran the same body
conditioning order as the BEFORE capture and compared every canonical JSON and
metadata byte against `before/deepseek-conformance/`.

| File | AFTER SHA-256 | Byte-identical to BEFORE |
|---|---|---|
| `reasoner-max.json` | `3755f23ef7301dfd906e5c249103e115e1b9a72accb809d55ff592adb71fd708` | PASS |
| `chat-absent.json` | `182724327f4ad0508472cd3147bb36a958223ae438e0465b48077c13768ccf47` | PASS |
| `reasoner-no-identity.json` | `5f8ff7d8d1af6c44f574b45d0d808f48fe18c7588e8e658a897a57a0b025492b` | PASS |

The finished gateway's DeepSeek thinking/strip tests and the client's frozen
DeepSeek replay/wire-shape tests also pass. No provider call was made.
