#!/usr/bin/env python3
"""Shared Ansible fixture setup for the ops/scripts/tests harnesses.

Every fixture run needs three things that production gets from the real host:
- the test-only community.docker shims first in ANSIBLE_COLLECTIONS_PATH;
- a renderer at the pinned version, with its checksum recorded (no download);
- a writable shim mutation log.

Python: `env, extra = fixture_env.prepare(tmp)`.
Shell:  `eval "$(python3 fixture_env.py shell "$tmp")"` exports the environment and
        writes "$tmp/fixture-vars.json" for `-e @"$tmp/fixture-vars.json"`.
The fixture renderer answers `version --short` with the pin and delegates every
other call to the given render command (a fake Compose), or fails without one. The
command is baked into the script: the renderer runs under env -i.
"""
import hashlib
import json
import os
import platform
import shlex
import sys
from pathlib import Path

TESTS = Path(__file__).resolve().parent
ROOT = TESTS.parents[2]
SHIMS = TESTS / "fixtures/collections"
REAL = ROOT / "ops/ansible/collections"
VERSION = "2.40.3"
RENDERER = """#!/bin/sh
if [ "$1" = version ]; then echo {version}; exit 0; fi
{delegate}
"""


def prepare(tmp, render=None):
    tmp = Path(tmp)
    renderer_dir = tmp / "renderer"
    renderer_dir.mkdir(parents=True, exist_ok=True)
    renderer = renderer_dir / "docker-compose"
    delegate = f'exec {render} "$@"' if render else "echo 'fixture renderer: no render command' >&2; exit 2"
    renderer.write_text(RENDERER.format(version=VERSION, delegate=delegate))
    renderer.chmod(0o755)
    digest = hashlib.sha256(renderer.read_bytes()).hexdigest()
    log = tmp / "shim-calls.jsonl"
    log.touch()
    env = {"ANSIBLE_COLLECTIONS_PATH": f"{SHIMS}:{REAL}", "ANSIBLE_COLLECTIONS_SCAN_SYS_PATH": "false",
           "ANSIBLE_ROLES_PATH": str(ROOT / "ops/ansible/roles"), "AI_OPS_SHIM_LOG": str(log)}
    extra = {"ai_ops_compose_renderer_dir": str(renderer_dir),
             "ai_ops_compose_renderer_sha256": {platform.machine(): digest}}
    return env, extra


def shim_calls(tmp):
    log = Path(tmp) / "shim-calls.jsonl"
    return [json.loads(line) for line in log.read_text().splitlines() if line]


def main():
    if len(sys.argv) < 3 or sys.argv[1] != "shell":
        raise SystemExit("usage: fixture_env.py shell TMP [RENDER_COMMAND]")
    env, extra = prepare(sys.argv[2], sys.argv[3] if len(sys.argv) > 3 else None)
    (Path(sys.argv[2]) / "fixture-vars.json").write_text(json.dumps(extra))
    for key, value in env.items():
        print(f"export {key}={shlex.quote(value)}")


if __name__ == "__main__":
    main()
