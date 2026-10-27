#!/usr/bin/env python3
"""Static and controller-only checks of the standard Ansible layout (no remote host).

Covers: pinned collection resolution, shim/real argument-spec equality, shared
module_defaults, component choices versus profiles, argument validation before any
connection, the remote state label set (design D8), the tracked-inventory leak
guard, and the allow-list of kept Compose/Docker CLI calls (design D12).
"""
import copy
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import unittest

import yaml

TESTS = Path(__file__).resolve().parent
ROOT = TESTS.parents[2]
ANSIBLE = ROOT / "ops/ansible"
sys.path.insert(0, str(TESTS))
import docker_shim_specs  # noqa: E402

PLAYBOOKS = ("deploy", "rollback", "deploy-config", "rollback-config", "recovery", "unlock")
CONTROLLER_ENV = {k: v for k, v in os.environ.items() if not k.startswith("ANSIBLE_")}


def run(argv, **kwargs):
    return subprocess.run(argv, cwd=ANSIBLE, env=dict(CONTROLLER_ENV, ANSIBLE_NOCOLOR="1"),
                          text=True, capture_output=True, timeout=180, **kwargs)


def tasks_of(path):
    """Yield every task of a task file, descending into blocks."""
    def walk(items):
        for task in items or []:
            yield task
            for key in ("block", "rescue", "always"):
                yield from walk(task.get(key))
    yield from walk(yaml.safe_load(path.read_text()))


class Toolchain(unittest.TestCase):
    def test_pinned_collection_resolves_from_the_project(self):
        result = run(["ansible-doc", "--json", "community.docker.docker_compose_v2"])
        self.assertEqual(result.returncode, 0, result.stderr + "\ninstall: ansible-galaxy collection install -r requirements.yml -p collections")
        doc = next(iter(json.loads(result.stdout).values()))["doc"]
        self.assertTrue(doc["filename"].startswith(str(ANSIBLE / "collections") + "/"), doc["filename"])
        manifest = json.loads((ANSIBLE / "collections/ansible_collections/community/docker/MANIFEST.json").read_text())
        self.assertEqual(manifest["collection_info"]["version"], "5.4.0")
        pins = yaml.safe_load((ANSIBLE / "requirements.yml").read_text())["collections"]
        self.assertIn({"name": "community.docker", "version": "5.4.0"}, pins)

    def test_example_inventory_uses_test_net_only(self):
        result = run(["ansible-inventory", "-i", "inventories/example", "--list"])
        self.assertEqual(result.returncode, 0, result.stderr)
        hosts = json.loads(result.stdout)["_meta"]["hostvars"]
        self.assertEqual(list(hosts), ["gateway.example"])
        self.assertTrue(hosts["gateway.example"]["ansible_host"].startswith("192.0.2."))

    def test_shim_specs_equal_the_pinned_modules(self):
        real = docker_shim_specs.real_specs(str(ANSIBLE / "collections"))
        committed = docker_shim_specs.committed_specs()
        self.assertEqual(docker_shim_specs.compare(committed, real), [])
        broken = copy.deepcopy(committed)
        del broken["docker_compose_v2"]["dependencies"]
        self.assertTrue(docker_shim_specs.compare(broken, real), "removing a shim option was not detected")


