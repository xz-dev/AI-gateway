## Context

Before this change, CPA both held provider credentials and hosted the model-router plugin used for client-visible aliases. New API was permanently rejected, and LiteLLM did not fit the required Responses, routing, or memory profile. AISIX 1.2.0 passed the functional candidate matrix, but production later exposed an upstream cooldown exclusion-log DashMap deadlock; upstream commit `7d6d14bbf5f5ec4577466da48ce7763d519d81bb` fixes it.

The final architecture separates authority cleanly: Sub2API owns clients, entitlements, quota, WebSocket ingress, and `http_bridge`; AISIX owns logical model routing and target cooldown; CPA owns provider credentials and pool execution. Existing public APISIX, SSE, catalog, WebSocket compatibility, and non-AISIX relay components remain independently required.

The operator also needs a direct status page, but AISIX's configured URL rewrites apply only to its proxy listener and its Admin routes are compiled into the Rust router. Carrying a local UI handler as a long-lived AISIX patch would make every upstream upgrade a source-merge exercise. Conversely, publishing the whole Admin API merely to support a browser page would expose more management surface than the operator needs.

## Goals / Non-Goals

**Goals:**

- Make AISIX the only logical-model scheduler on the CPA-backed path.
- Preserve native `/v1/responses` HTTP/SSE payloads while rewriting only logical model IDs to concrete CPA IDs.
- Isolate cooldown per concrete target and keep retries bounded across AISIX and Sub2API.
- Preserve catalog metadata and authorization semantics while adding AISIX-exclusive IDs.
- Fit production memory headroom, expose only a private server-rendered AISIX status page, keep the Admin API on an unpublished internal network, and leave unrelated dirty production work intact.
- Remove candidate infrastructure and permanently retire CPA model routing after zero-use verification.

**Non-Goals:**

- Restoring New API, LiteLLM, or CPA model-router as standby or rollback paths.
- Native end-to-end upstream WebSocket. Sub2API terminates WebSocket and reconstructs HTTP Responses requests.
- Solving implicit `previous_response_id` continuation; that requires a separate stateful bridge proposal.
- Removing unrelated public-entry, SSE, catalog, WebSocket compatibility, or relay components.
- Reproducing the commercial AISIX Cloud dashboard, permanently publishing Scalar/Admin APIs, or turning the status page into a general management reverse proxy.
- Inferring recent served targets by scraping or joining independent text log lines when AISIX exposes no authoritative retained last-served state.

## Decisions

### 1. One CPA execution channel, AISIX-owned logical routing

AISIX resources contain one CPA provider key. The 25 concrete model resources reference that key and use the exact CPA target IDs. The 12 client-visible bare-ID resources use ordered failover routing. A concrete target reused by more than one logical route remains one direct resource, so its cooldown state is shared correctly across those routes while remaining independent from other targets.

Concrete cooldown is enabled with a 60-second default, a 300-second clamp, and `honor_retry_after: true`. Logical routes use one bounded retry with `retry_on_429: true`; Sub2API accounts use bounded retry and boolean pool mode so retries are not accidentally multiplied by string coercion.

### 2. Direct data paths and isolated management path

Final production retains exactly three direct internal data edges:

- `sub2api-aisix`: Sub2API -> AISIX
- `aisix-cpa`: AISIX -> CPA
- `enricher-aisix`: models-enricher -> AISIX

These inference/catalog edges have no relays and only their intended members. AISIX has no external/default route and no Squid edge. Its model upstream is `http://cpa:8317/v1`; CPA-backed Sub2API accounts use `http://aisix:3000/v1`, never a static container address.

Management uses a separate internal network between the AISIX namespace owner and the status renderer. AISIX binds its Admin listener only to its address on that network and does not publish the Admin port to the host. A small status relay in the existing AISIX network namespace owns the approved loopback/Tailscale port and forwards it to the isolated renderer; it is outside all three data paths. The renderer is attached only to the management network and has no CPA, Sub2API, catalog, default-route, or public-network membership.

