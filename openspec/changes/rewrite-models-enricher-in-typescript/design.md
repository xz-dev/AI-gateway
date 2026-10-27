## Context

Motivation is in proposal.md. Current state that shapes the approach:

**The Go sidecar** (`models-enricher/`, about 4,300 non-test lines):
- Serves `/v1/models`, `/models-table`, `/models-table/refresh`, `/readyz`, `/healthz` and `/routing-index` on `:8090`.
- Runs in `models-enricher-netns` with `mem_limit: 384m`, `cpus: 0.5`, `pids_limit: 64`, `read_only: true` and a 64 MiB `/tmp` tmpfs. It sets `GOMEMLIMIT=288MiB`.
- Mounts the `models-enricher-cache` volume at `/var/cache/models-enricher`. The volume holds a bbolt file.
- Reaches CPA, AISIX and an egress relay through socat relays. The egress relay is used only for the models.dev and modelparams.dev HTTP reads.
- Ansible calls `/models-enricher validate-config` and depends on its `{version, config_digest}` JSON. Compose calls `/models-enricher healthcheck`, which probes `/readyz`.

**The extension** (after `add-chain-source-keyword-registry`):
- Exposes `metadata-core.ts` with no Pi imports. It provides:
  - config parsing from a parsed value with a path prefix and registered record sources;
  - `selectModels`;
  - `runCompileWorker` with a `tablesPath` option;
  - `makeResolver` with record-source providers;
  - `buildSourceTables`.
- Its worker receives only config and public IDs. Record sources are resolved on the caller's thread.

**Repository and data:**
- The repo already uses git submodules (`middleware/*`).
- `@opencode-ai/models` and `modelparams` are ESM packages. The models.dev snapshot is about 6.9 MB of JS.

## Goals / Non-Goals

**Goals:**
- Behavior parity with the Go sidecar except for the BREAKING items in the proposal. Parity is proven by a shadow diff before cutover.
- One metadata engine shared with the extension, with no gateway fork of chain logic.
- Steady memory no higher than Go, peak below 384 MiB, and bounded CPU on a 0.5 CPU quota.
- Unchanged operator surfaces: compose service name, IP, port, health and validate commands, volume path, and the `:9083` diagnostic exposure.

**Non-Goals:**
- Changes to APISIX routing, `cpa-model-sync`, AISIX or Sub2API.
- Automatic translation of the private YAML. The operator migrates it, helped by `validate-config` hints.
- Migrating old bbolt cache contents.
- Horizontal scaling or multi-process clustering.

## Decisions

### D1 Layout: separate directory during shadow, renamed after Go removal

New code lives in `models-enricher-ts/`, with the extension as a submodule at `models-enricher-ts/vendor/openai-api-pi-extension`. The submodule is pinned to the commit that contains the host-neutral entry. Go stays untouched in `models-enricher/` until cutover. The last step deletes Go and runs `git mv models-enricher-ts models-enricher`; that command also updates `.gitmodules`.

- Alternative: mix TS into `models-enricher/`. Rejected: two toolchains in one build context during shadow, and Go CI would see unrelated files.
- Alternative: a repo-root `vendor/` submodule. Rejected: the Docker build context would have to be the repo root or an additional context.

Module layout (deep modules, thin edges):
- `src/config.ts`: one YAML parse. Gateway keys are validated here; legacy keys are rejected. The `metadata` block goes to the extension parser with prefix `metadata` and sources `@self` and `@cpa-upstream`.
- `src/cpa.ts`: management reads and api-call, with size limits, the concurrency limiter and secret-safe errors.
- `src/authority.ts`: native manifest pinned to `client_version=1`, identity admission (including unprefixed models), AISIX supplement and the generation publisher.
- `src/upstream.ts`: `@cpa-upstream` reads for OpenAI-style lists and Ollama `/api/show`, deduplicated per channel and lookup ID. Reads go through the disk cache.
- `src/build.ts`: one generation build (D5).
- `src/postprocess.ts`: static `inherit`, the `metadata_from` map, custom pools, `sync_max_context_window` and the reasoning-default cleanup.
- `src/project.ts`: Codex manifest, standard list and models-table rows, with the projection cache.
- `src/server.ts`: `node:http` routes, scheduler, singleflight, `/readyz` and the CLI subcommands.
- `src/worker.ts`: a thin wrapper around the extension's worker body.

