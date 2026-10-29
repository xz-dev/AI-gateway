# AISIX stream timeout 60 s → 120 s — 2026-10-29

## Cause

The private production AISIX `config.yaml` set `upstream.stream_timeout_ms: 60000`. The tracked `config.example.yaml` did not have this line, so the value was private drift. The value is the longest silence allowed between two upstream stream chunks. Long Claude/Codex generations have silent stretches longer than 60 s, for example a 378k-token `claude-opus-5-5` request that received its first byte at 3.1 s. AISIX cut them with `504 upstream request timed out after 60000ms` (`error_kind="timeout"`, `routing_attempt_count=1`).

Evidence, from the AISIX access log before the fix:

- 36 timeouts in the 3 h before the fix;
- 70+ over 72 h, mostly `claude-opus-5-5` → `xl/claude-opus-5-5`, a few `gpt-6.1-sol`, `axis/gpt-6.1-sol` and `codex/gpt-6.1-sol`.

## Change

- User decision: 120 s.
- Tracked `services/routing/aisix/config.example.yaml` now carries `upstream.stream_timeout_ms: 120000`, so tracked and private agree. The AISIX README documents the value and the manual procedure.
- No playbook manages AISIX `config.yaml`; `deploy-config.yml -e component=aisix` deploys only `resources.yaml`. The change therefore used a manual window that mirrors the managed-file activation:
  1. Back up the host file to `/var/tmp/aisix-config.yaml.20261029T030905Z`.
  2. Change that one line (`diff` showed only line 23). Mode 444 and owner were kept.
  3. Apply the same edit to the controller `private-config` copy. The new SHA-256 `1d283538…` is identical on both sides.
  4. Run `docker compose -p ai-gateway -f compose.yaml -f compose.override.yaml --env-file .env up -d --no-deps --force-recreate aisix` at 03:09:30 UTC.

## Verification

- AISIX was recreated and running at 03:09:55 UTC, with restarts 0 and no OOM. In-container `/etc/aisix/config.yaml` shows `stream_timeout_ms: 120000`. The log shows `resources loaded … resources=79` and all listeners up. Of 52 running containers, only the AISIX container ID changed.
- An SSE probe went straight to AISIX through the Sub2API relay, using the existing Sub2API caller key. No key was created. The scratch header file was removed, and its directory was confirmed absent afterwards.
  - Responses `zcode/glm-5.3-flash` completed with the full event sequence through `response.completed`.
  - Chat streamed 131 `data:` lines.
- Live traffic from 03:09:55 to 03:20 UTC:
  - 209 `200` log lines and 0 `timed out after` lines.
  - 5 `502/503` responses, which were upstream status errors from `xl/claude-opus-5-5`, not timeouts.
- The `deploy-config.yml -e component=aisix --check --diff` preview afterwards showed changed=0.

## Rollback

On the host, restore the backup and recreate AISIX:

```bash
cd /root/AI-gateway
cp -a /var/tmp/aisix-config.yaml.20261029T030905Z services/routing/aisix/config.yaml
docker compose -p ai-gateway -f compose.yaml -f compose.override.yaml --env-file .env up -d --no-deps --force-recreate aisix
```

Then revert the controller `private-config` copy and the tracked example to match.

## Residual risk

- A real silence between chunks of more than 120 s still ends with 504.
- Other hops may have their own idle limits, for example Sub2API, keeper or the client. Those limits are not changed here.
