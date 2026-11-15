# Production move to an Alpine LXC — 2026-11-15

## Delivered

The production Compose stack moved from the 2C/2G Ubuntu VPS to an unprivileged
Alpine 3.24 LXC: 2 cores, 4 GiB RAM, 8 GiB swap and a 64 GiB root, with nesting,
keyctl, fuse and `/dev/net/tun`. The LXC uses the operator's existing Alpine LXC
baseline: daily `apk-autoupdate` behind a maintenance lock, logrotate, timezone
and SSH keys. The project directory, Compose project name and all 48 containers
are unchanged. The host joined the tailnet as a new node with the same tag as the
old host. Its private `.env` changed only `TAILSCALE_IP`, `CPA_BIND_ADDRESS` and
`SUB2API_BIND_ADDRESS`. Egress goes through the LAN gateway, not the old host.

The systemd units became OpenRC services; see [OpenRC hosts](../gateway-health-and-recovery.md)
and `ops/scripts/host/openrc/`. Docker runs with `live-restore`. A test container
and its published port survived a daemon restart, so package-update daemon
restarts keep the stack running. A Tailscale restart left every tailnet listener
answering, and the stack stayed up.

## Timeline (UTC)

| Time | Event |
|---|---|
| 03:35 | LXC created |
| 04:20–05:14 | Images exported, loaded and resolved (see deviations) |
| 04:06–08:47 | First data pass, with the old stack live (relayed via the controller until 04:39, then direct) |
| 08:47:09–08:54:20 | Warm pass, with the old stack live; established TCP samples 6/5/5 |
| 08:54:46 | Window open: old recovery timer and stack stopped **and disabled** |
| 08:56:24 | Old host reports 0 running project containers; final delta sync |
| 08:57:31 | `.env` rewritten (3 lines); `rc-service ai-gateway start` |
| 08:59:25 | 48 Up; tunnel registered; public route back at its baseline status |
| 09:01:24 | Recovery checks 8/8; window closed |
| 09:23:25–09:27:33 | Planned Compose-pin recreate (see deviations) |

Measured window, from old stop to all gates passing: **398 s**. The public route
was out for at most 279 s (08:54:46 → 08:59:25). Public interruption during the
09:23 recreate was not measured; the whole recreate took 248 s.

## Checks

- Health gate: all 48 containers Up, none unhealthy or starting. Tunnel connections
  registered. `gateway-recovery.py` reported 8/8 checks ok. The public route stayed
  at its pre-move status and never returned 5xx. The cutover script would have
  rolled back automatically if the gate failed within 10 minutes.
- Live traffic after the move: front APISIX returned 200s, and egress-proxy logged
  successful provider `CONNECT`/`POST` requests through the new egress path.
- The recovery loop runs with `apply=true` and reports all checks ok with 48/48
  observed. Its cgroup reads `memory.max=67108864` and `cpu.max=10000 100000`.
- Compose renders of old and new hosts, compared as parsed JSON, differed only
  in the three addresses plus renderer serialization (`create_host_path`
  defaults, duplicated `x-*` lists).
- After the pin, `docker compose --dry-run up -d` plans 0 recreates. Ansible
  `deploy.yml -e component=cloudflared --check --diff` passed (`changed=0`,
  `config_diff: false`). The private inventory and controller `.env` match live
  (SHA-256 of `.env`, `compose.yaml` and `compose.override.yaml`).

## Deviations

- **Image names.** `docker save` of `name:tag@digest` references wrote no names,
  so the images loaded as bare IDs. Eleven public images were pulled again by
  digest. Five could not be pulled: four local `rollback-20261015/*` images, and
  one CPA CI digest no longer in its registry. Each loaded image ID equals its
  pinned digest, so those five were tagged `name:tag` from that ID. All 18
  references then resolved by digest.
- **Nested cgroups.** The LXC's cgroup root held init and getty. Because of that, it
  delegated no controllers, and `pids_limit` containers failed with a missing
  `pids.max`. The `cgroup-delegate` boot service moves those processes into a
  leaf and enables the controllers. The leaf is named `init.scope`, because lxcfs
  prunes that suffix. Without that name, `/proc/meminfo` reported near-zero use,
  and the Ansible memory gate would always pass.
- **Compose pin.** The distribution Compose (5.6.1) did not match the
  `ai_ops_compose_renderer_version` pin (2.40.3), and Ansible correctly refused it.
  The checksum-verified 2.40.3 binary is installed at
  `/usr/local/lib/docker/cli-plugins/docker-compose`, ahead of the apk plugin and
  out of reach of auto-update. Because 2.40.3 hashes configuration differently,
  every container was recreated once. That recreate had a health gate and a
  rollback path.
- **Host package.** `py3-requests` was installed so that `community.docker` can
  run. The old host had `python3-requests`.
- **Tailnet tag.** Adding the tag to the new node changed its key. After the LXC
  rebooted, the node needed operator approval again.
- **Transfer.** The old host's uplink ran at 0.3–2 MB/s, so the first data pass took
  about 4.5 h. The new host pulled directly from the old one, authenticated by
  the operator's forwarded agent key. No temporary credential was created.
  Excluded from the copy: old `.ai-ops-state` window backups, `.rollback`
  material, one old forensics directory, and one orphan keeper volume. All of
  these remain on the old host.

## Rollback

The old host keeps all data, images and 48 stopped containers. Its units are
disabled, so a reboot does not start a second tunnel connector. To roll back:

1. On the new host, stop and disable `ai-gateway-recovery` and `ai-gateway`.
2. Confirm 0 running project containers on the new host.
3. On the old host, run `systemctl enable --now ai-gateway.service ai-gateway-recovery.timer`.

Data written on the new host after 08:57 is lost unless it is synced back while
both stacks are stopped.

## Follow-ups

- Decommission the old VPS after an observation period. It is still a tailnet exit node.
- The `ai_ops_recovery` role installs systemd units only. On OpenRC hosts, the
  units in `ops/scripts/host/openrc/` are installed manually.
- Tracked history still contains the old host's private addresses: 11 occurrences
  in archived records, archived OpenSpec changes, and the one-off
  `ops/scripts/host/keeper-observe.py`.
