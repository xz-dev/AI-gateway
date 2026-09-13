# Design: CPA-first catalog routing

## Context

Live serving baseline (parent-supplied operational evidence, not re-queried): production accounts 1/4/5/6 serve directly from AISIX at `base_url=http://aisix:3000/v1`; AISIX forwards to its configured CPA targets. The internal `apisix-models` APISIX entrance and its Sub2API-side relay (`sub2api-cpa-relay`) are deployed and reachable — retained connectivity — but carry no active serving placement today. The models-enricher sidecar already collects both raw inventories: CPA native manifest (pre-identity-filter IDs) for the catalog baseline and the optional AISIX `/v1/models` for the supplement difference. Catalog evidence (observations, not acceptance constants): CPA standard and `client_version=1` both 223 raw IDs, AISIX 37, overlap 21, AISIX-only 16, enriched 239. The operator selected catalog-based routing and explicitly superseded the earlier AISIX-first/global-wildcard approach.

Constraints carried from the confirmed design: classify before inference; no cross-backend retry on 429/5xx/timeout/stream error scoped to the new entrance; unknown availability ≠ empty membership; preserve streaming, bodies, provider errors, Sub2API authorization, and Pi non-chat filtering; keep the mechanism minimal (one snapshot owner, bounded lookups, atomically published generations; no distributed 2PC/ack/index-replica framework; no entrance-side result cache).

## Goals / Non-Goals

Goals:

- One classification seam at the internal APISIX entrance selecting between the existing CPA and AISIX upstreams on a bounded, enumerated HTTP route scope.
- A four-outcome index contract (`cpa`/`aisix`/`not_found`/`unavailable`) so authoritative absence and unusable sources map to distinct client errors.
- Reuse the enricher's existing source collection, `readCache` stale-fallback policy, and singleflight build discipline; add only what routing needs (retained raw memberships + the index view + a startup/periodic lifecycle).
- Honest failure classes: model-not-found only under authoritative snapshots; 503-class otherwise.

Non-Goals:

- No change to AISIX's internal routing policy, cooldowns, target inventory, or combo retry budgets.
- No catalog metadata synthesis changes (field precedence, identity admission, `bare_models_takeover`, static/custom rules unchanged).
- No new persistent store, no new service, no Sub2API source/image change, no public exposure of the index, no duplicate model registrar.
- No inference-status CPA→AISIX retry; Sub2API pool retries (pool_mode=true, retry_count=1 kept) remain untouched.
- No production rollout in this change (separate approval; see Migration Plan).
- No reachability assumption for the four remaining AISIX-only direct declarations (axis GPT trio, `z-ai/glm-5.3-flash`); they route to AISIX because membership says so; upstream health is verified later, and a standard-list listing is not proof of invocation eligibility.
- No upgrade-time model inference or raw-frame classification for the legacy direct-WS transport: WS client ingress and first-frame/`http_bridge` processing stay at Sub2API, and the classifier sees only the resulting HTTP request.

## Decisions

### D1: Reuse `apisix-models` as the classification and forwarding seam

The retained internal instance sits on the Sub2API → backend edge (`sub2api-cpa-relay` feeds it) and owns the models route split. Adding upstream selection here reuses deployment isolation conventions (dedicated relay pairs, two-member internal networks, no wildcard host binding) instead of inventing a new gateway hop. Making it the serving path is a separately approved account migration — retained reachability is not serving placement (see D6).

Alternatives: a new dedicated router service (new container, networks, failure surface — rejected); routing inside Sub2API (requires Sub2API code changes, rejected); routing everything inside AISIX (the superseded AISIX-first premise).

