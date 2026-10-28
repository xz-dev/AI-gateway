## Context

See `proposal.md` for motivation. This is a **planning-only, decision-blocked change**. The recommendations below are not operator approval. Tasks 0.1–0.2 must resolve A–D and make the deltas describe one selected policy before implementation or deployment. No runtime mechanism is selected by this document.

Today `routing_selector.lua` validates the top-level model and selects CPA when the string contains `/`, otherwise AISIX. The classified seam is exactly `POST /v1/responses`, `/v1/chat/completions`, and `/v1/images/generations`. Multipart `/v1/images/edits` goes directly to AISIX; `/v1/messages`, direct WS upgrades and other catch-all requests go to CPA. `GET /v1/models` goes to model-catalog and must not be used recursively as the selector's discovery URL.

### Research evidence (2026-10-28)

Saved read-only probes are in `/var/tmp/ai-aisix-first/`: `probe.lua`, `probe-unknown.txt`, `probe-dead.txt`, `timing2.lua`, `timing2.txt`, `usage-24h.txt`, `ts.txt`, and `aisix-ids.txt`. They are operator scratch, not durable repository attachments. The production AISIX pin is 1.5.0; APISIX is 3.19.0. Source references below are to AISIX tag **v1.5.0**, not merely the newer checkout HEAD:

- `crates/aisix-proxy/src/error.rs`: `ModelNotFound`, `status()`, `kind()`, and `anthropic_kind_from_status()`.
- `crates/aisix-proxy/src/error_translate.rs`: upstream-originated OpenAI errors use `error.type = "upstream_error"`, even when `error.code = "model_not_found"`.
- `crates/aisix-proxy/src/model_resolve.rs`: exact models and wildcard aliases can resolve at dispatch.
- `crates/aisix-proxy/src/models.rs:55–105`: listing authenticates the caller, omits wildcard-pattern names, includes direct models and virtual routes, and filters through `accessible_models`.
- `crates/aisix-proxy/src/messages.rs`: Anthropic error mapping and model resolution before dispatch.

#### Unknown model versus broken target

For both a bare and a slash-containing unknown ID, the three classified endpoints return **HTTP 404**, `Content-Type: application/json`, chunked transfer, and `x-aisix-request-id`. The body is:

```json
{"error":{"message":"model \"aisix-first-probe-unknown\" not found","type":"model_not_found"}}
```

| Direct AISIX endpoint | Saved modes | Unknown-model result |
| --- | --- | --- |
| `/v1/responses` | non-stream and stream | 404, OpenAI envelope above, before SSE commitment |
| `/v1/chat/completions` | non-stream and stream | same |
| `/v1/images/generations` | ordinary JSON request | same |
| `/v1/messages` | stream | 404, `{"type":"error","error":{"type":"not_found_error","message":"model \"aisix-first-probe-unknown\" not found"}}` |

`/v1/messages` is research context, **not** a new classified route. Its non-stream mode and multipart edits were not live-probed; no streaming image behavior is claimed by the saved JSON probe.

An existing AISIX direct model with an unregistered CPA target instead returned **400 `error.type = "upstream_error"`**. On chat, `error.code = "model_not_found"` was preserved; on Responses the CPA JSON appeared inside `error.message`. Thus neither matching `code` nor searching the message for `model_not_found` is safe. This existing saved probe is sufficient; do not investigate or modify the intentionally retained Axis entries again.

Other distinctions:

- Invalid/missing caller key: 401 `invalid_api_key` (live-probed), even for an unknown model.
- Malformed JSON/missing model: 400 `invalid_request_error` (live-probed).
- Forbidden model: 403 `permission_denied`; unavailable provider/all candidates: 503 `provider_unavailable` / `all_candidates_unavailable` (source evidence, not live fault injection).
- Rate limit 429, connection failures, upstream 404 and all target failures are **not absence**. Source fixes upstream OpenAI error type to `upstream_error`. A status-only 404 rule would conflate missing definitions with provider errors.
- If error fallback is selected, only a complete, bounded pre-commit JSON response with status 404 and top-level `error.type == "model_not_found"` is eligible on the three classified OpenAI endpoints. Never inspect a nested message/code as an absence signal. Ambiguous/truncated/oversized bodies do not qualify. An Anthropic `not_found_error` alone would not distinguish origin; expanding classification to messages needs separate design.

