# Tailnet front-door binding — 2026-10-30

## Outcome

Completed the approved private-configuration migration without rollback during **03:15:04–03:28:59 UTC**. The front door is still available on host loopback and the tailnet at port **9080**; private model-table diagnostics remain on **9083**. Public front-door policies, authentication and middleware are unchanged.

The private `compose.override.yaml` now adds one tailnet port publication to `apisix-host-netns`. The controller-private and live override files match byte-for-byte. Public tracked `compose.yaml`, environment files, application configuration, images and networks were not changed. No configuration baseline was adopted or advanced manually.

The hand-started `tailnet-apisix-relay` container was stopped for the cutover, retained until verification completed, then removed. Only the existing host namespace owner and its two namespace sharers were recreated; the other **45 containers**, including APISIX, cloudflared ingress relays and middleware, retained identical IDs, start times, status/health and restart counts. There are **48 containers** after legacy-relay removal.

## Preview and narrow-diff evidence

Before editing, private/live override SHA-256 was `b3f46dcb7c8284845ecf247b1f225c4e7dff08684aa4b09d9e68822249ae6521`. The final value is `72aa55babb05b00028d76b8c41703abef6fcb9f5d50571e11fe47b7aa0e9d115` on both copies.

The initial APISIX component preview returned **`ok=63 changed=0 failed=0`**. After preparing the candidate locally, its component preview correctly refused at **Validate single-service scope and pure-cache topology**, with `changed=0`: this operation permits only the selected service's image/environment changes, not another service's port publication. There is no host-namespace component profile. The controller override was restored before activation preparation.

The supervisor explicitly approved the manual-window preview exception, not a scope-gate bypass. Old and new candidates were rendered independently with the pinned controller renderer and the deployment's secret-free `env -i` Compose arguments. Normalized JSON differed **only** by one added tailnet `9080 → 9080` publication on `apisix-host-netns`; all three old publications remained intact. Host `docker compose config -q` passed, and a separate host render verified the same single effective delta.

Both overrides were backed up before in-place rewrites. Host snapshots, including the legacy container inspect needed to reconstruct it, are mode 0600 inside the mode-0700 directory `.ai-ops-state/tailnet-front-door-binding-20261030T032032Z/`. The controller backup remains under private `/var/tmp/ai-tailnet-window/` scratch.

## Cutover and interruption

Host-ingress established-connection samples at **03:20:53 / 03:21:03 / 03:21:13 UTC** were **0 / 0 / 0**. Activation started at **03:21:18.106 UTC**; all three replacement containers were running with zero restarts by **03:21:37.823 UTC**. They have no Docker healthcheck, so HTTP/inference checks below establish readiness.

The existing deployment's `env -i PATH=... docker compose` arguments ran:

```sh
timeout 90 env -i PATH="$REMOTE_PATH" docker compose -p ai-gateway \
  -f "$DEPLOY_ROOT/compose.yaml" -f "$DEPLOY_ROOT/compose.override.yaml" \
  --env-file "$DEPLOY_ROOT/.env" \
  up -d --force-recreate --no-deps --no-build --pull never \
  apisix-host-netns apisix-host-relay apisix-host-table-relay
```

| Container | Old ID | New ID |
|---|---|---|
| `apisix-host-netns` | `319cedb8d5b1` | `4da45d4f1902` |
| `apisix-host-relay` | `ad1cfccb5a80` | `2eba365d787d` |
| `apisix-host-table-relay` | `d101db8ca382` | `d39a69983e33` |
| Legacy `tailnet-apisix-relay` | `e5e04aedc1e4` | Removed after verification |

A host-side loopback-9080 HTTP monitor was reachable before and after activation. Its **185 samples**, approximately 100 ms apart with a 500 ms request timeout, recorded **132 failures in one interval of 14.243 seconds**, from the first failed sample to the first recovered sample. This is a sampled host-path interruption, not an exact duration for every client or port. Tailnet/9083 interruption was not independently timed. Public cloudflared-path containers were untouched; public availability was spot-checked rather than continuously monitored.

