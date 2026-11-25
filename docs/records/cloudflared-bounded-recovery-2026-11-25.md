# Bounded Cloudflare connector recovery — 2026-11-25

## Scope and policy

The operator selected three consecutive HTTP 503 responses from the same running `cloudflared` connector's loopback `/ready` endpoint. Responses other than 503 and unavailable probes break the streak. The guard reuses the 120-second startup grace, ten-minute cooldown, three-attempt hourly ceiling, fresh ownership and instance checks, and one restart per cycle. It does not start stopped containers, restart dependencies, or change the tunnel protocol.

## Production evidence (UTC)

- The baseline `recovery.yml --check --diff` had no drift. The candidate preview had exactly one changed file, `ops/scripts/host/gateway-recovery.py`. The production copy of the previous script matched the Git baseline and was retained in protected controller scratch. The playbook also created a backup on the host.
- The recovery playbook applied at **14:16:25–14:16:44** with `changed=1`, `failed=0`. The new script's SHA-256 matched the controller copy (`db99f95072d89a5b73bb7d612b541af32cccf42bc946afd05ad282cf269ca682`). The playbook verified the OpenRC guard and its cgroup limits.
- All **48** project containers had unchanged IDs, start times and statuses. The connector returned HTTP 200 from `/ready` and reported four active edge connections. Compose, the image and transport settings were not changed.
- The **14:16:53** and **14:17:25** scheduled apply cycles reported `autoRecovery=true`, HTTP 200, `failures=0`, `action=observe`. No production failure was injected. Availability during the deployment was not measured externally; unchanged start times establish that the deployment itself did not restart containers.

## Deferred-cycle correction (UTC)

Independent review reproduced an unintended restart for `503, 503, 200, 503` when another application consumed the restart slot on the 200 cycle. The corrected guard clears the connector streak before sending a restart for another application; that cycle's connector probe is observation-only. The regression failed on the first deployed version and passed after the correction.

The playbook applied the single-script correction at **14:43:28–14:43:49**, with `changed=1`, `failed=0`. The deployed SHA-256 was `5e572f3ea4046aec5448f12645361b7b3af5eedff1e20e0e45c27cccf409e472`. All 48 containers retained their identities, start times and statuses, `/ready` returned 200 with four active edge connections, and the next seven recorded scheduled apply cycles returned HTTP 200 with zero failures and `autoRecovery=true`.

## Offline verification after deployment

The recovery suite (25 tests), component deployment suite (28 tests), `git diff --check`, and strict OpenSpec validation (23 items) passed. The mocked Docker case confirms a restart only on the third consecutive 503, resets on 200, 502 and an unavailable probe, and verifies that observe-only mode, cooldown, pause or absence do not cause an extra restart.

## Rollback

Preview and apply the prior reviewed guard artifact with `ops/ansible/playbooks/recovery.yml` against the production inventory. Confirm that `cloudflared` returns to observation-only and that readiness and container identities are unchanged. Retain `/var/lib/ai-gateway-recovery/state.json`; do not clear restart budgets.