CPA's final unknown-model response on Responses/chat is 400 `invalid_request_error`, `code = "model_not_found"`, `param = "model"`. Images use a different 400 unsupported-model message. Those differences must remain visible at the inner seam; existing Sub2API/public safe-error handling remains responsible for client-facing translation.

#### AISIX's model list

`GET http://aisix:3000/v1/models` from the inner selector's existing network works with an AISIX caller key. It returned 76 exact IDs in a **6,567-byte** OpenAI list, including 53 direct slash IDs and 23 logical IDs. This is not a health list: intentionally unusable models still appear. No ETag, Last-Modified or Cache-Control was returned. `created` is generated at request time, so a body hash is not a useful stable config revision.

**Important limitation:** the list means *accessible concrete names for this key*, not *every definition*. An absent entry can mean ACL denial or an unenumerated wildcard match, not just absence. Managed configuration inspected for this research has two wildcard-access caller keys and zero wildcard model definitions. Verify that invariant against live configuration before any list-based rollout. A restricted key or wildcard definition must block that rollout until its policy is resolved; it must not silently broaden access via CPA. Using a full-definition credential for discovery is a possible later decision, not a new secret or configuration silently added by this change. Inference to AISIX always retains the original caller credential.

#### Measured latency and load

`timing2.lua` ran with LuaJIT/resty inside the production `apisix-models` container, through its existing AISIX relay. Each row has **300 sequential samples**. Times below are milliseconds, not end-to-end generation times:

| Extra operation | p50 | p95 | p99 | max |
| --- | ---: | ---: | ---: | ---: |
| Live list, reused connection | 1.122 | 63.357 | 86.513 | 156.396 |
| List JSON decode + ID-set construction | 0.116 | 0.645 | 1.422 | 59.381 |
| Live list, fresh connection per sample | 2.736 | 92.230 | 263.265 | 482.689 |
| Fresh-connection list decode + set | 0.104 | 0.636 | 2.413 | 67.532 |
| AISIX unknown Responses request, reused connection | 1.409 | 100.123 | 115.721 | 184.218 |
| AISIX unknown Responses request, fresh connection per sample | 2.331 | 102.075 | 263.413 | 556.739 |
| Today's additional discovery/miss check | 0 | 0 | 0 | 0 |

The zero row is **by construction**, not a latency benchmark of today's whole inference path. Today's slash selector has no extra network check. Reused-list p50 plus decode p50 is roughly **1.24 ms**; this is an illustrative sum, not a measured combined percentile. Percentiles must not be added to claim combined p95/p99. The fresh-connection probe starts its timer **after `connect()`**, so those rows exclude DNS/TCP establishment and understate cold-path total cost. CPU scheduling and the live relay produce substantial tails: do not summarize this as “always about 1 ms”. No selected implementation, full error-replay path, TTL cache, or end-to-end inference comparison has been benchmarked yet.

Interpretation:

- **Per-request list:** one list round trip + decode on *every* classified request, before the unchanged native inference proxy.
- **Error fallback:** no preliminary discovery round trip on AISIX hits; misses pay the measured unknown-response round trip before CPA, plus unmeasured relay/replay overhead. All AISIX responses need a controlled relay, including successful streams.
- **Short TTL:** cache hits remove list I/O (local lookup cost not benchmarked); refresh requests pay the list cost. Aggregate p50/p95/p99 depend on key, TTL and hit ratio, so no invented cache percentiles are supplied.

