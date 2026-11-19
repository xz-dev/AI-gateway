# Production component update — 2026-11-18

## Outcome: 13 of 13 components deployed

The operator asked to update every component and deploy each directly, one at a time. All 13 are live. Each change went through a guarded preview and approved-digest apply, or through a backed-up manual window with automatic rollback for the components that have no deploy profile. After each step: public probes passed, and a container-identity readback showed that only the intended containers changed.

| Component | Before | After | Path | Live (UTC) |
| --- | --- | --- | --- | --- |
| cpa-model-sync | v0.1.3 | v0.1.4 (in-repo build) | guarded, preload | 04:25 |
| apisix-models | apache/apisix 3.19.0-debian | 3.20.0-debian | guarded | 05:19 |
| apisix (front) | 3.19.0-debian | 3.20.0-debian | guarded (4 pull retries) | 05:38 |
| cloudflared | 2026.10.0 | 2026.11.1 | guarded | 05:42 |
| egress-proxy | squid-openssl 6.13-2+deb13u3 | 6.13-2+deb13u4 (`-r1`) | guarded, preload | 05:47 |
| model-catalog | v1.4.0-6f595ab | v1.4.1-66e36e3 | guarded | 05:58 |
| cli-proxy-api | fork f38c87df | fork 3d1f7004 on upstream v8.0.77 | guarded | 06:39 |
| provider-sidecar (ZCode) | 4.8.1 | 4.9.0 | guarded (see deviations) | 08:53 |
| provider-sidecar-tunnel | tun2proxy v0.8.4 | v0.8.7 | manual pair window | 09:04 |
| postgres | 18.6-alpine3.24 | 18.7-alpine3.24 | manual window | 09:18 |
| redis | 8.10.2-alpine3.23 | 8.10.3-alpine3.23 | manual window (same as postgres) | 09:18 |
| aisix | fork af9f9147 (v1.8-based) | fork 67343036 (v1.10.0-based) | guarded | 09:28 |
| sub2api | fork 02918ea957 (v0.2.16 lineage) | fork 2f0213fd2c on exact tag v0.2.22 | guarded | 09:58 |
| netns guards ×10 | alpine 3.24.2 | alpine 3.24.4 | rolling manual window | 10:03–10:20 |

Deployed image references:

```text
CPA_MODEL_SYNC (compose image)=localhost/cpa-model-sync:v0.1.4
APISIX_IMAGE=docker.io/apache/apisix:3.20.0-debian@sha256:013de62a9dfba7d8e6006b3e85b524c28c9f1c3549fca3b91d5b258b0b9e759a
APISIX_MODELS_IMAGE=docker.io/apache/apisix:3.20.0-debian@sha256:013de62a9dfba7d8e6006b3e85b524c28c9f1c3549fca3b91d5b258b0b9e759a
CLOUDFLARED_IMAGE=docker.io/cloudflare/cloudflared:2026.11.1@sha256:e613aadbc2d9511785d2e67a86b0c8ac18dd63a3f9af0d06faf04cbfdcb35f2d
EGRESS_PROXY_IMAGE=ai-gateway-squid:6.13-2-deb13u4-r1
MODEL_CATALOG_IMAGE=ghcr.io/xz-dev/ai-gateway-model-catalog:v1.4.1-66e36e3@sha256:03b5e4e37e2ce79b947787905737f79f68227236cb2b28e47227dbcc2233ef48
CLI_PROXY_IMAGE=ghcr.io/xz-dev/cli-proxy-api:ci-3d1f7004-v8.0.77-capacity-sse@sha256:c938d0b716db235c0b182104758d45a046bcf592339c31eafc1a9f1b1c7484fd
PROVIDER_SIDECAR_IMAGE=ghcr.io/tridefender/zcode-proxy:4.9.0@sha256:089821b6e8fd93a5601224c3a0e6f82e2146480eea3418fcbbd6ce82dd9ebeec
TUN2PROXY_IMAGE=ghcr.io/tun2proxy/tun2proxy:v0.8.7@sha256:831732e66cf66b4746bc090b6346d040a880efb7972d623b99c7ee8dda4146b0
POSTGRES_IMAGE=docker.io/library/postgres:18.7-alpine3.24@sha256:b6972e1115a4ff8eba5d7f7d19184926d1e16d7d1136ae1e1a7ebf286da0f015
REDIS_IMAGE=docker.io/library/redis:8.10.3-alpine3.23@sha256:7958cfbb9d152f87dadfbc89f1b0b15938f0b81ff8982feb327476f4911935c3
AISIX_IMAGE=ghcr.io/xz-dev/aisix@sha256:9721ff105e2e568bcaa31c5e5ae4231ecce1c74df822619fd51f4e087e3c6108
SUB2API_IMAGE=ghcr.io/xz-dev/sub2api@sha256:8e329cb79d9c4c1bd46074e399d0037f10dc506962fb3ce6b9f2997aeaf6ac5d
NETNS_GUARD_IMAGE=docker.io/library/alpine:3.24.4@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf
```

