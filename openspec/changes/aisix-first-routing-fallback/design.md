## Context

**Decision A is revised by the user: routing must be a streaming pipe, not a bigger bucket, and legal field order must not change routing.** Error-based fallback/replay remains rejected. Read incrementally until a complete model or uncertainty, using one spooled sliding-window buffer: a fixed 64 KiB memory window with automatic lazy disk overflow. A background-refreshed AISIX membership set permits selection as soon as the model is available. Late models add upload/disk latency, not a speculative AISIX decision. Decisions B/C remain: pure priority for AISIX-defined slash IDs, and authorized CPA-only bare IDs may reach CPA without entering the catalog.

This document is a planning update, not an implementation or prototype approval. The next prototype must not run until the supervisor confirms this plan. `MAX_ROUTING_PREFIX_BYTES=65,536` is the single named **in-memory window bound**, not a model-search cutoff. The completed client survey and separately approved keeper model-first work support that size, but do not establish production offset percentiles or final Sub2API ordering. User decisions recorded 2026-10-28 resolve retention ownership (targeted CPA config, no gateway filter), post-dispatch validation/cancellation, HTTP passthrough OFF and a 1 GiB aggregate spool quota. Prototype/storage implementation evidence and supervisor review remain gates, not permission to run.

### Existing routing and compatibility responsibilities

The current code was inspected, not inferred from desired behavior:

| Location | Routes | Current semantics |
| --- | --- | --- |
| `services/routing/apisix-models/routing_selector.lua`, called by two rewrite hooks in `apisix.yaml` | POST `/v1/responses`, `/v1/chat/completions`, `/v1/images/generations`, regardless of Content-Type | `ngx.req.read_body()` collects the complete upload; read memory or entire bounded body file; cjson decodes the complete object. Reject malformed JSON, scalar/null/array, absent/empty/non-string `model`, model over 1,024 bytes and over-limit standalone reader with local 400 before forwarding. Nonstandard numeric values are disabled. |
| Same selector, `strip_prompt_cache_retention` | Same three classified routes | Remove the top-level field whenever present, including JSON null; encode the complete remaining object once and replace the request body. Nested fields remain; arrays preserve their type. If absent, original body bytes remain unchanged. Model value is not rewritten. |
| `services/routing/apisix-models/apisix.yaml`, catch-all rewrite hook | Otherwise matched requests, including `/v1/messages`; only Content-Type containing the case-sensitive text `json` | Read the complete body, but process only an in-memory nonempty body. `dkjson` decodes a table; malformed/non-table/file-backed bodies are left alone, not locally rejected. If a top-level retention field exists, remove it and re-encode. No model validation. |
| Separate images-edits/catalog/WS routes | Multipart POST `/v1/images/edits`; GET `/v1/models`; direct WS upgrades | No classified JSON rewrite. Edits go to AISIX; catalog to model-catalog; WS frames pass to CPA without inspection. Front-door APISIX has no `prompt_cache_retention` remover. |
| nginx/global and route `client-control` | Responses: 64 MiB; other routes: 16 MiB | Existing size ceilings. With the current whole-body reader, normal oversized requests can be rejected before forwarding. The pipe cannot promise that for an upload whose length is initially unknown. |

The new pipe removes whole-body cjson decoding/retention stripping on the **classified** routes. Also remove the catch-all `dkjson` decode/strip/re-encode hook in `services/routing/apisix-models/apisix.yaml`: retention ownership moves to targeted CPA config, not the scanner. Other non-classified routing remains unchanged; this is not a gateway-wide streaming rewrite. The followup2 report says only catch-all strips retention, but the current selector's `strip_prompt_cache_retention` and its call prove classified routes strip it too; both removals are required. Sub2API authorization, catalog membership/order/priority and WS/keeper chain placement remain unchanged. The separately delivered keeper serialization-order optimization is a rollout prerequisite, not a topology change.

### Evidence: AISIX cannot supply an early miss for replay-free fallback

Read-only source checked at exact tag **v1.5.0** in `/var/tmp/ai-aisix-first/aisix`:

- `crates/aisix-proxy/src/responses.rs:189`: `Result<Json<Value>, ...>` extractor; unpack at 192; model resolution at 599.
- `crates/aisix-proxy/src/chat.rs:101`: `Result<Json<ChatFormat>, ...>` extractor; model resolution at 1509.
- `crates/aisix-proxy/src/images.rs:73`: `Result<Json<Value>, ...>` extractor; unpack at 76; model resolution at 323.