Saved inner access-log timestamps cover **15,275 classified POSTs** in the requested 24-hour log window (first/last retained timestamps span about 21 h 42 min). Average over the requested window is **0.177 requests/s**; observed peak fixed second is **5 requests/s**, and busiest fixed minute is **67 requests/min** (1.12/s). This is observed traffic, not a capacity or saturation test. Per-request lookup at that peak adds 5 list calls/s and **32,835 response bytes/s** (about 32.1 KiB/s, excluding HTTP framing), or about 100.3 MB of list bodies for 15,275 requests. It does not call inference providers. Measure actual CPU/RSS and concurrency in the local harness before rollout.

The separate Sub2API usage sample totals 45,926 rows across all model paths, including embeddings/reranking and internal jobs; it is **not** the classified-request denominator. It contains **1,746 calls to AISIX-defined slash IDs** that would change path under pure AISIX-first. Do not divide that number by the inner access-log count to infer a precise miss rate.

## Goals / Non-Goals

**Goals:** replace backend naming conventions with explicit priority; distinguish local absence from operational failures; preserve model/body fidelity, credentials, authorization, session headers, SSE and bounded resources.

**Non-Goals:** health-based cross-backend failover; a generic provider adapter platform; changing AISIX internal fallback or Sub2API retries; changing catalog membership, entitlement, CPA model registration, multipart/WS catch-all routing; Lua membership rollout, public-list sorting, pruning or dependency updates.

## Decisions

### D1. Compare three mechanisms; A is still open

**Recommendation: L, live list per request**, conditional on the all-access-key/no-wildcard invariants above and operator approval of A–D. It answers the user's list alternative, keeps inference in APISIX's native proxy, costs little at the observed traffic rate, and avoids implementing a streaming relay just to inspect errors. It is not inherently a stronger existence oracle outside those invariants.

| Concern | L: list per request | T: per-worker short TTL (1–5 s) | E: error fallback |
| --- | --- | --- | --- |
| Routing | Fresh AISIX list, then one native inference proxy | L on refresh; bounded per-key membership on hit | Send actual request to AISIX; replay to CPA only on confirmed local absence |
| Failure/timeout | Recommend dispatch to AISIX, not CPA, if list fails; keep its auth/error behavior | Same on cold/expired refresh failure; recommend **no stale-on-error**; last-known-good serving needs separate approval | Auth, ACL, 429, 5xx, transport/timeout, provider error and malformed error all stop; never skip to CPA |
| Bounds proposed for review | One total 2 s lookup deadline, connect/send/read individually capped within it; 1 MiB decoded response ceiling; bounded keepalive pool (32 idle, 60 s), no automatic retries | Same lookup bounds; bounded LRU by credential identity, entry/byte limits, single-flight refresh; never raw keys in cache IDs/logs | Existing inference deadlines (up to 3600 s), one attempt per backend, bounded not-found inspection (64 KiB); no new global retry budget |
| Deploy consistency | Next successful list sees new snapshot; dispatch can race a later reload; listed-then-missing returns AISIX error, no second backend | Up to TTL stale; invalidation not supplied by AISIX; workers can disagree; process restart clears cache | Dispatch sees current snapshot, no list/dispatch TOCTOU gap |
| Credentials and callers | Lookup and AISIX inference use caller key; CPA gets service key; restricted-list absence cannot authorize fallback | Same; partition by credential identity, never share membership or auth decisions across callers | AISIX proves caller authentication before reporting absence; CPA gets service key only after eligible miss |
| SSE | Native proxy unchanged; no response-body read in selector | Same | Relay must withhold headers until classification, then stream incrementally; no fallbacks after commitment |
| Request body | Existing validation/spooling limits, no second inference send | Same | Must retain/replay exact validated bytes, including file-backed 64 MiB Responses; avoid duplicate Lua strings and relay buffering |
| Cost | List + decode on every request | Refresh cost + unmeasured cache-hit cost; low traffic limits gain | Unknown round trip on miss; new relay overhead on hits and misses, unmeasured |