Each changed private `.env` pin keeps its predecessor on a `# rollback:` comment line. Final production `.env` SHA256: `bd99d76b1cb068b3294017108b04b8314a1c72c4459c4431880c524427b5e7d8`. It matches the controller's private copy, and both Compose files are byte-identical between controller and host.

Tracked pins: `0c12c11` (squid deb13u4 build set) and `844e6d9` (`.env.example` / Compose defaults). The capacity-fork defaults for Sub2API, AISIX and CPA stay on `ci/large-request/ci/compatibility-lock.json` by design (see follow-ups).

## Acceptance

- Final state: 48/48 managed containers running, every healthcheck healthy, container count equal to the pre-round baseline.
- After every step: authorized public `/v1/models` HTTP 200, plus Responses SSE turns that reached `response.completed` for `gpt-6.1-sol`, `claude-opus-5-5` and a ZCode model. Sub2API and AISIX steps also probed `glm-5.2` and `gpt-6-luna`.
- Readback after each guarded step: only the target container's identity changed. The netns window changed exactly the 10 guards and their 14 dependents (24 containers). No other container changed.
- AISIX: restart count 0, no OOM; `deploy-config.yml -e component=aisix --check --diff` reported no `resources.yaml` drift; `config.yaml` unchanged.
- CPA: plugins `codex-auto-reset` 0.3.0 and `cpa-quota-estimator` 0.21.3 registered, also after the netns window recreated CPA.
- Sub2API: `/health` healthy, restart count 0, image revision label `2f0213fd2c…`, schema_migrations 296 → 304.
- Postgres/Redis: server 18.7 and Redis 8.10.3. schema_migrations (296) and the users count were unchanged across the window. Sub2API `/health` was OK immediately afterwards.
- All 10 netns guards run alpine 3.24.4 and have no IPv4/IPv6 default route.

## Gates per component

**CPA.** Fork branch `candidate/v8.0.77-capacity-sse` on upstream tag v8.0.77. The first candidate (`6f822eaa`) was blocked by review. The replacement `3d1f7004` passed patch tests, SSE replay and a delta review (APPROVE). Production acceptance: long tool-call streams of 1500 and 3500 words with 72 s idle completed. A forced 6000-word tool call ended after 120 s with `upstream request timed out after 120000ms`. That message comes from the `xl` upstream relay, and the failure arrived as a proper `response.failed` event (see follow-ups).

**Sub2API.** Rebased onto the exact tag v0.2.22 as branch `integrate/capacity-0.2.22-r2`. Operator decisions:
- WS keepalive PR #3172 dropped this round.
- The Codex passthrough "instructions required" 403 removed, following upstream.

Fork CI failures were all in downstream patches and were fixed:
- golangci-lint findings; 5 dead functions removed.
- Process-wide allocation counters made 8 bounded-allocation tests flaky; those tests now run alone in a separate process.

One real regression was found and fixed: upstream's `instructions` echo copies the text several times, which pushed memory to 10.2× the request size against an 8× budget. Reading it in place brings that to about 1.0–1.2× (192/192 cases).