Axum's JSON extractor collects and parses the complete body before these handlers dispatch. Therefore AISIX's local model-not-found response follows the entire accepted upload, not just its early model field. Authentication or body-size failures can reject earlier, but are not absence signals.

Saved production research, not repeated for this update, found unknown bare/slash models return pre-SSE HTTP 404 JSON `{"error":{"message":"model not found","type":"model_not_found"}}` on these three endpoints. Actual message includes the requested model. A defined model with a failed target returns `error.type=upstream_error`, including cases with `error.code=model_not_found`. This exact distinction remains useful research but **does not authorize a retry in the selected design**. All backend errors are terminal.

### Evidence: list semantics, latency and load

AISIX `/v1/models` authenticates the key, filters accessible models and omits wildcard definitions (`models.rs:55–105`, tag v1.5.0). The saved list has **76 exact IDs, 6,567 bytes**: 53 direct slash IDs plus 23 logical routes. No ETag/Last-Modified/Cache-Control was present; `created` varies with request time. It is a definition list, not a health oracle: intentionally unavailable Axis models still appear.

Saved `timing2.lua` measurements used LuaJIT/resty inside the existing production selector namespace, 300 sequential samples per row, milliseconds:

| Operation | p50 | p95 | p99 | max |
| --- | ---: | ---: | ---: | ---: |
| List request, reused connection | 1.122 | 63.357 | 86.513 | 156.396 |
| Separate decode + set construction | 0.116 | 0.645 | 1.422 | 59.381 |
| List request, fresh connection | 2.736 | 92.230 | 263.265 | 482.689 |
| Unknown-model Responses request, reused connection | 1.409 | 100.123 | 115.721 | 184.218 |
| Unknown-model request, fresh connection | 2.331 | 102.075 | 263.413 | 556.739 |

Fresh-connection timers started **after connect**, so those rows exclude TCP/DNS setup. Independent percentiles must not be added. No end-to-end pipe, cache-hit, scanner or atomic-snapshot lookup benchmark exists yet. A local set lookup removes the discovery round trip from each request, not all selector CPU, scheduling, or upload-prefix delay.

Retained access logs showed 15,275 classified requests in the requested 24-hour window (actual retained timestamp span about 21 h 42 min), 0.177/s over that requested window, peak fixed second **5/s**, busiest fixed minute **67/min**. A separate broader Sub2API sample contained 1,746 invocations of AISIX slash IDs; it has a different denominator. These are observed loads, not capacity guarantees.

### Rejected replay prototype and pre-existing OOM

Private harness/evidence is in `/var/tmp/ai-aisix-first/prototype/`. It used APISIX 3.19.0, one CPU, one worker and the existing **134,217,728-byte** container memory/swap ceiling, with loopback stubs in another container. Baseline mounted the unmodified current selector and route template (fixture-key substitution only), adapting listeners and Lua module path to the isolated namespace. Candidate used `resty.http` in serverless `before_proxy`, existing cjson validation and file-reader replay.

| Body | Current selector | Rejected relay |
| --- | --- | --- |
| 16 MiB, one request | Pass; peak 127,205,376 B | Byte-identical replay; peak 120,291,328 B |
| 19 MiB Responses, one request | Pass; peak 134,217,728 B, no OOM | Byte-identical replay; same peak, no OOM |
| 20/24/32 MiB Responses | Worker OOM before dispatch | Not sampled |
| 64 MiB Responses | Worker OOM; peak at cap, `oom=1/oom_kill=1` | Same OOM and client disconnect |

An integer-MiB sweep also passed 17/18 MiB in baseline; 19 MiB was largest passing sample, not a byte-exact maximum. Three paired runs at 16/19 MiB with one large upload plus six small hits at 5/s all completed without OOM; candidate peaks ranged 120,582,144–122,003,456 B at 16 MiB and 133,038,080–134,201,344 B at 19 MiB. This established a pre-existing full-body parser problem, not proof that replay caused it. A supervisor had temporarily allowed comparative no-worse-than-baseline testing; **the revised pipe contract supersedes that allowance** and requires 64 MiB success under the same cap.

The rejected small-body relay passed ten status/attempt checks and two incremental SSE cases. SSE SHA-256 was `f72689f1056e78e26d9247275d5f094f611ce6ed5f8053543d96bc26022f1661`, first byte/EOF 0.001/1.201 s on a hit and 0.043/1.243 s after replay. Retention, cancellation and later fault additions were not completed before the pause. These are not pipe acceptance tests and do not lock a mechanism.

