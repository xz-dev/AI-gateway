## Context

**Decision A is revised by the user: routing must be a streaming pipe, not a bucket.** Error-based fallback/replay is rejected. A background-refreshed AISIX membership set permits selection before the complete upload arrives. Decisions B/C remain: pure priority for AISIX-defined slash IDs, and authorized CPA-only bare IDs may reach CPA without entering the catalog.

This document is a planning update, not an implementation or prototype approval. The next prototype must not run until the supervisor confirms this plan. The request-prefix cap is a single named constant, `MAX_ROUTING_PREFIX_BYTES`, with **value pending client survey**. The user requested separate research on where real clients put `model`; do not set or justify a numeric cap before that survey. Retention removal and complete-JSON validation also have explicit user decision gates below.

### Existing routing and compatibility responsibilities

The current code was inspected, not inferred from desired behavior:

| Location | Routes | Current semantics |
| --- | --- | --- |
| `services/routing/apisix-models/routing_selector.lua`, called by two rewrite hooks in `apisix.yaml` | POST `/v1/responses`, `/v1/chat/completions`, `/v1/images/generations`, regardless of Content-Type | `ngx.req.read_body()` collects the complete upload; read memory or entire bounded body file; cjson decodes the complete object. Reject malformed JSON, scalar/null/array, absent/empty/non-string `model`, model over 1,024 bytes and over-limit standalone reader with local 400 before forwarding. Nonstandard numeric values are disabled. |
| Same selector, `strip_prompt_cache_retention` | Same three classified routes | Remove the top-level field whenever present, including JSON null; encode the complete remaining object once and replace the request body. Nested fields remain; arrays preserve their type. If absent, original body bytes remain unchanged. Model value is not rewritten. |
| `services/routing/apisix-models/apisix.yaml`, catch-all rewrite hook | Otherwise matched requests, including `/v1/messages`; only Content-Type containing the case-sensitive text `json` | Read the complete body, but process only an in-memory nonempty body. `dkjson` decodes a table; malformed/non-table/file-backed bodies are left alone, not locally rejected. If a top-level retention field exists, remove it and re-encode. No model validation. |
| Separate images-edits/catalog/WS routes | Multipart POST `/v1/images/edits`; GET `/v1/models`; direct WS upgrades | No classified JSON rewrite. Edits go to AISIX; catalog to model-catalog; WS frames pass to CPA without inspection. Front-door APISIX has no `prompt_cache_retention` remover. |
| nginx/global and route `client-control` | Responses: 64 MiB; other routes: 16 MiB | Existing size ceilings. With the current whole-body reader, normal oversized requests can be rejected before forwarding. The pipe cannot promise that for an upload whose length is initially unknown. |

The new pipe removes whole-body cjson decoding on the **classified** routes. Non-classified catch-all compatibility remains unchanged in this change; do not silently expand this into a gateway-wide streaming rewrite. Sub2API authorization, catalog membership/order/priority and the WS/keeper chain remain unchanged.

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

Goals: bounded-memory request and response pipes, backend choice from one ordered list, no full-body model decoder/replay, no inference-path discovery request, full 64 MiB support under 128 MiB, explicit uncertainty/refresh/compatibility semantics.

Non-goals: health-based failover, provider registration lookup, catalog membership changes, extending wildcard matching, Sub2API patches, revisiting Axis, rewriting unclassified catch-all processing, overlapping production windows, dependency updates or deferred unrelated work.

## Decisions

### D1. One backend, no retry

Use one ordered Lua list: AISIX (upstream, caller credential, exact snapshot membership source), then CPA (upstream, service credential, terminal/no membership source). Slash characters have no special meaning. A future backend adds an entry with a bounded membership source rather than per-model naming branches.

If a usable snapshot contains the exact model, choose AISIX. If it does not, choose terminal CPA. If routing is uncertain, choose the first priority AISIX directly. Inference always uses exactly one backend. Never inspect its error to select another; 404, `upstream_error`, ACL/auth failure, 429, 5xx, transport failure and mid-stream failure are terminal. Missing CPA key fails closed with local 503 only when CPA would be selected, never by substituting the discovery key.

### D2. Bounded prefix and request pipe