Independent review: APPROVE-WITH-NOTES. Build, vet, lint, 62 unit packages and the focused tests passed.

Isolated migration gate (21/21 PASS): a fresh production dump was restored into a `--network none` pod (Postgres 18.7 + Redis 8.10.3 + candidate; only `lo`, no routes, no published ports).
- Migrations 246–251: 8 rows with the expected full filenames, including the duplicate `249_`/`251_` prefixes; checksums match the lane report; all 296 old rows unchanged.
- Row counts, ID hashes and full-record hashes unchanged. The only differences were the new columns, all at their defaults.
- Timings: 249 (new `usage_logs` index, 1.79 M rows) 1.5 s; 250 about 11 ms; the others 4–21 ms.
- Migration 246 flipped **0 groups**: there are no gemini/antigravity groups.
- Rollback rehearsal: the previous image `56557012…` starts on the migrated schema (`/health` 200, no errors), so an image-only rollback is safe.

Production migration ran inside the guarded apply. Postgres showed no long transaction just before it.

**AISIX.** A v1.10.0 port of the 19 downstream commits. Review history:
- First review: BLOCK (two findings), fixed in `e59ee6a1`.
- Delta review: BLOCK on one MEDIUM. A 413 whose body stalls on non-streaming OpenAI Chat / Vertex paths was retried and failed over. The deployed af9f9147 had the same behaviour, so the gap predates this port.
- `67343036` adds `timeout_keeping_413` for all five bridges.
- Delta-2 review: APPROVE. Stalled-413 probes return 413 after one primary call in about 0.27 s; the 48-request boundary matrix passes; the whole-call deadline is not reset; 2,734 tests passed.

Image-only change; `config.yaml` / `resources.yaml` kept.

**APISIX ×2.** The 3.19.0 → 3.20.0 changelog was reviewed against the plugins we use: compatible, with no configuration change. The front apply failed four times, each time while pulling the image (registry TLS handshake timeout), before activation. The fifth attempt succeeded.

**ZCode 4.9.0.** An offline gate on the prestaged image passed (health 200). 4.9.0 serves only `glm-5.3` and `glm-5.3-flash`. The operator accepted losing `zcode/glm-5.2` and asked for no routing change. `cpa-model-sync` removes `zcode/glm-5.2` within 600 s. The aggregate `glm-5.2` fails over to other providers and still completes.

## Manual windows (no deploy profile)

All three windows followed the same pattern:
- Back up `.env` and both Compose files under `.ai-ops-state/update-20261118/` on the deployment root.
- Require that the candidate `.env` differs only in the intended key(s).
- Require that the `env -i` effective render differs only at the intended `image` paths. For netns, the top-level `x-netns-owner` anchor is also allowed.
- Sample established TCP connections.
- Stop the `ai-gateway-recovery` OpenRC loop during the window and restart it on exit.
- Recreate with `--no-deps --no-build --pull never`, verify, and restore the backup `.env` automatically on any failed check.

