## Context

The public chain is: public APISIX → keepalive → keeper → responses-ws-sse-bridge → Sub2API → (account `base_url http://cli-proxy-api:8317/v1`) `sub2api-cpa-relay` → `apisix-models` → AISIX or CPA. Today `apisix-models` reads and decodes every classified JSON body in Lua and applies the slash rule: an ID with `/` goes to CPA with `CPA_API_KEY`; otherwise it goes to AISIX with the caller key. The user asked for priority routing instead (AISIX first, otherwise CPA) without per-component special configuration, and for a pipe, not a bucket.

### Evidence: AISIX resolves priority natively

The source is identical at tags v1.5.0 and v1.6.0:
- `crates/aisix-proxy/src/model_resolve.rs`: exact names always win; among wildcards, the most specific (longest literal) pattern wins. A wildcard match clones the row with `model_name` resolved to the requested ID. Attribution stays on the wildcard row, and the client sees the name it requested.
- `crates/aisix-core/src/wildcard.rs`: a bare `*` matches any candidate.
- Every model-bearing handler uses `resolve_model`, including `/v1/images/generations` and `/v1/images/edits` (multipart).

### Evidence: local tests (scratch in `/var/tmp/ai-aisix-star/`)

The tests used the production-shaped 77-row resources (secrets replaced) plus one `*` row, a CPA stub, the production AISIX limits (192m, 256m memswap) and one CPU. Results were the same on AISIX 1.6.0 (production) and 1.5.0 (old tracked pin):

| Check | Result |
| --- | --- |
| AISIX loads `*` → `cpa-pool`, `model_name: "*"` | pass |
| Repo policy validator | rejects `*` without the shared cooldown block; passes with it |
| Normalizer and orphan report | normalized 0; 23 routes; 0 orphans |
| `claude-opus-5-5` (logical route) | upstream `xl/claude-opus-5-5`, not `*` |
| `codex/gpt-6-luna` (direct row) | exact row, not `*` |
| A more specific `devin/*` row | beats `*` |
| CPA-only bare and slash IDs | reach CPA with the ID unchanged |
| Body fields (Responses, rich fields incl. `prompt_cache_retention`) | none dropped, added or changed; Chat adds `stream:false`; AISIX re-serializes, so bodies are not byte-identical |
| SSE, Responses and Chat, `*` and route | incremental, events at 0.3 s steps |
| Images generations and multipart edits: route, direct, `*` | pass; edits stay multipart and the model is mapped |
| Unknown everywhere, Responses | 400 `{"error":{"message":"<CPA error JSON as a string>","type":"upstream_error"}}` |
| Unknown everywhere, Chat | 400 `{"error":{"message":"unknown provider for model X","type":"upstream_error","param":"model","code":"model_not_found"}}` |
| Today: unknown bare via AISIX / slash via CPA | 404 `model_not_found` / 400 `invalid_request_error`, `code=model_not_found` |
| Cooldown on `*` after a 429 | next request through `*` or a direct row still goes upstream; cooldown filters only routing-group targets |
| Added loopback latency | p50 0.22 ms vs 0.23 ms direct; p95 0.37 vs 0.42 ms |
| Attribution | access log keeps the requested ID; Prometheus series carry `model="*"` |
| AISIX `/v1/models` | 76 IDs, no `*`; model-catalog membership is unaffected |
| Caller keys | production `sub2api` and `model-catalog` keys already allow `["*"]` |
| 64 MiB Responses at production limit | **OOM kill**; 16 MiB already reaches the 192 MiB limit |

Memory scaling, 1.6.0, fresh container, 1 GiB limit (peak `memory.peak`): idle 147.6 MB; 4 MiB 177.5 MB; 16 MiB 232.1 MB; 64 MiB 457.3 MB. AISIX holds about 4.6–7× the body. Production AISIX before the change: anon 84 MB plus swap 67 MB (its 64 MiB swap is full), `memory.peak` 195 MB of 201 MB. The host has 2 vCPU and 1,962 MiB RAM, about 829 MiB available.

### Evidence: production read-only

