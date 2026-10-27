#!/usr/bin/env python3
"""Bounded local, non-inference checks. Restart only allowlisted application containers."""
import argparse
import datetime
import fcntl
import http.client
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import time
from urllib.parse import quote

from gateway_health import native_health, role_health, worker_health

PROJECT = "ai-gateway"
# APISIX checks are local routes, not checks of their upstream dependencies.
CHECKS = {
    "cli-proxy-api": (8317, "/v1/models", 200),
    "sub2api": (8080, "/health", 200),
    "openai-api-cache-keeper": (8085, "/healthz", 200),
    "responses-ws-sse-bridge": (8086, "/healthz", 200),
    "models-enricher": (8090, "/healthz", 200),
    "ai-sse-keepalive-proxy": (8080, "/healthz", 200),
    "apisix": (9080, "/healthz", 404),
    "apisix-models": (9080, "/healthz", 200),
}
# Observations only: never expand the application restart allowlist above.
OBSERVE_HTTP = {
    "cloudflared": ("127.0.0.1", 20241, "/ready", 200),
    "aisix-status": ("127.0.0.1", 8080, "/", 404),
    "aisix": ("172.30.68.2", 9090, "/metrics", 200),
    "model-catalog-sidecar": ("172.30.41.3", 8080, "/models-table/refresh", 403),
    "egress-proxy": ("172.30.18.2", 3128, "*", 400),
}
GRACE = 120
FAILURES = 3
COOLDOWN = 600
HOURLY_LIMIT = 3
STATE_DIR = Path("/var/lib/ai-gateway-recovery")
# CPA's loopback-only pprof listener inside its own network namespace.
CPA_PPROF_PORT = 8316
PPROF_PROFILES = ("heap", "goroutine")
PPROF_LIMIT = 16 * 1024 * 1024
PPROF_TIMEOUT = 10
PPROF_KEEP = 6


def probe_http(port, path, expected, key=None, catalog=False, timeout=3, host="127.0.0.1"):
    """Use a direct socket, never proxy environment variables or provider inference."""
    if catalog and not key:
        return {"ok": None, "reason": "catalog_credential_missing"}
    connection = http.client.HTTPConnection(host, port, timeout=timeout)
    try:
        headers = {"Host": "localhost"}
        if key:
            headers["Authorization"] = "Bearer " + key
        connection.request("GET", path, headers=headers)
        response = connection.getresponse()
        if catalog and response.status in (401, 403):
            return {"ok": None, "reason": "catalog_credential_rejected"}
        if response.status != expected:
            return {"ok": False, "reason": "unexpected_status", "status": response.status}
        if catalog:
            body = response.read(2 * 1024 * 1024 + 1)
            if len(body) > 2 * 1024 * 1024:
                return {"ok": False, "reason": "catalog_too_large"}
            document = json.loads(body)
            if not isinstance(document, dict) or not isinstance(document.get("data"), list):
                return {"ok": False, "reason": "invalid_catalog"}
        return {"ok": True, "status": response.status}
    except (OSError, http.client.HTTPException, ValueError) as exc:
        # Never print exception messages: HTTP errors can contain credentials or payloads.
        return {"ok": False, "reason": type(exc).__name__}
    finally:
        connection.close()


def probe_redis(port=6379):
    try:
        with socket.create_connection(("127.0.0.1", port), timeout=3) as connection:
            connection.sendall(b"*1\r\n$4\r\nPING\r\n")
            with connection.makefile("rb") as stream:
                reply = stream.readline(256)
        if reply.startswith((b"-NOAUTH", b"-WRONGPASS")):
            return {"ok": None, "reason": "redis_credential_required"}
        return {"ok": reply == b"+PONG\r\n", "reason": "strict_redis_pong"}
    except OSError as exc:
        return {"ok": False, "reason": type(exc).__name__}


