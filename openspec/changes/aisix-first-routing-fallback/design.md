## Context

See `proposal.md` for motivation. This is a **planning-only update to an approved policy**: error-based AISIX→CPA fallback, pure priority for slash IDs, authorized fallback for CPA-only bare IDs, no lookup/cache, and one narrow retry exception. The local APISIX 3.19 prototype is the first implementation task and must pass before a relay mechanism or production implementation is locked.

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
- Only a complete, bounded pre-commit JSON response with status 404 and top-level `error.type == "model_not_found"` is eligible on the three classified OpenAI endpoints. Never inspect a nested message/code as an absence signal. Ambiguous/truncated/oversized bodies do not qualify. An Anthropic `not_found_error` alone would not distinguish origin; expanding classification to messages needs separate design.

CPA's final unknown-model response on Responses/chat is 400 `invalid_request_error`, `code = "model_not_found"`, `param = "model"`. Images use a different 400 unsupported-model message. Those differences must remain visible at the inner seam; existing Sub2API/public safe-error handling remains responsible for client-facing translation.

#### AISIX's model list

`GET http://aisix:3000/v1/models` from the inner selector's existing network works with an AISIX caller key. It returned 76 exact IDs in a **6,567-byte** OpenAI list, including 53 direct slash IDs and 23 logical IDs. This is not a health list: intentionally unusable models still appear. No ETag, Last-Modified or Cache-Control was returned. `created` is generated at request time, so a body hash is not a useful stable config revision.

**Rejected alternative limitation:** the list means *accessible concrete names for this key*, not *every definition*. An absent entry can mean ACL denial or an unenumerated wildcard match, not just absence. Managed configuration inspected for this research has two wildcard-access caller keys and zero wildcard model definitions, but that snapshot does not make list omission a durable existence oracle. No list-based rollout, privileged discovery credential, or list invariant is part of the selected mechanism. Inference to AISIX retains the original caller credential.

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

### D1. Approved error-based fallback; no discovery or cache

Send every classified request to AISIX with the caller key. Advance exactly once to terminal CPA with `CPA_API_KEY` only when AISIX returns HTTP 404 and a complete JSON body with top-level `error.type == "model_not_found"`, before any response headers/body/SSE are committed. Matching `error.code`, nested text, another type or status alone is insufficient. CPA's status, body and end-to-end headers are terminal, including its unknown-model error.

All non-miss AISIX responses pass through unchanged at the inner seam, including `upstream_error` with code `model_not_found`, other 404s, 401/403, 429 and 5xx. Preserve existing gateway timeout/connection-error semantics when there is no upstream response; do not fabricate an AISIX response or invoke CPA on transport failure. After response commitment, propagate stream failure as today without replay or synthetic terminal events. Existing public safe-error translation remains downstream of this seam.

Inspect at most **64 KiB** of a candidate JSON 404 body. Invalid/non-JSON, truncated, oversized, timed-out or otherwise ambiguous envelopes do not qualify. Keep inspected bytes until forwarding or discarding is decided; for an oversized non-miss, forward the buffered prefix plus remaining bytes without loss or duplication. Do not read a successful response wholesale. Fully consume a rejected miss before safe keepalive reuse; close incomplete/cancelled connections. No retry of either attempt and no cross-request membership state.

**Rejected alternatives (not runtime modes):**

| Alternative | Measured cost / benefit | Rejection reason |
| --- | --- | --- |
| Per-request `/v1/models` | 300 reused-connection samples: 1.122/63.357/86.513 ms p50/p95/p99; separate decode/set 0.116/0.645/1.422 ms; 76 IDs, 6,567 bytes; about 32.1 KiB/s at observed peak 5 requests/s | Adds lookup/tail cost on every request; permission-filtered and wildcard-incomplete, so omission is not authoritative; races reload between lookup and inference |
| Per-worker 1–5 s TTL cache | Cache-hit performance unmeasured; refresh pays list cost | Same false-absence risk plus stale membership, caller partitioning/eviction and refresh failure complexity; no measured cache percentiles |
| Selected error fallback | Unknown-response round trip 1.409/100.123/115.721 ms p50/p95/p99 on misses; full relay/replay overhead unmeasured | Uses AISIX's actual resolver; hits have no discovery round trip. Costs request replay and streaming-relay engineering, proven by prototype first |

No list lookup, TTL switch, stale-on-error or last-known-good behavior is implemented. Retain `docs/model-routing.md`'s ban on `/routing-index` lookups, last-known-good caches and provider-registration checks. Update that document during implementation to describe error fallback as the sole narrow exception to one-backend/no-cross-retry wording.

### D2. Relay candidate and mandatory prototype gate

