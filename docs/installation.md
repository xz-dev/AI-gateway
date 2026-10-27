# Installation

Step-by-step first installation and the Cloudflare Tunnel setup.

## Quick start

Requirements: Linux with `/dev/net/tun`, either Docker Engine with Compose v2.33.1+ or rootless Podman with a Compose provider, OpenSSL, and a remotely managed Cloudflare Tunnel. Scripts automatically select a running Docker or Podman engine; no `docker` compatibility shim is required. The same Compose uses only container-local shared namespaces, temporary route-setup capabilities, virtual DNS, and the TUN device; it never changes host firewall or routing state.

```bash
git clone --recurse-submodules https://github.com/xz-dev/AI-gateway.git "$HOME/AI-gateway"
cd "$HOME/AI-gateway"
./scripts/init.sh
```

`init.sh` generates private values without printing them. It refuses to overwrite an existing `.env`, CPA config, or AISIX runtime file. It creates `aisix/config.yaml` and `aisix/resources.yaml` behind a mode-`0700` directory, plus mode-`0600` `aisix/caller-keys.json` for operator use.

1. Replace `ADMIN_EMAIL=admin@example.invalid` in `.env`.
2. In Cloudflare Dashboard, create a remotely managed Tunnel and replace `CLOUDFLARED_TUNNEL_TOKEN` in `.env` using an editor that does not expose it in shell history. Keep `.env` mode `0600`.
3. Replace the example model entries in private `aisix/resources.yaml` with the approved concrete CPA targets and logical routes. Keep one CPA provider key, reuse each concrete target as one direct model, and give Sub2API only the logical IDs it is authorized to expose. Configure CPA-backed Sub2API accounts with Base URL `http://cli-proxy-api:8317/v1` (the relay to the inner models gateway), the private `sub2api` caller key from `aisix/caller-keys.json`, boolean pool mode, bounded retry, and account WS mode `off` after the front bridge is live. Never print either caller key.
4. Review the built-in control-plane rules in `data/egress-proxy/policy.json`, add only the deployment-specific provider or optional-feature destinations you need, then render it. This ignored runtime file is created only when absent, so repository upgrades never merge into or replace the user's allowlist:

   ```bash
   ./scripts/init-egress-proxy.sh
   ```

   The generated policy allows only the exact CPA and Sub2API control-plane `GET` requests documented in [Egress policy](egress-policy.md); provider, OAuth, plugin, private-sidecar, and user-defined egress remains denied. Use `tls: "bump"` with explicit methods and anchored POSIX path expressions. Use `tls: "splice"` only when preserving end-to-end TLS behavior is required; splice entries cannot enforce HTTP Host/path.
5. If production uses an optional provider-sidecar, add an explicit non-floating version-tagged `PROVIDER_SIDECAR_IMAGE`, non-root `PROVIDER_SIDECAR_USER`, and `PROVIDER_SIDECAR_API_KEY` only to the ignored `.env`. Initialize dedicated internal TLS, generate the ignored transport override, then add the image-specific environment, mounts, command, and healthcheck to that private override:

   ```bash
   ./scripts/init-provider-sidecar-tls.sh
   ./scripts/init-provider-sidecar-override.sh
   ```

   The public contract is intentionally narrow: the sidecar is OpenAI-compatible at `/v1`, listens on plain HTTP port `8080` inside its shared tunnel namespace, publishes no host port, runs as the declared non-root user, and obtains all Internet access through TUN → relay → Squid. The generator does not know or store any vendor-specific runtime setting.

6. Validate and start. AISIX uses the official `ghcr.io/api7/aisix` release; GitHub Actions publishes the independent status image to GHCR from its tracked Dockerfile. Compose consumes fixed version tags and never compiles them on the production host; ai-sse-keepalive-proxy and models-enricher remain local builds:

   ```bash
   git submodule update --init --recursive
   ./scripts/validate.sh
   source ./scripts/container-runtime.sh
   "${AI_GATEWAY_COMPOSE[@]}" pull cli-proxy-api postgres redis sub2api aisix aisix-status apisix cloudflared
   "${AI_GATEWAY_COMPOSE[@]}" up -d --build
   "${AI_GATEWAY_COMPOSE[@]}" ps
   ```

   Do not rely on `--wait`: rootless Podman installations without automatic healthcheck scheduling (for example OpenRC) may leave health at `starting`. `./scripts/validate.sh` performs explicit runtime probes instead of relying on engine health timers.

## Cloudflare Tunnel

Create a Public Hostname in Cloudflare Dashboard with this origin service:

```text
http://apisix-ingress:9080
```

Do not point the Tunnel directly at Sub2API or CPA. Configure a Cache Rule that bypasses cache for the API hostname. Cloudflare-side changes remain operator-owned; this repository stores no account ID, tunnel ID, hostname, or token.

The Compose service uses Cloudflare's supported `TUNNEL_TOKEN` environment variable. This is portable across Docker Compose implementations and avoids an unreadable UID-mismatched file secret. Docker daemon administrators can inspect container environments, so protect daemon access as root-equivalent. No local `cloudflared` ingress file is needed because hostname routing is managed in Cloudflare Dashboard.
