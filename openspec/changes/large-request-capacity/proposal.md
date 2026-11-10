## Reconciliation status — 2026-11-09

The [final deployment and cleanup record](../../../docs/records/maintenance-capacity-closeout-2026-11-09.md)
is current-state authority. PR21/PR22 are merged/deployed and operationally
accepted; full capacity acceptance is **not complete**. 245 MB admission is
best effort (latest samples admitted/refused/refused), realistic 8 MiB production
success is unobserved, and corrected overcap production behavior is unretested.
Keep this change open; do not sync unfinished requirements to main specs/archive.

Published Sub2API source `02918ea957f0` is v0.2.19-based, with native-path
`k_skel=8` (not the earlier 6/17 projections) and per-class allocation charges:
Responses 114, fallback 107, other-platform Responses 143, Chat 260, Messages
159, count_tokens 156, Gemini 147, web_search/embeddings/rerank 65, unmeasured
260. The lock still declares historical `k_skel=17`; its factors are not newly
measured production results. Do not rewrite lock provenance to conceal this.
Production Sub2API is 512 MiB memory / 320 MiB actual swap / CPU 1.5;
the PR ladder is 320 MiB/no swap. Production cap remains 256 MiB; 1 GiB is
nightly/throwaway only. CPU parity and TLS-bump-path deviations remain explicit
in the final record. Historical M0 measurements and proposed acceptance below
are retained as history/goals, not fresh runtime claims.

The host MemAvailable gate's anonymous+kernel charge does not double-count
available dirty/writeback pages. CI separately measures the conservative
working set `anon + file_dirty + file_writeback + kernel` for keeper/AISIX,
not raw memory.peak alone; clean reclaimable file cache is excluded.
Public image defaults now follow the final lock, but shared ledger/spool mounts,
flags and mount-mode/same-device validation remain a **public-template blocker**.
No private deployment infrastructure is copied here to complete task 9.1.

## Why

On 2026-11-04 a Pi session failed with `Connection error.`. The client used WebSocket continuation correctly and sent a 1.55 MB delta. Keeper expanded the stored history into one WS frame of about 17.9 MB. Bridge b482 rejects frames larger than `MAX_MESSAGE_BYTES` (default 16 MiB), so it closed the connection with code 1009. Keeper logged 502 and sent the client only a bare close frame. The client got no error event.

The capacity audit of 2026-11-04 measured each hop with a mock upstream and cgroup `memory.peak`. It shows that this failure is not the only one:

- **The caps are inconsistent:** firewall APISIX 16 MiB, bridge 16 MiB, keeper 32 MiB (hard limit 64 MiB), apisix-models 64 MiB, Sub2API 256 MiB.
- **Three hops hold the whole body in memory many times over.** At 256 MiB the peaks are Sub2API 1.66 GB, CPA 1.36 GB and AISIX 0.81 GB. One 256 MiB request needs about 2.6–3.2 GB of RAM, and the host has 2.06 GB.
- **A single 64 MiB body is fatal** in the no-swap red test: Sub2API (320m) and CPA (480m) are OOM-killed and send an empty reply. Production CPA (1g plus swap) carries 64 MiB but not 256 MiB.
- **Four hops answer an over-limit or inner failure with a bare close or an empty reply:** bridge (1009), keeper (bare close), Sub2API and CPA (OOM, empty reply).
- **Disk is tight.** The host has 4.66 GB free on a 31.6 GB disk, shared with the keeper store (1 GiB cap). There is no disk expansion, so large requests are best effort.

The user set the target: a single 245 MB request body must work smoothly, and a 1 GB body must work marginally. "Marginally" means a nightly CI run only. Production keeps the 256 MiB cap and returns an explicit 413 for a 1 GiB body. "Smoothly" means one 245 MB request succeeds whenever the host has room for it; concurrent large requests are refused explicitly. The design must hold on the current host, with no bigger host and no bypass topology.

Providers set their own ceilings (provider documentation, fetched 2026-11-04; exact bytes, decimal reading):

| Provider route | Max request (bytes) |
|---|---|
| Anthropic first-party Messages | 128,000,000; larger gets 413 `request_too_large` |
| Anthropic on Bedrock | 20,000,000 |
| Anthropic on Google-managed platforms | 30,000,000 |
| Anthropic on other partner platforms | 32,000,000 |
| ChatGPT codex backend, third-party relays | unknown |