This extra relay preserves the project's established namespace-owner pattern. Letting the renderer share the AISIX namespace would save one container but would also give an Internet-reachable process holding the admin credential access to every AISIX data network.

### 3. Page-only server-rendered observability

The status renderer is an independently versioned minimal service rather than an AISIX source patch or an APISIX plugin. APISIX-style routing cannot remove the need for a renderer, and reusing a public APISIX listener would risk making unrelated gateway routes reachable on the management port.

For each `GET /status`, the renderer authenticates internally to AISIX's read-only model listing and runtime model-status endpoints. It parses only model identity, routing target order/strategy/budget, and runtime exclusion/cooldown fields, then emits escaped HTML. It reads the existing AISIX bootstrap config through a read-only mount for the admin credential, avoiding a duplicate secret lifecycle. It never returns or logs the credential, original response body, provider-key reference, upstream URL, or request content.

The external listener accepts only `GET` and `HEAD` for `/status`; Admin, Scalar, playground, metrics, inference, and unknown paths are not proxied. The page has embedded CSS, no JavaScript or external assets, a bounded automatic refresh, `no-store`, and a restrictive content-security policy. Errors produce a generic server-rendered unavailable page without backend detail.

Configured order and current exclusions are authoritative. A first eligible candidate is shown only when failover semantics are deterministic without request tags; dynamic strategies remain `request-dependent`. `eligible` means not currently excluded, not independently health-checked. Routing models are not labelled healthy, and no persistent current or last-served target is invented.

### 4. Private configuration and reproducible images

Tracked `aisix/config.example.yaml` and `aisix/resources.example.yaml` contain no production secrets. `scripts/init.sh` creates private runtime files without printing generated values and refuses to overwrite any existing AISIX, CPA, or `.env` state. `scripts/validate.sh` checks file modes, ignored paths, caller-key JSON shape, and caller-key hashes against the resources document.

Production uses one CPA provider key, 25 concrete models, 12 logical aliases, and two caller keys, totaling 40 resources. Caller plaintext exists only in private operator/Sub2API configuration; AISIX resources store hashes.

The AISIX image is built from the pinned 1.2.0 base plus only upstream deadlock-fix commit `7d6d14bbf5f5ec4577466da48ce7763d519d81bb`. The status renderer has its own explicit image tag and does not modify AISIX source. Production identifies exact built image IDs and records relevant checksums in redacted evidence. The deadlock patch can be removed when an official AISIX release contains it without coupling that upgrade to the status page.

### 5. Responses and WebSocket boundary

AISIX passes `/v1/responses` HTTP and SSE through CPA without protocol translation. Sub2API's `http_bridge` terminates client WebSocket and reconstructs the HTTP request, so explicit full-history WebSocket replay remains supported without upstream-native WebSocket.

CPA does not emit reusable `resp_*` IDs. Therefore implicit cached continuation containing only `previous_response_id` plus the new input delta fails and is intentionally outside this migration. It is planned separately rather than weakening or rolling back AISIX ownership.

### 6. Exact catalog supplement and memory bounds

The supplement is computed before CPA-local filtering:

`Extra = unique(case-sensitive AISIX IDs) - complete original CPA IDs`

Only `Extra` enters existing enrichment. Existing metadata precedence and the final Sub2API entitlement intersection remain unchanged, so catalog presence cannot grant authorization. An AISIX source failure falls back to the complete successful CPA baseline without duplicates.

Successful completed catalogs are cached by stable `client_version` for five minutes, limited to 24 entries and 16 MiB aggregate body bytes. Oversized catalogs are served but not cached. Requests for the same version share a build, and full builds across different versions are serialized to prevent concurrent response construction from exceeding memory. Production models-enricher runs with a 384 MiB cgroup limit and `GOMEMLIMIT=288MiB`.

### 7. Cutover and permanent CPA router retirement

