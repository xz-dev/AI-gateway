# AISIX page-only operator status

The operator approved a sidecar design so AISIX upgrades do not carry a long-lived UI source patch. The standing external management surface is only `GET /status`; AISIX Admin APIs remain on a dedicated unpublished internal network.

## Agreed external examples

- `GET /status` returns complete server-rendered HTML without JavaScript and shows routing target order, strategy, deterministic failover candidate where valid, and direct-target eligibility/cooldown/unavailable details.
- Dynamic text is escaped. Admin credentials, provider-key references, upstream URLs, request content, and raw AISIX JSON never appear.
- `eligible` means not currently excluded, not independently health-checked. Routing models have no persistent current target. No last-served value is inferred from logs.
- Every non-`/status` path on the page listener is rejected; Admin, Scalar, playground, metrics, and inference APIs are not proxied.
- AISIX Admin remains continuously reachable only from the dedicated internal management network. Operator access is temporary through SSH or a default-off maintenance relay.

## Sequential slices

- [x] Record pre-change production behavior (`GET /status` is 404).
- [x] Drive the standalone Go status renderer through focused external HTTP tests, then build its pinned minimal image.
- [x] Add the internal management network and status relay; update init/validation while preserving all three direct AISIX data networks.
- [x] After explicit approval, remove the unshipped AISIX UI patch and prove the AISIX image retains only the upstream deadlock fix.
- [x] Run isolated Docker acceptance, then perform the bounded production management cutover.
- [x] Update operator documentation/evidence and pass strict OpenSpec/repository validation.