- `apisix-models` 37 h log (since its last start): only `POST /v1/responses`, `GET /v1/models`, one `POST /v1/images/generations` and `GET /healthz`. Zero catch-all, WebSocket, compact, `/v1/responses/{id}` or embeddings requests.
- Sub2API, last 7 days, through the 8 accounts on this hop (1,4,5,6,9,10,11,12): `/v1/responses` 54.6k, `/v1/chat/completions` (sent upstream as Responses) 49.3k, images 1. Embeddings and rerank use account 3, which has its own URL. All 8 account API keys hash to the AISIX `sub2api` caller key.
- Image traffic, last 30 days: generations for `gpt-image-1.5/2/2.5/2.5-flare` and `grok-imagine-image(/2.0)`, plus edits for `gpt-image-2`. All are bare AISIX routes today and are unchanged.
- Sub2API reads `GET /v1/models` from the account base URL: the Codex manifest with `client_version` (46 in 37 h) and the plain list (82 in 37 h). Both must keep reaching model-catalog. AISIX's typed `/v1/models` returns its own 76-ID list, and its passthrough routes cannot shadow `/v1/*`.
- The front-door catalog leg reads `apisix-models` over `front-catalog` (51 in 37 h). The `127.0.0.1:9081` host relay had 2 operator calls, and no runbook in docs or ops uses it.
- AISIX reaches CPA through its own `aisix-cpa-relay` and does not depend on `apisix-models-cpa-relay`.
- Today the `apisix-models` `/healthz` is not local. The pinned APISIX 3.19.0 image rejects the `local-health` route (`unknown plugin [request-termination]`), so `/healthz` falls through the catch-all and CPA answers it with 200. The Compose healthcheck and the recovery check pass only because CPA is up. With the catch-all now pointing at AISIX, which serves no `/healthz` on its proxy listener, the same config would turn `apisix-models` unhealthy. That is why D2 replaces the plugin. The front `apisix.yaml` uses only plugins present in 3.19.
- AISIX request bodies in 16 h: Responses p50 0.82 MB, p99 3.38 MB, max 3.95 MB.
- Production AISIX runs `ghcr.io/api7/aisix:1.6.0@sha256:26eae08b5c94291050f179d3c0a56ca154f40751f5c755b5922b044efc523690` from the private `.env`; tracked Compose and `.env.example` still pin 1.5.0.

## Goals / Non-Goals

**Goals**
- AISIX-first priority for every ID: exact route or direct row, then a more specific glob, then `*` → CPA with the ID unchanged. Decisions B and C hold.
- `apisix-models` becomes a constant pipe: no body read, no Lua, no CPA upstream, no CPA credential, no network path to CPA.
- Codex manifest, plain model list, public list intersection and catalog membership stay byte-identical.
- Image generation and multipart edits keep working.
- AISIX admits one 64 MiB Responses body.
- The tracked AISIX pin matches production.

**Non-goals**
- Sub2API changes: passthrough stays OFF; account URLs, WS mode, mappings and code are unchanged.
- AISIX, CPA or keeper source changes. CPA only gets a config rule.
- Deleting `apisix-models`: Sub2API's `/v1/models` contract needs it.
- Making two concurrent 64 MiB requests safe, and AISIX `request_body_limit_bytes` (the 64m `client_max_body_size` in `apisix-models` stays the backstop).
- Disabling nginx request buffering in `apisix-models`. It buffers to disk up to 64m, and AISIX reads the whole body anyway.
- The public sshd key-exchange closure (see Risks).

## Decisions

### D1. AISIX `*` is the only fallback

The private `resources.yaml` gets exactly one row:

```yaml
- display_name: "*"
  provider: openai
  model_name: "*"
  provider_key: cpa-pool
  cooldown: <the shared direct-model cooldown block, byte-equal>
```

The cooldown block is required by the existing direct-model policy and is harmless here, as the tests show. `check-aisix-resource-policy.py` additionally requires:
- exactly one `*` row with `model_name: "*"` and the CPA pool provider key;
- that no routing target names `*`.