Incidental local finding: the image reported `module 'apisix.plugins.request-termination' not found` for the current health route. The isolated candidate used a test-only serverless health fixture; baseline inference routes stayed unchanged. Production plugin/config/image identity has **not** been checked by this worker; production was previously reported healthy. Do not infer production health failure or change its health route from this local observation. A read-only production correlation remains pending.

## Goals / Non-Goals

Goals: bounded-memory request and response pipes through one sliding-window spool, field-order-independent selection from one ordered backend list, no full-body model decoder/replay, no inference-path discovery request, full 64 MiB support under 128 MiB, explicit uncertainty/refresh/compatibility/storage semantics.

Non-goals: health-based failover, provider registration lookup, catalog membership changes, extending wildcard matching, Sub2API patches or passthrough/WS-mode/account-mapping changes, revisiting Axis, unclassified streaming redesign beyond removing catch-all retention processing, overlapping production windows, dependency updates or deferred unrelated work.

## Decisions

### D1. One backend, no retry

Use one ordered Lua list: AISIX (upstream, caller credential, exact snapshot membership source), then CPA (upstream, service credential, terminal/no membership source). Slash characters have no special meaning. A future backend adds an entry with a bounded membership source rather than per-model naming branches.

If a usable snapshot contains the exact model, choose AISIX. If it does not, choose terminal CPA. If routing is uncertain, choose the first priority AISIX directly. Inference always uses exactly one backend. Never inspect its error to select another; 404, `upstream_error`, ACL/auth failure, 429, 5xx, transport failure and mid-stream failure are terminal. Missing CPA key fails closed with local 503 only when CPA would be selected, never by substituting the discovery key.

### D2. One spooled sliding-window request pipe

Before any `ngx.req.read_body`, use `ngx.req.socket()` and a fixed-size chunk reader (initial candidate 8 KiB). No earlier plugin/nginx body reader may collect the complete classified upload. Use **one code path for every model position**, not `if early then pipe else spill`. Its buffer and scanner are separate narrow interfaces:

- `spool.append(chunk)`: retain byte order with at most `MAX_ROUTING_PREFIX_BYTES=65,536` bytes in its sliding in-memory window. When full, flush oldest bytes to a private temp file automatically. Create the file lazily at first overflow; this is a buffer detail, never a routing branch. Do not join/copy the complete retained prefix. Reader/drain chunks and scanner state are separately fixed-size overhead, not falsely included in a 64 KiB total-process claim.
- `spool.drain(sink)`: write the file part first, then the memory-window part, exactly once with backpressure; release consumed storage. Empty file part needs no alternate implementation. Subsequent chunks use the same append/drain operations; no complete-response buffer.
- `spool.close()`: idempotently close/unlink any file, release quota reservation and memory. Call on success, all read/scan/send/backend errors, timeout and client cancellation; clean abandoned instance files after crash/restart before accepting new spools.
- `scanner.feed(chunk) -> needMore | model(id) | uncertain(reason)`: incrementally validate JSON structure/tokens and match decoded top-level keys while retaining only bounded model/key/token state. EOF is explicit, detecting missing model, incomplete tokens/delimiters and trailing data. Keep `MAX_MODEL_BYTES=1024`; propose maximum nesting 128 with a fixed-capacity parser stack, so O(depth) grammar state has an O(1) bound independent of body size. Match escaped top-level `model` incrementally; nested keys are not routing fields. Invalid/empty/non-string/overlong model or exceeded scanner bounds are untrustworthy model/prefix cases, not permission to allocate more. Inspect the entire supplied chunk before committing so a duplicate already in that chunk is uncertain. Continue the same feed after dispatch; duplicate/malformed results then trigger cancellation, not another routing decision. Never decode a complete document or accumulate large non-model values; retention is not filtered.

The routing sequence is linear: **read → feed → append until decided → choose → drain → pipe the rest**. Every body byte is fed to the scanner and appended to the spool; after selection, append/drain continue under upstream backpressure with the decision latched. Model at offset 1 and at offset 4 MiB run the same logic; the former simply has an empty file part when its decision is made. A model token split across the 64 KiB boundary still yields `needMore`; keep reading/scanning with automatic overflow until the token completes. Neither memory-window overflow nor field position is uncertainty.