### D2 Build: esbuild bundles plus build-time SourceTables

`package.json` pins every dependency exactly, and `package-lock.json` is committed: `@opencode-ai/models`, `modelparams`, `yaml` and `minimatch` at the versions the extension declares; `esbuild` and `typescript` as dev dependencies.

The Docker build stage runs:
1. `npm ci`.
2. `node scripts/build-tables.mjs`. It imports both packages, calls `buildSourceTables`, fails on any degraded shape, and writes `/app/data/source-tables.json` with both versions and the generation time.
3. `esbuild --bundle --platform=node --format=esm` for `dist/server.mjs` and `dist/worker.mjs`.

The runtime stage copies only `dist/`, the tables JSON, `config.yaml` and the `/models-enricher` wrapper. Neither `node_modules` nor the npm packages ship.

- Alternative: ship the packages and load them in the worker. Rejected: a larger image, a runtime module graph, and a different code path from the gateway's tests.

### D3 Runtime image and startup

- **Base image**: `node:<active LTS>-bookworm-slim`, pinned by digest. The exact major version is chosen at implementation time from the then-current Active LTS line; it must satisfy the extension's `engines`.
- **Entrypoint**: `/models-enricher` is a POSIX sh wrapper, `exec node /app/dist/server.mjs "$@"`. Compose and Ansible keep their commands unchanged.
- **User and cache directory**: the image runs as UID/GID 65532, and the mount point is created with 0700. The existing volume keeps its ownership.
- **Compile cache**: `NODE_COMPILE_CACHE=/app/.node-compile-cache`. A final-stage `RUN /models-enricher warm-compile-cache` imports both bundles, calls `module.flushCompileCache()` and exits, so the cache matches the runtime binary and paths. At runtime the directory is read-only. Writes then fail silently, and reads still hit the cache; task 3.3 verifies this.
- **Compile cache results** (task 3.3, 2026-10-27, local podman, image `node:26-bookworm-slim@sha256:1884ec27…` with Node 26.12.1; host load average about 17, so the numbers are noisy):
  - Node keys the cache directory by UID (`v26.12.1-x64-310b2ce3-<uid>`), so the warm `RUN` runs as 65532. A warm run as root produced a cache that the runtime user never read (`Cannot create cache directory`).
  - With `--read-only --tmpfs /tmp` and `NODE_DEBUG_NATIVE=COMPILE_CACHE`, both `server.mjs` (89,584-byte entry) and `worker.mjs` (7,728-byte entry) report `V8 code cache … was accepted`, then `skip persisting … because cache was the same`. There were no errors or warnings, so the tmpfs fallback is not needed.
  - Process launch until `/healthz` answers, median of 25 runs, read-only root, `/var/cache/models-enricher` on a tmpfs: 88 ms and 112 ms with the cache (two runs), 122 ms and 116 ms with `NODE_DISABLE_COMPILE_CACHE=1`.
  - `validate-config` interleaved 40 times in one container (loads the full server bundle): median 65 ms with the cache and 70 ms without.
  - The cache saves 5–30 ms per start. It also makes each `healthcheck` process cheaper.
  - Image size: 259 MB, against 251 MB for the base image.
- **Cache directory**: a missing or unwritable `CACHE_DIR` stops startup with exit 1 and the log line `read cache initialization failed; CACHE_DIR must be a writable directory`. This matches the Go sidecar. Read-only runs need the named volume or a tmpfs on `/var/cache/models-enricher`.
- **JIT**: stays on (no `--jitless`).
- **Heap limits**: the main process gets `--max-old-space-size`, and each worker gets `resourceLimits.maxOldGenerationSizeMb`. Both are set from the measurements in task 6.2.
- **Removed settings**: `GOMEMLIMIT`, `HTTP_PROXY`, `HTTPS_PROXY` and `SSL_CERT_FILE` (D7).

**Go baseline** (task 1.2), measured on production on 2026-10-27 against container `ai-gateway-models-enricher-1`, which had been up since 2026-10-23.

- **Metric.** The host has 2 GiB RAM and actively swaps idle pages (`free -m`: about 1.1 GiB swap used). VmRSS alone therefore swings between 6 MiB and 54 MiB for the same process. The comparison metric is the cgroup footprint: `memory.stat anon` + `memory.swap.current`.
- **Steady footprint:**
  - about 50 MiB anon before a refresh;
  - about 89 MiB anon+swap four minutes after a forced refresh (1.8 MiB anon + 87 MiB swap).