- **Tunnel pair (tun2proxy v0.8.7).** Recreated `provider-sidecar-tunnel` and `provider-sidecar`. Run 1 aborted on its precondition check (busybox diff format). Run 2 activated v0.8.7, then rolled itself back on a false positive. The awk guard compared container IDs as numbers, and `8912302e8564` parsed as scientific notation, so `cpa-model-sync` looked changed. Run 3 passed: sidecar and tunnel restarts 0. Cost: two extra pair recreates (~20 s each, ZCode models only).
- **Postgres + Redis.** Before activation: a fresh custom-format `pg_dump` (307 MB, TOC readable) and a Redis `BGSAVE` plus data copy. Postgres recreated first (healthy in 9 s), then Redis (healthy in 9 s). Socat relays use static IPs and fork per connection, so they were not recreated.
- **netns guards (rolling).** Run per unit (a guard plus every service with `network_mode: service:<guard>`), least critical first:
  - model-catalog, apisix-models, the three host relays;
  - aisix, cpa, sub2api, the keepalive trio;
  - the APISIX front last.

  Each unit had to show: guard running on the new image with no default route; dependents healthy; no unrelated container changed; public probes passing. Before arming, two ablations ran:
  - (a) a throwaway 3.24.4 container with the exact guard command and caps reached `sleep` as `nobody` with no default route;
  - (b) a precheck against the untouched stack passed.

  Two probes needed one retry each. After the sub2api-host unit, a ZCode turn returned HTTP 200 with an empty stream. After the apisix-host unit, a `gpt-6.1-sol` read timed out. Both passed on retry, and no unit failed. cloudflared ERR lines, 10:00–10:30:

  | Minute | Count | Type | Coincides with |
  | --- | --- | --- | --- |
  | 10:06 | 2 | client-canceled | host-relay units |
  | 10:10 | 2 | client-canceled | host-relay units |
  | 10:19 | 12 | origin-unreachable | APISIX front recreate |
  | 10:21 | 2 | client-canceled | after the window |

  The 10:06 and 10:10 lines coincide with the host-relay units and the two probe retries. So even the "least critical" units caused brief client-visible stalls. Whether every canceled request was a window probe or real traffic was not established.

## Backup and rollback boundaries

- Guarded components: `rollback.yml -e component=<name>` with a fresh digest, to the `# rollback:` pin.
- Sub2API: image-only rollback to `56557012…` was rehearsed on the migrated schema. Migrations 246–251 stay applied (they are additive; 246 changed nothing).
- Postgres/Redis: restore the backup `.env` (minor-version downgrade on the same data directory). The dumps and the Redis copy are kept under the window backup directory.
- netns guards: the window script has a `rollback` mode that restores the backup `.env` and recreates the touched units in reverse order.
- The controller-side `pg_dump` used for the gate is kept in protected scratch (0600) and was not committed.

## Deviations and incidents

- **ZCode rollback against the operator decision.** 4.9.0 first went live at 06:12. The agent then rolled it back to 4.8.1 at 06:14 because `zcode/glm-5.2` disappeared, overriding the explicit "go live directly" decision. After the operator restated the decision, it was redeployed at 08:52 with no routing change.
- **Sub2API fork tag push.** The global `push.followTags=true` pushed upstream tags v0.2.20–v0.2.22 to the `xz-dev/sub2api` fork during branch pushes. The Release workflow runs they triggered were cancelled. Later pushes used `--no-follow-tags`. No upstream repository was pushed to.
- **Tunnel window false rollback** (above): two unnecessary recreates. Fixed in the window script (string comparison of IDs); the later windows used the fixed logic and a precheck mode.
- An earlier Sub2API candidate (r1, `f5704b20`) still contained the keepalive patch and was never deployed.

## Follow-up: GPT-6.2 Sol and Astra (operator request)

**Gateway listing (12:53–12:55 UTC).** Before the change, the CPA native manifest already had `codex/`, `xl/` and `congee/` `gpt-6.2-sol` / `gpt-6.2-astra`, plus the bare IDs. They were missing from the public catalog.

The change follows the [gpt-6.1-terra precedent](add-gpt-6-1-terra-2026-11-01.md):
- **AISIX:** added aggregates `gpt-6.2-sol` and `gpt-6.2-astra` with ordered failover `codex/` → `xl/`, copying terra's routing policy (zero retries, one fallback, `retry_on_429`, fallback statuses 400–404). Added four direct declarations that copy terra's cooldown and provider keys.
- **model-catalog:** added two static slugs with a 500k context, in the private profile and in the tracked example (`f59ed91`).

The operator said "fallback 一样". This was read as terra's codex → xl. `gpt-6.1-sol` only uses codex, because `xl` does not serve 6.1-sol; both providers serve 6.2.