Choose from the membership snapshot when `model(id)` is available; if the snapshot is unusable, choose AISIX. Scanner uncertainty means no trustworthy top-level model at complete-body EOF, malformed/untrustworthy JSON before a usable decision, or a duplicate top-level model detected before commitment. These and no usable snapshot go to AISIX, with no second backend. A valid late CPA-only model absent from a usable snapshot goes to CPA, not AISIX. Storage/transport/size failures are explicit failures, not guessed membership absence or permission to switch backend.

The scanner cannot identify an arbitrary later duplicate, malformed suffix or missing closing delimiter **before** forwarding an early model. The user accepted loss of the old whole-document local-400/no-forward guarantee, but selected continuous validation rather than relying on backend rejection. After dispatch, feed each chunk before releasing it upstream and continue through EOF. A later duplicate top-level `model` (including escaped spelling), malformed token/structure or incomplete/trailing JSON cancels the selected upstream, closes incomplete connections and cleans the spool/reservation. Return local 400 if client response headers are unsent; otherwise abort the client connection, including an already-started SSE stream. Never reroute/retry. Scanned duplicates before commitment remain uncertain-to-AISIX under the existing prefix policy; do not claim all duplicate requests get a local 400.

Cancellation cannot retract already-sent bytes or guarantee execution/billing has not begun. No custom terminal SSE event or backend-guaranteed duplicate 400 is promised. Backend semantic validation still applies, but the same scanner keeps bounded structural/token validation after selection; there is no second parser or early/late path.

#### Disk placement, bounds and cleanup

Use a dedicated disk-backed spool directory `/var/tmp/apisix-models-spool` inside the container, backed by a reviewed named volume or host mount, not tmpfs or an unbounded container writable layer. Files must be private, unguessable, no-follow/exclusive-created and never logged with body contents; remove them as soon as drained/closed. Per-request stored bytes (file plus window) may not exceed the existing body ceiling: 64 MiB Responses, 16 MiB other classified JSON. A model at EOF can require storing nearly that whole allowed body, but only for initial selection, never for replay, full JSON decode or error-driven retry.

The user approved a **1 GiB (1,073,741,824-byte) aggregate spool quota**, superseding the 256 MiB proposal. Per-request maximum remains 64 MiB (16 MiB for other classified routes). Enforce accounting/reservations across workers; when quota is exhausted, reject new requests with 503. Reserve the route's maximum possible spill lazily at first overflow and release it at drain/close; concurrent overflow admission must be atomic, so admitted long uploads cannot silently overcommit space. Prove real backing-store quota/capacity as well as logical accounting; a free-space sample alone is not a quota. Sixteen worst-case 64 MiB reservations illustrate the aggregate budget, not a measured concurrency guarantee. Cleanup on success, error, cancellation and restart must release both disk and accounting before reuse. Keep files on local disposable disk; do not silently choose tmpfs, increase the 128 MiB memory cap or claim disk page cache is free. Measure cgroup memory including page cache, file I/O scheduling/event-loop impact, disk high-water and overlapping slow readers. Mount/accounting implementation and measured capacity remain review gates, not an open quota choice.

If reservation/open/write fails before dispatch, return 503 and clean up; after dispatch stop/close the selected connection and return an error only if uncommitted. Never route to AISIX solely because disk is full, discard buffered bytes, or retry CPA/AISIX. A finite disk/timeout budget cannot guarantee availability under resource exhaustion. Within accepted limits and available storage, late model adds only the time to receive/scan preceding bytes and write/read spill: it does not change the selected backend. Once model completes, begin drain without waiting for client EOF.

Stream request drain/rest with a `resty.http` body iterator; responses use bounded `body_reader` chunks and timely flush, preserving SSE/event bytes. Select a content-capable APISIX hook only after the approved prototype proves it. The old `before_proxy` relay proves cosocket feasibility, not the new downstream socket or sliding-spool mechanism.

Correctly handle Content-Length versus nginx-dechunked uploads and upstream re-framing. Strip hop-by-hop/Connection-nominated headers. Preserve end-to-end/session/request IDs; AISIX gets caller key, CPA only its service key. Never send the discovery key to inference. Preserve connect/send/read timeout semantics, cancel upstream I/O on abort, close incomplete connections and keepalive only fully consumed responses.

Ingress ceilings stay 64 MiB Responses / 16 MiB other JSON. Known over-limit Content-Length fails 413 before dispatch where possible. A chunked upload may cross its ceiling after initial forwarding: stop upload, close selected upstream, clean spool and report 413 when response commitment permits, with no replay. If the limit is crossed while still searching for a late model, fail before dispatch rather than exceeding spool/body bounds. Do not claim zero upstream bytes in the already-dispatched case.

