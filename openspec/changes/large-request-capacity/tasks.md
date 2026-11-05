Milestones (design: Implementation milestones):
- **Milestone 0** (section 0) proves the red baseline first.
- **Milestone 1** (sections 1–3) is the work on the hops we own.
- **Milestone 2** (sections 4–7) is the third-party patches.
- **Milestone 3** (sections 8–9) is the full-chain lock and the single production window.

Milestones 0–2 end in local and CI acceptance only. No artifact deploys to production before task 9.2 (不过渡).

## 0. CI harness and red baseline — milestone 0

- [ ] 0.1 Promote the audit harness into `ops/scripts/capacity/`:
  - `gen.py` (padded to exact bytes): an image-heavy shape whose inline part is a fixed constant of at most 4 MiB at any size (review T1; the "about 88 % media" figure is withdrawn), and a text-heavy shape of strings ≤ 16 KiB with more than 8192 items at 32 MiB and above;
  - the streaming mock upstream (codex/chat SSE, Anthropic format, and fault modes: H2 reset before headers, HTML 403, provider 413);
  - `wsclient` and `chain_ws.sh`;
  - `harness.py` (cgroup `memory.peak`, `memory.stat` anon and file pages, and the time at each hop).

  Copy only these files, never `logs/` or transcripts, and keep only loopback addresses and fake keys. **Verify:** `check-tracked-inventory.py` passes, a grep finds no private address, hostname or key, and the generator asserts that the 245 MB, 256 MiB and 1 GiB image-heavy rungs carry less than 8 MiB inline.
- [ ] 0.2 Reconcile the tracked CPA pin with production: `compose.yaml` and `.env.example` move from the official `v8.0.45` to the deployed `v8.0.51-pr7134-1a196bd` digest. **Verify:** `validate.sh .env.example` passes, and the tracked pin equals the running image.
- [ ] 0.3 Add a compose capacity project for the full chain with production `mem_limit`s, no swap, Squid in front of the mock, and spool and ledger volumes on a size-limited loop-mounted ext4 filesystem. Run it under a parent cgroup whose `memory.max` is the production RAM, and add the CI-only meminfo source (design D10); the production validator rejects that variable. Post the Sub2API compliance acknowledgment automatically in this project only. **Verify:** a validator fails if the acknowledgment appears in production compose, Ansible or private-config templates.
- [x] 0.0 Local Milestone 0 measurement of today's unpatched hops (memory and disk multiples, overlap, upload time), with a fresh container per request and the mock upstream only. Result: design Context "Milestone 0 measurement"; runner `run-m0.sh`.
- [ ] 0.4 Promote the Milestone 0 runner `run-m0.sh` (with its `gen_m0.py`, `hops.py`, `m0.py`, `overlap.py`, `upload.py` and `summarize.py`, no `logs/` or `raw/`) into `ops/scripts/capacity/` as the red-baseline deliverable, and extend it into the ladder runner with the exact image-heavy and text-heavy rungs and the pass criteria of design D10. **Verify:** on today's production pins, record the red baseline before any patch, with the entry point of each expectation (design D10):
  - through the firewall, a body above 16 MiB gets the nginx 413, and an HTTP body of exactly 16,777,216 bytes passes nginx;
  - at the CI-only keepalive entry, the keeper-expanded WS step above 16 MiB gets bridge 1009 and a keeper bare close, and 67,108,864 bytes hits keeper's hard limit or a Sub2API or CPA OOM with an empty reply;
  - a text-heavy 32 MiB body at the keepalive entry OOM-kills Sub2API or fails in another recorded way;
  - a text body of more than 8192 items gets keeper's item-count 400.

  Record what was actually observed, not only the prediction, and compare it with the M0 results.
- [ ] 0.5 Measure the inline bytes (the bytes outside allow-listed media values) of the owner's heaviest real sessions, recording aggregate numbers only, never content (review m5). **Verify:** every measured session stays below the 8 MiB ticketed limit; if any does not, report to the user before task 9.2.

## 1. Keeper spool and admission gate (P15 base) — milestone 1

