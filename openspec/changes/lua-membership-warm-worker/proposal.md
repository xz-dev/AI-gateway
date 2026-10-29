## Why

Production model-catalog (v1.3.0) intermittently logs `membership script wall_time: collection exceeded 2000 ms`. The catalog stays correct because the previous generation keeps serving, but those collections are lost. The 2 s bound covers far more than the user's Lua code:
- worker thread start and module load;
- WebAssembly and Lua runtime setup;
- the trusted prelude;
- script load;
- 564 `select` calls with their JS↔Lua conversions;
- result delivery.

With the container's 0.5 CPU quota and host contention, this cold path takes 2.0–2.4 s, against 130–159 ms when idle. The diagnosis is in `/var/tmp/ai-lua-walltime/report.md`, which reproduces the failure 5/5.

## What Changes

- **Separate init bound.** Starting the worker, instantiating WASM and creating the Lua state with the trusted prelude get their own 10 s initialization bound. This phase never runs operator script code.
- **The execution bound starts after a ready handshake.** The 2 s wall-time bound starts after the worker reports ready and before any script byte runs. It still covers script load, every `select` call, host calls, result checking and result delivery. All execution limits stay as they are: 2 s wall time, the instruction counts, 16 MiB of Lua memory and the 32 MiB worker heap.
- **Warm resident membership worker.** One long-lived membership worker per server process keeps the WebAssembly runtime warm. Each collection gets a fresh Lua state, created from that runtime, and the script is loaded into it. No Lua state, global, hook counter or glob cache survives from one collection to the next.
- **Recreate on failure or new script.** The worker is terminated, and its exit awaited, after any failure: init timeout, wall time, instruction or memory bound, runtime error, invalid return, or worker exit. It is also recreated when the script digest changes. The next collection starts a new worker under the init bound.
- **Errors stay distinguishable.** A new error class, `init_timeout`, is logged separately from `wall_time`. Fallback behavior is unchanged: the previous generation keeps serving, one WARN is logged, the table attempt is marked failed, and the next collection retries.
- `validate-config --script` and the startup dry-run use the same two-phase protocol on a worker that exits afterwards.
- **Not changed:** the container CPU quota and memory limits.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `models-enricher-runtime`: the requirement "Membership script runs in a bounded sandbox" changes in two ways:
  - the wall-time bound covers script execution after a separately bounded initialization, instead of the whole collection;
  - the worker is resident and reused, with a fresh Lua state per collection, instead of a worker created for each collection.

## Impact

- Code: `services/routing/model-catalog/src/membership.ts`, `src/membership-worker.ts`, plus the `src/authority.ts` call site and the `src/main.ts` dry-run. Tests: `tests/membership.test.ts`, `tests/membership-stage.test.ts` and `tests/measure-memory.ts`.
- Docs: `docs/model-catalog.md`, the sandbox and failure behavior paragraphs.
- Image: republish `ai-gateway-model-catalog` and pin it in `compose.yaml` and `.env.example`. The production rollout is image-only, through `deploy.yml -e component=model-catalog`, and the profile config is unchanged.
- Memory: the resident worker keeps one WASM runtime and one bounded V8 heap alive between collections. This must stay inside the existing heap caps and the 384 MiB container limit, with no growth over 50 or more rounds.
