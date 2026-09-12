# AISIX production migration acceptance evidence

## Final production state

- Production root: `/root/AI-gateway`, Docker Compose project `ai-gateway`.
- Routing path: Sub2API -> AISIX -> CPA. The catalog enricher also reaches AISIX directly.
- AISIX resources: one CPA provider key, 25 concrete models, 12 logical routing models, and two caller keys; 40 resources total. `/v1/models` exposes all 37 model IDs.
- CPA-backed accounts 1, 4, 5, and 6 use `http://aisix:3000/v1`, boolean pool mode, bounded retry, and remain active/schedulable without account-level fault state.
- Sub2API retains client authentication, entitlement, quota, WebSocket ingress, and `http_bridge`. AISIX exclusively owns logical IDs, ordered fallback, and per-concrete-target cooldown. CPA retains provider credentials and pool execution.

## Immutable images and upstream repair

- AISIX image: `ghcr.io/xz-dev/ai-gateway-aisix:1.2.0-deadlockfix-7d6d14b`.
- AISIX production image ID: `sha256:9ec1fdfdb32625e4aa21f1087d45d02d3264d8cce265dccc8e29607975a56aaf`.
- AISIX status image: `ghcr.io/xz-dev/ai-gateway-aisix-status:0.1.0`.
- AISIX status production image ID: `sha256:a9e2532038450d5a8a18d6a35efa8ec0cf9d1fcb660fc32d8a2a8dafecd575e2`.
- GitHub Actions run [`34700631293`](https://github.com/xz-dev/AI-gateway/actions/runs/34700631293) built and published both fixed-version GHCR artifacts with OCI provenance and SBOM attestations. Production pulled those artifacts and did not compile either image on the 2 GB host.
- AISIX source revision: upstream 1.2.0 base `adcf0523b9f84deed64fbdea311df292c5bc541b` plus upstream commit `7d6d14bbf5f5ec4577466da48ce7763d519d81bb`; the retained patch SHA-256 is `3908eefe1d81f4325b3d953a9d773b6d50f0645d76b55f722d39528f6e9f7a0a`.
- The patch fixes the cooldown exclusion-log DashMap deadlock observed in production. A bounded soak and subsequent natural traffic exercised the patched path without blocked listeners, panic, fatal log, or restart. It is a temporary single-patch build until an official stable AISIX release contains the fix.
- Models-enricher image: `models-enricher:aisix-cachebound-20260912`.
- Models-enricher production image ID: `sha256:f8e31ec1e0ebeafedfb31a3446feed561803b44632c025eb045864c3f8a03bbd`.

## Network and management boundary

Final production has no AISIX data-path socat relay. Docker network inspection verified exactly two members on each internal edge:

- `sub2api-aisix`: `sub2api-netns`, `aisix-netns`
- `aisix-cpa`: `aisix-netns`, `cpa-netns`
- `enricher-aisix`: `models-enricher-netns`, `aisix-netns`
- `aisix-status-admin`: `aisix-netns`, `aisix-status`

Inside the running AISIX namespace, IPv4 default routes = 0 and IPv6 default routes = 0. AISIX Admin binds only at `172.30.68.2:3002` on `aisix-status-admin`; a direct connection to port 3002 on the AISIX data-network address was refused. The namespace owner publishes only the page relay at `127.0.0.1:3001` and `100.94.238.35:3001`. Neither the AISIX nor status container publishes a host port, and the status renderer joins only `aisix-status-admin`.

The first cutover restart exposed an existing mode-`0400` AISIX config that both non-root readers could not reopen. The operator approved the intended mode-`0444` correction inside the mode-`0700` host directory; AISIX and the status renderer then remained running. Their Docker restart counters retained the ten short failed starts from that bounded incident but did not increase afterward. Sub2API, CPA, the provider sidecar, and egress proxy remained running and healthy with restart count 0; available memory was 319 MiB with 140 MiB swap used.

## Page-only management acceptance

Production Docker acceptance verified identical HTTP 200 complete HTML at loopback and `http://100.94.238.35:3001/status`: 12 logical routes and 25 direct targets matched authenticated `/admin/v1/models` and `/admin/v1/models/status` read-back, including configured target order and current state. At acceptance time all 25 direct targets were `eligible`; no synthetic cooldown was triggered merely to populate the page.

The page contained no JavaScript, Admin credential, caller key, provider-key reference, upstream URL, or raw AISIX response. It returned `Cache-Control: no-store` and a restrictive `default-src 'none'` CSP. Root, Admin, Scalar, playground, metrics, inference, and unknown paths returned 404; `POST /status` returned 405. An authenticated inference-list request on the private data listener returned all 37 model IDs without consuming provider inference quota.

A temporary SSH local forward to `172.30.68.2:3002` returned 401 without the Admin key and 200 with it. After stopping SSH, the forwarded local endpoint was unreachable. This is the maintenance procedure; there is no standing Admin relay or publication.

The page labels `eligible` only as not currently excluded. It reports a first eligible candidate only for deterministic tag-free failover and calls dynamic strategies request-dependent. AISIX exposes no authoritative retained current or last-served target, so the page explicitly says this and does not join independent logs to invent one.

## Protocol acceptance

Real client-key tests through the production entry verified:

- Chat Completions: success.
- Responses non-stream: HTTP 200 with `completed` status.
- Responses SSE: complete event sequence ending in `response.completed`.
- WebSocket with explicit full-history replay: success through Sub2API `http_bridge`.
- A post-CPA-router-deletion `glm-5.3` Responses request: HTTP 200 with `completed` status.

Native `/v1/responses` HTTP/SSE remains passthrough. AISIX performs model selection only; it does not replace Sub2API's WebSocket boundary.

## Catalog formula and memory repair

The catalog supplement uses the exact case-sensitive difference:

`unique(AISIX IDs) - complete original CPA IDs before CPA-local filtering`

The result then enters the existing enrichment pipeline and final Sub2API entitlement intersection. Tests verified overlap rejection even when CPA-local filtering later removes the original, case-sensitive identity, exact-byte preservation, duplicate removal, valid-empty replacement, malformed/auth failure behavior, timeout stale-cache behavior, and CPA-baseline preservation. An isolated AISIX-source outage returned the complete CPA baseline without duplicate/resurrected IDs.

The completed-catalog cache is scoped by `client_version`, expires after five minutes, holds at most 24 entries and 16 MiB aggregate body bytes, does not retain oversized results, coalesces same-version requests, and serializes full builds across different versions. Production uses a 384 MiB cgroup limit and `GOMEMLIMIT=288MiB`.

The bounded production regression ran three concurrent distinct-version catalog builds. All returned HTTP 200; cgroup peak was 223,744,000 bytes below the 402,653,184-byte limit; AISIX and models-enricher restart counts remained 0.

## Group 14 decision

Live database read-back verified:

- Group 14 is `旋律`, active on platform `openai`, and linked to account 5.
- Account 5 is active/schedulable, uses `http://aisix:3000/v1`, boolean `pool_mode=true`, retry count 1, and WebSocket mode `http_bridge`.
- Account 5's exact `model_mapping` keys are `glm-5.2`, `glm-5.3`, `kimi-k3`, and `kimi-k3-256k`.

The user explicitly confirmed that this effective four-model entitlement is intentional. No production entitlement mutation was needed.

## Candidate and CPA router retirement

Candidate key 24, account 8, and group 18 were deleted and each returned HTTP 404 afterward. `/root/AI-gateway-aisix-candidate`, candidate containers/networks, transferred image archives, local candidate trees, and superseded candidate test files were removed without disturbing the production stack.

Fresh pre-delete CPA model-router accounting was:

- direct records: 326
- unattributed records: 33
- routed records: 0

This proves the plugin no longer carried routed traffic; it does not claim the plugin had no accounting activity. After explicit user confirmation, CPA's native delete endpoint returned `status=deleted`. The active plugin configuration/binary, 26 router databases, old binaries, configuration snapshots, database copies, and two router-only GitHub download ACL paths were removed. The generated Squid configuration was rebuilt, checked, and hot-reconfigured. A final active-tree scan excluding retained migration evidence found zero paths named for model-router. CPA model-router is not a standby or rollback path.

## Initialization, templates, and local verification

Tracked examples contain no production secrets. `scripts/init.sh` creates private AISIX config, resources, and caller-key files without printing generated values, keeps `aisix/config.yaml` mode `0444` inside the mode-`0700` directory for both non-root readers, and refuses overwrite. `scripts/validate.sh` checks those modes, ignored paths, caller-key shape and hashes, the three exact direct AISIX networks, the dedicated unpublished management network, fixed-version images, and established catalog exceptions without weakening the ordinary relay checks.

Final local verification passed:

- `gofmt -d` on all changed models-enricher Go files: clean.
- Focused AISIX/difference/cache/concurrency tests: pass.
- `go test -race ./...` and `go vet ./...` in both `aisix-status` and `models-enricher`: pass.
- `actionlint` over both GitHub workflows: pass.
- Bash syntax and ShellCheck through the full validator: pass.
- `openspec validate add-aisix-model-routing --strict`: pass.
- `scripts/validate.sh .env.example` against the base template with an explicit empty temporary override: pass, including Compose render, APISIX/Squid parsing, fail-closed egress, TLS, relay boundaries, namespace guard, and secret-path checks.

The repo-root private `compose.override.yaml` was deliberately excluded from the base-template run because it requires deployment-only provider-sidecar credentials. It was not overwritten.

Production syntax and Compose rendering also passed using the real `.env` and private override without printing secrets. Non-secret AISIX examples, source, and `scripts/init.sh` matched local final hashes. Production-specific Compose and `.env.example` differences were preserved; their AISIX and cache-bounded image values and rendered semantics were verified instead of copying local files wholesale.

## Retained evidence and accepted limitations

Private snapshots remain under `/root/AI-gateway/.migration-evidence/` with tightened modes. The retained Sub2API SQL recovery point is 848,698,229 bytes. All 24 files covered by the retained cutover `SHA256SUMS` verified successfully. The redacted production receipt is mode 0600 and its final SHA-256 is `b3adbeeba9918bc5d33fd39a484c992f5e3c16b5482f031cf5adf52eaa510c9d`.

Accepted limitations:

- CPA does not emit reusable `resp_*` IDs. Implicit WebSocket continuation using only `previous_response_id` plus a new delta remains unsupported and is planned as a separate change; explicit full-history replay remains supported.
- Kimi's natural quota exhaustion prevented a successful black-box `prompt_cache_retention` probe. Static CPA core filtering evidence is accepted; shared quota was not intentionally exhausted or repeatedly probed.
- Retained private recovery evidence is intentionally not deleted. Its future removal requires a separate destructive-operation decision.
- The custom AISIX image exists only because official stable `v1.2.0` predates deadlock fix `7d6d14b`; replace it with a fixed-version official stable image after a release containing that fix is verified.