**Candidate: a minimal Lua `resty.http` streaming relay**, with a content-capable APISIX lifecycle. Do not issue a full-response-buffering `request_uri()` call in the existing rewrite hook. Withhold candidate miss headers until JSON classification; otherwise copy status/end-to-end headers and stream incrementally using `body_reader`, flush/backpressure and cancellation. Keep protocol payload/SSE bytes unchanged, while removing hop-by-hop framing headers and letting nginx produce correct downstream framing. Preserve existing connect/send/read timeout semantics (currently 3600 s each), bounded keepalive and cleanup; do not describe separate socket timeouts as one total deadline.

Mechanism comparison:

- **nginx `proxy_intercept_errors` + `error_page` named location:** status-only interception cannot enforce the JSON predicate. AISIX can return non-miss 404s from providers; v1.5.0 error translation uses `upstream_error` for them. Research live-probed a failed target returning 400 with that type, not a provider 404; a deterministic non-miss 404 stub is therefore mandatory. An additional body-inspection step and preserved original method/body/credentials would still be required. Do not activate plain 404 interception.
- **Lua cosocket relay:** supports exact pre-stream JSON inspection and incremental forwarding without protocol conversion. It takes responsibility for replay, safe headers, streaming, timeouts, cancellation and socket reuse. This is the smallest plausible candidate, not a proven design.
- **APISIX plugin hooks:** header/body filters cannot safely start a second cosocket attempt after native proxy response processing has begun. A content-capable custom plugin using the same relay could work, but generic `ai-proxy-multi`/status-retry plugins do not enforce this predicate and may transform protocols. Do not introduce a plugin framework or sidecar unless the prototype demonstrates the existing seam cannot support the relay.

**FIRST task, before further implementation is locked:** run a local APISIX **3.19** harness with synthetic AISIX/CPA stubs. Prove byte-identical replay including file-backed bodies; delayed/split SSE bytes and arrival timing before EOF; caller/service credential isolation, `session_id` and request correlation; non-miss 404/upstream-error/auth/429/5xx/transport boundaries; terminal CPA error; limits, large-body memory, cancellation and cleanup. Record actual hook/config wiring and observations. This task is not complete merely because unit tests or strict validation pass. If it fails, stop for mechanism review rather than weakening replay/SSE/security behavior or deploying an unproven alternative. No relay/plugin prototype has been executed in this planning update.

### Prototype ablation: existing parser memory failure (2026-10-28)

The implementation worker stopped at the first resource-gate failure and requested review. The supervisor directed a comparative ablation before any new parser mechanism and explicitly authorized recording a **no-worse-than-current** prototype criterion. This is a supervisor direction, not independent human approval of reduced body support; the advertised 16/64 MiB ingress limits are unchanged. Full prototype completion and mechanism lock remain pending. No production or tracked runtime files were changed by this experiment.

Both runs used the pinned `apache/apisix:3.19.0-debian`, one worker, one CPU and the existing **134,217,728-byte (128 MiB)** memory/swap ceiling, with synthetic loopback AISIX/CPA stubs in another container. Baseline mounted the unmodified current selector and route template (only fixture CPA-key substitution), adapting listener addresses/custom Lua search path to the isolated test namespace. Candidate used `resty.http` in serverless `before_proxy`, existing full-body cjson validation, file-reader replay and bounded streaming. The candidate's health fixture replaced `request-termination`, which this pinned image did not contain; the baseline inference routes were not changed. This unrelated health-route finding is not repaired here.

| Request body | Current selector outcome / cgroup peak | Relay candidate outcome / cgroup peak |
| --- | --- | --- |
| Exactly 16 MiB | Pass, one CPA attempt; 127,205,376 B | Pass, AISIX then CPA with identical SHA-256; 120,291,328 B |
| Exactly 17 MiB Responses | Pass; 125,632,512 B | Not sampled |
| Exactly 18 MiB Responses | Pass; 128,798,720 B | Not sampled |
| Exactly 19 MiB Responses | Pass; 134,217,728 B, no OOM | Pass, identical replay; 134,217,728 B, no OOM |
| 20, 24 and 32 MiB Responses | Worker OOM; no backend attempt | Not sampled |
| Exactly 64 MiB Responses | Worker OOM; peak 134,217,728 B, `oom=1`, `oom_kill=1` | Same worker OOM, same peak/events; client disconnected |

These are single isolated samples with one large request in flight, not a capacity guarantee. The largest passing body in the **integer-MiB sweep** was 19 MiB; no byte-exact maximum is claimed. Peaks include charged page cache and need repeated paired measurements; a saturated peak alone cannot quantify allocations beyond the ceiling. The existing full-body reader plus cjson extraction already fails before dispatch at 64 MiB. Do not attribute that pre-existing failure to fallback or claim 64 MiB success. Track a separate bounded parsing/sanitization follow-up; do not raise memory or lower ingress limits silently.

