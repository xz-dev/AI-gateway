#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"
env_file=${1:-}
if [ -z "$env_file" ]; then
  if [ -f .env ]; then env_file=.env; else env_file=.env.example; fi
fi
[ -f "$env_file" ] || { echo "missing $env_file" >&2; exit 1; }
for command in docker openssl python3; do
  command -v "$command" >/dev/null || { echo "$command is required" >&2; exit 1; }
done

value_from_env() {
  local key=$1
  awk -F= -v key="$key" '$1 == key {sub(/^[^=]*=/, ""); print; exit}' "$env_file"
}

if [ "$env_file" = .env ] || [ "$env_file" = "$root/.env" ]; then
  ! grep -Eq '^[A-Z0-9_]+=replace-with-' "$env_file" || { echo '.env still contains placeholders' >&2; exit 1; }
  [ "$(value_from_env ADMIN_EMAIL)" != admin@example.invalid ] || { echo 'set ADMIN_EMAIL in .env' >&2; exit 1; }
  for path in data/cpa/conf/config.yaml data/cpa/mgmt.key \
    data/egress-proxy/ca.key data/egress-proxy/ca.crt \
    data/egress-proxy/ca-bundle.pem data/egress-proxy/policy.json \
    data/egress-proxy/generated/squid.conf data/egress-proxy/virtual-resolv.conf; do
    [ -f "$path" ] || { echo "missing runtime file: $path" >&2; exit 1; }
  done
  [ "$(stat -c %a data/egress-proxy 2>/dev/null)" = 700 ] || {
    echo 'data/egress-proxy must have mode 700' >&2
    exit 1
  }
  [ "$(stat -c %a data/egress-proxy/generated 2>/dev/null)" = 755 ] || {
    echo 'data/egress-proxy/generated must have mode 755 for the unprivileged proxy UID' >&2
    exit 1
  }
  key_id=$(openssl pkey -in data/egress-proxy/ca.key -pubout -outform DER 2>/dev/null | openssl dgst -sha256)
  cert_id=$(openssl x509 -in data/egress-proxy/ca.crt -pubkey -noout 2>/dev/null | openssl pkey -pubin -outform DER 2>/dev/null | openssl dgst -sha256)
  [ "$key_id" = "$cert_id" ] || { echo 'egress CA key and certificate do not match' >&2; exit 1; }
  openssl x509 -in data/egress-proxy/ca.crt -noout -checkend 2592000 >/dev/null || {
    echo 'egress CA expires within 30 days' >&2
    exit 1
  }
  [ "$(grep -Ec '^nameserver[[:space:]]+198\.18\.0\.1$' data/egress-proxy/virtual-resolv.conf)" = 1 ] || {
    echo 'virtual-resolv.conf must use tun2proxy virtual DNS' >&2
    exit 1
  }
  [ "$(grep -Ec '^nameserver[[:space:]]+' data/egress-proxy/virtual-resolv.conf)" = 1 ] || {
    echo 'virtual-resolv.conf must contain exactly one nameserver' >&2
    exit 1
  }
  [ "$(stat -c %a data/egress-proxy/virtual-resolv.conf)" = 444 ] || {
    echo 'virtual-resolv.conf must have mode 444' >&2
    exit 1
  }
  cert_lines=$(wc -l <data/egress-proxy/ca.crt)
  tail -n "$cert_lines" data/egress-proxy/ca-bundle.pem | cmp -s - data/egress-proxy/ca.crt || {
    echo 'egress CA bundle does not end with the inspection CA' >&2
    exit 1
  }
  [ "$(stat -c %a data/sub2api/postgres 2>/dev/null)" = 1777 ] || {
    echo 'data/sub2api/postgres must have mode 1777 for PostgreSQL 18' >&2
    exit 1
  }
fi

for path in .env .pi cpa/config.yaml secrets/cloudflare-tunnel-token data; do
  if git ls-files --error-unmatch "$path" >/dev/null 2>&1; then
    echo "private runtime path is tracked: $path" >&2
    exit 1
  fi
done

submodule=middleware/ai-sse-keepalive-proxy
expected_submodule=c427191d5a171aa7dbdf8577b52c35c274592e53
[ "$(git config -f .gitmodules --get submodule.middleware/ai-sse-keepalive-proxy.path)" = "$submodule" ] || {
  echo 'AI SSE keepalive proxy submodule path mismatch' >&2
  exit 1
}
[ "$(git config -f .gitmodules --get submodule.middleware/ai-sse-keepalive-proxy.url)" = 'https://github.com/xz-dev/ai-sse-keepalive-proxy.git' ] || {
  echo 'AI SSE keepalive proxy submodule URL mismatch' >&2
  exit 1
}
[ "$(git ls-files -s "$submodule" | awk '{print $1 " " $2}')" = "160000 $expected_submodule" ] || {
  echo 'AI SSE keepalive proxy gitlink mismatch' >&2
  exit 1
}
if [ ! -f "$submodule/Dockerfile" ] || [ "$(git -C "$submodule" rev-parse HEAD)" != "$expected_submodule" ]; then
  echo 'AI SSE keepalive proxy submodule is not initialized at reviewed commit' >&2
  exit 1
fi
if [ -n "$(git -C "$submodule" status --porcelain --untracked-files=all)" ]; then
  echo 'AI SSE keepalive proxy submodule is dirty' >&2
  exit 1
fi
! git grep -nE 'apisix-sse-keepalive|middleware/sse-keepalive|ghcr\.io/.*/ai-sse-keepalive-proxy' -- ':!middleware/ai-sse-keepalive-proxy' ':!scripts/validate.sh' >/dev/null || {
  echo 'obsolete or registry-hosted AI SSE middleware reference found' >&2
  exit 1
}

cpa_config=cpa/config.example.yaml
if [ "$env_file" = .env ] || [ "$env_file" = "$root/.env" ]; then cpa_config=data/cpa/conf/config.yaml; fi
grep -Eq '^proxy-url:[[:space:]]*"?http://cpa-egress-relay:3128"?[[:space:]]*$' "$cpa_config" || {
  echo "$cpa_config must set the global pairwise proxy-url" >&2
  exit 1
}

tmpdir=$(mktemp -d /tmp/ai-gateway-validate.XXXXXX)
trap 'rm -rf "$tmpdir"' EXIT
python3 scripts/render-egress-policy.py egress-proxy/testdata/policy.json "$tmpdir/proxy-test"
python3 - "$tmpdir/proxy-test" <<'PY'
import ipaddress
import sys
from pathlib import Path

root = Path(sys.argv[1])
if root.stat().st_mode & 0o777 != 0o755:
    raise SystemExit("generated proxy directory must be mode 755")
ipv4 = tuple(ipaddress.ip_network(line) for line in (root / "policy-blocked-ipv4-cidrs").read_text().splitlines())
ipv6 = tuple(ipaddress.ip_network(line) for line in (root / "policy-blocked-ipv6-cidrs").read_text().splitlines())
if any(network.version != 4 for network in ipv4) or any(network.version != 6 for network in ipv6):
    raise SystemExit("blocked destination ACLs must keep IPv4 and IPv6 separate")
