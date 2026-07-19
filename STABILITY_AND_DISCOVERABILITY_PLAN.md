# Stability, Caching, Discoverability & Onboarding Plan

**Date**: 2026-07-19 · **Author**: Claude (Fable 5) — plan only, no code was modified
**Repos**:
- Gateway: `F:\MuhiyaWorkspace\MuhiyaWorkspace` (Go 1.22, stdlib net/http, PostgreSQL via lib/pq, optional Redis; branch `main`, clean tree)
- Agent: `F:\MuhiyaCode Agent Go` (Go, Bubble Tea v2; branch `main`; three UNCOMMITTED files: `internal/workspace/files.go`, `shell.go`, `workspace_test.go` — the git-diff argv hardening. **Commit these first, separately.** They are unrelated to this plan.)

**Requirements covered**:
1. Gateway: no errors, crashes, or sign-in failures; 100 % stable (Workstream A)
2. Caching works perfectly with DeepSeek and OpenRouter (Workstream B)
3. New per-model "MuhiyaCode Discoverable" toggle in the gateway admin panel; MuhiyaCode only discovers marked models (Workstream C)
4. Gateway at its best in speed/stability + properly integrated with the MuhiyaCode agent (A, B, E)
5. Agent: first-run onboarding modal with one option — "Log in with Muhiya Account" (Workstream D)

Every task has file:line anchors verified against the code on 2026-07-19, an exact change, and acceptance criteria (AC). Execute in the order of §6.

---

## §0 Locked decisions (read before coding)

| # | Decision | Rationale |
|---|---|---|
| D-1 | New column is **`muhiyacode_visible BOOLEAN NOT NULL DEFAULT FALSE`** on `models`. Migration **backfills `TRUE` for every existing non-transcribe LLM row** (any status). | `DEFAULT FALSE` gives the requested opt-in semantics for all *future* models ("only models we explicitly mark"); the backfill makes the deploy a zero-behavior-change event. The owner then unchecks chat-only models (e.g. Gemini 2.5 Flash Lite) in the panel. Backfilling regardless of status means re-activating an old model restores its old visibility — least surprise. |
| D-2 | The flag gates **discovery only** (`GET /v1/models`, `/v1/models/{id}`) and **only when `getClientAppName(r) == "MuhiyaCode"`**. It does **not** gate inference (`GetModelByName`), the `muhiya-ai-router`, or any other client app. | MuhiyaChat/platform must keep seeing everything; the vision router must stay free to route image parts to Gemini Flash Lite; a MuhiyaCode power user who sets a hidden model by exact name may still use it. `X-Client-App` is client-asserted, so this is a UX filter, not a security boundary (documented in §8). |
| D-3 | Onboarding modal has **exactly one choice** — "Log in with Muhiya Account" — per the requirement. **Esc dismisses it** (standard modal behavior) so `/login <key>` remains an escape hatch; the existing `configurationNotice` warn-flash stays as the residual guidance after dismissal. | One-option requirement + never trap the user. |
| D-4 | The onboarding modal is TUI-only. One-shot `-p` mode keeps its current hard error (`root.go:124-126`); the simple-TUI fallback (`MUHIYA_SIMPLE_TUI`) keeps the plain notice. | Modal machinery only exists in the full TUI. |
| D-5 | No agent-side re-filtering of discovery results is added (the gateway is the source of truth). But `C7` adds **stale-model pruning on refresh** so a model hidden later doesn't linger in `settings.json` forever. | Single source of truth; fixes a real UX wart the flag would otherwise expose. |
| D-6 | OpenRouter `cache_control` injection (B4) is implemented **only if** a models-table inventory shows Anthropic-family targets routed via OpenRouter; otherwise it ships as a guarded no-op with a test. | Don't mutate bodies for a case that can't occur; keep the lever ready. |

---

## §1 Workstream A — Gateway stability & sign-in hardening

