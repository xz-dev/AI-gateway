# Postgres and CPA memory raise — 2026-11-15

## Delivered

After the [move to the 4 GiB Alpine LXC](alpine-lxc-migration-2026-11-15.md),
the operator approved raising memory only for the two services with evidence:

| Service | Before | After |
|---|---|---|
| postgres | `mem_limit` 400 MiB, swap default, `shared_buffers=64MB` | 1 GiB, swap default, `shared_buffers=256MB` |
| cli-proxy-api | 768 MiB RAM, 1 GiB RAM+swap, `GOMEMLIMIT=384MiB` | 1 GiB RAM, 2 GiB RAM+swap, `GOMEMLIMIT=700MiB` |

The CPA values equal the tracked template defaults. The values are set in the
private `compose.override.yaml`. The live private `compose.yaml` still carries the older
480 MiB / 384 MiB CPA base, so removing the override lines would lower CPA rather
than restore the template. `max_connections=50` is unchanged.

Not changed on purpose: AISIX's 512 MiB/no-swap is a spec requirement, the
keeper's 384/384 and the bridge's 256/256 are validator-asserted, and Sub2API's
512 MiB + 320 MiB swap is the documented production value. Every other service
peaked far below its limit.

## Evidence (about 1.5 h on the new host, Sunday traffic)

- postgres reached its limit; its cgroup counted 3,717 `memory.max` reclaim events.
  Anonymous memory was 43 MiB and file cache 259 MiB, for a 4.8 GB database
  (mostly usage/ops log tables). The buffer hit ratio was 96.9%.
- CPA had no events on the new host; its peak was 158 MiB. On the 2 GiB host it
  had run 525 MiB anonymous RSS plus 242 MiB swapped, and its GC target was lowered
  from 512 MiB to 384 MiB to fit the 768 MiB limit
  ([memory pressure record](cpa-memory-pressure-2026-11-10.md)).
- redis also counted reclaim events at 80 MiB, but its data was 5 MiB; the rest was
  AOF page cache. It was left unchanged.

## Window (UTC)

| Time | Event |
|---|---|
| 10:56:07 | Baseline 48 Up, 0 unhealthy, recovery checks 8/8; established TCP 3/2/2 |
| 10:56:27 | Recovery loop stopped; override written in place |
| 10:56:27–10:56:45 | postgres recreated; gate passed 18 s after the recreate began |
| 10:57:18–10:59:00 | CPA recreated; recovery probes failed until 10:59:00 (133 s step) |
| 10:59:03 | Recovery loop restarted; first applying cycle 11:01:05, all ok |

Each step used `--force-recreate --no-deps --no-build --pull never --timeout 60`.
A dry run had shown that each touched only its own container. The public route
was not separately measured. CPA-dependent paths can have failed during the
CPA step, until its health probe recovered.

## Checks

- Old and new renders, made with `env -i` and compared as parsed JSON, differed
  only in the five approved fields.
- postgres reported `shared_buffers=256MB`, with `memory.max` at 1 GiB. A Sub2API
  admin login and user listing ran through the database path after its restart.
- CPA's environment had `GOMEMLIMIT=700MiB`; its cgroup had `memory.max` 1 GiB and
  `memory.swap.max` 1 GiB.
- The other 46 containers kept identical IDs. After the window, a dry run planned
  0 recreates.
- Both services had 0 restarts and 0 `max`/OOM/OOM-kill events afterwards. The LXC used
  about 2.0 GiB.
- The controller and live override have equal SHA-256 hashes.

## Rollback

The old override and `.env` are backed up in a mode-0700 directory under the
deployment's `.ai-ops-state/`, as `mem-limits-20261115T*`. A protected copy is
on the controller. To roll back, write the backup over the override in place,
then recreate `postgres` and `cli-proxy-api` with the same flags. Roll back for
OOMs, unhealthy services or change-caused inference failures, not for higher but
bounded memory use.
