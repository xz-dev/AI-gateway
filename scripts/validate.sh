#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=scripts/container-runtime.sh
source "$root/scripts/container-runtime.sh"
RUNTIME=("$AI_GATEWAY_RUNTIME")
COMPOSE=("${AI_GATEWAY_COMPOSE[@]}")
cd "$root"

env_file=${1:-}
if [ -z "$env_file" ]; then
  if [ -f .env ]; then env_file=.env; else env_file=.env.example; fi
fi
[ -f "$env_file" ] || { echo "missing $env_file" >&2; exit 1; }
for command in openssl python3; do
  command -v "$command" >/dev/null || { echo "$command is required" >&2; exit 1; }
done

value_from_env() {
  local key=$1
  awk -F= -v key="$key" '$1 == key {sub(/^[^=]*=/, ""); print; exit}' "$env_file"
}

runtime_mode=0
if [ "$env_file" = .env ] || [ "$env_file" = "$root/.env" ]; then
  runtime_mode=1
  ! grep -Eq '^[A-Z0-9_]+=replace-with-' "$env_file" || { echo '.env still contains placeholders' >&2; exit 1; }
  [ "$(value_from_env ADMIN_EMAIL)" != admin@example.invalid ] || { echo 'set ADMIN_EMAIL in .env' >&2; exit 1; }
  for path in data/cpa/conf/config.yaml data/cpa/mgmt.key \
    data/egress-proxy/ca.key data/egress-proxy/ca.crt \
    data/egress-proxy/ca-bundle.pem data/egress-proxy/policy.json \
    data/egress-proxy/generated/squid.conf data/egress-proxy/virtual-resolv.conf; do
    [ -f "$path" ] || { echo "missing runtime file: $path" >&2; exit 1; }
  done
  [ "$(stat -c %a data/egress-proxy)" = 700 ] || { echo 'data/egress-proxy must have mode 700' >&2; exit 1; }
  [ "$(stat -c %a data/egress-proxy/generated)" = 755 ] || { echo 'generated egress config must have mode 755' >&2; exit 1; }
  key_id=$(openssl pkey -in data/egress-proxy/ca.key -pubout -outform DER 2>/dev/null | openssl dgst -sha256)
  cert_id=$(openssl x509 -in data/egress-proxy/ca.crt -pubkey -noout 2>/dev/null | openssl pkey -pubin -outform DER 2>/dev/null | openssl dgst -sha256)
  [ "$key_id" = "$cert_id" ] || { echo 'egress CA key and certificate do not match' >&2; exit 1; }
  openssl x509 -in data/egress-proxy/ca.crt -noout -checkend 2592000 >/dev/null || { echo 'egress CA expires within 30 days' >&2; exit 1; }
  [ "$(stat -c %a data/sub2api/postgres)" = 1777 ] || { echo 'data/sub2api/postgres must have mode 1777' >&2; exit 1; }
fi

for path in .env .pi compose.override.yaml cpa/config.yaml secrets/cloudflare-tunnel-token data; do
  if git ls-files --error-unmatch "$path" >/dev/null 2>&1; then
    echo "private runtime path is tracked: $path" >&2
    exit 1
  fi
done

cpa_config=cpa/config.example.yaml
[ "$runtime_mode" = 0 ] || cpa_config=data/cpa/conf/config.yaml
grep -Eq '^proxy-url:[[:space:]]*"?http://cpa-egress-relay:3128"?[[:space:]]*$' "$cpa_config" || {
  echo "$cpa_config must force global provider traffic through cpa-egress-relay" >&2
  exit 1
}

tmpdir=$(mktemp -d /tmp/ai-gateway-validate.XXXXXX)
trap 'rm -rf "$tmpdir"' EXIT
python3 scripts/render-egress-policy.py egress-proxy/policy.example.json "$tmpdir/proxy-default"
python3 - egress-proxy/policy.example.json <<'PY'
import copy
import json
import sys


def entry(domain, *paths):
    return (domain, "bump", ("GET",), tuple(sorted(paths)))