Verification:
- Both `deploy-config.yml --check --diff` previews were add-only: AISIX 73 → 79 models, catalog +2 slugs, 0 removals, no orphans. Both AISIX callers are `allowed_models: ['*']`.
- Only the `aisix` and `model-catalog` containers changed.
- Public `/v1/models` went from 358 to 360.
- Both models completed through `codex/`, and the `gpt-6.1-sol` / `gpt-6.1-terra` regression probes completed.
- model-catalog `npm test` and `test-ansible-model-catalog-config.sh` passed.

**Sub2API GPT-6.2 support (operator decision: port and deploy).** Upstream `6107cf1748` ("support GPT-6.2 Sol and Astra") adds pricing, Ultrafast entitlement, reasoning handling and alias normalization. It was not in the v0.2.22 tag, so `cherry-pick -x` put it on the new fork branch `integrate/capacity-0.2.22-r3`. Commits:
- `03197517cb`: the cherry-pick.
- `cbe35e093b`: classifies the reintroduced bounded raw-body account read in `TestRawBodyPathAccountReadsAreClassified`.
- `f249326936`: runs `TestPassthroughNormalizersAllocateLittleWhenUnchanged` in its own process, the same flake class and fix as `a01da92f4f`.

Gates, all passed before the deploy:
- Fork CI, ghcr-ci and Security Scan green on `90cee09934`.
- Image `ghcr.io/xz-dev/sub2api@sha256:e26d4b7dbef57fb55ee4e635a7010edd319245ead53265d5b4563e2e31f68f39`; revision label = source. Compared with `8e329cb7`, only the labels differ. No migrations.
- Independent delta review: APPROVE-WITH-NOTES. The cherry-pick has upstream's stable patch ID; capacity charges, request-scoped errors and passthrough/WS precedence are intact. 9,618 tests passed; lint 0 issues. Note: `billing_inflight_reservation_test.go:464` fails under host-default concurrency but passes alone and in the suite.
- Ladder run `20261118T164720Z-605106`: 10/10 rungs, with the Sub2API peak at 80.6 MiB (93.7 MiB before).

The lock was promoted together with the tracked `SUB2API_IMAGE` default.

Guarded deploy at 17:23–17:25 UTC, first attempt:
- Only `sub2api` changed; it is healthy with 0 restarts.
- schema_migrations stayed at 304.
- Controller `.env` = live `.env`.
- gpt-6.2-sol/astra, gpt-6.1-sol, claude-opus-5-5 and zcode probes completed.

Rollback: image-only, to `8e329cb7…`.

**Zakk access (operator request).** User Zakk (id 5) has one key (27) in group 20, whose model allowlist is disabled. The group's only account is 10 "CPA - Zakk", whose `model_mapping` acts as the allowlist.

The change went through the admin API. `PUT /accounts/:id` replaces every non-sensitive credentials key with what is sent. So the full non-sensitive credentials from a fresh GET were sent back unchanged, except `model_mapping` +2 (`gpt-6.2-sol`, `gpt-6.2-astra`, each mapped to itself). A dry run came first; the read-back asserted the key set, every other value, groups `[20]`, WS mode `off` and status.

The admin account test on account 10 succeeded for both new models and for `gpt-6.1-sol`. Zakk's path is Sub2API → CPA direct, using CPA's own routing for the bare IDs, not the AISIX aggregate.

## Follow-up: Grok 4.7 access for Zakk (2026-11-19)

The operator requested `grok-4.7` as the last fallback for the bot using Zakk's
Sub2API group. The gateway-side change was limited to account 10 "CPA - Zakk":
add `{"grok-4.7": "grok-4.7"}` to `credentials.model_mapping`.

**Registration.** Before the write, CPA's native `/v1/models?client_version=1`
and plain `/v1/models` both returned HTTP 200 and listed `grok-4.7` and
`supergrok/grok-4.7`. Catalog presence was checked separately from inference.

