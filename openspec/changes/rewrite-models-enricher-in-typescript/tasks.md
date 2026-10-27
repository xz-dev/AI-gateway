## 1. Prerequisites and baseline

- [x] 1.1 Confirm openai-api-pi-extension has `add-chain-source-keyword-registry` applied and pushed. Verify: its `metadata-core.ts` exists at a pushed commit and `npm run check` passes there.
- [x] 1.2 Measure the Go sidecar on production over 24h: steady RSS, peak RSS during a forced refresh, CPU per build, and `pids.current`. Record the numbers in design.md under D3. Verify: the numbers and the measurement commands are recorded.
- [x] 1.3 Capture a Go output fixture set for the shadow diff: Codex manifest for each client_version in use, the standard list, and the models table. Store it in a gitignored directory. Verify: the files exist, and `git check-ignore` confirms they are not tracked.

## 2. Project skeleton and build

- [x] 2.1 Create `models-enricher-ts/` and add the submodule `vendor/openai-api-pi-extension` at the commit from 1.1. Verify: `git submodule status` shows the pin.
- [x] 2.2 Add `package.json` with exact-pinned `@opencode-ai/models`, `modelparams`, `yaml`, `minimatch`, `esbuild` and `typescript`; commit `package-lock.json`. Verify: `npm ci` succeeds, and `tsc --noEmit` passes over the `src/` skeleton and the submodule's core imports.
  - Done: `npm run typecheck` first emits declarations for `metadata-core.ts` and `metadata-worker.ts` (`tsconfig.core.json`, `noCheck`), then checks `src/` and `tests/` against them through the `#core` paths. The vendor sources themselves are not strict-checked here: under this repo's `noUncheckedIndexedAccess` they report errors, and the extension's own `npm run check` owns them.
- [x] 2.3 Write `scripts/build-tables.mjs` using `buildSourceTables`. It fails on degraded shapes and writes versions and the generation time. Verify: a test runs the script and asserts both versions and non-empty provider/canonical/modelparams tables.
- [x] 2.4 Add an esbuild config producing `dist/server.mjs` and `dist/worker.mjs`. Verify: both bundles build, and a test asserts they contain no `@earendil-works` references.

## 3. Runtime image

- [x] 3.1 Write the Dockerfile: a build stage running `npm ci`, the tables script and the bundles, and a runtime stage from a digest-pinned `node:<active LTS>-bookworm-slim` with UID 65532, the `/models-enricher` sh wrapper and the 0700 cache mount point. Verify: `docker build` succeeds, and `docker run --rm <img> validate-config /app/config.yaml` exits 0 with the digest JSON.
- [x] 3.2 Add the `warm-compile-cache` subcommand, plus a final-stage `RUN` that populates `NODE_COMPILE_CACHE`. Verify: the cache directory is non-empty in the image.
- [x] 3.3 Verify compile-cache behavior on a read-only root (`docker run --read-only --tmpfs /tmp`). Compare startup time and check for errors with the cache present and absent. Verify: the server starts without errors, and the recorded startup time is lower with the cache. If not, apply the tmpfs fallback from design D3 and re-verify.

## 4. Core service

- [x] 4.1 `src/config.ts`: gateway schema, `metadata` block delegated to the extension parser with sources `@self` and `@cpa-upstream`, legacy-field rejection with hints, and `inherit`/`metadata_from` cycle detection. Verify: unit tests cover every legacy field, the `@ollama_cloud` rejection, an empty `include_models`, and a cycle.
- [x] 4.2 `src/cpa.ts`: management reads and api-call with per-endpoint size limits, the `http_concurrency` limiter, timeouts and secret-free errors. Verify: tests cover an oversized body, a timeout, and no key in error text.
- [x] 4.3 In-memory authority cache: 32 MiB bound, oldest-inserted-first eviction, and the existing stale policy (502/503/504/network only). Verify: tests cover the bound, the stale reuse cases, and 401/invalid bodies not reusing stale data.
- [x] 4.4 `src/authority.ts`: native manifest at `client_version=1`, identity admission including unprefixed models, AISIX supplement as an exact difference, isolated optional phase, and immutable generations. Verify: tests ported from Go `identity_test`, `aisix_test` and `bare_takeover_test` pass.
- [x] 4.5 Disk cache `v2/`: file per entry, atomic rename, header scan at startup, 100,000,000-byte FIFO budget, oversized bodies never admitted, old format ignored. Verify: tests cover exact-capacity admission, FIFO not LRU, FIFO across restart, oversized bodies, and a stray bbolt file.
- [x] 4.6 `src/upstream.ts`: `@cpa-upstream` reads for list paths and Ollama `/api/show`, on demand per channel, deduplicated per lookup ID, with the five-minute fresh window and last-good fallback. Verify: tests cover each scenario of "Per-channel models fetch via CPA api-call" and "Cached @cpa-upstream responses keep last-good fallback".
- [x] 4.7 `src/build.ts`: the D5 pipeline with one-shot worker compilation, worker skip when no site is needed, and resolver providers. Verify: tests show hidden unreferenced models trigger no api-call, referenced hidden models are compiled, a worker crash keeps the previous generation, and the worker has exited after a build.
- [x] 4.8 `src/postprocess.ts`: static `inherit` lists, the `metadata_from` map, custom pools, `sync_max_context_window`, the reasoning-default cleanup, and Codex-name override application with `slug`/`id` protected. Verify: tests ported from Go `reasoning_default_test`, `pool` and `static` cases, plus the gpt-image rule scenario.
- [x] 4.9 `src/project.ts`: Codex manifest and standard list from kept models only, a projection cache keyed by generation, version and format, and models-table rows including the hidden, referenced-by and data-version display. Verify: tests cover each scenario of "Model selection with include_models" and "Models table shows hidden and referenced-only models", and the generation-pinning scenario.
- [x] 4.10 `src/server.ts`: routes `/v1/models`, `/models-table`, `/models-table/refresh` (POST only), `/readyz` and `/healthz`; `/routing-index` returns 404. Also the scheduler (startup, interval, entry window), singleflight, graceful shutdown that cancels and joins work, and the `healthcheck`/`validate-config` subcommands. Verify: HTTP tests ported from Go `force_refresh_test`, `catalog_http_test` and `handler_test` pass, along with CLI exit-code and stdout-JSON tests.

