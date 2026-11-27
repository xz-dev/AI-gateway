# Docker Hub mirror on the production daemon — 2026-11-26 UTC

## Change

The operator approved China-region Docker Hub acceleration after repeated TLS handshake timeouts. The daemon now uses `https://docker.m.daocloud.io` under `registry-mirrors`; no application egress, DNS, credential, or TLS trust settings changed.

Before installing it, a production-origin HTTPS probe verified that DaoCloud returned the exact upstream OCI index bytes for the APISIX, PostgreSQL, Redis, and cloudflared candidates. Public registry bearer exchange used no user credentials. Another endpoint was rate-limited and was not configured.

At ~23:47 UTC the validated JSON was applied and Docker 29.9.3 reloaded with SIGHUP. The daemon PID/start time and all 48 gateway container IDs, images, start times, and states/health were unchanged across the operation. No application was restarted; user-visible traffic interruption was not measured separately. The original daemon config is stored with mode 0600 in a protected local directory under `/etc/docker/.ai-gateway-daemon-backups/`.

## Readback

- The daemon reported the one selected mirror and retained `live-restore`.
- `docker pull --platform linux/amd64` using the original `docker.io/apache/apisix:3.21.0-debian` reference succeeded; the exact index RepoDigest was `sha256:1741a8e1186e5c39ea10e6b4fb3e682534d1705efb8328195de5b41ad0657380`.
- Both guarded APISIX component previews passed after the pull, with exact local image proof. The standalone Docker CLI manifest lookup does not necessarily use the daemon mirror; this result does not claim that the direct Docker Hub TLS path recovered.
- This operation changed registry access and image cache only, not the application version selections.

## Recovery

If reverting, validate the saved JSON before restoring it. A SIGHUP reload of a file that omits `registry-mirrors` does not itself clear a previously configured mirror on this daemon release: load an explicit empty list, read back the empty live mirror list, then restore the original bytes. Check daemon identity, `live-restore`, and gateway container identities again. No rollback was needed during this change.