### A1. Audit-delta pass over `GATEWAY_AUDIT.md`
The 2026-07-11 audit (`GATEWAY_AUDIT.md`, findings G1…) predates major fixes. Re-verify each G-finding against today's code and write the verdict table into a new `GATEWAY_AUDIT_DELTA.md` (fixed / still open / won't-fix + reason). Already-confirmed-fixed during planning (cite these, don't re-fix):
- G1 router stickiness → `proxy/stickysession.go` (X-Muhiya-Session, 24 h TTL, evict-on-failure).
- G2/G3 lossy typed decode → OpenAI↔OpenAI path now round-trips the raw body through `map[string]interface{}` preserving unknown fields (`proxy/handler.go:988-1018`; comment at `proxy/translator.go:19-24`).
- `stream_options.include_usage` forced on streams (`handler.go:1009-1014`).
- Stream-stall watchdog (`handler.go:49-63`, `armIdleWatchdog`, 120 s idle), upstream client timeouts bounded (`handler.go:21-42`), billing outbox (`proxy/outbox.go`), Redis Lua atomic limiter (`limiter.go:274-316`), merge-patch model PUT (`admin/handlers.go:742-794`), panic recovery middleware (`main.go:364`).
**AC**: delta doc exists; every still-open G-finding either has a task in this plan or an explicit won't-fix rationale.

### A2. Concrete defects found in this planning pass (fix all)

**A2a — `/v1/models/{id}` leaks inactive models.** `proxy/handler.go:1613` matches `m.Name == modelID && !m.Transcribe` but never checks `m.Status`. The list branches check `active` (`:1689`, `:1722`); the detail branch must too.
Fix: `if m.Name == modelID && m.Status == "active" && !m.Transcribe`.
**AC**: new proxy test — detail request for an `inactive` model returns 404 in both dialects.

**A2b — nil-map panic risk in `proxyOpenAIToOpenAI`.** `handler.go:989-991`: `_ = json.Unmarshal(origBody, &bodyMap)` then `bodyMap["model"] = …`. If `origBody` is valid-enough to pass earlier typed decode but unmarshals to a non-object (or the error path changes later), assignment to a nil map panics (recovery middleware turns it into an opaque 500).
Fix: check the error and `bodyMap == nil` → return a 400 `invalid_request_error`. Sweep for the same pattern on the Anthropic-bound paths near `handler.go:1184` and `:1448` (where `InjectAnthropicCacheControl(bodyMap, …)` is called) and apply the same guard.
**AC**: unit test feeding a JSON array body to each path gets 400, not a panic/500.

**A2c — sticky map can exceed its cap with live entries.** `stickysession.go:106-113`: overflow sweep deletes only *expired* entries; >10 000 concurrent live sessions grow the map unbounded (24 h TTL).
Fix: after the expired sweep, if still over `stickyMapCap`, evict arbitrary entries down to the cap (map iteration order is fine; a miss only costs one cache-cold turn per the file's own comment).
**AC**: unit test inserting `stickyMapCap+N` unexpired entries observes `len(entries) <= stickyMapCap` after the next `set`.

**A2d — unbounded `textAccumulator` on streams.** `handler.go:1056`: the full response text accumulates per request for token estimation.
Fix: stop appending past a cap (e.g. 2 MiB) and mark the estimate as clamped (usage from the final chunk is the primary source anyway; the accumulator is the fallback estimator).
**AC**: test streaming > cap asserts memory-bounded accumulator and unchanged billing when upstream usage is present.

**A2e — DB-down auth returns 500.** `authenticateVirtualKey` → `GetVirtualKey` error → `internalErrorResponse` 500 (`handler.go:411`). During a Postgres blip every request looks like a server bug and clients don't retry.
Fix: map DB errors during auth to **503 + `Retry-After: 2`** with a clear `"gateway database unavailable"` message (consistent with the `dbReady` 503 gate at `main.go`). Leave true auth rejections as 401.
**AC**: test with a closed DB pool asserts 503, not 500; agent-side friendly error verified in E1.

**A2f — housekeeping (small, do together)**: (i) singleton goroutines (`watchDatabase` `main.go:388`, `runRetentionSweeper` `main.go:416`, `logOutbox.run` `outbox.go:32`, `sweepInMemoryLimiters` `limiter.go:163`) don't stop on shutdown — wire them to a context cancelled by the shutdown path (`main.go:345-355`); cosmetic-only if skipped, but cheap. (ii) Add a `[BILLING-LOSS]` counter to `/health` output so outbox drops (`outbox.go:47,57`) are observable. (iii) Leave ignored `json.Encoder.Encode` returns as-is (cosmetic).
**AC**: `go vet ./...` clean; shutdown under load exits within the 60 s drain.

### A3. Static & race sweep
Run and fix to zero: `go vet ./...`, `go test ./... -race -count=1` (pure tests run DB-less; DB tests need `TEST_DATABASE_URL` — provision a scratch Postgres, e.g. `docker run -e POSTGRES_PASSWORD=test -p 5433:5432 postgres:16`), plus `staticcheck ./...` if available.
**AC**: all three clean on the gateway; same (`make verify` + `make test-race`) on the agent.

### A4. Sign-in chain verification (gateway side)
The full chain: platform `/authorize` (browser) → platform `/api/oauth/token` (mints `sk-virt` via gateway admin/service API `admin/handlers.go:377` → `db.CreateVirtualKey` `db/db.go:822`) → agent stores key → every request `authenticateVirtualKey` (`handler.go:395-424`, SHA-256 lookup `db/db.go:753-755`).
Verify, with a test or documented manual check each:
1. `backfillVirtualKeyHashes` (`db/db.go:260`) has run in prod (pre-migration-006 keys authenticate). Query: `SELECT count(*) FROM virtual_keys WHERE key_hash IS NULL OR key_hash = ''` → must be 0.
2. Expired / `status != 'active'` keys → 401 with a distinguishable message (`handler.go:414,419`) so the agent can say "key expired — log in again" rather than a generic failure.
3. Service-credential allowlist (`main.go:529-537`) still covers exactly what the platform's token-mint flow calls — no more, no less.
4. CSRF guard (`main.go:623`) does not block the platform's server-to-server calls (they send no Origin) — regression test.
5. `/v1/models` requires a valid key (`handler.go:293-294`) — this is why onboarding (D) must complete before discovery; confirm the agent's pre-login state never spams 401s in a loop (it doesn't today: discovery only triggers on key-present paths, `runtime_build.go:108-123, 669-684`).
**AC**: checklist committed to `GATEWAY_AUDIT_DELTA.md` with evidence per item.

### A5. Speed pass (verify, then micro-fix only)
1. Measure per-request gateway overhead (auth + limiter + logging) at p50/p95 with a local echo upstream; target < 15 ms p50 DB-warm.
2. `limiter.CheckLimit` DB round-trips (`limiter.go:190`): confirm the defs cache (`limiterDefsCache`, `limiter.go:49`) actually bounds steady-state to ≤ 2 queries; collapse if more.
3. Confirm pool sizing (`db/db.go:228-231`, 25/25/5 m) vs expected concurrency; bump only with measurements.
4. Confirm keep-alives to upstreams are reused (shared `httpClient`, `handler.go:21-42`) — no per-request client construction anywhere (grep `http.Client{`).
**AC**: numbers recorded in the delta doc; any change justified by a before/after measurement.

---

## §2 Workstream B — Caching correctness: DeepSeek & OpenRouter

Background (verified): the gateway has **no response cache**. DeepSeek/OpenRouter cache automatically upstream; the gateway's levers are (1) byte-stable request transformation, (2) model stickiness per session, (3) honest parsing/billing of cache usage. Anthropic-protocol upstreams additionally get `cache_control` injection (`proxy/cache.go:63`, called at `handler.go:1184` and `:1448` only).

### B1. DeepSeek prefix-stability invariants (pin with tests)
The OpenAI passthrough mutates only: `model` swap, `web_search` delete, media strip, thinking translation, `stream_options`, identity sanitization (`handler.go:988-1018`) — then `json.Marshal` of a map (alphabetical keys, deterministic). Pin these invariants:
1. **Byte determinism**: same input body + same session → identical upstream bytes across two calls (extend `feature009_deepseek_capture_test.go` with a two-turn capture asserting turn-2 body is a strict prefix-superset of turn-1's messages, unchanged earlier bytes).
2. **Thinking determinism**: `ApplyThinkingOpenAI` (`thinking.go`) output depends only on (provider, target, level) — a mid-session `X-Muhiya-Effort` change is the *client's* choice; the gateway must not spontaneously vary it. Assert no other field is reordered/added between turns.
3. `stream_options.include_usage` always present on streams (`handler.go:1011`) — existing behavior, add an assertion to the capture test.
4. Cache token parsing: DeepSeek `prompt_cache_hit_tokens`/`prompt_cache_miss_tokens` (`translator.go:89-90`, `CacheReadTokens` `:109`, `CacheMissTokensFor` `:145`) — covered by `cache_test.go:24-28`; extend with a stream-final-chunk fixture.
**AC**: new/extended tests in `proxy/` pass; a deliberate byte-instability (e.g. injected map key) fails the capture test.

### B2. Sticky-session behavior tests (gateway)
`stickysession.go` + router: add tests asserting (1) same `X-Muhiya-Session`+key → same model across requests even when the complexity heuristic would choose differently; (2) eviction on upstream failure (`evict`, `:76`) lets the next turn re-route; (3) the `[ROUTER-STICKY-SWAP]` log fires on a genuine repin (`:98-104`). Agent side already pins: `X-Muhiya-Session` derives from `SessionID` (`internal/gateway/provider.go:167-170`; test `internal/gateway/gateway_test.go:152-167`) — no agent change.
**AC**: tests pass; a two-turn live check (B7) shows turn-2 `prompt_cache_hit_tokens > 0` on DeepSeek.

### B3. OpenRouter usage & billing correctness
1. Parse coverage: OR reports `prompt_tokens_details.cached_tokens` + `cache_write_tokens` (`translator.go:74-79`, `CacheWriteTokensReported` `:97`). Add a fixture test with an OR-shaped final stream chunk carrying both, asserting `log.CacheReadTokens`/`CacheWriteTokens` and `calculateCost` (`handler.go:1885-1908`) including the write-rate fallback (write rate = input rate when the model row has 0, `:1889-1891`).
2. Provider pinning: `setOpenRouterHeaders` (`handler.go:977-998`) sends attribution + `X-Session-Id` from the Muhiya session so OR pins the same underlying provider (prefix caches are per-provider). Add a header-assertion test.
3. Pricing rows audit (data, not code): for every `provider_id='openrouter'` model row, verify `cache_read_cost_per_million`/`cache_write_cost_per_million` match the current OR pricing of the underlying provider; same for DeepSeek rows (hit ≈ $0.014/M class). Record corrections applied via the admin panel in the delta doc.
**AC**: tests pass; a spot-check request via OR bills within $0.0001 of hand-computed cost.

### B4. OpenRouter `cache_control` for Anthropic targets (conditional — see D-6)
Inventory first: `SELECT name, target_model FROM models WHERE provider_id='openrouter' AND (target_model ILIKE '%claude%' OR target_model ILIKE 'anthropic/%');`
- If rows exist (or the owner plans them): in `proxyOpenAIToOpenAI` after `ApplyThinkingOpenAI` (`handler.go:1006`), when `provider.ID == "openrouter"` and the target is Anthropic-family, inject OpenAI-shape `cache_control` breakpoints (system message content-part + last message content-part — OR accepts Anthropic-style `cache_control` inside OpenAI content parts). Reuse the semantics of `proxy/cache.go`: skip if `containsCacheControl(bodyMap)` (`cache.go:39`), never touch unknown shapes, injection must be **deterministic** so B1's byte-stability holds.
- If no rows and none planned: implement the guard + a no-op test only.
**AC**: unit tests for inject/skip; live two-turn OR-Claude check shows `cache_write_tokens` turn 1, `cached_tokens` turn 2 (only if such a model exists).

### B5. Anthropic-path injection edge (small)
`cache.go:88,109`: when the last system/content block rejects `cache_control` (e.g. thinking block), nothing is cached for that message. Improve: walk backwards to the last block that `blockAcceptsCacheControl` instead of only inspecting the final block.
**AC**: unit test — a message ending in a thinking block still gets a breakpoint on the preceding text block.

### B6. Client-side cache accounting parity (agent, verify only)
Agent already parses both dialects (`internal/gateway/sse.go:218-321`) with contradiction guards, and `muhiya_log` cost (`provider.go:276-295`; gateway emits it only to `MuhiyaChat`/`MuhiyaCode`, `handler.go:2172-2178`). Re-run `internal/gateway` tests; no changes planned. Known context: live sessions plateau ~96.5 % hit rate with first-turn misses tracked separately in `ULTIMATE_POLISH_PLAN.md` (skills drift / turn-1 guard) — **out of scope here; do not duplicate**.
**AC**: `go test ./internal/gateway/...` green; cross-reference note added to the delta doc.

### B7. Live two-turn cache gauntlet (extends `LIVE_GAUNTLET.md`)
Script (in agent repo `benchmarks/` or a `scripts/cache_gauntlet.*`): for each of {DeepSeek direct, DeepSeek-family via OR if present, any OR model}: send turn 1, then turn 2 with the same `X-Muhiya-Session` and superset messages; assert (1) turn-2 cache-read tokens > 50 % of prompt, (2) `muhiya_log.cost` matches `calculateCost` recomputation, (3) admin `request_logs` row records the same numbers (`db/db.go:1104-1108`).
**AC**: gauntlet passes against the live gateway; results table pasted into `LIVE_GAUNTLET.md`.

---

## §3 Workstream C — "MuhiyaCode Discoverable" model flag

The threading pattern mirrors `supports_vision` (migration 016) exactly. Client identity comes from `getClientAppName(r)` (`proxy/handler.go:153-158`) — the agent sends `X-Client-App: MuhiyaCode` on every call (`internal/gateway/provider.go:161,441`, `usage.go:65`, `web.go:151`), and the exact string `"MuhiyaCode"` is already the contract (`metaChunkClientApps`, `handler.go:2172-2175`).

### C1. Migration `db/migrations/021_muhiyacode_visible.sql`
```sql
-- MuhiyaCode discoverability is opt-in: new models are hidden from the
-- MuhiyaCode app's /v1/models listing until an operator marks them.
ALTER TABLE models ADD COLUMN IF NOT EXISTS muhiyacode_visible BOOLEAN NOT NULL DEFAULT FALSE;
-- Behavior-preserving backfill: everything MuhiyaCode could discover before
-- this migration stays discoverable (any status; transcribe/non-llm rows were
-- never listed anyway).
UPDATE models SET muhiyacode_visible = TRUE
 WHERE COALESCE(transcribe, FALSE) = FALSE AND COALESCE(model_type, 'llm') = 'llm';
```
Also update the migration-name list test (`db/migrations_test.go:36` area) with `021_muhiyacode_visible.sql`. Migrations auto-apply at boot under `pg_advisory_lock` (`db/migrations.go:33-46`) — additive column ⇒ zero-downtime.
**AC**: fresh DB and upgraded DB both end with the column; migrations test green.

### C2. DB layer threading (`db/db.go`)
1. Struct field after `AcceptedMimeTypes` (`db.go:130`):
```go
// MuhiyaCodeVisible marks a model as discoverable by the MuhiyaCode app
// (X-Client-App: MuhiyaCode) on /v1/models. Discovery-only: inference by
// exact name and router selection are NOT gated. Opt-in for new rows.
MuhiyaCodeVisible bool `json:"muhiyacode_visible"`
```
2. Add `COALESCE(muhiyacode_visible, FALSE)` + `&m.MuhiyaCodeVisible` to all three SELECT/Scan sites: `GetModelByName` (`db.go:914-928`), `GetModel` (`:940-951`), `ListModels` (`:962-981`) — keep column and scan positions aligned.
3. `CreateModel` (`:997-1008`): add column, `$25` → `$26`. `UpdateModel` (`:1019-1030`): add `muhiyacode_visible = $25`, shift `WHERE id` to `$26`.
**AC**: `go build ./...`; a create→get round-trip test preserves the flag both ways.

### C3. Enforcement in `/v1/models` (`proxy/handler.go:1594-1766`)
At the top of `handleModelDiscovery`, compute once:
```go
onlyMuhiyaCodeVisible := getClientAppName(r) == "MuhiyaCode"
```
Apply in **all three** branches:
- detail branch (`:1613`): `m.Name == modelID && m.Status == "active" && !m.Transcribe && (!onlyMuhiyaCodeVisible || m.MuhiyaCodeVisible)` (includes the A2a status fix; hidden ⇒ 404 for MuhiyaCode only);
- Anthropic list (`:1689`) and OpenAI list (`:1722`): add `&& (!onlyMuhiyaCodeVisible || m.MuhiyaCodeVisible)`.
Do **not** add the flag to the response payload (entries are hand-built maps; operator metadata stays internal). Do **not** touch `GetModelByName`, `router.go`, or `agent.go` (decision D-2).
**AC**: table-driven proxy test: catalog of {visible, hidden, inactive, transcribe} × client app ∈ {`MuhiyaCode`, `MuhiyaChat`, none/curl} × both dialects; MuhiyaCode sees only visible+active+non-transcribe; everyone else is unchanged; hidden model detail → 404 for MuhiyaCode, 200 for MuhiyaChat.

### C4. Router pseudo-model sanity
Verify how `muhiya-ai-router` reaches MuhiyaCode's picker: if it is a `models` row, the C1 backfill already marks it visible — assert it stays listed; if it is synthetic (never in `/v1/models`), note that and confirm the agent's auto-mode is unaffected. Check `router.go` + `seed.sql`/`add_base_models.sql`.
**AC**: one-line finding in the delta doc + (if a row) a test that it remains discoverable.

### C5. Admin panel UI (`static/`)
Mirror the `supports_vision` checkbox triplet:
1. `static/index.html` (model modal, checkbox rows at `:656-677`): add
```html
<div class="form-group" style="display:flex; align-items:center; gap:.5rem; margin-bottom:1rem;">
    <input type="checkbox" id="model-muhiyacode-visible" style="width:auto; margin-bottom:0;">
    <label for="model-muhiyacode-visible">MuhiyaCode Discoverable — listed in the MuhiyaCode app's model picker (/v1/models). Unchecked models stay fully usable by MuhiyaChat and by exact name.</label>
</div>
```
New-model form default: **unchecked** (opt-in, D-1). Ensure the "add model" reset path clears it.
2. `static/app.js`: submit handler (`:410-459`) — read `document.getElementById('model-muhiyacode-visible').checked` and include `muhiyacode_visible` in the POST/PUT body (`:452` area); `editModel` (`:1178-1209`) — `…checked = !!m.muhiyacode_visible;`; models table (`:1093-1142`) — add a `MuhiyaCode` badge next to the capability badges (`:1109-1117`) so discoverability is visible at a glance.
Note: PUT is merge-patch server-side (`admin/handlers.go:742-794`), so a stale cached `app.js` that omits the field cannot clear it — no compat shim needed.
**AC**: manual panel pass — create (unchecked default), edit toggle on/off persists, badge renders; `admin/handlers_test.go` gains a create+merge-patch case asserting an omitted field is preserved and a sent field is applied.

### C6. Seed/ops follow-up (owner action, document in the plan's rollout notes)
After deploy: uncheck "MuhiyaCode Discoverable" on chat-only rows (Gemini 2.5 Flash Lite, and any other MuhiyaChat-only media models); leave the DeepSeek/MiniMax/router rows checked. Record the final matrix in `GATEWAY_AUDIT_DELTA.md`.
**AC**: `curl -H "X-Client-App: MuhiyaCode" -H "Authorization: Bearer sk-virt-…" …/v1/models` no longer lists Gemini 2.5 Flash Lite; same call with `X-Client-App: MuhiyaChat` still does.

### C7. Agent: prune undiscoverable models on refresh (UX completion of the flag)
Today `addDiscoveredModels` (`internal/command/actions.go:186-197`) only upserts — a model the gateway stops listing lingers in `settings.json` forever. Change `Actions.DiscoverModels` (`actions.go:102-115`) and `config discover` (`root.go:252-273`) to: after a **successful** fetch, remove models with `Source == "endpoint"` that are absent from the fresh list, **except** any currently assigned as main/subagent (`ActiveModelID`/`SubagentModelID`) — for those, keep them and surface a one-line notice ("model X is no longer offered by the gateway; pick a new model with /model"). Never prune on fetch error. Manually-added models (`Source != "endpoint"`) are never pruned.
**AC**: unit test on the prune helper (present→absent transitions, active-model exemption, error-path no-op); `/model` refresh in the TUI reflects a gateway-side uncheck within one refresh.

---

## §4 Workstream D — First-run "Log in with Muhiya Account" onboarding modal (agent)

Current behavior (verified): with no key, the TUI launches fully; the only cue is a warn-flash of `configurationNotice` (`internal/command/root.go:541-556`) surfaced in `applyHydration` (`internal/tui/update.go:66-68`) plus the decorative `firstRunHint` (`internal/tui/render_transcript.go:100-107`). No blocking prompt. All wiring for the fix already exists: `Actions.IsLoggedIn` (`internal/command/actions.go:97-101`), `Actions.LoginViaBrowser` (`:81-92` → `performBrowserLogin` + `setAPIKey` which persists, updates the live provider, and auto-discovers models when none exist, `runtime_build.go:656-684`), modal component (`internal/tui/modals.go`), success toast (`handleAction case "login"`, `modals.go:49-54`).

### D1. Trigger: open the modal at hydration when signed out
In `applyHydration` (`internal/tui/update.go:48-85`), after actions are adopted and the notice warn (keep the warn — it's the post-Esc residual guidance), add:
```go
if !m.simpleMode() && msg.err == nil &&
    m.actions.IsLoggedIn != nil && !m.actions.IsLoggedIn() {
    m.openOnboarding() // D2; defers itself if a reply modal is showing (D3)
}
```
(`m.simpleMode()` = whatever guard matches how `Options.Simple`/`MUHIYA_SIMPLE_TUI` (`run.go:23`) is represented on `Model`; in simple mode the notice alone remains.) Do not gate on `configurationNotice` text — key presence is the condition; a signed-in user with no model configured gets the existing notice, not the modal.
**AC**: TUI test — hydration with empty `ProviderAPIKey` opens a modal titled "Welcome to MuhiyaCode"; with a key present, no modal.

### D2. The modal itself
New `openOnboarding()` in `internal/tui/modals.go`, built on `openChoice` (`modals.go:162`):
- Title: `Welcome to MuhiyaCode`. Message: short — "Sign in to connect this machine to your Muhiya account. Your browser will open to approve the sign-in." (+ hint line that Esc dismisses and `/login` reopens).
- Choices: exactly one — `{Label: "Log in with Muhiya Account", Description: "Opens your browser (muhiya.com) — approve, then return here", Recommended: true}` (D-3).
- `onSelect`: identical to the bare `/login` branch (`internal/tui/slash.go:121-123`): `m.notify("Opening your browser to sign in… approve the request, then return here.")` then `return actionCommand("login", func() (any, error) { return m.actions.LoginViaBrowser(m.ctx) })`. Success lands in the existing `case "login"` toast; failure lands in `handleAction`'s error path (`modals.go:20-25`) — extend that warn for `kind == "login"` to append "— you can also paste a key with /login <key>".
- Esc: standard choice-modal Esc closes it (`handleModalKey`, `modals.go:270-294`); nothing else needed.
**AC**: selecting the option fires an `actionMsg{kind:"login"}` (assert via test with a stub `LoginViaBrowser`); Esc closes and the app remains usable; after a successful stubbed login the toast shows and `IsLoggedIn` flips.

### D3. Interplay with bridge reply-modals (trust prompt) and the modal queue
`openChoice` refuses to open over a reply modal (`replyModalOpen`, `modals.go:158-160`) — at startup the workspace-trust prompt (`enqueueModal`, `modals.go:141-151`) may already be showing, and the refusal would silently drop onboarding. Implement deferral: if `m.replyModalOpen()` (or `m.modal != nil` generally), set `m.onboardingPending = true` instead of opening; in `closeModal` (`modals.go:297`), after the queue drains (`m.modal == nil` and `len(m.modalQueue) == 0`), if `onboardingPending` && still `!IsLoggedIn()` → open it and clear the flag. Also clear the flag on successful login from any path (`case "login"` in `handleAction`).
**AC**: test — enqueue a reply modal, hydrate signed-out, answer the trust prompt ⇒ onboarding opens next; sign in via `/login <key>` while pending ⇒ it never opens.

### D4. Pending prompt typed during loading must not fire signed-out
`applyHydration` currently submits `m.pendingSubmit` (`update.go:74-83`); signed-out that submit just errors. When the onboarding modal opens (or is pending), restore `pendingSubmit` into the composer (`m.input`) instead of submitting, so the user's text survives login.
**AC**: test — type during loading while signed out ⇒ no submit, text present in input, modal shown.

### D5. Browser-didn't-open fallback (real onboarding failure mode)
`Actions.LoginViaBrowser` calls `performBrowserLogin(ctx, "", nil)` (`actions.go:82`) — the `onURL` callback is nil, and `OpenBrowser`'s error is discarded (`login.go:178`), so a headless/SSH user sees nothing for 5 minutes then a timeout. Thread a URL surface: pass an `onURL` from the TUI action that emits the authorize URL to the user (via the bridge notice/warn channel the TUI already renders), e.g. "If the browser did not open, visit: <url>". Verify the CLI `muhiyacode login` path already prints the URL (`runLogin`, `login.go:69`) — if not, same fix there.
**AC**: with `mcpclient.OpenBrowser` stubbed to fail, the URL appears in the TUI transcript/notice; login still completes when the callback hits.

### D6. Logout re-entry (nice-to-have, small)
In `handleAction case "logout"` (`modals.go:55-56`), after the toast, reopen onboarding (same `openOnboarding()`, same deferral rules). Next cold start signed-out shows it anyway via D1.
**AC**: `/logout` immediately presents the modal again.

### D7. Test/regression sweep for D
Keep green (they encode adjacent behavior): `internal/command/firstrun_test.go` (`configurationNotice` strings — unchanged), `internal/tui/application_state_test.go:26` (`TestFirstRunState`), `internal/tui/robustness_test.go:104` (first-run cue), `internal/tui/command_visibility_test.go` (`/login` hidden when signed in), `internal/tui/modal_reentrancy_test.go`, `internal/tui/wiring_inventory_test.go`. Add the new tests from D1–D6 beside them.
**AC**: `make verify` green; manual first-run walkthrough in E1 passes.

---

## §5 Workstream E — End-to-end integration, perf & release

### E1. Fresh-install E2E matrix (manual, scripted where possible)
On a machine/profile with no `~/.muhiya`: launch `muhiyacode` ⇒ onboarding modal ⇒ "Log in with Muhiya Account" ⇒ browser PKCE flow against the live platform ⇒ key written to `~/.muhiya/secrets.json` (0600) + `base_url` applied ⇒ auto-discovery lists **only** `muhiyacode_visible` models ⇒ `/model` picker shows them ⇒ chat streams ⇒ turn-2 DeepSeek cache-read > 0 in `/context` ⇒ `muhiya_log` cost shown ⇒ `/usage` returns account data ⇒ `/logout` re-shows the modal. Also: Esc at the modal ⇒ `/login <key>` path works.
**AC**: every step recorded (pass/fail + screenshot or transcript) in `LIVE_GAUNTLET.md`.

### E2. Cross-client regression
With the flag live and Gemini 2.5 Flash Lite unchecked: MuhiyaChat still lists it (`X-Client-App: MuhiyaChat`), image requests still route to it (vision router untouched, D-2), Claude-Code/curl/other clients see the unchanged full active list, and a MuhiyaCode request naming it exactly still works (inference ungated). `muhiya_log` still emitted to both first-party apps only (`handler.go:2172-2178`).
**AC**: four curl matrix calls + one MuhiyaChat image round-trip documented.

### E3. Gateway load & soak
30-minute soak at realistic concurrency (e.g. 50 in-flight streams, mixed DeepSeek/OR) with `-race`-built binary in staging: zero panics, zero `[BILLING-LOSS]`, memory plateau (sticky map + accumulator caps from A2c/A2d hold), graceful shutdown mid-load drains within 60 s.
**AC**: soak metrics attached to the delta doc.

### E4. Release & deploy order (zero-downtime)
1. Agent repo: commit the pending workspace argv changes separately → then land Workstream D + C7 → `make verify` → version bump (v1.0.3) → goreleaser/npm; **remember the npm vendored-binary shadow** (a stale global `muhiyacode` runs the vendored exe — overwrite the vendor exe or set `MUHIYACODE_BINARY` when testing locally).
2. Gateway: land A + B + C → `go test ./...` (+ DB tests) → Docker build → deploy; migration 021 auto-applies under the advisory lock; backfill makes it behavior-neutral.
3. Owner: C6 panel curation (uncheck chat-only models).
4. Run E1/E2 against prod.
**AC**: both repos tagged; prod `/health` shows the new migration count; E1/E2 signed off.

---

## §6 Execution order & dependencies

```
A2 (defects) ──┐
A1/A4 (audit delta, sign-in)  ─┤→ A3 (vet/race gate) ─┐
C1 → C2 → C3 → C5 (gateway flag) ─ C4 ────────────────┤→ B7 (live gauntlet) → E1-E4
B1/B2/B3/B5 (cache tests) → B4 (conditional) ─────────┘
D1 → D2 → D3/D4/D5 → D6/D7 (agent, independent of gateway work)
C7 (agent prune) — after C3 is deployed to staging
```
Parallelizable: {A*, B*, C1-C5} (gateway) ∥ {D*} (agent). C7 and E* last.

## §7 Verification quick-reference

```bash
# Gateway
cd /f/MuhiyaWorkspace/MuhiyaWorkspace
go vet ./... && go test ./... -count=1          # pure tests, no DB needed
TEST_DATABASE_URL=postgres://… go test ./... -race -count=1   # full suite
# Agent
cd "/f/MuhiyaCode Agent Go"
make verify        # check-fmt + vet + test + build
make test-race
# Flag smoke (staging)
curl -s -H "Authorization: Bearer $KEY" -H "X-Client-App: MuhiyaCode" $BASE/v1/models | jq '.data[].id'
curl -s -H "Authorization: Bearer $KEY" -H "X-Client-App: MuhiyaChat" $BASE/v1/models | jq '.data[].id'
```

## §8 Risks & non-goals

- **`X-Client-App` is client-asserted** — any caller can claim `MuhiyaChat` and see hidden models. Accepted: the flag is catalog curation, not access control (inference was never gated). If real per-key scoping is ever needed, that's a `virtual_keys`-side allowlist, out of scope.
- **Stale agent catalogs**: users who discovered a model before it was hidden keep it in `settings.json` until a refresh — C7 bounds this to one refresh cycle.
- **OR injection vs byte-stability**: B4 mutates bodies only deterministically and only for OR+Anthropic targets, so DeepSeek prefix caching (B1 invariants) is untouched.
- **Old admin JS**: merge-patch PUT means a cached `app.js` can't zero the new flag; the badge/checkbox just won't render until refresh.
- **Migration on prod**: additive column + backfill inside the advisory-lock migration path; no lock-heavy rewrite (`models` is a small table).
- **Non-goals**: platform (muhiya.com) code changes; distributed sticky store; gateway response caching; MiniMax threshold refactors; the turn-1 cache-miss/skills-drift work already tracked in `ULTIMATE_POLISH_PLAN.md`.
