#!/usr/bin/env python3
"""A private 0600 source must stay readable by the non-root AISIX container."""
import os
from pathlib import Path
import subprocess
import tempfile

import yaml

ROOT = Path(__file__).resolve().parents[3]
play = yaml.safe_load((ROOT / "ops/ansible/ops.yml").read_text())[0]
task = next(t for t in play["tasks"] if t.get("name") == "Deploy resources.yaml")
mode = task["vars"]["ai_file_mode"]
normalize_task = next(t for t in play["pre_tasks"] if t.get("name") == "Normalize max_fallbacks from targets count")
validate_task = next(t for t in play["pre_tasks"] if t.get("name") == "Validate fallback-status contract")
assert normalize_task["changed_when"] == "_aisix_normalize.stdout != 'normalized 0'"
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
    fixture = [{"hosts": "localhost", "connection": "local", "gather_facts": False,
                "vars": {"ai_ops_mode": "apply", "ai_ops_deploy_root": str(root),
                         "ai_file_label": "aisix/resources.yaml", "ai_file_local": str(source),
                         "ai_file_remote": str(remote), "ai_file_mode": mode,
                         "ai_activate_cmd": "/bin/true"},
                "tasks": [{"include_role": {"name": "ai_ops", "tasks_from": "deploy-file"}}]}]
    path = root / "fixture.yml"
    path.write_text(yaml.safe_dump(fixture))
    env = dict(os.environ, ANSIBLE_ROLES_PATH=str(ROOT / "ops/ansible/roles"),
               ANSIBLE_LOCAL_TEMP=str(root / "ansible-tmp"))
    result = subprocess.run(["ansible-playbook", "-i", "localhost,", str(path)],
                            env=env, text=True, capture_output=True, timeout=120)
    assert result.returncode == 0, result.stdout + result.stderr
    assert remote.read_bytes() == source.read_bytes()
    assert remote.stat().st_mode & 0o777 == 0o444
print("ansible_aisix_readable_mode=passed")