Other single-`*` globs stay legal, because their precedence is a feature. `resources.example.yaml` documents the row. The orphan report is unaffected: the adopted list is empty, and `*` is a standalone row by design.

### D2. Slim `apisix-models`

`apisix.yaml` has three upstreams/routes and no `global_rules`:

| Route | Match | Target |
| --- | --- | --- |
| `local-health` | `GET, HEAD /healthz`, priority 1000 | `fault-injection` `abort` 200, body `{"status":"ok"}` |
| `codex-models` | `GET /v1/models`, priority 20 | model-catalog |
| `aisix` | `/*`, priority 1, `enable_websocket: true`, `proxy-buffering.disable_proxy_buffering: true` | AISIX, caller credential unchanged |

The user's "two constant routes" are the two non-health routes; `/healthz` stays local as instructed. The pinned APISIX 3.19.0 image has no `request-termination` plugin (the route is rejected with `unknown plugin [request-termination]`), so `fault-injection` `abort`, which is enabled by default, answers it. No Lua is involved, and it returns 200 with every upstream down.
- No `serverless-*` function, no `client-control`, and no `cpa` upstream.
- `config.yaml` keeps `client_max_body_size: 64m` and drops `extra_lua_path`.
- `start.sh` copies the template unchanged, with no `CPA_API_KEY` check or `sed`.
- `routing_selector.lua` and `routing_selector_test.lua` are deleted.
- The front door still enforces 16 MiB on client bodies. Only keeper's expanded Responses history can exceed it, up to the inner 64m backstop.

Direct WebSocket upgrades now reach AISIX, which has no `GET /v1/responses`. That path already failed for bare IDs, and none were seen in 37 h; the front bridge remains the supported WS path.

### D3. Network cut

Remove from `compose.yaml`:
- services `apisix-models-cpa-relay`, `apisix-models-host-netns` and `apisix-models-host-relay`;
- networks `apisix-models-cpa-source`, `apisix-models-cpa-target`, `apisix-models-host-source` and `host-apisix-models-target`;
- in `apisix-models-netns`, the `cpa` extra host and both removed network attachments;
- in `apisix-models`, `CPA_API_KEY` and the dependencies on the CPA and host relays;
- in `cpa-netns`, the `apisix-models-cpa-target` attachment.

The `config.yaml` listeners drop `172.30.34.2`.

Kept edges, all `apisix-models-netns`: `sub2api-cpa-target` (inbound from Sub2API), `front-catalog` (inbound from the front door), `apisix-models-catalog-source` and `apisix-models-aisix-source` (outbound).

`validate.sh` adds a reachability check. It walks networks and socat relays from `apisix-models-netns`, does not traverse an application namespace such as `aisix-netns` or `model-catalog-netns`, and fails if `cpa-netns` is reachable. It also asserts:
- no `cpa` extra host;
- no `8317` and no `cpa:` node in `apisix.yaml`;
- no `CPA_API_KEY` in the service environment.

An intentional-break fixture that re-adds the relay must fail.

### D4. AISIX memory and pin

- `mem_limit: 512m` and `memswap_limit: 512m` (swap removed).
- Why no swap: production already fills its 64 MiB of swap. On a 2 GiB host, swap thrash degrades every service, while a deterministic OOM makes Docker restart AISIX.
- 512m covers idle (about 150 MB) plus one 64 MiB request (457 MB peak measured).
- Tracked `compose.yaml` and `.env.example` pin `ghcr.io/api7/aisix:1.6.0@sha256:26eae08b5c94291050f179d3c0a56ca154f40751f5c755b5922b044efc523690`, the digest production runs and on which `*` was validated.

### D5. Retention moves to CPA

Both gateway strips disappear with the selector and the catch-all. All traffic now passes AISIX to CPA with `prompt_cache_retention` intact (verified: not dropped). CPA `requests.payload.filter[].params` removes the root field for the upstreams that reject it, initially Kimi. The filter must match the names CPA receives (`kimi-coding/kimi-k3`, `sensenova/kimi-k3`, `devin/kimi-k2-7`, CPA-only bare Kimi IDs); confirm the matcher syntax before the window.

