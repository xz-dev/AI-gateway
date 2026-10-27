#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
# shellcheck source=ops/scripts/container-runtime.sh
source "$root/ops/scripts/container-runtime.sh"
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
    data/egress-proxy/generated/squid.conf data/egress-proxy/virtual-resolv.conf \
    services/routing/aisix/config.yaml services/routing/aisix/resources.yaml services/routing/aisix/caller-keys.json; do
    [ -f "$path" ] || { echo "missing runtime file: $path" >&2; exit 1; }
  done
  [ "$(stat -c %a data/egress-proxy)" = 700 ] || { echo 'data/egress-proxy must have mode 700' >&2; exit 1; }
  [ "$(stat -c %a data/egress-proxy/generated)" = 755 ] || { echo 'generated egress config must have mode 755' >&2; exit 1; }
  key_id=$(openssl pkey -in data/egress-proxy/ca.key -pubout -outform DER 2>/dev/null | openssl dgst -sha256)
  cert_id=$(openssl x509 -in data/egress-proxy/ca.crt -pubkey -noout 2>/dev/null | openssl pkey -pubin -outform DER 2>/dev/null | openssl dgst -sha256)
  [ "$key_id" = "$cert_id" ] || { echo 'egress CA key and certificate do not match' >&2; exit 1; }
  openssl x509 -in data/egress-proxy/ca.crt -noout -checkend 2592000 >/dev/null || { echo 'egress CA expires within 30 days' >&2; exit 1; }
  [ "$(stat -c %a data/sub2api/postgres)" = 1777 ] || { echo 'data/sub2api/postgres must have mode 1777' >&2; exit 1; }
  [ "$(stat -c %a aisix)" = 700 ] || { echo 'aisix runtime directory must have mode 700' >&2; exit 1; }
  [[ $(stat -c %a services/routing/aisix/config.yaml) == 444 ]] || { echo 'services/routing/aisix/config.yaml must have mode 444 for both non-root AISIX readers' >&2; exit 1; }
  case $(stat -c %a services/routing/aisix/resources.yaml) in 400|444) ;; *) echo 'services/routing/aisix/resources.yaml must have mode 400 or 444' >&2; exit 1 ;; esac
  [ "$(stat -c %a services/routing/aisix/caller-keys.json)" = 600 ] || { echo 'services/routing/aisix/caller-keys.json must have mode 600' >&2; exit 1; }
  readarray -t aisix_key_hashes < <(python3 - <<'PY'
import hashlib
import json
from pathlib import Path

keys = json.loads(Path("services/routing/aisix/caller-keys.json").read_text())
if set(keys) != {"sub2api", "enricher_catalog"} or not all(isinstance(value, str) and value for value in keys.values()):
    raise SystemExit("services/routing/aisix/caller-keys.json has an invalid shape")
for name in ("sub2api", "enricher_catalog"):
    print(hashlib.sha256(keys[name].encode()).hexdigest())
PY
  )
  [ "${#aisix_key_hashes[@]}" = 2 ] || { echo 'failed to derive AISIX caller-key hashes' >&2; exit 1; }
  for hash in "${aisix_key_hashes[@]}"; do
    grep -Eq "^[[:space:]]*key_hash:[[:space:]]*${hash}[[:space:]]*$" services/routing/aisix/resources.yaml || {
      echo 'AISIX caller key does not match resources.yaml' >&2
      exit 1
    }
  done
fi

for path in .env .pi .migration-evidence compose.override.yaml services/cpa/config/config.yaml \
  services/routing/aisix/config.yaml services/routing/aisix/resources.yaml services/routing/aisix/caller-keys.json \
  secrets/cloudflare-tunnel-token data; do
  if git ls-files --error-unmatch "$path" >/dev/null 2>&1; then
    echo "private runtime path is tracked: $path" >&2
    exit 1
  fi
done

# Tailscale addresses are deployment inputs (${TAILSCALE_IP}); never commit one.
if grep -Eq '(^|[^0-9.])100\.(6[4-9]|[7-9][0-9]|1[01][0-9]|12[0-7])\.[0-9]+\.[0-9]+' compose.yaml; then
  echo 'compose.yaml must not contain a literal Tailscale address; use the TAILSCALE_IP variable' >&2
  exit 1
fi

cpa_config=services/cpa/config/config.example.yaml
[ "$runtime_mode" = 0 ] || cpa_config=data/cpa/conf/config.yaml
grep -Eq '^proxy-url:[[:space:]]*"?http://cpa-egress-relay:3128"?[[:space:]]*$' "$cpa_config" || {
  echo "$cpa_config must force global provider traffic through cpa-egress-relay" >&2
  exit 1
}
aisix_config=services/routing/aisix/config.example.yaml
aisix_resources=services/routing/aisix/resources.example.yaml
if [ "$runtime_mode" = 1 ]; then
  aisix_config=services/routing/aisix/config.yaml
  aisix_resources=services/routing/aisix/resources.yaml
fi
python3 - "$aisix_config" <<'PY'
import re
import sys
from pathlib import Path

