#!/usr/bin/env python3
"""Fail if a tracked file under ops/ansible/ leaks a real inventory.

A tracked file must not contain a Tailscale (100.64.0.0/10) address, and only
the example inventory may set ansible_host. Usage: check-tracked-inventory.py [REPO]
"""
import re
import subprocess
import sys
from pathlib import Path

TAILSCALE = re.compile(r"(?<![0-9.])100\.(6[4-9]|[7-9][0-9]|1[01][0-9]|12[0-7])\.[0-9]{1,3}\.[0-9]{1,3}(?![0-9])")
HOST = re.compile(r"\bansible_host\b")
EXAMPLE = "ops/ansible/inventories/example/"


def main() -> int:
    repo = Path(sys.argv[1] if len(sys.argv) > 1 else ".")
    files = subprocess.run(["git", "-C", str(repo), "ls-files", "-z", "ops/ansible"],
                           check=True, capture_output=True).stdout.decode().split("\0")
    problems = []
    for name in filter(None, files):
        path = repo / name
        if not path.is_file():
            continue
        text = path.read_text(errors="replace")
        if TAILSCALE.search(text):
            problems.append(f"{name}: tracked Tailscale address")
        if not name.startswith(EXAMPLE) and HOST.search(text) and name.endswith((".yml", ".yaml", ".ini", ".cfg")):
            problems.append(f"{name}: ansible_host outside {EXAMPLE}")
    for problem in problems:
        print(problem, file=sys.stderr)
    if problems:
        print("real inventory data belongs in ops/ansible/private-config/inventories/", file=sys.stderr)
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
