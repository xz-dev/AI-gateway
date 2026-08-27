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
    data/egress-proxy/generated/squid.conf data/egress-proxy/virtual-resolv.conf \
    data/egress-proxy/tunnel-resolv.conf; do
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
  [ "$(grep -Ec '^nameserver[[:space:]]+10\.0\.0\.1$' data/egress-proxy/tunnel-resolv.conf)" = 1 ] || {
    echo 'tunnel-resolv.conf must use the TUN setup resolver' >&2
    exit 1
  }
  for resolv in data/egress-proxy/virtual-resolv.conf data/egress-proxy/tunnel-resolv.conf; do
    [ "$(grep -Ec '^nameserver[[:space:]]+' "$resolv")" = 1 ] || {
      echo "$resolv must contain exactly one nameserver" >&2
      exit 1
    }
  done
  [ "$(stat -c %a data/egress-proxy/virtual-resolv.conf)" = 444 ] || {
    echo 'virtual-resolv.conf must have mode 444' >&2
    exit 1
  }
  [ "$(stat -c %a data/egress-proxy/tunnel-resolv.conf)" = 600 ] || {
    echo 'tunnel-resolv.conf must have mode 600' >&2
    exit 1
  }
  [ "$(stat -c %u data/egress-proxy/tunnel-resolv.conf)" = "$(id -u)" ] || {
    echo 'tunnel-resolv.conf must be owned by the deployment user' >&2
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

cpa_config=cpa/config.example.yaml
if [ "$env_file" = .env ] || [ "$env_file" = "$root/.env" ]; then cpa_config=data/cpa/conf/config.yaml; fi
grep -Eq '^proxy-url:[[:space:]]*"?http://172\.23\.0\.3:3128"?[[:space:]]*$' "$cpa_config" || {
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
if [ -f "$root/compose.override.yaml" ]; then
  compose_args+=(-f "$root/compose.override.yaml")
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
def volume_targets(config):
    targets = set()
    for volume in config.get("volumes", []):
        if isinstance(volume, dict):
            targets.add(volume.get("target"))
        elif isinstance(volume, str):
            parts = volume.split(":")
            if len(parts) >= 2:
                targets.add(parts[1])
    return targets

def extra_hosts(config):
    result = {}
    value = config.get("extra_hosts", [])
    if isinstance(value, dict):
        return {str(name): str(address) for name, address in value.items()}
    for item in value:
        separator = "=" if "=" in str(item) else ":"
        name, address = str(item).split(separator, 1)
        result[name] = address
    return result

def require_pinned_image(config, repository):
    image = config.get("image", "")
    marker = "@sha256:"
    if not image.startswith(repository + marker):
        raise SystemExit(f"{repository} image must use its official repository and a digest")
    digest = image.split(marker, 1)[1]
    if len(digest) != 64 or any(character not in "0123456789abcdef" for character in digest):
        raise SystemExit(f"invalid pinned image digest for {repository}")

def require_dependency(config, dependency, condition, restart=None):
    settings = config.get("depends_on", {}).get(dependency, {})
    if settings.get("condition") != condition:
        raise SystemExit(f"dependency {dependency} must use {condition}")
    if restart is not None and settings.get("restart") is not restart:
        raise SystemExit(f"dependency {dependency} restart must be {restart}")

expected = {
    "egress-proxy": {"cpa-egress", "sub2api-egress", "proxy-egress"},
    "cpa-netns": {"cpa-sub2api", "cpa-egress", "cpa-host"},
    "cli-proxy-api": set(),
    "postgres": {"postgres-sub2api"},
    "redis": {"redis-sub2api"},
    "sub2api-netns": {"cpa-sub2api", "postgres-sub2api", "redis-sub2api", "sub2api-egress", "sub2api-apisix", "sub2api-host"},
    "sub2api": set(),
    "apisix-netns": {"sub2api-apisix", "apisix-cloudflared", "apisix-host"},
    "apisix": set(),
    "cloudflared": {"apisix-cloudflared", "cloudflare-egress"},
}
has_zcode = "zcode-proxy" in services
has_zcode_tunnel = "zcode-egress-tunnel" in services
if has_zcode != has_zcode_tunnel:
    raise SystemExit("zcode-proxy and zcode-egress-tunnel must be enabled together")
if sys.argv[3] == "1":
    squid_lines = Path("data/egress-proxy/generated/squid.conf").read_text().splitlines()
    required_listeners = {
        "http_port 172.23.0.3:3128 name=cpa": True,
        "http_port 172.24.0.3:3128 name=sub2api": True,
        "http_port 172.25.0.3:3128 name=zcode": has_zcode,
    }
    for prefix, required in required_listeners.items():
        present = any(line.startswith(prefix + " ") for line in squid_lines)
        if present != required:
            raise SystemExit(f"runtime listener parity mismatch for {prefix}")
if has_zcode:
    expected["egress-proxy"].add("zcode-egress")
    expected["cpa-netns"].add("zcode-cpa")
    expected["zcode-egress-tunnel"] = {"zcode-cpa", "zcode-egress"}
    expected["zcode-proxy"] = set()
if set(services) != set(expected):
    raise SystemExit(f"unexpected services: {sorted(set(services) ^ set(expected))}")
for service, repository in {
    "cli-proxy-api": "ghcr.io/xz-dev/cli-proxy-api",
    "sub2api": "ghcr.io/wei-shaw/sub2api",
    "apisix": "docker.io/apache/apisix",
    "postgres": "docker.io/library/postgres",
    "redis": "docker.io/library/redis",
    "cloudflared": "docker.io/cloudflare/cloudflared",
}.items():
    require_pinned_image(services[service], repository)
for service in ("cpa-netns", "sub2api-netns", "apisix-netns"):
    require_pinned_image(services[service], "docker.io/library/alpine")
for service, networks in expected.items():
    actual = set(services[service].get("networks") or {})
    if actual != networks:
        raise SystemExit(f"{service} networks {sorted(actual)} != {sorted(networks)}")

members = {
    network: {service for service, config in services.items() if network in config.get("networks", {})}
    for network in compose["networks"]
}
expected_members = {
    "cpa-sub2api": {"cpa-netns", "sub2api-netns"},
    "postgres-sub2api": {"postgres", "sub2api-netns"},
    "redis-sub2api": {"redis", "sub2api-netns"},
    "sub2api-apisix": {"sub2api-netns", "apisix-netns"},
    "apisix-cloudflared": {"apisix-netns", "cloudflared"},
    "cpa-egress": {"cpa-netns", "egress-proxy"},
    "sub2api-egress": {"sub2api-netns", "egress-proxy"},
    "cpa-host": {"cpa-netns"},
    "sub2api-host": {"sub2api-netns"},
    "apisix-host": {"apisix-netns"},
    "proxy-egress": {"egress-proxy"},
    "cloudflare-egress": {"cloudflared"},
}
if has_zcode:
    expected_members["zcode-cpa"] = {"zcode-egress-tunnel", "cpa-netns"}
    expected_members["zcode-egress"] = {"zcode-egress-tunnel", "egress-proxy"}
if members != expected_members:
    raise SystemExit(f"network membership mismatch: {members!r}")
if "default" in compose["networks"]:
    raise SystemExit("implicit default network is forbidden")
if compose.get("volumes"):
    raise SystemExit("namespace route guards must not require persistent volumes")
internal_networks = ["cpa-sub2api", "postgres-sub2api", "redis-sub2api", "sub2api-apisix", "apisix-cloudflared", "cpa-egress", "sub2api-egress"]
if has_zcode:
    internal_networks += ["zcode-cpa", "zcode-egress"]
for network in internal_networks:
    if compose["networks"][network].get("internal") is not True:
        raise SystemExit(f"{network} must be internal")
for network in ("cpa-host", "sub2api-host", "apisix-host"):
    config = compose["networks"][network] or {}
    if config.get("internal") is True or config.get("driver_opts"):
        raise SystemExit(f"{network} must be an engine-neutral port-publication bridge")
addresses = [
    ("egress-proxy", "cpa-egress", "172.23.0.3"),
    ("cpa-netns", "cpa-egress", "172.23.0.2"),
    ("cpa-netns", "cpa-sub2api", "172.27.0.2"),
    ("sub2api-netns", "cpa-sub2api", "172.27.0.3"),
    ("postgres", "postgres-sub2api", "172.28.0.2"),
    ("sub2api-netns", "postgres-sub2api", "172.28.0.3"),
    ("redis", "redis-sub2api", "172.29.1.2"),
    ("sub2api-netns", "redis-sub2api", "172.29.1.3"),
    ("egress-proxy", "sub2api-egress", "172.24.0.3"),
    ("sub2api-netns", "sub2api-egress", "172.24.0.2"),
    ("sub2api-netns", "sub2api-apisix", "172.22.0.2"),
    ("apisix-netns", "sub2api-apisix", "172.22.0.3"),
    ("apisix-netns", "apisix-cloudflared", "172.21.0.3"),
    ("cloudflared", "apisix-cloudflared", "172.21.0.2"),
]
if has_zcode:
    addresses += [
        ("egress-proxy", "zcode-egress", "172.25.0.3"),
        ("zcode-egress-tunnel", "zcode-egress", "172.25.0.2"),
        ("zcode-egress-tunnel", "zcode-cpa", "172.26.0.2"),
        ("cpa-netns", "zcode-cpa", "172.26.0.3"),
    ]
for service, network, address in addresses:
    actual = services[service]["networks"][network].get("ipv4_address")
    if actual != address:
        raise SystemExit(f"{service}/{network} address {actual!r} != {address!r}")

expected_owner_command = [
    "/bin/sh",
    "-ec",
    "while ip -4 route show default | grep -q .; do ip -4 route del default; done; "
    "while ip -6 route show default | grep -q .; do ip -6 route del default; done; "
    'test -z "$(ip -4 route show default)"; test -z "$(ip -6 route show default)"; '
    "exec su nobody -s /bin/sh -c 'exec sleep infinity'",
]
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
def normalized_command(parts):
    # Docker Compose preserves escaped dollars in `config --format json`,
    # while podman-compose emits the runtime single-dollar form.
    return [str(part).replace("$$", "$") for part in parts]

for owner_name, app_name in (
    ("cpa-netns", "cli-proxy-api"),
    ("sub2api-netns", "sub2api"),
    ("apisix-netns", "apisix"),
):
    owner = services[owner_name]
    app = services[app_name]
    if owner.get("read_only") is not True or set(owner.get("cap_drop", [])) != {"ALL"}:
        raise SystemExit(f"{owner_name} must be a read-only namespace owner")
    if set(owner.get("cap_add", [])) != {"NET_ADMIN", "SETGID", "SETUID"}:
        raise SystemExit(f"{owner_name} must receive only route setup and privilege-drop capabilities")
    if owner.get("privileged") is True or "no-new-privileges:true" not in owner.get("security_opt", []):
        raise SystemExit(f"{owner_name} must be unprivileged with no-new-privileges")
    if normalized_command(owner.get("command", [])) != expected_owner_command:
        raise SystemExit(f"{owner_name} must remove default routes and drop privilege")
    if not owner.get("ports") or app.get("ports"):
        raise SystemExit(f"{owner_name} alone must own {app_name} host publications")
    if app.get("network_mode") != f"service:{owner_name}":
        raise SystemExit(f"{app_name} must share {owner_name} network namespace")
    entrypoint, command = app_commands[app_name]
    if normalized_command(app.get("entrypoint", [])) != entrypoint:
        raise SystemExit(f"{app_name} must wait for namespace hardening before startup")
    if normalized_command(app.get("command", [])) != [command]:
        raise SystemExit(f"{app_name} startup command must preserve its upstream entrypoint after the guard")
    require_dependency(app, owner_name, "service_started", True)

for service, config in services.items():
    for dependency, settings in config.get("depends_on", {}).items():
        if settings.get("condition") == "service_healthy":
            raise SystemExit(f"{service} dependency {dependency} uses non-portable service_healthy")

for service, network, alias in (
    ("cpa-netns", "cpa-sub2api", "cli-proxy-api"),
    ("sub2api-netns", "cpa-sub2api", "sub2api"),
    ("sub2api-netns", "sub2api-apisix", "sub2api-apisix"),
    ("apisix-netns", "apisix-cloudflared", "apisix-ingress"),
):
    if alias not in set(services[service]["networks"][network].get("aliases", [])):
        raise SystemExit(f"{service}/{network} must provide alias {alias}")

expected_hosts = {
    "cpa-netns": {"sub2api": "172.27.0.3"},
    "sub2api-netns": {"cli-proxy-api": "172.27.0.2", "postgres": "172.28.0.2", "redis": "172.29.1.2"},
    "apisix-netns": {"sub2api-apisix": "172.22.0.2"},
}
if has_zcode:
    expected_hosts["cpa-netns"]["zcode-proxy"] = "172.26.0.2"
for service, hosts in expected_hosts.items():
    if extra_hosts(services[service]) != hosts:
        raise SystemExit(f"{service} must use fixed pairwise service addresses")

if str(services["sub2api"]["environment"].get("SECURITY_URL_ALLOWLIST_ENABLED", "")).lower() != "false":
    raise SystemExit("Sub2API URL allowlist must remain disabled")
proxied_services = [("cli-proxy-api", "http://172.23.0.3:3128"), ("sub2api", "http://172.24.0.3:3128")]
for service, proxy in proxied_services:
    environment = services[service]["environment"]
    if environment.get("HTTP_PROXY") != proxy or environment.get("HTTPS_PROXY") != proxy:
        raise SystemExit(f"{service} must use its pairwise egress proxy")
    if environment.get("SSL_CERT_FILE") != "/etc/ssl/certs/ai-gateway-ca-bundle.pem":
        raise SystemExit(f"{service} must trust the inspection CA bundle")
    targets = volume_targets(services[service])
    if "/etc/ssl/certs/ai-gateway-ca-bundle.pem" not in targets:
        raise SystemExit(f"{service} must mount the public inspection CA bundle")

if has_zcode:
    zcode = services["zcode-proxy"]
    tunnel = services["zcode-egress-tunnel"]

    require_pinned_image(zcode, "ghcr.io/tridefender/zcode-proxy")
    require_pinned_image(tunnel, "ghcr.io/tun2proxy/tun2proxy")
    if zcode.get("network_mode") != "service:zcode-egress-tunnel":
        raise SystemExit("zcode-proxy must share the namespace-wide egress tunnel")
    require_dependency(zcode, "zcode-egress-tunnel", "service_started", True)
    require_dependency(tunnel, "egress-proxy", "service_started", True)
    require_dependency(services["cli-proxy-api"], "zcode-proxy", "service_started", True)
    aliases = set(tunnel["networks"]["zcode-cpa"].get("aliases", []))
    if "zcode-proxy" not in aliases:
        raise SystemExit("zcode-cpa must alias the shared namespace as zcode-proxy")

    environment = zcode.get("environment", {})
    for name in ("HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"):
        if environment.get(name):
            raise SystemExit("zcode-proxy must rely on namespace egress, not application proxy variables")
    if environment.get("SSL_CERT_FILE") != "/etc/ssl/certs/ai-gateway-ca-bundle.pem":
        raise SystemExit("zcode-proxy must trust the public inspection CA bundle")
    if environment.get("NODE_EXTRA_CA_CERTS") != "/etc/ssl/certs/ai-gateway-ca-bundle.pem":
        raise SystemExit("zcode-proxy must trust the inspection CA in Node/Bun TLS")
    targets = volume_targets(zcode)
    for target in ("/etc/ssl/certs/ai-gateway-ca-bundle.pem", "/etc/resolv.conf"):
        if target not in targets:
            raise SystemExit(f"zcode-proxy must mount {target}")
    healthcheck = " ".join(str(part) for part in zcode.get("healthcheck", {}).get("test", []))
    if "198.18." not in healthcheck:
        raise SystemExit("zcode-proxy healthcheck must verify virtual DNS through the namespace tunnel")

    if tunnel.get("read_only") is not True:
        raise SystemExit("zcode-egress-tunnel root filesystem must be read-only")
    if "/etc/resolv.conf" not in volume_targets(tunnel):
        raise SystemExit("zcode-egress-tunnel must receive a writable private resolv.conf")
    tmpfs = tunnel.get("tmpfs", [])
    if not any(str(mount).split(":", 1)[0] == "/tmp" for mount in tmpfs):
        raise SystemExit("zcode-egress-tunnel must use a writable /tmp tmpfs")
    if set(tunnel.get("cap_add", [])) != {"NET_ADMIN"} or set(tunnel.get("cap_drop", [])) != {"ALL"}:
        raise SystemExit("zcode-egress-tunnel must have only NET_ADMIN after dropping all capabilities")
    if tunnel.get("privileged") is True:
        raise SystemExit("zcode-egress-tunnel must not be privileged")
    if "no-new-privileges:true" not in tunnel.get("security_opt", []):
        raise SystemExit("zcode-egress-tunnel must enable no-new-privileges")
    devices = set()
    for device in tunnel.get("devices", []):
        if isinstance(device, dict):
            devices.add((device.get("source"), device.get("target")))
        elif isinstance(device, str):
            parts = device.split(":")
            if len(parts) >= 2:
                devices.add((parts[0], parts[1]))
    if devices != {("/dev/net/tun", "/dev/net/tun")}:
        raise SystemExit("zcode-egress-tunnel must receive only the TUN device")
    sysctls = tunnel.get("sysctls", {})
    for name in ("net.ipv6.conf.all.disable_ipv6", "net.ipv6.conf.default.disable_ipv6"):
        if str(sysctls.get(name)) != "1":
            raise SystemExit("zcode-egress-tunnel must disable IPv6 to prevent route bypass")
    command = [str(part) for part in tunnel.get("command", [])]
    expected_command = [
        "--proxy", "http://172.25.0.3:3128",
        "--dns", "virtual",
        "--bypass", "172.25.0.3/32",
        "--tcp-timeout", "600",
        "--exit-on-fatal-error",
        "--verbosity", "info",
    ]
    if command != expected_command:
        raise SystemExit("zcode-egress-tunnel command must match the validated fail-closed profile")
    if tunnel.get("restart") != "unless-stopped" or zcode.get("restart") != "unless-stopped":
        raise SystemExit("zcode namespace services must restart unless stopped")
    if zcode.get("read_only") is not True:
        raise SystemExit("zcode-proxy root filesystem must be read-only")
    if set(zcode.get("cap_drop", [])) != {"ALL"} or zcode.get("cap_add"):
        raise SystemExit("zcode-proxy must run without Linux capabilities")
    if zcode.get("privileged") is True or "no-new-privileges:true" not in zcode.get("security_opt", []):
        raise SystemExit("zcode-proxy must be unprivileged with no-new-privileges")
    if zcode.get("ports") or tunnel.get("ports"):
        raise SystemExit("ZCode namespace services must not publish host ports")

key_holders = {
    service
    for service, config in services.items()
    if "/etc/squid/ca.key" in volume_targets(config)
}
if key_holders != {"egress-proxy"}:
    raise SystemExit(f"inspection CA key holders must be only egress-proxy, got {sorted(key_holders)}")
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
zcode_image=$(value_from_env ZCODE_PROXY_IMAGE)
[ -n "$zcode_image" ] || zcode_image=ghcr.io/tridefender/zcode-proxy@sha256:947dcf83314e16a87b61191c4847bf3d4f10baf4c370c25191d5a49f08a06e52
[[ "$zcode_image" =~ ^ghcr\.io/tridefender/zcode-proxy@sha256:[0-9a-f]{64}$ ]] || {
  echo 'ZCODE_PROXY_IMAGE must use the official repository pinned by digest' >&2
  exit 1
}
"$root/scripts/test-egress-proxy.sh" "$egress_image" "$tun2proxy_image" "$zcode_image"
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
echo 'compose=valid pairwise_networks=valid egress_proxy=valid apisix=valid private_paths=untracked'