expected = {
    "cpa": sorted((
        entry(
            "api.github.com",
            r"^/repos/router-for-me/CLIProxyAPI/releases/latest$",
            r"^/repos/router-for-me/Cli-Proxy-API-Management-Center/releases/latest$",
        ),
        entry(
            "github.com",
            r"^/router-for-me/Cli-Proxy-API-Management-Center/releases/download/[^/]+/management\.html$",
        ),
        entry(
            "release-assets.githubusercontent.com",
            r"^/github-production-release-asset/1051566067/",
        ),
        entry(
            "raw.githubusercontent.com",
            r"^/router-for-me/models/refs/heads/main/models\.json$",
            r"^/router-for-me/models/refs/heads/main/codex_client_models\.json$",
        ),
    )),
    "sub2api": sorted((
        entry(
            "api.github.com",
            r"^/repos/Wei-Shaw/sub2api/releases/latest$",
            r"^/repos/openai/codex/releases/latest$",
            r"^/repos/openai/codex/releases\?per_page=30$",
        ),
        entry(
            "raw.githubusercontent.com",
            r"^/Wei-Shaw/model-price-repo/main/model_prices_and_context_window\.json$",
            r"^/Wei-Shaw/model-price-repo/main/model_prices_and_context_window\.sha256$",
        ),
    )),
}


def shape(policy):
    services = policy.get("services") if isinstance(policy, dict) else None
    if not isinstance(services, dict):
        return None
    return {
        name: sorted(
            (
                item.get("domain"),
                item.get("tls"),
                tuple(sorted(item.get("methods", []))),
                tuple(sorted(item.get("paths", []))),
            )
            for item in spec.get("destinations", [])
        )
        for name, spec in services.items()
    }


def check(policy):
    actual = shape(policy)
    if actual != expected:
        raise ValueError(
            "default egress policy differs from the exact control-plane baseline\n"
            f"expected={expected!r}\nactual={actual!r}"
        )


with open(sys.argv[1], encoding="utf-8") as handle:
    policy = json.load(handle)
check(policy)

# Prove the exact-shape check rejects the unsafe wildcard this baseline replaces.
broadened = copy.deepcopy(policy)
next(
    item
    for item in broadened["services"]["cpa"]["destinations"]
    if item["domain"] == "github.com"
)["paths"] = [r"^/[^/]+/[^/]+/releases/download/"]
try:
    check(broadened)
except ValueError:
    pass
else:
    raise SystemExit("default policy validation accepted a broad GitHub release path")
PY
python3 scripts/render-egress-policy.py egress-proxy/testdata/policy.json "$tmpdir/proxy-test"
python3 - "$tmpdir/proxy-test" <<'PY'
import ipaddress
import sys
from pathlib import Path

root = Path(sys.argv[1])
ipv4 = tuple(ipaddress.ip_network(line) for line in (root / "policy-blocked-ipv4-cidrs").read_text().splitlines())
ipv6 = tuple(ipaddress.ip_network(line) for line in (root / "policy-blocked-ipv6-cidrs").read_text().splitlines())
for text in ("10.0.0.1", "100.64.0.1", "127.0.0.1", "169.254.169.254", "192.168.0.1"):
    address = ipaddress.ip_address(text)
    if not any(address in network for network in ipv4):
        raise SystemExit(f"private/reserved IPv4 is not blocked: {address}")
for text in ("::1", "::ffff:10.0.0.1", "2001:db8::1", "fc00::1", "fe80::1"):
    address = ipaddress.ip_address(text)
    mapped = address.ipv4_mapped
    blocked = any(address in network for network in ipv6) or (mapped is not None and any(mapped in network for network in ipv4))
    if not blocked:
        raise SystemExit(f"private/reserved IPv6 is not blocked: {address}")
if "dns_nameservers 127.0.0.1" not in (root / "squid.conf").read_text().splitlines():
    raise SystemExit("Squid must resolve exclusively through the filtered local resolver")
