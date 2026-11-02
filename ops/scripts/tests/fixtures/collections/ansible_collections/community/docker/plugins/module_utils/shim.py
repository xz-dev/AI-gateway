# Test-only stand-in for community.docker 5.4.0. Never install outside fixtures.
"""Shared runner for the fixture shims.

Each shim accepts exactly the pinned module's argument spec (shim_specs.SPECS),
logs every call with its arguments and check-mode flag to AI_OPS_SHIM_LOG, and
answers from the fixture's fake CLIs:
  AI_OPS_SHIM_COMPOSE  Compose command prefix (default "docker compose")
  AI_OPS_SHIM_RUNTIME  container CLI (default "docker")
  AI_OPS_SHIM_PROJECT  Compose project used in container names (default "ai-gateway")
"""
import json
import os
import shlex
import subprocess

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.community.docker.plugins.module_utils.shim_specs import SPECS


def start(name):
    module = AnsibleModule(argument_spec=SPECS[name], supports_check_mode=True)
    log = os.environ.get("AI_OPS_SHIM_LOG")
    if log:
        with open(log, "a", encoding="utf-8") as out:
            out.write(json.dumps({"module": name, "check_mode": module.check_mode,
                                  "params": module.params}, sort_keys=True) + "\n")
    return module


def compose(module, *args, stdin=None):
    prefix = shlex.split(os.environ.get("AI_OPS_SHIM_COMPOSE", "docker compose"))
    params = module.params
    base = ["-p", params["project_name"]] if params.get("project_name") else []
    for path in params.get("files") or []:
        base += ["-f", path]
    for path in params.get("env_files") or []:
        base += ["--env-file", path]
    return run(prefix + base + list(args), stdin)


def runtime(*args):
    return run([os.environ.get("AI_OPS_SHIM_RUNTIME", "docker"), *args])


def run(argv, stdin=None):
    result = subprocess.run(argv, input=stdin, capture_output=True, text=True, check=False)
    return result.returncode, result.stdout.rstrip("\r\n"), result.stderr.rstrip("\r\n")


def service_of(container_name):
    project = os.environ.get("AI_OPS_SHIM_PROJECT", "ai-gateway") + "-"
    name = container_name.lstrip("/")
    if name.startswith(project) and name.endswith("-1"):
        return name[len(project):-2]
    return name