def blocked_ipv6(address: ipaddress.IPv6Address) -> bool:
    if address.ipv4_mapped is not None:
        return any(address.ipv4_mapped in network for network in ipv4)
    return any(address in network for network in ipv6)

for text in (
    "::", "::1", "::ffff:127.0.0.1", "::ffff:10.0.0.1", "64:ff9b::7f00:1",
    "64:ff9b:1::1", "100::1", "2001::1", "2001:db8::1", "2002:0a00:1::1",
    "3ffe::1", "3fff::1", "5f00::1", "fc00::1", "fe80::1", "ff00::1",
):
    address = ipaddress.ip_address(text)
    if not blocked_ipv6(address):
        raise SystemExit(f"non-global IPv6 address is not blocked: {address}")
for text in ("2606:4700:4700::1111", "2607:f8b0:4005:805::200e"):
    address = ipaddress.ip_address(text)
    if any(address in network for network in ipv6):
        raise SystemExit(f"global IPv6 address is unexpectedly blocked: {address}")

unbound = Path("egress-proxy/unbound.conf").read_text().splitlines()
resolver_private = tuple(
    ipaddress.ip_network(line.split(":", 1)[1].strip())
    for line in unbound
    if line.strip().startswith("private-address:")
)
for text in (
    "10.0.0.1", "100.64.0.1", "169.254.169.254", "192.168.0.1",
    "::1", "::ffff:10.0.0.1", "64:ff9b::7f00:1", "2002:0a00:1::1", "3ffe::1", "fc00::1",
):
    address = ipaddress.ip_address(text)
    if not any(address in network for network in resolver_private if address.version == network.version):
        raise SystemExit(f"filtered resolver does not cover non-global address: {address}")
for text in ("1.1.1.1", "2606:4700:4700::1111"):
    address = ipaddress.ip_address(text)
    if any(address in network for network in resolver_private if address.version == network.version):
        raise SystemExit(f"filtered resolver unexpectedly blocks global address: {address}")
if "dns_nameservers 127.0.0.1" not in (root / "squid.conf").read_text().splitlines():
    raise SystemExit("Squid must resolve exclusively through the filtered local resolver")
PY
if [ "$env_file" = .env ] || [ "$env_file" = "$root/.env" ]; then
  python3 scripts/render-egress-policy.py data/egress-proxy/policy.json "$tmpdir/proxy-runtime"
  diff -ru "$tmpdir/proxy-runtime" data/egress-proxy/generated >/dev/null || {
    echo 'stale egress config; run ./scripts/init-egress-proxy.sh' >&2
    exit 1
  }
fi

compose_args=(-f "$root/compose.yaml")
override_file=${AI_GATEWAY_COMPOSE_OVERRIDE:-}
if [ -n "$override_file" ]; then
  [ -f "$override_file" ] || { echo "missing AI_GATEWAY_COMPOSE_OVERRIDE: $override_file" >&2; exit 1; }
  compose_args+=(-f "$override_file")
elif [ -f "$root/compose.override.yaml" ]; then
  override_file=$root/compose.override.yaml
  compose_args+=(-f "$override_file")
fi
if [ -n "$override_file" ] && grep -Eq '^[[:space:]]{2}provider-sidecar:' "$override_file"; then
  tls_check_env=()
  for variable in PROVIDER_SIDECAR_TLS_DIR PROVIDER_SIDECAR_CPA_AUTH_DIR PROVIDER_SIDECAR_ENV_FILE PROVIDER_SIDECAR_EGRESS_CA_CERT PROVIDER_SIDECAR_EGRESS_CA_BUNDLE; do
    [ -z "${!variable:-}" ] || tls_check_env+=("$variable=${!variable}")
  done
  env "${tls_check_env[@]}" ./scripts/init-provider-sidecar-tls.sh --check >/dev/null
fi
docker compose "${compose_args[@]}" --env-file "$env_file" config --quiet
rendered=$tmpdir/compose
if docker compose "${compose_args[@]}" --env-file "$env_file" config --format json >"$rendered" 2>/dev/null; then
  rendered_format=json
else
  docker compose "${compose_args[@]}" --env-file "$env_file" config >"$rendered"
  rendered_format=yaml
fi
runtime_mode=0
if [ "$env_file" = .env ] || [ "$env_file" = "$root/.env" ]; then runtime_mode=1; fi
python3 - "$rendered" "$rendered_format" "$runtime_mode" <<'PY'
import json
import re
import sys
from pathlib import Path

with open(sys.argv[1], encoding="utf-8") as handle:
    if sys.argv[2] == "json":
        compose = json.load(handle)
    else:
        try:
            import yaml
        except ImportError as error:
            raise SystemExit("Compose lacks JSON output; install PyYAML for validation") from error
        compose = yaml.safe_load(handle)

services = compose["services"]
networks = compose["networks"]
has_provider_sidecar = "provider-sidecar" in services

if sys.argv[3] == "1" and has_provider_sidecar:
    lines = Path("data/cpa/conf/config.yaml").read_text().splitlines()
    old_matches = [line for line in lines if re.match(r'^\s{4}base-url:\s*["\']?http://provider-sidecar:8080/v1/?["\']?\s*$', line, re.IGNORECASE)]
    if old_matches:
        raise SystemExit("CPA config still contains plaintext provider-sidecar base URL")
    base_matches = [line for line in lines if re.match(r'^\s{4}base-url:\s*["\']?https://provider-sidecar:8080/v1["\']?\s*$', line)]
    for base in base_matches:
        if base.count("https://provider-sidecar:8080/v1") != 1:
            raise SystemExit("malformed provider-sidecar HTTPS base URL")

    auth_dir = Path("data/cpa/auths")
    planner_matches = []

    def strict_object(pairs):
        value = {}
        seen = set()
        for key, item in pairs:
            folded = key.casefold()
            if folded in seen:
                raise ValueError("duplicate JSON key")
            seen.add(folded)
            value[key] = item
        return value

    def accepted_type(value):
        if not isinstance(value, str):
            return False
        value = value.strip().lower()
        return value == "openai-compatibility" or value.startswith("openai-compatible-") or value.startswith("openai-compatibility:")

    def normalize_base_url(raw):
        from urllib.parse import urlsplit, urlunsplit
        if not isinstance(raw, str):
            raise ValueError("missing base_url")
        parsed = urlsplit(raw.strip())
        port = parsed.port
        host = parsed.hostname
        if parsed.scheme.lower() != "https" or not host or parsed.username is not None or parsed.password is not None or parsed.query or parsed.fragment:
            raise ValueError("invalid base_url")
        host = host.lower()
        if ":" in host:
            host = f"[{host}]"
        if port not in (None, 443):
            host += f":{port}"
        path = "" if parsed.path == "/" else parsed.path.rstrip("/")
        return urlunsplit(("https", host, path, "", ""))

    for path in auth_dir.glob("*.json"):
        try:
            raw = path.read_text()
            loose = json.loads(raw)
        except (OSError, json.JSONDecodeError):
            continue
        if not isinstance(loose, dict) or loose.get("disabled") is True or not accepted_type(loose.get("type")):
            continue
        try:
            value = json.loads(raw, object_pairs_hook=strict_object)
        except (json.JSONDecodeError, ValueError) as error:
            raise SystemExit(f"active OpenAI-compatible CPA auth is not strict JSON: {path.name}") from error
        try:
            normalized_base = normalize_base_url(value.get("base_url"))
        except ValueError as error:
            raise SystemExit(f"active OpenAI-compatible CPA auth has invalid base_url: {path.name}") from error
        if normalized_base == "https://provider-sidecar:8080/v1":
            planner_matches.append((path, value))
    if len(planner_matches) != 1:
        raise SystemExit("CPA auth directory must contain exactly one HTTPS provider-sidecar planner auth")
    _, planner = planner_matches[0]
    if planner.get("proxy_url") != "direct" or not isinstance(planner.get("token"), str) or not planner["token"]:
        raise SystemExit("provider-sidecar planner auth must contain nonempty token and proxy_url direct")

    matches = [index for index, line in enumerate(lines) if re.match(r'^\s{4}base-url:\s*["\']?https://provider-sidecar:8080/v1["\']?\s*$', line)]
    if len(matches) != 1:
        raise SystemExit("CPA config must contain exactly one HTTPS provider-sidecar compatibility entry")
    for base in matches:
        start = max(index for index in range(base, -1, -1) if re.match(r'^\s{2}-\s+name:', lines[index]))
        end = next((index for index in range(base + 1, len(lines)) if re.match(r'^\s{2}-\s+name:', lines[index])), len(lines))
        block = lines[start:end]
        if any(re.match(r'^\s{4}proxy-url:', line) for line in block):
            raise SystemExit("provider-sidecar direct routing must be set per API key, not on the provider")
        keys = [index for index, line in enumerate(block) if re.match(r'^\s{6}-\s+api-key:', line)]
        if not keys:
            raise SystemExit("provider-sidecar compatibility entry has no API keys")
        for key in keys:
            key_end = next((index for index in range(key + 1, len(block)) if len(block[index]) - len(block[index].lstrip()) <= 6), len(block))
            if not any(re.match(r'^\s{8}proxy-url:\s*["\']?direct["\']?\s*$', line) for line in block[key + 1:key_end]):
                raise SystemExit("every provider-sidecar API key must set proxy-url: direct")

