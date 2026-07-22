# Tool broker offline parity report

Date: 2026-07-22

No provider or paid requests were used.

Verified offline:

- balanced fixed prefix is <=10,000 bytes and <=2,500 estimated tokens;
- lean universal prompt is <=3,000 characters;
- 100 unused MCP schemas do not enter or perturb the core definition bytes;
- discovery order, descriptor caps, schema hashes, recursion rejection, stale
  schema rejection, and original call/result identity are deterministic;
- MCP descriptors retain the pinned server/account fingerprint and invocation
  rejects a missing or stale fingerprint;
- brokered file reads retain workspace containment and secret-file denial;
- brokered edits retain approval denial;
- brokered shell calls retain destructive-command denial;
- `inspect_workspace` map/search/read-many modes return bounded structured
  results with source coordinates and skipped-input metadata;
- symbol mode reports explicit unavailability until the repository index
  exists;
- section-safe skill reads are bounded to an exact heading, while unmarked
  skills fall back to the full mandatory body;
- skill bodies are loaded once per session;
- instruction and wire-prefix goldens were updated once for the deliberate
  balanced epoch; legacy/off goldens remain intact.

Commands:

`go test ./internal/orchestrator ./internal/mcpclient ./internal/workspace ./internal/instructions ./internal/contract ./internal/arch -count=1`

Live common-task and integration comparisons remain deferred under the user's
no-paid-tests instruction.