def decision(previous, instance, ok, now, started):
    """Keep restart budgets across container recreation; reset consecutive failures."""
    history = [stamp for stamp in previous.get("restarts", []) if now - 3600 < stamp]
    count = previous.get("failures", 0) if previous.get("instance") == instance else 0
    state = {"instance": instance, "restarts": history, "failures": 0}
    if now < started + GRACE or ok is not False:
        return state, False
    state["failures"] = count + 1
    restart = (state["failures"] >= FAILURES and len(history) < HOURLY_LIMIT
               and (not history or now - history[-1] >= COOLDOWN))
    return state, restart


def docker_api(method, path, timeout=10):
    connection = http.client.HTTPConnection("localhost", timeout=timeout)
    connection.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    connection.sock.settimeout(timeout)
    try:
        connection.sock.connect("/var/run/docker.sock")
        connection.request(method, path)
        response = connection.getresponse()
        body = response.read(4 * 1024 * 1024 + 1)
        if response.status >= 400 or len(body) > 4 * 1024 * 1024:
            raise RuntimeError("docker_api_failed")
        return json.loads(body) if body else None
    finally:
        connection.close()


def inspect(names):
    return [docker_api("GET", "/containers/" + quote(name, safe="") + "/json") for name in names]


def deadline_expired(*_):
    raise TimeoutError("probe_deadline")


def namespace_probe(pid, request):
    # One single-threaded interpreter; restore its network namespace after every probe.
    with open("/proc/self/ns/net", "rb") as original, open(f"/proc/{pid}/ns/net", "rb") as target:
        handler = signal.signal(signal.SIGALRM, deadline_expired)
        signal.setitimer(signal.ITIMER_REAL, 6)
        try:
            os.setns(target.fileno(), 0)
            if request.get("protocol") == "redis":
                return probe_redis()
            return probe_http(**request)
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0)
            signal.signal(signal.SIGALRM, handler)
            os.setns(original.fileno(), 0)


def fetch_pprof(port, profile, target):
    """Stream one debug=1 text profile to disk; never gc=1/seconds, never hold it in memory."""
    connection = http.client.HTTPConnection("127.0.0.1", port, timeout=PPROF_TIMEOUT)
    try:
        connection.request("GET", f"/debug/pprof/{profile}?debug=1", headers={"Host": "localhost"})
        response = connection.getresponse()
        if response.status != 200:
            return {"profile": profile, "ok": False, "reason": "unexpected_status", "status": response.status}
        size = 0
        with open(target, "xb", opener=lambda path, flags: os.open(path, flags, 0o600)) as stream:
            while chunk := response.read(64 * 1024):
                size += len(chunk)
                if size > PPROF_LIMIT:
                    break
                stream.write(chunk)
        if size > PPROF_LIMIT:
            target.unlink()
            return {"profile": profile, "ok": False, "reason": "profile_too_large"}
        return {"profile": profile, "ok": True, "bytes": size}
    finally:
        connection.close()


