# Codex catalog transfer interrupted after HTTP 200 — 2026-11-20

The versioned catalog reached both inner endpoints intact. One public request also completed; another ended mid-body after the front APISIX had logged a partial send and the tunnel reported `unexpected EOF` from its origin. The failure is on the **delivery of the front response**, not the construction of the inner catalog. The first component to close the connection was not identified. This follows up the [original observation](aisix-fallback-capability-2026-11-20.md#observation-codex-model-list-transfer-interrupted).

## Measurements

All requests below were `GET /v1/models?client_version=1`. Times are UTC; public byte counts are bytes read from the response, before content decoding.

| Start | Endpoint | HTTP | Bytes read | JSON / metadata | Elapsed |
|---|---|---:|---:|---|---:|
| 07:15:36 | model-catalog | 200 | 3,050,610 | Valid, 354 models; matched Content-Length | 8.15 s |
| 07:15:44 | inner apisix-models | 200 | 3,050,610 | Valid, 354 models; same SHA-256 prefix `4a35300daa13` | 0.03 s |
| 07:16:16 | Public front, identity encoding | 200 | 3,040,477 | Valid, 343 entitled models | 125.06 s |
| 07:20:37 | Public front, gzip encoding | 200 | 433,021 | Incomplete chunked response (`IncompleteRead`); no complete JSON | 86.03 s |

The front reads Sub2API's entitlement response and the enriched catalog separately and returns their intersection. The 343 and 354 counts are not expected to match. Sub2API logged completed `/v1/models` responses with HTTP 200 in 1.56 s and 12.09 s at the respective public probe times; its leg was not separately replayed with the caller credential. The [front handler](../../services/firewall/apisix/lua/model_list_intersection.lua) reads and validates both bodies before returning the intersection.

For the complete public sample, the front access log recorded 3,040,492 bytes; for the interrupted one, it recorded only 2,086,301. Both were logged as 200. At 07:22:03 the tunnel logged `unexpected EOF` from the front origin. Front access-log counts are not the same measurement as client entity bytes, especially across compression. The earlier 406,351-byte failure and the new 433,021-byte failure are not evidence of a fixed cutoff. The earlier 48-second client deadline was a separate event.

**Likely mechanism, not proven:** the live front nginx configuration has `send_timeout 10s`. A sufficiently long pause between downstream writes can close a response after its headers were sent. The error log was at `warn`; there was no direct send-timeout record, socket trace or controlled timeout comparison. A tunnel or downstream disconnect could also produce a partial front send. The 64 MiB Lua body limits are far above these bodies and would reject the catalog before the handler sends 200. Do not interpret the two samples as proof that gzip causes truncation.

## Comparison with the two inference incidents

The following comparisons use existing access and routing logs only. They match by time, endpoint, model when recorded, and number of requests. There was no verified end-to-end request ID from the reporting client.

- **06:54:05 and 06:54:08, Responses:** CPA logged 500 for `/v1/responses` at both seconds. AISIX logged one attempt per `gpt-6.2-sol` request, `upstream_status` and HTTP 502; Sub2API also logged 502. The front recorded two HTTP 200 Responses requests with only 132 bytes each at approximately those times. That is consistent with an error after the front committed the stream, not proof of who closed the client connection. Unlike the catalog incident, an inner error was visible in both cases.
- **06:55:10–06:56:00, search:** two concurrent CPA 500s at 06:55:10 coincided with two single-attempt AISIX 502s for `gpt-6.1-sol` and two Sub2API 502s. Subsequent Sub2API entries included short 503 responses (8–50 ms); the front access log showed both 502 and 503. The numeric evidence establishes upstream failures and a different fast-failure path, **not** Codex overload or the exact cause of every retry. It does not support attributing these 5xx to the catalog transfer issue.

No live generation, configuration change, restart, new credential or forced refresh was used. The catalog and inference results do not justify changing AISIX return-code classification outside its separately specified aggregate-target case.