### D3. Background exact-ID membership, bounded age

Propose refresh interval **3 s**: removes inference-path I/O, modest control-plane load (one call per worker per interval), and a short normal membership lag. This is not a measured optimal interval. Propose maximum snapshot age **15 s** (five refresh intervals), bounding temporary refresh disruption without indefinite stale routing. A refresh failure does not reset the last-success timestamp. A cold snapshot, age beyond 15 s, clock anomaly or invalid snapshot is uncertain and routes to AISIX. A still-young previous snapshot remains usable after failure until the age bound. A valid empty set is different from no snapshot: it sends known parsed IDs to CPA.

Each worker has one timer, no overlapping refreshes and no per-request refresh/wait. Authenticate `/v1/models` with a **dedicated full-visibility AISIX discovery key**, not the caller or CPA key. Bound refresh duration (proposed total 2 s), response bytes (proposed 1 MiB) and count (proposed 4,096 IDs of at most 1,024 bytes). These discovery bounds are distinct from the approved 64 KiB sliding request-memory window. Validate the complete small discovery envelope and all IDs before atomically replacing the worker's set; never publish a partial/truncated set. Keep only current and in-progress generations, releasing superseded tables.

Log refresh/uncertain warnings at WARN with a per-worker/reason interval, proposed 30 s, without body, keys or raw URLs. No per-request INFO fallback log remains: there is no fallback. Benchmark real request-path overhead; the prior 1.122/63.357/86.513 ms list percentiles belong to refresh requests, not every inference call. Ordinary lookup adds no discovery RTT, but prefix arrival and scheduling still add latency.

The set is not an authorization grant. An in-set request keeps the caller key, so AISIX ACL denial remains terminal. Sub2API entitlement stays in front of the selector, including CPA-only bare IDs. The discovery key must expose all exact definitions; restricted discovery would create false absence. No wildcard models are allowed: enforce this on local resources and the Ansible AISIX deployment validation path, including every supported definition/name field and normalized representation. Prove the concrete-name list includes direct and logical definitions; do not confuse wildcard caller ACLs with wildcard model names.

Polling is not transactional with AISIX reload: additions can briefly go to CPA, deletions can briefly return AISIX 404, and different workers can see adjacent generations. These are bounded membership-staleness tradeoffs explicitly replacing resolver-driven fallback. No health, catalog or availability filtering enters this set; Axis entries remain intentional.

### D4. Provisioning and documentation exception

Create a dedicated full-visibility AISIX caller key with private storage and a distinct identity. Update necessary current two-key assumptions (`ops/scripts/init.sh`, runtime validation of `caller-keys.json`, applicable Ansible paths/examples), Compose secret delivery and APISIX startup/config visibility. Choose the smallest supported secret injection after review; never embed a private value in tracked YAML, logs or client errors. Never reuse this key for inference. This is a coupled AISIX-resource/key plus selector activation, requiring synchronized rollback; generic image/env deployment is not sufficient.

Revise `docs/model-routing.md` explicitly: background exact membership with a bounded maximum age is now permitted. Keep `/routing-index`, catalog-driven inference, provider-registration checks and unbounded last-known-good caches prohibited. The old unconditional no-cache wording is superseded, not silently retained. README stays short; technical limits/uncertainty/refresh/migration belong in docs.

### D5. B/C remain binding

AISIX-defined slash direct models now go through AISIX with caller credentials, AISIX accounting and its CPA-pool credential hop. Preserve `session_id`; entering AISIX does not itself grant logical-route hashing. The point-in-time 53-ID inventory is:

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

Authorized CPA-only bare IDs can select CPA on a fresh/usable membership absence, while catalog membership stays unchanged. Bare CPA names can use any registering credentials, unlike prefix-qualified names. Hiding a model is not authorization. Uncertain requests intentionally go to AISIX even when CPA would know the ID.

### D6. Client ordering evidence, keeper prerequisite and Sub2API passthrough

The completed survey (`/var/tmp/ai-model-offset/report.md`, scratch evidence, not production percentiles) contains 25 SDK captures and 33 chain samples. Modern Codex/pi/Python/Vercel paths often put model first, but Node preserves caller order and older Python/LangChain/Chat paths can place history first. The surveyed pre-change keeper pin wrote expanded input first: model offsets were 81,978 / 278,586 / 1,065,018 / 4,210,746 for 64 KiB / 256 KiB / 1 MiB / 4 MiB input with 16 KiB instructions. A fixed cutoff can therefore break ordinary CPA-only requests; 64 KiB must bound memory, not search.