def capture_pprof(pid, now):
    """Best-effort CPA evidence before a decided restart; failures never block recovery."""
    if not CPA_PPROF_PORT:
        return [{"ok": None, "reason": "pprof_port_unconfigured"}]
    directory = STATE_DIR / "pprof"
    directory.mkdir(mode=0o700, exist_ok=True)
    stamp = datetime.datetime.fromtimestamp(now, datetime.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    results = []
    with open("/proc/self/ns/net", "rb") as original, open(f"/proc/{pid}/ns/net", "rb") as target:
        handler = signal.signal(signal.SIGALRM, deadline_expired)
        try:
            os.setns(target.fileno(), 0)
            for profile in PPROF_PROFILES:
                signal.setitimer(signal.ITIMER_REAL, PPROF_TIMEOUT)
                try:
                    results.append(fetch_pprof(CPA_PPROF_PORT, profile, directory / f"{stamp}-{profile}.txt"))
                except (OSError, http.client.HTTPException) as exc:
                    (directory / f"{stamp}-{profile}.txt").unlink(missing_ok=True)
                    results.append({"profile": profile, "ok": False, "reason": type(exc).__name__})
                finally:
                    signal.setitimer(signal.ITIMER_REAL, 0)
        finally:
            signal.signal(signal.SIGALRM, handler)
            os.setns(original.fileno(), 0)
    stamps = sorted({path.name.split("-", 1)[0] for path in directory.glob("*.txt")})
    for old in stamps[:-PPROF_KEEP]:
        for path in directory.glob(old + "-*.txt"):
            path.unlink(missing_ok=True)
    return results


def coverage(containers, report):
    """Observe every running container; only CHECKS can ever authorize a restart."""
    cached = {container["Id"]: container for container in containers}
    applications = {row["service"]: row for row in report}
    inventory = docker_api("GET", "/containers/json?all=false")
    rows = []
    for item in inventory:
        service = item["Names"][0].lstrip("/")
        try:
            container = cached.get(item["Id"]) or inspect([item["Id"]])[0]
            service = (container["Config"].get("Labels") or {}).get("com.docker.compose.service") or service
            state = container["State"]
            proc = Path("/proc") / str(state["Pid"])
            if not state.get("Running") or state.get("Paused") or state.get("Restarting"):
                kind, result = "transition", {"ok": None, "reason": "not_running_or_operator_paused"}
            elif service in CHECKS:
                entry = applications.get(service, {})
                kind, result = "application", {"ok": entry.get("ok"), "reason": entry.get("reason", entry.get("action", "cycle_deferred"))}
            elif service.endswith("-relay"):
                kind = "own_relay_listener"
                result = role_health("relay", proc, command=" ".join(container["Config"].get("Cmd") or []))
            elif service.endswith("-netns"):
                kind = "namespace_interfaces_routes"
                result = role_health("netns", proc, networks=list(container["NetworkSettings"]["Networks"].values()))
            elif service == "provider-sidecar-tunnel":
                kind, result = "owned_tun_interface_route", role_health("tunnel", proc)
            elif service in OBSERVE_HTTP:
                host, port, path, expected = OBSERVE_HTTP[service]
                kind = "tunnel_readiness" if service == "cloudflared" else "local_http"
                result = namespace_probe(state["Pid"], {"host": host, "port": port, "path": path, "expected": expected})
                if service == "egress-proxy":
                    result["ready"] = native_health(container, time.time())["ok"]
            elif service == "redis":
                kind, result = "strict_redis_protocol", namespace_probe(state["Pid"], {"protocol": "redis"})
            elif service == "cpa-model-sync":
                kind, result = "worker_cycle_progress", worker_health(container, time.time())
            elif service in ("postgres", "provider-sidecar"):
                kind, result = "audited_native", native_health(container, time.time())
            else:
                kind, result = "unsupported", {"ok": None, "reason": "role_contract_unknown"}
            rows.append({"service": service, "kind": kind, "autoRecovery": service in CHECKS, **result})
        except (OSError, RuntimeError, ValueError, KeyError, http.client.HTTPException) as exc:
            rows.append({"service": service, "ok": None, "reason": type(exc).__name__})
            break  # Do not hammer a failing runtime; report incomplete inventory explicitly.
    return {"running": len(inventory), "observed": len(rows), "checks": rows}


def owned(container, service):
    labels = container.get("Config", {}).get("Labels") or {}
    return (labels.get("com.docker.compose.project") == PROJECT
            and labels.get("com.docker.compose.service") == service
            and container.get("Name") == f"/{PROJECT}-{service}-1")


def save(state, filename):
    temporary = filename.with_suffix(".tmp")
    with temporary.open("w", encoding="utf-8") as stream:
        os.chmod(temporary, 0o600)
        json.dump(state, stream)
        stream.flush()
        os.fsync(stream.fileno())
    temporary.replace(filename)


def startup_unit_health():
    """Observe a failed stack startup; never start/reset the unit or stopped containers."""
    try:
        result = subprocess.run(
            ["systemctl", "show", "ai-gateway.service", "--property=ActiveState", "--value"],
            capture_output=True, text=True, timeout=2, check=False)
        state = result.stdout.strip() if result.returncode == 0 else "unknown"
    except (OSError, subprocess.TimeoutExpired):
        state = "unknown"
    return {"unit": "ai-gateway.service", "state": state,
            "ok": False if state == "failed" else None if state == "unknown" else True,
            "action": "observe_only"}


def run(apply):
    names = [f"{PROJECT}-{service}-1" for service in CHECKS]
    containers = inspect(names)
    by_service = {}
    for container in containers:
        service = (container.get("Config", {}).get("Labels") or {}).get("com.docker.compose.service")
        if service in CHECKS and owned(container, service):
            by_service[service] = container
    if len(by_service) != len(CHECKS):
        raise RuntimeError("application ownership mismatch")
    # Existing service key stays in this process's memory, never argv or logs.
    # The cache keeper holds no CPA key; apisix-models still carries the client key.
    key = next((item.partition("=")[2] for item in by_service["apisix-models"]["Config"].get("Env") or []
                if item.startswith("CPA_API_KEY=")), None)
    filename = STATE_DIR / "state.json"
    state = json.loads(filename.read_text()) if filename.exists() else {}
    now = time.time()
    report = []
    for service, container in by_service.items():
        current = container["State"]
        if not current.get("Running") or current.get("Paused") or current.get("Restarting"):
            # Docker handles exits. Do not undo an operator's explicit stop/pause.
            report.append({"service": service, "action": "skip_not_running"})
            continue
        instance = container["Id"] + ":" + current["StartedAt"]
        started = datetime.datetime.fromisoformat(current["StartedAt"].replace("Z", "+00:00")).timestamp()
        if now - started < GRACE:
            state[service], _ = decision(state.get(service, {}), instance, None, now, started)
            report.append({"service": service, "action": "startup_grace"})
            continue
        port, path, expected = CHECKS[service]
        request = {"port": port, "path": path, "expected": expected,
                   "catalog": service == "cli-proxy-api"}
        if request["catalog"]:
            request["key"] = key
        try:
            result = namespace_probe(current["Pid"], request)
        except OSError:
            result = {"ok": None, "reason": "namespace_unavailable"}
        entry, restart = decision(state.get(service, {}), instance, result["ok"], now, started)
        state[service] = entry
        row = {"service": service, **result, "failures": entry["failures"], "action": "observe"}
        if restart:
            row["action"] = "would_restart"
            if apply:
                fresh = inspect([container["Id"]])[0]
                if (not owned(fresh, service) or fresh["State"]["StartedAt"] != current["StartedAt"]
                        or not fresh["State"].get("Running") or fresh["State"].get("Paused")):
                    row["action"] = "skip_changed_instance"
                else:
                    # Consume before issuing a possibly timing-out mutation. Never retry blindly.
                    entry["restarts"].append(now)
                    entry["failures"] = 0
                    save(state, filename)
                    if service == "cli-proxy-api":
                        try:
                            row["pprof"] = capture_pprof(current["Pid"], now)
                        except (OSError, TimeoutError, ValueError) as exc:
                            row["pprof"] = [{"ok": False, "reason": type(exc).__name__}]
                    try:
                        docker_api("POST", "/containers/" + quote(container["Id"], safe="") + "/restart?t=10", timeout=25)
                        row["action"] = "restarted"
                    except (OSError, RuntimeError, http.client.HTTPException):
                        row["action"] = "restart_outcome_unknown_cooldown"
        report.append(row)
        if apply and row["action"] in ("restarted", "restart_outcome_unknown_cooldown"):
            break  # One application per cycle; never restart the whole dependency chain.
    if apply:
        save(state, filename)
    observations = coverage(containers, report)
    print(json.dumps({"apply": apply, "utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
                      "checks": report, "coverage": observations,
                      "startup_unit": startup_unit_health()}), flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--apply", action="store_true", help="enable bounded application restarts")
    args = parser.parse_args()
    STATE_DIR.mkdir(mode=0o700, parents=True, exist_ok=True)
    with (STATE_DIR / "lock").open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            return
        run(args.apply)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, RuntimeError, http.client.HTTPException) as exc:
        print(json.dumps({"error": type(exc).__name__, "action": "cycle_failed_inspect_state_before_retry"}), file=sys.stderr)
        sys.exit(1)