text = Path(sys.argv[1]).read_text(encoding="utf-8")
match = re.search(r"(?ms)^admin:\s*\n(.*?)(?=^[A-Za-z_][A-Za-z0-9_]*:\s*(?:#.*)?$|\Z)", text)
if not match or not re.search(r"(?m)^\s+addr:\s*172\.30\.68\.2:3002\s*$", match.group(0)):
    raise SystemExit(f"{sys.argv[1]} must bind AISIX Admin only to 172.30.68.2:3002")
# Every AISIX listener binds one explicit relay-target/admin address. A wildcard
# would let each relay peer reach every AISIX port (proxy, Admin, metrics).
if re.search(r"(?m)^\s*(?:-\s*)?addr:\s*(?:0\.0\.0\.0|\[?::\]?|\*)?:\d+\s*$", text):
    raise SystemExit(f"{sys.argv[1]} must not bind any AISIX listener to a wildcard address")
proxy_listeners = set(re.findall(r"(?m)^\s+-\s*addr:\s*(\S+)\s*$", text))
if proxy_listeners != {"172.30.61.2:3000", "172.30.65.2:3000", "172.30.71.2:3000"}:
    raise SystemExit(f"{sys.argv[1]} must list exactly the three AISIX relay-target proxy listeners")
if not re.search(r"(?ms)^\s+prometheus:\s*\n(?:\s+#.*\n)*\s+addr:\s*172\.30\.68\.2:9090\s*$", text):
    raise SystemExit(f"{sys.argv[1]} must bind AISIX metrics only to 172.30.68.2:9090")
PY
tmpdir=$(mktemp -d /tmp/ai-gateway-validate.XXXXXX)
trap 'rm -rf "$tmpdir"' EXIT
resources_wrapper=$tmpdir/aisix-resources-compose.yaml
resources_normalized=$tmpdir/aisix-resources-normalized
{
  printf 'x-aisix-resources:\n'
  sed 's/^/  /' "$aisix_resources"
  printf '\nservices: {}\n'
} >"$resources_wrapper"
if "${COMPOSE[@]}" -f "$resources_wrapper" config --format json >"$resources_normalized" 2>/dev/null; then
  python3 ops/scripts/check-aisix-resource-policy.py --compose-json "$resources_normalized"
elif "${COMPOSE[@]}" -f "$resources_wrapper" config >"$resources_normalized" 2>/dev/null; then
  python3 ops/scripts/check-aisix-resource-policy.py --yaml "$resources_normalized"
else
  echo "$aisix_resources could not be parsed as YAML" >&2
  exit 1
fi
python3 ops/scripts/check-aisix-resource-policy.py --self-test
python3 ops/scripts/check-aisix-session-affinity.py --yaml "$aisix_resources"
python3 ops/scripts/check-aisix-session-affinity.py --self-test
python3 ops/scripts/check-cpa-session-affinity.py --config "${CPA_CONFIG:-$cpa_config}"
python3 ops/scripts/check-cpa-session-affinity.py --self-test
python3 ops/scripts/check-zcode-egress-policy.py --self-test