base_services = {
    "egress-proxy", "cpa-host-netns", "cpa-host-relay", "cpa-netns", "cli-proxy-api", "cpa-squid-relay",
    "postgres", "redis", "sub2api-host-netns", "sub2api-host-relay", "sub2api-netns", "sub2api",
    "sub2api-cpa-relay", "sub2api-postgres-relay", "sub2api-redis-relay", "sub2api-squid-relay",
    "apisix-host-netns", "apisix-host-relay", "apisix-netns", "apisix", "apisix-ai-sse-relay",
    "ai-sse-keepalive-proxy-netns", "ai-sse-keepalive-proxy", "ai-sse-sub2api-relay",
    "cloudflared-apisix-relay", "cloudflared",
}
optional_services = {"cpa-provider-sidecar-relay", "provider-sidecar-tunnel", "provider-sidecar", "provider-sidecar-squid-relay"}
expected_services = base_services | (optional_services if has_provider_sidecar else set())
if set(services) != expected_services:
    raise SystemExit(f"unexpected services: {sorted(set(services) ^ expected_services)}")
if ("provider-sidecar-tunnel" in services) != has_provider_sidecar:
    raise SystemExit("provider-sidecar and its namespace tunnel must be enabled together")
if "default" in networks or compose.get("volumes"):
    raise SystemExit("default network and persistent namespace volumes are forbidden")

def image(config):
    return str(config.get("image", ""))

def require_image(service, repository, exact=None):
    value = image(services[service])
    if exact:
        if value != exact:
            raise SystemExit(f"{service} must use {exact}")
    elif not value.startswith(repository + "@sha256:"):
        raise SystemExit(f"{service} image must use {repository} pinned by digest")

def volume_targets(config):
    out = set()
    for volume in config.get("volumes", []):
        if isinstance(volume, dict): out.add(volume.get("target"))
        else:
            parts = str(volume).split(":")
            if len(parts) > 1: out.add(parts[1])
    return out

def extra_hosts(config):
    value = config.get("extra_hosts", [])
    if isinstance(value, dict):
        return {str(name): str(address) for name, address in value.items()}
    result = {}
    for item in value:
        separator = "=" if "=" in str(item) else ":"
        name, address = str(item).split(separator, 1)
        result[name] = address
    return result

def normalized(parts):
    return [str(part).replace("$$", "$") for part in parts]

def memory_bytes(value):
    text = str(value).lower()
    if text.isdigit():
        return int(text)
    units = {"k":1024, "m":1024**2, "g":1024**3}
    return int(text[:-1]) * units[text[-1]]

def require_relay_capacity(service, config):
    if int(config.get("pids_limit", 0)) != 512:
        raise SystemExit(f"{service} must reserve PID headroom for streaming connections")
    if memory_bytes(config.get("mem_limit", 0)) != 64 * 1024**2 or memory_bytes(config.get("memswap_limit", 0)) != 64 * 1024**2:
        raise SystemExit(f"{service} must reserve memory for 256 concurrent connections")

def require_dependency(service, dependency, restart=None):
    settings = services[service].get("depends_on", {}).get(dependency, {})
    if settings.get("condition") != "service_started":
        raise SystemExit(f"{service}->{dependency} must use service_started")
    if restart is not None and settings.get("restart") is not restart:
        raise SystemExit(f"{service}->{dependency} restart must be {restart}")

for service, repository in {
    "cli-proxy-api":"ghcr.io/xz-dev/cli-proxy-api", "sub2api":"ghcr.io/wei-shaw/sub2api",
    "apisix":"docker.io/apache/apisix", "postgres":"docker.io/library/postgres",
    "redis":"docker.io/library/redis", "cloudflared":"docker.io/cloudflare/cloudflared",
}.items(): require_image(service, repository)
for service in ("cpa-netns", "sub2api-netns", "apisix-netns", "ai-sse-keepalive-proxy-netns", "cpa-host-netns", "sub2api-host-netns", "apisix-host-netns"):
    require_image(service, "docker.io/library/alpine")
proxy_image = image(services["ai-sse-keepalive-proxy"])
if proxy_image != "ai-sse-keepalive-proxy:latest":
    raise SystemExit("AI SSE keepalive proxy must use exact local tag ai-sse-keepalive-proxy:latest")
build = services["ai-sse-keepalive-proxy"].get("build", {})
if Path(str(build.get("context", ""))).resolve() != Path("middleware/ai-sse-keepalive-proxy").resolve():
    raise SystemExit("AI SSE keepalive proxy must build from its pinned submodule")
if services["ai-sse-keepalive-proxy"].get("pull_policy") != "build":
    raise SystemExit("AI SSE keepalive proxy must force a local Compose build")
socat_image = "docker.io/alpine/socat@sha256:3d9e7966201dd3a065df591020a09fd3c70845de7e7086e3531ea69db774406b"
relays = {
    "cpa-squid-relay", "sub2api-host-relay", "sub2api-cpa-relay", "sub2api-postgres-relay",
    "sub2api-redis-relay", "sub2api-squid-relay", "apisix-host-relay", "apisix-ai-sse-relay",
    "ai-sse-sub2api-relay", "cloudflared-apisix-relay",
} | ({"cpa-provider-sidecar-relay", "provider-sidecar-squid-relay"} if has_provider_sidecar else set())
for relay in relays: require_image(relay, "docker.io/alpine/socat", socat_image)
require_image("cpa-host-relay", "docker.io/alpine/socat", socat_image)