- [ ] 1.1 Start from the parked P15 tree (manifest `03fd6345…`) and fold the P-series in-memory fixes into it. **Verify:** the P14/P15 tests pass, and anonymous memory is flat at 64 MiB.
- [ ] 1.2 Raise `MAX_REQUEST_BYTES` and `maxHistoryBytes` to 256 MiB and `STORE_PIN_MAX_BYTES` to 256 MiB. Stream the expanded history into the frame or body, and add the expanded-size pre-check. Apply the D3 spool rules:
  - disk only, and startup refuses tmpfs;
  - 0600 files in a 0700 directory;
  - write-side and read-side `fadvise`;
  - the per-write disk floor (`f_bavail × f_frsize`);
  - anonymous `O_TMPFILE | O_CLOEXEC` spools only, refusing to start if unsupported;
  - close on every exit path (for keeper, after the history record);
  - a startup assert that the spool directory holds no regular file (no sweep, no timer).

  Remove the item-count cap `maxHistoryItems = 8192` (`working.go:25`) and bound history by bytes and accounting only (design D5).

  **Verify:**
  - anonymous memory stays flat at 256 MiB, and `anon + file_dirty + file_writeback + kernel ≤ 384m − 32 MiB` with `oom = oom_kill = 0`, on a filesystem that charges page cache (keeper criterion, D3), with PSI and latency recorded;
  - each D3 rule has a test that fails first;
  - there are no timers, expiries, global request locks, or count or ID caps;
  - a text body with more than 8192 input items (for example 16 MiB of 16 KiB strings with 11,000 items) is accepted, not refused with the misleading 400;
  - per-WS ordering is unchanged.
- [ ] 1.3 Implement the admission gate (design D4):
  - the `capacity-ledger` volume (read-write in keeper only, 0750/0640), `ledger.lock` and ticket files written atomically (`O_TMPFILE` + `fsync` + `linkat(AT_FDCWD, "/proc/self/fd/N", …, AT_SYMLINK_FOLLOW)`, which works with `cap_drop: [ALL]`, review m2);
  - authentication through the existing Sub2API billing revalidation before admitting ≥ 64 MiB, outside `ledger.lock`; 503 `auth_unavailable` naming keeper when the revalidation is unreachable; for chunked bodies, authentication when the received bytes cross 64 MiB (review m6);
  - the disk and memory formulas, including the on-disk keeper store headroom, the measured `k_disk` (7 N) and the `R_mem` terms `c_h` with measured `k_skel`, with AISIX and CPA doubled in full (review m9);
  - 503 `capacity_busy` with `Retry-After`, 507 `insufficient_storage` and 503 `insufficient_memory`, on HTTP and as WS error events;
  - admission for chunked bodies and fragmented messages at the cap, lowered to the real N;
  - stripping of the inbound ticket header and member, and propagation of the ticket; on WS, the ticket is written as the **first** member of the forwarded turn frame (bridge contract, D4);
  - leases, the exclusive-lock release waiter and the startup waiters; an unparsable ticket at startup is a loud startup error naming the file;
  - the live-ticket metric (count, and per ticket age, sizes and inode; no holder PIDs, review m1), with no expiry.

  **Verify:**
  - a body under 64 MiB gets no ticket;
  - an unauthenticated 256 MiB request gets 401 with no ticket and no spool, and an unreachable billing check gets 503 `auth_unavailable`;
  - a ticket links successfully in a container with `cap_drop: [ALL]`;
  - with the test meminfo source at 901 MiB, one 256 MiB ticket is admitted; below `R_mem + 232 MiB` it gets 503 `insufficient_memory`;
  - two concurrent 256 MiB requests on a filesystem sized for one: one is admitted, and the other is refused immediately with no queue;
  - after the first ends, the ledger is empty;
  - after a keeper kill, the restarted keeper clears the ticket once all leases are gone;
  - the floors are never crossed.

## 2. Bridge and APISIX limits — milestone 1