OpenAI's 50 MB limit applies to combined file inputs, not to the request size, so it is not a gateway ceiling. The target for each route is `min(256 MiB, provider ceiling)`. Above the ceiling, the gateway returns an explicit 413 that names the limit.

Body composition was measured on the user's heaviest real sessions:
- Text-heavy sessions are 70–96 % small strings (< 16 KiB), with no images.
- Image sessions are about 80 % base64.
- The context window bounds text requests to a few MB, so any request above about 32 MB is dominated by media. Externalizing media and file strings therefore covers the large-body case.

## What Changes

- **One end-to-end cap of 256 MiB.** It applies to the client body and to keeper-expanded history, and it replaces every per-hop cap. A larger body gets an explicit 413 or a protocol error event at the first hop that sees it.
- **Per-route provider ceilings.** A route whose known ceiling is below 256 MiB rejects a larger body with a 413 that names the ceiling. Routes with unknown ceilings keep the 256 MiB cap. They are not probed with oversized requests, and the provider's own 413 or error is relayed explicitly.
- **Host-wide admission gate for bodies of 64 MiB or more.** Keeper first authenticates the caller, then admits each large request against real free disk (minus the 1 GiB reserve and the keeper store headroom) and real MemAvailable (minus 200 MiB and a 32 MiB margin). It reserves the request's measured footprint across all hops in one shared ledger that only keeper can write; the inner hops mount it read-only. A request that does not fit gets an immediate 503 `capacity_busy` (with `Retry-After`), 507 or 503, with no queue. Every hop holds a lease on the ticket, and the reservation is released only after every spool is closed. The initial fit projection is historical; final admission is best effort and not always granted. Smaller bodies carry no ticket.
- **Inline-bytes limit and per-hop memory charge for every request.** The bytes left inline after media externalization are limited to 16 MiB per request (8 MiB for ticketed requests); above that, the hop returns a 413 that names the limit. Each JSON-parsing hop also charges every request against its own cgroup headroom and refuses with 503 `insufficient_memory` instead of being OOM-killed. Text-heavy bodies above 16 MiB are therefore refused by name, not carried into an OOM.
- **Configuration hops.** Set firewall APISIX and apisix-models to `client_max_body_size 256m` with `proxy_request_buffering off`, so they stream and keep no temp copy. Set bridge `MAX_MESSAGE_BYTES` and `MAX_BODY_BYTES` to 256 MiB. The bridge already spools.
- **Keeper spool redesign.** The base is the parked P15 tree (manifest `03fd6345…`), which folds in the P-series in-memory fixes. This change:
  - raises `MAX_REQUEST_BYTES`, the history hard limit and `STORE_PIN_MAX_BYTES`;
  - hosts the gate;
  - checks the expanded size before sending;
  - removes the `maxHistoryItems = 8192` item-count cap, which Milestone 0 showed refusing text bodies with a misleading 400;
  - relays an inner close or failure as an explicit error event.
- **Shared "skeleton + blob spool" pattern for Sub2API, the CPA fork and AISIX.** Each component writes allow-listed media and file string values to an on-disk spool. All other strings stay inline. Its existing JSON code runs on the small skeleton, and at egress the blobs are spliced back, byte-identical to today's output. Retries re-read the spool. Paths that still need the full body, including `/responses/compact`, keep their in-memory behaviour up to a measured limit of at most 16 MiB and refuse larger bodies with a named 413. Tier B (text-heavy bodies) remains an optional non-goal.
- **Sub2API error passthrough.** Gateway-generated 413, 503 and 507 capacity errors from inner hops, and every provider 413 for every request size, are request-scoped. They get no account failover, no retry and no message rewrite. Trade-off: an account with a smaller private ceiling no longer fails over to another account.
- **Third-party patches.** Each patch has a config switch that defaults to off upstream and is on in this deployment, and each goes upstream as a PR.
  - Each component runs a digest-pinned fork build. It returns to an official image only once every carried patch is released.
  - The CPA fork is based on the production tree, `v8.0.51` plus the PR #7134 backport.
  - If an upstream declines, the fork becomes long-lived and is rebased periodically under the CI gate.