expected_owner_command = [
    "/bin/sh", "-ec",
    "while ip -4 route show default | grep -q .; do ip -4 route del default; done; "
    "while ip -6 route show default | grep -q .; do ip -6 route del default; done; "
    'test -z "$(ip -4 route show default)"; test -z "$(ip -6 route show default)"; '
    "exec su nobody -s /bin/sh -c 'exec sleep infinity'",
]
app_owners = {
    "cli-proxy-api":"cpa-netns", "sub2api":"sub2api-netns", "apisix":"apisix-netns",
    "ai-sse-keepalive-proxy":"ai-sse-keepalive-proxy-netns",
}
app_commands = {
    "cli-proxy-api": (
        ["/bin/bash", "-ec"],
        "while awk 'NR > 1 && $2 == \"00000000\" && $8 == \"00000000\" {found=1} END {exit(found ? 0 : 1)}' /proc/net/route || "
        "awk '$1 == \"00000000000000000000000000000000\" && $2 == \"00\" && $10 != \"lo\" {found=1} END {exit(found ? 0 : 1)}' /proc/net/ipv6_route; "
        "do sleep 0.05; done; exec ./CLIProxyAPI -config /CLIProxyAPI/conf/config.yaml",
    ),
    "sub2api": (
        ["/bin/sh", "-ec"],
        "while awk 'NR > 1 && $2 == \"00000000\" && $8 == \"00000000\" {found=1} END {exit(found ? 0 : 1)}' /proc/net/route || "
        "awk '$1 == \"00000000000000000000000000000000\" && $2 == \"00\" && $10 != \"lo\" {found=1} END {exit(found ? 0 : 1)}' /proc/net/ipv6_route; "
        "do sleep 0.05; done; exec /app/docker-entrypoint.sh /app/sub2api",
    ),
    "apisix": (
        ["/bin/bash", "-ec"],
        "while awk 'NR > 1 && $2 == \"00000000\" && $8 == \"00000000\" {found=1} END {exit(found ? 0 : 1)}' /proc/net/route || "
        "awk '$1 == \"00000000000000000000000000000000\" && $2 == \"00\" && $10 != \"lo\" {found=1} END {exit(found ? 0 : 1)}' /proc/net/ipv6_route; "
        "do sleep 0.05; done; exec /docker-entrypoint.sh docker-start",
    ),
}
for owner in ("cpa-netns", "sub2api-netns", "apisix-netns", "ai-sse-keepalive-proxy-netns", "cpa-host-netns", "sub2api-host-netns", "apisix-host-netns"):
    cfg=services[owner]
    if cfg.get("read_only") is not True or set(cfg.get("cap_drop",[])) != {"ALL"} or set(cfg.get("cap_add",[])) != {"NET_ADMIN","SETGID","SETUID"}:
        raise SystemExit(f"{owner} namespace hardening mismatch")
    if cfg.get("privileged") is True or "no-new-privileges:true" not in cfg.get("security_opt",[]):
        raise SystemExit(f"{owner} must not be privileged")
    if normalized(cfg.get("command",[])) != expected_owner_command:
        raise SystemExit(f"{owner} must remove default routes and drop privilege")
for app, owner in app_owners.items():
    cfg = services[app]
    if cfg.get("network_mode") != f"service:{owner}" or cfg.get("ports") or cfg.get("networks"):
        raise SystemExit(f"{app} must share only {owner} network namespace")
    require_dependency(app, owner, True)
    if app in app_commands:
        entrypoint, command = app_commands[app]
        if normalized(cfg.get("entrypoint", [])) != entrypoint or normalized(cfg.get("command", [])) != [command]:
            raise SystemExit(f"{app} must preserve its route guard and upstream entrypoint")
for owner, relay in (("cpa-host-netns","cpa-host-relay"),("sub2api-host-netns","sub2api-host-relay"),("apisix-host-netns","apisix-host-relay")):
    if not services[owner].get("ports") or services[relay].get("network_mode") != f"service:{owner}" or services[relay].get("networks"):
        raise SystemExit(f"{relay} must share only published namespace {owner}")
    require_dependency(relay, owner, True)
for service, dependency, restart in (
    ("cli-proxy-api", "cpa-host-relay", None),
    ("cli-proxy-api", "cpa-squid-relay", True),
    ("sub2api", "sub2api-host-relay", None),
    ("sub2api", "sub2api-cpa-relay", True),
    ("sub2api", "sub2api-postgres-relay", True),
    ("sub2api", "sub2api-redis-relay", True),
    ("sub2api", "sub2api-squid-relay", True),
    ("apisix", "apisix-host-relay", None),
    ("apisix", "apisix-ai-sse-relay", True),
    ("ai-sse-keepalive-proxy", "ai-sse-sub2api-relay", True),
    ("cloudflared", "cloudflared-apisix-relay", True),
    ("cpa-squid-relay", "egress-proxy", None),
    ("sub2api-squid-relay", "egress-proxy", None),
    ("sub2api-cpa-relay", "cli-proxy-api", None),
    ("sub2api-postgres-relay", "postgres", None),
    ("sub2api-redis-relay", "redis", None),
    ("apisix-ai-sse-relay", "ai-sse-keepalive-proxy", None),
    ("ai-sse-sub2api-relay", "sub2api", None),
    ("cloudflared-apisix-relay", "apisix", None),
):
    require_dependency(service, dependency, restart)
if has_provider_sidecar:
    for service, dependency, restart in (
        ("cli-proxy-api", "cpa-provider-sidecar-relay", True),
        ("cpa-provider-sidecar-relay", "provider-sidecar", None),
        ("provider-sidecar-tunnel", "provider-sidecar-squid-relay", True),
        ("provider-sidecar", "provider-sidecar-tunnel", True),
        ("provider-sidecar-squid-relay", "egress-proxy", None),
    ):
        require_dependency(service, dependency, restart)
for service, cfg in services.items():
    if service not in ("cpa-host-netns","sub2api-host-netns","apisix-host-netns") and cfg.get("ports"):
        raise SystemExit(f"only host ingress namespace owners may publish ports: {service}")
    if cfg.get("privileged") is True:
        raise SystemExit(f"production service must not be privileged: {service}")
    for dependency, settings in cfg.get("depends_on",{}).items():
        if settings.get("condition") == "service_healthy":
            raise SystemExit(f"{service}->{dependency} uses nonportable service_healthy")

def published_targets(config):
    targets = set()
    for port in config.get("ports", []):
        if isinstance(port, dict):
            targets.add((int(port["target"]), str(port.get("protocol", "tcp"))))
        else:
            value = str(port).split("/", 1)
            targets.add((int(value[0].rsplit(":", 1)[-1]), value[1] if len(value) == 2 else "tcp"))
    return targets

