"""Read-only Linux process/network probes; no upstream requests or recovery actions."""
import datetime
import ipaddress
import json
from pathlib import Path
import re
import socket
import sys


def _routes(proc):
    return [line.split() for line in (proc / "net/route").read_text().splitlines()[1:]]


def _up(route):
    flags = int(route[3], 16)
    return bool(flags & 1) and not flags & 0x200


def _hex(address):
    return f"{int.from_bytes(socket.inet_aton(str(address)), sys.byteorder):08X}"


def _interfaces(proc):
    return {line.split(":", 1)[0].strip()
            for line in (proc / "net/dev").read_text().splitlines()[2:]}


def _descriptors(proc):
    for descriptor in (proc / "fd").iterdir():
        try:
            yield descriptor.name, str(descriptor.readlink())
        except FileNotFoundError:
            continue  # A connection can close while the process's descriptors are read.


def _relay(proc, command):
    # shortcut: IPv4 relay contracts only; extend when an IPv6 relay is deployed.
    matches = list(re.finditer(r"(?:TCP4?|OPENSSL)-LISTEN:(\d+)([^\s]*)", command))
    sources = [proc]
    expected = set()
    if matches:
        for match in matches:
            address = re.search(r"(?:^|,)bind=([^,\s\"]+)(?:,|$)", match[2])
            expected.add((_hex(address[1] if address else "0.0.0.0"), int(match[1])))
    else:
        # Match the existing literal-port supervisor; never evaluate arbitrary shell.
        loop = re.search(r"for port in ([0-9 ]+); do\s+socat \"TCP4-LISTEN:\$\{port\},bind=([0-9.]+),", command)
        if not loop:
            return {"ok": None, "reason": "listener_contract_unknown"}
        expected = {(_hex(loop[2]), int(port)) for port in loop[1].split()}
        children = (proc / "task" / proc.name / "children").read_text().split()
        if len(children) > 64:
            return {"ok": None, "reason": "supervisor_process_bound"}
        for pid in children:
            child = proc.parent / str(int(pid))
            parent = re.search(r"^PPid:\s*(\d+)$", (child / "status").read_text(), re.MULTILINE)
            if parent and parent[1] == proc.name:
                sources.append(child)
    owned = {link[8:-1] for source in sources for _, link in _descriptors(source)
             if link.startswith("socket:[")}
    listening = {}
    for line in (proc / "net/tcp").read_text().splitlines()[1:]:
        fields = line.split()
        host, port = fields[1].split(":")
        if fields[3] == "0A":
            listening.setdefault((host, int(port, 16)), set()).add(fields[9])
    ok = bool(expected) and all(listening.get(endpoint, set()) & owned for endpoint in expected)
    return {"ok": ok, "reason": "own_listener_present" if ok else "own_listener_missing",
            "expectedListeners": len(expected)}


def _netns(proc, networks):
    if not networks or any(not network.get("IPAddress") for network in networks):
        return {"ok": None, "reason": "network_contract_unknown"}
    routes = [route for route in _routes(proc) if _up(route)]
    default = any(route[1] == "00000000" and route[7] == "00000000" for route in routes)
    ipv6 = proc / "net/ipv6_route"
    if ipv6.exists():
        for line in ipv6.read_text().splitlines():
            fields = line.split()
            flags = int(fields[-2], 16)
            if fields[0] == "0" * 32 and fields[1] == "00" and flags & 1 and not flags & 0x200:
                default = True
    interfaces = _interfaces(proc) - {"lo"}
    connected = 0
    for endpoint in networks:
        network = ipaddress.IPv4Network(f"{endpoint['IPAddress']}/{endpoint['IPPrefixLen']}", strict=False)
        connected += any(route[0] in interfaces and route[1] == _hex(network.network_address)
                         and route[7] == _hex(network.netmask) for route in routes)
    return {"ok": not default and connected == len(networks),
            "reason": "namespace_routes_valid" if not default and connected == len(networks)
                      else "namespace_routes_invalid",
            "defaultRoute": default, "connectedNetworks": connected, "expectedNetworks": len(networks)}