Keeper model-first implementation/publication/pin are delivered: feature `5a69df7`, release revision `b49ad9ac6c1eb65b1785d66ecb7249413d4c82bf`, image `ghcr.io/xz-dev/openai-api-cache-keeper:b49ad9ac6c1eb65b1785d66ecb7249413d4c82bf@sha256:eb21638bdfc65ea09c119baf62337bb8f7f7b421890eeb1e63c1815c60030872`, gateway pin `1dfc027`. It reuses its existing scanned raw span and 32 KiB copy buffer when writing its existing spool; model first, every other field in current order, native passthrough unchanged. No new full-body decode/history copy/spool/config flag. Independent provenance/CI verification and a separate image-only component activation must precede the router window; delivery/pin is not proof of production activation. Worker evidence at `/var/tmp/keeper-model-first-evidence/summary.md` reports HTTP and WS keeper→bridge offsets 1 at 1 KiB/1 MiB/4 MiB. This optimizes buffer residency only; the router must remain correct without an ordering guarantee. At 64 simultaneous prefix readers, 64 KiB windows total 4 MiB versus 64 MiB for 1 MiB windows, excluding scanner/chunks/connections and disk cache.

Passthrough investigation (`/var/tmp/ai-model-offset/passthrough.md`) at Sub2API `ab6b00e` distinguishes HTTP `extra.openai_passthrough=true` from WS `extra.openai_apikey_responses_websockets_v2_mode=passthrough`. Current relay accounts are OpenAI API-key, `force_responses`, WS off; seven of eight have nonempty account mappings. Ordinary HTTP passthrough bypasses account model replacement (`openai_gateway_forward.go:258–288`, `openai_gateway_passthrough.go:126–151`) and bypasses the mapping whitelist in account eligibility (`account.go:890–897`). Common schema/reasoning mutations still run before passthrough. Chat→Responses does not use that flag and still map-marshals GPT/Codex cache-key injection (`openai_gateway_chat_completions.go:300–312`). Thus it cannot guarantee body bytes/order and can change model/account/billing semantics. Usage accounting, error handling and retries remain active; retention stripping is not guaranteed on raw API-key Responses.

Followup2 selected-core benchmark measured 4 MiB raw 6.87 ms versus normal unmodified 6.87 ms; raw allocated about 4.20 MB versus 32 B for normal/no edit. This is not a full Forward/end-to-end speedup measurement. Account 6 has real `memory-fast`, `memory-reflect`, `memory-consolidation` → `codex/gpt-6-luna` aliases; raw skips their replacement. Account eligibility whitelist and catalog projection also change. On these findings the user decided **HTTP passthrough stays OFF**; enabling it is out of scope, not pending approval in this plan.

The old docs prohibition paired with `ctx_pool` originated from WS-mode routing incidents (`1dab95f`, front-bridge record `751b37d`; `docs/websocket-termination.md`), not analysis of the separate HTTP raw flag. WS stays off with no mode changes. No Sub2API patch or silent mapping change is authorized. Sliding-window routing handles Sub2API's legal reordering without depending on passthrough; it cannot repair a corrupt request or removed mapping.

### D7. Backend-owned retention and parser-differential cancellation

The user chose CPA config filtering, not a scanner filter. Merge v8 rules into the existing live private configuration; do not add a second legacy root `payload` spelling:

```yaml
config-version: 8
requests:
  payload:
    filter:
      - models:
          - name: "kimi-*"
        params:
          - "prompt_cache_retention"
```

`models[].name` must match the effective Kimi model/base aliases seen by CPA's payload helper; verify actual prefixed/alias paths before activation and add only narrowly scoped selectors needed for rejecting upstreams. Omit `protocol` so native Responses (`openai-response`) and converted Kimi formats are both covered. Do not silently use `name: "*"`: that would remove valid retention on supporting providers. The retained 24 h AISIX/CPA/Sub2API/inner-APISIX logs contain zero retention mentions/errors, so current rejection scope is not proven by logs. Kimi is the historical rejecting upstream; require retention-present/absent streaming and nonstreaming probes, including a production Kimi request carrying retention returning 200.

