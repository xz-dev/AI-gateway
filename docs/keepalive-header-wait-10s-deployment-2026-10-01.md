# Keepalive `HEADER_WAIT` 30s → 10s — 2026-10-01

## Result

Production `AI_SSE_KEEPALIVE_PROXY_HEADER_WAIT` is now **10s**. The 2026-09-30 value of 30s caused streaming `/v1/responses` requests from the OpenAI Python SDK to be dropped at about 30–32 seconds whenever the upstream's first byte took 30s or longer. Hermes (`api_mode: codex_responses`) retried each turn for minutes and never got a response.

**Keep `HEADER_WAIT` well below 30s.**

## Symptom

- Hermes logged `Codex Responses request failed: ... stream_opened=false exception_chain=APIConnectionError <- ReadError` about 31s into each physical attempt, two attempts per API call, five API calls per turn.
- APISIX recorded the same requests as `POST /v1/responses 200 45 ~31.5s`. The 45 bytes are the proxy's `response.in_progress` startup frame.
- cloudflared logged `Request failed error="stream ... canceled by remote with error code 0"` for each one.

## Boundary isolation

Same `stream:true`, `reasoning.effort=high` long-output request against `gpt-6.1-sol`; the only variable is `Accept`:

| Path | `Accept` | Outcome |
|---|---|---|
| Public (Cloudflare → tunnel → APISIX → keepalive) | `text/event-stream` | 200, streamed normally |
| Public | `application/json` (OpenAI Python SDK default) | Connection reset at ~30.6s, no response |
| Origin direct (`apisix-relay`, bypassing Cloudflare) | `application/json` | 200 committed at 30.01s, streamed normally |
| Origin direct | `text/event-stream` | 200, streamed normally |

The gateway behaves correctly on both `Accept` values. **The reset happens at the Cloudflare edge/tunnel hop**, only for `Accept: application/json` when the origin's first response byte arrives at about 30s or later. Moving the keepalive commit point earlier avoids the condition. A request whose first upstream byte comes before `HEADER_WAIT` is unaffected.

Not established: Cloudflare's exact rule (a fixed 30s first-byte deadline for that content negotiation, or something else). Do not raise `HEADER_WAIT` near 30s again without repeating the public-path `Accept: application/json` canary.

## Trade-off

`HEADER_WAIT` also decides how long a fast upstream rejection can keep its real HTTP status (see [late-error deployment](keepalive-late-error-deployment-2026-09-30.md)). Quota/credit 4xx were observed at 2.5–8.3s on 2026-09-30, so 10s still covers them. A rejection arriving after 10s now becomes an in-band stream error. Approved 4xx statuses keep the upstream message.

## Deployment

- Only the private `.env` line changed; image, compose bytes and other settings were unchanged. Before the change, the local private config and the remote `.env`/`compose.yaml` checksums matched.
- `ansible-playbook ops.yml --tags deploy-ws-keepalive -e ai_ops_mode=plan`: the effective diff was the single `HEADER_WAIT` line.
- Apply with the existing digest `sha256:c4af69df…2c5c5` and revision `bd38e50`: `failed=0`, `rescued=0`. Container recreated, approved image identity and OCI revision confirmed, healthcheck `healthy`, protected neighbours unchanged.

## Verification

- Running container env: `HEADER_WAIT=10s`.
- OpenAI Python SDK, same slow request through the public hostname: before, `RemoteProtocolError` at 32s; after, stream opened at 12s, first delta at 14s, still streaming at 75s (1,338 events).
- Hermes production log: its API calls since 19:59:26 +08:00 receive stream events and no longer fail.

## Rollback

Restore `AI_SSE_KEEPALIVE_PROXY_HEADER_WAIT=30s` in private desired state and rerun the same scoped plan/apply. Expect the SDK failure above to come back.