Comparative resource gate directed by the supervisor: retain the existing cgroup; at 16 MiB and the largest reproducibly passing baseline size, the relay must complete with no OOM and peak growth no more than **8 MiB** above a paired baseline, while remaining within the existing cgroup ceiling. Repeat paired runs and concurrency checks before completion. Any baseline failure remains explicit evidence, not a passing near-limit replay assertion. A new bounded streaming JSON parser is not authorized by this ablation.

The initial small-body candidate passed ten attempt/error boundary assertions, including terminal CPA miss, non-miss 404 with `upstream_error/code=model_not_found`, 400/401/403/429/500 and connection closure. Both delayed SSE cases produced SHA-256 `f72689f1056e78e26d9247275d5f094f611ce6ed5f8053543d96bc26022f1661`; first-byte/EOF arrival was 0.001/1.201 s on an AISIX hit and 0.043/1.243 s after fallback. Caller/service credential isolation and `session_id` passed. Cancellation, concurrent large bodies, split/ambiguous candidate bodies, one-time retention and limit-plus-one/chunked checks remain unproven; task 0.1 remains unchecked.

Private reproducible harness and logs: `/var/tmp/ai-aisix-first/prototype/` (`relay.lua`, `run.sh`, `baseline-run.sh`, `stub.py`, `probe.py`, baseline/candidate resource and probe logs). No production access was used.

### D3. One ordered list, exact bounded replay, safe logs

Use one Lua list with entries supplying upstream identity, credential mode and a miss predicate: AISIX uses caller credential and the exact 404/type predicate; terminal CPA uses the service credential and a never-advance predicate. A future component needs one entry plus its proven miss signature, address and credential mode, not per-model naming maps or top-level backend-name conditionals. No runtime switches for rejected mechanisms.

Current bounds are factual: `config.yaml` sets nginx `client_max_body_size: 64m`; the Responses route sets `client-control.max_body_size: 67108864`, and the global rule limits other routes to `16777216`. Retain **64 MiB for `/v1/responses`, 16 MiB for other classified JSON**. Read/buffer the complete request before the first attempt; retain one replay source, using disk-backed spooling where applicable rather than keeping duplicate large Lua strings. Existing file-backed model validation must continue to work. The current selector itself reads file-backed bodies for validation; the prototype must measure peak memory, not assume disk spooling alone proves the 128 MiB service budget.

nginx/client-control over-limit rejection remains **413 before any backend attempt**, including chunked bodies crossing the limit. Preserve the selector's existing defensive **400** when its standalone reader sees an invalid/over-limit body after boundary controls are bypassed. No truncation, larger limit, partial AISIX send or CPA fallback is permitted. Boundary tests include exactly-at-limit and limit-plus-one for both routes and the 64 MiB global ceiling.

Apply the existing `prompt_cache_retention` removal once before the AISIX attempt. If absent, preserve original validated request bytes; if present, reuse exactly the same sanitized serialization for AISIX and CPA. Never decode/re-encode between attempts. The original model string is never rewritten. Replay content length/framing must describe these same bytes.

AISIX receives only the original caller authorization; CPA receives only `CPA_API_KEY`. Remove conflicting credential headers before forwarding to CPA, preserve safe end-to-end headers including `session_id`, and do not return the service key in responses/logs. Missing CPA service key remains local 503 only when a proven miss requires CPA. Do not leak CPA credentials into AISIX, even on reused connections.

Emit exactly one **INFO** record for each actual fallback with escaped, bounded model ID, a non-secret request ID and AISIX→CPA identity. Preserve the upstream `x-aisix-request-id` where applicable and an entrance correlation ID; generate a correlation ID if none is trustworthy. The current inner `error_log_level: warn` would suppress INFO: implementation must enable an appropriate INFO logging sink/level and verify the record is visible. Never log bodies, credentials, full headers or private addresses.

### D4. Approved slash/bare policy; unchanged catalog and authorization

**AISIX-defined slash IDs use pure priority**, with no slash compatibility exception. These **53 IDs** change from direct CPA to AISIX (point-in-time inventory):

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

These direct entries target the same-named CPA IDs through the shared CPA provider pool. The approved change adds AISIX processing/accounting and uses AISIX's pool credential instead of the selector's CPA service credential on the final CPA hop; do not assume those private credentials are equal. AISIX authenticates the caller; CPA retains provider credential custody. Sub2API remains client quota/entitlement authority. Verify usage attribution and `session_id` forwarding. Direct models do not acquire logical-route hashing merely by entering AISIX; existing logical hash/fallback policies stay unchanged. The seven Axis entries remain intentionally present and may still fail; presence does not promise availability. Do not investigate or remove them in this work.