`deploy-config.yml` does not manage CPA, so this is a reviewed manual live-config step:
- no plugin hot reload and no plugin version-key change;
- before the window, confirm whether CPA applies `requests.payload` by config reload without touching plugins; otherwise schedule a restart with an online SQLite backup.

### D6. Recovery

`gateway-recovery.py` keeps the `apisix-models` `/healthz` check. Its CPA `/v1/models` probe takes the key from the model-catalog container's `CPA_CLIENT_KEY` (Compose defaults it to `CPA_API_KEY`), not from `apisix-models`. The key stays in memory only.

### D7. Ansible

`apisix-models` stays a component (`deploy.yml`/`rollback.yml` image changes). No removed service appears in a component profile, argument spec or the model-catalog protected-identity list (`model-catalog-netns`, `catalog-cpa-relay`, `apisix-models-catalog-relay`, `apisix-models` stay). Compose structure changes are outside the component gate, hence the manual window.

## Rejected alternatives

- **Error fallback with replay.** AISIX's handlers use Axum `Json` extractors (`responses.rs:189`, `chat.rs:101`, `images.rs:73` at v1.5.0), so its 404 `model_not_found` only arrives after the whole body has been read. A retry needs the full request kept for replay, which is the bucket the user rejected. A replay prototype at the 128 MiB cap also reproduced the existing selector OOM: 19 MiB passed, 20/24/32/64 MiB OOM-killed before dispatch.
- **Per-request or background list lookup.** AISIX `/v1/models` is filtered by the caller ACL and omits wildcard rows (`models.rs:55–105`). A list call per request costs p50 1.1 ms / p95 63 ms on a reused connection. A background snapshot needs a dedicated discovery key, an age policy and wildcard bans. AISIX's own resolver makes all of that unnecessary.
- **Lua sliding-window pipe in `apisix-models`.** This was a 64 KiB window with disk overflow, an incremental scanner and a 1 GiB quota. The prototype passed 64 MiB near-EOF bodies under 128 MiB, later duplicate or malformed cancellation at 1 KiB and 4 MiB, concurrency, and 503 at the quota. But APISIX 3.19 `ngx.req.socket()` refuses chunked request bodies (`chunked request bodies not supported yet`), so it needed a 411 rule or a hand-written framing decoder, with a request-smuggling risk. It is a large custom streaming proxy for a decision AISIX already makes, and AISIX reads the full body anyway.
- **Delete `apisix-models` entirely.** Sub2API takes the Codex manifest and the plain list from its inference base URL. AISIX would answer with its own 76-ID list, breaking the manifests and shrinking the public intersection. Pinning group manifests to one account is database state and does not cover the plain list. An AISIX Host-matched passthrough would be a side door with a loop risk.
- **Lower the 64 MiB contract or set AISIX `request_body_limit_bytes`.** Not chosen: the user kept the contract and raised memory instead.
- **Keep the slash rule and only drop the CPA network edge.** Not possible: slash IDs need a CPA path.

## Risks / Trade-offs

