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
    data/egress-proxy/generated/squid.conf data/egress-proxy/virtual-resolv.conf \
    aisix/config.yaml aisix/resources.yaml aisix/caller-keys.json; do
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
  [[ $(stat -c %a aisix/config.yaml) == 444 ]] || { echo 'aisix/config.yaml must have mode 444 for both non-root AISIX readers' >&2; exit 1; }
  case $(stat -c %a aisix/resources.yaml) in 400|444) ;; *) echo 'aisix/resources.yaml must have mode 400 or 444' >&2; exit 1 ;; esac
  [ "$(stat -c %a aisix/caller-keys.json)" = 600 ] || { echo 'aisix/caller-keys.json must have mode 600' >&2; exit 1; }
  readarray -t aisix_key_hashes < <(python3 - <<'PY'
import hashlib
import json
from pathlib import Path

keys = json.loads(Path("aisix/caller-keys.json").read_text())
if set(keys) != {"sub2api", "enricher_catalog"} or not all(isinstance(value, str) and value for value in keys.values()):
    raise SystemExit("aisix/caller-keys.json has an invalid shape")
for name in ("sub2api", "enricher_catalog"):
    print(hashlib.sha256(keys[name].encode()).hexdigest())
PY
  )
  [ "${#aisix_key_hashes[@]}" = 2 ] || { echo 'failed to derive AISIX caller-key hashes' >&2; exit 1; }
  for hash in "${aisix_key_hashes[@]}"; do
    grep -Eq "^[[:space:]]*key_hash:[[:space:]]*${hash}[[:space:]]*$" aisix/resources.yaml || {
      echo 'AISIX caller key does not match resources.yaml' >&2
      exit 1
    }
  done
fi

for path in .env .pi .migration-evidence compose.override.yaml cpa/config.yaml \
  aisix/config.yaml aisix/resources.yaml aisix/caller-keys.json \
  secrets/cloudflare-tunnel-token data; do
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
aisix_config=aisix/config.example.yaml
[ "$runtime_mode" = 0 ] || aisix_config=aisix/config.yaml
python3 - "$aisix_config" <<'PY'
import re
import sys
from pathlib import Path

text = Path(sys.argv[1]).read_text(encoding="utf-8")
match = re.search(r"(?ms)^admin:\s*\n(.*?)(?=^[A-Za-z_][A-Za-z0-9_]*:\s*(?:#.*)?$|\Z)", text)
if not match or not re.search(r"(?m)^\s+addr:\s*172\.30\.68\.2:3002\s*$", match.group(0)):
    raise SystemExit(f"{sys.argv[1]} must bind AISIX Admin only to 172.30.68.2:3002")
PY

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
    "enricher": sorted((
        entry("modelparams.dev", r"^/api/v1/models\.json($|[?])"),
        entry("models.dev", r"^/(api\.json|catalog\.json)($|[?])"),
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
    if config.get("ports") and not (service.endswith("-host-netns") or service == "aisix-netns"):
        raise SystemExit(f"only host namespace owners may publish ports: {service}")
    depends = config.get("depends_on") or {}
    if isinstance(depends, dict):
        for dependency, settings in depends.items():
            if isinstance(settings, dict) and settings.get("condition") == "service_healthy":
                raise SystemExit(f"{service}->{dependency} uses nonportable service_healthy")
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

network_members = {name: members(name) for name in networks}

# AISIX is the intentional direct-edge exception to the ordinary relay shape.
# Validate exact namespace-owner membership so a future Compose edit cannot
# silently widen these pairwise networks or reintroduce an AISIX relay.
direct_aisix_edges = {
    "sub2api-aisix": {"sub2api-netns", "aisix-netns"},
    "aisix-cpa": {"aisix-netns", "cpa-netns"},
    "enricher-aisix": {"models-enricher-netns", "aisix-netns"},
}
for name, expected_members in direct_aisix_edges.items():
    if network_members.get(name) != expected_members:
        raise SystemExit(f"invalid direct AISIX edge {name}: {sorted(network_members.get(name, set()))}")
for forbidden in ("aisix-relay", "sub2api-aisix-relay", "aisix-cpa-relay", "enricher-aisix-relay"):
    if forbidden in services:
        raise SystemExit(f"AISIX-specific relay is forbidden: {forbidden}")

aisix_service = services.get("aisix", {})
status_service = services.get("aisix-status", {})
for name, service in (("aisix", aisix_service), ("aisix-status", status_service)):
    if service.get("build") is not None:
        raise SystemExit(f"{name} must consume its fixed-version GHCR image, not build on the deployment host")
if aisix_service.get("network_mode") != "service:aisix-netns":
    raise SystemExit("AISIX must share the route-stripped aisix-netns namespace")
aisix_owner = services.get("aisix-netns", {})
if service_networks(aisix_owner) != set(direct_aisix_edges) | {"aisix-host-source", "aisix-status-admin"}:
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
if "TCP4-LISTEN:3001" not in relay_text or "TCP4:172.30.68.3:8080" not in relay_text:
    raise SystemExit("AISIX status relay must forward only port 3001 to the isolated renderer")

internal_membership_exceptions = {
    # The front APISIX, catalog bridge, and private table host share this
    # established catalog-source segment; every other internal net is a pair.
    "apisix-catalog-source": {"apisix-netns", "model-catalog-sidecar", "models-table-host-netns"},
}
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
    if source_name == "apisix-catalog-source":
        bridge = services.get("model-catalog-sidecar", {})
        if target_members != {"model-catalog-sidecar", "apisix-models-netns"}:
            raise SystemExit(f"invalid catalog target membership: {sorted(target_members)}")
        if str(bridge.get("user")) != "101:101" or bridge.get("read_only") is not True or bridge.get("ports"):
            raise SystemExit("catalog bridge must be non-root/read-only without ports")
        if {str(cap).upper() for cap in bridge.get("cap_drop", [])} != {"ALL"} or bridge.get("cap_add"):
            raise SystemExit("catalog bridge must drop all capabilities")
        if "no-new-privileges:true" not in bridge.get("security_opt", []):
            raise SystemExit("catalog bridge must set no-new-privileges")
        continue
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
    if source_name == "models-table-host-source":
        table_relay = services.get("models-table-relay", {})
        if table_relay.get("network_mode") != "service:models-table-host-netns" or not relay_is_hardened("models-table-relay"):
            raise SystemExit("models table host edge must use its hardened namespace relay")
        continue
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
