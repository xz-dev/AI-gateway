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
- `ops/ansible/playbooks/deploy.yml -e component=apisix` uploads only `compose.yaml`, `compose.override.yaml` and `.env`. If they are unchanged it reports a verified no-op and does not restart anything. `deploy-config.yml` accepts only `aisix`, `egress-proxy`, `cpa-model-sync` and `model-catalog`; the shared `ai_ops_managed_file` role is wired to those roles only. No playbook deploys the front door's `apisix.yaml` or `lua/`. Tracked front-door files have historically been deployed in a reviewed manual window.
- `cloudflared-apisix-relay` depends on `apisix` with `condition: service_started` and no `restart: true`, so a `--no-deps` recreate of `apisix` does not restart it. It is a forking socat relay, so new connections reach the new container.
- The test harness `lua/model_list_intersection_test.lua` runs under `resty` in the pinned APISIX image, uses fake `ngx` and `resty.http` objects, and is executed by `ops/scripts/validate.sh` (and therefore CI).

## Goals / Non-Goals

**Goals:**
- Give the plain public model list the same order rule as model-catalog's `compareCatalogIDs`, with no change to members, record bytes, auth or error behavior.
- Keep the change to the front door's Lua module and its plain route, with a clean entry-point name, activated by one reviewed front-door recreate.

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

### D2: New `_M.list` entry point; `_M.admit` stays the admission slot (user decision)

The module gains `_M.list(_, ctx)`, the plain-list handler: take the admission slot through the existing `_M.admit`, then D1. `_M.admit`, `_M.release` and `_M.run` keep their current behavior and names.

`apisix.yaml` route `ai-api-models-get` changes its rewrite-phase pre-function from `require("model_list_intersection").admit(_, ctx)` to `require("model_list_intersection").list(_, ctx)`, with the same `if status then return status, body, headers end` shape. Its log-phase `release()` post-function, `upstream_id: sub2api` and `plugin_config_id` stay unchanged. The upstream is no longer contacted by the proxy phase, because `list()` always returns a status, but keeping it leaves the route's static shape and `apisix test` unchanged.

`apisix.yaml` is a single-file bind mount (Context), so the route change takes effect only when the front door is recreated (D5). The user accepted that cost for a clean name.

*Rejected alternative*: reuse `_M.admit` as the plain-list handler and activate by `SIGHUP` reload without touching `apisix.yaml`. It avoids the recreate but leaves a misleading entry name.

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

### D5: Activation by one front-door recreate in a reviewed manual window (user decision)

Production changes two tracked files: `services/firewall/apisix/lua/model_list_intersection.lua` and `services/firewall/apisix/apisix.yaml`. No Ansible path can carry them (Context): `deploy.yml -e component=apisix` is a no-op when Compose and `.env` are unchanged, and `deploy-config.yml` has no front-door component. So, as in earlier front-door migrations, the deploy is a reviewed manual window. Steps:

1. Back up both live files.
2. Upload both and check each SHA-256 against the reviewed commit. Upload order is safe either way: the running container keeps the old `apisix.yaml` inode, and the new module still exports an unchanged `_M.admit` for the old route.
3. Check traffic: sample the front listener's established TCP connections three times, 10 s apart (`/proc/net/tcp` and `/proc/net/tcp6` inside `ai-gateway-apisix-1`, state `01`, local port 9080). Proceed at a low point and record the counts. If traffic stays high for the window budget, stop and ask the supervisor.
4. Recreate only `apisix` with the component playbook's secret-free argv: `env -i PATH=<remote PATH> docker compose ... up -d --force-recreate --no-deps --no-build --pull never apisix`. The user accepted that this cuts in-flight public streams.
5. Wait for `healthy`, then check the error log for Lua or route-load errors.

Rollback: restore both backed-up files, check their SHA-256 against the backup, recreate `apisix` the same way, and repeat the comparison against the pre-capture order.

`deploy.yml -e component=apisix --check --diff` reports `changed=0` before and after; it guards Compose and `.env` drift only.

*Alternative*: an Ansible managed-file deploy for tracked front-door files. That is new scope, recorded under Risks.

### D6: Independence from union-catalog-membership

This change adds one requirement to `apisix-cpa-gateway`. No requirement in union-catalog-membership's deltas is modified. Union's "Catalog output order" requirement says only that this list is outside model-catalog's requirement, and that stays true after both changes are archived. This change also depends on no union code: it reimplements the rule in Lua and does not rely on model-catalog's order.

The change is therefore **independent**, with no task 0 gate. Operationally, its production window must not overlap another open production window, because there is one production writer.

## Risks / Trade-offs

- [The recreate cuts in-flight public SSE and WebSocket streams] → Accepted by the user. The window picks a low point by the connection samples (D5 step 3) and records the counts.
- [Route and module must match] → The new module keeps `_M.admit` unchanged, so the old route still works if the module is loaded first, and `list()` exists before the new route loads. Both files activate together at the recreate.
- [A recognized-shape body from a future Sub2API version adds a second collection or nested lists] → It counts as unrecognized and passes through unchanged with a WARN, so no client sees an error. The WARN is checked in the production verify.
- [Cache headers on the plain list change (Sub2API ETag/Cache-Control → front-door ETag/`private, no-cache`)] → They match the parameterized list. Clients only lose a stale validator.
- [Memory on the front door] → The plain list is tens of KiB for about 400 entries. The fragments plus the concatenation need about 2× the body, far below the 320 MiB container limit. Sort cost is about 3.5k comparisons per request (well under 1 ms), and requests are already serialized by the admission slot.
- [No Ansible path for tracked front-door files] → Recorded gap. Every such deploy is a reviewed manual window with backup and SHA checks.
- [Partial deploy (files uploaded, no recreate)] → The running container keeps the old `apisix.yaml` inode and its workers keep the cached module, so behavior stays old until the recreate. Verification compares the live body, not the files.

## Migration Plan

1. Merge the code and tests; CI must be green.
2. Pre-capture with one temporary normal-client key:
   - plain `/v1/models` and `/models`: bodies, ID lists, header sets;
   - the Codex manifest `?client_version=<current>`;
   - an unauthenticated request, which today returns the opaque 404;
   - front APISIX container ID, StartedAt and restarts.
3. Window (D5):
   - back up the live Lua file and `apisix.yaml` under `.ai-ops-state/sort-public-models-list-<ts>/`;
   - upload both reviewed files and check their SHA-256;
   - sample established connections and pick a low point;
   - recreate only `apisix`;
   - check health and the error log (no Lua or route-load errors).
4. Post-capture and compare (task 3.3).
5. Rollback: restore both backed-up files, check their SHA-256, recreate `apisix` again, and repeat the comparison against the pre-capture order.
6. Delete the temporary key with proof. Write the record.

## Resolved decisions

- Activation (user, 2026-10-28): new `_M.list` entry plus the `apisix.yaml` route change, activated by one front-door recreate in a reviewed manual window. The HUP-reload design was rejected.
- Response headers (user, 2026-10-28): a reordered `200` carries the front door's `Cache-Control: private, no-cache` and a strong ETag of the final body, with `304`/`412` support, as on the parameterized intersection (D4). Sub2API's own ETag and Cache-Control are not forwarded.