The 2 s lookup deadline is a proposed cap for A, not an existing timeout or a measured SLA. Treat timeout, non-200, non-JSON, invalid `data`, invalid IDs, duplicate/ambiguous schema and response over the limit as **unknown membership**, not an empty list. Ensure pooled responses are fully consumed or close the connection. Total deadlines must include connection acquisition, connect, headers and body; setting three independent socket timeouts alone does not establish a total deadline.

**L/T failure-policy choice (part of A):** prefer native dispatch to AISIX on uncertain membership. This avoids using CPA's service key after an invalid AISIX credential or discovery outage and preserves backend errors; it also means CPA-only requests are unavailable while discovery fails. Alternative: a local protocol-shaped 503 without inference. **Do not fail open to CPA.** No stale-on-error default is implied by T.

Other implementation mechanisms considered for E:

- **resty.http/custom Lua relay:** technically can inspect headers and a bounded 404 body before choosing CPA. This is not a small `ctx.upstream_id` change. It needs a deliberate content-phase/plugin lifecycle, incremental `body_reader`, flush/backpressure, cancellation, hop-by-hop header stripping, correct framing, request replay, timeout and keepalive cleanup. A rewrite-phase full `request_uri()` response buffer is not acceptable. Exact preservation requires a real APISIX harness before claiming support.
- **`proxy_intercept_errors` / `error_page`:** native interception is status-based, not `error.type`-based. Generic 404/5xx interception is unsafe. A named-location/internal-redirect design would also need reviewed method/body retention, credentials and original request context. Do not inject nginx snippets merely to infer absence from status.
- **APISIX `ai-proxy-multi` / retry plugins:** generic provider/status fallback does not establish this local-model-absence predicate and may impose protocol conversion. Do not replace transparent Responses/SSE forwarding with those semantics. A custom plugin implementing the same bounded relay remains possible but is larger than L.

No E relay or new plugin has been implemented or validated by this research. No extra sidecar is proposed.

### D2. One ordered configuration, not a backend framework

Keep backend order in one Lua list, initially AISIX then terminal CPA. Each entry supplies the existing upstream identity, credential mode and selected presence predicate; terminal CPA needs no discovery. A future component still must supply an address, credential and compatible membership/error contract—“one entry” cannot remove those requirements. No per-model naming maps or per-backend top-level conditionals. Do not add runtime mechanism switches for all three alternatives: implement only A's selected mechanism.

Preserve 16 MiB ordinary JSON / 64 MiB Responses bounds, model validation, file-backed body handling, and the existing `prompt_cache_retention` removal. Preserve `session_id` and caller-safe correlation headers on inference, never put request bodies, caller keys or service keys in routing logs. A missing CPA service key remains a local 503 before CPA forwarding. Log selection/fallback once with escaped, bounded model ID and backend identities; do not call a lookup-only miss a failed inference attempt.

### D3. Routing and catalog are separate; B/C/D remain open

**B — AISIX-defined slash IDs:** recommend pure priority for all IDs. These **53 IDs** change from direct CPA to AISIX under that choice (point-in-time inventory):

