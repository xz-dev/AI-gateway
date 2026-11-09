# Large-request capacity ladder

CI harness for the large-request capacity work (OpenSpec `large-request-capacity`, milestone 3 task 8.2). Lives in
`ci/large-request/`, checked out whole: `ci/ladder.sh` next to its support files (`hlib.py`, `rungs.py`,
`manifest.py`, `gen*.py`, `s2a_setup.py`, `cfg/`, `mock/`, `wsclient/`).

## What it checks

`ci/large-request/ci/ladder.sh` keeps the original PR gate: `keeper -> bridge -> Sub2API -> AISIX -> CPA -> mock`,
using `ci/compatibility-lock.json`, production per-hop memory limits and no swap. It runs HTTP and WebSocket
rungs at 1, 16, 64 and 256 MiB on every pull request and non-nightly `workflow_dispatch`.
`NIGHTLY=1` alone merges `ci/compose.full-stack.yaml`, uses `ci/nightly-compatibility-lock.json`, and adds the
three missing hops: `firewall -> keeper -> bridge -> Sub2API -> apisix-models -> AISIX -> CPA -> Squid -> mock`.
It runs the same ladder plus both 1 GiB rungs. A rung passes when:

- the mock received the body unchanged (whole-body hash, or item-by-item for hops that add fields);
- no hop's memory exceeded `memory.max − 32 MiB` (`ci/verdict.py`); keeper is the documented D3 exception — its
  `memory.max` is routinely reached by reclaimable `bbolt`-store page cache, so it is margin-checked on
  `anon + file_dirty + file_writeback + kernel` (`hlib.py`'s `keeper_criterion`), not raw `memory.peak`;
  AISIX uses the same working-set criterion as keeper under decision (a): hosted 1 GiB evidence shows anon 26.3 MiB and reclaimable file cache 482.9 MiB.
  no container hit `oom_kill`;
- the capacity ticket was released and no spool or ticket file remains;
- no container restarted.

A nightly scheduled run (`cron: 17 2 * * *`, or `workflow_dispatch` with `nightly: true`) adds the 1 GiB rungs on
the extended throwaway stack only — the production request cap stays 256 MiB. That step is `continue-on-error: true`
while hosted-runner capacity is being established. Admission prechecks require MemAvailable ≥ 1216 MiB and
ledger-filesystem free space ≥ 8 GiB (the 1 GiB reserve plus 7 × request-size disk budget). Before each container
group, the script records `free -m` and `uptime`, and waits up to 30 minutes for MemAvailable ≥ 4 GiB and
1-minute load ≤ 12. A failure preserves the original assertions and exact precheck numbers; it is not a pass.
Ledger, spool, run output and generated bodies live on real disk under `/var/tmp`, never `/tmp`; generated
bodies are removed between rungs. Production keeper/bridge/Sub2API/AISIX/CPA caps stay at 256 MiB.
The nightly changes all their caps together to 1073741824 bytes and both APISIX body limits to `1024m`, with
request buffering off. The PR gate does not start the APISIX/Squid extension. No production configuration is modified.

## How the lock gates a deploy

The PR `ci/large-request/ci/compatibility-lock.json` and nightly `ci/nightly-compatibility-lock.json` (schema: `ci/large-request/ci/compatibility-lock.schema.json`) name
every image their respective ladder ran by digest (`name@sha256:...`, never a tag), plus the config hashes, memory peaks and the
`k_disk` / `k_mem` / `k_skel` footprint factors design D4 uses. `ci/lock_from_manifest.py` writes it from a finished
run's `manifest.json` and `summary.json`, and refuses to write anything unless that run's verdict is `pass`. A
production deploy only pulls image digests that appear in a lock generated this way.

## Refreshing the lock

```bash
cd ci/large-request
# point LOCK at a candidate lock naming the images to test (keeper, bridge, sub2api, aisix, cpa,
# plus postgres/redis/alpine; NIGHTLY_LOCK names the extended nightly lock)
LOCK=/path/to/candidate-lock.json COMPOSE=podman-compose ./ci/ladder.sh
run=$(ls -d /var/tmp/ai-gateway-ladder/logs/ci/*/ | tail -1)
python3 ci/lock_from_manifest.py "$run" ci/compatibility-lock.json
```

The CI workflow runs the same two commands on every pull request and uploads the result as the
`compatibility-lock-candidate-<run id>` artifact; it does not overwrite the tracked lock. Promoting a candidate
lock to the tracked `ci/compatibility-lock.json` is a reviewed change, not an automatic CI step.

## Running the nightly rung

```bash
cd ci/large-request
NIGHTLY=1 COMPOSE=podman-compose ./ci/ladder.sh http-image-1g ws-image-1g
# Omitting rung arguments runs the usual ladder followed by both 1 GiB rungs.
gh workflow run large-request-ladder.yml --repo xz-dev/AI-gateway \
  --ref feat/large-request-capacity-ci -f nightly=true
```

The workflow uploads summaries, JUnit, per-rung memory samples, manifests and container logs even on failure.
The five component digests are unchanged; nightly adds APISIX and Squid without redefining the PR gate. APISIX uses the production 3.19.0-debian image digest;
Squid is built by `publish-capacity-squid.yml` from the **unchanged production Dockerfile and build context**.
The publish job records its digest; the ladder only pulls that digest from the compatibility lock, never builds
or follows a mutable tag. A new Squid build does not automatically update the lock.

## Differences from the full-chain harness

- Squid is a plain HTTP forward proxy in front of the mock, **without TLS bump**. This measures request-body
  streaming and the unlimited Squid body cap, not TLS, certificate trust, bumped-provider policy or DNS behavior.
  Its published production image runs Squid directly, without the unneeded unbound/CA entrypoint machinery.
- The production Squid image is now published to GHCR and digest-pinned for CI; production builds it locally.
- One throwaway Compose network replaces production relays and network boundaries. Production APISIX routes
  and Lua stay in use, but listeners/DNS/trusted-client addresses are rendered for CI, the front upstream points
  directly to keeper, and unused catalog upstreams point to the mock. Catalog behavior is not tested.
- The separate keepalive hop and production-RAM parent cgroup are not reproduced. Per-hop production memory
  limits and no-swap settings remain enforced, including `oom_kill=0`, the 32 MiB margin (D3 keeper working-set
  exception), ticket release and Sub2API `off_list=0`. This is a per-hop capacity check, not a claim that the whole
  hosted runner has production's RAM size or production's egress isolation.

## M3 local verification

Initial extended-stack smoke `20261109T093222Z-1856775` passed both 1 GiB rungs (39.757 s HTTP / 86.7 s WS;
keeper D3 criterion 45 MiB, `off_list=0`, OOM/restarts zero), before the CPU-quota correction below.

### AISIX worker-count correction

The first full-stack 256 MiB regression attempt failed `http-tiny-16m`: AISIX reached 512 MiB (484 MiB anon)
against the unchanged 480 MiB margin bound, without an OOM kill. Inspection found the nightly compose omitted
production's `cpus: 1.0`; AISIX's thread-per-core mode consequently started 20 workers on this host rather than
one. Its `worker_threads()` uses `available_parallelism()`, including the cgroup CPU quota. A subsequent
five-hop-only regression reproduced the same 512 MiB tiny-rung peak, proving that the extra
hops were not required to trigger it. The cause was missing production CPU quotas, not APISIX body buffering;
the oversized thread-per-core worker pool can retain multiple allocation-heavy small/tiny-request heaps.
Both stacks now match production CPU quotas for every constrained service, including AISIX 1.0,
Sub2API 1.5, CPA 1.75, keeper/bridge 0.5, PostgreSQL 1.0, Redis 0.5; nightly firewall/models 1.0 and Squid 0.5.
AISIX's memory limit remains 512 MiB and all assertions remain unchanged. The unit test compares these
quotas to the production Compose template. The PR stack retains its original five-hop topology and pinned images.

Isolating the AISIX quota correction in run `20261109T094940Z-2094385` gave 12/12 passes: readback showed
`cpu.max=100000 100000` and only worker 0, and its tiny-rung peak fell from 512 to 445 MiB (414 MiB anon).

With **all** CPU quotas aligned, PR run `20261109T100118Z-2386210` passed 10/10; the full nightly run
`20261109T100328Z-2484208` passed **12/12**. Final 1 GiB HTTP/WS timings were 50.729 s / 105.0 s; peak MiB
maxima across those two rungs were keeper 384 (D3 criterion 61), bridge 20, Sub2API 59, AISIX 163, CPA 96,
firewall 60, apisix-models 67, Squid 24. Keeper's D3 criterion leaves 323 MiB headroom; the smallest other-hop
headroom is apisix-models at 61 MiB, both exceeding the unchanged 32 MiB margin. AISIX tiny-rung peak was
440 MiB (72 MiB headroom). All OOM/restart/off-list/release assertions passed and no spool/ticket residue
remained. Generated bodies were removed between rungs. The separate nightly lock was generated and
schema-validated from this final green 12-rung run; the original PR image lock remains unchanged.
