#!/usr/bin/env python3
"""Offline HTTP and restart-policy regressions; never invokes Docker or a provider."""
import http.server
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import threading
import time
import unittest
from unittest import mock
import contextlib
import io
import tempfile

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / "ops/scripts/host"))
import gateway_health as health

SPEC = importlib.util.spec_from_file_location("recovery", ROOT / "ops/scripts/host/gateway-recovery.py")
recovery = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(recovery)


class Handler(http.server.BaseHTTPRequestHandler):
    status = 200
    body = b'{"data": []}'
    delay = 0
    catalog_delay = 0

    def do_GET(self):
        time.sleep(type(self).catalog_delay if self.path == "/v1/models" else type(self).delay)
        self.send_response(type(self).status)
        self.end_headers()
        try:
            self.wfile.write(type(self).body)
        except BrokenPipeError:
            pass

    def log_message(self, *args):
        pass


class RecoveryTests(unittest.TestCase):
    def setUp(self):
        Handler.status, Handler.body, Handler.delay, Handler.catalog_delay = 200, b'{"data": []}', 0, 0
        self.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.port = self.server.server_port
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()

    def test_strict_http_status(self):
        for status in (200, 502, 503):
            Handler.status = status
            with self.subTest(status=status):
                self.assertEqual(recovery.probe_http(self.port, "/healthz", 200)["ok"], status == 200)

    def test_catalog_requires_real_success_and_valid_json(self):
        for status, body, expected in [(200, b'{"data": []}', True), (200, b'{}', False),
                                        (200, b'not-json', False), (502, b'', False),
                                        (503, b'', False), (401, b'', None), (403, b'', None)]:
            Handler.status, Handler.body = status, body
            with self.subTest(status=status, body=body):
                self.assertIs(recovery.probe_http(self.port, "/v1/models", 200,
                                                 key="offline-fixture", catalog=True)["ok"], expected)
        self.assertIsNone(recovery.probe_http(self.port, "/v1/models", 200, catalog=True)["ok"])

    def test_unresponsive_or_closed_listener_fails(self):
        Handler.delay = 0.2
        self.assertFalse(recovery.probe_http(self.port, "/healthz", 200, timeout=0.03)["ok"])
        self.server.shutdown()
        self.server.server_close()
        self.assertFalse(recovery.probe_http(self.port, "/healthz", 200, timeout=0.03)["ok"])

    def test_actual_compose_health_command_rejects_5xx(self):
        # Private production overrides are not repository test fixtures.
        text = (ROOT / "compose.yaml").read_text()
        block = text.split("\n  apisix-models:\n", 1)[1].split("\n  openai-api-cache-keeper:\n", 1)[0]
        command = json.loads(re.search(r'test: \[CMD, /bin/bash, -ec, ("[^\n]+")\]', block)[1])
        command = command.replace("$$", "$")  # Compose expands the escaped shell variable.
        command = command.replace("127.0.0.1/9080", "127.0.0.1/" + str(self.port))
        for status in (200, 502, 503):
            Handler.status = status
            with self.subTest(status=status):
                result = subprocess.run(["bash", "-ec", command], capture_output=True, timeout=2)
                self.assertEqual(result.returncode == 0, status == 200)
        Handler.delay = 0.2
        with self.assertRaises(subprocess.TimeoutExpired):
            subprocess.run(["bash", "-ec", command], capture_output=True, timeout=0.03)
        Handler.delay = 0

    def test_public_apisix_probes_use_local_unmatched_route_not_authenticated_catalog(self):
        self.assertEqual(recovery.CHECKS['apisix'], (9080, '/healthz', 404))
        text = (ROOT / 'compose.yaml').read_text()
        block = text.split('\n  apisix:\n', 1)[1].split('\n  operator-host-netns:\n', 1)[0]
        command = json.loads(re.search(r'test: \[CMD, /bin/bash, -ec, ("[^\n]+")\]', block)[1])
        self.assertIn('GET /healthz HTTP/1.0', command)
        self.assertNotIn('/v1/models', command)
        command = command.replace('$$', '$').replace('127.0.0.1/9080', '127.0.0.1/' + str(self.port))
        paths = []
        original = Handler.do_GET
        def record(handler):
            paths.append(handler.path)
            original(handler)
        with mock.patch.object(Handler, 'do_GET', record):
            for status in (404, 401, 200, 502, 503):
                Handler.status = status
                with self.subTest(status=status):
                    result = subprocess.run(['bash', '-ec', command], capture_output=True, timeout=2)
                    self.assertEqual(result.returncode == 0, status == 404)
                    self.assertEqual(recovery.probe_http(self.port, recovery.CHECKS['apisix'][1], 404)['ok'], status == 404)
        self.assertEqual(paths, ['/healthz'] * 10)

    def test_failure_threshold_grace_reset_cooldown_and_hourly_budget(self):
        state = {}
        for offset in range(3):
            state, restart = recovery.decision(state, "id:started", False, 1000 + offset * 30, 0)
            self.assertEqual(restart, offset == 2)
        for ok in (True, None):
            new, restart = recovery.decision(state, "id:started", ok, 1100, 0)
            self.assertEqual(new["failures"], 0)
            self.assertFalse(restart)
        _, restart = recovery.decision(state, "id:started", False, 1100, 1090)
        self.assertFalse(restart)
        state["restarts"] = [1000]
        _, restart = recovery.decision(state, "id:started", False, 1200, 0)
        self.assertFalse(restart)
        _, restart = recovery.decision(state, "id:started", False, 1700, 0)
        self.assertTrue(restart)
        changed, restart = recovery.decision(state, "new:started", False, 1700, 0)
        self.assertFalse(restart)
        self.assertEqual(changed["failures"], 1)
        self.assertEqual(changed["restarts"], [1000])
        state["restarts"] = [1000, 1600, 2200]
        _, restart = recovery.decision(state, "id:started", False, 3000, 0)
        self.assertFalse(restart)
        _, restart = recovery.decision(state, "id:started", False, 5000, 0)
        self.assertTrue(restart)

    def test_restart_execution_is_single_target_and_unknown_outcome_consumes_budget(self):
        started = "1970-01-01T00:00:01+00:00"
        containers = [{"Id": service, "Name": f"/ai-gateway-{service}-1", "Config": {
            "Labels": {"com.docker.compose.project": "ai-gateway", "com.docker.compose.service": service},
            "Env": ["CPA_CLIENT_KEY=offline-sensitive-fixture"]},
            "State": {"Running": True, "Pid": 123, "StartedAt": started}}
            for service in recovery.CHECKS]
        attempts = []

        def inspect(names):
            return containers if len(names) > 1 else [containers[0]]

        def probe(pid, request):
            return {"ok": request["path"] != "/v1/models" or request["expected"] != 200}

        def api(method, path, timeout=10):
            if method == "GET":
                return []
            attempts.append((method, path))
            raise TimeoutError("unknown_restart_outcome")

        output = io.StringIO()
        with tempfile.TemporaryDirectory() as directory, \
                mock.patch.object(recovery, "STATE_DIR", Path(directory)), \
                mock.patch.object(recovery, "inspect", side_effect=inspect), \
                mock.patch.object(recovery, "namespace_probe", side_effect=probe), \
                mock.patch.object(recovery, "docker_api", side_effect=api), \
                mock.patch.object(recovery.time, "time", return_value=1000), \
                contextlib.redirect_stdout(output):
            for _ in range(4):
                recovery.run(True)
            state = json.loads((Path(directory) / "state.json").read_text())
        self.assertEqual(len(attempts), 1)
        self.assertEqual(attempts[0], ("POST", "/containers/cli-proxy-api/restart?t=10"))
        self.assertEqual(state["cli-proxy-api"]["restarts"], [1000])
        self.assertIn("restart_outcome_unknown_cooldown", output.getvalue())
        self.assertNotIn("offline-sensitive-fixture", output.getvalue())

    def catalog_keys(self, environments):
        started = "1970-01-01T00:00:01+00:00"
        containers = [{"Id": service, "Name": f"/ai-gateway-{service}-1", "Config": {
            "Labels": {"com.docker.compose.project": "ai-gateway", "com.docker.compose.service": service},
            "Env": environments.get(service, [])},
            "State": {"Running": True, "Pid": 123, "StartedAt": started}}
            for service in recovery.CHECKS]
        keys = []

        def probe(pid, request):
            if request.get("catalog"):
                keys.append(request.get("key"))
            return {"ok": True}

        with tempfile.TemporaryDirectory() as directory, \
                mock.patch.object(recovery, "STATE_DIR", Path(directory)), \
                mock.patch.object(recovery, "inspect", return_value=containers), \
                mock.patch.object(recovery, "namespace_probe", side_effect=probe), \
                mock.patch.object(recovery, "docker_api", return_value=[]), \
                mock.patch.object(recovery.time, "time", return_value=1000), \
                contextlib.redirect_stdout(io.StringIO()):
            recovery.run(False)
        return keys

    def test_catalog_key_comes_from_model_catalog_only(self):
        # apisix-models no longer holds a CPA credential; a stale one must never be read.
        keys = self.catalog_keys({
            "model-catalog": ["CPA_CLIENT_KEY=catalog-key", "CPA_API_KEY=other"],
            "apisix-models": ["CPA_API_KEY=stale-models-key", "CPA_CLIENT_KEY=stale-models-key"],
        })
        self.assertEqual(keys, ["catalog-key"])
        self.assertIn("openai-api-cache-keeper", recovery.CHECKS)
        self.assertNotIn("response-keeper", recovery.CHECKS)

    def test_missing_catalog_key_fails_closed(self):
        self.assertEqual(self.catalog_keys({"model-catalog": ["CPA_API_KEY=not-the-client-key"]}), [None])
        self.assertIsNone(recovery.probe_http(self.port, "/v1/models", 200, catalog=True)["ok"])
        source = (ROOT / "ops/scripts/host/gateway-recovery.py").read_text()
        self.assertNotIn('by_service["apisix-models"]', source)

    def test_failed_stack_startup_is_observation_only(self):
        for state in ("active", "failed"):
            with self.subTest(state=state), mock.patch.object(recovery.subprocess, "run", return_value=
                    subprocess.CompletedProcess([], 0, state + "\n", "")) as run:
                result = recovery.startup_unit_health()
                self.assertEqual(result["state"], state)
                self.assertEqual(result["ok"], state != "failed")
                self.assertEqual(result["action"], "observe_only")
                self.assertEqual(run.call_args.args[0], ["systemctl", "show", "ai-gateway.service",
                                                        "--property=ActiveState", "--value"])
                self.assertEqual(run.call_count, 1)
        with mock.patch.object(recovery.subprocess, "run", side_effect=FileNotFoundError):
            self.assertIsNone(recovery.startup_unit_health()["ok"])
        with mock.patch.object(recovery.subprocess, "run", side_effect=subprocess.TimeoutExpired([], 2)):
            self.assertIsNone(recovery.startup_unit_health()["ok"])

    def test_bridge_recovery_probes_8086_and_never_starts_an_exited_bridge(self):
        started = "1970-01-01T00:00:01+00:00"
        containers = [{"Id": service, "Name": f"/ai-gateway-{service}-1", "Config": {
            "Labels": {"com.docker.compose.project": "ai-gateway", "com.docker.compose.service": service},
            "Env": []}, "State": {"Running": True, "Pid": 123, "StartedAt": started}}
            for service in recovery.CHECKS]
        bridge = next(row for row in containers if row["Id"] == "responses-ws-sse-bridge")
        probes, restarts = [], []

        def probe(pid, request):
            probes.append(request)
            return {"ok": request["port"] != 8086}

        def api(method, path, timeout=10):
            if method == "POST":
                restarts.append(path)
            return []

        with tempfile.TemporaryDirectory(dir="/var/tmp") as directory, \
                mock.patch.object(recovery, "STATE_DIR", Path(directory)), \
                mock.patch.object(recovery, "inspect", side_effect=lambda names: containers if len(names) > 1 else [bridge]), \
                mock.patch.object(recovery, "namespace_probe", side_effect=probe), \
                mock.patch.object(recovery, "docker_api", side_effect=api), \
                mock.patch.object(recovery, "startup_unit_health", return_value={"state": "failed", "action": "observe_only"}), \
                mock.patch.object(recovery.time, "time", return_value=1000), \
                contextlib.redirect_stdout(io.StringIO()) as output:
            for _ in range(3):
                recovery.run(True)
            self.assertEqual(restarts, ["/containers/responses-ws-sse-bridge/restart?t=10"])
            request = next(row for row in probes if row["port"] == 8086)
            self.assertEqual((request["path"], request["expected"]), ("/healthz", 200))
            bridge["State"]["Running"] = False
            recovery.run(True)
            report = json.loads(output.getvalue().splitlines()[-1])
        self.assertEqual(len(restarts), 1)
        row = next(row for row in report["checks"] if row["service"] == "responses-ws-sse-bridge")
        self.assertEqual(row["action"], "skip_not_running")
        self.assertEqual(report["startup_unit"]["state"], "failed")

    def test_pprof_capture_streams_bounded_files_and_keeps_recent(self):
        Handler.body = b"heap profile\n"
        with tempfile.TemporaryDirectory(dir="/var/tmp") as directory, \
                mock.patch.object(recovery, "STATE_DIR", Path(directory)), \
                mock.patch.object(recovery, "CPA_PPROF_PORT", self.port), \
                mock.patch.object(recovery, "PPROF_KEEP", 2), \
                mock.patch.object(recovery.os, "setns"):
            for now in (1000, 2000, 3000):
                results = recovery.capture_pprof(os.getpid(), now)
                self.assertEqual([row["ok"] for row in results], [True, True])
            files = sorted(path.name for path in (Path(directory) / "pprof").iterdir())
            self.assertEqual(len(files), 4)
            self.assertFalse(any(name.startswith("19700101T001640Z") for name in files))
            self.assertEqual(oct((Path(directory) / "pprof" / files[0]).stat().st_mode & 0o777), "0o600")
            with mock.patch.object(recovery, "PPROF_LIMIT", 4):
                results = recovery.capture_pprof(os.getpid(), 4000)
            self.assertEqual({row["reason"] for row in results}, {"profile_too_large"})
            self.assertFalse(list((Path(directory) / "pprof").glob("19700101T010640Z-*")))
            with mock.patch.object(recovery, "CPA_PPROF_PORT", None):
                self.assertIsNone(recovery.capture_pprof(os.getpid(), 5000)[0]["ok"])

    def test_cpa_restart_captures_pprof_first_and_capture_failure_does_not_block(self):
        started = "1970-01-01T00:00:01+00:00"
        containers = [{"Id": service, "Name": f"/ai-gateway-{service}-1", "Config": {
            "Labels": {"com.docker.compose.project": "ai-gateway", "com.docker.compose.service": service},
            "Env": []}, "State": {"Running": True, "Pid": 123, "StartedAt": started}}
            for service in recovery.CHECKS]
        order = []

        def api(method, path, timeout=10):
            if method == "POST":
                order.append("restart")
            return []

        def capture(pid, now):
            order.append("pprof")
            raise TimeoutError("probe_deadline")

        with tempfile.TemporaryDirectory(dir="/var/tmp") as directory, \
                mock.patch.object(recovery, "STATE_DIR", Path(directory)), \
                mock.patch.object(recovery, "inspect", side_effect=lambda names: containers if len(names) > 1 else [containers[0]]), \
                mock.patch.object(recovery, "namespace_probe", side_effect=lambda pid, request: {"ok": request["path"] != "/v1/models" or request["expected"] != 200}), \
                mock.patch.object(recovery, "docker_api", side_effect=api), \
                mock.patch.object(recovery, "capture_pprof", side_effect=capture), \
                mock.patch.object(recovery.time, "time", return_value=1000), \
                contextlib.redirect_stdout(io.StringIO()) as output:
            for _ in range(3):
                recovery.run(True)
        self.assertEqual(order, ["pprof", "restart"])
        self.assertIn('"action": "restarted"', output.getvalue())
        self.assertIn("TimeoutError", output.getvalue())

    def test_live_health_but_stuck_catalog_triggers_only_after_three_failures(self):
        Handler.catalog_delay = 0.2
        state = {}
        for attempt in range(3):
            self.assertTrue(self.thread.is_alive())
            self.assertTrue(recovery.probe_http(self.port, "/healthz", 200, timeout=0.03)["ok"])
            result = recovery.probe_http(self.port, "/v1/models", 200,
                                         catalog=True, key="offline-fixture", timeout=0.03)
            self.assertFalse(result["ok"])
            state, restart = recovery.decision(state, "same-live-process", result["ok"], 1000 + attempt * 30, 0)
            self.assertEqual(restart, attempt == 2)

    def test_namespace_probe_restores_namespace_and_wall_deadline(self):
        Handler.catalog_delay = 0.2
        set_timer = recovery.signal.setitimer

        def arm(kind, seconds):
            return set_timer(kind, min(seconds, 0.03))

        with mock.patch.object(recovery.os, "setns") as setns, \
                mock.patch.object(recovery.signal, "setitimer", side_effect=arm):
            result = recovery.namespace_probe(os.getpid(), {"port": self.port, "path": "/v1/models",
                                                "expected": 200, "catalog": True, "key": "fixture"})
        self.assertFalse(result["ok"])
        self.assertEqual(setns.call_count, 2)
        self.assertEqual(recovery.signal.getitimer(recovery.signal.ITIMER_REAL), (0.0, 0.0))

    def test_redis_requires_pong_and_does_not_treat_auth_error_as_process_failure(self):
        for reply, expected in [(b"+PONG\r\n", True), (b"-LOADING fixture\r\n", False),
                                (b"-NOAUTH fixture\r\n", None)]:
            client, peer = recovery.socket.socketpair()
            with self.subTest(reply=reply), client, peer, mock.patch.object(recovery.socket, "create_connection", return_value=client):
                peer.sendall(reply)
                result = recovery.probe_redis()
                self.assertIs(result["ok"], expected)
                self.assertEqual(peer.recv(64), b"*1\r\n$4\r\nPING\r\n")

    def test_no_database_or_network_owner_can_be_restarted(self):
        for service in recovery.CHECKS:
            self.assertNotIn(service, {"postgres", "redis", "provider-sidecar", "provider-sidecar-tunnel"})
            self.assertNotIn("netns", service)
        self.assertFalse(recovery.owned({"Name": "/ai-gateway-cli-proxy-api-1", "Config": {
            "Labels": {"com.docker.compose.project": "other", "com.docker.compose.service": "cli-proxy-api"}}},
            "cli-proxy-api"))


class RoleHealthTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(dir="/var/tmp")
        self.addCleanup(self.directory.cleanup)
        self.proc = Path(self.directory.name) / "100"
        self.proc.mkdir()
        for path in ["net", "fd", "fdinfo"]:
            (self.proc / path).mkdir()
        (self.proc / "comm").write_text("socat\n")
        (self.proc / "net/dev").write_text("header\nheader\n lo: 0\n eth0: 0\n tun0: 0\n")
        (self.proc / "net/route").write_text("header\neth0 00011EAC 00000000 0001 0 0 0 FCFFFFFF 0 0 0\n")
        (self.proc / "net/ipv6_route").write_text("")
        (self.proc / "net/tcp").write_text("header\n0: 0100007F:207D 00000000:0000 0A 0:0 00:0 0 0 0 42\n")
        (self.proc / "fd/3").symlink_to("socket:[42]")

    def test_relay_requires_own_socket_and_exact_configured_bind(self):
        command = "TCP4-LISTEN:8317,bind=127.0.0.1,reuseaddr,fork TCP:172.30.1.2:8317"
        self.assertTrue(health.role_health("relay", self.proc, command=command)["ok"])
        # Same shared namespace and port, but another process owns the socket.
        path = self.proc / "net/tcp"
        path.write_text(path.read_text().replace(" 42\n", " 99\n"))
        self.assertFalse(health.role_health("relay", self.proc, command=command)["ok"])
        path.write_text(path.read_text().replace(" 99\n", " 42\n").replace("0100007F:", "00000000:"))
        self.assertFalse(health.role_health("relay", self.proc, command=command)["ok"])

    def test_supervised_relay_requires_every_port_and_real_child_ownership(self):
        command = 'for port in 8317 8085; do\n  socat "TCP4-LISTEN:${port},bind=127.0.0.1,reuseaddr,fork" "TCP4:172.30.1.2:${port}" &\ndone\nwait -n'
        task = self.proc / "task/100"
        task.mkdir(parents=True)
        (task / "children").write_text("101")
        child = self.proc.parent / "101"
        (child / "fd").mkdir(parents=True)
        (child / "status").write_text("PPid:\t100\n")
        (child / "fd/4").symlink_to("socket:[43]")
        self.assertFalse(health.role_health("relay", self.proc, command=command)["ok"])
        tcp = self.proc / "net/tcp"
        tcp.write_text(tcp.read_text() + "1: 0100007F:1F95 00000000:0000 0A 0:0 00:0 0 0 0 43\n")
        self.assertTrue(health.role_health("relay", self.proc, command=command)["ok"])
        (child / "status").write_text("PPid:\t999\n")
        self.assertFalse(health.role_health("relay", self.proc, command=command)["ok"])

    def test_namespace_needs_connected_routes_and_no_default_escape(self):
        endpoints = [{"IPAddress": "172.30.1.2", "IPPrefixLen": 30}]
        route = self.proc / "net/route"
        original = route.read_text()
        self.assertTrue(health.role_health("netns", self.proc, networks=endpoints)["ok"])
        route.write_text(original + "eth0 00000000 01011EAC 0003 0 0 0 00000000 0 0 0\n")
        self.assertFalse(health.role_health("netns", self.proc, networks=endpoints)["ok"])
        route.write_text("header\n")
        self.assertFalse(health.role_health("netns", self.proc, networks=endpoints)["ok"])
        route.write_text(original)
        ipv6 = self.proc / "net/ipv6_route"
        prefix = "0" * 32 + " 00 " + "0" * 32 + " 00 " + "0" * 32
        ipv6.write_text(prefix + " 00000000 00000001 00000000 00200200 lo\n")
        self.assertTrue(health.role_health("netns", self.proc, networks=endpoints)["ok"])
        ipv6.write_text(prefix + " 00000000 00000001 00000000 00000001 eth0\n")
        self.assertFalse(health.role_health("netns", self.proc, networks=endpoints)["ok"])

    def test_tunnel_needs_owned_tun_interface_and_active_default_route(self):
        (self.proc / "comm").write_text("tun2proxy-bin\n")
        (self.proc / "fd/4").symlink_to("/dev/net/tun")
        (self.proc / "fdinfo/4").write_text("iff:\ttun0\n")
        route = self.proc / "net/route"
        self.assertFalse(health.role_health("tunnel", self.proc)["ok"])
        route.write_text(route.read_text() + "tun0 00000000 00000000 0001 0 0 0 00000000 0 0 0\n")
        self.assertTrue(health.role_health("tunnel", self.proc)["ok"])
        # Interface/route remain in the namespace, but this process lost its TUN handle.
        (self.proc / "fdinfo/4").write_text("pos:\t0\n")
        self.assertFalse(health.role_health("tunnel", self.proc)["ok"])

    def test_native_health_refuses_stale_engine_success(self):
        container = {"Config": {"Healthcheck": {"Interval": 10000000000}},
                     "State": {"Health": {"Status": "healthy", "Log": [{"End": "1970-01-01T00:15:00Z"}]}}}
        self.assertTrue(health.native_health(container, 1000)["ok"])
        self.assertIsNone(health.native_health(container, 2000)["ok"])

    def test_worker_cycle_can_be_alive_but_dependencies_not_ready(self):
        (self.proc / "root").mkdir()
        (self.proc / "root/config.json").write_text('{"interval_seconds":600}')
        log = self.proc / "worker.log"
        summary = {"updated": 0, "unchanged": 1, "failed": 4, "unconfirmed": 0, "skipped": 0}
        log.write_text(json.dumps({"time": "1970-01-01T00:15:00Z", "log": json.dumps(summary)}) + "\n")
        container = {"Config": {"Cmd": ["/config.json"]}, "State": {"Pid": 100},
                     "HostConfig": {"LogConfig": {"Type": "json-file"}}, "LogPath": str(log)}
        with mock.patch.object(health, "Path", return_value=self.proc.parent):
            result = health.worker_health(container, 1000)
            self.assertTrue(result["ok"])
            self.assertFalse(result["ready"])
            self.assertEqual(result["dependencyFailures"], 4)
            self.assertFalse(health.worker_health(container, 4000)["ok"])

    def test_missing_or_unknown_contract_is_not_healthy(self):
        self.assertIsNone(health.role_health("relay", self.proc, command="sleep infinity")["ok"])
        self.assertIsNone(health.role_health("netns", self.proc)["ok"])
        self.assertIsNone(health.role_health("relay", self.proc / "missing", command="TCP-LISTEN:8317")["ok"])


if __name__ == "__main__":
    unittest.main()
