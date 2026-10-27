# models-enricher

Technical reference for the TypeScript Codex catalog enricher in [`models-enricher/`](../models-enricher/). Compose service `models-enricher` runs inside the `models-enricher-netns` namespace, listens on `:8090`, and is reached only by `apisix-models` (through `apisix-models-enricher-relay`); it reads CPA and AISIX through `enricher-cpa-relay` and `enricher-aisix-relay` and has no internet egress.

## HTTP interface

| Method and path | Purpose |
|---|---|
| `GET /v1/models?client_version=<v>` | Codex manifest for that client version; without a version, the standard `{object:"list", data:[{id}]}` list |
| `GET /models-table` | server-rendered diagnostic table (no JavaScript) |
| `POST /models-table/refresh` | full-chain forced refresh; waits for the result |
| `GET /healthz` | process liveness (`ok`) |
| `GET /readyz` | `200 {"status":"ready","generation":N}` once a CPA catalog generation exists, otherwise `503 catalog_not_ready` |

## Commands

The image entrypoint is `/models-enricher`:

```bash
/models-enricher                          # serve
/models-enricher healthcheck              # exit 0 when GET 127.0.0.1:8090/readyz is 200 (Compose healthcheck)
/models-enricher validate-config <path|-> # prints {"version":1,"config_digest":"<sha256 of the bytes>"}; non-zero on invalid config
/models-enricher warm-compile-cache       # image build step: fills NODE_COMPILE_CACHE for both bundles
```

Ansible's `deploy-models-enricher-config` runs `validate-config` inside the running container before activation and compares `config_digest` after recreation ([Ansible operations](ansible-operations.md#models-enricher-config-safety)).

## Environment

| Variable | Default | Meaning |
|---|---|---|
| `LISTEN_ADDR` | `:8090` | listen address |
| `CONFIG_PATH` | `/app/config.yaml` | YAML configuration (production: the private config mounted read-only) |
| `CACHE_DIR` | `/var/cache/models-enricher` | volume holding `v2/` (`@cpa-upstream` cache) and `out/` (published outputs) |
| `CPA_MANAGEMENT_KEY` | required | CPA Management API key (discovery, identity, `api-call`) |
| `CPA_CLIENT_KEY` | `CPA_API_KEY` | client key for the CPA native manifest |
| `AISIX_MODELS_URL` / `AISIX_TOKEN` | `aisix_models_url` from config / none | optional AISIX-only catalog supplement |
| `WORKER_MAX_OLD_GENERATION_MB` | `160` | old-generation cap of the per-generation compile worker |
| `TABLES_PATH` | bundled `data/source-tables.json` | build-time source tables |

`GOMEMLIMIT`, `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY` and `SSL_CERT_FILE` are no longer used.

## Configuration reference

Annotated example: [`models-enricher/config.yaml`](../models-enricher/config.yaml). Production policy lives only in the ignored private config. Unknown keys and Go-era keys fail `validate-config` with the field path and a migration hint.

