# Controlled gateway rollout — 2026-10-23

One production writer, no VM reboot. Order: bridge → models-enricher → recovery guard → keeper experiment. Each step requires fresh configuration/container identity reads, protected backup, narrow activation, readback, bounded real continuation checks, and exact-tip CI before the next step. Local ignored `ansible/private-config` is the push-model source; the host Git checkout is not pulled or reset.

## Bridge — deployed

- Source: `responses-ws-sse-bridge` `83bb9819a5afbda0dbf0c6c7af998d92de82adae`; index digest `sha256:9b5c28879f521bd52d13183a76fc7625271c317b07e571a5f5a945088c760fb6`. Running OCI revision matches. `PORT=8086` stays aligned with `LISTEN_ADDR=127.0.0.1:8086`; the new binary probes HTTP `/healthz`, not a shared-namespace TCP listener.
- Ansible front-component plan approved with digest `489423bb67fe1a9b8b477c1902d735582c3a1ea99b8c985d2fd4ad5d886cf815`. Apply succeeded; bridge-only replacement started **05:20:42 UTC**. All other 54 project containers retained their identities and start times. Existing sockets closed during this authorized interruption; a keeper `502 stored=false` at 05:20:41 is not part of the earlier 04:47–04:58 upstream-rate-limit event.
- Private snapshot: `/root/AI-gateway/.ai-ops-state/front-responses-ws-sse-bridge-jjkkoq9r` (0700 directory, 0600 files). Explicit guarded rollback uses a fresh `--tags rollback -e service=responses-ws-sse-bridge -e ai_ops_mode=plan` and separately approved digest. No production rollback drill was run.
- Readback **05:22:04 UTC**: bridge/keeper/keepalive/public APISIX healthy, no restart-count/OOM change; explicit bridge CLI healthcheck exit 0; `ai-gateway.service` active/success; full-stack Compose dry-run has no create/recreate/start/pull/build pending.
- Four real calls, zero retries: bare `gpt-6.1-sol` SSE marker plus parent+delta recall; slash `codex/gpt-6-sol` WS marker plus parent+delta recall on a **new socket**. All terminal statuses `completed`, both exact recalls passed. Keeper 05:22:47/49 HTTP and 05:22:51/05:23:01 WS records have `stored=true`, paired `previous=false/true`. Sub2API request IDs: `23b58601-e4d8-44fc-afe1-42f9652b241e`, `a3eca95c-4022-4b40-88b1-1bede6829118`, `9ed388f4-18f2-4b92-953d-d2aed51612b6`, `b52ec2d6-fa56-4e5d-9e1c-8ae7f26aa179`. Bare GPT avoided repeating the recently rate-limited Opus channel; no fallback/retry was used.
- Production/private-config SHA256: compose `b627271ab5fa8cb52588d2ffc3b8e109caec21afdf8e451656d9b06980f370bd`; override `b3f46dcb7c8284845ecf247b1f225c4e7dff08684aa4b09d9e68822249ae6521`; `.env` `0934ac93dee1028d0198fefdda6fd546f5d7e8ab67bf7cb020cc4f657827607b`. This three-file equality does not claim that every managed file was checked.
- Local topology regression 11/11 and `git diff --check` pass. First smoke harness launch lacked Python `websockets` and made **zero** calls; rerun used the existing cached dependency via `uv run --offline --with websockets`, not a production retry. Private evidence: `/var/tmp/deploy-batch-20261023/bridge-*`.

## Models-enricher — deployed