```text
aihub/gpt-5.6-sol
aihub/gpt-5.6-terra
aihub/gpt-6-astra
axis/gpt-5.6-luna
axis/gpt-5.6-sol
axis/gpt-5.6-terra
axis/gpt-6-astra
axis/gpt-6-luna
axis/gpt-6-sol
axis/gpt-6.1-sol
cline/deepseek/deepseek-v4.1-flash
codex/gpt-5.6-luna
codex/gpt-5.6-sol
codex/gpt-5.6-terra
codex/gpt-6-astra
codex/gpt-6-luna
codex/gpt-6-sol
codex/gpt-6.1-sol
codex/gpt-image-1.5
codex/gpt-image-2
codex/gpt-image-2.5
codex/gpt-image-2.5-flare
codex/gpt-image-2.5-sunburst
commandcode/deepseek/deepseek-v4.1-flash
commandcode/zai-org/GLM-5.2
commandcode/zai-org/GLM-5.3
devin/glm-5-2
devin/glm-5-3
kimi-coding/kimi-k3
kimi-coding/kimi-k3-256k
ollama-cloud/deepseek-v4.1-flash
ollama-cloud/glm-5.2
ollama-cloud/glm-5.3
ollama-cloud/glm-5.3-flash
senseaudio/deepseek-v4.1-flash
senseaudio/glm-5.3-flash
sensenova/deepseek-flash
sensenova/glm-5.2
sensenova/kimi-k3
supergrok/grok-4.7
supergrok/grok-imagine-image
supergrok/grok-imagine-image-2.0
supergrok/grok-imagine-image-quality
xl/claude-fable-5-1
xl/claude-opus-5-5
xl/gpt-6-luna
xl/gpt-6.1-sol
xl/gpt-image-2.5-flare
xl/gpt-image-2.5-sunburst
xl/grok-4.7
zcode/glm-5.2
zcode/glm-5.3
zcode/glm-5.3-flash
```

These direct entries target the same-named CPA IDs through the shared CPA provider pool. The change adds AISIX processing/accounting and uses AISIX's pool credential instead of the selector's CPA service credential on the final CPA hop; do not assume those private credentials are equal. AISIX authenticates the caller; CPA retains provider credential custody. Sub2API remains client quota/entitlement authority. Verify usage attribution and `session_id` forwarding on the chosen endpoints. Direct models do not acquire logical-route hashing merely by entering AISIX; existing logical hash/fallback policies stay unchanged. The seven Axis entries remain intentionally present and may still fail; presence does not promise availability. Alternative B: keep slash IDs direct to CPA, an explicit compatibility exception which retains the naming convention the user wants to remove.

**C — CPA-only bare IDs:** recommend allowing terminal CPA fallback for authorized requests, while leaving catalog membership unchanged. CPA `force-model-prefix=false` registers bare copies, so this exposes an invocation path that previously stopped at AISIX's 404. Such a bare name can select among all CPA credentials registering it, unlike a prefix-qualified name. Catalog omission is not authorization. Sub2API entitlement/group controls must still deny unauthorized invocation; verify rather than assume those controls. Alternative: keep CPA-only bare IDs rejected at AISIX. A “catalog-visible only” fallback rule adds a distinct catalog/admission dependency and cannot be smuggled into this design as a small filter. If chosen, revise the deltas and scope first.

**D — documented no-lookup/cache rule:** the current `docs/model-routing.md` says no catalog lookup and explicitly prohibits `/routing-index` lookups and last-known-good caches. L reverses the no-lookup rule, even though its URL is `/v1/models`, not `/routing-index`; T also reverses the no-cache rule. Recommend allowing **direct live backend membership only**, retaining the ban on aggregated routing indexes and last-known-good fallback. E needs an explicit exception to the current one-backend/no-cross-retry wording for proven pre-dispatch absence, but introduces no list/cache. Do not leave contradictory rules in docs or main specs after approval.

## Risks / Trade-offs

- **ACL/wildcard false absence** → list rollout blocked unless live keys are full-access and definitions concrete, or a separately approved safe membership policy exists. Never treat permission denial as absence.
- **Lookup tail latency/outage** → disclose measured tails, impose total deadline/body cap and conservative failure policy; measure implementation under concurrent load, not only sequential probes.
- **Reload race** → L/T cannot guarantee atomicity with inference dispatch. A listed model removed between calls returns AISIX's failure without retry; T additionally has explicit TTL staleness.
- **Expanded bare-ID invocation** → approve C, preserve Sub2API authorization, test denied callers, do not change public catalog visibility to conceal this effect.
- **Different path/credential/accounting on slash IDs** → approve B; capture matching target and credential-identity evidence without recording secrets; verify session affinity and provider error boundaries.
- **Relay memory/stream regression if E wins** → bounded inspection and replay, no full response buffering; test cancellation, large file bodies, split SSE frames, premature EOF and final CPA errors before rollout.
- **Future backends vary in discovery/error semantics** → one list entry is possible only with a proven compatible predicate. Unknown is not absent. Do not make health failures advance priority.
- **Concurrent work / stale spec base** → rebase deltas onto any completed Lua/public-sort changes before apply; preserve their catalog ordering, priority and membership guarantees. Keep production windows separate.

