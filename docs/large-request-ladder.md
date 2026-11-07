# Large-request capacity ladder

CI harness for the large-request capacity work (OpenSpec `large-request-capacity`, milestone 3 task 8.2). Lives in
`ci/large-request/`, checked out whole: `ci/ladder.sh` next to its support files (`hlib.py`, `rungs.py`,
`manifest.py`, `gen*.py`, `s2a_setup.py`, `cfg/`, `mock/`, `wsclient/`).

## What it checks

`ci/large-request/ci/ladder.sh` starts a throwaway chain (`mock -> keeper -> bridge -> Sub2API -> AISIX -> CPA`,
production `mem_limit`s, no swap, no egress) from the images named in `ci/large-request/ci/compatibility-lock.json`,
then runs a ladder of HTTP and WebSocket rungs at 1, 16, 64 and 256 MiB (`.github/workflows/large-request-ladder.yml`
runs this on every pull request and on `workflow_dispatch`). A rung passes when:

- the mock received the body unchanged (whole-body hash, or item-by-item for hops that add fields);
- no hop's memory exceeded `memory.max − 32 MiB` (`ci/verdict.py`); keeper is the documented D3 exception — its
  `memory.max` is routinely reached by reclaimable `bbolt`-store page cache, so it is margin-checked on
  `anon + file_dirty + file_writeback + kernel` (`hlib.py`'s `keeper_criterion`), not raw `memory.peak`;
  no container hit `oom_kill`;
- the capacity ticket was released and no spool or ticket file remains;
- no container restarted.

A nightly scheduled run (`cron: 17 2 * * *`, or `workflow_dispatch` with `nightly: true`) adds the 1 GiB rungs on
the same throwaway stack only — the production request cap stays 256 MiB. That step is `continue-on-error: true`
until the nightly stack (M3 step 3) lands; it needs ≥ 12 GiB free memory and disk, which standard GitHub runners
do not have yet.

## How the lock gates a deploy

`ci/large-request/ci/compatibility-lock.json` (schema: `ci/large-request/ci/compatibility-lock.schema.json`) names
every image the ladder ran by digest (`name@sha256:...`, never a tag), plus the config hashes, memory peaks and the
`k_disk` / `k_mem` / `k_skel` footprint factors design D4 uses. `ci/lock_from_manifest.py` writes it from a finished
run's `manifest.json` and `summary.json`, and refuses to write anything unless that run's verdict is `pass`. A
production deploy only pulls image digests that appear in a lock generated this way.

## Refreshing the lock

```bash
cd ci/large-request
# point LOCK at a candidate lock naming the images to test (keeper, bridge, sub2api, aisix, cpa,
# plus the pinned postgres/redis/alpine digests already in compatibility-lock.json)
LOCK=/path/to/candidate-lock.json COMPOSE=podman-compose ./ci/ladder.sh
run=$(ls -d logs/ci/*/ | tail -1)
python3 ci/lock_from_manifest.py "$run" ci/compatibility-lock.json
```

The CI workflow runs the same two commands on every pull request and uploads the result as the
`compatibility-lock-candidate-<run id>` artifact; it does not overwrite the tracked lock. Promoting a candidate
lock to the tracked `ci/compatibility-lock.json` is a reviewed change, not an automatic CI step.
