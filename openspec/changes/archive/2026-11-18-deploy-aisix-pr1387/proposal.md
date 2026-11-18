> Historical plan, superseded for desired runtime on 2026-11-09 by the
> [final deployment/cleanup record](../../../docs/records/maintenance-capacity-closeout-2026-11-09.md).
> Preserve the original acceptance history. This plan is not an instruction to
> redeploy its old candidate or rebuild an existing immutable AISIX tag.

## Why

The running AISIX gateway does not contain the request-body allocation fix in upstream PR #1387. Its Responses source blob is exactly the old side of that patch. The user requested this fix be deployed first, then the already-authorized Sub2API maintenance resumed.

## What Changes

- Publish an owned AISIX runtime image containing PR head `06e8075266119380844aa4e9fcb9fb80468715eb`. Gateway publisher changes are packaging/CI only; fetched application, dependencies, build scripts and runtime configuration remain equal to that head.
- Change only the private production `AISIX_IMAGE` pin to the accepted immutable image digest through the existing guarded component rollout.
- Preserve the old image/configuration, verify runtime readiness and unchanged neighbouring services, and retain a sanitized rollout receipt.
- Resume `trim-sub2api-patches-and-upgrade` after AISIX acceptance. Do not turn this deployment into another HTTP/2 root-cause investigation.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. This is an allocation/timing implementation fix and an image delivery operation. API bytes, configuration contracts, routes, account/pool settings and resource limits are unchanged; `skip_specs: true` is intentional.

## Impact

- Owned `xz-dev/aisix` image publication workflow and candidate image.
- Private AISIX image pin and the AISIX application container only.
- No CPA, keeper, bridge, PostgreSQL, Redis, account/group, retry/cooldown or memory-limit changes; no AISIX database migration.