Flows after migration: inference Sub2API → internal APISIX → CPA **or** AISIX; AISIX → its configured CPA targets (unchanged). Raw source collection stays direct: enricher → CPA (`enricher-cpa-relay`) and enricher → AISIX (`enricher-aisix`), never through the entrance — the non-recursion prohibition applies to raw source collection only. Client catalog delivery through the entrance is intended and retained: the model-list-intersection Lua path (basic leg = Sub2API; original leg = sidecar → entrance → enricher) serves `GET /v1/models` and remains the public front standard listing with its Sub2API authorization/projection intersection.

### D2: One in-process snapshot owner — the models-enricher

The enricher already fetches exactly the two raw inventories routing needs (`CPAClient.NativeManifest` → `extractCPAIDs` pre-filter baseline; `AISIXClient.FetchModelIDs` → full AISIX list with validation and cache semantics). Decision: the enricher becomes the routing-snapshot owner in addition to the manifest builder.

- Retain `C` = exact original CPA inventory IDs (same capture point as today's `originalCPAIDs`), `A` = accepted AISIX IDs. Keep them distinct; never reconstruct either from `mergeManifest` output (that would make AISIX aliases CPA members).
- Publish atomically: one immutable snapshot value (generation counter + per-source availability + membership sets) swapped under the existing lock discipline; concurrent readers see old-or-new, never mixed. This is deliberately per-process consistency only — no cross-request simultaneity is promised.
- Stale eligibility follows the existing `readCache` policy: 502/503/504/timeout may fall back to last-good; auth failure or validation failure invalidates rather than resurrects. Valid empty `{"data":[]}` overwrites. Unknown availability is published as *failed*, never as empty.
- Collection phase isolation is preserved: the CPA mandatory phase and optional AISIX phase stay separate, so a failed optional AISIX collection preserves successful CPA output and known CPA routing decisions in the published generation.

Alternative: a separate routing-index sidecar duplicating source collection (rejected: second fetch path, second cache policy, drift risk). Alternative: APISIX fetching catalogs itself per request (rejected: hot-path full-catalog fetch, unbounded).

### D3: Snapshot lifecycle — startup init + bounded periodic refresh

The existing build pipeline runs only when a catalog request arrives; routing must not depend on that. The owner adds a minimal lifecycle: initialize the routing snapshot at startup, then refresh on a bounded periodic cadence aligned with the existing cache TTL, reusing the existing collector and singleflight discipline (one in-flight refresh; concurrent lookups read the published generation). Failed refreshes retry on the same bounded cadence — no tight loop, no new store, no dependency on catalog-client or lookup traffic. This makes cold readiness and recovery truthful when only inference traffic exists: `unavailable` until the first successful collection, then bounded-cadence recovery.

Catalog projection caching becomes generation-pinned: each cached catalog body (Codex manifest, standard list, models-table) is keyed by raw generation + client_version/format, and the builder pins the raw snapshot it reads while building. On a new generation, stale completed projections cannot be served as current, and a late old-generation build cannot overwrite the current entry; in-flight readers may finish serving their pinned generation. No global simultaneous multi-request guarantee is claimed.

### D4: Bounded internal routing-index view — four outcomes

One internal HTTP endpoint on the enricher (network-restricted, like its existing listener): `GET /routing-index?model=<exact ID>` → `{"decision": "cpa"|"aisix"|"not_found"|"unavailable", "generation": N}` (plus a bounded reason detail for `unavailable`). Semantics:

- `cpa`: ID ∈ C (overlap included).
- `aisix`: ID ∉ C ∧ ID ∈ A, both memberships authoritative.
- `not_found`: both required memberships authoritative for the generation and ID absent from both — maps to the served protocol's model-not-found error.
- `unavailable`: a membership required to decide the ID is failed/not authoritative (e.g. only C authoritative and ID ∉ C; or C unusable) — maps, together with any lookup transport failure, to a 503-class response retaining the existing public opacity layer.

Known C IDs answer `cpa` even when only A is unavailable; an unusable C never yields an AISIX-only guess. Bounded body, bounded timeout; sets stay in the owner. The entrance performs exactly one bounded lookup per inference request and keeps **no entrance-side result cache** — no second cache or generation-synchronization mechanism.

Alternative: push-style index replication into APISIX shared dict (rejected: replica coherence/ack machinery — the forbidden distributed index-replica framework).

### D5: Entrance selection mechanics — bounded route seam, selector precedence

The selector is a scoped set of high-priority internal entrance routes over the enumerated model-bearing JSON HTTP inference paths — at minimum `POST /v1/responses` and `POST /v1/chat/completions`, which also covers what Sub2API's `http_bridge` emits as HTTP inference calls (WS client ingress and first-frame/`http_bridge` processing remain at Sub2API; the classifier sees the resulting HTTP request). The seam boundary is path+method (+ the internal source host), not a body heuristic over all traffic.

Selector routes take precedence over the legacy HTTP alias-rewrite (`filter_func` model-rewrite) and CPA-direct routes for those paths and sources, so the original requested ID reaches the selected backend untouched; conflicting legacy HTTP alias rules are removed from inside the classified scope, while legacy alias/WS consumers outside that scope keep compatibility behavior. The legacy direct-WS route (`GET /v1/responses` upgrade → `ws-alias-proxy`) is outside the classifier: no upgrade-time model inference, no raw-frame classification, existing behavior preserved.

Non-classified endpoints keep today's behavior — this is a deliberate scope boundary, distinct from failed classification. On a classified path, a malformed body or a model field that cannot be extracted fails explicitly with a protocol-shaped error; it never silently falls through to CPA or another backend.

Selection picks one upstream and the configured per-upstream service credential (CPA keeps `__CPA_API_KEY__`-injected header; AISIX uses the caller key Sub2API already sends — accounts authenticate to AISIX that way today; the entrance forwards identity headers as now, without inventing credentials). Exactly one backend per request; the selection code adds no retry, no fallback list, and does not consume backend errors — they propagate through the existing passthrough and error-propagation paths (see parallel change `fix-sub2api-error-propagation` for the client-visible error contract; this change neither implements nor assumes it). Keepalive reuse of the `resty.http` pool pattern (as `model_list_intersection.lua` does) bounds lookup cost.

### D6: Real catalog endpoints — standard list and Codex split

`GET /v1/models` on the unified internal entrance always reaches the Go sidecar: absent or empty `client_version` returns the standard OpenAI projection `{"object":"list","data":[{"id":...,"object":"model"}]}` (exact ID bytes, existing admission rules, canonical build version `1`); non-empty `client_version` returns the existing Codex `models[]`/slug manifest, retaining the caller's version for channel inventory. The raw CPA `NativeManifest` fetch stays pinned to `client_version=1`; models-table keeps its current inventory version (`v0.65.0`). The old "no-version never reaches the sidecar / proxies to CPA" contract is superseded in the `apisix-cpa-gateway` and `codex-models-enricher` deltas. The public front standard listing still flows through Sub2API authorization/projection and the existing parameterized intersection; standard-list visibility and upstream invocation remain distinct.

### D7: Account placement, rollback, and parallel-change coordination

Accounts 1/4/5/6 serve from `http://aisix:3000/v1` today. The future, separately approved migration updates only the owned `base_url`/necessary routing fields computed from the latest complete credentials object, preserving unrelated fields. Rollback restores the prior AISIX serving behavior for affected accounts (prior direct-AISIX placement included) while preserving independent peer configuration edits — a route-only CPA-catch-all revert does **not** restore prior service for AISIX-only models and is not the rollback plan.

Interoperability constraints (recorded for the apply phase, coordinated with the parallel `fix-sub2api-error-propagation` owner; their pinned worktree is `/home/xz/Code/ai/sub2api-worktrees/fix-error-propagation` at v0.2.4/`5de5e2b`):

- The entrance's no-cross-backend-retry rule is seam-scoped. It does not disable or modify Sub2API's existing account pool retries (pool_mode=true, retry_count=1 kept by operator choice, explicit retry_status_codes) nor AISIX's combo retry/cooldown policies.
- The account `base_url` migration must re-read the latest whole credentials/config object and write back only the URL-related change, preserving unrelated retry/pool fields — including the peer change's eventual supported unset of the broad retry-status override (never `[]`, never a re-default to 3).
- Rollback of this routing migration must not clobber the peer change's independent account/config edits.
- Before any overlapping production config deployment, the two change owners exchange diffs. No Sub2API source or image changes are in this change's scope.

Headroom coordination gate: `add-headroom-context-compression` is a separate, unimplemented, independently approved change — not "unaffected". This change neither implements nor edits it. Ordinary accounts target the internal entrance. Any combined deployment with opted-in Headroom accounts is gated on that owner's approved rebase so Headroom's downstream reaches the internal entrance (candidate `Sub2API → Headroom → internal APISIX → CPA/AISIX`), rather than bypassing the selector or removing compression. The safe-cutover delta and migration tasks must not blindly overwrite an opted-in Headroom `base_url`. No Headroom code tasks are added here. Session-affinity (`preserve-end-to-end-session-affinity`) header forwarding must not be consumed or rewritten by the seam.

## Risks / Trade-offs

- [One internal lookup per inference] → Mitigation: tiny bounded response, keepalive reuse of the existing httpc pool pattern. Failure mode is 503-class, never misclassification.
- [Per-process snapshot means the entrance can briefly act on the previous generation] → Mitigation: acceptable and documented; no simultaneous cross-request consistency is promised. Generation is exposed for diagnostics; refresh is bounded-cadence, not on-demand.
- [Entrance-side classification cannot see Sub2API's model_mapping authorization] → Mitigation: by design — classification is membership-only; Sub2API keeps account/group authorization exactly as today, before and after the seam.
- [Malformed/unsupported payload on a classified path] → Mitigation: explicit protocol-shaped error; never a silent CPA fall-through. Scope boundary (non-classified endpoints keep compatibility behavior) is distinct from failed classification.
- [Legacy alias consumers depend on old rewrite behavior] → Mitigation: selector precedence applies only within the classified scope; consumers outside it keep compatibility behavior. Residual: any client relying on an alias rewrite *inside* the classified scope now gets exact-ID selection instead — intended, and observable in acceptance.
- [AISIX-only declarations may be unreachable upstreams] → Mitigation: out of scope; membership-based routing still sends them to AISIX, whose own error/cooldown behavior surfaces failures. Reachability verification is a documented follow-up; four declarations affected.
- [Two catalog formats (`client_version=1` vs standard) could disagree] → Mitigation: both observed identical (223); raw collection stays pinned to version 1 and the standard projection is built at canonical version 1, so divergence surfaces as generation refresh, not silent drift.
- [Headroom combined deployment could regress either change] → Mitigation: explicit coordination gate — Headroom's approved rebase must target the internal entrance before any combined rollout; no blind `base_url` overwrite.

## Migration Plan

Implementation slices stay offline (Go tests, Lua checks, fake-upstream integration harness). Deployment (separately approved, not in this change): build and stage images → extend compose with the internal APISIX→AISIX edge following existing relay/network conventions → deploy the selector with legacy compatibility behavior outside the scope intact → verify overlap/AISIX-only/not-found/unavailable/malformed classes against fakes → account placement migration (credentials-preserving, from latest complete credentials, backed up; rollback restores prior AISIX serving behavior while preserving peer edits) → observe stop thresholds. No provider quota is spent on routing demonstrations. Any combined deployment involving opted-in Headroom accounts waits for that owner's approved rebase.

## Open Questions

- Exact internal endpoint path (`/routing-index` placeholder) — safe to finalize at implementation; specs constrain shape, not spelling.
- Concrete refresh interval value within the existing TTL cadence — observable after deployment, no spec impact.