| Key | Type / default | Meaning |
|---|---|---|
| `cpa_base_url` | string, required | CPA base URL (`http://cpa:8317` through the relay) |
| `http_concurrency` | positive integer, `8` | shared outbound pool size |
| `channel_timeout` / `overall_deadline` | duration, `8s` / `25s` | per-read timeout / whole-build deadline |
| `aisix_models_url` / `aisix_timeout` | string / duration (`channel_timeout`) | optional AISIX supplement |
| `scheduled_refresh_interval` / `entry_refresh_window` | duration, `30m` / `10m` | table schedule / staleness window that lets a page view start a background refresh |
| `metadata.chain` | list | default field-source chain |
| `metadata.models[]` | `{match, chain?, overrides?}` | ordered glob rules; a later matching `chain` replaces, `overrides` merge in order |
| `metadata.include_models` | ordered glob list, optional | public membership filter (see [the catalog section](#catalog-compatibility-stack-apisix-models--models-enricher)) |
| `channels.<prefix>` | `{path?, ollama_native_base_url?}` | transport settings for `@cpa-upstream` reads only |
| `custom_channels.<key>.models` | list of names | hidden pools; `<key>/<name>` entries are compiled but only surface through `static_models` `inherit` |
| `static_models[]` | `{slug, inherit?, overrides?}` | operator models; `inherit` is an ordered list, earlier entries win |
| `metadata_from` | map slug → slug | copy compiled metadata from another output slug, then reapply the target's overrides |
| `sync_max_context_window` | `true` / `false` / glob list, `false` | which models get `max_context_window = context_window` |

Durations use Go syntax (`500ms`, `30s`, `10m`, `1h30m`) and must be positive.

Chain entries:

| Entry | Source |
|---|---|
| `models.dev/<provider>/<access>[/<model id>]` | bundled models.dev provider record; a literal model id is looked up verbatim |
| `modelparams.dev/<provider>/<access>[/<model id>]` | bundled modelparams.dev record (`api_key` or `subscription`) |
| `models.dev/@creator/<access>`, `modelparams.dev/@creator/<access>` | the model maker's records, located through models.dev canonical identity |
| `@id` (model segment) | the model's own query candidates (case/separator/date variants) |
| `@default` | the shared default chain |
| `@self` | the CPA native manifest row |
| `@cpa-upstream` | the channel upstream row read by CPA `api-call` (Ollama: `POST /api/show`) |

## Development

```bash
git submodule update --init models-enricher/vendor/openai-api-pi-extension
cd models-enricher
npm ci
npm run typecheck
npm test
npm run build            # data/source-tables.json + dist/server.mjs + dist/worker.mjs
podman build -t models-enricher:dev .
```

The shared compile core comes from the pinned [`openai-api-pi-extension`](https://github.com/xz-dev/openai-api-pi-extension) submodule; Dependabot bumps the pin weekly. `@opencode-ai/models` and `modelparams` are exact npm pins in one `metadata-data` Dependabot group checked hourly; review the `/models-table` difference before merging a data bump. Images are published by the `Publish models-enricher image` workflow (`workflow_dispatch` with an immutable `version`) and deployed by digest.

## Catalog compatibility stack (apisix-models + models-enricher)

The dedicated `apisix-models` instance remains for catalog and protocol compatibility, but CPA-backed Sub2API inference accounts no longer traverse it: their path is Sub2API -> AISIX -> CPA. Parameterized `GET /v1/models` requests reach `models-enricher` through the retained catalog sidecars, while missing or empty versions retain the established CPA fallback behavior. Requests sent directly to the enricher still require a nonempty version.

`models-enricher` (TypeScript service in `models-enricher/`) answers Codex manifest requests. The CPA **native manifest** (`GET /v1/models?client_version=1` with the client key, regardless of the caller's version) is the authoritative baseline for OAuth/native models: if neither a fresh response nor an eligible successful cached response is available, the enricher returns 502 `native_manifest_failed` and synthesizes nothing. It then discovers enabled key-type CPA channels via the management API (`CPA_MANAGEMENT_KEY`), fetches explicitly configured channels' live models through CPA `api-call` (credentials never leave CPA), and enriches from explicitly configured sources. Channel upstream inventories supply metadata only; membership stays CPA-authoritative (CPA identity admission, the AISIX supplement and `static_models`). All outbound HTTP (CPA calls, `@cpa-upstream` reads, Ollama lookups) shares one bounded pool (`http_concurrency`, default 8); channels are read concurrently with deterministic ordering, and a single channel failure degrades with a WARN instead of failing the request.

**Positive directory admission — local candidate, not deployed:** first remove Management-listed originals/bare aliases, then retain a remaining candidate only when removing one CPA prefix leaves an exact Management model match in the same channel/account. Key-channel responses provide the real `prefix` and effective original ID (`alias`, or `name` when the alias is empty). `hub/nvidia/model` must match `nvidia/model` under `hub`, not a global model or a guessed vendor. An ID also declared as another channel's complete route remains eligible. The enrichment configuration is not an identity whitelist.

For OAuth, the candidate reads Management `auth-files` metadata, account-associated `auth-files/models`, `model-definitions/<provider>` and `oauth-model-alias`; it does not download authentication files. It removes listed originals/direct aliases, including vendor-qualified originals. A surviving ID must be registered for the same account as its exact original/direct alias, with a nonempty leading prefix separating the two. Both account registrations and the original-name definition must match; a static-definition difference alone, cross-account name match or recursive alias chain cannot admit a candidate. This is a name-pair check, not a new source of OAuth prefix configuration or proof that static definitions cover every dynamic model. Successfully read data with no match—including unknown dynamic names—is rejected, not passed through.

A lookup miss is different from incomplete discovery or a failed identity read without usable cache. The latter still preserves affected existing CPA records and warns, retaining nulls, empty values, numeric precision and original IDs without enrichment/overrides. Matched OAuth records also remain native passthrough. Explicit static entries materialize separately under their existing rules. Management records do not directly add public models; the five Rust-managed kinds still require CPA public membership. Gemini/Interactions retain their existing candidate source, but enrichment cannot reintroduce a candidate that fails positive admission. Authorization and actual model-call routing are unchanged. See [local identity verification](catalog-identity-verification.md) for the behavior contract and the [2026-09-08 deployment record](catalog-positive-admission-deployment-2026-09-08.md) for the authorized Go-only rollout, exact artifacts and production acceptance.

Source configuration is fully explicit (`models-enricher/config.yaml`). The `metadata` block uses the [`openai-api-pi-extension`](https://github.com/xz-dev/openai-api-pi-extension) chain schema; the sidecar bundles that project's host-neutral compile core from the `models-enricher/vendor/openai-api-pi-extension` submodule, so the gateway and Pi resolve metadata with the same code:

- **Chains.** `metadata.chain` is the default field-source chain; each field takes the first valid value in chain order. `metadata.models` is an ordered list of `match:` rules (standard glob: `*` stays inside one `/` segment, `**` spans segments, case-sensitive); a later matching rule's `chain` replaces the earlier one, and `overrides` merge in rule order (RFC 7396). Chain entries are `models.dev/<provider>/<access>[/<literal model id>]`, `modelparams.dev/<provider>/<access>[/<id>]`, the built-ins `@creator` (the model maker's provider records, via models.dev canonical identity), `@id` and `@default`, and two gateway record sources: `@self` (the CPA native manifest row) and `@cpa-upstream` (the rich row CPA reads from the channel's upstream through Management `api-call`; for Ollama it is `POST /api/show` against `channels.<prefix>.ollama_native_base_url`). Sources ahead of `@self` override CPA's values, including `display_name`; `@self` last only fills what they lack, and `@self` first keeps CPA's values. `@cpa-upstream` is read only for channels whose kept models have it in their effective chain.
- **`metadata.include_models`** is an optional ordered glob list with the same matcher: the last matching entry decides, a leading `!` hides, and unmatched models are hidden. Omitting it keeps every admitted model; an empty list is a configuration error. Hidden models are not processed (no lookup, no `@cpa-upstream` read) unless a kept model references them through `inherit` or `metadata_from`. Unprefixed CPA models admitted by identity are always members; `["*/**"]` hides them.
- **Channel identity = CPA `prefix`.** The channel display `name` is not routing identity. Empty or duplicate key-channel prefixes remain configuration errors; this is separate from the Management-backed original-name admission rule above. `channels.<prefix>` only holds transport settings (`path`, `ollama_native_base_url`); metadata policy lives in `metadata.models` rules.
- **Gateway-only post-processing** (not part of the shared chain semantics): `static_models` with an ordered `inherit` list (earlier entries win, missing references do not erase metadata) and `overrides`; the root `metadata_from` map (full output slug → slug whose compiled metadata it copies, then the target's overrides are reapplied); `custom_channels` hidden pools that `static_models` materialize through `inherit`; and removal of a `default_reasoning_level` that is not among the model's supported levels unless an override sets it.
- **`sync_max_context_window`** accepts `true` (every non-native entry), `false`/absent (none) or an ordered glob list with `include_models` rules. Selected models get `max_context_window = context_window`; unselected ones only have it raised when missing or smaller, so a larger CPA bound (for example 872k) survives. An explicit `max_context_window` override always wins, and CPA-native passthrough rows are untouched.
- Go-era fields (`source_priority`, `provider_prefix_map`, `lookup_ids`, `global_fallback`, channel `include`/`exclude`/`overrides`/`models`/`fetch_models`/`sync_max_context_window`, `exclude_models`, `bare_models_takeover`) are rejected by `validate-config` with a migration hint.
- Mapped Codex fields are `display_name`, `context_window`, `max_input_tokens`, `max_output_tokens` (mirrored into `max_tokens`), `input_modalities`, `output_modalities`, `supported_reasoning_levels` and `default_reasoning_level`. Raw source fields are not copied into Codex entries. Public `slug` and any emitted `id` remain the configured routing identity. These are declared metadata bounds, not a unified OpenAI Models schema or proof of universal model capacity.
- The gateway keeps the complete upstream inventory, including non-chat models and unknown capability fields. Nulls remain observable via `/models-table`. The separate [`openai-api-pi-extension`](https://github.com/xz-dev/openai-api-pi-extension) filters non-chat entries for Pi, retains image-input text models, and uses a 128,000 context fallback when a valid chat entry lacks a context limit. Pi selects the first finite positive value from `max_output_tokens`, `max_completion_tokens`, then `max_tokens`, otherwise the resolved context limit. This unchanged client policy is not verified model capability; removing a synthesized alias can change Pi's selected value when the remaining fields disagree.

Only `@cpa-upstream` reads can fail at runtime; bundled snapshot data cannot. A failed channel read without usable cached data makes `@cpa-upstream` yield nothing for that channel's models, so their chains fall through to the remaining entries; an Ollama per-model failure affects only that model. Other channels continue. A successful response without a matching model is an ordinary metadata miss, not a request failure. With no usable CPA native baseline, the whole catalog still fails closed. Invalid configuration remains distinct from an upstream outage.

**Runtime.** The image runs Node LTS (slim) with esbuild-bundled `server.mjs` and `worker.mjs`, a baked `NODE_COMPILE_CACHE`, JIT on, `MALLOC_ARENA_MAX=2` and a 96 MiB main-heap cap. Each catalog generation is compiled in a one-shot worker (160 MiB old-generation cap) that reads the build-time source tables, posts the compiled catalog and exits, so the tables never stay resident. The main event loop owns all state; no mutable state is shared across threads.

APISIX does **not** cache the final enriched catalog. Each generation's outputs (the Codex manifest per `client_version`, the standard list and the models-table HTML) are serialized once, written to `<CACHE_DIR>/out/` (temp file + rename) and streamed to clients in 64 KiB chunks; the idle process keeps only a small index, and old generation files are deleted once no response still reads them. At most 24 published keys are kept. Concurrent requests for the same generation and version share one build. Shared builds and request-triggered refreshes are detached from client disconnect (still bounded by their deadlines). Table-entry requests do not wait for collection at all.

Reads are split by purpose. **`@cpa-upstream` responses** (channel model lists and Ollama `/api/show` via `api-call`) are persisted as one file per request identity under `<CACHE_DIR>/v2/` on the dedicated named volume `models-enricher-cache`, so they survive restart and container recreation. The SHA-256 identity covers method, complete URL/query, request headers/authentication and body. After a failed refresh the last validated success is reused; failures never replace or delete it, and a valid empty response replaces it. The cache holds at most **100,000,000 bytes of payload** and evicts whole entries **FIFO** by admission sequence, which survives restart; a body larger than the whole budget is never admitted. An eviction removes fallback for that input until it is read again. **Authority reads** (CPA native inventory, management discovery/identity, AISIX membership) keep their earlier policy — stale only on network errors, timeouts or 502/503/504, invalidated on 401/403 or invalid bodies — and stay in a 32 MiB process-local FIFO memory cache; they are never written to disk, because management responses can contain credentials. Persisted metadata never grants catalog membership, entitlement or routing readiness: catalogs are always recomposed from the current CPA snapshot, so newly added CPA models appear during metadata outages and removed models do not return. The volume is private to the enricher (0700/0600, owned by 65532); deleting it is a deliberate destructive action. The container keeps its 64MiB `/tmp` tmpfs and 384 MiB cgroup limit; measured production peak is about 256 MiB and steady anon+swap about 76 MiB (median). The client-facing CPA key comes from `CPA_CLIENT_KEY` (defaults to `CPA_API_KEY`).

AISIX owns alias routing over concrete CPA targets. Client WebSocket ends at the front keeper and `responses-ws-sse-bridge`, which turns each turn into `POST /v1/responses`; Sub2API account WS mode is `off` ([docs/websocket-termination.md](websocket-termination.md)). The internal models gateway routes model IDs containing `/` to CPA and all others to AISIX ([docs/model-routing.md](model-routing.md)).

Cutover: configure every CPA-backed Sub2API account for `http://cli-proxy-api:8317/v1` with an AISIX caller key and WS mode `off`, only after the front bridge is live. Rolling back the bridge restores account WS modes first. CPA model-router is not a standby or rollback path.