def _tunnel(proc):
    # Deployed tun2proxy routing was verified: main-table default via its owned TUN.
    if "tun2proxy" not in (proc / "comm").read_text().strip():
        return {"ok": None, "reason": "tunnel_process_contract_unknown"}
    interfaces = _interfaces(proc)
    for descriptor, link in _descriptors(proc):
        if link != "/dev/net/tun":
            continue
        info = (proc / "fdinfo" / descriptor).read_text()
        match = re.search(r"^iff:\s*(\S+)", info, re.MULTILINE)
        if match and match[1] in interfaces:
            routed = any(route[0] == match[1] and route[1] == "00000000"
                         and route[7] == "00000000" and _up(route) for route in _routes(proc))
            if routed:
                return {"ok": True, "reason": "owned_tunnel_interface_and_route"}
    return {"ok": False, "reason": "tunnel_interface_or_route_missing"}


def worker_health(container, now):
    """Read a completed cycle, not an idle PID; never trigger a sync or print log text."""
    try:
        args = container["Config"].get("Cmd") or []
        path = Path("/proc") / str(container["State"]["Pid"]) / "root" / args[-1].lstrip("/")
        if path.stat().st_size > 1024 * 1024:
            return {"ok": None, "reason": "worker_config_bound"}
        interval = int(json.loads(path.read_text()).get("interval_seconds", 600))
        if interval <= 0 or container["HostConfig"]["LogConfig"]["Type"] != "json-file":
            return {"ok": None, "reason": "worker_progress_contract_unknown"}
        with open(container["LogPath"], "rb") as stream:
            stream.seek(0, 2)
            stream.seek(max(0, stream.tell() - 65536))
            lines = stream.read(65536).decode(errors="replace").splitlines()
        latest = None
        for line in lines:
            try:
                entry = json.loads(line)
                summary = json.loads(entry["log"])
                if isinstance(summary, dict) and all(type(summary.get(key)) is int
                        for key in ["updated", "unchanged", "failed", "unconfirmed", "skipped"]):
                    stamp = datetime.datetime.fromisoformat(entry["time"].replace("Z", "+00:00")).timestamp()
                    latest = (stamp, summary)
            except (ValueError, KeyError, TypeError):
                continue
        if latest is None:
            return {"ok": None, "reason": "completed_cycle_not_observed"}
        age = now - latest[0]
        alive = 0 <= age <= 2 * interval + 120
        return {"ok": alive, "reason": "completed_cycle_progress", "cycleAgeSeconds": round(age),
                "ready": alive and latest[1]["failed"] == 0 and latest[1]["unconfirmed"] == 0,
                "dependencyFailures": latest[1]["failed"], "unconfirmed": latest[1]["unconfirmed"]}
    except (OSError, ValueError, KeyError, IndexError, TypeError):
        return {"ok": None, "reason": "worker_progress_unavailable"}


def native_health(container, now):
    """Use only audited native probes, and refuse stale engine observations."""
    try:
        health = container["State"]["Health"]
        stamp = datetime.datetime.fromisoformat(health["Log"][-1]["End"].replace("Z", "+00:00")).timestamp()
        config = container["Config"]["Healthcheck"]
        budget = max(120, (2 * config.get("Interval", 0) + config.get("Timeout", 0)) / 1e9 + 30)
        age = now - stamp
        if age < 0 or age > budget:
            return {"ok": None, "reason": "native_probe_stale"}
        return {"ok": health["Status"] == "healthy", "reason": "audited_native_probe",
                "sampleAgeSeconds": round(age)}
    except (ValueError, KeyError, IndexError, TypeError):
        return {"ok": None, "reason": "native_probe_unavailable"}


def role_health(role, proc, command="", networks=()):
    """Unknown/inaccessible contracts stay unknown; never turn missing evidence green."""
    proc = Path(proc)
    try:
        if role == "relay":
            return _relay(proc, command)
        if role == "netns":
            return _netns(proc, networks)
        if role == "tunnel":
            return _tunnel(proc)
        return {"ok": None, "reason": "role_contract_unknown"}
    except (OSError, ValueError, IndexError, KeyError):
        return {"ok": None, "reason": "process_evidence_unavailable"}