expected_published = {
    "cpa-host-netns": {(8317,"tcp"),(8085,"tcp"),(1455,"tcp"),(54545,"tcp"),(51121,"tcp"),(11451,"tcp")},
    "sub2api-host-netns": {(8080,"tcp")},
    "apisix-host-netns": {(9080,"tcp")},
}
for owner, expected in expected_published.items():
    if published_targets(services[owner]) != expected:
        raise SystemExit(f"{owner} host publication mismatch")

expected_network_members = {
    "cpa-host-source": {"cpa-host-netns":"172.30.1.2"},
    "host-cpa-target": {"cpa-host-netns":"172.30.2.3","cpa-netns":"172.30.2.2"},
    "sub2api-host-source": {"sub2api-host-netns":"172.30.3.2"},
    "host-sub2api-target": {"sub2api-host-netns":"172.30.4.3","sub2api-netns":"172.30.4.2"},
    "apisix-host-source": {"apisix-host-netns":"172.30.5.2"},
    "host-apisix-target": {"apisix-host-netns":"172.30.6.3","apisix-netns":"172.30.6.2"},
    "cloudflared-apisix-source": {"cloudflared":"172.30.7.2","cloudflared-apisix-relay":"172.30.7.3"},
    "cloudflared-apisix-target": {"cloudflared-apisix-relay":"172.30.8.3","apisix-netns":"172.30.8.2"},
    "apisix-ai-sse-source": {"apisix-netns":"172.30.9.2","apisix-ai-sse-relay":"172.30.9.3"},
    "apisix-ai-sse-target": {"apisix-ai-sse-relay":"172.30.10.3","ai-sse-keepalive-proxy-netns":"172.30.10.2"},
    "sub2api-cpa-source": {"sub2api-netns":"172.30.11.2","sub2api-cpa-relay":"172.30.11.3"},
    "sub2api-cpa-target": {"sub2api-cpa-relay":"172.30.12.3","cpa-netns":"172.30.12.2"},
    "sub2api-postgres-source": {"sub2api-netns":"172.30.13.2","sub2api-postgres-relay":"172.30.13.3"},
    "sub2api-postgres-target": {"sub2api-postgres-relay":"172.30.14.3","postgres":"172.30.14.2"},
    "sub2api-redis-source": {"sub2api-netns":"172.30.15.2","sub2api-redis-relay":"172.30.15.3"},
    "sub2api-redis-target": {"sub2api-redis-relay":"172.30.16.3","redis":"172.30.16.2"},
    "cpa-squid-source": {"cpa-netns":"172.30.17.2","cpa-squid-relay":"172.30.17.3"},
    "cpa-squid-target": {"cpa-squid-relay":"172.30.18.3","egress-proxy":"172.30.18.2"},
    "sub2api-squid-source": {"sub2api-netns":"172.30.19.2","sub2api-squid-relay":"172.30.19.3"},
    "sub2api-squid-target": {"sub2api-squid-relay":"172.30.20.3","egress-proxy":"172.30.20.2"},
    "ai-sse-sub2api-source": {"ai-sse-keepalive-proxy-netns":"172.30.25.2","ai-sse-sub2api-relay":"172.30.25.3"},
    "ai-sse-sub2api-target": {"ai-sse-sub2api-relay":"172.30.26.3","sub2api-netns":"172.30.26.2"},
    "proxy-egress": {"egress-proxy":None},
    "cloudflare-egress": {"cloudflared":None},
}
if has_provider_sidecar:
    expected_network_members.update({
        "cpa-provider-sidecar-source": {"cpa-netns":"172.30.21.2","cpa-provider-sidecar-relay":"172.30.21.3"},
        "cpa-provider-sidecar-target": {"cpa-provider-sidecar-relay":"172.30.22.3","provider-sidecar-tunnel":"172.30.22.2"},
        "provider-sidecar-squid-source": {"provider-sidecar-tunnel":"172.30.23.2","provider-sidecar-squid-relay":"172.30.23.3"},
        "provider-sidecar-squid-target": {"provider-sidecar-squid-relay":"172.30.24.3","egress-proxy":"172.30.24.2"},
    })
if set(networks) != set(expected_network_members):
    raise SystemExit(f"unexpected networks: {sorted(set(networks) ^ set(expected_network_members))}")
for network, expected in expected_network_members.items():
    actual = {}
    for service, config in services.items():
        settings = (config.get("networks") or {}).get(network)
        if settings is not None:
            actual[service] = str(settings.get("ipv4_address")) if settings.get("ipv4_address") else None
    if actual != expected:
        raise SystemExit(f"{network} membership/address mismatch: {actual}")
required_aliases = {
    ("cpa-squid-relay","cpa-squid-source"):"cpa-egress-relay",
    ("sub2api-cpa-relay","sub2api-cpa-source"):"cli-proxy-api",
    ("sub2api-postgres-relay","sub2api-postgres-source"):"postgres",
    ("sub2api-redis-relay","sub2api-redis-source"):"redis",
    ("sub2api-squid-relay","sub2api-squid-source"):"sub2api-egress-relay",
    ("apisix-ai-sse-relay","apisix-ai-sse-source"):"ai-sse-keepalive-ingress-relay",
    ("ai-sse-sub2api-relay","ai-sse-sub2api-source"):"sub2api-ai-sse-relay",
    ("cloudflared-apisix-relay","cloudflared-apisix-source"):"apisix-ingress",
}
if has_provider_sidecar:
    required_aliases.update({
        ("cpa-provider-sidecar-relay","cpa-provider-sidecar-source"):"provider-sidecar",
        ("provider-sidecar-squid-relay","provider-sidecar-squid-source"):"provider-sidecar-egress-relay",
    })
for (service, network), alias in required_aliases.items():
    if alias not in set(services[service]["networks"][network].get("aliases", [])):
        raise SystemExit(f"{service}/{network} must provide {alias}")

source_target_edges = [
    ("cloudflared","cloudflared-apisix-relay","apisix-netns","cloudflared-apisix-source","cloudflared-apisix-target"),
    ("apisix-netns","apisix-ai-sse-relay","ai-sse-keepalive-proxy-netns","apisix-ai-sse-source","apisix-ai-sse-target"),
    ("ai-sse-keepalive-proxy-netns","ai-sse-sub2api-relay","sub2api-netns","ai-sse-sub2api-source","ai-sse-sub2api-target"),
    ("sub2api-netns","sub2api-cpa-relay","cpa-netns","sub2api-cpa-source","sub2api-cpa-target"),
    ("sub2api-netns","sub2api-postgres-relay","postgres","sub2api-postgres-source","sub2api-postgres-target"),
    ("sub2api-netns","sub2api-redis-relay","redis","sub2api-redis-source","sub2api-redis-target"),
    ("cpa-netns","cpa-squid-relay","egress-proxy","cpa-squid-source","cpa-squid-target"),
    ("sub2api-netns","sub2api-squid-relay","egress-proxy","sub2api-squid-source","sub2api-squid-target"),
]
if has_provider_sidecar:
    source_target_edges += [
        ("cpa-netns","cpa-provider-sidecar-relay","provider-sidecar-tunnel","cpa-provider-sidecar-source","cpa-provider-sidecar-target"),
        ("provider-sidecar-tunnel","provider-sidecar-squid-relay","egress-proxy","provider-sidecar-squid-source","provider-sidecar-squid-target"),
    ]