- **Concurrent large bodies.** One 64 MiB Responses request peaks at about 460 MB in AISIX. Two concurrent 64 MiB requests (about 770 MB) exceed 512m, and AISIX is OOM-killed and restarted; every in-flight AISIX request fails. The host's roughly 829 MiB of available memory is also under pressure. Observed bodies are far smaller (max 3.95 MB in 16 h; slash→CPA input p99 190k tokens). The record must state this residual risk.
- **Liveness semantics change.** Today the `apisix-models` health effectively probes CPA, through the catch-all. After the change it is purely local, and CPA and AISIX health come from their own checks. The window must prove `/healthz` returns 200 with no upstream hit.
- **Every CPA-bound request now crosses AISIX.** AISIX restarts or OOMs affect all IDs, not only bare ones. The added hop costs about 0 ms on loopback, plus AISIX's full-body read.
- **Error shape for unknown-everywhere IDs** changes to 400 `upstream_error` (Responses message holds the CPA JSON as a string). The cause stays actionable ("unknown provider for model X"). Verify that Sub2API does not penalize or disable accounts on this 400.
- **Observability.** AISIX metrics for CPA-only traffic collapse to `model="*"`; per-ID detail stays in the access log. `*` may show as cooling on the status page after any unknown-ID 400 or 429; this is cosmetic, as routing is unaffected.
- **Body fidelity.** AISIX re-serializes JSON. Fields are preserved, but bodies are not byte-identical (Chat gains `stream:false`). This was already true for bare IDs.
- **Untested paths through `*`.** `/v1/embeddings`, `/v1/messages` (AISIX converts Anthropic shape for an `openai` provider) and AISIX `/v1/realtime` had 0 requests on this hop in 37 h. They are not acceptance-tested here.
- **Retention.** If the CPA filter misses a rejecting upstream, that upstream returns 400 instead of the gateway silently stripping the field. Kimi is the only known case.
- **CPA namespace recreate** for the network detach restarts CPA. Take an online SQLite backup first and do not hot-reload plugins.
- **Out-of-scope finding, not acted on:** during research, the production host's public SSH address closed the connection during key exchange (`kex_exchange_identification: Connection closed by remote host`). The Tailscale path presented the same host key and worked.

## Migration Plan

All repository changes, CI and local tests land first. Production then changes in ONE reviewed manual window, because Compose structure changes are outside the component gate. Steps run in order, and each step has its own check and rollback:

1. **Backups and baselines.** Back up:
   - private Compose and `.env`;
   - `services/routing/apisix-models/`;
   - AISIX `resources.yaml` and `config.yaml`;
   - CPA `config.yaml`;
   - the recovery script and unit;
   - an online CPA SQLite backup.

   Capture:
   - container identities, images and restart counts;
   - the five catalog bodies (internal standard and Codex, public standard and Codex, Sub2API standard) with SHA-256;
   - read-only Sub2API account rows;
   - AISIX metrics and the probe baseline.
2. **CPA retention filter.** Apply the reviewed `requests.payload.filter` rule using the reload method proven before the window. Verify that a Kimi request with `prompt_cache_retention`, streaming and non-streaming, sent straight to CPA (the inner namespace can still reach it at this step) returns 200, and that plugins and version keys are unchanged. Rollback: restore `config.yaml` the same way.
3. **AISIX.** Edit the private Compose AISIX block (512m, no swap, pin) and add the `*` row. Run `deploy-config.yml -e component=aisix --check --diff`, review it, then apply; this force-recreates AISIX. Verify: AISIX healthy; `/v1/models` has no `*`; a CPA-only bare ID through the public chain now succeeds through `*`. Rollback: restore `resources.yaml` and Compose, then run deploy-config again.
4. **Slim `apisix-models`.** Install the new `apisix.yaml`, `config.yaml` and `start.sh`, remove `routing_selector.lua`, and apply the Compose removals. Stop and remove `apisix-models-cpa-relay`, `apisix-models-host-relay` and `apisix-models-host-netns`. Recreate `apisix-models-netns` and `apisix-models` only (no `--remove-orphans`). Verify health, inference through AISIX, the Codex manifest via Sub2API, and catalog `cmp`. Rollback: restore the files and Compose, then `up -d` the old services.
5. **CPA namespace detach.** Recreate `cpa-netns` and the services that share it, with the step-1 SQLite backup current. Then remove the four obsolete networks. Verify CPA healthy and plugins loaded. From `apisix-models-netns`, a TCP connect to every former CPA address and to `cpa:8317` fails. Rollback: restore Compose and recreate with the old networks.
6. **Recovery.** Deploy the updated `gateway-recovery.py` with the recovery playbook (`--check --diff`, then apply). Verify that a timer run reports every check healthy, using the model-catalog key.
7. **Post-checks.** Run the full list in tasks section 5. Delete every temporary key with proof. Write the record.

If a step's check fails, roll back that step and every earlier step in reverse order, verify the baseline again, and stop the window.
