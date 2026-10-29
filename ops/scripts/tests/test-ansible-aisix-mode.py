#!/usr/bin/env python3
"""AISIX routes stay readable; unmanaged config drift checks never write config."""
import os
from pathlib import Path
import subprocess
import sys
import tempfile

import yaml

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(Path(__file__).resolve().parent))
import fixture_env  # noqa: E402

tasks = yaml.safe_load((ROOT / "ops/ansible/roles/ai_ops_aisix/tasks/deploy.yml").read_text())
task = next(t for t in tasks if t.get("name") == "Deploy resources.yaml")
mode = task["vars"]["ai_ops_managed_file_mode"]
assert mode == "0444", mode
assert task["vars"]["ai_ops_managed_file_activate"] == {"service": "aisix", "dependencies": False}
normalize_task = next(t for t in tasks if t.get("name") == "Normalize max_fallbacks from targets count")
validate_task = next(t for t in tasks if t.get("name") == "Validate fallback-status contract")
assert normalize_task["changed_when"] == "__ai_ops_aisix_normalize.stdout != 'normalized 0'"
assert validate_task["changed_when"] is False
with tempfile.TemporaryDirectory(prefix="aisix-mode-", dir="/var/tmp") as temp:
    root = Path(temp)
    (root / ".ai-ops-state").mkdir()
    source, remote = root / "private.yaml", root / "live.yaml"
    source.write_text("models: []\n")
    source.chmod(0o600)
    remote.write_text("models: [old]\n")
    remote.chmod(0o444)
    (root / ".ai-ops-state/aisix__resources.yaml.baseline").write_bytes(remote.read_bytes())
    env_extra, _ = fixture_env.prepare(root)
    fixture = [{"hosts": "localhost", "connection": "local", "gather_facts": False,
                "module_defaults": {"community.docker.docker_compose_v2": {"project_src": str(root), "project_name": "ai-gateway"}},
                "vars": {"ai_ops_deploy_root": str(root)},
                "tasks": [{"name": "Deploy resources.yaml", "ansible.builtin.include_role": {
                    "name": "ai_ops_managed_file", "tasks_from": "deploy"},
                    "vars": {"ai_ops_managed_file_label": "aisix/resources.yaml", "ai_ops_managed_file_local": str(source),
                             "ai_ops_managed_file_remote": str(remote), "ai_ops_managed_file_mode": mode,
                             "ai_ops_managed_file_activate": {"service": "aisix", "dependencies": False}}}]}]
    path = root / "fixture.yml"
    path.write_text(yaml.safe_dump(fixture))
    env = dict(os.environ, **env_extra, AI_OPS_SHIM_COMPOSE="true", ANSIBLE_LOCAL_TEMP=str(root / "ansible-tmp"))
    result = subprocess.run(["ansible-playbook", "-i", "localhost,", str(path)],
                            env=env, text=True, capture_output=True, timeout=120)
    assert result.returncode == 0, result.stdout + result.stderr
    assert remote.read_bytes() == source.read_bytes()
    assert remote.stat().st_mode & 0o777 == 0o444
    calls = [c for c in fixture_env.shim_calls(root) if c["module"] == "docker_compose_v2"]
    assert len(calls) == 1 and calls[0]["params"]["services"] == ["aisix"], calls
print("ansible_aisix_readable_mode=passed")

config_check = next(t for t in tasks if t.get("name") == "Check unmanaged AISIX config.yaml")
config_tasks = ROOT / "ops/ansible/roles/ai_ops_aisix/tasks" / config_check["ansible.builtin.include_tasks"]
with tempfile.TemporaryDirectory(prefix="aisix-config-drift-", dir="/var/tmp") as temp:
    root = Path(temp)
    desired, live = root / "desired", root / "live"
    relative = Path("services/routing/aisix/config.yaml")
    for parent in (desired, live):
        (parent / relative).parent.mkdir(parents=True)
    source, remote = desired / relative, live / relative
    secret = "fixture-admin-key-must-not-appear"
    content = f"admin:\n  admin_keys: [{secret}]\nupstream:\n  stream_timeout_ms: 120000\n"
    env_extra, _ = fixture_env.prepare(root)
    env = dict(os.environ, **env_extra, ANSIBLE_LOCAL_TEMP=str(root / "ansible-tmp"))
    for scenario, source_content, remote_content in (
        ("matching", content, content),
        ("different", content, content.replace("120000", "60000")),
        ("missing-source", None, content),
        ("missing-remote", content, None),
    ):
        for path, value in ((source, source_content), (remote, remote_content)):
            if value is None:
                path.unlink(missing_ok=True)
            else:
                path.write_text(value)
                path.chmod(0o600)
        before = {p: (p.read_bytes(), p.stat()) for p in (source, remote) if p.exists()}
        fixture = [{"hosts": "localhost", "connection": "local", "gather_facts": False,
                    "vars": {"ai_ops_config_dir": str(desired), "ai_ops_deploy_root": str(live)},
                    "tasks": [{"name": "Check unmanaged config", "ansible.builtin.include_tasks": str(config_tasks)},
                              {"name": "Verify drift result", "ansible.builtin.assert": {"that": [
                                  "ai_ops_aisix_config_diff | bool == " + str(scenario != "matching").lower()]}}]}]
        path = root / "fixture.yml"
        path.write_text(yaml.safe_dump(fixture))
        for mode in (["--check", "--diff"], []):
            result = subprocess.run(["ansible-playbook", "-i", "localhost,", str(path), *mode],
                                    env=env, text=True, capture_output=True, timeout=120)
            output = result.stdout + result.stderr
            assert result.returncode == 0, (scenario, mode, output)
            assert "changed=0" in output, (scenario, mode, output)
            assert secret not in output, (scenario, mode, output)
            assert {p for p in (source, remote) if p.exists()} == set(before)
            for p, (data, stat) in before.items():
                assert p.read_bytes() == data
                after = p.stat()
                assert (after.st_ino, after.st_mtime_ns, after.st_mode) == (stat.st_ino, stat.st_mtime_ns, stat.st_mode)
    assert not (live / ".ai-ops-state").exists()
    assert not fixture_env.shim_calls(root)
print("ansible_aisix_config_drift_read_only=passed")