members={network:{service for service,cfg in services.items() if network in (cfg.get("networks") or {})} for network in networks}
for source, relay, target, source_net, target_net in source_target_edges:
    if members.get(source_net) != {source, relay} or members.get(target_net) != {relay, target}:
        raise SystemExit(f"edge {source}->{relay}->{target} lacks two pairwise networks")
    if target in members[source_net] or source in members[target_net]:
        raise SystemExit(f"edge {source}->{target} has direct network reachability")
for owner, relay, target, source_net, target_net in (
    ("cpa-host-netns","cpa-host-relay","cpa-netns","cpa-host-source","host-cpa-target"),
    ("sub2api-host-netns","sub2api-host-relay","sub2api-netns","sub2api-host-source","host-sub2api-target"),
    ("apisix-host-netns","apisix-host-relay","apisix-netns","apisix-host-source","host-apisix-target"),
):
    if members.get(source_net) != {owner} or members.get(target_net) != {owner,target}:
        raise SystemExit(f"host edge {relay}->{target} network ownership mismatch")

for network, network_members in members.items():
    if network in {"proxy-egress","cloudflare-egress","cpa-host-source","sub2api-host-source","apisix-host-source"}:
        if len(network_members)!=1: raise SystemExit(f"{network} must have one namespace/egress owner")
    elif len(network_members)!=2:
        raise SystemExit(f"{network} must have exactly two Compose members, got {network_members}")
    cfg=networks[network] or {}
    if network.endswith("-source") and network in {"cpa-host-source","sub2api-host-source","apisix-host-source"}:
        if cfg.get("internal") is True or cfg.get("driver_opts"):
            raise SystemExit(f"{network} must remain engine-neutral host publication bridge")
    elif network not in {"proxy-egress","cloudflare-egress"} and cfg.get("internal") is not True:
        raise SystemExit(f"{network} must be internal")

commands = {
 "sub2api-host-relay": (None,8080,"172.30.4.2",8080),
 "apisix-host-relay": (None,9080,"172.30.6.2",9080),
 "cloudflared-apisix-relay": ("172.30.7.3",9080,"172.30.8.2",9080),
 "apisix-ai-sse-relay": ("172.30.9.3",8080,"172.30.10.2",8080),
 "ai-sse-sub2api-relay": ("172.30.25.3",8080,"172.30.26.2",8080),
 "sub2api-cpa-relay": ("172.30.11.3",8317,"172.30.12.2",8317),
 "sub2api-postgres-relay": ("172.30.13.3",5432,"172.30.14.2",5432),
 "sub2api-redis-relay": ("172.30.15.3",6379,"172.30.16.2",6379),
 "cpa-squid-relay": ("172.30.17.3",3128,"172.30.18.2",3128),
 "sub2api-squid-relay": ("172.30.19.3",3128,"172.30.20.2",3128),
}
if has_provider_sidecar:
 commands.update({"provider-sidecar-squid-relay":("172.30.23.3",3128,"172.30.24.2",3128)})
for relay,(bind,bport,target,tport) in commands.items():
    cfg=services[relay]
    listener = f"TCP4-LISTEN:{bport},reuseaddr,fork" if bind is None else f"TCP4-LISTEN:{bport},bind={bind},reuseaddr,fork"
    expected=[listener,f"TCP4:{target}:{tport},connect-timeout=10"]
    if normalized(cfg.get("command",[])) != expected:
        raise SystemExit(f"{relay} command mismatch")
    if str(cfg.get("user"))!="65534:65534" or cfg.get("read_only") is not True or set(cfg.get("cap_drop",[]))!={"ALL"} or cfg.get("cap_add"):
        raise SystemExit(f"{relay} hardening mismatch")
    if "no-new-privileges:true" not in cfg.get("security_opt",[]) or cfg.get("api") or cfg.get("metrics"):
        raise SystemExit(f"{relay} exposes extra control surface")
    require_relay_capacity(relay, cfg)

if has_provider_sidecar:
    relay = services["cpa-provider-sidecar-relay"]
    expected_tls_command = [
        "OPENSSL-LISTEN:8080,bind=172.30.21.3,reuseaddr,fork,cert=/run/provider-sidecar-tls/server.crt,key=/run/provider-sidecar-tls/server.key,verify=0,openssl-min-proto-version=TLS1.2",
        "TCP4:172.30.22.2:8080,connect-timeout=10",
    ]
    if normalized(relay.get("command", [])) != expected_tls_command:
        raise SystemExit("cpa-provider-sidecar-relay TLS command mismatch")
    if str(relay.get("user")) != "65534:65534" or relay.get("read_only") is not True or set(relay.get("cap_drop", [])) != {"ALL"} or relay.get("cap_add"):
        raise SystemExit("cpa-provider-sidecar-relay hardening mismatch")
    if "no-new-privileges:true" not in relay.get("security_opt", []) or relay.get("api") or relay.get("metrics"):
        raise SystemExit("cpa-provider-sidecar-relay exposes extra control surface")
    require_relay_capacity("cpa-provider-sidecar-relay", relay)

cpa_host = services["cpa-host-relay"]
cpa_host_command="\n".join(normalized(cpa_host.get("command",[])))
for port in (8317,8085,1455,54545,51121,11451):
    expected=f'TCP4-LISTEN:${{port}},reuseaddr,fork" "TCP4:172.30.2.2:${{port}},connect-timeout=10'
    if expected not in cpa_host_command:
        raise SystemExit(f"CPA host relay misses supervised port {port}")
if normalized(cpa_host.get("entrypoint",[])) != ["/bin/sh","-ec"] or "wait -n" not in cpa_host_command:
    raise SystemExit("CPA host relay must use the bounded shell supervisor")
if str(cpa_host.get("user"))!="65534:65534" or cpa_host.get("read_only") is not True or set(cpa_host.get("cap_drop",[]))!={"ALL"} or cpa_host.get("cap_add"):
    raise SystemExit("CPA host relay hardening mismatch")
if "no-new-privileges:true" not in cpa_host.get("security_opt",[]) or cpa_host.get("api") or cpa_host.get("metrics"):
    raise SystemExit("CPA host relay exposes extra control surface")
require_relay_capacity("cpa-host-relay", cpa_host)

expected_hosts = {
    "cpa-netns": {"cpa-egress-relay":"172.30.17.3"},
    "sub2api-netns": {
        "cli-proxy-api":"172.30.11.3", "postgres":"172.30.13.3",
        "redis":"172.30.15.3", "sub2api-egress-relay":"172.30.19.3",
    },
    "apisix-netns": {"ai-sse-keepalive-ingress-relay":"172.30.9.3"},
    "ai-sse-keepalive-proxy-netns": {"sub2api-ai-sse-relay":"172.30.25.3"},
    "cloudflared": {"apisix-ingress":"172.30.7.3"},
}
if has_provider_sidecar:
    expected_hosts["cpa-netns"]["provider-sidecar"] = "172.30.21.3"
for service, expected in expected_hosts.items():
    if extra_hosts(services[service]) != expected:
        raise SystemExit(f"{service} must use fixed relay addresses")

if services["sub2api"].get("environment",{}).get("SECURITY_URL_ALLOWLIST_ENABLED") not in (False,"false"):
    raise SystemExit("Sub2API URL allowlist must remain false")
