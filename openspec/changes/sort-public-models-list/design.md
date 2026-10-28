## Context

The front-door APISIX (`services/firewall/apisix/`) has two model-list routes in `apisix.yaml`:

- **`ai-api-models-intersect`** (priority 200): `GET /v1/models` with a non-empty `client_version`. Its pre-function calls `model_list_intersection.run()`. That serially fetches:
  1. Sub2API through `ai-sse-keepalive-ingress-relay:8080`, for the entitlement;
  2. `apisix-models:9080`, for model-catalog's manifest.

  It returns model-catalog's records filtered to the entitlement. `filter_original()` walks model-catalog's array in order, so this list already follows model-catalog's output order (union-catalog-membership D7). It needs no change.
- **`ai-api-models-get`** (priority 100): `GET /v1/models` and `GET /models` otherwise, including an empty `client_version` or any other query. Its pre-function only calls `model_list_intersection.admit()`, which takes the shared one-request admission slot. APISIX then proxies to upstream `sub2api`, the same relay node, and Sub2API's body reaches the client in Sub2API's order. The log-phase `release()` frees the slot.

Constraints:

- Both routes go through the global rules: the public 401/404 → empty 404 rewrite and the header filters.
- `model_list_intersection.lua` already provides a bounded `fetch()` (timeouts, keepalive, 64 MiB limits). It also has raw-JSON boundary helpers (`object_fields`, `value_end`) that keep record bytes exact, so large integers and unknown fields are never re-encoded through Lua numbers. Finally, it has the final-entity ETag, `Vary` and conditional logic.
- Compose mounts the files read-only:
  - `config.yaml` and `apisix.yaml` as **single-file bind mounts**. A replaced file (a new inode) is not seen by the running container.
  - `lua/` as a **directory** mount. A replaced file is seen, but OpenResty caches `require`d modules per worker until the workers restart.
  - `worker_processes: 1`.
- `ops/ansible/playbooks/deploy.yml -e component=apisix` uploads only `compose.yaml`, `compose.override.yaml` and `.env`. If they are unchanged it reports a verified no-op and does not restart anything. No playbook deploys tracked service files. Tracked front-door Lua has historically been deployed in a reviewed manual window.
- The test harness `lua/model_list_intersection_test.lua` runs under `resty` in the pinned APISIX image, uses fake `ngx` and `resty.http` objects, and is executed by `ops/scripts/validate.sh` (and therefore CI).

## Goals / Non-Goals

**Goals:**
- Give the plain public model list the same order rule as model-catalog's `compareCatalogIDs`, with no change to members, record bytes, auth or error behavior.
- Keep the change to one Lua file and deploy it without interrupting in-flight public streams.

**Non-Goals:**
- Changing Sub2API or its entitlement model.
- Reordering the Codex manifest path. It is already ordered by model-catalog.
- Adding an Ansible activation path for tracked front-door files. That is recorded as a gap, not built here.

## Decisions

### D1: The front door fetches and reorders the plain list itself

The plain route stops proxying the Sub2API body. The handler:

1. takes the admission slot, as today;
2. calls the existing `fetch()` against the same relay (`ai-sse-keepalive-ingress-relay:8080`) with the caller's path, raw query and `copy_request_headers()`, exactly like the intersection's basic leg;
3. returns that response.

The body is reordered only when it is a recognized 2xx collection.

*Alternative*: a `body_filter` on the proxied response. It would have to buffer chunks across calls, delete `Content-Length` in `header_filter` before the body is known, and handle compression and early errors in a second code path. The fetch-and-respond pattern already exists and is tested in this file, so it is reused.

### D2: Lua-only change; the existing `admit()` entry becomes the plain-list handler

`apisix.yaml` is a single-file bind mount (Context), so changing it means either recreating the public front door, which interrupts every in-flight SSE and WebSocket stream, or an in-place write that relies on standalone file watching. To avoid both, the route keeps calling `require("model_list_intersection").admit(_, ctx)`.

`_M.admit` becomes the plain-list entry point: admission, then D1. The intersection's `run()` switches to a local `admit_request()`, the current admission body, so its behavior is unchanged. A one-line comment on `_M.admit` records that it is the `ai-api-models-get` entry, kept under this name to avoid a front-door recreate. The rename waits for the next window that recreates the front door anyway.

*Alternative*: a new `_M.list` called from `apisix.yaml`. The name is clearer, but it costs a public front-door recreate (D5).

### D3: Recognition and ordering

The handler parses the body with the existing `record_json` codec and `catalog()` / `model_id()` helpers. Recognition means:

- a JSON object with exactly one `data` (`id`) or `models` (`slug`) array;
- every element is an object with a non-empty string identity;
- no element has a conflicting `id` or `slug`, and no element repeats its identity key (the same raw-key check as `filter_original`).

Anything else counts as **unrecognized**. The handler then returns the received status, headers and body unchanged and logs a WARN. It never answers 502 for a body that today reaches the client.

When the body is recognized, the handler:

1. collects each element's raw byte range with the existing boundary walker;
2. builds an array of `{bare, id, index, fragment}`, where `bare` is true when `id` contains no `/` (plain find);
3. sorts it with `table.sort`. The comparator puts `bare` first, then compares by `id` with Lua `<`, then by the original `index`. The `index` tie-break turns the unstable `table.sort` into a total, deterministic order that keeps Sub2API's order for equal IDs;
4. writes `raw:sub(1, first) .. concat(fragments, ",") .. raw:sub(last - 1)`. This is the same splice as `filter_original`, so the envelope and every record stay byte-exact; only the separators between elements are normalized to `,`.

