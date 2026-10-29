# Responses path: Pi Codex transports and cache keeper

Pi Codex SSE/WebSocket configuration and the standard Responses cache path through openai-api-cache-keeper.

## Pi Codex SSE and WebSocket transports

Pi's built-in `openai-codex` provider parses its `apiKey` as a JWT to obtain the ChatGPT account ID, while Sub2API expects its own opaque API key. Keep those identities separate: give Pi a JWT-shaped, non-credential parse token through private environment `PI_CODEX_PARSE_TOKEN`, and send the real Sub2API credential only as `x-api-key`. Do not reuse a live OpenAI OAuth access token as the parse token.

Override the built-in provider in the user's private `~/.pi/agent/models.json`:

```json
{
  "providers": {
    "openai-codex": {
      "baseUrl": "${AI_GATEWAY_PUBLIC_URL}/backend-api",
      "apiKey": "$PI_CODEX_PARSE_TOKEN",
      "headers": {
        "x-api-key": "$AI_GATEWAY_API_KEY",
        "x-ai-gateway-auth": "pi-x-api-key"
      }
    }
  }
}
```

The higher-priority APISIX Codex Responses route matches only that marker plus a nonempty `x-api-key`. It removes the parse-only `Authorization` and marker before proxying; it neither stores nor validates the credential. Sub2API remains the sole API-key authority and authenticates `x-api-key`. Requests missing either header fail closed through the normal opaque public response policy. The same `/backend-api/codex/responses` path handles POST/SSE and GET/WebSocket.

Sub2API account WS mode is `off`: public WebSocket ends at the front keeper and stateless bridge, not Sub2API. Pi `transport: "websocket-cached"` sends a parent plus new input delta; keeper expands cached parents before the bridge converts each turn to HTTP/SSE. `transport: "websocket"` sends full context; `transport: "sse"` remains HTTP streaming. Set keeper `IGNORE_CLIENT_STORE_FALSE=true` for Codex. Installed-Pi cached continuation across a forced reconnect passed on 2026-10-23. Native upstream WS caching is not equivalent to this replay; read [the topology decision and limits](websocket-termination.md). Same-generation replay remains a separate documented background Responses contract.

## Standard Responses cache path

```text
APISIX or restricted Operator -> keepalive -> openai-api-cache-keeper
  -> responses-ws-sse-bridge -> Sub2API (HTTP/SSE) -> apisix-models pipe -> AISIX (* -> CPA)
```

Keeper uses standard Responses paths only; no `/rk` alias, prefix stripping or private recovery header exists. Ordinary/unverified APIs, upstream errors and non-SSE responses remain transparent. Keeper does not select accounts/models or implement billing.

- **Replay.** A supported `background: true` Responses stream has one upstream generation. Standard `GET /v1/responses/{id}?stream=true&starting_after=N` replays complete retained events, then follows the same generation. Cancel uses the standard cancel path. Missing, expired or failed task state is an explicit error, never hidden regeneration.
- **Ownership.** Cached background reads and cancel require a credential that `/v1/sub2api/billing` accepts as valid. In single-upstream mode there is no per-key task isolation; the response ID acts as the capability. Caller credentials remain intact through the chain.
- **Checkpoint.** Keeper-owned history uses a budgeted disk spool and bbolt volume `openai-api-cache-keeper-cache` at `/var/tmp/openai-api-cache-keeper`. Native HTTP/WS turns also record supported history: known parents expand locally; unknown parents pass through unchanged and are not recorded. The bridge owns no history. Restart invalidates disposable task/checkpoint state.
- **Limits.** Keeper uses 384 MiB, 0.5 CPU, 64 PID and no swap. Repository defaults are TTL `10m`, `100` response-ID aliases, and `STORE_MAX_BYTES=0` (no live-payload cap, not unlimited metadata/pins/compaction or per-request budgets). Access LRU stays unchanged; TTL starts at write/replacement, not reads. After the bounded `24h / 10000 / 0` experiment, production now uses `24h / 10000 / 1073741824` (1 GiB live-payload cap), per the user's 2026-10-24 decision; see the [rollout record](records/deployment-batch-20261023.md#keeper--1-gib-cap-deployed-2026-10-24). Restart still wipes history. Keepalive retains 128 MiB, with `GOMEMLIMIT=96MiB` and an 8 MiB inspection limit. Larger bodies pass through without keepalive heartbeats. Expanded internal Responses history is bounded at 64 MiB; other inner routes retain 16 MiB.
- **Operator.** Reusable Compose defines a loopback-only `:8087` entrance to the same keepalive namespace, bypassing APISIX filters but not authentication. A specific Tailscale binding needs an explicit ACL. The urgent production cutover deferred this new entrance to avoid recreating namespaces; that is not two-entrance acceptance.

Pin keeper, bridge and keepalive GHCR images by commit and digest. Production uses explicit registry pulls and `--no-build --pull never`, not an old host submodule build. The 2026-10-23 front-bridge rollback restores account WS modes before reverting keeper; its private CAS/backup/readback receipt is `/var/tmp/ws-front-deploy-20261023/REPORT.md`. Do not apply a pre-bridge deployment snapshot without reconciling the new service and volume. Snapshot the entire old Compose, private override/environment, both APISIX configurations and old image identities before switching. Stop the old inner keeper; never run two history owners on the request path or share their volumes. Removing a component also removes its WebSocket handling and path aliases: migrate Sub2API account `base_url` and WebSocket mode in the same rollout ([checklist](model-routing.md#checklist-before-pushing-or-deploying)). Preserve unrelated production service settings. The legacy Ansible `deploy-response-keeper`, `deploy-responses-path` and `restart-response-keeper` tags, and the retired scoped `deploy-standard-cache-path`, `restart-openai-api-cache-keeper` and `rollback-standard-cache-path` tags, fail during preflight. Keeper and bridge use guarded single-service `deploy`/`rollback` profiles with a fresh approved plan digest; see [Keeper and ws-sse bridge rollout](ansible-operations.md#guarded-component-rollout). These profiles do not switch topology or Sub2API account modes.