- **Peak:**
  - VmHWM over 4 days: 228 MiB;
  - cgroup `memory.peak`: 240 MiB;
  - VmRSS sampled every 0.2 s during one forced refresh: 212 MiB.
- **CPU:** one forced refresh (`POST /models-table/refresh`, HTTP 200) took 9.8 s wall and 2.27 CPU-seconds.
- **Threads:** 9 threads, `pids.current` = 9.
- **Catalog size:** 367 models. The Codex manifest is 3.1 MB and the standard list 19 KB.
- **Fixtures:** stored outside the repository in `/var/tmp/enricher-shadow/go/`.
- **Commands:** `/proc/<pid>/status`, the cgroup v2 `memory.stat`, `memory.peak`, `memory.swap.current`, `cpu.stat` and `pids.current` of the container scope, and `nsenter -t <pid> -n curl`.

### D4 Concurrency: single event loop plus a one-shot worker per generation

The main thread owns all mutable state: caches, the published generation and projection entries. Shared work uses promise-based singleflight keyed by purpose: authority collection, generation build, and each projection build per generation, version and format.

The worker gets a structured-clone copy of the `metadata` config and the compile-set IDs. It reads the tables JSON itself, posts the compiled catalog and exits. No `SharedArrayBuffer` and no shared mutable objects cross threads.

Worker strategies considered:
- **(A) One-shot worker per generation** — chosen. The tables are released when the worker exits. The cost is one JSON parse per build: startup, every 30 minutes, and forced refreshes.
- **(B) Persistent worker that caches the tables.** Rejected: it keeps the tables resident and would raise the steady-state memory.
- **(C) In-process compile.** Rejected: the tables would live on the main heap and GC timing would decide the peak.

The worker is skipped when no effective chain in the compile set needs an external site, as decided by the extension's `neededSites`.

### D5 Generation build pipeline

1. **Authority snapshot (process-local).** The native manifest is captured before identity filtering. Discovery and identity come from management. AISIX is optional and isolated.
2. **Membership.** Admitted CPA IDs (including admitted unprefixed IDs), plus the AISIX supplement (exact difference against the raw CPA inventory), plus static slugs.
3. **Selection.** `selectModels` gives the kept and hidden sets. The compile set is the kept set plus the transitive closure over `inherit` and `metadata_from`. A reference cycle is a config error.
4. **Compile.** The worker builds fixed chains for the compile set.
5. **`@cpa-upstream`.** For each channel whose compile-set models have `@cpa-upstream` in their fixed chain, read through the cache (D6). The result is a `Map<publicId, row>`. Rows are references into the parsed response; nothing is copied.
6. **Resolve.** `makeResolver` uses two providers: `@self`, a lookup into the native-row map, and `@cpa-upstream`, a lookup into the map from step 5. Fields are mapped to Codex names, overrides are applied, and then post-processing runs.
7. **Publish.** The generation becomes immutable. Projections are built lazily per client version and format and cached under the generation key. When a newer generation is published, the old generation and its projections become unreachable.

Intermediate maps are function-local, so they are collected after the build. Only the published generation, its projection entries and the bounded caches survive.

### D6 Caches

**Management and authority reads** use a memory map bounded at 32 MiB of payload, evicting the oldest-inserted entry first. Freshness and stale use follow the existing authority-specific policy: a 502, 503, 504 or network error may reuse last-good where that policy allows it; denial and invalid responses never do. These reads are never persisted.

**`@cpa-upstream` responses** use a disk cache at `/var/cache/models-enricher/v2/`:
- Each entry is one file named `sha256(identity)`. Its header holds the admission sequence, the last-success time and the payload length.
- Writes go to a temp file followed by `rename()`.
- At startup, an index is built by scanning the headers. The 100,000,000-byte budget is accounted in memory. FIFO eviction uses the admission sequence, so it survives restart.
- A body that does not fit the whole budget is never admitted.
- The old bbolt file at the volume root is ignored. It is removed only after Go is deleted (task 7.4), which keeps rollback possible.
- Alternative: SQLite (`node:sqlite`). Rejected: still experimental on some LTS lines, and a file-per-entry layout is enough at this scale (one file per channel or Ollama lookup).

**Projection cache**: keyed by generation, version and format. Old generations are dropped on publish.

### D7 Network: no internet egress