class Playbooks(unittest.TestCase):
    def play(self, name):
        return yaml.safe_load((ANSIBLE / "playbooks" / (name + ".yml")).read_text())[0]

    def test_module_defaults_are_identical(self):
        blocks = {name: self.play(name)["module_defaults"] for name in PLAYBOOKS}
        first = blocks["deploy"]
        self.assertEqual(set(first), {"community.docker.docker_compose_v2", "community.docker.docker_compose_v2_exec"})
        self.assertEqual(first["community.docker.docker_compose_v2"]["project_name"], "{{ ai_ops_compose_project }}")
        for name, block in blocks.items():
            self.assertEqual(block, first, name)

    def test_component_choices_equal_profile_keys(self):
        specs = yaml.safe_load((ANSIBLE / "roles/ai_ops_component/meta/argument_specs.yml").read_text())["argument_specs"]
        profiles = yaml.safe_load((ANSIBLE / "roles/ai_ops_component/defaults/main.yml").read_text())["ai_ops_component_profiles"]
        for entry in ("deploy", "rollback"):
            self.assertEqual(specs[entry]["options"]["ai_ops_component_name"]["choices"], list(profiles), entry)

    def test_component_is_validated_before_any_connection(self):
        with tempfile.TemporaryDirectory(dir="/var/tmp") as tmp:
            inventory = Path(tmp) / "hosts.yml"
            # TEST-NET host over SSH: any connection attempt would report UNREACHABLE.
            inventory.write_text(yaml.safe_dump({"gateway": {"hosts": {"unreachable.example": {
                "ansible_host": "192.0.2.1", "ansible_ssh_common_args": "-o ConnectTimeout=2"}}}}))
            cases = {"deploy": "openai-api-cache-keeper", "rollback": "responses-ws-sse-bridge",
                     "deploy-config": "egress-proxy", "rollback-config": "model-catalog"}
            for name, choice in cases.items():
                for extra in (["-e", "component=bogus"], []):
                    for tags in ([], ["--tags", "verify"]):
                        with self.subTest(playbook=name, extra=extra, tags=tags):
                            result = run(["ansible-playbook", "-i", str(inventory), "playbooks/" + name + ".yml", *extra, *tags])
                            text = result.stdout + result.stderr
                            self.assertNotEqual(result.returncode, 0, text)
                            self.assertNotIn("UNREACHABLE", text)
                            self.assertNotIn("Verify SSH connectivity", text)
                            if extra:
                                self.assertIn("must be one of", text)
                                self.assertIn(choice, text)
                            else:
                                self.assertIn("missing required arguments", text)

    def test_unknown_runtime_is_rejected_by_the_argument_spec(self):
        with tempfile.TemporaryDirectory(dir="/var/tmp") as tmp:
            play = Path(tmp) / "play.yml"
            play.write_text(yaml.safe_dump([{"hosts": "localhost", "connection": "local", "gather_facts": False,
                                             "roles": [{"role": "ai_ops"}]}]))
            env = dict(CONTROLLER_ENV, ANSIBLE_ROLES_PATH=str(ANSIBLE / "roles"), ANSIBLE_NOCOLOR="1")
            result = subprocess.run(["ansible-playbook", "-i", "localhost,", str(play), "-e", "ai_ops_runtime=bogus"],
                                    env=env, text=True, capture_output=True, timeout=120)
            text = result.stdout + result.stderr
            self.assertNotEqual(result.returncode, 0, text)
            self.assertIn("ai_ops_runtime", text)
            self.assertIn("must be one of", text)
            self.assertNotIn("Detect Compose provider", text)


def check_state_labels(root):
    """Return the D8 state-name problems of the role tree under root."""
    roles = root / "ops/ansible/roles"
    problems = []
    labels = set()
    for path in roles.glob("*/tasks/*.yml"):
        for task in tasks_of(path):
            args = task.get("vars") or {}
            if (task.get("ansible.builtin.include_role") or {}).get("name") == "ai_ops_managed_file":
                labels.add(args.get("ai_ops_managed_file_label"))
            for value in args.values():
                if isinstance(value, dict) and "ai_ops_managed_file_label" in value:
                    labels.add(value["ai_ops_managed_file_label"])
    labels.discard("{{ __ai_ops_model_catalog_file.ai_ops_managed_file_label }}")
    expected = {"aisix/resources.yaml", "cpa-model-sync/config.json", "model-catalog/config.yaml"}
    if labels != expected:
        problems.append(f"managed-file labels {sorted(labels)} != {sorted(expected)}")
    managed = "".join(p.read_text() for p in (roles / "ai_ops_managed_file/tasks").glob("*.yml"))
    for name in (".ai-ops-state/{{ ai_ops_managed_file_label | replace('/', '__') }}.baseline",
                 ".ai-ops-state/{{ ai_ops_managed_file_label | replace('/', '__') }}.predeploy"):
        if name not in managed:
            problems.append("missing managed-file state name " + name)
    component = (roles / "ai_ops_component/tasks/transaction.yml").read_text()
    for needle in ("loop: [compose.yaml, compose.override.yaml, .env]",
                   ".ai-ops-state/front-{{ ai_ops_component_name }}-last.json",
                   ".ai-ops-state/front-{{ ai_ops_component_name }}-failure.json",
                   'prefix: "front-{{ ai_ops_component_name }}-"',
                   "'/.ai-ops-state/front-' + ai_ops_component_name + '-'",
                   "dest: \"{{ ai_ops_deploy_root }}/.ai-ops-state/{{ item }}.baseline\"",
                   "'service': ai_ops_component_name, 'operation': __ai_ops_component_operation"):
        if needle not in component:
            problems.append("missing component state name " + needle)
    sync = (roles / "ai_ops_model_sync/tasks/deploy.yml").read_text()
    if ".ai-ops-state/model-sync-transition.json" not in sync:
        problems.append("missing model-sync-transition.json")
    return problems


class StateCompatibility(unittest.TestCase):
    def test_state_names_are_unchanged(self):
        self.assertEqual(check_state_labels(ROOT), [])

    def test_a_changed_label_is_detected(self):
        with tempfile.TemporaryDirectory(dir="/var/tmp") as tmp:
            copy_root = Path(tmp)
            shutil.copytree(ANSIBLE / "roles", copy_root / "ops/ansible/roles")
            path = copy_root / "ops/ansible/roles/ai_ops_aisix/tasks/deploy.yml"
            path.write_text(path.read_text().replace("ai_ops_managed_file_label: aisix/resources.yaml",
                                                     "ai_ops_managed_file_label: aisix/resources.yml"))
            self.assertTrue(check_state_labels(copy_root))