Before any `ngx.req.read_body`, use `ngx.req.socket()` and a fixed-size chunk reader (initial candidate 8 KiB). A bounded lexical scanner tracks only the root object's fields, JSON nesting and string escapes; it retains no complete document or large field value. Keep `MAX_MODEL_BYTES=1024`; propose scanner maximum nesting 128, exceeding which is uncertain, not permission to allocate more. `MAX_ROUTING_PREFIX_BYTES` is the **single named request-prefix cap, value pending client survey**. Do not pick 1 MiB or duplicate numeric cap literals before that evidence is reviewed.

Stop accumulating at a usable model decision or the prefix cap. Scan all bytes actually retained before commitment so a duplicate top-level model in that prefix is uncertain. A model token cut by the cap, invalid/null/array prefix, missing/invalid model or malformed prefix is uncertain. With no complete trusted model by the cap, send the exact retained prefix and rest to AISIX rather than growing the buffer, rejecting solely for late field position, or switching to the slash rule. A key after this retained prefix cannot be used to revise routing.

The scanner cannot detect an arbitrary later duplicate `model`, malformed suffix or absent end delimiter before forwarding an early prefix. That limitation is intrinsic to the pipe, not an excuse to buffer the full request. Backend-owned whole-document validation and the effect of later duplicates require the explicit compatibility decision in Open Questions.

Feed retained prefix once, release consumed storage, then read and forward remaining chunks with backpressure via the `resty.http` request-body iterator. Stream responses with bounded `body_reader` chunks and timely flush, preserving SSE/event bytes. Select a content-capable APISIX hook only after the approved prototype proves it; ensure no earlier plugin or nginx path has already read the entire body. The previous `before_proxy` relay is evidence of cosocket feasibility, not proof of the new downstream socket hook.

Correctly handle Content-Length versus nginx-dechunked client uploads and re-framing to upstream. Strip hop-by-hop and Connection-nominated headers. Preserve end-to-end/session/request IDs; AISIX gets the caller key, CPA only its service key. Never send the discovery key to either inference endpoint. Retain connect/send/read semantics, cancel upstream I/O on client abort, close incomplete connections and keepalive only fully consumed responses. Do not queue/spool complete bodies or hold successful responses wholesale.

Existing ingress ceilings remain 64 MiB Responses / 16 MiB other JSON. A known over-limit Content-Length can fail 413 before dispatch; an unknown-length/chunked request may cross its ceiling **after a prefix was sent**. Stop the upload and close that selected upstream without replay, report 413 when response commitment permits, and do not claim zero upstream bytes on that path. No silent limit increase is permitted.

### D3. Background exact-ID membership, bounded age

Propose refresh interval **3 s**: removes inference-path I/O, modest control-plane load (one call per worker per interval), and a short normal membership lag. This is not a measured optimal interval. Propose maximum snapshot age **15 s** (five refresh intervals), bounding temporary refresh disruption without indefinite stale routing. A refresh failure does not reset the last-success timestamp. A cold snapshot, age beyond 15 s, clock anomaly or invalid snapshot is uncertain and routes to AISIX. A still-young previous snapshot remains usable after failure until the age bound. A valid empty set is different from no snapshot: it sends known parsed IDs to CPA.

Each worker has one timer, no overlapping refreshes and no per-request refresh/wait. Authenticate `/v1/models` with a **dedicated full-visibility AISIX discovery key**, not the caller or CPA key. Bound refresh duration (proposed total 2 s), response bytes (proposed 1 MiB) and count (proposed 4,096 IDs of at most 1,024 bytes). These discovery bounds are distinct from the unresolved request-prefix cap. Validate the complete small discovery envelope and all IDs before atomically replacing the worker's set; never publish a partial/truncated set. Keep only current and in-progress generations, releasing superseded tables.

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

### Rejected alternatives

- **Error fallback/replay:** distinct AISIX miss signature but emitted only after complete JSON extraction; requires retaining the whole request and contradicts the user's pipe principle. Old prototype results do not revive it.
- **Per-request model-list lookup:** adds list RTT and decode on every inference request, permission/wildcard pitfalls unless constrained, and lookup/dispatch reload race. Background discovery moves that network work off the request path.
- **Unbounded last-known-good cache:** avoids a cold failure but permits arbitrarily stale absence; rejected in favor of bounded age and uncertain→AISIX.
- **Grow/spool until a late model arrives:** reintroduces the bucket; rejected. Header-only/model-first client requirements are not selected substitutes for the prefix survey.

## Risks / Trade-offs

