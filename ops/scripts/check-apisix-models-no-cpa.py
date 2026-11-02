#!/usr/bin/env python3
"""Fail if apisix-models can reach CPA directly.

apisix-models is a constant pipe to AISIX and model-catalog; only AISIX falls
back to CPA (its single `*` row). The walk starts at apisix-models-netns, crosses
shared networks and socat relays, and stops at every other application
namespace (AISIX or model-catalog reaching CPA through their own edges is
legitimate). Reaching cpa-netns this way is a direct path and fails.
"""
import json
import sys
from collections import deque
from pathlib import Path

START = "apisix-models-netns"
CPA = "cpa-netns"


def load(path, fmt):
    text = Path(path).read_text(encoding="utf-8")
    if fmt == "json":
        return json.loads(text)
    import yaml

    return yaml.safe_load(text)


def networks_of(config):
    value = config.get("networks") or {}
    return set(value if isinstance(value, list) else value.keys())


def command_text(config):
    value = config.get("command") or []
    return value if isinstance(value, str) else " ".join(map(str, value))


def reachable(services):
    attached = {name: networks_of(config) for name, config in services.items()}
    relays = {name for name, config in services.items() if attached[name] and "LISTEN:" in command_text(config)}
    seen, reached, queue = {START}, set(), deque([START])
    while queue:
        node = queue.popleft()
        for network in attached.get(node, ()):
            for peer, peer_networks in attached.items():
                if peer in seen or network not in peer_networks:
                    continue
                seen.add(peer)
                if peer in relays:
                    queue.append(peer)
                else:
                    reached.add(peer)
    return reached


def violations(compose, routes_text):
    services = compose.get("services") or {}
    found = []
    if CPA in reachable(services):
        found.append(f"{START} reaches {CPA} without crossing an application namespace")
    hosts = (services.get(START) or {}).get("extra_hosts") or []
    names = hosts.keys() if isinstance(hosts, dict) else [str(host).split(":", 1)[0].split("=", 1)[0] for host in hosts]
    if "cpa" in names or "cli-proxy-api" in names:
        found.append(f"{START} must not resolve a CPA host name")
    environment = (services.get("apisix-models") or {}).get("environment") or {}
    keys = environment.keys() if isinstance(environment, dict) else [str(item).split("=", 1)[0] for item in environment]
    if "CPA_API_KEY" in keys:
        found.append("apisix-models must not carry CPA_API_KEY")
    if "cpa:" in routes_text or "8317" in routes_text:
        found.append("apisix-models apisix.yaml must not name a CPA node")
    return found


def main():
    if len(sys.argv) != 4:
        raise SystemExit("usage: check-apisix-models-no-cpa.py RENDERED_COMPOSE json|yaml APISIX_YAML")
    compose = load(sys.argv[1], sys.argv[2])
    found = violations(compose, Path(sys.argv[3]).read_text(encoding="utf-8"))
    if found:
        for item in found:
            print(f"apisix-models CPA cut violated: {item}", file=sys.stderr)
        raise SystemExit(1)
    print("apisix_models_cpa_path=absent")


if __name__ == "__main__":
    main()