- [ ] 2.1 Bridge:
  - set `MAX_MESSAGE_BYTES` and `MAX_BODY_BYTES` to 268,435,456;
  - apply the D3 spool rules (as in 1.2); the spool is closed at turn end, and its disk stays accounted by the ticket (`k_disk = 1`) for the whole turn. Closing it before the response status would release no credit earlier and could stall an upstream that answers before reading the whole body (M1 deviation, accepted 2026-11-04);
  - set the WebSocket library read limit above the cap, and count message bytes in our own code, including fragments and the inflated permessage-deflate size;
  - hold the ticket lease (read-only ledger mount, blocking `LOCK_SH` on an `O_RDONLY | O_CLOEXEC` fd, `st_nlink > 0` check), taken from the ticket that keeper writes as the frame's first member and held **before any spooled byte**; strip the frame member and forward the header to Sub2API only;
  - refuse a ticketless frame at the 64 MiB crossing, and fail closed without a ledger;
  - free queued spools before the close frame; keep reading the socket after a refusal so a client close still releases the turn;
  - read every spool pass through the page-cache-dropping reader; keep separate disk and memory budgets; no cgroup headroom check on the bridge;
  - deployment: the existing production spool volume is mode 1777 from the old image; recreate it or set it to 0700 in the window.
  - measure the 256 MiB `memory.peak` on ext4 in CI (local acceptance ran on ZFS, which does not charge page cache to the cgroup).

  **Verify:**
  - a 256 MiB frame passes, with `memory.peak ≤ 256m − 32 MiB`;
  - fragmented and deflated over-limit messages get our error event, not a library 1009;
  - a 16 MiB and a 64 MiB frame, which M0 showed closing with 1009 today, pass.
- [ ] 2.2 Firewall APISIX and apisix-models:
  - set `client_max_body_size 256m` and `proxy_request_buffering off` at server or http level (`http_server_configuration_snippet`), not in a location, so the named location `@disable_proxy_buffering` is covered;
  - update `test-standard-cache-topology.py`.

  **Verify:**
  - a 256 MiB body is streamed byte-identical with disk peak 0;
  - memory stays flat (measured, not projected);
  - no route reads the request body.

## 3. Explicit error relay at every owned hop — milestone 1

- [ ] 3.1 Keeper: an inner close, an inner HTTP failure or a local rejection (including the pre-check and gate refusals) becomes a Responses `error` event with code, reason and hop, then the end of the turn. On HTTP it becomes 413, 503 or 507. Keeper never forwards a bare close. **Verify:**
  - the incident replay (a 1.55 MB delta into about 17.9 MB of history, with bridge at 16 MiB) yields an error event and no bare close;
  - with bridge at 256 MiB, the same replay succeeds.
- [ ] 3.2 Bridge: an over-limit frame or body gets a Responses `error` event before the close, and an upstream HTTP error before SSE becomes an error event in the WS turn. **Verify:** 268,435,457 bytes yields `request_too_large` with the limit, never a bare 1009.
- [ ] 3.3 APISIX hops: add a JSON 413 body in OpenAI shape that names the 256 MiB limit. **Verify:** a body above the cap gets the JSON 413, and so does `Content-Length: 1073741824` with `Expect: 100-continue` and no body.
- [ ] 3.4 Milestone 1 acceptance, local only, through keeper → bridge → mock:
  - no hop answers with a bare close or an empty reply;
  - gate refusals and over-cap steps yield explicit errors that name the hop;
  - a request refused by keeper while still uploading, entering through the firewall, gets the JSON 503, never a 502 or a reset;
  - nothing is deployed.

  **Verify:** the replay, the gate and the over-cap steps pass. Record the result.

## 4. Shared skeleton + blob spool contract — milestone 2

- [ ] 4.1 Write the shared contract and corpus:
  - the marker format with a 128-bit nonce and fail-closed collision;
  - threshold T;
  - the per-component tier A allow-list of media and file keys plus `data:` URLs;
  - the inline-bytes limit S: 16 MiB for every request on a covered path, 8 MiB for ticketed requests, lowered per hop where `(mem_limit − 32 MiB − idle) / k_skel` is smaller, with a named 413 when exceeded;
  - the splice escaping rules;
  - skeleton-only logging;
  - the differential and rejection-parity corpus.

  **Verify:** the corpus covers escapes, unicode, surrogates, duplicate keys, big integers, `1e3`, deep nesting, data URLs, affinity tags, signatures, and large inline text (which must stay inline).
- [ ] 4.2 Package the D3 spool rules, the ticket-lease client (read-only fd, blocking `LOCK_SH`, `st_nlink` check, header forwarded only to internal upstreams) and the per-hop memory charge (design D4) as tests that each third-party component reuses. The charge covers `k_skel × inline` for every request plus `k_mem × N + P_h` for a ticketed request (review T2), and is implemented once per language (one Go helper shared by Sub2API and CPA, one Rust module for AISIX). **Verify:** each rule has a failing test before the component patch, including a concurrent-charge test that refuses with 503 `insufficient_memory` instead of crossing `memory.max − 32 MiB`, and a step with one ticketed 256 MiB request under concurrent small-request load.
- [ ] 4.3 Add the provider ceiling table keyed by concrete AISIX route and CPA executor plus credential keys, with exact bytes:
  - Anthropic first-party 128,000,000;
  - Bedrock 20,000,000;
  - Google-managed 30,000,000;
  - other partners 32,000,000.

  The OpenAI file-input limit is not a request ceiling and is not in the table. The codex backend and relays are unknown and are not probed; they use the 256 MiB cap. **Verify:** a body above a known ceiling gets a 413 that names the ceiling and the route, and a provider 413 is relayed unchanged in cause.