PY
if [ "$runtime_mode" = 1 ]; then
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
elif [ -f "$root/compose.override.yaml" ]; then
  override_file=$root/compose.override.yaml
fi
[ -z "$override_file" ] || compose_args+=(-f "$override_file")
"${COMPOSE[@]}" "${compose_args[@]}" --env-file "$env_file" config --quiet
rendered=$tmpdir/compose
if "${COMPOSE[@]}" "${compose_args[@]}" --env-file "$env_file" config --format json >"$rendered" 2>/dev/null; then
  rendered_format=json
else
  "${COMPOSE[@]}" "${compose_args[@]}" --env-file "$env_file" config >"$rendered"
  rendered_format=yaml
fi

python3 - "$rendered" "$rendered_format" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as handle:
    if sys.argv[2] == "json":
        compose = json.load(handle)
    else:
        try:
            import yaml
        except ImportError as error:
            raise SystemExit("Compose lacks JSON output; install PyYAML for validation") from error
        compose = yaml.safe_load(handle)

services = compose.get("services") or {}
networks = compose.get("networks") or {}
if not services or not networks:
    raise SystemExit("Compose must define services and networks")
if "default" in networks or compose.get("volumes"):
    raise SystemExit("default network and named persistent volumes are forbidden")

def service_networks(config):
    value = config.get("networks") or {}
    return set(value if isinstance(value, list) else value.keys())

def command_text(config):
    value = config.get("command") or []
    return value if isinstance(value, str) else " ".join(map(str, value))

def members(name):
    return {service for service, config in services.items() if name in service_networks(config)}

def volume_targets(config):
    result = set()
    for volume in config.get("volumes", []):
        if isinstance(volume, dict):
            result.add(str(volume.get("target", "")))
        else:
            parts = str(volume).split(":")
            if len(parts) > 1:
                result.add(parts[1])
    return result

def relay_is_hardened(name):
    config = services[name]
    if str(config.get("user")) != "65534:65534" or config.get("read_only") is not True:
        return False
    if {str(cap).upper() for cap in config.get("cap_drop", [])} != {"ALL"} or config.get("cap_add"):
        return False
    if config.get("privileged") is True or config.get("ports"):
        return False
    if "no-new-privileges:true" not in config.get("security_opt", []):
        return False
    text = command_text(config)
    return "LISTEN:" in text and ("TCP4:" in text or "OPENSSL-" in text)

for service, config in services.items():
    if config.get("privileged") is True:
        raise SystemExit(f"privileged service is forbidden: {service}")
    if config.get("ports") and not service.endswith("-host-netns"):
        raise SystemExit(f"only host namespace owners may publish ports: {service}")
    depends = config.get("depends_on") or {}
    if isinstance(depends, dict):
        for dependency, settings in depends.items():
            if isinstance(settings, dict) and settings.get("condition") == "service_healthy":
                raise SystemExit(f"{service}->{dependency} uses nonportable service_healthy")
    image = str(config.get("image", ""))
    if not image:
        continue
    if "@sha256:" in image:
        raise SystemExit(f"image digest references are forbidden: {service}")
    image_name = image.rsplit("/", 1)[-1]
    if ":" not in image_name:
        raise SystemExit(f"image is missing an explicit version tag: {service}")
    image_tag = image_name.rsplit(":", 1)[1]
    if image_tag.lower() == "latest" or not any(char.isdigit() for char in image_tag):
        raise SystemExit(f"image tag is not an explicit non-floating version: {service}={image_tag}")

network_members = {name: members(name) for name in networks}
for name, config in networks.items():
    config = config or {}
    current = network_members[name]
    if config.get("internal") is True:
        if len(current) != 2:
            raise SystemExit(f"internal network must have exactly two members: {name}={sorted(current)}")
    elif name.endswith("-host-source"):
        if len(current) != 1 or not next(iter(current)).endswith("-host-netns"):
            raise SystemExit(f"host source network must have one namespace owner: {name}")
    elif name.endswith("-egress"):
        if len(current) != 1:
            raise SystemExit(f"egress network must have one owner: {name}={sorted(current)}")
    else:
        raise SystemExit(f"unexpected non-internal network: {name}")