All reads go to CPA (`cpa:8317`, management and api-call) or AISIX. The service drops `HTTP(S)_PROXY`, the CA bundle mount and the `enricher-squid-relay` dependency. After cutover, the relay service itself is removed if nothing else uses it (task 7.3).

### D8 Shadow and cutover

- A compose profile `enricher-shadow` adds `models-enricher-shadow`. It uses its own netns IP, the same CPA and AISIX relays, its own volume `models-enricher-shadow-cache`, and the migrated config.
- APISIX does not route to the shadow service.
- `scripts/enricher-shadow-diff.mjs` fetches these from both services through the diagnostic path:
  - `/v1/models?client_version=<v>` for the Codex versions in use;
  - `/v1/models` (standard list);
  - `/models-table`.
- The script normalizes ordering and diffs:
  - membership, where any difference blocks cutover unless it is explained by a BREAKING item;
  - per-field metadata values, which are reviewed and accepted per field or fixed with config rules.
- Cutover changes the `models-enricher` service to the TS image and config, recreating only that service. Rollback restores the Go image pin and the old private config; the Go bbolt file is still intact.
- Go deletion, the directory rename and CI cleanup happen only after the observation window in task 6.4.

### D9 Dependabot

`.github/dependabot.yml` covers these ecosystems:
- `gitsubmodule` at `/`;
- `npm` at `/models-enricher-ts` (later `/models-enricher`);
- `docker` at each in-repo Dockerfile directory: `aisix`, `aisix-status`, `cpa-model-sync`, `egress-proxy` and the enricher;
- `cargo` at `/cpa-model-sync`;
- `gomod` at `/aisix-status`, plus `/models-enricher` until Go is deleted;
- `github-actions` at `/`.

All run weekly. `@opencode-ai/models` and `modelparams` form one `metadata-data` group, so a single PR updates both pins; build-time tables then make the new data reviewable through the shadow-diff script. Submodule directories owned by other repos (`middleware/*`) are left to their own repos.

## Risks / Trade-offs

- [Default `@creator` resolution changes metadata for some models] → The shadow-diff field review gates cutover. Unwanted changes are pinned with literal fourth-segment chain entries or overrides in the private config.
- [models.dev npm snapshot differs from the old HTTP API data] → Same gate. The table shows the bundled data versions, so differences can be traced.
- [Node baseline RSS (about 40–60 MiB) plus a tables parse may exceed the Go steady state] → The worker exits after each build, so steady state excludes the tables. The heap limits from task 6.2 apply. If the steady-state target is still missed, `build-tables` strips fields unused by the extension before writing the JSON. That fallback is recorded, not pre-built.
- [The healthcheck spawns a Node process every 30s, a transient of about 30–40 MiB inside the cgroup] → It is measured with the compile cache in task 6.2. If peak headroom is too small, the wrapper's `healthcheck` path uses a minimal no-bundle script that only does an HTTP GET to `/readyz`.
- [Thread count against `pids_limit: 64`] → The main thread plus libuv and V8 threads plus one worker stays well below 64. Task 6.2 verifies this with `/sys/fs/cgroup/pids.current` during a build.
- [Submodule drift between the extension and the gateway] → The submodule pin moves only through a reviewed Dependabot or manual PR. Gateway CI type-checks and tests against the pinned commit.
- [Read-only compile cache behaves differently than assumed] → Task 3.3 tests it. If Node refuses to start, the cache directory moves to the tmpfs and is pre-copied at start. The behavior is verified before relying on it.
- [Legacy private config silently ignored] → Prevented by legacy-field rejection. Startup fails closed, and Ansible validates before deploy.

## Migration Plan

1. Land the extension change, then pin the submodule.
2. Implement the TS service with unit and integration tests in CI next to Go.
3. Measure the Go steady RSS, peak and CPU on production, and record the numbers in this design (task 1.2).
4. Migrate the private YAML (gitignored) to the new schema. Validate it with the TS `validate-config`.
5. Deploy the shadow service, run the shadow diff, and iterate config or code until the diff is accepted.
6. Cut over by switching the image pin, the private config and the compose environment. Verify `/readyz`, the catalog and the table on `:9083`.
7. Observe for one scheduled-refresh day. Rollback is the Go image pin plus the old config.
8. Delete Go, rename the directory, update CI, Dependabot and docs, remove the old bbolt file, and remove the egress relay if it is unused.