- [ ] 4.4 Measure `U_egress` from existing production request logs (bytes and durations through Squid), with no synthetic upload, and compute the largest body that fits in the 120 s window (design D8). Record every hop timeout before AISIX (firewall `send`/`read` 600 s, apisix-models, Sub2API, bridge, keeper) and compare each with the 245 MB path time from CI. M0 measured that AISIX's 120 s covers the request upload (504 at 120.0 s), so 245 MB needs about 20–25 Mbit/s sustained egress (design D8). **Verify:** the values are recorded. If the 120 s window fits less than 245 MB, or any earlier timeout is shorter than the path time, report to the user before task 9.2 together with the D8 options; no timeout is changed silently.
- [ ] 4.5 D8 AISIX timer-start patch (user accepted 2026-11-04): arm `stream_timeout` after the last request-body byte is written, and bound the send by a 120000 ms no-progress timeout. **Verify:** `stream_timeout_ms` stays 120000; a slowly reading upstream that keeps accepting bytes no longer gets the 504 at 120 s; an upstream that accepts no byte for 120 s gets an explicit timeout error relayed to the client.

## 5. CPA fork — milestone 2

- [ ] 5.1 Create the patch branch on the production tree, `v8.0.51` plus the PR #7134 backport (`1a196bd`), with the `internal/bodyspool` prototype, its fuzz tests and a config switch that defaults to off. **Verify:** `go test ./internal/bodyspool` and the fuzz run pass, and the PR #7134 regression tests still pass.
- [ ] 5.2 Wire the patch in:
  - ingest in `ReadRequestBody`;
  - skeleton-only request-log tee and error capture;
  - splice at the codex, claude, openai-compat and codex-WS send points;
  - codex compression and CCH signing over the splice stream, with compression output reserved and spooled;
  - the conditional copy sites converted;
  - the ticket lease, with the header never sent to a provider.

  **Verify:** for each translator, `translate(body)` equals `splice(translate(skeleton))`, the session ID is unchanged, and no blob byte reaches a log file.
- [ ] 5.3 Add per-executor ceilings, the inline-bytes limit and per-hop memory charge, and the fallback limits L (at most 16 MiB) with a named 413 for unconverted executors, body-reading plugin hooks and `/responses/compact` (not converted). **Verify:**
  - the red test at 480m passes 64 and 256 MiB with `memory.peak ≤ mem_limit − 32 MiB`;
  - an over-ceiling body and an above-L fallback body get 413s that name the limit;
  - the H2-reset, HTML-403 and provider-413 mocks give bounded attempts and an explicit error;
  - a provider 413 is request-scoped in CPA: exactly one provider attempt, no credential rotation, and the provider's cause relayed (review m7).
- [ ] 5.4 Open the upstream PR, then build and pin the fork image by digest. If upstream declines, keep the fork long-lived with periodic rebases. Return to an official image only when both PR #7134 and the spool PR are released. Update the `cpa-first-catalog-routing` pin, `compose.yaml` and `.env.example` together. **Verify:** `validate.sh .env.example` passes, and the ladder is green with the fork pin.

## 6. AISIX fork — milestone 2

- [ ] 6.1 Open an upstream issue, then a branch rebased on `upstream/main`, with the `SpooledJson` extractor (T = 0 means off). Keep the content-type check, the read-error message, and a 413 at the limit that also covers chunked bodies. Expect conflicts: upstream has already reworked this area. Carry the same patch into the repo build line (`services/routing/aisix`, `1.8.0-pr1387-06e8075-r2`, `patches/`), reusing its pipeline. **Verify:** rejection parity with serde on the corpus, on both branches.
- [ ] 6.2 Build:
  - the serde-faithful skeleton parse;
  - the streaming outbound writer;
  - positional-read splices for each attempt;
  - content capture on the skeleton;
  - the token estimate on the skeleton (it never reads a blob);
  - the inline-bytes limit and the per-hop memory charge;
  - fallback limits L (at most 16 MiB) for guardrail and bridged-target paths, with a named 413;
  - the ticket lease, with the header forwarded to CPA.

  **Verify:**
  - the differential test gives identical upstream bytes and `Content-Length` on both branches;
  - in `tests/request_body_memory.rs`, a 64 MiB single-string body holds at most 0.05 N and peaks at most 0.1 N plus fixed overhead.