# Every ordinary source/target edge uses two disjoint pairwise networks joined
# only by one hardened relay. Endpoints never share any network directly.
for source_name in sorted(name for name in networks if name.endswith("-source") and not name.endswith("-host-source")):
    target_name = source_name[:-7] + "-target"
    if target_name not in networks:
        raise SystemExit(f"missing target network for {source_name}")
    source_members = network_members[source_name]
    target_members = network_members[target_name]
    relays = source_members & target_members
    if len(relays) != 1:
        raise SystemExit(f"edge {source_name} must have exactly one shared relay")
    relay = next(iter(relays))
    if not relay_is_hardened(relay):
        raise SystemExit(f"edge relay is not hardened: {relay}")
    source_endpoint = next(iter(source_members - {relay}))
    target_endpoint = next(iter(target_members - {relay}))
    if service_networks(services[source_endpoint]) & service_networks(services[target_endpoint]):
        raise SystemExit(f"edge endpoints share a direct network: {source_endpoint}->{target_endpoint}")

# Host ingress also has a dedicated hardened relay sharing the route-stripped
# namespace owner; no application publishes a port itself.
for source_name in sorted(name for name in networks if name.endswith("-host-source")):
    prefix = source_name[:-12]
    target_name = f"host-{prefix}-target"
    if target_name not in networks:
        raise SystemExit(f"missing host target network for {source_name}")
    owner = next(iter(network_members[source_name]))
    if owner not in network_members[target_name]:
        raise SystemExit(f"host namespace owner does not join {target_name}")
    relays = [name for name, config in services.items() if config.get("network_mode") == f"service:{owner}" and "LISTEN:" in command_text(config)]
    if len(relays) != 1 or not relay_is_hardened(relays[0]):
        raise SystemExit(f"host edge must have one hardened relay: {source_name}")

# Any service sharing another service's namespace is safe only when that owner
# has no external route or explicitly removes all default routes before startup.
for service, config in services.items():
    mode = str(config.get("network_mode", ""))
    if not mode.startswith("service:"):
        continue
    owner = mode.split(":", 1)[1]
    owner_config = services.get(owner)
    if owner_config is None:
        raise SystemExit(f"missing network namespace owner: {service}->{owner}")
    owner_networks = service_networks(owner_config)
    has_external = any((networks[name] or {}).get("internal") is not True for name in owner_networks)
    route_guard = "ip -4 route del default" in command_text(owner_config) and "ip -6 route del default" in command_text(owner_config)
    if has_external and not route_guard:
        raise SystemExit(f"shared namespace owner retains external routing: {owner}")

for service, config in services.items():
    environment = config.get("environment") or {}
    if isinstance(environment, dict) and (environment.get("HTTP_PROXY") or environment.get("HTTPS_PROXY")):
        if environment.get("HTTP_PROXY") != environment.get("HTTPS_PROXY") or not str(environment.get("HTTPS_PROXY")).startswith("http://"):
            raise SystemExit(f"proxy environment mismatch: {service}")

sub2api_environment = services.get("sub2api", {}).get("environment") or {}
if sub2api_environment.get("UPDATE_PROXY_URL") != "http://sub2api-egress-relay:3128":
    raise SystemExit("Sub2API update and pricing clients must use the fail-closed egress relay")

key_holders = {service for service, config in services.items() if "/etc/squid/ca.key" in volume_targets(config)}
if key_holders != {"egress-proxy"}:
    raise SystemExit(f"private egress CA key holders: {sorted(key_holders)}")
for service, config in services.items():
    if service != "egress-proxy" and any(target.endswith("/ca.key") for target in volume_targets(config)):
        raise SystemExit(f"dedicated CA private key mounted by service: {service}")
PY