class LeakGuard(unittest.TestCase):
    def test_guard_fails_on_a_planted_inventory(self):
        guard = ROOT / "ops/scripts/check-tracked-inventory.py"
        with tempfile.TemporaryDirectory(dir="/var/tmp") as tmp:
            repo = Path(tmp)
            subprocess.run(["git", "init", "-q", str(repo)], check=True)
            (repo / "ops/ansible/inventories/example").mkdir(parents=True)
            (repo / "ops/ansible/inventories/example/hosts.yml").write_text("gateway:\n  hosts:\n    g:\n      ansible_host: 192.0.2.10\n")
            subprocess.run(["git", "-C", str(repo), "add", "-A"], check=True)
            self.assertEqual(subprocess.run([sys.executable, str(guard), str(repo)]).returncode, 0)
            for name, text in (("inventory/hosts.ini", "[gateway]\nhost ansible_host=192.0.2.11\n"),
                               ("group_vars/notes.yml", "note: reachable at 100.94.1.2\n")):
                planted = repo / "ops/ansible" / name
                planted.parent.mkdir(parents=True, exist_ok=True)
                planted.write_text(text)
                subprocess.run(["git", "-C", str(repo), "add", "-A"], check=True)
                with self.subTest(name=name):
                    result = subprocess.run([sys.executable, str(guard), str(repo)], capture_output=True, text=True)
                    self.assertEqual(result.returncode, 1, result.stderr)
                subprocess.run(["git", "-C", str(repo), "rm", "-q", "--cached", str(planted)], check=True)
                planted.unlink()

    def test_tracked_tree_is_clean(self):
        self.assertEqual(subprocess.run([sys.executable, str(ROOT / "ops/scripts/check-tracked-inventory.py"), str(ROOT)]).returncode, 0)


# Each kept command call that runs the Compose/Docker/Podman CLI (design D12), by
# role task file and task name. Everything else uses a community.docker module.
KEPT_CLI_CALLS = {
    ("ai_ops/tasks/main.yml", "Detect Compose provider (mirrors ops/scripts/container-runtime.sh)"),
    ("ai_ops/tasks/prerequisites.yml", "Read the host Compose version"),
    ("ai_ops/tasks/renderer.yml", "Read the renderer version"),
    ("ai_ops_component/tasks/transaction.yml", "Validate the actual remote Compose argv before any mutation"),
    ("ai_ops_component/tasks/transaction.yml", "Validate candidate Compose explicitly without mutation"),
    ("ai_ops_component/tasks/transaction.yml", "Render candidate Compose without exposing secrets"),
    ("ai_ops_component/tasks/transaction.yml", "Validate uploaded candidate using the exact apply Compose argv"),
    ("ai_ops_component/tasks/transaction.yml", "Recreate only selected front component without builds or implicit pulls"),
    ("ai_ops_component/tasks/state.yml", "Read live effective Compose without exposing secrets"),
    ("ai_ops_component/tasks/runtime.yml", "Read component readiness and protected container identities"),
    ("ai_ops_component/tasks/image.yml", "Verify exact candidate image presence or read-only pullability"),
    ("ai_ops_model_sync/tasks/verify.yml", "Wait for the first bounded synchronization summary"),
    # Controller-only offline parse of the rendered policy (controller scratch, design D5).
    ("ai_ops_squid/tasks/render.yml", "Validate rendered config with pinned Squid (k parse)"),
}
CLI = re.compile(r"\b(docker|podman)\b|compose_argv|compose_base_argv|compose_cmd|runtime_cli|renderer_path|render_argv")
COMMANDS = {"ansible.builtin.command", "ansible.builtin.shell", "ansible.builtin.raw", "ansible.builtin.script"}


class KeptCalls(unittest.TestCase):
    def test_cli_calls_only_at_the_kept_d12_calls(self):
        found = set()
        for path in sorted((ANSIBLE / "roles").glob("*/*/*.yml")):
            if path.parent.name not in ("tasks", "handlers"):
                continue
            for task in tasks_of(path):
                for module in COMMANDS & set(task):
                    if CLI.search(json.dumps(task[module])):
                        found.add((str(path.relative_to(ANSIBLE / "roles")), task.get("name")))
        self.assertEqual(found - KEPT_CLI_CALLS, set(), "CLI call outside the D12 allow-list")
        self.assertEqual(KEPT_CLI_CALLS - found, set(), "stale D12 allow-list entry")
        for path in (ANSIBLE / "playbooks").glob("*.yml"):
            self.assertNotRegex(path.read_text(), r"\b(docker|podman) ", path)


if __name__ == "__main__":
    unittest.main()
