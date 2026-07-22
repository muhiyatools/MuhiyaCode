# Evidence reducer rollout

Date: 2026-07-22
Reducer version: `014-reducer-v1`

The rollout is mode-gated and preserves the complete formatted tool result in a content-addressed session artifact before any model-visible reduction.

1. `off`: no artifact write and no output rewrite.
2. `observe`: artifact write and hash/ownership validation; original output remains model-visible.
3. `balanced`/`aggressive`, successful result: reduce only above 6,000 bytes.
4. `balanced`/`aggressive`, failed result: preserve up to the larger failure band and reduce only above 12,000 bytes; prioritized failure lines remain in the card.
5. File/search/diff/JSON/MCP results use deterministic source-aware reducers; unsupported formats fall back to bounded exact excerpts.

Completeness comparison is enforced locally: the raw bytes round-trip by content hash, every card reports `status` and `complete`, omitted/skipped counts remain explicit, invalid JSON is identified rather than coerced, and a failed store leaves the original full result in context instead of returning a lossy card.

Rollback is immediate through `tokenEconomyMode=off`; artifacts remain readable and are safe to garbage-collect through reference-aware mark/sweep.