## Migration Plan

1. Obtain A–D; revise this change into one concrete mechanism/policy. Strict validation is structural evidence, not approval. All tasks remain unchecked during planning.
2. Tests first: selector units and a local APISIX 3.19 harness with stub AISIX/CPA, plus latency/resource and SSE/replay assertions appropriate to A. Preserve current routes and startup credential injection unless selected mechanism proves a necessary change.
3. Run `ops/scripts/validate.sh .env.example` with any local `compose.override.yaml` temporarily moved to `/var/tmp` and restored on every exit path. Commit/push only explicit approved paths, unsigned, then wait for CI. Do not run this validation or mutate override files during this planning task.
4. Existing operations were inspected: `ops/ansible/playbooks/deploy.yml -e component=apisix-models` uses `ai_ops_component` for guarded image/env/Compose transactions. `deploy-config.yml` manages AISIX, model-catalog, model-sync and egress config, **not these tracked Lua/YAML files**. Neither is a source-file deployment mechanism. Do not claim playbook success deployed Lua.
5. Use one reviewed, low-traffic manual window for the selected source-file change. Save live Lua, config, template, start script, Compose/env identity and checksums privately. Transfer only the reviewed files into the current host layout, preserve file ownership/SELinux labels, then recreate **only `apisix-models`**, with no build, pull or dependency recreation. Recreating rebinds replaced files, clears Lua module state, and reruns `start.sh` to inject the existing CPA key if the template changed. Do not mix this with the Lua catalog or public-front-door windows. No Compose topology/image/secret change is authorized here.
6. Pre/post capture catalog bytes/IDs (unchanged), health, restart identity, routes, key ACL and wildcard invariants, requests by path, and selection/target logs. Test AISIX hit, CPA-only prefixed hit, B's slash path, C's bare policy, unknown everywhere, streaming, auth/ACL rejection, lookup-failure policy, and a controlled stub target failure that must not fall back. Avoid further Axis diagnosis. Use existing production keys privately; delete any temporary normal-client key with DELETE success, subsequent GET 404 and local-file removal evidence.
7. Rollback: restore the saved source files together, recreate only `apisix-models` again, verify slash-rule paths, health, catalog and SSE. Generic image/env rollback alone is insufficient for these files. Record exact digests/checksums, timing, tests and redacted evidence under `docs/records/` only during authorized implementation.

## Open Questions — required user decisions before apply

These are **blocking**, not deferrable implementation details. This task intentionally prepares alternatives for review rather than asking a worker to choose them.

| Decision | Options | Recommendation (not selected) |
| --- | --- | --- |
| **A: mechanism and failure policy** | L live list/request; T bounded per-worker 1–5 s cache; E proven-absence error fallback. For L/T uncertainty: native AISIX error or local 503; no CPA fail-open. Resolve restricted keys/wildcards before enabling L/T. | L, no cache, conservative native AISIX dispatch on uncertainty, only with full-access-key/concrete-definition invariants verified |
| **B: AISIX-defined slash IDs** | Pure AISIX-first for the 53 IDs above; or explicit slash→CPA compatibility exception | Pure priority; accept AISIX accounting/credential hop; preserve `session_id` |
| **C: CPA-only bare IDs** | Allow authorized fallback while remaining catalog-hidden; or retain rejection; catalog-visible-only policy requires scope/spec revision | Allow with explicit authorization tests; catalog visibility is not authorization |
| **D: current rule reversal** | Allow live backend discovery while banning indexes/LKG; additionally allow bounded TTL if T; or E's proven-absence-only retry exception with no discovery | Match approved A explicitly; do not silently replace the old prohibition |