- **Real resource accounting.** Spools are anonymous `O_TMPFILE` files on disk, never on tmpfs, with mode 0600. Every exit path closes them, and their page cache is bounded. `memory.peak` stays at least 32 MiB under each `mem_limit`. `GOMEMLIMIT` and OOM tolerance do not count as accounting.
- **Explicit errors at every hop.** Every hop reports an over-limit, refusal, timeout or inner failure as an error in the client's protocol. No hop sends an empty reply or a bare close.
- **Time budget.** `upstream.stream_timeout_ms` stays 120000. Milestone 0 measured that it also covers AISIX's upload of the body, so 245 MB needs about 20–25 Mbit/s sustained egress. The record states the measured egress upload rate and the largest body that fits the window. A timeout is relayed explicitly. Whether AISIX should start the 120 s only after the body is sent is a pending user decision.
- **CI/CD capacity gate.** A compose stack runs the full chain against a mock upstream behind Squid, with no paid inference.
  - Every run tests exact rungs: 1 MiB, 16 MiB, just under and at 64 MiB, 245 MB, 256 MiB, and over the cap, plus text-heavy rungs at 16 MiB, 32 MiB, just under and at 64 MiB.
  - It also runs concurrency, memory-admission, per-hop memory, authentication-first, refusal-during-upload, disk-floor, release, inner-error, provider-413, fault and throttled-egress steps.
  - A nightly run tests 1 GiB.
  - When all checks pass, the gate publishes a digest-pinned compatibility lock with the measured footprint factors.
- **BREAKING (operational):** CPA, AISIX and Sub2API move to digest-pinned fork builds named by the compatibility lock.
- **Milestones without an interim state.** Milestone 0 proves the red baseline in CI. Milestone 1 builds the hops we own (keeper spool and gate, bridge, APISIX, error relay) and accepts them locally only. Milestone 2 adds the third-party patches. Milestone 3 publishes the lock and deploys everything in one window. Nothing reaches production earlier (不过渡).

## Capabilities

### New Capabilities

- `large-request-capacity`: the end-to-end 256 MiB cap and per-route ceilings; the host-wide admission gate; flat memory through the skeleton + blob spool pattern; spool storage and cleanup rules; keeper transparency constraints; the time budget; the CI capacity gate and compatibility lock.

### Modified Capabilities

- `client-visible-upstream-errors`: every hop, including keeper and bridge, reports over-limit, refusal and inner failures explicitly. Keeper relays an inner WS close as an error event. Sub2API passes inner gateway errors and every provider 413 through without failover.
- `apisix-cpa-gateway`: the apisix-models body backstop changes from 64 MiB to 256 MiB, and request buffering is turned off.
- `aisix-model-routing`: the AISIX memory requirement changes from one 64 MiB body to one 256 MiB body with flat memory, under the admission gate. The pin follows the compatibility lock.
- `cpa-first-catalog-routing`: CPA runs a digest-pinned fork of the production tree (`v8.0.51` + PR #7134) plus the body-spool patch. It returns to an official image only once every carried patch is released.

## Impact

- **Config:** `services/firewall/apisix/config.yaml`, `services/routing/apisix-models/config.yaml`, `compose.yaml`, `.env.example` (caps, request buffering, spool and ledger volumes, image pins), and the Ansible roles that render them.
- **Our code:** keeper (P15 base, admission gate) and bridge (limits, ticket, error event).
- **Third-party forks:**
  - Sub2API (v0.2.19-based `02918ea957f0`; spool, per-class accounting and inner-error passthrough);
  - CPA (`v8.0.51` + PR #7134 base);
  - AISIX (the repository's `1.8.0-pr1387` build line).

  Each has a patch branch, an upstream PR and a fork image.
- **Disk and memory:** a ticketed request reserves about 7× its size of disk across the hops (about 1.75 GiB at 256 MiB, from measured and target factors) on top of the 1 GiB reserve and the keeper store headroom, and about 600 MiB of memory against 669 MiB available today. This was the initial projection, not a final admission guarantee.
- **CI:** a new capacity workflow and release artifact next to `.github/workflows/validate.yml`. The nightly runner needs at least 12 GiB free.
- **Unchanged:** AISIX `upstream.stream_timeout_ms: 120000`, Squid egress, ingress authentication, the namespace topology, account/pool/OAuth state, per-WebSocket ordering, session affinity, and Cloudflare edge limits (out of scope).
