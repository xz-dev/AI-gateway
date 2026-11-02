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
FAKE_ENGINE = """#!/bin/sh
name=${0##*/}
echo "$name $*" >>"$FAKE_ENGINE_LOG"
case "$name:$1:$2" in
  docker:info:) [ "$FAKE_DOCKER" = 1 ] ;;
  docker:compose:version) [ "$FAKE_DOCKER" = 1 ] || exit 1; echo 2.40.3 ;;
  podman:info:) [ "$FAKE_PODMAN" = 1 ] ;;
  podman:compose:version) [ "$FAKE_PODMAN_PLUGIN" = 1 ] || exit 1; echo 2.40.3 ;;
  podman-compose:version:--short) echo 2.40.3 ;;
  *) echo 'unexpected fixture runtime call' >&2; exit 98 ;;
esac
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
                                              "roles": [{"role": "ai_ops"}], "tasks": [{"name": "Selected runtime",
                                              "ansible.builtin.debug": {"msg": "runtime engine: {{ ai_ops_runtime_cli }}"}}]}]))
        self.inventory = self.root / "hosts.yml"
        self.inventory.write_text(yaml.safe_dump({"gateway": {"hosts": {"fixture": {"ansible_connection": "local"}}}}))

    def tearDown(self):
        self.tmp.cleanup()

    def run_play(self, play, *args, env=None, auto=False, **variables):
        values = dict(self.extra, ai_ops_deploy_root=str(self.root / "deploy"), ai_ops_config_dir=str(self.root / "private"),
                      ansible_python_interpreter=sys.executable)
        if not auto:
            values.update(ai_ops_compose_cmd=str(self.compose), ai_ops_runtime="docker")
        values.update(variables)
        path = self.root / "vars.yml"
        path.write_text(yaml.safe_dump(values))
        full = dict(os.environ, **self.env_extra, AI_OPS_SHIM_COMPOSE=str(self.compose), ANSIBLE_NOCOLOR="1",
                    ANSIBLE_LOCAL_TEMP=str(self.root / "ansible-tmp"), ANSIBLE_REMOTE_TEMP=str(self.root / "remote-tmp"),
                    **(env or {}))
        result = subprocess.run(["ansible-playbook", "-i", str(self.inventory), str(play), "-e", "@" + str(path), *args],
                                env=full, text=True, capture_output=True, timeout=180)
        return result.returncode, result.stdout + result.stderr

    def engine_environment(self, docker=False, podman=False, plugin=False, standalone=False):
        commands = self.root / "commands"
        commands.mkdir(exist_ok=True)
        # Do not append host PATH: absent providers must never find a real engine.
        for name in ("ansible-playbook", "python3", "env", "printenv", "dirname", "uname", "mkdir", "rm", "chmod", "cat"):
            link = commands / name
            if not link.exists():
                link.symlink_to(shutil.which(name))
        engine = commands / "fixture-engine"
        engine.write_text(FAKE_ENGINE)
        engine.chmod(0o755)
        for name in ("docker", "podman"):
            link = commands / name
            if not link.exists():
                link.symlink_to(engine)
        standalone_path = commands / "podman-compose"
        standalone_path.unlink(missing_ok=True)
        if standalone:
            standalone_path.symlink_to(engine)
        (self.root / "engine-calls").write_text("")
        return {"PATH": str(commands), "FAKE_ENGINE_LOG": str(self.root / "engine-calls"),
                "FAKE_DOCKER": str(int(docker)), "FAKE_PODMAN": str(int(podman)),
                "FAKE_PODMAN_PLUGIN": str(int(plugin))}

    def test_auto_provider_and_canonical_engine_in_check_mode(self):
        for name, options, provider, runtime in (
            ("docker", {"docker": True}, "docker compose", "docker"),
            ("both", {"docker": True, "podman": True, "plugin": True}, "docker compose", "docker"),
            ("podman-plugin", {"podman": True, "plugin": True}, "podman compose", "podman"),
            ("podman-standalone", {"podman": True, "standalone": True}, "podman-compose", "podman"),
        ):
            with self.subTest(provider=name):
                rc, text = self.run_play(self.play, "--check", auto=True, env=self.engine_environment(**options))
                self.assertEqual(rc, 0, text)
                self.assertIn("compose provider: " + provider, text)
                self.assertIn('"msg": "runtime engine: ' + runtime + '"', text)
                self.assertFalse((self.root / "deploy/.ai-ops-state").exists())
                calls = (self.root / "engine-calls").read_text()
                self.assertNotRegex(calls, r"\b(up|pull|restart|exec|build)\b")
                if runtime == "docker":
                    self.assertNotIn("podman", calls)
        self.assertTrue(all(c["module"] == "docker_host_info" for c in fixture_env.shim_calls(self.root)))

    def test_no_usable_runtime_names_prerequisite_without_mutation(self):
        rc, text = self.run_play(self.play, "--check", auto=True, env=self.engine_environment())
        self.assertNotEqual(rc, 0, text)
        self.assertIn("no usable container runtime", text)
        self.assertNotIn("No first item", text)
        self.assertFalse((self.root / "deploy/.ai-ops-state").exists())
        self.assertFalse(fixture_env.shim_calls(self.root))

    def test_explicit_provider_and_runtime_do_not_probe_engines(self):
        rc, text = self.run_play(self.play, "--check", env=self.engine_environment(),
                                 ai_ops_compose_cmd=str(self.compose), ai_ops_runtime="docker")
        self.assertEqual(rc, 0, text)
        self.assertIn("compose provider: " + str(self.compose), text)
        self.assertIn('"msg": "runtime engine: docker"', text)
        self.assertEqual((self.root / "engine-calls").read_text(), "")
        self.assertFalse((self.root / "deploy/.ai-ops-state").exists())

    def test_runtime_override_takes_precedence_over_docker(self):
        rc, text = self.run_play(self.play, "--check", auto=True, ai_ops_runtime="podman",
                                 env=self.engine_environment(docker=True, podman=True, plugin=True))
        self.assertEqual(rc, 0, text)
        self.assertIn("compose provider: podman compose", text)
        self.assertIn('"msg": "runtime engine: podman"', text)
        self.assertNotIn("docker", (self.root / "engine-calls").read_text())
        self.assertFalse((self.root / "deploy/.ai-ops-state").exists())

    def test_empty_extra_vars_still_request_auto_detection(self):
        rc, text = self.run_play(self.play, "--check", auto=True, env=self.engine_environment(docker=True),
                                 ai_ops_compose_cmd="", ai_ops_runtime="")
        self.assertEqual(rc, 0, text)
        self.assertIn("compose provider: docker compose", text)

    def test_whitespace_provider_names_missing_prerequisite(self):
        rc, text = self.run_play(self.play, "--check", env=self.engine_environment(), ai_ops_compose_cmd="   ", ai_ops_runtime="")
        self.assertNotEqual(rc, 0, text)
        self.assertIn("Missing prerequisite: Compose provider", text)
        self.assertNotIn("No first item", text)

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
        self.addCleanup(server.server_close)
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
                     "ai_ops_managed_file : Read remote current checksum of model-catalog/config.yaml",
                     "ai_ops_managed_file : Record the comparison result"):
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