- Prefix position and duplicate keys: arbitrary JSON field order is supported by uncertain→AISIX, not by accumulating an entire document. An early model followed by a later duplicate may not determine backend semantics consistently; user must accept/delegate validation or choose another contract.
- Retention stripping: keeping the old full-object rewrite would defeat the pipe. Streaming filtering changes framing/byte serialization; backend-owned removal requires proven backend support. Neither is silently selected.
- Wildcards/full visibility: list completeness depends on enforced model invariants and dedicated-key provisioning. An out-of-band AISIX edit can break that assumption; deployment validation and live audits must make drift visible.
- Snapshot freshness: three-second normal lag and fifteen-second failure age are proposed finite bounds, not resolver consistency. Cold/expired routes to AISIX and never tries CPA after its error.
- Resources: per-request prefix/chunk bounds do not alone prove whole-container concurrency safety. Measure simultaneous prefix readers and slow streams; stop for review if existing 128 MiB cannot hold the tested capacity. No memory-limit increase or buffering workaround.
- Incidental health-plugin mismatch remains uncorrelated with production; do not bundle an unreviewed health fix.

## Open Questions — blocking user/review decisions

1. **Request-prefix cap:** one `MAX_ROUTING_PREFIX_BYTES`, **value pending the separately requested client survey**. Collect actual model offsets from representative request formats without recording private prompts. Review coverage and failure cases before setting the constant. No 1 MiB default is authorized by this document.
2. **`prompt_cache_retention` compatibility:** choose explicitly between a bounded streaming top-level key filter and backend-owned stripping. A filter removes only that root member/value and the necessary separator, preserves all other bytes (unlike today's reserialization), and streams arbitrarily large values without retaining them; changed length requires upstream chunked framing or another proven mechanism. Moving removal to AISIX/CPA preserves gateway byte identity but is safe only if existing backend behavior/config can implement it without rejected-upstream failures; no backend patch is implicitly authorized. Recommend the filter only if equivalent removal and framing pass the prototype; prefer native backend handling if verified. This remains a user choice, not an implementation default.
3. **Validation semantics:** confirm replacing gateway whole-document/model validation with bounded prefix classification and backend-owned complete validation. Malformed-prefix→AISIX is already binding. Malformed suffixes and later duplicate models cannot be pre-dispatch locally rejected by a pipe. This changes today's local-400/no-forward guarantee and requires explicit acknowledgement. Do not retain full decoding or invent a new rejection rule to conceal this difference.
4. Supervisor must confirm this revised plan before the new pipe prototype runs; compatibility decisions and cap must be resolved before mechanism lock/implementation.

## Migration Plan

1. Complete the client survey and explicit compatibility decisions; synchronize proposal/specs/tasks. Get supervisor confirmation before prototype execution.
2. Run task 0.1's real APISIX 3.19 pipe prototype: flat memory across 1/16/64 MiB under 128 MiB; first upstream byte for an early model before client EOF and independent of total body size; byte/framing fidelity; uncertain cases; SSE, cancellation and chunked uploads. The 64 MiB failure is now in scope to fix, not a waived baseline. Stop on failed gates; no mechanism lock based on the old replay prototype.
3. Tests, minimal implementation, wildcard/key validation and docs; full local validation with any Compose override temporarily moved to `/var/tmp` and always restored. Explicit-path unsigned commits, pull/rebase before push, exact-SHA CI. No production work in sections 0–3.
4. Review one coupled key/config/source activation after the Lua production window and before public-list sorting. Existing `deploy.yml -e component=apisix-models` handles guarded image/env transactions, not arbitrary tracked source installation. Preview applicable Ansible changes with `--check --diff`, review approval digests and the manual source/config window; provision discovery key and wildcard-safe definitions before relying on the snapshot. Preserve ownership/SELinux labels, no unrelated recreation.
5. Back up every changed AISIX key/resource, private env/Compose identity and selector source file. Recreate only reviewed components when necessary; interruption and old-key retention/cleanup order must be reviewed. Verify cold/warm/expired snapshot behavior, slash/bare paths, authorization, dedicated-key isolation, full 64 MiB and SSE, catalog byte equality, resources and source checksums.
6. Rollback restores coupled config/key/source files and recreates only reviewed components. AISIX rollback uses saved resources plus `deploy-config.yml -e component=aisix`, preview first; `rollback-config.yml` is not an AISIX rollback. Verify old slash selection, catalog and SSE. Delete temporary probe keys with DELETE-success/GET-404/file-absence proof, check drift and record sanitized evidence. Do not revoke the discovery key while a running selector still needs it.