- [ ] 6.3 Add per-route ceilings, a spool volume (no tmpfs), and a fork image pinned by digest, replacing the tracked `1.8.0-rc.1` pin. **Verify:**
  - a 512m container with no swap passes 256 MiB with `memory.peak ≤ 512m − 32 MiB`;
  - the CI-only 1 GiB step passes;
  - `stream_timeout_ms` stays 120000.

## 7. Sub2API fork — milestone 2

- [ ] 7.1 Finish the study red test, `openai_responses_large_body_memory_test.go`, for the OAuth and API-key paths. Add red tests showing that an inner gateway 413 or 507, and a provider 413, are failed over and rewritten today. **Verify:** all fail on `ab6b00e`.
- [ ] 7.2 Add the spool patch with the `gateway.body_spool.*` switch:
  - spool ingest in `ReadRequestBodyWithPrealloc`;
  - `normalizeCodex*Bootstrap`, `ensureReqBody` and `NormalizeCompactionTriggerInputOrder` run on the skeleton;
  - splice output uses `encoding/json` escaping where the skeleton was marshalled;
  - the session hash and content seed are computed over the splice stream;
  - the forward path sends the splice reader;
  - fallback endpoints (other APIs, `/responses/compact`, `/v1beta/*`, `/antigravity/*`, not converted) refuse above L (at most 16 MiB) with a named 413 (leader decision 2026-11-05: 0 requests on `/v1beta` and `/antigravity` in 6 days of production logs);
  - the inline-bytes limit and the per-hop memory charge;
  - the ticket lease, with the header forwarded to apisix-models only and stripped on the Squid relay path.

  **Verify:**
  - the differential test gives identical upstream bytes and session hash, the red test passes, and 256 MiB fits with `memory.peak ≤ 320m − 32 MiB`;
  - a text-heavy 32 MiB body gets the named 413;
  - concurrent text-heavy 16 MiB bodies either pass or get 503 `insufficient_memory`, with no OOM;
  - the patched skeleton path measures `k_skel ≤ 15.1` at 16 MiB, so the 16 MiB inline limit fits in 320m with the 32 MiB margin (today's code measures 17, giving `S_h ≈ 14.3 MiB`). If it does not, report the measured `S_h` to the user before task 9.2 (review m8); do not lower the limit silently.
- [ ] 7.3 Add the passthrough patch. These responses are request-scoped:
  - an inner gateway error whose JSON `code` is `request_too_large`, `insufficient_storage`, `insufficient_memory`, `capacity_busy` or `capacity_ticket_*`, and that carries a `hop` field;
  - **every provider 413, for every request size** (user decision), replacing today's account scope, next-account retry and "Request payload is too large" rewrite in `openai_gateway_upstream_errors.go`.

  For them: no failover and no retry on another account, no account or pool statistic, and status, headers and body passed through unchanged. Other provider errors keep today's classification. Record the trade-off in the PR and docs: an account with a smaller private ceiling no longer fails over. **Verify:**
  - the red tests pass;
  - an over-ceiling route through the full chain yields exactly one AISIX attempt, with the limit and route preserved;
  - an inner 507 produces no failover;
  - a provider 413 from the mock, on a 1 MiB and on a 64 MiB body, reaches the client unchanged after exactly one account attempt.
- [ ] 7.4 Open the upstream PRs (spool and passthrough separately), then pin the fork image by digest (long-lived if declined). **Verify:** account, group and pool state and the OAuth flow are unchanged, as read back through the supported admin API.

- [ ] 7.5 **M2 exit gate (frozen scope, 2026-11-05).** One production-RAM full-chain run on the approved images of all five forks, in production-like mode: 2 cores with the compose cpus quotas, production memory limits and GOMEMLIMIT, THP off per hop to emulate production `madvise`, and keeper reading literal `si_mem_available` (dirty pages count as available). The run adds the hops that earlier smokes skipped: Squid with its production config and limit, the firewall APISIX and the keepalive hops. It also adds one WS turn that falls back to HTTP Responses, and a compressed-body check at keeper and bridge (decoded bytes are charged). **Verify:**
  - 245 MB and 256 MiB are admitted and correct over HTTP and WS;
  - turn 2 arriving 1–17 s and 60 s after a 256 MiB turn 1 is admitted;
  - no oom or `oom_kill` in any cgroup; PSI memory `full avg10` peak recorded with a proposed threshold;
  - no timer fires against the measured longest gap;
  - Squid peak memory and OOM count recorded;
  - swap-in rate recorded during the 245 MB rung;
  - the stricter dirty-subtracting meminfo column logged.
  New observations that cannot change this run's verdict go to the later list, not into this task.

## 8. Full-chain gate and lock — milestone 3

- [ ] 8.1 Run the full ladder (design D10) on the patched pins, with these extra steps:
  - concurrency, memory admission, disk floor and ticket release (keeper kill and inner-hop kill);
  - the text-heavy rungs and the per-hop memory charge;
  - authentication first, refusal during upload and ledger permissions;
  - inner errors, provider 413 passthrough and fallback paths;
  - PR #7134 faults;
  - the throttled-egress step at the measured `U_egress`.

  **Verify:** every red-baseline step from 0.4 is now green, and every expected explicit error has the expected code, limit and hop.
- [ ] 8.2 Add the CI workflow: the ladder on every run, and a nightly 1 GiB run that raises the cap to 1 GiB on the throwaway stack only, on a runner with at least 12 GiB free. When green, publish `compatibility-lock.json` as a release artifact, with digests, config hashes, peaks, `k_disk`, `k_mem`, `k_skel`, the per-hop inline-bytes limits `S_h` and the fallback limits L. The pass criteria include each spooling hop's disk peak ≤ `k_disk,h × N` (review m4). **Verify:** a red ladder publishes no lock, and the production cap stays 256 MiB.

## 9. Deployment and docs — milestone 3

- [ ] 9.1 Ansible and compose:
  - spool volumes for every spooling hop, and the shared `capacity-ledger` volume with its group, mounted read-write in keeper only and read-only in bridge, Sub2API, AISIX and CPA;
  - a validator for the ledger mount modes, the same-device check for the ledger and spool volumes, and the absence of the CI-only meminfo variable;
  - caps, thresholds, switches and nginx `proxy_request_buffering off`;
  - image pins taken only from a published compatibility lock;
  - a pre-deploy record of `statfs` free space, the keeper store headroom and MemAvailable, with the largest admissible body. If not even one 245 MB footprint fits, report to the user before the window.

  **Verify:** the playbook check mode shows only the declared changes, and AISIX `stream_timeout_ms`, the Squid ACLs, the ingress auth and the namespace topology stay unchanged.
- [ ] 9.2 Roll out in the design order within one window, with no interim cap. No milestone 0–2 artifact may have been deployed before. **Verify:**
  - each step's acceptance from the design;
  - a realistic 8 MiB production request passes;
  - the origin 1 GiB check gets the JSON 413;
  - the disk reserve and the MemAvailable floor hold, and the ledger is empty when idle;
  - the namespace topology is unchanged.

  Record the results in `docs/records/`.
- [ ] 9.3 Update `docs/responses-path.md`, `docs/websocket-termination.md` and `docs/architecture.md` with the cap, the per-route ceilings, the admission gate, the spool and ledger locations, the time budget and the on-disk data note. Add the leaked-lease runbook to `docs/operations.md`: read the live-ticket metric, match the ticket's inode in the host's `lslocks` or `/proc/locks` (PIDs inside keeper read as 0 across namespaces), map the PID to its container, restart only that hop, and never delete a ticket file while a lock is held. Record in `docs/websocket-termination.md` the explicit exception that allows the Sub2API spool and passthrough patches.
- [ ] 9.4 Run `openspec validate --all --strict` and `ops/scripts/validate.sh`.
- [ ] 9.5 After the M3 deploy, re-run the read-only exposure check (leader decision 2026-11-05): the front APISIX allowlist has no `/api*` or `/*` route, cloudflared has a single ingress to the front, and no container binds `0.0.0.0` or a public address. The admin exception (no keeper L on `/api/v1/admin/*`, opaque admin WS) holds only while admin is reachable from loopback and the tailnet operator relay, never from public ingress.