The namespace owner's IPv4 and IPv6 default routes remain absent, and its PID 1 runs as the unprivileged user. No extra network attachment or direct application publication was introduced.

## Post-checks

Checks ran both on the host and from the controller across the tailnet. No synthetic large-body probe was used.

| Check | Result |
|---|---|
| Loopback and tailnet `9080 /healthz` | Expected opaque **404**, empty body |
| Authenticated loopback/tailnet `/v1/models` | **200**, **343 → 343** entries; same bytes and independently checked bare-first code-point ordering |
| Controller-to-tailnet `/v1/models` | **200**, same 343-entry body as pre-capture |
| Plain non-stream Responses | **200**, completed; host loopback, host tailnet address and controller-to-tailnet |
| SSE Responses | **200**, terminal `response.completed`; all three access paths above |
| Unauthenticated `/v1/models` | Opaque **404**, empty body on loopback and tailnet |
| Loopback and tailnet `9083 /models-table` | **200**, including controller-to-tailnet |
| Public hostname `/v1/models` | **200**, same 343-entry body, sorted |
| APISIX error/module/route-load failures since activation | **0** |
| New host-ingress containers | Running, **restarts=0** |
| Other retained containers | All **45** unchanged |

All four host socket bindings—loopback/tailnet for each of **9080** and **9083**—are now owned by **docker-proxy**. No `socat` process owns a host 9080 socket. The existing in-namespace forwarding relays still run as designed; this removes only the hand-started host-network container.

The 9083 diagnostic body changed in size as its live observations updated; its endpoint remained successful. Client model-list bytes and membership did not change.

Temporary normal-client key **48** was deleted at **03:22:45 UTC**: DELETE **200**, follow-up GET **404**, then both the host and controller key files were removed and absence verified. Existing keys were not modified.

## Final state

Final read-only previews, from `ops/ansible/`:

- `deploy.yml -e component=apisix --check --diff`: **`ok=63 changed=0 failed=0`**, `config_diff=false`. This component reads the shared three-file set, including `compose.override.yaml`.
- `deploy.yml -e component=aisix --check --diff`: **`ok=63 changed=0 failed=0`**, `config_diff=false`.
- `deploy-config.yml -e component=aisix --check --diff`: **`ok=46 changed=0 failed=0`**, including the read-only unmanaged AISIX config comparison.

There was no final scope refusal. Only after these previews and HTTP/inference checks passed was `docker rm tailnet-apisix-relay` run; absence was verified at **03:28:59 UTC**. A search of systemd unit/config directories, system/user cron directories and the system crontab found **no reference** that would recreate it. No unit, cron entry, volume or database was removed.

Private inspect/config captures, socket listings, rendered-diff proofs, monitoring samples, HTTP captures, temporary-key deletion proofs and container snapshots remain under `/var/tmp/ai-tailnet-window/` on host/controller. They are not tracked. This record contains no credentials, host address or public hostname.

## Rollback (not exercised)

1. Restore the saved override to both controller and host by **in-place rewrite**; verify the original SHA-256 above and controller/live equality. Keep every unrelated private setting unchanged.
2. Run the deployment's `docker compose config -q`, then repeat the scoped owner-plus-two-sharer recreation shown above. This frees the tailnet 9080 publication while restoring the original three bindings.
3. Before legacy-container removal, `docker start tailnet-apisix-relay` would restore forwarding. After removal, reconstruct that container from the protected `legacy-inspect.json`: same captured image ID and command, host network and `unless-stopped` restart policy. Its command binds only the configured tailnet address and forwards to the existing loopback front door; do not publish a wildcard listener.
4. Verify loopback/tailnet 9080 and 9083, authenticated list/Responses/SSE, opaque 404 and public access again, and compare all unrelated container identities. No APISIX, cloudflared or middleware recreation is required.