**CPA-only bare IDs may fall back for authorized requests**, with catalog membership unchanged. CPA `force-model-prefix=false` registers bare copies, so this exposes an invocation path previously stopped at AISIX's 404. A bare name can select among all CPA credentials registering it, unlike a prefix-qualified name. Catalog omission is not authorization. Preserve and test Sub2API entitlement/group denial; do not add a catalog-visible-only gate or new routing lookup.

## Risks / Trade-offs

- **Miss signature changes across AISIX versions** → pin version/evidence, test exact status/type with deterministic negative 404 fixtures, re-prove semantics before upgrades. Never match code/message text or health failures.
- **Relay memory/stream regression** → prototype first; bounded 64 KiB error inspection and 16/64 MiB replay, no full successful-response buffering, concurrent large-body/RSS and cancellation tests within existing 128 MiB service limit. If preservation cannot fit, stop for review rather than raising limits or dropping support.
- **Expanded bare-ID invocation** → explicitly approved; preserve Sub2API authorization and test denied callers while leaving catalog unchanged.
- **Different slash path/credential/accounting** → explicitly approved; capture matching target and non-secret credential-identity evidence, verify `session_id` and provider error boundaries.
- **Added miss latency / relay overhead** → record full-path p50/p95 and separate hit/miss overhead; current probes measure only AISIX round trips, not replay or generation. No zero-overhead hit claim.
- **INFO log suppression or leakage** → configure visible INFO output, bound/escape model and request IDs, test credential/body absence.
- **Future backend semantics differ** → each new entry needs a proven miss predicate. Unknown, denied or unhealthy never means absent.
- **Concurrent work / stale spec base** → reconcile against completed Lua/public-sort changes before implementation; preserve their membership/order/priority and separate production windows.

## Migration Plan

1. User policy is resolved. **First complete task 0.1's local APISIX 3.19 prototype gate**, then record the proven mechanism and reconcile deltas against current main specs. No production work or mechanism lock before this gate; planning tasks remain unchecked here.
2. Add regression tests, implement only the selected error fallback, wire harness into `ops/scripts/validate.sh`, and update short README plus detailed `docs/model-routing.md`, retaining no-index/cache rules and documenting the narrow retry exception.
3. Run `./ops/scripts/validate.sh .env.example` with any local `compose.override.yaml` temporarily moved to `/var/tmp` and restored through an exit trap. Commit/push only explicit approved paths, unsigned, then wait for CI on exact implementation SHA. Do not run gateway validation or mutate override files during this planning update.
4. Existing operations were inspected: `ops/ansible/playbooks/deploy.yml -e component=apisix-models` uses `ai_ops_component` for guarded image/env/Compose transactions. `deploy-config.yml` manages AISIX, model-catalog, model-sync and egress config, **not these tracked Lua/YAML files**. Neither installs the source-file changes. Preview the applicable component playbook with `--check --diff`; do not claim it deployed Lua.
5. Use one reviewed, low-traffic manual source-file window. Save live Lua, config, template, start script, Compose/env identity and checksums privately. Transfer only reviewed files into the host layout, preserve ownership/SELinux labels, then recreate **only `apisix-models`**, no build/pull/dependency recreation. This rebinds replaced files, clears Lua state and reruns CPA-key injection. Recreate interrupts streams: sample active connections and record window/rollback criteria. Do not overlap Lua-catalog or public-front-door windows. No topology/image/secret change is silently authorized by this plan.
6. Capture pre/post catalog bytes/IDs, health, restarts, routes and logs. Prove AISIX-known logical and slash models use AISIX; CPA-only prefixed and bare models fall back exactly once with visible INFO/request IDs; unknown-to-both returns terminal CPA error; SSE stays incremental; AISIX target failure does not fall back. Preserve `session_id` and authorization. Do not inject production faults or revisit Axis; use controlled harness faults for destructive negative cases and correlate available non-destructive live evidence.
7. Record post-activation p50/p95 added latency for misses, sample limits, health and resource use; verify source checksums plus final applicable drift checks `changed=0 failed=0`. Delete any temporary normal-client key with DELETE success, subsequent GET 404 and local credential-file absence. Record sanitized evidence under `docs/records/` during implementation only.
8. Rollback restores the saved changed source files together and recreates only `apisix-models`; prove old slash selection, catalog, health and SSE. Generic image/env rollback alone does not restore Lua. Keep rollback ready; failed acceptance triggers rollback, not unrelated component changes.

No unresolved user-policy decisions remain. Exact relay hook/wiring remains a **blocking prototype result**, not permission to deploy an unproven mechanism.
