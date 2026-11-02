#!/usr/bin/env python3
"""Fixture cases of the ai_ops_managed_file role (design D5, D12).

Runs real ansible-playbook on localhost with the test-only community.docker shims.
Cases: the drift gate blocks; a no-op deploy reports no change; check mode uploads
nothing; a .env canary secret never appears in --check --diff output; the shim log
shows the exact activation module call, and no module mutation in check mode.
"""
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import unittest

import yaml

TESTS = Path(__file__).resolve().parent
ROOT = TESTS.parents[2]
sys.path.insert(0, str(TESTS))
import fixture_env  # noqa: E402

CANARY = "canary-secret-7f3a"
MODULE_DEFAULTS = yaml.safe_load((ROOT / "ops/ansible/playbooks/deploy-config.yml").read_text())[0]["module_defaults"]


class ManagedFile(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="managed-file-", dir="/var/tmp")
        self.root = Path(self.tmp.name)
        self.deploy, self.private = self.root / "deploy", self.root / "private"
        (self.deploy / ".ai-ops-state").mkdir(parents=True)
        self.private.mkdir()
        compose = self.root / "fake-compose"
        compose.write_text("#!/bin/sh\necho \"$@\" >>\"$(dirname \"$0\")/compose-calls\"\n")
        compose.chmod(0o755)
        self.env_extra, _ = fixture_env.prepare(self.root)
        self.env_extra["AI_OPS_SHIM_COMPOSE"] = str(compose)

    def tearDown(self):
        self.tmp.cleanup()

    def files(self, label, local, remote, baseline):
        name = label.replace("/", "__")
        paths = self.private / Path(label).name, self.deploy / label, self.deploy / ".ai-ops-state" / (name + ".baseline")
        paths[1].parent.mkdir(parents=True, exist_ok=True)
        for path, text in zip(paths, (local, remote, baseline)):
            if text is not None:
                path.write_text(text)
        if baseline is not None:
            paths[2].chmod(0o600)  # as every baseline the role writes
        return paths

    def run_play(self, label, check=False, activate=True):
        variables = {"ai_ops_managed_file_label": label,
                     "ai_ops_managed_file_local": str(self.private / Path(label).name),
                     "ai_ops_managed_file_remote": str(self.deploy / label)}
        if activate:
            variables["ai_ops_managed_file_activate"] = {"service": "model-catalog", "dependencies": False}
        play = [{"hosts": "localhost", "connection": "local", "gather_facts": False,
                 "module_defaults": MODULE_DEFAULTS,
                 "vars": {"ai_ops_deploy_root": str(self.deploy), "ai_ops_compose_project": "ai-gateway",
                          "ai_ops_compose_project_src": str(self.deploy),
                          "ai_ops_compose_files": [str(self.deploy / "compose.yaml"), str(self.deploy / "compose.override.yaml")],
                          "ai_ops_compose_env_files": [str(self.deploy / ".env")]},
                 "tasks": [{"name": "Deploy the managed file", "ansible.builtin.include_role": {
                     "name": "ai_ops_managed_file", "tasks_from": "deploy"}, "vars": variables}]}]
        path = self.root / "play.yml"
        path.write_text(yaml.safe_dump(play))
        argv = ["ansible-playbook", "-i", "localhost,", str(path)] + (["--check", "--diff"] if check else [])
        env = dict(os.environ, **self.env_extra, ANSIBLE_NOCOLOR="1", ANSIBLE_LOCAL_TEMP=str(self.root / "ansible-tmp"))
        result = subprocess.run(argv, env=env, text=True, capture_output=True, timeout=180)
        return result.returncode, result.stdout + result.stderr

    def changed(self, text):
        return int(re.search(r"localhost\s+: ok=\d+\s+changed=(\d+)", text)[1])

    def module_calls(self):
        return [c for c in fixture_env.shim_calls(self.root) if c["module"] == "docker_compose_v2"]

    def test_drift_gate_blocks(self):
        _, remote, _ = self.files("model-catalog/config.yaml", "local\n", "hand edit\n", "deployed\n")
        rc, text = self.run_play("model-catalog/config.yaml")
        self.assertNotEqual(rc, 0, text)
        self.assertIn("changed since last deploy", text)
        self.assertEqual(remote.read_text(), "hand edit\n")
        self.assertEqual(self.module_calls(), [])

    def test_noop_deploy_reports_no_change(self):
        self.files("model-catalog/config.yaml", "same\n", "same\n", "same\n")
        (self.deploy / ".ai-ops-state/model-catalog__config.yaml.predeploy").write_text("older\n")
        for check in (True, False):
            rc, text = self.run_play("model-catalog/config.yaml", check=check)
            self.assertEqual(rc, 0, text)
            self.assertEqual(self.changed(text), 0, text)
        self.assertEqual(self.module_calls(), [])
        # A no-op keeps the last rollback point.
        self.assertEqual((self.deploy / ".ai-ops-state/model-catalog__config.yaml.predeploy").read_text(), "older\n")

    def test_check_mode_uploads_nothing_and_does_not_activate(self):
        _, remote, baseline = self.files("model-catalog/config.yaml", "new\n", "old\n", "old\n")
        rc, text = self.run_play("model-catalog/config.yaml", check=True)
        self.assertEqual(rc, 0, text)
        self.assertEqual(remote.read_text(), "old\n")
        self.assertEqual(baseline.read_text(), "old\n")
        self.assertFalse((self.deploy / ".ai-ops-state/model-catalog__config.yaml.predeploy").exists())
        self.assertIn("+new", text)  # the native check-mode diff of a non-secret file
        self.assertEqual(self.module_calls(), [])
        self.assertFalse((self.root / "compose-calls").exists())

    def test_env_canary_never_appears_in_check_diff_or_apply(self):
        _, remote, _ = self.files(".env", f"TOKEN={CANARY}\n", "TOKEN=old-secret\n", "TOKEN=old-secret\n")
        for check in (True, False):
            rc, text = self.run_play(".env", check=check, activate=False)
            self.assertEqual(rc, 0, text)
            self.assertNotIn(CANARY, text)
            self.assertNotIn("old-secret", text)
        self.assertEqual(remote.read_text(), f"TOKEN={CANARY}\n")

    def test_activation_module_call_and_no_mutation_in_check_mode(self):
        _, remote, baseline = self.files("model-catalog/config.yaml", "new\n", "old\n", "old\n")
        rc, text = self.run_play("model-catalog/config.yaml", check=True)
        self.assertEqual(rc, 0, text)
        self.assertEqual(self.module_calls(), [])
        rc, text = self.run_play("model-catalog/config.yaml")
        self.assertEqual(rc, 0, text)
        calls = self.module_calls()
        self.assertEqual(len(calls), 1, calls)
        params = calls[0]["params"]
        self.assertFalse(calls[0]["check_mode"])
        expected = {"services": ["model-catalog"], "state": "present", "recreate": "always", "dependencies": False,
                    "build": "never", "pull": "policy", "project_name": "ai-gateway", "project_src": str(self.deploy),
                    "files": [str(self.deploy / "compose.yaml"), str(self.deploy / "compose.override.yaml")],
                    "env_files": [str(self.deploy / ".env")]}
        self.assertEqual({k: params[k] for k in expected}, expected)
        self.assertEqual(remote.read_text(), "new\n")
        self.assertEqual(baseline.read_text(), "new\n")
        self.assertEqual((self.deploy / ".ai-ops-state/model-catalog__config.yaml.predeploy").read_text(), "old\n")
        argv = (self.root / "compose-calls").read_text().split()
        self.assertEqual(argv[-6:], ["up", "-d", "--force-recreate", "--no-deps", "--no-build", "model-catalog"])


if __name__ == "__main__":
    unittest.main()