CPA v8.0.45 `internal/runtime/executor/helps/payload_helpers.go:200–218` applies `sjson.DeleteBytes`; `kimi_executor.go:440–475,578–613` invokes payload rules for native Responses, with converted paths also covered. No source patch is needed. AISIX 1.5.0 has no equivalent generic deletion config for native Responses. By user choice the gateway passes retention unchanged, with no automatic scanner fallback; do not claim generalized stripping parity on unsupported AISIX-native paths. Supporting upstreams must still receive valid retention.

Remove the selector's `strip_prompt_cache_retention` path and the catch-all rewrite hook's `dkjson` decode/strip/re-encode, including its whole-body read. Catch-all upstream/WS matching and other behavior stay unchanged. Request-byte identity is measured at the gateway-to-selected-backend boundary; CPA's targeted deletion occurs after that seam.

`ops/ansible/playbooks/deploy-config.yml` only supports `aisix`, `egress-proxy`, `cpa-model-sync`, `model-catalog`; **not `cli-proxy-api`**. Per `docs/ansible-operations.md#worker-owned-cpa-configuration`, live `data/cpa/conf/config.yaml` belongs to CPA/cpa-model-sync and is not managed by deploy/rollback baselines. Use a reviewed manual config window to back up and merge only the targeted rule into live state, preserving concurrent model-sync changes; never overwrite from a private seed or invent an unsupported deploy-config command. Record activation/readback and rollback, coordinating writers. No plugin hot reload, plugin-version-key edits or `.so` changes; if activation requires a CPA restart, review it explicitly. Any CPA image switch would require an online SQLite backup first, but no image switch is needed for these rules.

Followup2's per-hop semantics explain why backend validation alone cannot guarantee duplicate rejection:

| Hop/path | Duplicate top-level model semantics |
| --- | --- |
| Sub2API gjson handler/raw view | First exact-case value |
| Sub2API decoded Go map / Chat struct | Last; group allowlist separately checks all duplicate/case-variant candidates |
| Keeper rewritten/expanded request | Last, collapses to one; native passthrough does not guarantee this |
| Bridge WS / HTTP | WS rejects duplicate; HTTP transparent |
| Current inner cjson selector | Last, no duplicate rejection |
| AISIX Responses / Chat | Responses Value last; Chat struct rejects |
| CPA HTTP Responses/Chat handler | First; later executor/translator behavior may differ |

A second model is another key in the same object, not a mid-response model switch. Buggy raw JSON merging or deliberate parser-differential input can cause it. Source/tests show selection/execution disagreement mechanisms, not a demonstrated production authorization bypass. Keep group authorization intact. The approved single scanner detects later duplicates/malformed JSON and cancels as D2 specifies; bytes already forwarded and execution/billing cannot be recalled.

### Rejected alternatives

- **Error fallback/replay:** distinct AISIX miss signature but emitted only after complete JSON extraction; requires retaining the whole request and contradicts the user's pipe principle. Old prototype results do not revive it.
- **Per-request model-list lookup:** adds list RTT and decode on every inference request, permission/wildcard pitfalls unless constrained, and lookup/dispatch reload race. Background discovery moves that network work off the request path.
- **Unbounded last-known-good cache:** avoids a cold failure but permits arbitrarily stale absence; rejected in favor of bounded age and uncertain→AISIX.
- **Prefix overflow→AISIX:** superseded by the user's late-model direction; can break CPA-only requests solely due to field order.
- **Unbounded memory growth/full-body decode/error-replay spool:** still rejected. The newly selected bounded disk-backed sliding window is only a selection buffer and is drained once, not an error replay cache.
- **Sub2API passthrough as an ordering guarantee or generic small-field sorter:** not valid/needed. Raw passthrough changes mapping/eligibility and still normalizes some paths; arbitrary scalar strings can be huge. Model-first keeper plus order-independent router is the narrow change.

## Risks / Trade-offs

- Field position and duplicate keys: valid late models get the same selection as early models; disk latency replaces incorrect overflow routing. Continue the same scanner after dispatch and cancel on duplicate/malformed input. Return 400 before response headers, otherwise abort; execution/billing may already have begun and cannot be guaranteed undone. No ordering guarantee can be inferred from passthrough.
- Retention: targeted CPA configuration replaces both gateway removers without a scanner filter. Verify Kimi effective-model matching and production 200 before removing compatibility hooks. AISIX-native unsupported fields are not covered by this config; do not claim universal parity or silently broaden to a global filter.
- Wildcards/full visibility: list completeness depends on enforced model invariants and dedicated-key provisioning. An out-of-band AISIX edit can break that assumption; deployment validation and live audits must make drift visible.
- Snapshot freshness: three-second normal lag and fifteen-second failure age are proposed finite bounds, not resolver consistency. Cold/expired routes to AISIX and never tries CPA after its error.
- Resources: the 64 KiB sliding window bounds retained Lua request bytes, not total RSS/cgroup memory. Disk page cache, fixed scanner/chunks, simultaneous spools, slow drain and filesystem I/O must be measured. Stop for review if existing 128 MiB or approved 1 GiB disk quota cannot hold tested capacity. No memory-limit increase or unbounded buffering workaround.
- Incidental health-plugin mismatch remains uncorrelated with production; do not bundle an unreviewed health fix.

