#!/usr/bin/env python3
"""Replay overload and cooldown on a local AISIX image; upstream is a loopback mock."""
import hashlib
from http.client import HTTPConnection
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import threading
import time
import uuid

import yaml

PUBLIC = "gpt-6.2-sol"
PRIMARY = "codex/" + PUBLIC
RETIRED = "xl/" + PUBLIC
KEY = "local-fallback-test"


def replay(image, source, old):
    calls = []

    class CPA(BaseHTTPRequestHandler):
        def do_POST(self):
            assert self.path == "/v1/responses", self.path
            model = json.loads(self.rfile.read(int(self.headers["Content-Length"])))["model"]
            calls.append(model)
            assert model in (PRIMARY, RETIRED), model
            status, code = (503, "server_is_overloaded") if model == PRIMARY else (400, "model_not_found")
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            if status == 503:
                self.send_header("Retry-After", "361")
            self.end_headers()
            self.wfile.write(json.dumps({"error": {"message": code, "code": code}}).encode())

        def log_message(self, *_args):
            pass

    resources = yaml.safe_load(Path(source).read_text())
    wanted = {PUBLIC, PRIMARY, "*"} | ({RETIRED} if old else set())
    models = [m for m in resources["models"] if m["display_name"] in wanted]
    assert {m["display_name"] for m in models} == wanted
    assert any(m["display_name"] == RETIRED for m in resources["models"]) == old
    route = next(m["routing"] for m in models if m["display_name"] == PUBLIC)
    assert [t["model"] for t in route["targets"]] == ([PRIMARY, RETIRED] if old else [PRIMARY])
    with ThreadingHTTPServer(("127.0.0.1", 0), CPA) as server, tempfile.TemporaryDirectory(
        prefix="aisix-overload-", dir="/var/tmp"
    ) as work:
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            port = listener.getsockname()[1]
            with socket.socket() as admin_listener:
                admin_listener.bind(("127.0.0.1", 0))
                admin_port = admin_listener.getsockname()[1]
        work = Path(work)
        cfg, res = work / "config.yaml", work / "resources.yaml"
        cfg.write_text(yaml.safe_dump({"proxy": {"addr": f"127.0.0.1:{port}"},
                                       "admin": {"addr": f"127.0.0.1:{admin_port}", "admin_keys": [KEY]},
                                       "observability": {"metrics": {"prometheus": {"enabled": False}}},
                                       "resources_file": "/etc/aisix/resources.yaml"}))
        res.write_text(yaml.safe_dump({
            "_format_version": resources["_format_version"], "models": models,
            "provider_keys": [{"display_name": "cpa-pool", "provider": "openai", "api_key": KEY,
                               "api_base": f"http://127.0.0.1:{server.server_port}/v1"}],
            "api_keys": [{"display_name": KEY, "key_hash": hashlib.sha256(KEY.encode()).hexdigest(),
                          "allowed_models": ["*"]}],
        }))
        cfg.chmod(0o444)
        res.chmod(0o444)
        name = f"aisix-overload-{uuid.uuid4().hex[:12]}"

        def request(method, path, body=None, *, server_port=port):
            conn = HTTPConnection("127.0.0.1", server_port, timeout=3)
            try:
                conn.request(method, path, body=body,
                             headers={"Authorization": "Bearer " + KEY, "Content-Type": "application/json"})
                with conn.getresponse() as resp:
                    return resp.status, resp.read()
            finally:
                conn.close()

        try:
            subprocess.run(["docker", "run", "--detach", "--pull=never", "--network=host",
                            "--stop-signal=SIGKILL", "--name", name,
                            "--volume", f"{cfg}:/etc/aisix/config.yaml:ro",
                            "--volume", f"{res}:/etc/aisix/resources.yaml:ro", image],
                           check=True, stdout=subprocess.DEVNULL, timeout=10)
            for _ in range(40):
                try:
                    if request("GET", "/v1/models")[0] == 200:
                        break
                except OSError:
                    pass
                time.sleep(0.25)
            else:
                raise AssertionError("local AISIX never became ready")
            body = json.dumps({"model": PUBLIC, "input": "fixture", "stream": True})
            first, first_body = request("POST", "/v1/responses", body)
            assert calls == ([PRIMARY, RETIRED] if old else [PRIMARY]), calls
            assert first == (400 if old else 502), (first, first_body)
            if not old:
                status, payload = request("GET", "/admin/v1/models/status", server_port=admin_port)
                assert status == 200, (status, payload)
                primary = next(row for row in json.loads(payload) if row["display_name"] == PRIMARY)
                assert primary["status"].lower() == "cooldown", primary
                second, second_body = request("POST", "/v1/responses", body)
                # The deployed router can retry its only candidate even while cooling.
                assert second == 502 and calls == [PRIMARY, PRIMARY], (second, second_body, calls)
            print(f"{'baseline' if old else 'candidate'}: first={first}; " +
                  ("old XL mock was called" if old else f"cooling={second}; old XL mock not called"))
        except Exception:
            subprocess.run(["docker", "logs", name], check=False, timeout=10)
            raise
        finally:
            subprocess.run(["docker", "rm", "-f", name], stdout=subprocess.DEVNULL, check=True, timeout=25)
            server.shutdown()
            thread.join()


if __name__ == "__main__":
    image, before, after = sys.argv[1:]
    replay(image, before, old=True)
    replay(image, after, old=False)
