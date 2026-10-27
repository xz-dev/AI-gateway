#!/usr/bin/env python3
"""Fixture cases of the shared ai_ops preflight and of the recovery check run.

- a playbook that calls only ai_ops passes preflight;
- with the renderer directory removed, preflight fetches the pinned binary (from a
  local HTTP server here) and verifies its checksum;
- a tampered binary, a host Compose version that differs from the pin, and a remote
  Python without requests each stop preflight with the named cause;
- --tags verify still runs argument validation and preflight;
- recovery.yml --check writes no unit and copies no script.
"""
import functools
import hashlib
import http.server
import os
from pathlib import Path
import platform
import shutil
import subprocess
import sys
import tempfile
import threading
import unittest

import yaml

TESTS = Path(__file__).resolve().parent
ROOT = TESTS.parents[2]
sys.path.insert(0, str(TESTS))
import fixture_env  # noqa: E402

FAKE_COMPOSE = """#!/bin/sh
if [ "$1 $2" = "version --short" ]; then echo "${FAKE_HOST_COMPOSE:-2.40.3+ds1-0ubuntu1}"; exit 0; fi
echo "$@" >>"$(dirname "$0")/compose-calls"
"""


class Preflight(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="preflight-", dir="/var/tmp")
        self.root = Path(self.tmp.name)
        (self.root / "deploy").mkdir()
        (self.root / "private").mkdir()
        self.compose = self.root / "fake-compose"
        self.compose.write_text(FAKE_COMPOSE)
        self.compose.chmod(0o755)
        self.env_extra, self.extra = fixture_env.prepare(self.root)
        self.renderer = Path(self.extra["ai_ops_compose_renderer_dir"]) / "docker-compose"
        self.play = self.root / "only-ai-ops.yml"
        self.play.write_text(yaml.safe_dump([{"name": "Preflight only", "hosts": "gateway", "gather_facts": False,
                                              "roles": [{"role": "ai_ops"}]}]))
        self.inventory = self.root / "hosts.yml"
        self.inventory.write_text(yaml.safe_dump({"gateway": {"hosts": {"fixture": {"ansible_connection": "local"}}}}))

    def tearDown(self):
        self.tmp.cleanup()

    def run_play(self, play, *args, env=None, **variables):
        values = dict(self.extra, ai_ops_deploy_root=str(self.root / "deploy"), ai_ops_config_dir=str(self.root / "private"),
                      ai_ops_compose_cmd=str(self.compose), ai_ops_runtime="docker", **variables)
        path = self.root / "vars.yml"
        path.write_text(yaml.safe_dump(values))
        full = dict(os.environ, **self.env_extra, AI_OPS_SHIM_COMPOSE=str(self.compose), ANSIBLE_NOCOLOR="1",
                    ANSIBLE_LOCAL_TEMP=str(self.root / "ansible-tmp"), **(env or {}))
        result = subprocess.run(["ansible-playbook", "-i", str(self.inventory), str(play), "-e", "@" + str(path), *args],
                                env=full, text=True, capture_output=True, timeout=180)
        return result.returncode, result.stdout + result.stderr

    def test_only_ai_ops_passes_preflight(self):
        rc, text = self.run_play(self.play)
        self.assertEqual(rc, 0, text)
        self.assertIn("compose provider: " + str(self.compose), text)
        self.assertTrue((self.root / "deploy/.ai-ops-state").is_dir())

    def test_missing_renderer_is_fetched_and_verified(self):
        served = self.root / "release/v2.40.3"
        served.mkdir(parents=True)
        asset = served / ("docker-compose-linux-" + platform.machine())
        shutil.copyfile(self.renderer, asset)
        digest = hashlib.sha256(asset.read_bytes()).hexdigest()
        handler = functools.partial(http.server.SimpleHTTPRequestHandler, directory=str(self.root / "release"))
        handler.log_message = lambda *a: None
        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), handler)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        self.addCleanup(server.shutdown)
        base = f"http://127.0.0.1:{server.server_port}"
        shutil.rmtree(self.renderer.parent)
        rc, text = self.run_play(self.play, "--check", ai_ops_compose_renderer_base_url=base)
        self.assertEqual(rc, 0, text)
        self.assertEqual(hashlib.sha256(self.renderer.read_bytes()).hexdigest(), digest)
        # A served asset that does not match the pin is refused.
        shutil.rmtree(self.renderer.parent)
        asset.write_text(asset.read_text() + "# tampered\n")
        rc, text = self.run_play(self.play, "--check", ai_ops_compose_renderer_base_url=base)
        self.assertNotEqual(rc, 0, text)
        self.assertIn("checksum", text.lower())

    def test_tampered_renderer_stops_preflight(self):
        self.renderer.write_text(self.renderer.read_text() + "# tampered\n")
        rc, text = self.run_play(self.play, "--check")
        self.assertNotEqual(rc, 0, text)
        self.assertIn("Renderer checksum mismatch", text)

    def test_host_compose_version_must_equal_the_pin(self):
        rc, text = self.run_play(self.play, "--check", env={"FAKE_HOST_COMPOSE": "2.39.1"})
        self.assertNotEqual(rc, 0, text)
        self.assertIn("Host Compose 2.39.1", text)
        self.assertIn("differs from renderer pin 2.40.3", text)

    def test_missing_requests_stops_preflight(self):
        venv = self.root / "venv"
        subprocess.run([sys.executable, "-m", "venv", "--without-pip", str(venv)], check=True)
        rc, text = self.run_play(self.play, "--check", ansible_python_interpreter=str(venv / "bin/python"))
        self.assertNotEqual(rc, 0, text)
        self.assertIn("Missing prerequisite", text)
        self.assertIn("requests", text)

    def test_tag_filter_still_runs_validation_and_preflight(self):
        playbook = ROOT / "ops/ansible/playbooks/deploy-config.yml"
        for base in (self.root / "private", self.root / "deploy"):
            config = base / "services/routing/model-catalog/config.yaml"
            config.parent.mkdir(parents=True)
            config.write_text("unchanged\n")
        rc, text = self.run_play(playbook, "--check", "--tags", "verify", "-e", "component=model-catalog")
        self.assertEqual(rc, 0, text)
        for task in ("Validate the component", "ai_ops : Verify SSH connectivity", "ai_ops : Require host and renderer Compose",
                     "ai_ops_managed_file : Drift gate for model-catalog/config.yaml"):
            self.assertIn(task, text)
        self.assertNotIn("Upload model-catalog/config.yaml", text)

    def test_recovery_check_writes_no_unit(self):
        units, bin_dir = self.root / "units", self.root / "bin"
        units.mkdir()
        bin_dir.mkdir()
        # Read-only systemctl stand-in; check mode only queries unit state.
        systemctl = bin_dir / "systemctl"
        systemctl.write_text("#!/bin/sh\necho \"$@\" >>\"$(dirname \"$0\")/systemctl-calls\"\n"
                             "case \"$*\" in *show*) printf 'Id=ai-gateway-recovery.timer\\nLoadState=loaded\\n"
                             "ActiveState=inactive\\nUnitFileState=disabled\\n' ;; *is-enabled*) echo disabled; exit 1 ;; esac\n")
        systemctl.chmod(0o755)
        rc, text = self.run_play(ROOT / "ops/ansible/playbooks/recovery.yml", "--check", "--diff",
                                 env={"PATH": f"{bin_dir}:{os.environ['PATH']}"}, ai_ops_recovery_unit_dir=str(units))
        self.assertEqual(rc, 0, text)
        self.assertEqual(list(units.iterdir()), [])
        self.assertFalse((self.root / "deploy/ops/scripts/host").exists())
        self.assertIn("ai-gateway-recovery.timer", text)
        calls = (bin_dir / "systemctl-calls").read_text()
        self.assertNotRegex(calls, r"daemon-reload|\b(start|restart|enable)\b")


if __name__ == "__main__":
    unittest.main()
