# APISIX local healthcheck and orphan-container cleanup — 2026-10-30

## Outcome

Completed both explicitly approved actions without rollback during **01:20:24–01:26:48 UTC**:

- Removed only the stopped legacy `response-keeper` container, retaining its named volume and database unchanged.
- Aligned the private/live APISIX healthcheck with tracked Compose: local `/healthz`, expecting **404**, instead of unauthenticated `/v1/models`. The full healthcheck block now equals the tracked block, including its 15-second interval, 5-second timeout, three retries and 20-second start period.

Only APISIX was recreated. All **48** other retained containers, including the current cache keeper and public relay, kept identical IDs, start times, health/status and restart counts. The total container count is now **49**, one fewer because of the approved orphan removal. No image, resource limit, route, namespace, network, application configuration or database changed.

## Orphan removal and data preservation

Immediately before removal, `ai-gateway-response-keeper-1` was exited with code 0, had no networks, and was absent from the effective Compose service list. Its only named volume was `ai-gateway_response-keeper-cache`.

| Proof | Result |
|---|---|
| Removal command | `docker rm ai-gateway-response-keeper-1` — no force or volume flag |
| Removed at | **01:20:59 UTC** |
| Container afterwards | Absent |
| Named volume afterwards | Still present |
| Regular-file bytes before / immediately after / final check | **114,041,579 / 114,041,579 / 114,041,579** |
| Active cache keeper | Container identity and state unchanged |

The retained volume contains a database. No volume, file or database was opened for content inspection or deleted. Container removal does **not** reclaim the volume's approximately 109 MiB; data retention/deletion remains a separate decision.

## Compose preconditions and narrow edit

Controller-private and live `compose.yaml`, `compose.override.yaml` and `.env` matched byte-for-byte before the window. The initial APISIX component preview returned **`ok=63 changed=0 failed=0`**, with `config_diff=false`.

Both private/live Compose copies were backed up. The controller copy remains in private scratch under `/var/tmp/ai-healthcheck-window/`. The host copy and hash manifest are mode 0600 within the mode-0700 directory `.ai-ops-state/apisix-healthcheck-20261030T012102Z/`.

The prepared file was checked against the backup: its only byte change replaces the healthcheck request path `/v1/models` with `/healthz`. Both destination files were rewritten **in place**, preserving their inode and permissions. The complete resulting `services.apisix.healthcheck` value was compared with tracked Compose, and controller/live checksum equality was verified again after activation. Host `docker compose config -q` succeeded before recreation.

This resolves original private-versus-tracked drift path **#17**, `services.apisix.healthcheck.test`. Other private overrides and the undeployed operator lane were not reconciled.

## Recreate evidence

Established front-listener connection samples at **01:21:14 / 01:21:24 / 01:21:34 UTC** were **1 / 1 / 1**. At that low count, the existing deployment's secret-free `env -i PATH=... docker compose` arguments recreated only `apisix` with `up -d --force-recreate --no-deps --no-build --pull never apisix`, bounded by 90 seconds.

| Evidence | Value |
|---|---|
| Recreate invocation | **01:21:38.216 UTC** |
| Old APISIX ID | `05c819b496ccd7eb5918761f4f32bd6953e3b7b8bc88587199d3d9f339094d01` |
| New APISIX ID | `2913cb1e9c06aa7080213c88ba5a32c34211461f51913f2a571d4ca73be538b9` |
| New StartedAt | **01:21:43.866 UTC** |
| Healthy confirmed | **01:21:50.187 UTC** |
| Recreate-to-healthy | Approximately **12.0 seconds** |
| New health/restarts | Healthy / **0** |
| Actual public listener outage | **Unmeasured**; not inferred from lifecycle timings |

Recreating the public front door interrupts in-flight connections. No additional recreation was performed to measure listener downtime.

## Public and healthcheck verification

A temporary normal-client key exercised the public hostname before and after activation. No existing client key was changed.

| Check | Before | After |
|---|---|---|
| `/v1/models` and `/models` | 200, **343** entries each | 200, **343** each; bytes and ID order identical |
| Codex manifest | 200, **334** entries | 200, **334**; bytes/order identical |
| Private cache policy and identity-encoding ETag | Captured | `private, no-cache`, `Vary: Authorization`, strong final-body ETag preserved |
| Conditional requests, both plain routes | Not required | **304 / 412**, empty bodies |
| Unauthenticated plain and parameterized requests | Opaque 404 | Opaque **404**, empty body |
| Periodic `/v1/models HTTP/1.0` access requests | **20 / 300 seconds** | **0 / 309.5 seconds** since recreate |
| Local `/healthz HTTP/1.0` access requests | Not the old probe | **20 / 309.5 seconds** |
| Status-conflict errors / other Lua or route-load errors | Prior auth fix active | **0 / 0** during observation |

Request counts use access-log entries only, excluding the separate serverless-plugin WARN lines that also repeated the old request text. The approximately four-per-minute unauthenticated client described in the earlier [model-list window record](sort-public-models-list-2026-10-29.md) was this private Docker healthcheck. Switching the probe removes its unnecessary Sub2API requests and upstream/auth dependency from APISIX's own liveness signal. The local 404 is intentional, not a failed probe.

Temporary key **47** was deleted at **01:22:02 UTC**: DELETE **200**, follow-up GET **404**, then the host-local key file was removed and absence verified. No key material was copied to the controller.

Final component previews, from `ops/ansible/`:

- `deploy.yml -e component=apisix --check --diff`: **`ok=63 changed=0 failed=0`**, `config_diff=false`.
- `deploy.yml -e component=aisix --check --diff`: **`ok=63 changed=0 failed=0`**, `config_diff=false`.

Private captures, timeline, checksum/parity proofs and container snapshots remain under `/var/tmp/ai-healthcheck-window/` on host/controller. No credentials, addresses, public hostname or private configuration contents are included here.

## Rollback (not exercised)

Restore both private/live Compose backups by **in-place rewrite**, verify their pre-window hashes, run the deployment's `docker compose config -q`, then repeat the same APISIX-only recreation:

```sh
timeout 90 env -i PATH="$REMOTE_PATH" docker compose -p ai-gateway \
  -f "$DEPLOY_ROOT/compose.yaml" -f "$DEPLOY_ROOT/compose.override.yaml" \
  --env-file "$DEPLOY_ROOT/.env" \
  up -d --force-recreate --no-deps --no-build --pull never apisix
```

Verify healthy/restarts, public lists, manifest and opaque 404 again. This would reinstate the upstream-dependent healthcheck and interrupt public connections again. The deleted stopped container need not be restored for service continuity; its volume/database is retained regardless.