**Method and read-back (05:17 UTC).** Reused a copy of the earlier Zakk grant
script, changing its additions to this one identity mapping. The dry run found
9 existing entries and exactly one addition. Apply performed another fresh GET,
required the reviewed mapping baseline, and sent the complete non-sensitive
credentials through `PUT /api/v1/admin/accounts/10`. The single PUT succeeded;
its GET read-back passed all assertions. Existing entries, the credentials key
set and every other credentials value, all `extra` fields, groups `[20]`, WS
mode `off`, status `active` and `schedulable` were unchanged. The script skips
PUT if the requested mapping is already present.

The read-back contained exactly 10 mappings:

```json
{
  "devin/swe-2": "devin/swe-2",
  "glm-5.3": "glm-5.3",
  "glm-5.3-flash": "glm-5.3-flash",
  "gpt-6-astra": "gpt-6-astra",
  "gpt-6-luna": "gpt-6-luna",
  "gpt-6.1-sol": "gpt-6.1-sol",
  "gpt-6.1-terra": "gpt-6.1-terra",
  "gpt-6.2-astra": "gpt-6.2-astra",
  "gpt-6.2-sol": "gpt-6.2-sol",
  "grok-4.7": "grok-4.7"
}
```

**Acceptance (05:17 UTC).** One admin-authenticated
`POST /api/v1/admin/accounts/10/test` for `model_id: "grok-4.7"`, with the prompt
"Reply with the single word OK.", returned HTTP 200. Its SSE events were
`test_start`, `content` and `test_complete`; the parsed `test_complete` event
had `success: true`. This verifies the bare ID through Sub2API → CPA, not just
its catalog registration; it does not exercise the bot's fallback order. No
temporary API key was created. No other account, group, routing, CPA configuration
or deployment was changed.

## Follow-ups (not done this round)

1. **Compatibility lock: done the same day.**
   - The lock was regenerated from a passing local ladder run (`20261118T115318Z-3515308`, 10/10 rungs, 0 restarts). The method was unchanged from the 2026-11-09 lock: the same no-swap wrapper and mock upstreams only.
   - The run covered production's Sub2API `8e329cb7…`, AISIX `9721ff10…` and CPA `c938d0b7…`, plus Postgres 18.7, Redis 8.10.3 and alpine 3.24.4.
   - The tracked Sub2API, AISIX and CPA defaults now follow that lock.
   - Memory: Sub2API v0.2.22 peaks 25–40 MiB higher (94 MiB lock peak against its 320 MiB limit); AISIX and CPA peaks went down.
   - `mock_base` records the index digest `3f4d6b32…` that the local `alpine:3.24.4` tag resolved to. It contains the same amd64 manifest as the production pin `f59abe65…`.
   - The nightly lock (1 GiB rungs) was not refreshed.
2. **`glm-5.2` fallback chain is thin.** `zcode/glm-5.2` is gone (accepted); `sensenova/glm-5.2` returns "no available model instance"; `commandcode` returns 401 "API key has expired". The aggregate still completes through a later target.
3. **Redis has no `requirepass`.** `REDISCLI_AUTH` is set, but the server runs without a password, so `redis-cli` prints `AUTH failed`. Redis is on an isolated two-member network. This gap existed before this round.
4. **CPA 120 s limit.** The forced 6000-word tool call fails at the `xl` relay's 120 s timeout. It is likely upstream behaviour but unproven.
5. **cpa-model-sync** reports 5 failing channels (zakk, kimi-cn-api, senseaudio, anthropic-api, axis). No pre-change baseline was taken.
6. **AISIX** warns that the heap-profile auto-dump directory is not writable (automatic heap dumps are off).
7. **Deploy previews always show `config_diff: true` when `.env` changes**, because `.env` is part of the candidate hash set. Expected; worth a note in `docs/ansible-operations.md`.
8. **Manual-window scripts** for the tunnel pair, Postgres/Redis and the netns guards were round scratch. Consider tracking a generic version with a precheck mode.
9. **Responses WebSocket 1006 on long agent sessions.** Two subagent workers whose model traffic uses this gateway's Responses WebSocket path died with `Responses WebSocket closed (1006)` (about 13:2x and 16:3x UTC). Probes and readbacks before and after were healthy. Correlate with front/bridge logs.