# Build local components and parse their native configuration. These are syntax
# gates, not snapshots of ports, service counts, or application policy values.
BUILDAH_FORMAT=docker "${COMPOSE[@]}" "${compose_args[@]}" --env-file "$env_file" build ai-sse-keepalive-proxy >/dev/null
egress_image=$(value_from_env EGRESS_PROXY_IMAGE)
[ -n "$egress_image" ] || egress_image=ai-gateway-squid:6.13-2-deb13u2
"${RUNTIME[@]}" build --quiet --tag "$egress_image" egress-proxy >/dev/null
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=validation \
  -addext 'basicConstraints=critical,CA:TRUE' \
  -addext 'keyUsage=critical,keyCertSign,cRLSign' \
  -keyout "$tmpdir/ca.key" -out "$tmpdir/ca.crt" >/dev/null 2>&1
chmod 644 "$tmpdir/ca.key" "$tmpdir/ca.crt"
if ! squid_output=$("${RUNTIME[@]}" run --rm --entrypoint /usr/sbin/squid \
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

# Durable behavioral gates: fail-closed egress/no direct route, and genuinely
# one-way relay edges. The egress test intentionally distinguishes domain,
# method, path, Host, and SNI allowlist decisions.
tun2proxy_image=$(value_from_env TUN2PROXY_IMAGE)
[ -n "$tun2proxy_image" ] || tun2proxy_image=ghcr.io/tun2proxy/tun2proxy:v0.8.3
"$root/scripts/test-egress-proxy.sh" "$egress_image" "$tun2proxy_image"
socat_image=$(value_from_env SOCAT_IMAGE)
[ -n "$socat_image" ] || socat_image=docker.io/alpine/socat:1.8.1.3
"$root/scripts/test-socat-boundary.sh" "$socat_image"
"$root/scripts/test-provider-sidecar-tls-boundary.sh" "$socat_image"
netns_guard_image=$(value_from_env NETNS_GUARD_IMAGE)
[ -n "$netns_guard_image" ] || netns_guard_image=docker.io/library/alpine:3.22.5
"$root/scripts/test-netns-guard.sh" "$netns_guard_image"

python3 - "$root/apisix/apisix.yaml" <<'PY'
import sys
from pathlib import Path

text = Path(sys.argv[1]).read_text(encoding="utf-8")
route_id = "  - id: ai-api-pi-codex-auth-normalize\n"
if text.count(route_id) != 1:
    raise SystemExit("APISIX must declare exactly one Pi Codex auth-normalization route")
start = text.index(route_id)
end = text.find("\n  - id:", start + len(route_id))
route = text[start:] if end < 0 else text[start:end]
for required in (
    "      - GET\n      - POST",
    "priority: 200",
    "^/backend-api/codex/responses(?:/.*)?$",
    'http_x_ai_gateway_auth, "==", "pi-x-api-key"',
    "http_x_api_key, \"~~\", '.+'",
    "enable_websocket: true",
    'ngx.req.clear_header("Authorization")',
    'ngx.req.clear_header("X-AI-Gateway-Auth")',
):
    if required not in route:
        raise SystemExit(f"incomplete Pi Codex auth normalization: {required}")
if "Bearer " in route or 'clear_header("X-API-Key")' in route:
    raise SystemExit("Pi Codex normalization must not embed or clear the Sub2API credential")
PY

apisix_image=$(value_from_env APISIX_IMAGE)
[ -n "$apisix_image" ] || apisix_image=docker.io/apache/apisix:3.18.0-debian
"${RUNTIME[@]}" run --rm \
  -e APISIX_STAND_ALONE=true \
  -v "$root/apisix/config.yaml:/usr/local/apisix/conf/config.yaml:ro" \
  -v "$root/apisix/apisix.yaml:/usr/local/apisix/conf/apisix.yaml:ro" \
  -v "$root/apisix/lua:/opt/apisix/custom:ro" \
  "$apisix_image" apisix test >/dev/null

if command -v shellcheck >/dev/null; then shellcheck scripts/*.sh egress-proxy/*.sh; fi
echo "runtime=$AI_GATEWAY_RUNTIME compose=valid firewall=valid one_way_edges=valid private_paths=untracked"