- Published on main `68a37627e1649b50c966c0c886a17679bd308f77` by [39905857374](https://github.com/xz-dev/AI-gateway/actions/runs/39905857374). Its `models-enricher` tree equals the approved `af3376d` tree (`79b3c30e75d4285be051970a06269cbdd89bd60b`); workflow blob also matches (`0ba4e3c495e4d24a3616009fd71150cea4f22c3b`). Build context is only `./models-enricher`; no other source tree is copied. No extra release refs were created.
- Pin: `ghcr.io/xz-dev/ai-gateway-models-enricher:scheduled-refresh-af3376dc95c5@sha256:432e88475f0131475caac054a9d8731bf1aedc119f40ab1389739c8294949310`. GHCR index, linux/amd64 manifest/config SHA256 and source revision were independently read back. This workflow publishes amd64, not a newly claimed dual-architecture image.
- Existing generic Ansible path used; only this container changed. Readback **06:00:38 UTC**: all 54 neighbours unchanged; health and actual `/readyz`-based CLI probe pass; running OCI revision is `68a3762`. Existing production YAML preserved with SHA256 `8a71b24832525e4964e3266b58954e3193dfb4b6a9b6bf0d745c59ed85e4ad2e`; defaults 30m schedule / 10m entry window apply without overwriting private settings. Three-file equality verified: compose/override unchanged from bridge; `.env` now `dbba913363c27346fb44d3befb612a44566fc770842f7548dcf5639dbd3021d6`.
- Single local `:9083/models-table` GET at **05:59:42 UTC**: HTTP200, 334.894ms, last success `05:58:54Z`, next schedule `06:28:44Z`. This is a warm-page production measurement, not a cold-page or scheduled-tick acceptance. Cold/nonblocking/manual/single-flight behaviour has source-level integration evidence in the Lane B receipt; no extra refresh POST was sent here.
- Three public `/v1/models` GETs, zero retries: **4.525s / 0.390s / 0.702s**, all HTTP200 / 361 models. A short checkpoint, not p95 or proof every Pi discovery meets its deadline.
- Four real inference calls, zero retries: same bare GPT SSE and slash Codex WS new-socket marker/parent-delta pairs, all `completed` with exact recall. Keeper HTTP 06:00:46/48 and WS 06:00:51/56 are `stored=true`, paired `previous=false/true`; Sub2API request IDs `8ea020f1-db3f-4171-b4e5-823ec1b87f8e`, `5ad26c10-4faf-4ecf-a0a3-3ecb2c7dae93`, `be93057b-0fcd-40d3-ac86-cb9a8526c9b5`, `ba15f5b7-2eb4-4564-8a0c-915b82675953`.

### Two stopped apply attempts, restored before activation

First apply uploaded the candidate `.env` but failed before pull: a space-bearing `-e key=value` command override was parsed as `env` alone. CAS proved all 55 containers unchanged. Only `.env` was restored; its prematurely updated generic baseline was backed up and restored under separate supervisor approval. The second apply used the correct default `docker compose`, but existing `pull_policy: never` made `compose pull` return success without fetching; `up --pull never` correctly refused the missing image. Again all 55 containers were unchanged, and `.env` plus its baseline were CAS-restored before continuing. Neither attempt sent inference smoke or recreated a service.

Supervisor-approved minimal fixes: `9e9a8b0` forbids implicit pulls during generic activation (CI [39906312680](https://github.com/xz-dev/AI-gateway/actions/runs/39906312680) success); `c50ac22` forces the explicit pull branch to `pull --policy always` (CI [39908152120](https://github.com/xz-dev/AI-gateway/actions/runs/39908152120) success). The preloaded/`pull:false` branch still requires an existing local image and never pulls. Production Compose v2.40.3 supports `--policy`. Regression was red before each fix; topology 12/12 and full `scripts/validate.sh .env.example` pass with an explicit empty test override, leaving ignored operator override untouched. Initial local validate hit that unrelated private override's missing variable; it was not a source failure.

Third apply, explicitly capped as the last allowed attempt, succeeded after a fresh Ansible `config --quiet` argv proof, plan and external hash/container CAS. The legacy profile has no approval digest; no front-profile digest is invented for it. Group backup and receipts: `/var/tmp/deploy-batch-20261023-enricher-before` (0700/0600); local evidence `/var/tmp/deploy-batch-20261023/enricher-*`.

### Generic profile follow-ups (not fixed here)

- Plan does not exercise/validate the actual pull/activation command; a valid plan alone did not catch a malformed command override.
- File baselines advance before pull/recreate succeed; failed apply can leave a baseline describing undeployed configuration.
- Group-transaction CAS, automatic readiness/digest/image-present checks and all-neighbour verification are still absent from this legacy profile. This batch supplied external CAS, grouped private backups, image/revision/readiness and neighbour readback; they are not newly guaranteed by generic Ansible.

## Recovery guard — deployed

- `deploy-recovery` plan changed only `gateway-recovery.py`; apply succeeded. Private pre-operation four-file plus Compose snapshot: `/var/tmp/deploy-batch-20261023-recovery-before` (0700/0600), local receipt `/var/tmp/deploy-batch-20261023/recovery-rollback-receipt.json`. Guard SHA256 `8235b2b9c60ee5c7c56313ef0722f36e0bec29676caaaf8fde277c7c6a7cee90`; shared role probes SHA256 unchanged `3db2597b01e5a43218c769e17d526c314aca58543d8efbfd30fb222e115ebc8b`; unit files unchanged.
- Actual timer cycle **06:09:29 UTC** reports eight application checks, all `ok=true`, including bridge8086. `startup_unit` reports `active`, `ok=true`, `action=observe_only`; timer is enabled/active. Coverage is 55 running / 55 observed, including the standalone relay; this count is not a claim that provider dependencies are all ready. No restart action was taken.
- At 06:09:53 all 55 project-container identities and all three Compose hashes are unchanged; production/private-config still equal. Recovery regression 22/22 passes.
- Four real smoke calls, zero retries, all completed with exact SSE/WS new-socket recall. Keeper HTTP 06:10:01/03 and WS 06:10:06/08 each show `stored=true`, `previous=false/true`; correlated Sub2API IDs `863f2aec-37f4-4947-bc83-e21888396b20`, `c54639cc-974a-407e-9aff-dff200ac1ff1`, `9bd10322-a7aa-4c77-b152-93dcfbebd30f`, `162a5822-52df-490d-a065-f2b172637941`. Concurrent unrelated traffic was not counted as smoke.

## Keeper — 24h experiment deployed (2026-10-23)

Source/submodule `65ef67cd4cff0d15f7f75c7d338575058f274bd1`; dual-architecture index `sha256:6ae3cc3c4883ca447ee898e82ef52c56398eadb602059eba1a41721280baef1a`. Registry index, both platform manifests/configs and OCI source revisions were independently verified. Defaults/example are `10m / 100 / 0`; production `.env` overrides `24h / 10000 / 0`. Access LRU, write-time TTL and wipe-on-start are unchanged. The pre-rollout baseline was `10m / 200MiB payload cap / entries unlimited`, not `10m / 100 / 0`.

The remote monitor `scripts/ops/keeper-observe.py` is bounded to 24h with approximately 60s sampling. It records cache bytes/files, both filesystem free-byte values, RSS, cgroup `memory.current`, `inactive_file`, working set and CPU deltas. Working set is `max(0, current - inactive_file)`. A working-set/RSS sample ≥90% of `memory.max` (about 345.6MiB for keeper's 384MiB limit; fallback 345MiB if unlimited), or a ≥60s CPU mean exceeding `min(1 core, 80% of quota)`, writes an event and stops monitoring **without changing production**. The profile stays experimental if this happens; an operator must review it.

**Only free disk <4GiB can trigger unattended rollback.** Cache ≥1GiB is record-only, with one `cache_1gib_crossed` note: 10000 aliases can legitimately exceed 1GiB. On low free disk alone the monitor attempts keeper-only TTL10m/entries100, same image/bytes0, then exits. It uses the shared front lock, fresh file/container CAS, private backup, rendered two-env-only proof, explicit `--no-deps --no-build --pull never`, and image/environment/health/neighbour readback. Any unknown or drift stops, never loops. Reversion is not guaranteed to reclaim disk occupied by other services or bbolt high-water pages.

Samples and baseline are 0600 in a 0700 directory; `/var/lib/ai-gateway-keeper-observe/EVENT.json` is written 0600 on a stop/revert/end (absent while observing normally). Event receipts include current Compose/environment hashes and a private-config re-adoption command; the coordinator checks them, without a push notifier. Completion counts follow requested log definitions (`previous=true status=200` hit proxy, `previous=true status=400` miss proxy, `previous=false` full-history/new-turn proxy, stored flags, last-reported alias/byte values). These are not proof of actual expanded history or successful model output: WS status200 can include an upstream error, and a new turn also has `previous=false`. Client bytes avoided cannot be measured from these logs. Baseline and window carry the known **04:47–04:59 UTC** 429-outlier interval separately.

Offline monitor tests 12/12 pass, including record-only 1GiB crossing, page-cache exclusion, working-set stop, CPU stop, one-shot disk fallback and CAS refusal. Mutations that authorize memory rollback, omit CAS or count inactive page cache as working memory are rejected. Updated keeper gofmt/test/race/vet and full template validation pass with ambient `CPA_API_KEY` cleared. The initial offline fixture had an incorrect argv suffix assertion, corrected before these passes; no production/inference calls were involved.

### Actual deployment and bounded completion

- Plan approved digest `50f56c44301a098980b5b2fdda93fa7f2e288eaf108171f4cfbda1cd0f372063`. Keeper activated **06:49:27 UTC**, container `735f50dc89b6`, with exact pin/revision and `STORE_TTL=24h`, `STORE_MAX_ENTRIES=10000`, `STORE_MAX_BYTES=0`. Namespace remains `net:[4026532500]`, owner `c6c96e158b02`; all 54 neighbours retain ID/image/StartedAt. Resources remain memory/swap384MiB, CPU0.5, PID64, read-only, GOMEMLIMIT256MiB.
- The local Ansible controller hit its **300s overall timeout after activation**, before writing its final receipt or releasing its lock. **No Ansible exit0 is claimed.** No second apply was run. Supervisor independently confirmed healthy candidate and approved bounded completion, rather than a destructive second wipe. Readbacks verified image/revision/env/namespace/all neighbours, exact production/candidate/private hashes, private snapshot permissions and absence of local/remote controllers. The empty root-owned lock inode264357 was created **06:46:30Z**, within this transaction and before activation; its inode/ctime were rechecked before removal.
- Snapshot `/root/AI-gateway/.ai-ops-state/front-openai-api-cache-keeper-hg8udcya` is 0700; old configuration files are 0600. Explicit `recovered-controller-timeout.json` receipt was written there at **06:59:22Z**, recording `ansible_exit_code=null`, checks, hashes and lock release. Only this batch's stale `.env.baseline` (`dbba9133…`→`aabb67d5…`) and `compose.yaml.baseline` (`b627271a…`→`212d6695…`) were backed up and CAS-adopted from live files. Override baseline and keeper last-pointer already matched. Historical snapshots and bridge last-pointer were preserved.
- First smoke attempted TLS handshake only, failed with `SSL: UNEXPECTED_EOF_WHILE_READING`, and never reached HTTP or returned a response ID: **call0**. It was not silently retried. Supervisor checked healthy real traffic/public reachability and granted one fresh four-call budget. That budget passed, without inference retries or model changes: SSE3.364s/2.307s, WS2.838s/4.386s, exact marker recall on a new WS socket, all completed. Keeper HTTP **07:02:44/46** and WS **07:02:49/54** are paired previous=false/true and stored=true; correlated Sub2API IDs `849e3432-c009-4108-b72f-9bee382382af`, `cd9bc41e-275e-4bdd-b62f-746c45b188e0`, `7dbe6969-ef67-43a2-ab47-67bd291e1c96`, `319c1b79-cb80-4ce3-8d74-c4a822375249`.
- Natural-traffic checkpoint **06:49:27–07:02:56Z**: 95 completion logs including those four smoke calls (91 other turns), 51 parent requests/status200, zero status400 parent misses; stored=true93/false2. These status proxies do not prove that all 95 model outputs succeeded.
- Three production files re-adopted with rsync at **07:09:07Z**, then SHA256 equality checked. Final stack dry-run has no pending create/recreate/start/pull/build. Gateway startup unit remains active/success; recovery timer remains active. Production hashes: compose `212d6695a8a538b6eadcdb272a80231b6643f95078fb7d0edde0aa0d33019de4`, override `b3f46dcb7c8284845ecf247b1f225c4e7dff08684aa4b09d9e68822249ae6521`, `.env` `aabb67d553429f0f6259c9f9ea2d3be7339c58a8424b38f7783743d0b3532c8d`. This does **not** assert equality of all managed files; see historical debt below.

### Armed remote monitor

Unit **`ai-gateway-keeper-observe-20261023.service`**, observation **2026-10-23 07:05:35Z → 2026-10-24 07:05:35Z**. Unit RuntimeMax is 24h+5min solely for bounded final sampling/fallback completion; the observation window is 24h. Monitor is root-only, CPUQuota10%, MemoryMax192MiB, UMask0077, no restart loop. Installed script SHA256 `6b2c40e257bb567a7b88d761ed69a5b00174aa92895f2eabbdd58d674061bdf1` equals committed source. State directory `/var/lib/ai-gateway-keeper-observe` is 0700; `monitor.py`, `arm.json`, `BASELINE.json`, `samples.jsonl` are 0600. First sample and subsequent CPU/log samples were read back; no revert attempt/event at the checkpoint below. The local notifier was dropped before implementation; none was built or armed.

A `finished` event does not restore limits automatically. Memory/CPU/drift/error exits stop protection without changing the experimental profile. The coordinator must inspect the event and decide the next deployment; no new long-term safety claim is made.

Inspect (read-only):

```sh
ssh root@100.94.238.35 'systemctl status ai-gateway-keeper-observe-20261023.service --no-pager; tail -n 2 /var/lib/ai-gateway-keeper-observe/samples.jsonl; if test -f /var/lib/ai-gateway-keeper-observe/EVENT.json; then cat /var/lib/ai-gateway-keeper-observe/EVENT.json; fi'
```

Cancel only when no front transaction is in progress. This stops monitoring, **not** the 24h/10000/0 profile; it does not restore history or parameters:

```sh
ssh root@100.94.238.35 'test ! -d /root/AI-gateway/.ai-ops-state/front-component.lock && systemctl stop ai-gateway-keeper-observe-20261023.service'
```

After any automatic revert, first compare live hashes with the 0600 event receipt; then re-adopt (run on the controller), and independently compare the three source/destination hashes again:

```sh
rsync -a --chmod=F600,D700 root@100.94.238.35:/root/AI-gateway/{compose.yaml,compose.override.yaml,.env} /home/xz/Code/ai/AI-gateway/ansible/private-config/
```

Raise keeper/bridge caller timeouts and retain partial stdout on timeout as follow-up. Generic file-baseline advancement and durable success/failure receipts/finally cleanup also need a separate transaction improvement. Front profiles already have an `always` unlock, but killing the controller bypasses it; a normal-task `always` alone cannot guarantee recovery from controller death.

Source/policy pin `d43893e` exact-tip [Validate39912843418](https://github.com/xz-dev/AI-gateway/actions/runs/39912843418) passed both jobs before the approved plan. Final documentation-tip CI and lease handoff are recorded in the local Chinese report `/var/tmp/deploy-batch-20261023/REPORT.md`.

## Keeper — 1 GiB cap deployed (2026-10-24)

After the completed `24h / 10000 / 0` observation, the user approved a 1 GiB
live-payload cap and explicitly chose immediate deployment despite the cache
wipe. Only the ignored production `.env` key
`OPENAI_API_CACHE_KEEPER_STORE_MAX_BYTES` changed from `0` to `1073741824`.
Repository defaults remain `10m / 100 / 0`; access LRU, write-time TTL,
metadata/pin/compaction limits and the separate transient spool budget remain
unchanged. Source `65ef67c` parses a non-negative decimal int64 byte count, not a
size string. The cap does not bound total bbolt file size or filesystem usage.

The first apply stopped **before snapshot/upload/activation** at an async pull:
Tailscale SSH's su/PAM session emitted OSC 3008 terminal context on the same line
as the async wrapper's JSON. Ansible could not deserialize it. The unchanged
image pull finished successfully; no orphan remained. The owned lock was
released with a failed progress/failure receipt, all containers/configuration/
baselines stayed unchanged, and the local candidate `.env` was restored.
The old `front-openai-api-cache-keeper-last.json` still described the 10-23
rollout (rollback target `fe746f7`), **not this stopped attempt**; it was correctly
not used. No destructive rollback was needed or claimed.

Supervisor-approved fix `3f07208` disables SSH PTY allocation in repository
`ansible.cfg`, without changing host PAM/profiles or the shared transaction.
Both new regressions failed with the old config, then all 21 guards passed;
the forced-PTY negative control still rejects OSC-prefixed JSON. The dynamic
Ansible-plugin test runs when Ansible is installed; the config guard always
runs. Local full validation and exact-tip [Validate 40090830698](https://github.com/xz-dev/AI-gateway/actions/runs/40090830698)
passed. A harmless real async command and the full read-only keeper plan from
clean pushed `main` passed with no PTY/become errors. Only then was the separately
authorized **single retry** made, with fresh plan digest
`f574d3ba2f02f8dc685f8ac25dce1c385b2de9ad194a9e1a6b66b19b8406639c`.

- Apply **11:44:56–11:54:19 UTC**, **562.304s**, exit 0, `ok=129 changed=10 failed=0`.
  Keeper alone was recreated at **11:52:08.733810177Z**, from `735f50dc89b6` to
  `915a5ac8d638`; image ID/reference remain the exact `65ef67c` pin above.
  Readback confirms healthy, `24h / 10000 / 1073741824`, and only that environment
  value changed. All **54 project neighbours**, plus one non-project container,
  retain ID/image/StartedAt/state. The namespace owner remains `c6c96e158b02`.
- This retry actually exercised the full guarded production transaction:
  `locked → preparing-image → image-ready → uploading-config → activating →
  verifying → committing-baselines → complete`. Independent one-second reads
  observed the old `.env.baseline` throughout upload/activation/verification,
  its advancement only at **11:53:49Z**, success at **11:54:12Z**, and lock release
  at **11:54:18Z**. Earlier no-op applies are not counted as this evidence.
- Transaction `2311960fe5194c30a1fcb707f0facfc0`; snapshot
  `/root/AI-gateway/.ai-ops-state/front-openai-api-cache-keeper-qmwxbpuu` is 0700,
  its three original files and `receipt.json` are 0600. Snapshot hashes equal the
  pre-deploy files. The last pointer now selects this deployment; a later
  explicit guarded rollback requires a fresh plan/digest and would restore
  `24h / 10000 / 0` with the same image, not recover wiped aliases.
- Production/private-config/all three baselines agree: compose
  `212d6695a8a538b6eadcdb272a80231b6643f95078fb7d0edde0aa0d33019de4`, override
  `b3f46dcb7c8284845ecf247b1f225c4e7dff08684aa4b09d9e68822249ae6521`, `.env`
  `63594d28c97bf17ee9d435dd453ea1e8e29d4d80e2565181df7b5f90e0ca6d12`.
  The last pre-retry cache observation was 9477 aliases; startup discarded the
  old history, and natural traffic was already repopulating it at readback.
- **Four real calls, zero retries**, **11:55:11–11:55:35Z**: SSE marker/parent-delta
  recall `3.976s / 4.561s`; WS marker/new-socket parent-delta recall
  `2.304s / 2.604s`. All four returned `completed`, both exact marker recalls
  passed. Health/HTTP 101 were not treated as continuation acceptance.

Private logs, before/after reads, observer trace, prepared rollback instructions
and Chinese report: `/var/tmp/keeper-cap-deploy-20261024/` (0700, files 0600).
No production rollback drill, new load/CPU acceptance, VM reboot, Sub2API change
or egress-proxy rebuild occurred. The successful cap rollout does not close the
remaining controlled resource/throughput or Operator acceptance paths.

## Keeper — background terminal forwarding rollout (2026-10-24)

The user approved immediate deployment of source
`ade3d346de4c342d24996c058f3542d9a2bd3e1f` and accepted another cache wipe.
Pinned image:
`ghcr.io/xz-dev/openai-api-cache-keeper:ade3d346de4c342d24996c058f3542d9a2bd3e1f@sha256:b8e12e6d8a5caf74a17b4615754655df3f409c7ad5721bae40eb3ac768964c0f`.
Exact-source [CI 40100189550](https://github.com/xz-dev/openai-api-cache-keeper/actions/runs/40100189550)
passed fmt/vet/race and published the image; artifact publication alone does not
change production. Compose, the example image override and the keeper submodule
now select the same source revision.

The fix forwards valid upstream `response.failed`, `response.incomplete` and
`error` terminal events to active background subscribers, preserving nullable
error codes, empty messages and unknown fields. Failed tasks still cannot
produce a successful checkpoint/cache entry; a later GET remains explicitly
unavailable. It does not add an upstream generation, account/owner isolation,
model selection or any bridge change. Ordinary parent expansion, access LRU and
write-time TTL remain unchanged.

This is an **image-only** production rollout: preserve the complete environment,
including `24h / 10000 / 1073741824`, and existing 0.5 CPU / 384 MiB / no swap /
64 PID bounds. Repository defaults remain `10m / 100 / 0`. Before applying,
validate and push this pin with exact-tip green CI, then use a fresh guarded
keeper plan/digest from that clean `main`. Prepare rollback to the same
pre-deploy `65ef67c` image **with the 1 GiB cap**, using this operation's private
snapshot, not an earlier last-deploy pointer. Require image/revision/env/resource
readback, unchanged neighbours, successful receipt and delayed baseline commit,
released lock and production/private-config equality. Limit real smoke to four
calls, zero retries, covering SSE parent/delta and WS recall on a new socket.

This source-pin record does not itself attest runtime success. The per-stage
transaction, final identities, timings, smoke results and rollback preparation
are recorded privately in `/var/tmp/keeper-ade3-deploy-20261024/REPORT.md`
(0700 directory, 0600 files). Recreating keeper clears all cached aliases; no
production rollback drill or new CPU/load acceptance is authorized by this
rollout. The earlier 26.5-hour natural-traffic observation applies to the old
pre-cap container, not this newly recreated process.

## Limits

No second VM boot, real rollback drill, formal 0.5-core CPU acceptance, long soak, or standards certification. Messages remains disabled; Sub2API, account modes, slash routing and keeper LRU are unchanged. Keeper startup still clears aliases; later long TTL will not survive a process restart. The production observation must have stop thresholds and an explicit owner before being armed.

## Value versus cost — preliminary checkpoint

| Metric | Old baseline | New bounded observation checkpoint |
|---|---|---|
| Profile | 10m TTL / entries unlimited / 200MiB cap | 24h TTL / 10000 aliases / bytes0 |
| Measured period | 03:33:00–06:45:14Z retained logs | 07:05:35–07:10:41Z, 6 samples only |
| Completion logs | 1829 | 5 |
| Hit proxy / parent attempts | 954/964 = 98.96% | 5/5 = 100.00% |
| Excluding 04:47–04:59Z outlier | 932/940 = 99.15% | No overlap; same 5/5 |
| Unknown-parent miss proxy | 6 | 0 |
| Full-history/new-turn proxy | 865 | 0 |
| stored true / false | 1719 / 110 | 5 / 0 |
| Last logged aliases / store_bytes | 76 / 6224181 | 110 / 5619771 |
| Actual client bytes avoided | Not measured; log has no byte counts | Not measured; do not substitute store_bytes |
| Cache directory bytes/files | 32.27MiB / 5 (end only) | 30.90MiB / 4 (window max) |
| Free root/docker filesystem | 6.972GiB (end) | 6.974GiB (window min) |
| RSS | 11.57MiB (end; not historical peak) | 13.21MiB (window max) |
| Working set | 8.73MiB (end) | 14.58MiB (window max) |
| CPU cores | 0.03820 lifetime mean since 03:33:44 | 0.00812 mean / 0.01543 max of 60s windows |

Hit ratio above is the requested `previous=true status200 / all previous=true completions` proxy, not a verified cache lookup or model-success ratio. Outlier interval contains 40 completion logs (24 parent attempts, 22 status200, 2 status400; stored true/false 12/28); that is **not** a claim that all 40 were 429. It includes concurrent traffic in the known upstream-429 period. Old cost is one end checkpoint, not old peaks; old CPU is cgroup lifetime CPU/uptime. New CPU uses actual sampled deltas, not formal 0.5-core throughput/latency acceptance. Traffic volume and durations differ greatly; these are not matched A/B workloads.

The old proxy already exceeds 99% outside the outlier. Five new parent completions cannot show a durable improvement. Neither a larger TTL nor a 10000-alias cap is proved optimal; the old production alias count was unlimited, so this is not a production 100→10000 comparison. The reasonable decision is a bounded experiment with a free-disk exit, not “bigger is always better.” Wait for the full window and request byte telemetry separately before quantifying saved client traffic; no keeper-binary instrumentation was added here.

## Six historical baseline debts — intentionally not adopted

Only this batch’s compose/.env baselines were repaired. No runtime/configuration content changed during that repair. Historical snapshot/bridge pointer hashes are not rewritten to pretend they describe the later keeper deployment.

| Live path | Recorded baseline SHA256 | Current live SHA256 | private-config / likely origin |
|---|---|---|---|
| `apisix-models/config.yaml` | `f718077f27ceb23180b8da6f1f27a664087451e8b31a7d057747a6cfbc9a6c64` | `b9c26a41fa43a77164c612fcfa7738af6244a460864eea1906c991681c8cc172` | Not covered by private-config (public tracked input). Tracked source equals live; 4f851db (2026-10-22 routing_lkg cleanup). |
| `apisix-models/apisix.yaml` | `9ada163cdafa8cf7c08096702817922387406e67e6e107317f3768f1758d036d` | `90a1d0398a388800ad3f940a7903c7f02495984501471f1f506fe56cc1dacc6c` | Not covered by private-config (public tracked input). Tracked source equals live; b112be3 (2026-10-22 front-cache routing). |
| `apisix-models/routing_selector.lua` | `e5fe09bbba231d90406a197e587269fb9bd2b6edacf392647d815f3c2737f1a5` | `5e727da9e991201509c52d46324c8422f1a4868fdc2371512aa8cad3e90504d9` | Not covered by private-config (public tracked input). Tracked source equals live; 63b0717 (2026-10-22 slash routing). |
| `aisix/config.yaml` | `2acd69a7fdbe58acd5b4a5d2796f9085e0a7ef4aa70c8e63bdf8869837afdc0b` | `943fb9851460dbf471567f0b470d0af768df21defbcf441eda295784dbb20f8c` | Does not equal live. Origin not established in this deployment; separate review required. |
| `aisix/resources.yaml` | `855a9721394013ce0954d6768c65856970947648ec47d103e7b085c81f67a7d7` | `9fc3f6c84b230174c43cc307a2c51b64f0540b748baf313247ca31c3f4761c84` | Does not equal live. Origin not established in this deployment; separate review required. |
| `data/cpa/conf/config.yaml` | `ee897302abc7c30e8f8ef035aa9d754082dd954af287118894861e1a78f4665c` | `f366c2127d71db064162e5dd69e5a846e686c21259300385a4690ebb9e0df359` | Does not equal live. Origin not established in this deployment; separate review required. |