if services["sub2api"].get("environment",{}).get("SERVER_TRUSTED_PROXIES") != "172.30.26.3/32":
    raise SystemExit("Sub2API must trust only AI SSE relay target address")
proxy = services["ai-sse-keepalive-proxy"]
expected_proxy_env = {
    "UPSTREAM_URL":"http://sub2api-ai-sse-relay:8080", "LISTEN_ADDR":":8080",
    "HEADER_WAIT":"2s", "IDLE_INTERVAL":"15s", "MAX_INSPECT_BODY_BYTES":"16777216",
    "REQUIRE_NO_DEFAULT_ROUTE":"true",
}
actual_proxy_env = proxy.get("environment", {})
for name, value in expected_proxy_env.items():
    if actual_proxy_env.get(name) != value:
        raise SystemExit(f"AI SSE keepalive proxy {name} mismatch")
if str(proxy.get("user")) != "65534:65534" or proxy.get("read_only") is not True or set(proxy.get("cap_drop", [])) != {"ALL"} or proxy.get("cap_add"):
    raise SystemExit("AI SSE keepalive proxy hardening mismatch")
if "no-new-privileges:true" not in proxy.get("security_opt", []) or proxy.get("volumes") or proxy.get("tmpfs") or proxy.get("api") or proxy.get("metrics"):
    raise SystemExit("AI SSE keepalive proxy exposes extra writable/control surface")
if proxy.get("entrypoint") or proxy.get("command"):
    raise SystemExit("AI SSE keepalive proxy must preserve its reviewed image entrypoint")
if int(proxy.get("pids_limit", 0)) != 128 or memory_bytes(proxy.get("mem_limit", 0)) != 64 * 1024**2 or memory_bytes(proxy.get("memswap_limit", 0)) != 64 * 1024**2 or float(proxy.get("cpus", 0)) != 0.5:
    raise SystemExit("AI SSE keepalive proxy resource limits mismatch")
if normalized(proxy.get("healthcheck", {}).get("test", [])) != ["CMD", "/ai-sse-keepalive-proxy", "healthcheck"]:
    raise SystemExit("AI SSE keepalive proxy healthcheck mismatch")
for service,proxy in (("cli-proxy-api","http://cpa-egress-relay:3128"),("sub2api","http://sub2api-egress-relay:3128")):
    env=services[service].get("environment",{})
    if env.get("HTTP_PROXY")!=proxy or env.get("HTTPS_PROXY")!=proxy:
        raise SystemExit(f"{service} must use its source-side Squid relay")
    if "/etc/ssl/certs/ai-gateway-ca-bundle.pem" not in volume_targets(services[service]):
        raise SystemExit(f"{service} must receive only public trust")
if has_provider_sidecar:
    cpa_mounts = services["cli-proxy-api"].get("volumes", [])
    trust_sources = [str(v.get("source")) if isinstance(v, dict) else str(v).split(":", 1)[0] for v in cpa_mounts if (v.get("target") if isinstance(v, dict) else (str(v).split(":")[1] if ":" in str(v) else "")) == "/etc/ssl/certs/ai-gateway-ca-bundle.pem"]
    if len(trust_sources) != 1 or not trust_sources[0].endswith("/data/provider-sidecar-tls/cpa-ca-bundle.pem"):
        raise SystemExit("CPA must override trust with the combined provider-sidecar bundle")
    relay_targets = volume_targets(services["cpa-provider-sidecar-relay"])
    if relay_targets != {"/run/provider-sidecar-tls/server.crt", "/run/provider-sidecar-tls/server.key"}:
        raise SystemExit("cpa-provider-sidecar-relay TLS mounts mismatch")
if "ai-sse-keepalive-ingress-relay:8080" not in Path("apisix/apisix.yaml").read_text():
    raise SystemExit("APISIX upstream must target its dedicated AI SSE relay")
if sys.argv[3]=="1":
    squid=Path("data/egress-proxy/generated/squid.conf").read_text()
    required={"172.30.18.2:3128":"cpa","172.30.20.2:3128":"sub2api","172.30.24.2:3128":"provider-sidecar" if has_provider_sidecar else None}
    for address,name in required.items():
        present=f"http_port {address} name={name}" in squid if name else address in squid
        if (name is not None) != present: raise SystemExit(f"Squid listener parity mismatch: {address}")

if has_provider_sidecar:
    tunnel=services["provider-sidecar-tunnel"]
    sidecar=services["provider-sidecar"]
    require_image("provider-sidecar-tunnel","ghcr.io/tun2proxy/tun2proxy")
    if not re.fullmatch(r"[^@\s]+@sha256:[0-9a-f]{64}", image(sidecar)):
        raise SystemExit("provider-sidecar image must be pinned by manifest digest")
    if sidecar.get("network_mode")!="service:provider-sidecar-tunnel":
        raise SystemExit("provider-sidecar must share tun2proxy namespace")
    if str(sidecar.get("user", "")).lower() in {"", "0", "0:0", "root", "root:root"}:
        raise SystemExit("provider-sidecar must declare a non-root user")
    if sidecar.get("read_only") is not True or set(sidecar.get("cap_drop",[]))!={"ALL"} or sidecar.get("cap_add") or sidecar.get("privileged") is True:
        raise SystemExit("provider-sidecar hardening mismatch")
    if set(tunnel.get("cap_add",[]))!={"NET_ADMIN"} or set(tunnel.get("cap_drop",[]))!={"ALL"} or tunnel.get("privileged") is True:
        raise SystemExit("tun2proxy must have exact NET_ADMIN, not privileged")
    if tunnel.get("read_only") is True:
        raise SystemExit("tun2proxy needs an ephemeral writable layer for resolver setup")
    if str(tunnel.get("restart")) != "no":
        raise SystemExit("tun2proxy must stay stopped after failure until its shared namespace is rebuilt")
    if normalized(tunnel.get("command",[])) != ["--proxy","http://172.30.23.3:3128","--dns","virtual","--bypass","172.30.23.3/32","--tcp-timeout","600","--exit-on-fatal-error","--verbosity","info"]:
        raise SystemExit("tun2proxy command mismatch")
    if "/etc/resolv.conf" in volume_targets(tunnel):
        raise SystemExit("tun2proxy must use Docker's restart-safe writable resolver file")
    if sidecar.get("ports") or tunnel.get("ports"):
        raise SystemExit("provider-sidecar must not publish ports")

key_holders={service for service,cfg in services.items() if "/etc/squid/ca.key" in volume_targets(cfg)}
if key_holders!={"egress-proxy"}: raise SystemExit(f"private egress CA key holders: {key_holders}")
provider_sidecar_leaf_key_holders = set()
for service, cfg in services.items():
    for volume in cfg.get("volumes", []):
        source = str(volume.get("source", "")) if isinstance(volume, dict) else str(volume).split(":", 1)[0]
        target = str(volume.get("target", "")) if isinstance(volume, dict) else (str(volume).split(":")[1] if ":" in str(volume) else "")
        if source.endswith("/data/provider-sidecar-tls/server.key"):
            provider_sidecar_leaf_key_holders.add((service, target))