python3 ops/scripts/render-egress-policy.py services/firewall/egress-proxy/policy.example.json "$tmpdir/proxy-default"
python3 - services/firewall/egress-proxy/policy.example.json <<'PY'
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
            r"^/repos/anthropics/claude-code/releases/latest$",
            r"^/repos/anthropics/claude-code/releases\?per_page=30$",
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
python3 ops/scripts/render-egress-policy.py services/firewall/egress-proxy/testdata/policy.json "$tmpdir/proxy-test"
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
  python3 ops/scripts/check-zcode-egress-policy.py --policy data/egress-proxy/policy.json
  python3 ops/scripts/render-egress-policy.py data/egress-proxy/policy.json "$tmpdir/proxy-runtime"
  diff -ru "$tmpdir/proxy-runtime" data/egress-proxy/generated >/dev/null || {
    echo 'stale egress config; run ./ops/scripts/init-egress-proxy.sh' >&2
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
import ipaddress
import json
import re
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
# Persistent application state stays in backed-up data/ binds. Each disposable
# cache/spool has one named disk volume, mounted only by its owner at its path.
CACHE_VOLUMES = {
    "openai-api-cache-keeper-cache": ("openai-api-cache-keeper", "/var/tmp/openai-api-cache-keeper"),
    "model-catalog-cache": ("model-catalog", "/var/cache/model-catalog"),
    "responses-ws-sse-bridge-spool": ("responses-ws-sse-bridge", "/spool"),
}
if "default" in networks or set(compose.get("volumes") or {}) != set(CACHE_VOLUMES):
    raise SystemExit("default network forbidden; only dedicated cache/spool volumes may be defined")
def volume_mounts(config):
    """(source, target) pairs; docker compose JSON uses long form, YAML may keep short form."""
    for volume in config.get("volumes", []):
        if isinstance(volume, dict):
            yield str(volume.get("source", "")), str(volume.get("target", ""))
        else:
            parts = str(volume).split(":")
            yield parts[0], parts[1] if len(parts) > 1 else ""

for service, config in services.items():
    for source, target in volume_mounts(config):
        if source in CACHE_VOLUMES and CACHE_VOLUMES[source] != (service, target):
            raise SystemExit(f"{source} must be mounted only by its cache owner at its cache path")
for source, (owner, target) in CACHE_VOLUMES.items():
    if (source, target) not in set(volume_mounts(services.get(owner, {}))):
        raise SystemExit(f"{owner} must mount its dedicated disk cache volume")
if ((services.get("model-catalog") or {}).get("environment") or {}).get("CACHE_DIR") != "/var/cache/model-catalog":
    raise SystemExit("model-catalog CACHE_DIR must use its dedicated disk cache volume")

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

def service_ips(config):
    return {str(value.get("ipv4_address")) for value in (config.get("networks") or {}).values() if isinstance(value, dict) and value.get("ipv4_address")}

# A relay listener must bind its own source-side address. Without bind= socat
# listens on every interface, so the relay target (or any other peer on the
# owner's networks) could open connections back through it.
for service, config in services.items():
    text = command_text(config)
    if "LISTEN:" not in text:
        continue
    mode = str(config.get("network_mode", ""))
    owner = services.get(mode.split(":", 1)[1], {}) if mode.startswith("service:") else config
    owner_ips = service_ips(owner)
    listeners = re.findall(r"(?:TCP4|OPENSSL)-LISTEN:[^\s\"']+", text)
    if not listeners:
        raise SystemExit(f"relay listener is not parseable: {service}")
    for listener in listeners:
        bound = re.search(r",bind=([0-9.]+)(?:,|$)", listener)
        if not bound or bound.group(1) not in owner_ips:
            raise SystemExit(f"relay listener must bind one own source address: {service}: {listener}")

for service, config in services.items():
    if config.get("privileged") is True:
        raise SystemExit(f"privileged service is forbidden: {service}")
    if config.get("ports") and not (service.endswith("-host-netns") or service == "aisix-netns"):
        raise SystemExit(f"only host namespace owners may publish ports: {service}")
    depends = config.get("depends_on") or {}
    # service_healthy is portable: podman-compose maps it (podman_compose.py
    # ServiceDependencyCondition.HEALTHY) and production runs docker compose.
    # The 0ca7e00-era ban was lifted on 2026-09-19 after verification.
    image = str(config.get("image", ""))
    if not image:
        continue
    if "@" in image:
        image, digest = image.rsplit("@", 1)
        if not digest.startswith("sha256:") or len(digest.removeprefix("sha256:")) != 64:
            raise SystemExit(f"image digest must be a complete sha256 pin: {service}")
    image_name = image.rsplit("/", 1)[-1]
    if ":" not in image_name:
        raise SystemExit(f"image is missing an explicit version tag: {service}")
    image_tag = image_name.rsplit(":", 1)[1]
    if image_tag.lower() == "latest" or not any(char.isdigit() for char in image_tag):
        raise SystemExit(f"image tag is not an explicit non-floating version: {service}={image_tag}")

# Validate merged private bindings: overrides must not expose trusted listeners.
def private_host_binding(port, target_port, published_port=None):
    if isinstance(port, dict):
        host, published, target, protocol = (port.get(k) for k in ("host_ip", "published", "target", "protocol"))
        protocol = protocol or "tcp"
    else:
        address, _, protocol = str(port).partition("/")
        parts = address.rsplit(":", 2)
        if len(parts) != 3:
            return False
        host, published, target = parts
        host, protocol = host.strip("[]"), protocol or "tcp"
    try:
        ip = ipaddress.ip_address(host or "")
        published = int(published)
        return (ip.is_loopback or ip in ipaddress.ip_network("100.64.0.0/10")) and 1 <= published <= 65535 and (published_port is None or published == published_port) and str(target) == str(target_port) and protocol == "tcp"
    except (TypeError, ValueError):
        return False

for port, expected in (
    ("127.0.0.1:9083:9083", True), ("100.94.238.35:9083:9083", True),
    ("[::1]:9083:9083", True), ("0.0.0.0:9083:9083", False),
    ("[::]:9083:9083", False), ("9083:9083", False),
    ({"host_ip": "0.0.0.0", "published": "9083", "target": 9083}, False),
    ({"host_ip": "127.0.0.1", "published": "9083", "target": 9083}, True),
    ({"published": "9083", "target": 9083}, False),
    ("203.0.113.1:9083:9083", False),
):
    if private_host_binding(port, 9083, 9083) != expected:
        raise SystemExit(f"models-table binding self-test failed: {port}")

def published_port(port):
    if isinstance(port, dict):
        return str(port.get("published", ""))
    parts = str(port).partition("/")[0].rsplit(":", 2)
    return parts[-2] if len(parts) >= 2 else ""

# The front-door host namespace publishes the public API port and the operator
# models-table listener 9083; 9083 is loopback/Tailscale only, never wildcard.
front_ports = services.get("apisix-host-netns", {}).get("ports") or []
table_ports = [port for port in front_ports if published_port(port) == "9083"]
if not table_ports or not all(private_host_binding(port, 9083, 9083) for port in table_ports):
    raise SystemExit("models-table port 9083 must publish only on loopback or an explicit Tailscale address, never wildcard/public")
if len(front_ports) - len(table_ports) != 1:
    raise SystemExit("front-door host namespace may publish only the API port and the 9083 models table")

network_members = {name: members(name) for name in networks}

operator_owner = services.get("operator-host-netns", {})
operator_ports = operator_owner.get("ports") or []
if not operator_ports or not all(private_host_binding(port, 8080) for port in operator_ports):
    raise SystemExit("Operator must publish only on loopback or explicit Tailscale IP, forwarding TCP to port 8080")
if service_networks(operator_owner) != {"operator-host-source", "host-operator-target"} \
        or network_members.get("operator-host-source") != {"operator-host-netns"} \
        or network_members.get("host-operator-target") != {"operator-host-netns", "ai-sse-keepalive-proxy-netns"}:
    raise SystemExit("Operator must use its isolated host edge into the shared keepalive namespace")
operator_relay = services.get("operator-host-relay", {})
if operator_relay.get("network_mode") != "service:operator-host-netns" or not relay_is_hardened("operator-host-relay") \
        or "TCP4-LISTEN:8080,bind=172.30.72.2," not in command_text(operator_relay) \
        or "TCP4:172.30.73.2:8080," not in command_text(operator_relay):
    raise SystemExit("Operator relay must target only the shared keepalive, not APISIX or Sub2API directly")
if (services.get("ai-sse-keepalive-proxy", {}).get("environment") or {}).get("LISTEN_ADDR") != ":8080":
    raise SystemExit("shared keepalive must accept both ingress relay targets on port 8080")
if (services.get("sub2api", {}).get("environment") or {}).get("SECURITY_TRUST_FORWARDED_IP_FOR_API_KEY_ACL") != "false":
    raise SystemExit("Operator ingress must not trust caller-supplied forwarded IP for API-key ACLs")

# AISIX has no direct data network: each of its four edges is an ordinary
# one-way source/target relay pair, checked by the generic edge loop below.
aisix_edges = {
    "sub2api-aisix": ("sub2api-netns", "aisix-netns"),
    "catalog-aisix": ("model-catalog-netns", "aisix-netns"),
    "apisix-models-aisix": ("apisix-models-netns", "aisix-netns"),
    "aisix-cpa": ("aisix-netns", "cpa-netns"),
}
for name, (source_owner, target_owner) in aisix_edges.items():
    relay = f"{name}-relay"
    if network_members.get(f"{name}-source") != {source_owner, relay} or network_members.get(f"{name}-target") != {relay, target_owner}:
        raise SystemExit(f"AISIX edge {name} must be a one-way relay pair {source_owner}->{relay}->{target_owner}")

aisix_service = services.get("aisix", {})
status_service = services.get("aisix-status", {})
for name, service in (("aisix", aisix_service), ("aisix-status", status_service)):
    if service.get("build") is not None:
        raise SystemExit(f"{name} must consume its fixed-version GHCR image, not build on the deployment host")
if aisix_service.get("network_mode") != "service:aisix-netns":
    raise SystemExit("AISIX must share the route-stripped aisix-netns namespace")
aisix_owner = services.get("aisix-netns", {})
if service_networks(aisix_owner) != {"aisix-host-source", "aisix-status-admin", "sub2api-aisix-target", "catalog-aisix-target", "apisix-models-aisix-target", "aisix-cpa-source"}:
    raise SystemExit("aisix-netns has an unexpected network membership")
aisix_ports = aisix_owner.get("ports") or []
if not aisix_ports:
    raise SystemExit("AISIX status page binding is missing")
for port in aisix_ports:
    if isinstance(port, dict):
        host_ip = str(port.get("host_ip") or "")
        target = int(port.get("target", 0))
    else:
        parts = str(port).rsplit(":", 2)
        if len(parts) != 3:
            raise SystemExit("AISIX status port must use an explicit host binding")
        host_ip = parts[0].strip("[]")
        target = int(parts[2].split("/", 1)[0])
    if host_ip in ("", "0.0.0.0", "::", "[::]"):
        raise SystemExit("AISIX status port must not use a wildcard host binding")
    if target != 3001:
        raise SystemExit("AISIX namespace may publish only the status relay port 3001")

routing_service = services.get("model-catalog", {})
if routing_service.get("network_mode") != "service:model-catalog-netns" or routing_service.get("ports"):
    raise SystemExit("model-catalog routing index must remain inside its route-stripped namespace")
if (routing_service.get("healthcheck") or {}).get("test") != ["CMD", "/model-catalog", "healthcheck"]:
    raise SystemExit("model-catalog must report canonical-snapshot readiness without publishing the index")
relay_dependencies = services.get("apisix-models-catalog-relay", {}).get("depends_on") or {}
if (relay_dependencies.get("model-catalog") or {}).get("condition") != "service_started":
    raise SystemExit("the selector relay must start during a cold snapshot so classified requests can return 503")
selector_service = services.get("apisix-models", {})
if selector_service.get("ports") or selector_service.get("network_mode") != "service:apisix-models-netns":
    raise SystemExit("catalog routing selector must not publish an application port")
if "/opt/apisix/custom/routing_selector.lua" not in volume_targets(selector_service):
    raise SystemExit("catalog routing selector module must be mounted read-only into APISIX")

# One front keeper and a stateless WS bridge share keepalive's namespace.
keeper = services.get("openai-api-cache-keeper", {})
keeper_env = keeper.get("environment") or {}
if keeper.get("network_mode") != "service:ai-sse-keepalive-proxy-netns" or keeper.get("ports") or keeper.get("networks"):
    raise SystemExit("cache keeper must share only the keepalive namespace without ports")
keeper_target = {
    "UPSTREAM_URL": "http://127.0.0.1:8086",
    "LISTEN_ADDR": "127.0.0.1:8085",
    "STORE_DIR": "/var/tmp/openai-api-cache-keeper",
    "IGNORE_CLIENT_STORE_FALSE": "true",
}
if any(keeper_env.get(k) != v for k, v in keeper_target.items()) \
        or {"CPA_API_KEY", "ROUTING_INDEX_URL", "UPSTREAM_CPA_URL", "UPSTREAM_AISIX_URL"} & set(keeper_env) \
        or str(keeper_env.get("REQUIRE_NO_DEFAULT_ROUTE")) != "true":
    raise SystemExit("cache keeper must retain client turns and use only the local WS bridge, not model routing")
keepalive = services.get("ai-sse-keepalive-proxy", {})
if (keepalive.get("environment") or {}).get("UPSTREAM_URL") != "http://127.0.0.1:8085" \
        or ((keepalive.get("depends_on") or {}).get("openai-api-cache-keeper") or {}).get("condition") != "service_healthy":
    raise SystemExit("keepalive must enter the shared healthy cache keeper before Sub2API")
if "response-keeper" in services:
    raise SystemExit("the old inner response-keeper must not remain active")
if ("openai-api-cache-keeper-cache", "/var/tmp/openai-api-cache-keeper") not in set(volume_mounts(keeper)):
    raise SystemExit("cache keeper must use its exclusive disk cache volume")
if str(keeper.get("user")) != "65534:65534" or keeper.get("read_only") is not True or {str(cap).upper() for cap in keeper.get("cap_drop", [])} != {"ALL"} or keeper.get("cap_add"):
    raise SystemExit("cache keeper must be non-root/read-only and drop all capabilities")
if "no-new-privileges:true" not in keeper.get("security_opt", []) \
        or str(keeper.get("mem_limit")).lower() not in {"384m", str(384 * 1024 * 1024)} \
        or str(keeper.get("memswap_limit")).lower() not in {"384m", str(384 * 1024 * 1024)} \
        or int(keeper.get("pids_limit") or 0) != 64 or float(keeper.get("cpus") or 0) != 0.5:
    raise SystemExit("cache keeper must retain 384 MiB, no swap, 0.5 CPU and 64 PIDs")
bridge = services.get("responses-ws-sse-bridge", {})
bridge_env = bridge.get("environment") or {}
if bridge.get("network_mode") != "service:ai-sse-keepalive-proxy-netns" or bridge.get("ports") or bridge.get("networks"):
    raise SystemExit("WS bridge must share only the keepalive namespace without ports")
if any(bridge_env.get(k) != v for k, v in {
    "UPSTREAM_URL": "http://sub2api-ai-sse-relay:8080", "LISTEN_ADDR": "127.0.0.1:8086", "SPOOL_DIR": "/spool", "GOMEMLIMIT": "192MiB",
}.items()) or bridge.get("tmpfs"):
    raise SystemExit("WS bridge must send HTTP to Sub2API and spool on disk")
if str(bridge.get("user")) != "65534:65534" or bridge.get("read_only") is not True or {str(cap).upper() for cap in bridge.get("cap_drop", [])} != {"ALL"} or bridge.get("cap_add"):
    raise SystemExit("WS bridge must be non-root/read-only and drop all capabilities")
if "no-new-privileges:true" not in bridge.get("security_opt", []) \
        or str(bridge.get("mem_limit")).lower() not in {"256m", str(256 * 1024 * 1024)} \
        or str(bridge.get("memswap_limit")).lower() not in {"256m", str(256 * 1024 * 1024)} \
        or int(bridge.get("pids_limit") or 0) != 64 or float(bridge.get("cpus") or 0) != 0.5:
    raise SystemExit("WS bridge must retain 256 MiB, no swap, 0.5 CPU and 64 PIDs")
if ((keeper.get("depends_on") or {}).get("responses-ws-sse-bridge") or {}).get("condition") != "service_healthy":
    raise SystemExit("keeper must wait for the healthy WS bridge")
if (sub2api_env := services.get("sub2api", {}).get("environment") or {}).get("GATEWAY_OPENAI_WS_INGRESS_PREVIOUS_RESPONSE_RECOVERY_ENABLED") is None:
    raise SystemExit("Sub2API must pin WS previous_response recovery explicitly")

if service_networks(status_service) != {"aisix-status-admin"}:
    raise SystemExit("AISIX status renderer must attach only to aisix-status-admin")
if network_members.get("aisix-status-admin") != {"aisix-netns", "aisix-status"}:
    raise SystemExit("AISIX Admin network must contain only AISIX and the status renderer")
if str(status_service.get("user")) != "65532:65532" or status_service.get("read_only") is not True or status_service.get("ports") or status_service.get("healthcheck"):
    raise SystemExit("AISIX status renderer must be non-root/read-only without ports or healthcheck timers")
if {str(cap).upper() for cap in status_service.get("cap_drop", [])} != {"ALL"} or status_service.get("cap_add"):
    raise SystemExit("AISIX status renderer must drop all capabilities")
if "no-new-privileges:true" not in status_service.get("security_opt", []):
    raise SystemExit("AISIX status renderer must set no-new-privileges")
if not status_service.get("pids_limit") or not status_service.get("mem_limit") or not status_service.get("cpus"):
    raise SystemExit("AISIX status renderer must have explicit resource limits")
status_environment = status_service.get("environment") or {}
if status_environment.get("AISIX_ADMIN_URL") != "http://172.30.68.2:3002":
    raise SystemExit("AISIX status renderer must use the fixed internal Admin address")
if set(volume_targets(status_service)) != {"/etc/aisix/config.yaml"}:
    raise SystemExit("AISIX status renderer may mount only the read-only AISIX bootstrap config")
status_relay = services.get("aisix-status-relay", {})
relay_text = command_text(status_relay)
if status_relay.get("network_mode") != "service:aisix-netns" or not relay_is_hardened("aisix-status-relay"):
    raise SystemExit("AISIX status page must use a hardened relay in the AISIX namespace")
if "TCP4-LISTEN:3001,bind=172.30.66.2," not in relay_text or "TCP4:172.30.68.3:8080" not in relay_text:
    raise SystemExit("AISIX status relay must forward only port 3001 to the isolated renderer")

# The front door reads the catalog (GET /v1/models) and the models table over two
# direct two-member pairs; neither carries public inference.
for name, expected_members in (
    ("front-catalog", {"apisix-netns", "apisix-models-netns"}),
    ("front-model-table", {"apisix-netns", "model-catalog-netns"}),
):
    if (networks.get(name) or {}).get("internal") is not True or network_members.get(name) != expected_members:
        raise SystemExit(f"{name} must be an internal pair {sorted(expected_members)}")
internal_membership_exceptions = {}
for name, config in networks.items():
    config = config or {}
    current = network_members[name]
    if config.get("internal") is True:
        expected_members = internal_membership_exceptions.get(name)
        if expected_members is not None:
            if current != expected_members:
                raise SystemExit(f"invalid bounded internal network {name}: {sorted(current)}")
        elif len(current) != 2:
            raise SystemExit(f"internal network must have exactly two members: {name}={sorted(current)}")
    elif name.endswith("-host-source"):
        if name == "aisix-host-source":
            if current != {"aisix-netns"}:
                raise SystemExit(f"AISIX host source must have only aisix-netns: {sorted(current)}")
        elif len(current) != 1 or not next(iter(current)).endswith("-host-netns"):
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
    if source_name == "aisix-host-source":
        if status_relay.get("network_mode") != "service:aisix-netns" or not relay_is_hardened("aisix-status-relay"):
            raise SystemExit("AISIX status host edge must use its hardened namespace relay")
        continue
    prefix = source_name[:-12]
    target_name = f"host-{prefix}-target"
    if target_name not in networks:
        raise SystemExit(f"missing host target network for {source_name}")
    owner = next(iter(network_members[source_name]))
    if owner not in network_members[target_name]:
        raise SystemExit(f"host namespace owner does not join {target_name}")
    relays = [name for name, config in services.items() if config.get("network_mode") == f"service:{owner}" and "LISTEN:" in command_text(config)]
    if owner == "apisix-host-netns":
        # Second relay: the operator models-table listener, 9083 to the front door's 9083 only.
        table_relay = services.get("apisix-host-table-relay", {})
        if "apisix-host-table-relay" not in relays or not relay_is_hardened("apisix-host-table-relay") \
                or "TCP4-LISTEN:9083,bind=172.30.5.2," not in command_text(table_relay) \
                or "TCP4:172.30.6.2:9083," not in command_text(table_relay):
            raise SystemExit("models table host edge must forward 9083 only to the front door's 9083 listener")
        relays.remove("apisix-host-table-relay")
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

# The synchronizer is deliberately a direct, private CPA management peer, not
# another source/target relay chain. It cannot reach an external network.
sync = services.get("cpa-model-sync", {})
if service_networks(sync) != {"model-sync-cpa"} or network_members.get("model-sync-cpa") != {"cpa-model-sync", "cpa-netns"}:
    raise SystemExit("model synchronization must have a dedicated direct CPA pair")
if str(sync.get("user")) != "65534:65534" or not sync.get("read_only") or sync.get("ports") or sync.get("healthcheck"):
    raise SystemExit("model synchronization must be non-root/read-only without ports or healthcheck timers")
if {str(cap).upper() for cap in sync.get("cap_drop", [])} != {"ALL"} or sync.get("cap_add"):
    raise SystemExit("model synchronization must drop all capabilities")
cpa_ip = services["cpa-netns"]["networks"]["model-sync-cpa"]["ipv4_address"]
hosts = sync.get("extra_hosts") or {}
if isinstance(hosts, dict):
    direct = hosts.get("cli-proxy-api") == cpa_ip
else:
    direct = any(host in (f"cli-proxy-api:{cpa_ip}", f"cli-proxy-api={cpa_ip}") for host in hosts)
if not direct:
    raise SystemExit("model synchronization must resolve cli-proxy-api directly to CPA")

key_holders = {service for service, config in services.items() if "/etc/squid/ca.key" in volume_targets(config)}
if key_holders != {"egress-proxy"}:
    raise SystemExit(f"private egress CA key holders: {sorted(key_holders)}")
for service, config in services.items():
    if service != "egress-proxy" and any(target.endswith("/ca.key") for target in volume_targets(config)):
        raise SystemExit(f"dedicated CA private key mounted by service: {service}")
PY
python3 ops/scripts/tests/test-standard-cache-topology.py
python3 ops/scripts/tests/test-component-deploy.py
python3 ops/scripts/tests/test-keeper-observe.py

# Build local components and parse their native configuration. These are syntax
# gates, not snapshots of ports, service counts, or application policy values.
# A digest selects a published image; it cannot be an output tag for a local build.
AI_SSE_KEEPALIVE_PROXY_IMAGE=ai-gateway-ai-sse-keepalive-proxy:validate \
OPENAI_API_CACHE_KEEPER_IMAGE=ai-gateway-openai-api-cache-keeper:validate \
BUILDAH_FORMAT=docker "${COMPOSE[@]}" "${compose_args[@]}" --env-file "$env_file" build ai-sse-keepalive-proxy openai-api-cache-keeper >/dev/null
egress_image=$(value_from_env EGRESS_PROXY_IMAGE)
[ -n "$egress_image" ] || egress_image=ai-gateway-squid:6.13-2-deb13u3-r1
"${RUNTIME[@]}" build --quiet --tag "$egress_image" services/firewall/egress-proxy >/dev/null
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
"$root/ops/scripts/tests/test-egress-proxy.sh" "$egress_image" "$tun2proxy_image"
socat_image=$(value_from_env SOCAT_IMAGE)
[ -n "$socat_image" ] || socat_image=docker.io/alpine/socat:1.8.1.3
"$root/ops/scripts/tests/test-socat-boundary.sh" "$socat_image"
"$root/ops/scripts/tests/test-provider-sidecar-tls-boundary.sh" "$socat_image"
netns_guard_image=$(value_from_env NETNS_GUARD_IMAGE)
[ -n "$netns_guard_image" ] || netns_guard_image=docker.io/library/alpine:3.22.5
"$root/ops/scripts/tests/test-netns-guard.sh" "$netns_guard_image"

python3 - "$root/services/firewall/apisix/apisix.yaml" <<'PY'
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

# The models table is served only on the 9083 operator listener, straight to model-catalog.
def route_block(route_id):
    marker = f"  - id: {route_id}\n"
    if text.count(marker) != 1:
        raise SystemExit(f"APISIX must declare exactly one {route_id} route")
    start = text.index(marker)
    end = text.find("\n  - id:", start + len(marker))
    return text[start:] if end < 0 else text[start:end]
for route_id, uri, method in (("models-table", "/models-table", "GET"), ("models-table-refresh", "/models-table/refresh", "POST")):
    block = route_block(route_id)
    for required in (f"uri: {uri}\n", f"methods: [{method}]", '[server_port, "==", "9083"]', "upstream_id: model-catalog"):
        if required not in block:
            raise SystemExit(f"incomplete operator models-table route {route_id}: {required}")
if text.count("upstream_id: model-catalog") != 2 or text.count('[server_port, "==", "9083"]') != 2:
    raise SystemExit("only the two 9083 models-table routes may reach model-catalog")
lua = (Path(sys.argv[1]).parent / "lua/model_list_intersection.lua").read_text(encoding="utf-8")
if 'local ORIGINAL_UPSTREAM_HOST = "apisix-models"' not in lua or "local ORIGINAL_UPSTREAM_PORT = 9080" not in lua:
    raise SystemExit("front-door catalog leg must read apisix-models directly over front-catalog")
PY

python3 - "$root/services/routing/apisix-models/apisix.yaml" <<'PY'
import sys
from pathlib import Path

text = Path(sys.argv[1]).read_text(encoding="utf-8")

def route(route_id):
    marker = f"  - id: {route_id}\n"
    if text.count(marker) != 1:
        raise SystemExit(f"APISIX models must declare exactly one {route_id} route")
    start = text.index(marker)
    end = text.find("\n  - id:", start + len(marker))
    return text[start:] if end < 0 else text[start:end]

selector = route("catalog-routing-selector")
for required in (
    "priority: 100",
    "      - /v1/chat/completions\n      - /v1/images/generations",
    "      - POST",
    'require("routing_selector").run(_, ctx, "__CPA_API_KEY__")',
    "disable_proxy_buffering: true",
):
    if required not in selector:
        raise SystemExit(f"incomplete catalog routing selector: {required}")
if "/v1/responses" in selector:
    raise SystemExit("Responses must use their separate expanded-history size boundary")
for forbidden in ("ai-proxy-multi", "alias-gpt-", "alias-grok-", "alias-team-"):
    if forbidden in text:
        raise SystemExit(f"legacy HTTP alias still conflicts with catalog selection: {forbidden}")
if "  - id: ws-alias\n" in text:
    raise SystemExit("retired ws-alias route must not return (retired in 103ecac)")
models = route("codex-models")
if "      - GET" not in models or "arg_client_version" in models or "upstream_id: model-catalog" not in models:
    raise SystemExit("standard and Codex model lists must both reach model-catalog")
if text.count("upstream_id: model-catalog") != 1 or "/models-table" in text.replace("# /models-table", ""):
    raise SystemExit("apisix-models must route only /v1/models to model-catalog, never the models table")
edits = route("aisix-images-edits")
for required in ("uri: /v1/images/edits", "      - POST", "upstream_id: aisix"):
    if required not in edits:
        raise SystemExit(f"incomplete AISIX images edits route: {required}")
if "__CPA_API_KEY__" in edits or text.count("__CPA_API_KEY__") != 2 or selector.count("__CPA_API_KEY__") != 1:
    raise SystemExit("CPA service credentials must occur only at the two HTTP selector boundaries")
if '  - id: aisix\n' not in text or '      "aisix:3000": 1' not in text:
    raise SystemExit("catalog selector AISIX upstream is missing")
responses = route("responses-routing-selector")
for required in ("uri: /v1/responses", "methods: [POST]", "priority: 110", 'require("routing_selector").run(_, ctx, "__CPA_API_KEY__")', "upstream_id: cpa", "disable_proxy_buffering: true", "max_body_size: 67108864"):
    if required not in responses:
        raise SystemExit(f"incomplete standard Responses selector: {required}")
if 'max_body_size: 16777216' not in text or '["route_id", "~=", "responses-routing-selector"]' not in text:
    raise SystemExit("other routes must keep 16 MiB while Responses admits bounded expanded history")
if "/rk" in text or "upstream_id: response-keeper" in text or '      "127.0.0.1:8085": 1' in text or '  - id: self\n' in text:
    raise SystemExit("inner APISIX must not retain private aliases, a duplicate keeper or a self loop")
if "enable_websocket: true" not in route("catch-all") or "upstream_id: cpa" not in route("catch-all"):
    raise SystemExit("native WebSocket forwarding must remain on the normal downstream path")
PY

apisix_image=$(value_from_env APISIX_IMAGE)
[ -n "$apisix_image" ] || apisix_image=docker.io/apache/apisix:3.19.0-debian
sed 's|__CPA_API_KEY__|fixture-cpa-key|g' "$root/services/routing/apisix-models/apisix.yaml" >"$tmpdir/apisix-models.yaml"
"${RUNTIME[@]}" run --rm --sysctl net.ipv4.ip_nonlocal_bind=1 --network none \
  -e APISIX_STAND_ALONE=true \
  -v "$root/services/routing/apisix-models/config.yaml:/usr/local/apisix/conf/config.yaml:ro" \
  -v "$tmpdir/apisix-models.yaml:/usr/local/apisix/conf/apisix.yaml:ro" \
  -v "$root/services/routing/apisix-models/routing_selector.lua:/opt/apisix/custom/routing_selector.lua:ro" \
  "$apisix_image" apisix test >/dev/null
"${RUNTIME[@]}" run --rm --network none \
  -v "$root/services/routing/apisix-models:/work:ro" \
  --entrypoint /usr/local/openresty/bin/resty \
  "$apisix_image" /work/routing_selector_test.lua >/dev/null
"${RUNTIME[@]}" run --rm --network none \
  -v "$root/services/firewall/apisix/lua:/work:ro" \
  --entrypoint /usr/local/openresty/bin/resty \
  "$apisix_image" /work/model_list_intersection_test.lua >/dev/null
"$root/ops/scripts/tests/test-catalog-routes.sh" "$apisix_image" >/dev/null
"${RUNTIME[@]}" run --rm --sysctl net.ipv4.ip_nonlocal_bind=1 \
  -e APISIX_STAND_ALONE=true \
  -v "$root/services/firewall/apisix/config.yaml:/usr/local/apisix/conf/config.yaml:ro" \
  -v "$root/services/firewall/apisix/apisix.yaml:/usr/local/apisix/conf/apisix.yaml:ro" \
  -v "$root/services/firewall/apisix/lua:/opt/apisix/custom:ro" \
  "$apisix_image" apisix test >/dev/null
# Both data planes must render only explicit relay-target/loopback listeners
# and no TLS listener (the APISIX default otherwise opens 0.0.0.0:9443).
for plane_dir in services/firewall/apisix services/routing/apisix-models; do
  plane=${plane_dir##*/}
  plane_routes=$root/$plane_dir/apisix.yaml
  [ "$plane" = apisix ] || plane_routes=$tmpdir/apisix-models.yaml
  listeners=$("${RUNTIME[@]}" run --rm --network none -e APISIX_STAND_ALONE=true \
    -v "$root/$plane_dir/config.yaml:/usr/local/apisix/conf/config.yaml:ro" \
    -v "$plane_routes:/usr/local/apisix/conf/apisix.yaml:ro" \
    --entrypoint /bin/sh "$apisix_image" -ec \
    'apisix init >/dev/null; sed -n "s/^[[:space:]]*listen[[:space:]]\{1,\}\([^ ;]*\).*/\1/p" /usr/local/apisix/conf/nginx.conf | sort -u')
  while IFS= read -r listener; do
    # Only the front door adds the operator models-table listener on its host relay target.
    [ "$plane:$listener" != apisix:172.30.6.2:9083 ] || continue
    case $listener in
      unix:*|127.0.0.1:*|172.30.*:9080) ;;
      *) echo "$plane renders a non-explicit or TLS listener: $listener" >&2; exit 1 ;;
    esac
  done <<<"$listeners"
done

if command -v shellcheck >/dev/null; then shellcheck ops/scripts/*.sh services/firewall/egress-proxy/*.sh; fi
echo "runtime=$AI_GATEWAY_RUNTIME compose=valid firewall=valid one_way_edges=valid private_paths=untracked"