## 5. Configuration and CI

- [x] 5.1 Rewrite the tracked `config.yaml` example in the new schema: a `metadata` block whose chain starts with `@self`, `@cpa-upstream` where channels need it, the gpt-image override rule, and an `include_models` example. Include no private policy from ansible/private-config or `.env`. Verify: `validate-config` exits 0 on it.
- [x] 5.2 Add a Node job to `.github/workflows/validate.yml`: submodule checkout, `npm ci`, `tsc`, tests and bundling. Keep the Go job until section 7. Verify: the workflow passes on a PR.
  - Partial: the `models-enricher-ts` job is added and passes `uvx check-jsonschema --builtin-schema vendor.github-workflows`; the same steps pass locally. Pending: a PR run, which needs a push.
- [x] 5.3 Make `publish-models-enricher.yml` build the TS image from `models-enricher-ts` with `submodules: true`, under a distinct shadow tag. Verify: a manual dispatch publishes an image whose digest can be pinned.
- [x] 5.4 Add `.github/dependabot.yml` per design D9, with the `metadata-data` weekly group. Verify: `uvx check-jsonschema --builtin-schema vendor.dependabot .github/dependabot.yml` passes.

## 5b. Shadow follow-ups (decided 2026-10-27)

- [ ] 5b.1 Publish outputs as files under `<CACHE_DIR>/out/`, stream them with fixed 64 KiB chunks, keep only the index in memory, release the parsed generation after each build, set `MALLOC_ARENA_MAX=2` and the heap caps. Verify: tests cover file publication, streaming, and cleanup of old generations; on the shadow, idle anon+swap is below the Go value measured at the same moment.
- [ ] 5b.2 Make `sync_max_context_window` accept a boolean or an ordered glob list. Reject channel-level `sync_max_context_window` as a legacy field, and remove it from the private config. Verify: tests cover the three spec scenarios; the shadow diff shows unconfigured-channel `max_context_window` unchanged from Go.
- [ ] 5b.3 Run the enricher npm Dependabot entry on an hourly `cron`. Verify: `check-jsonschema --builtin-schema vendor.dependabot` passes.

## 6. Shadow run and cutover

- [x] 6.1 Migrate the private models-enricher YAML to the new schema locally. Verify: the TS `validate-config -` exits 0 and the Go-era legacy fields are gone. The file stays gitignored.
- [ ] 6.2 Deploy `models-enricher-shadow` under the compose profile with its own volume. Measure steady RSS, peak RSS during a cold and a forced build, healthcheck transients and `pids.current`, then set the `--max-old-space-size` and worker `maxOldGenerationSizeMb` values. Verify: steady RSS is at or below the Go steady-state baseline measured in task 1.2, peak stays below 384 MiB, 50 forced refreshes show no upward RSS trend, and the numbers are recorded in design.md.
- [ ] 6.3 Run `scripts/enricher-shadow-diff.mjs` against Go and the shadow. Resolve membership differences, and accept or pin field differences through config rules. Verify: the final diff report has zero unexplained membership differences, and every field difference has an accepted reason.
- [ ] 6.4 Cut over: point the `models-enricher` service at the TS image, the new config and the cleaned environment (no `GOMEMLIMIT` or proxy variables), and recreate only that service. Verify: `/readyz` reports ready, the Sub2API-visible catalog matches the shadow output, the `:9083` table renders, and one scheduled refresh succeeds over the observation day.

## 7. Go removal and cleanup

- [ ] 7.1 Delete the Go sources, `go.mod`/`go.sum` and the Go CI job. Run `git mv models-enricher-ts models-enricher`, then update the paths in compose, the workflows, Dependabot and `.gitmodules`. Verify: CI passes, and `git grep -n 'models-enricher-ts\|golang'` shows no stale enricher references.
- [ ] 7.2 Remove the shadow profile and its volume definition from compose. Verify: `docker compose config` succeeds, and no shadow service remains.
- [ ] 7.3 Remove the enricher's egress relay dependency and the CA bundle mount; remove the relay service if nothing else references it. Verify: `docker compose config` succeeds, and the enricher still builds catalogs.
- [ ] 7.4 Remove the old bbolt file from the `models-enricher-cache` volume. Verify: the volume contains only `v2/`.
- [ ] 7.5 Update the docs (`docs/`, the Ansible README models-enricher section and `docs/model-routing.md` references) to describe the TS sidecar, the new config schema and `include_models`. Update the `codex-models-enricher` main-spec Purpose so it no longer says "Go sidecar". Verify: `git grep -n 'Go sidecar\|source_priority\|exclude_models'` hits only archived or historical documents.
- [ ] 7.6 Run `openspec validate rewrite-models-enricher-in-typescript --strict`. Verify: it passes.