expected_leaf_key_holders = {("cpa-provider-sidecar-relay", "/run/provider-sidecar-tls/server.key")} if has_provider_sidecar else set()
if provider_sidecar_leaf_key_holders != expected_leaf_key_holders:
    raise SystemExit(f"provider-sidecar leaf key source mounts: {sorted(provider_sidecar_leaf_key_holders)}")
for service,cfg in services.items():
    for volume in cfg.get("volumes", []):
        source = str(volume.get("source", "")) if isinstance(volume, dict) else str(volume).split(":", 1)[0]
        if source.endswith("/data/provider-sidecar-tls/ca.key"):
            raise SystemExit(f"dedicated provider-sidecar CA private key mounted by {service}")
PY
ai_sse_image=ai-sse-keepalive-proxy:latest
BUILDAH_FORMAT=docker docker compose "${compose_args[@]}" --env-file "$env_file" build ai-sse-keepalive-proxy >/dev/null
proxy_inspect=$(docker image inspect "$ai_sse_image")
python3 - "$proxy_inspect" <<'PY'
import json
import sys

image = json.loads(sys.argv[1])[0]
config = image["Config"]
if config.get("User") != "65534:65534":
    raise SystemExit("built AI SSE keepalive proxy image user mismatch")
if config.get("Entrypoint") != ["/ai-sse-keepalive-proxy"]:
    raise SystemExit("built AI SSE keepalive proxy entrypoint mismatch")
health = config.get("Healthcheck") or image.get("Healthcheck") or {}
if health.get("Test") != ["CMD", "/ai-sse-keepalive-proxy", "healthcheck"]:
    raise SystemExit("built AI SSE keepalive proxy healthcheck mismatch")
PY
egress_image=$(value_from_env EGRESS_PROXY_IMAGE)
[ -n "$egress_image" ] || egress_image=ai-gateway-squid:6.13-2-deb13u2
docker build --quiet --tag "$egress_image" egress-proxy >/dev/null
docker run --rm --entrypoint /usr/sbin/squid "$egress_image" -v 2>&1 | grep -q -- '--with-openssl' || {
  echo 'egress proxy image lacks OpenSSL support' >&2
  exit 1
}
printf 'api.example api.example\n- api.example\napi.example other.example\n' \
  | docker run --rm -i --entrypoint /usr/local/bin/ai-gateway-exact-host-helper "$egress_image" \
  >"$tmpdir/helper-output"
[ "$(cat "$tmpdir/helper-output")" = $'OK\nERR message=name-mismatch\nERR message=name-mismatch' ] || {
  echo 'egress exact-host helper failed' >&2
  exit 1
}
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=validation \
  -addext 'basicConstraints=critical,CA:TRUE' \
  -addext 'keyUsage=critical,keyCertSign,cRLSign' \
  -keyout "$tmpdir/ca.key" -out "$tmpdir/ca.crt" >/dev/null 2>&1
chmod 644 "$tmpdir/ca.key" "$tmpdir/ca.crt"
if ! squid_output=$(docker run --rm --entrypoint /usr/sbin/squid \
  -v "$tmpdir/proxy-test:/etc/squid/generated:ro" \
  -v "$tmpdir/ca.crt:/etc/squid/ca.crt:ro" \
  -v "$tmpdir/ca.key:/etc/squid/ca.key:ro" \
  "$egress_image" -k parse -f /etc/squid/generated/squid.conf 2>&1); then
  printf '%s\n' "$squid_output" >&2
  exit 1
fi
if grep -Eq 'FATAL|ERROR|WARNING' <<<"$squid_output"; then
  printf '%s\n' "$squid_output" | grep -E 'FATAL|ERROR|WARNING' >&2
  exit 1
fi
if [ "$runtime_mode" = 1 ]; then
  if ! squid_output=$(docker run --rm --entrypoint /usr/sbin/squid \
    -v "$tmpdir/proxy-runtime:/etc/squid/generated:ro" \
    -v "$root/data/egress-proxy/ca.crt:/etc/squid/ca.crt:ro" \
    -v "$root/data/egress-proxy/ca.key:/etc/squid/ca.key:ro" \
    "$egress_image" -k parse -f /etc/squid/generated/squid.conf 2>&1); then
    printf '%s\n' "$squid_output" >&2
    exit 1
  fi
  if grep -Eq 'FATAL|ERROR|WARNING' <<<"$squid_output"; then
    printf '%s\n' "$squid_output" | grep -E 'FATAL|ERROR|WARNING' >&2
    exit 1
  fi
fi
tun2proxy_image=$(value_from_env TUN2PROXY_IMAGE)
[ -n "$tun2proxy_image" ] || tun2proxy_image=ghcr.io/tun2proxy/tun2proxy@sha256:562a4208ecf1f53e3c790af512bcc1ce2656f1d10d3541614173eed8b3185708
[[ "$tun2proxy_image" =~ ^ghcr\.io/tun2proxy/tun2proxy@sha256:[0-9a-f]{64}$ ]] || {
  echo 'TUN2PROXY_IMAGE must use the official repository pinned by digest' >&2
  exit 1
}
"$root/scripts/test-egress-proxy.sh" "$egress_image" "$tun2proxy_image"
socat_image=$(value_from_env SOCAT_IMAGE)
[ -n "$socat_image" ] || socat_image=docker.io/alpine/socat@sha256:3d9e7966201dd3a065df591020a09fd3c70845de7e7086e3531ea69db774406b
[[ "$socat_image" =~ ^docker\.io/alpine/socat@sha256:[0-9a-f]{64}$ ]] || {
  echo 'SOCAT_IMAGE must use the official repository pinned by digest' >&2
  exit 1
}
"$root/scripts/test-socat-boundary.sh" "$socat_image"
"$root/scripts/test-provider-sidecar-tls-boundary.sh" "$socat_image"

netns_guard_image=$(value_from_env NETNS_GUARD_IMAGE)
[ -n "$netns_guard_image" ] || netns_guard_image=docker.io/library/alpine@sha256:4bcff63911fcb4448bd4fdacec207030997caf25e9bea4045fa6c8c44de311d1
[[ "$netns_guard_image" =~ ^docker\.io/library/alpine@sha256:[0-9a-f]{64}$ ]] || {
  echo 'NETNS_GUARD_IMAGE must use the official repository pinned by digest' >&2
  exit 1
}
"$root/scripts/test-netns-guard.sh" "$netns_guard_image"

apisix_image=$(value_from_env APISIX_IMAGE)
[ -n "$apisix_image" ] || apisix_image=docker.io/apache/apisix:3.18.0-debian
docker run --rm \
  -e APISIX_STAND_ALONE=true \
  -v "$root/apisix/config.yaml:/usr/local/apisix/conf/config.yaml:ro" \
  -v "$root/apisix/apisix.yaml:/usr/local/apisix/conf/apisix.yaml:ro" \
  -v "$root/apisix/lua:/opt/apisix/custom:ro" \
  "$apisix_image" apisix test >/dev/null

if command -v shellcheck >/dev/null; then shellcheck scripts/*.sh egress-proxy/*.sh; fi
echo 'compose=valid pairwise_networks=valid ai_sse_keepalive_proxy=valid egress_proxy=valid provider_sidecar_tls=valid apisix=valid private_paths=untracked'
