# Optional provider-sidecar

Generic optional OpenAI-compatible provider sidecar: TLS relay, tunnel namespace, virtual DNS and recovery.

`provider-sidecar` is a reusable deployment role, not a product integration. Pin selected service image to an explicit non-floating version tag and keep all concrete runtime details in ignored production files. CPA reaches the role at exactly `https://provider-sidecar:8080/v1`; every matching production `api-key-entries[]` remains `proxy-url: direct`, so the global CPA proxy still applies only to Internet providers.

Run `./ops/scripts/init-provider-sidecar-tls.sh` before `./ops/scripts/init-provider-sidecar-override.sh`. Initializer creates ignored `data/provider-sidecar-tls/` mode `0700`, a dedicated CA, `DNS:provider-sidecar` server certificate, and combined CPA trust bundle. Dedicated CA and egress inspection CA use distinct keys. The dedicated CA private key stays host-only mode `0600`; relay receives only its leaf certificate and key. Public certificates, leaf key, and combined bundle use mode `0444` inside the inaccessible mode-`0700` parent so unprivileged read-only mounts can read only explicitly mounted files.

Initializer owns TLS material only. It does not create CPA provider accounts, planner identities, plugin metadata, or API-key files. Configure those application details separately in ignored CPA runtime state. Init mode refreshes the derived trust bundle; `--check` validates existing material without modifying it. Rotate by stopping the coupled stack, moving the entire TLS directory to protected backup, rerunning initializer, validating, and recreating the coupled services.

Generated override terminates TLS >=1.2 in existing nonroot/read-only/capability-free `cpa-provider-sidecar-relay`, then forwards plaintext only across isolated target network to provider-sidecar `:8080`. CPA receives combined public trust at existing trust target; provider-sidecar API-key authentication remains unchanged. Relay mounts only leaf certificate/key; dedicated CA key stays host-only and dedicated public CA reaches CPA only through combined trust bundle.

Override uses a `provider-sidecar-tunnel` namespace owner based on pinned `TUN2PROXY_IMAGE`, with `network_mode: service:provider-sidecar-tunnel` on provider-sidecar. Tunnel receives only `/dev/net/tun` and `NET_ADMIN`; it is not privileged and never changes host firewall or routing state.

Use `--dns virtual` and mount an ignored `data/egress-proxy/virtual-resolv.conf` containing only:

```text
nameserver 198.18.0.1
options ndots:0
```

Virtual DNS preserves the requested hostname for Squid CONNECT while preventing client-side DNS escape. Do not bind a host file over the tunnel owner's `/etc/resolv.conf`. The tunnel intentionally keeps only its disposable container layer writable because tun2proxy must rewrite Docker's runtime resolver file and clean up TUN state during startup and teardown; it has no persistent writable mount, remains unprivileged, and receives only `NET_ADMIN` and `DAC_OVERRIDE`. The `DAC_OVERRIDE` capability is required because Docker re-mounts `/etc/resolv.conf` read-only on container restart/boot-restore, while a fresh create leaves it writable: without it, tun2proxy's fallback direct write fails with EACCES and the tunnel crash-loops until recreated. CPA reaches provider-sidecar only through `cpa-provider-sidecar-relay`; the tunnel reaches Squid only through `provider-sidecar-squid-relay`. Each direction has separate two-member source and target networks, and a failed relay or tunnel loses connectivity instead of gaining direct Internet access.

For recovery, treat CPA, `cpa-provider-sidecar-relay`, provider-sidecar tunnel, and provider-sidecar as one unit. A missing/expired/mismatched TLS file must keep path failed closed; repair initializer state, validate, then recreate coupled services. The tunnel runs `restart: unless-stopped` with a `tun2proxy-bin --version` healthcheck so `depends_on: service_healthy` can gate the sidecar on a ready netns (boot-time start race). The fail-closed property is unchanged: if the tunnel is down, the sidecar has no route anywhere. A provider-sidecar process keeps the shared network namespace alive after the tunnel owner exits, so a tunnel restart leaves the sidecar in a stale netns until the pair is rebuilt. Rebuild the pair in order:

```bash
source ./ops/scripts/container-runtime.sh
"${AI_GATEWAY_COMPOSE[@]}" rm -s -f provider-sidecar
"${AI_GATEWAY_COMPOSE[@]}" rm -s -f provider-sidecar-tunnel
"${AI_GATEWAY_COMPOSE[@]}" up -d --no-build provider-sidecar-tunnel provider-sidecar
```

Configure the CPA entry for `https://provider-sidecar:8080/v1` in ignored runtime state. Set `proxy-url: direct` on each matching `api-key-entries[]` item because this HTTPS endpoint is an internal relay, while CPA's global proxy remains mandatory for Internet destinations. The template intentionally does not define provider/plugin identity fields.
