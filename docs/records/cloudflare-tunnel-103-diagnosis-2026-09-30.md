# Cloudflare Tunnel: 103 forwarding and long-request limits

Verified on 2026-09-30. This investigation does not change Cloudflare account settings or the tunnel configuration.

## Result

**There is no documented Cloudflare Tunnel setting that makes repeated origin HTTP 103 responses into an end-to-end API heartbeat.** The first confirmed failing hop in the deployed HTTP-origin path is **cloudflared's origin HTTP transport**, not the current APISIX version.

For long AI requests on a non-Enterprise zone, keep the request streaming and flush SSE body frames during upstream silence. For a genuinely non-streaming response that cannot finish within the edge's read deadline, use an asynchronous API, a separately secured non-proxied access path, or investigate the Enterprise read-timeout setting. None of these makes 103 forwarding work by itself.

## Evidence by boundary

| Boundary | Evidence | Conclusion |
|---|---|---|
| Test origin | Direct HTTP/1.1 client saw 103 followed by final 200. | The fixture generated real interim responses. |
| Production APISIX | `nginx -v`: `openresty/1.29.2.4`; generated `/usr/local/apisix/conf/nginx.conf` contains `early_hints on;`. | The earlier old-Nginx assumption is outdated. NGINX introduced this directive in 1.29.0. Config presence alone is not a packet-level forwarding test. |
| cloudflared 2026.9.3 HTTP-origin transport | `proxyHTTPRequest` calls `httpService.RoundTrip`, then writes the returned response headers. `httpService.RoundTrip` calls Go's HTTP transport. The tagged first-party source has no `Got1xxResponse` forwarding hook. | Go consumes non-101 informational responses; cloudflared does not relay the origin's 103 at this boundary. |
| Temporary quick tunnel | Cloudflared 2026.9.3, QUIC, LAX edge; client HTTP/2. Short request received only final 200. Both long requests returned 524 at about 126 seconds, whether the origin emitted 103 every 15 seconds or remained silent. | Origin 103 did not reach the client and did not extend the edge deadline in this tested path. |

Source references:

- [NGINX `early_hints`](https://nginx.org/en/docs/http/ngx_http_core_module.html#early_hints)
- [cloudflared `proxyHTTPRequest`, 2026.9.3](https://github.com/cloudflare/cloudflared/blob/2026.9.3/proxy/proxy.go#L235-L313)
- [cloudflared HTTP-origin `RoundTrip`, 2026.9.3](https://github.com/cloudflare/cloudflared/blob/2026.9.3/ingress/origin_proxy.go#L35-L58)
- [Go HTTP `Transport`: 1xx handling](https://pkg.go.dev/net/http#Transport)

### Canary and limitations

The slow fixture emitted `Link: </x.css>; rel=preload; as=style` in its 103 responses. A direct-origin sanity request received the interim response. Through the temporary quick tunnel:

| Request | Origin behavior | Public client outcome |
|---|---|---|
| `/hint/35` | 103 at 15 and 30 seconds; final 200 after the fixture's sleep loop | Only HTTP/2 200; no 103. The loop rounded the final response to roughly 45 seconds. |
| `/hint/150` | 103 every 15 seconds, including at 120 seconds; intended final 200 at 150 seconds | HTTP/2 524 at about 126 seconds. |
| `/silent/150` | No response before the intended final 200 | HTTP/2 524 at about 126 seconds. |

The successful client path was the production host's public egress, requesting the temporary hostname; it did not route through the production gateway API or consume model quota. Initial client attempts from the local machine failed around 31 seconds while routed through `clash0`; those attempts are not evidence for the Cloudflare deadline.

The temporary origin, tunnel and binary were removed. The quick tunnel did not use the production zone's account settings. This establishes the tested connector/edge behavior, not a complete inventory of the production zone's plan or custom rules. It also does not establish how an edge would handle an origin 103 if a future connector actually forwarded it.

## What Cloudflare can be configured to do

### Enterprise: extend the response read timeout

Cloudflare documents a default **125-second Proxy Read Timeout**, configurable for Enterprise zones. Its 524 documentation describes increases up to **6,000 seconds**.

For authenticated JSON/POST model APIs, the zone-level setting is the relevant starting point:

```http
PATCH /client/v4/zones/{zone_id}/settings/proxy_read_timeout
Content-Type: application/json

{"value":600}
```

This is an illustrative 600-second value, **not an applied change or a recommendation to use 600 without measuring the workload**. First check the zone's plan, whether the setting reports `editable: true`, and whether the intended value applies to its Tunnel traffic. Re-run the delayed-response canary against a controlled production-zone test endpoint after changing it.

Enterprise Cache Rules also expose `read_timeout`, but Cloudflare's 524 documentation says that route requires cacheable content. **Do not enable caching for authenticated POST `/v1/*` responses just to reach this setting.** Our API traffic should not be treated as ordinary cacheable web pages.

Sources:

- [Connection limits](https://developers.cloudflare.com/fundamentals/reference/connection-limits/)
- [Error 524 and Enterprise adjustments](https://developers.cloudflare.com/support/troubleshooting/http-status-codes/cloudflare-5xx-errors/error-524/)
- [Zone setting API](https://developers.cloudflare.com/api/resources/zones/subresources/settings/methods/edit/)
- [Cache Rules: Proxy Read Timeout](https://developers.cloudflare.com/cache/how-to/cache-rules/settings/#proxy-read-timeout-enterprise-only)

### Tunnel options: different timeouts, not an edge-limit override

| Setting | What it controls | Why it is not this fix |
|---|---|---|
| `originRequest.connectTimeout` | Establishing TCP to the origin | Not the duration of an already-connected model request. |
| `originRequest.tlsTimeout` | TLS negotiation to the origin | Not response generation. |
| `originRequest.keepAliveTimeout` | Retention of an idle pooled origin connection | Not a heartbeat or active-request deadline. |
| `originRequest.http2Origin` | HTTP/2 instead of HTTP/1.1 to an HTTPS origin | Does not install a 1xx forwarding callback or override the edge read deadline. |
| `--protocol http2` / `quic` | Connector-to-edge tunnel transport | No documented option to relay 103 or extend the edge deadline. |

Source: [Tunnel origin parameters](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/configure-tunnels/cloudflared-parameters/origin-parameters/).

### The Early Hints dashboard toggle is a web-performance feature

Cloudflare's cached Early Hints feature derives preload/preconnect `Link` headers from previous 200/301/302 responses on HTML-like paths. It can send a cached 103 before contacting the origin. Turning it on is not a solution for periodically relaying an AI API's informational heartbeat, nor does its documentation promise a response-read-timeout reset.

Cloudflare's separate general 1xx page says it forwards origin informational responses. That statement must not be substituted for evidence that **cloudflared's HTTP-origin tunnel path** forwards them; the connector source and canary show the earlier loss.

Sources:

- [Cloudflare Early Hints](https://developers.cloudflare.com/cache/advanced-configuration/early-hints/)
- [Cloudflare's Early Hints implementation explanation](https://blog.cloudflare.com/early-hints/)
- [General 1xx documentation](https://developers.cloudflare.com/support/troubleshooting/http-status-codes/1xx-informational/)

## Corrected claims and remaining unknowns

- The investigation does **not** prove that Cloudflare's HTTP/2 tunnel protocol can never support informational responses. The present connector path does not forward them. Its QUIC response adapter writes status/headers as connect-response metadata and has no implemented interim-forwarding path; a compatible connector/edge implementation change would need separate testing.
- Normal 103 consumption in Go's transport does **not** by itself disable connection reuse. The transport loops over informational responses before the final response. An earlier conclusion based on a later defensive `resp.StatusCode <= 199` branch was incorrect.
- Even if a future connector forwarded 103, whether those informational headers reset the edge deadline still needs an end-to-end timeout test. No current configuration-only fix for that is established.
- SSE/WS keepalive and `HEADER_WAIT` do not restore exhausted GLM quota. The 2026-09-30 GLM incident was upstream quota/credit exhaustion whose actual errors were masked after the proxy had already committed HTTP 200.
- Production `HEADER_WAIT` (10s since 2026-10-01; 30s caused [Cloudflare resets of slow `Accept: application/json` streams](keepalive-header-wait-10s-deployment-2026-10-01.md)) allows errors returned within that interval to retain their HTTP status. Once final HTTP 200 has been committed, later failures must be represented within the stream; the HTTP status cannot be changed back to 400.