The migration first proved an isolated candidate through real CPA. Production then integrated AISIX into the main Compose project, migrated every CPA-backed account, verified real-key Chat, Responses HTTP/SSE, explicit full-history WebSocket replay, catalog behavior, direct-network isolation, and natural traffic, and removed the candidate account, group, key, project, and transferred image archives.

Fresh CPA accounting showed no model-router-routed traffic. After explicit user confirmation, the native CPA delete endpoint removed the plugin; its databases, binaries, configurations, and snapshots were removed, and its two plugin-only egress ACL paths were removed and hot-reloaded. Recovery snapshots remain useful for data/account reconstruction but must not restore CPA model routing.

### 8. Evidence custody

Private migration snapshots live under `/root/AI-gateway/.migration-evidence/` with tightened modes and checksums. They include the retained Sub2API SQL recovery point and account/Compose/CPA state. Redacted provenance records exact image IDs, patch/config checksums, deletion facts, and bounded verification results without exposing secrets. Deleting retained recovery data is a separate destructive decision and is not required for this change.

## Risks / Trade-offs

- **AISIX version maturity:** mitigated by a pinned build, the upstream deadlock patch, a bounded soak, and restart/panic/listener checks.
- **Admin-schema drift:** an AISIX upgrade could change a field consumed by the renderer -> run focused response-contract tests against the exact candidate image; a renderer failure returns a status-only 503 and never blocks inference.
- **Admin credential in the renderer:** the internal API has broader read authority than the page needs -> isolate the renderer on the management network, mount the bootstrap config read-only, construct fixed upstream paths, suppress backend bodies from logs, and expose no proxy route.
- **Status wording mistaken for health:** a non-excluded target may never have received an independent probe -> label it `eligible`, explain the term on-page, and reserve `unavailable`/`cooldown` for AISIX runtime exclusions.
- **Catalog memory:** mitigated by aggregate cache accounting, serialized builds, a Go soft limit, and a production cgroup regression test.
- **Shared-provider quota:** natural 429s may be observed, but acceptance never intentionally exhausts shared quota. Mock/contract coverage and natural traffic prove cooldown behavior without destructive probing.
- **`prompt_cache_retention`:** successful black-box confirmation was blocked by naturally exhausted Kimi quota. Static CPA core filtering evidence is accepted for this migration.
- **Implicit WebSocket continuation:** explicitly unsupported until the separate stateful continuation change is implemented and accepted.
- **Rollback scope:** retained data snapshots support repair, but neither New API nor CPA model-router may be restored as a routing fallback.

## Migration Plan

1. Build and test the renderer against fixed Admin API fixtures and the exact AISIX candidate image without changing production listeners.
2. Add the dedicated internal management network and start the renderer before moving the published management port.
3. In one bounded AISIX management cutover, move the Admin listener to the unpublished internal address and place the status relay on the existing loopback/Tailscale port. Keep inference port 3000, resources, caller keys, and all three direct data networks unchanged.
4. Verify `/status`, external rejection of Admin/API paths, absent wildcard/Admin publications, internal authenticated status reads, unchanged inference/readiness, and zero unexpected restarts or secret leakage.
5. Retire the unshipped local status-page source patch only after explicit deletion approval. If the page cutover fails, restore only the previous management binding while repairing the renderer; never restore a retired model router.
6. Keep normal operation with no host route to the Admin API. For maintenance, use an explicit temporary SSH tunnel to the internal address; add a default-off maintenance relay only if host-to-internal bridge reachability is unavailable.

## Resolved Operational Decisions

- Group 14 is the Sub2API entitlement group `旋律`, bound to account 5. The user confirmed its exact four-model entitlement: `glm-5.2`, `glm-5.3`, `kimi-k3`, and `kimi-k3-256k`.
- The catalog supplement is active after the AISIX production cutover.
- Retained private migration evidence remains in place; long-term deletion requires separate authorization.
- The standing external management surface is only the server-rendered `/status` page. AISIX Admin APIs remain internal and are opened to the operator only through an explicit temporary maintenance path.