**Code-point order in Lua**: OpenResty runs LuaJIT. LuaJIT's string `<` is a byte-wise comparison (`lj_str_cmp`, memcmp semantics, then length). It does not use `strcoll`, unlike PUC Lua 5.1, so locale cannot change it. IDs are decoded JSON strings, so `\u` escapes are already resolved to UTF-8. For well-formed UTF-8, byte order equals Unicode code-point order, which is a design property of UTF-8. The result therefore equals model-catalog's `compareCatalogIDs`, which maps UTF-16 surrogates to code-point order. The test fixture includes ASCII case, a BMP character (`é`) and a supplementary one (`😀`) so a locale-sensitive or UTF-16-style comparison would fail.

### D4: Response policy

**Non-2xx** responses pass through like the intersection's basic leg: `apply_upstream_headers()` and the received status and body. Global rules then still apply, for example the 401 → empty 404 rewrite.

A **recognized 2xx** reuses the intersection's final-entity policy:

- upstream headers minus `OMIT_RESPONSE_HEADERS`;
- `Content-Type` from upstream, or `application/json`;
- `Cache-Control: private, no-cache`;
- ETag `"apisix-<md5(body)>"`;
- `Vary` merged with `Authorization`;
- `If-Match` → 412, `If-None-Match` → 304;
- the status is kept (normally 200).

Sub2API's own ETag described the unordered body, so it must not be forwarded.

An **unrecognized 2xx** is handled like a non-2xx: `apply_upstream_headers()` with the status and body unchanged.

For both pass-through cases, the existing `OMIT_RESPONSE_HEADERS` set drops Sub2API's hop-by-hop, length, `Date`, `Server`, `ETag` and `Cache-Control` headers. ngx sets its own `Date` and `Server`. This is the intersection basic leg's existing behavior, and it is a small header difference from today's raw proxy.

**Streaming and caching**: the list is a single JSON document and was never streamed. It is now fully buffered under the existing 64 MiB limit. There is no front-door cache: per-key entitlement makes the body private, and caching stays confined to model-catalog's upstream reads (docs/catalog.md).

### D5: Activation by graceful reload in a reviewed manual window

Production changes only `services/firewall/apisix/lua/model_list_intersection.lua`, which is visible through the directory mount. Steps:

1. Back it up.
2. Upload it and check its SHA-256 against the reviewed commit.
3. Send `SIGHUP` to the OpenResty master (`docker kill --signal HUP ai-gateway-apisix-1`). The master starts new workers that load the new module. Old workers finish their in-flight requests, including long streams, within `worker_shutdown_timeout`.

Task 1.4 proves first, against the pinned image with production's `cap_drop: [ALL]` and `no-new-privileges`, that:
- the signal reaches the master as PID 1;
- the new module is used after the reload;
- an in-flight request survives it.

If that proof fails, the fallback is a recreate of only `apisix` (`up -d --force-recreate --no-deps --no-build --pull never apisix`, run through `env -i` as the component playbook does), in a low-traffic window. This needs the supervisor's explicit approval at that point.

The existing playbook is used for the drift checks: `deploy.yml -e component=apisix --check --diff` reports `changed=0` before and after. It cannot perform this deploy because it is a no-op when Compose and `.env` are unchanged.

*Alternative*: an Ansible managed-file deploy for tracked front-door files. That is new scope, not needed for a one-file change; recorded under Risks.

### D6: Independence from union-catalog-membership

This change adds one requirement to `apisix-cpa-gateway`. No requirement in union-catalog-membership's deltas is modified. Union's "Catalog output order" requirement says only that this list is outside model-catalog's requirement, and that stays true after both changes are archived. This change also depends on no union code: it reimplements the rule in Lua and does not rely on model-catalog's order.

The change is therefore **independent**, with no task 0 gate. Operationally, its production window must not overlap another open production window, because there is one production writer.

## Risks / Trade-offs

- [`admit()` now does more than admission] → One-line comment plus a recorded rename for the next front-door recreate (D2). The behavior is covered by tests.
- [A recognized-shape body from a future Sub2API version adds a second collection or nested lists] → It counts as unrecognized and passes through unchanged with a WARN, so no client sees an error. The WARN is checked in the production verify.
- [Cache headers on the plain list change (Sub2API ETag/Cache-Control → front-door ETag/`private, no-cache`)] → They match the parameterized list. Clients only lose a stale validator.
- [Memory on the front door] → The plain list is tens of KiB for about 400 entries. The fragments plus the concatenation need about 2× the body, far below the 320 MiB container limit. Sort cost is about 3.5k comparisons per request (well under 1 ms), and requests are already serialized by the admission slot.
- [Graceful reload is unproven in this container] → Task 1.4 proves it locally first. The recreate fallback needs explicit approval.
- [No Ansible path for tracked front-door files] → Recorded gap. Every such deploy is a reviewed manual window with backup and SHA checks.
- [Partial deploy (file uploaded, no reload)] → Already-running workers keep the old cached module, so behavior stays old until the HUP. Verification compares the live body, not the file.

## Migration Plan

1. Merge the code and tests; CI must be green.
2. Pre-capture with one temporary normal-client key:
   - plain `/v1/models` and `/models`: bodies, ID lists, header sets;
   - the Codex manifest `?client_version=<current>`;
   - an unauthenticated request, which today returns the opaque 404;
   - front APISIX container ID, StartedAt and restarts.
3. Window:
   - back up the live Lua file under `.ai-ops-state/sort-public-models-list-<ts>/`;
   - upload the reviewed file and check its SHA-256;
   - send HUP;
   - check health and the error log (no Lua errors).
4. Post-capture and compare (task 3.3).
5. Rollback: restore the backed-up file, check its SHA-256, send HUP again, and repeat the comparison against the pre-capture order.
6. Delete the temporary key with proof. Write the record.
