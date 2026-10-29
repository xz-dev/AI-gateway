This change is independent of union-catalog-membership (design D6). It has no spec overlap that needs a gate. Its production window (section 3) must not overlap another open production window.

## 1. Front-door Lua and tests

- [x] 1.1 Add tests to `services/firewall/apisix/lua/model_list_intersection_test.lua` first, for the plain route's new handler (`_M.list`, route `ai-api-models-get`). The fake `ngx` should cover `/v1/models` with no query, with `client_version=`, and with `foo=1`, plus `/models`. Each case is listed below.
  - order: `z/a`, `b`, `A/b`, `B`, `a`, `a/b` → `B`, `a`, `b`, `A/b`, `a/b`, `z/a`;
  - code points: `é`, `z`, `😀`, `Z` → `Z`, `z`, `é`, `😀`;
  - equal IDs keep Sub2API order;
  - byte fidelity:
    - large integers and unknown fields in records;
    - escaped keys;
    - the `data` and `models` envelopes;
    - other top-level fields, kept;
  - the empty list;
  - non-2xx (401, 404, 429, 500): status and body unchanged, no reorder;
  - unrecognized 2xx returned unchanged with a WARN logged:
    - non-JSON;
    - no collection;
    - two collections;
    - a non-object element;
    - an empty or non-string ID;
    - a conflicting `id`/`slug`;
    - a duplicate identity key;
  - ETag, 304 and 412 on the ordered body;
  - `Cache-Control: private, no-cache` and `Vary` include `Authorization`;
  - the request reaches the relay host and port with the caller's path, query and `Authorization`;
  - the admission slot is acquired and released, including the busy case → 503 with no upstream call;
  - the 64 MiB limit gives 502, as on the intersection's basic leg.

  Also add:
  - one intersection test: a model-catalog fixture in non-sorted order comes out in that same order, so the intersection is not reordered;
  - one `_M.admit` test: it still only takes the slot (returns nothing when free, 503 when busy) and makes no upstream call, so the old route keeps working with the new module.

  Verify: the new tests fail against the current file under `resty` in the pinned APISIX image (the command in `ops/scripts/validate.sh`).
- [x] 1.2 Implement design D1–D4:
  - in `model_list_intersection.lua`, add `_M.list` (admission through the unchanged `_M.admit`, then D1); `_M.admit`, `_M.release` and `_M.run` keep their behavior;
  - recognition, the `bare`/`id`/`index` sort and the raw splice reuse the existing helpers;
  - response policy as in D4;
  - in `services/firewall/apisix/apisix.yaml`, route `ai-api-models-get`'s pre-function calls `.list(_, ctx)` instead of `.admit(_, ctx)` (design D2); everything else in the route stays.

  Verify: all old and new harness tests pass under `resty`, and `git diff --stat` touches only the two Lua files and `apisix.yaml`.
- [x] 1.3 Run the gateway validation:
  - move `compose.override.yaml` to `/var/tmp`;
  - run `./ops/scripts/validate.sh .env.example`; this includes `apisix test` on the front config, the Lua harness and `ops/scripts/tests/test-catalog-routes.sh`;
  - always move the override back.

  Verify: exit 0, with any existing override restored (`test -f`). In an isolated worktree with no override, confirm it remains absent (`test ! -e`).

## 2. Docs and commit

- [x] 2.1 Update `docs/catalog.md`. The "Unparameterized `/v1/models`" note becomes: the front door fetches Sub2API's list and returns it in the catalog output order, with members and records unchanged; it covers pass-through, cache headers, and the admission slot. Also update the front APISIX README if it describes the plain route. Verify: the local link check reports 0 broken links, and `git grep -n 'keeps going straight' -- docs services/firewall/apisix/README.md` returns nothing.
- [ ] 2.2 Commit unsigned with explicit pathspecs (`git -c commit.gpgsign=false commit -- <paths>`) and push. Verify: the Validate CI run for the commit is green.

## 3. Production (reviewed manual window, `openai/*` model)

- [ ] 3.1 Pre-checks and pre-capture, read-only, into `/var/tmp`:
  - `ansible-playbook playbooks/deploy.yml -e component=apisix --check --diff` from `ops/ansible/` reports `changed=0`;
  - the live Lua file's and `apisix.yaml`'s SHA-256 equal the parent of the change commit (neither is drifted);
  - create one temporary normal-client key;
  - capture the plain `/v1/models` and `/models` bodies, ID lists and headers;
  - capture the Codex manifest `?client_version=<current>`;
  - make an unauthenticated plain `/v1/models` request;
  - record the front APISIX container ID, StartedAt, health and restarts.

  Verify: all captures are present, and the playbook reports `changed=0`.
- [ ] 3.2 Window (design D5):
  - back up the live `services/firewall/apisix/lua/model_list_intersection.lua` and `services/firewall/apisix/apisix.yaml` under `.ai-ops-state/sort-public-models-list-<ts>/` on the host (mode 0600 in a 0700 directory);
  - upload both reviewed files and check that each SHA-256 equals the commit;
  - sample the front listener's established connections three times, 10 s apart, and proceed at a low point; if traffic stays high, stop and ask the supervisor;
  - recreate only `apisix` with the component playbook's `env -i` argv: `up -d --force-recreate --no-deps --no-build --pull never apisix`, wrapped in `timeout`.

  Verify:
  - the connection samples are recorded;
  - health is `healthy` with restarts=0 on the new container, and `cloudflared-apisix-relay` was not restarted;
  - the error log since the recreate has no Lua or route-load errors.
- [ ] 3.3 Post-capture and compare against 3.1. Verify:
  - the plain `/v1/models` and `/models` ID **sets** and counts are identical to the pre-capture;
  - every entry parses equal to its pre-capture entry by ID;
  - the ID **order** equals bare-first code-point order, computed independently in Python by sorting the IDs by `(("/" in id), id)` with code-point string comparison;
  - other top-level fields are equal;
  - `Cache-Control` is `private, no-cache`, an ETag is present, and a repeat request with `If-None-Match` returns 304;
  - the Codex manifest's ID list and order equal the pre-capture;
  - the unauthenticated request still returns the opaque 404;
  - the error log has no WARN for an unrecognized body.

  Any mismatch: roll back (design Migration Plan step 5: restore both files, check their SHA-256, recreate `apisix`), repeat the comparison against the pre-capture order, then stop and report.
- [ ] 3.4 Close-out. Verify all of these:
  - the temporary key is deleted, with proof: DELETE 200, then GET 404, then the local key file is gone;
  - `deploy.yml -e component=apisix --check --diff` still reports `changed=0`;
  - `docs/records/sort-public-models-list-<date>.md` is written in English, with no public hostname, IP, keys or private values (it says "the public hostname"). It records the captures' counts and hashes, the connection samples, the recreate evidence (old and new container IDs, health) and the rollback commands, and it is committed with pathspecs and pushed;
  - CI is green.