## Remaining review and evidence gates

User choices are resolved: HTTP passthrough OFF, WS unchanged/off; targeted CPA retention filtering with no gateway/scanner removal; continuous post-dispatch validation/cancellation; aggregate spool quota 1 GiB. These are not open alternatives.

1. Supervisor must review and confirm this revised plan before any new pipe prototype. Prototype acceptance precedes mechanism lock/runtime work.
2. Prove enforceable backing-store quota, atomic cross-worker accounting/admission, cleanup/restart and measured capacity within 1 GiB disk/128 MiB memory. Review mount/accounting implementation; no production p99 or concurrency guarantee is established.
3. Verify Kimi effective-model/alias matching, writer-safe CPA manual config activation/readback/rollback and a production retention-carrying 200; no unsupported `deploy-config` invocation, plugin hot reload or generalized AISIX removal claim.
4. Independently verify delivered keeper provenance/CI and perform its separate image-only activation before the router window; pin delivery is not production activation.

## Migration Plan

1. Review completed survey/followup2 and the resolved user decisions in D2/D6/D7; verify delivered keeper model-first provenance/CI. Get supervisor confirmation before prototype execution. HTTP passthrough stays OFF; WS/account model mappings stay unchanged.
2. Run task 0.1's real APISIX 3.19 single-path sliding-spool prototype: same code at model offsets 1 and 4 MiB, fixed approximately 64 KiB window plus measured fixed overhead, 1/16/64 MiB success under 128 MiB including file cache. Early model starts upstream before client EOF; late model begins drain as soon as complete, selects correct CPA/AISIX, and preserves bytes. Test 1 GiB quota/503 admission/ENOSPC, success/error/cancel/crash cleanup, framing, uncertain cases, SSE and chunked uploads. At suffix offsets 1 KiB and 4 MiB inject later duplicate model and malformed JSON; prove cancellation/cleanup with local 400 before client headers versus connection abort after response commitment, and no reroute. The 64 MiB failure is in scope to fix, not a waived baseline. Stop on failed gates; no lock from old replay evidence.
3. Tests, minimal implementation, wildcard/key validation and docs; full local validation with any Compose override temporarily moved to `/var/tmp` and always restored. Explicit-path unsigned commits, pull/rebase before push, exact-SHA CI. No production work in sections 0–3.
4. After the Lua window, activate the delivered keeper image in its separate image-only component step. Review targeted CPA private-config merge/rollback through a manual window (deploy-config has no CPA component), preserving worker-owned state and plugin keys; prove Kimi retention handling before removing gateway stripping. Then review the coupled key/config/source router activation before public-list sorting. Existing `deploy.yml -e component=apisix-models` handles guarded image/env transactions, not arbitrary tracked source installation. Preview applicable Ansible changes with `--check --diff`, review approval digests and the manual source/config window; provision discovery key and wildcard-safe definitions before relying on the snapshot. Preserve ownership/SELinux labels, no unrelated recreation.
5. Back up every changed AISIX key/resource, private env/Compose identity, selector source and CPA config identity; rollback the targeted CPA rule without overwriting unrelated concurrent model-sync updates. Recreate only reviewed components when necessary; interruption and old-key retention/cleanup order must be reviewed. Verify cold/warm/expired snapshot behavior, slash/bare paths, authorization, dedicated-key isolation, full 64 MiB and SSE, catalog byte equality, resources and source checksums.
6. Rollback restores coupled config/key/source files and recreates only reviewed components. AISIX rollback uses saved resources plus `deploy-config.yml -e component=aisix`, preview first; `rollback-config.yml` is not an AISIX rollback. Verify old slash selection, catalog and SSE. Delete temporary probe keys with DELETE-success/GET-404/file-absence proof, check drift and record sanitized evidence. Do not revoke the discovery key while a running selector still needs it.
