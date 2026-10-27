# AISIX, CPA and Sub2API configuration

How the three inference components are wired: CPA provider entries, AISIX logical routes and Sub2API accounts.

CPA listens at `http://127.0.0.1:8317` by default. Its management UI/API requires the private `CPA_MANAGEMENT_KEY` stored in `.env`. OAuth callback ports are also loopback-only. Use SSH port forwarding rather than changing them to `0.0.0.0` on a remote host.

CPA's global `proxy-url` forces provider transports through Squid. If an OpenAI-compatible entry targets another service on a pairwise internal network, set that entry's `api-key-entries[].proxy-url` to `direct`; otherwise the global proxy would incorrectly receive the internal HTTP request. Do not use `direct` for Internet destinations—the CPA container has no direct Internet route.

Add provider accounts to CPA, define their concrete IDs and logical routes in private `services/routing/aisix/resources.yaml`, then create an OpenAI-compatible upstream account in Sub2API. Keep every direct AISIX `display_name` byte-identical to its CPA `model_name`, including `/`; only client-facing logical combos use separate bare names. Every logical route uses the same bounded fallback status set `[400, 401, 402, 403, 404]`: `400` covers observed disabled/unavailable targets, `401` invalid or revoked target credentials, `402` unavailable payment/credit/balance/quota so later targets remain reachable, `403` denied accounts/models, and `404` absent or unexposed targets. Every route also sets `retries: 0` (fail over immediately; CPA already retries per credential) and `retry_on_429: true`; `max_fallbacks` is normalized to `len(targets) - 1`. Every direct model uses one cooldown (`60`/`300` seconds, honoring `Retry-After`) whose `trigger_statuses` keeps AISIX defaults and adds `400`/`402`/`403`/`404`, so a target disabled or absent in CPA is skipped for the cooldown window instead of re-tried on every failover; direct models set no `retries`/`timeout`/`stream_timeout` overrides. `ops/scripts/check-aisix-resource-policy.py` enforces this whole contract. Including `400` means a malformed request also walks the chain once and cools the targets it hit; cooled targets are still used when no healthy target remains.

| Field | Value |
| --- | --- |
| Base URL | `http://cli-proxy-api:8317/v1` (never `/rk/v1`) |
| API key | private `sub2api` value from `services/routing/aisix/caller-keys.json` |
| Pool mode | boolean `true` |
| WebSocket mode (`extra.openai_apikey_responses_websockets_v2_mode`) | `off` on every OpenAI API-key account, only while the front bridge is live; never `http_bridge` or `passthrough` |

The inner gateway sends model IDs containing `/` to CPA and all other IDs to AISIX. WebSocket turns are classified because the front `responses-ws-sse-bridge` turns them into `POST /v1/responses` before Sub2API. See [docs/model-routing.md](model-routing.md) before changing routes, keepers or account settings.

Leave that internal AISIX account without a Sub2API proxy. For every Sub2API account whose Base URL is on the Internet, explicitly assign an active `http` proxy record pointing to `sub2api-egress-relay:3128` with fallback mode `none`; Sub2API's account transports do not consistently inherit proxy environment variables.

The only standing AISIX management surface is the complete no-JavaScript page at `http://127.0.0.1:3001/status`; a deployment may also bind that same page to one explicit Tailscale address. It displays configured target order, strategy, fallback budget, direct IDs, AISIX's current eligible/cooldown/unavailable/unresolved state, and a one-minute recent-fallback badge next to a concrete target that was skipped during fallback, derived from AISIX's private Prometheus counters. `eligible` means only that AISIX is not currently excluding the target. A first eligible candidate is shown only for deterministic tag-free failover; dynamic strategies remain request-dependent. AISIX retains no authoritative current or last-served target history, and the page does not infer one from logs.

AISIX Admin, Scalar, and playground paths are not proxied by the page listener. Admin listens only at `172.30.68.2:3002` on the internal `aisix-status-admin` network. For temporary maintenance, forward a local port over SSH instead of publishing Admin:

```bash
ssh -N -L 127.0.0.1:3002:172.30.68.2:3002 operator@gateway-host
```

While the SSH process is running, browse `http://127.0.0.1:3002/admin/openapi-scalar` or make an authenticated request with the private Admin key from `services/routing/aisix/config.yaml`; load the key without placing it in shell history. Stop the SSH process immediately afterward. The forwarded endpoint must then be unreachable again.

## Sub2API admin access

Sub2API admin UI is available at `http://127.0.0.1:8086`. For remote administration:

```bash
ssh -L 8086:127.0.0.1:8086 user@gateway-host
```

To retain direct Sub2API access over Tailscale, set `SUB2API_BIND_ADDRESS` to the host's specific Tailscale IP. Do not use `0.0.0.0` without an independently enforced external firewall/security-group allowlist.
