#!/usr/bin/env python3
"""Move a deploy root (or the local private config) to the services/ + ops/ layout.

Stages, each idempotent and receipt-backed:
  --plan (default)      print the file moves and conflicts; change nothing
  --apply               back up and move every planned path, then leave a
                        compatibility symlink at each old path so a Docker
                        restart of a not-yet-recreated container still finds
                        its bind-mount source
  --finalize            after the containers are recreated from the new paths:
                        drop the compatibility symlinks and remove retired paths
                        (backed up first)
  --restore RECEIPT     undo one apply or finalize receipt

data/ is never touched. A destination that exists with different bytes aborts
the run before any change. Backups and receipts live in <root>/.layout-migration/.
"""
import argparse
import datetime
import filecmp
import json
import os
from pathlib import Path
import shutil
import sys

TESTS = ("test-ansible-model-sync.sh", "test-ansible-models-enricher-config.sh",
         "test-ansible-squid-source.sh", "test-ansible-ws-keepalive.sh",
         "test-component-apply.py", "test-component-deploy.py", "test-egress-proxy.sh",
         "test-gateway-recovery.py", "test-keeper-observe.py", "test-netns-guard.sh",
         "test-provider-sidecar-compose-candidate.sh", "test-provider-sidecar-tls-boundary.sh",
         "test-socat-boundary.sh", "test-standard-cache-topology.py")

# (old, new, compatibility link). Children come before the parent that contains them.
DEPLOY_RULES = [
    ("apisix", "services/firewall/apisix", True),
    ("egress-proxy", "services/firewall/egress-proxy", True),
    ("apisix-models", "services/routing/apisix-models", True),
    ("aisix", "services/routing/aisix", True),
    ("aisix-status", "services/routing/aisix-status", True),
    ("models-enricher", "services/routing/model-catalog", True),
    ("cpa", "services/cpa/config", True),
    ("cpa-model-sync", "services/cpa/cpa-model-sync", True),
    ("middleware/ai-sse-keepalive-proxy", "services/middleware/ai-sse-keepalive-proxy", True),
    ("middleware/openai-api-cache-keeper", "services/middleware/openai-api-cache-keeper", True),
    ("ansible", "ops/ansible", True),
    ("scripts/start-apisix-models.sh", "services/routing/apisix-models/start.sh", True),
    ("scripts/ops", "ops/scripts/host", True),
    *((f"scripts/{name}", f"ops/scripts/tests/{name}", False) for name in TESTS),
    ("scripts", "ops/scripts", True),
    # Ansible drift baselines and rollback copies are keyed by the file label.
    (".ai-ops-state/models-enricher__config.yaml.baseline",
     ".ai-ops-state/model-catalog__config.yaml.baseline", False),
    (".ai-ops-state/models-enricher__config.yaml.predeploy",
     ".ai-ops-state/model-catalog__config.yaml.predeploy", False),
]
DEPLOY_RETIRED = ["model-catalog-sidecar"]

PRIVATE_RULES = [
    ("models-enricher", "services/routing/model-catalog", False),
    ("aisix", "services/routing/aisix", False),
    ("cpa-model-sync", "services/cpa/cpa-model-sync", False),
]
PRIVATE_RETIRED = []

STATE = ".layout-migration"


class Abort(Exception):
    pass


def check_rule_paths(rules, retired):
    for path in [p for rule in rules for p in rule[:2]] + retired:
        top = Path(path).parts[0]
        if top in ("data", STATE) or Path(path).is_absolute() or ".." in Path(path).parts:
            raise Abort(f"rule touches a forbidden path: {path}")


def same(a, b):
    if a.is_symlink() or b.is_symlink():
        return a.is_symlink() and b.is_symlink() and os.readlink(a) == os.readlink(b)
    return filecmp.cmp(a, b, shallow=False)


def entries(src):
    """Yield (relative path, kind) for src itself or every entry below it."""
    if src.is_symlink() or src.is_file():
        yield Path("."), "file"
        return
    for directory, dirs, files in os.walk(src):
        here = Path(directory)
        yield here.relative_to(src), "dir"
        for name in files + [d for d in dirs if (here / d).is_symlink()]:
            yield (here / name).relative_to(src), "file"
        dirs[:] = [d for d in dirs if not (here / d).is_symlink()]


def is_done_link(root, old, new):
    path = root / old
    return path.is_symlink() and path.resolve() == (root / new).resolve()


def plan(root, rules):
    """Return moves [{old,new,files,dirs,link}] and conflicts, without changing anything."""
    moves, conflicts, claimed = [], [], {}
    for index, (old, new, link) in enumerate(rules):
        src, dst = root / old, root / new
        if is_done_link(root, old, new) or not (src.exists() or src.is_symlink()):
            continue
        # Paths owned by a more specific earlier rule (scripts/ops inside scripts).
        owned = [r[0] for r in rules[:index] if r[0].startswith(old + "/")]
        files, dirs = [], []
        for rel, kind in entries(src):
            full = str(Path(old) / rel)
            if any(full == child or full.startswith(child + "/") for child in owned):
                continue
            target = dst / rel if rel != Path(".") else dst
            if kind == "dir":
                if target.exists() and not target.is_dir():
                    conflicts.append(f"{target.relative_to(root)} exists and is not a directory")
                dirs.append(str(rel))
                continue
            key = str(target.relative_to(root))
            if key in claimed:
                conflicts.append(f"{key} is claimed by both {claimed[key]} and {old}")
            claimed[key] = old
            if target.exists() or target.is_symlink():
                if target.is_dir() or not same(src / rel if rel != Path(".") else src, target):
                    conflicts.append(f"{key} already exists with different content")
                    continue
            files.append(str(rel))
        moves.append({"old": old, "new": new, "files": files, "dirs": dirs, "link": link})
    return moves, conflicts


def link_location(root, old, rules):
    """Where the old path lives after its parent rule moved: scripts/ops -> ops/scripts/ops."""
    for parent_old, parent_new, _ in rules:
        if old.startswith(parent_old + "/"):
            return root / parent_new / old[len(parent_old) + 1:]
    return root / old


def backup(root, stamp, paths):
    target = root / STATE / stamp / "backup"
    for rel in paths:
        src = root / rel
        dst = target / rel
        dst.parent.mkdir(parents=True, exist_ok=True)
        if src.is_dir() and not src.is_symlink():
            shutil.copytree(src, dst, symlinks=True, dirs_exist_ok=True)
        else:
            shutil.copy2(src, dst, follow_symlinks=False)
    return target


def remove_empty_dirs(path, stop):
    while path != stop and path.is_dir() and not path.is_symlink() and not any(path.iterdir()):
        path.rmdir()
        path = path.parent


def write_receipt(root, stamp, receipt):
    path = root / STATE / stamp / "receipt.json"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(receipt, indent=2) + "\n")
    os.chmod(path, 0o600)
    return path


def apply(root, rules, stamp):
    moves, conflicts = plan(root, rules)
    if conflicts:
        raise Abort("conflicts:\n  " + "\n  ".join(conflicts))
    if not moves:
        return None
    backup_dir = backup(root, stamp, [m["old"] for m in moves])
    links = []
    for move in moves:
        src, dst = root / move["old"], root / move["new"]
        for rel in move["dirs"]:
            (dst / rel).mkdir(parents=True, exist_ok=True)
            shutil.copystat(src / rel, dst / rel, follow_symlinks=False)
        for rel in move["files"]:
            source = src / rel if rel != "." else src
            target = dst / rel if rel != "." else dst
            target.parent.mkdir(parents=True, exist_ok=True)
            if target.exists() or target.is_symlink():
                source.unlink()  # byte-identical duplicate (checked in plan); kept in the backup
            else:
                os.rename(source, target)
        for rel in sorted(move["dirs"], key=lambda r: -len(Path(r).parts)):
            remove_empty_dirs(src / rel, root)
        if src.exists() and not src.is_symlink():
            raise Abort(f"{move['old']} still has entries after the move")
    for move in sorted(moves, key=lambda m: len(Path(m["old"]).parts)):
        if move["link"]:
            location = link_location(root, move["old"], rules)
            location.parent.mkdir(parents=True, exist_ok=True)
            location.symlink_to(os.path.relpath(root / move["new"], location.parent))
            links.append(str(location.relative_to(root)))
    return write_receipt(root, stamp, {"stage": "apply", "root": str(root), "backup": str(backup_dir),
                                       "moves": moves, "links": links})


def finalize(root, rules, retired, stamp):
    links = []
    for old, new, link in sorted(rules, key=lambda r: -len(Path(r[0]).parts)):
        location = link_location(root, old, rules)
        if link and location.is_symlink() and location.resolve() == (root / new).resolve():
            links.append({"path": str(location.relative_to(root)), "target": os.readlink(location)})
    removed = [path for path in retired if (root / path).exists() or (root / path).is_symlink()]
    if not links and not removed:
        return None
    backup_dir = backup(root, stamp, removed)
    for item in links:
        (root / item["path"]).unlink()
        remove_empty_dirs((root / item["path"]).parent, root)
    for path in removed:
        target = root / path
        shutil.rmtree(target) if target.is_dir() and not target.is_symlink() else target.unlink()
    return write_receipt(root, stamp, {"stage": "finalize", "root": str(root), "backup": str(backup_dir),
                                       "links": links, "removed": removed})


def restore(root, receipt_path):
    receipt = json.loads(Path(receipt_path).read_text())
    backup_dir = Path(receipt["backup"])
    if receipt["stage"] == "finalize":
        for path in receipt["removed"]:
            if (root / path).exists():
                raise Abort(f"{path} exists; refusing to restore over it")
            source = backup_dir / path
            shutil.copytree(source, root / path, symlinks=True) if source.is_dir() else shutil.copy2(source, root / path)
        for item in reversed(receipt["links"]):
            location = root / item["path"]
            location.parent.mkdir(parents=True, exist_ok=True)
            if not location.is_symlink():
                location.symlink_to(item["target"])
        return
    for path in reversed(receipt["links"]):
        if (root / path).is_symlink():
            (root / path).unlink()
    for move in reversed(receipt["moves"]):
        src, dst = root / move["old"], root / move["new"]
        for rel in move["dirs"]:
            (src / rel).mkdir(parents=True, exist_ok=True)
            shutil.copystat(backup_dir / move["old"] / rel, src / rel, follow_symlinks=False)
        for rel in move["files"]:
            source = dst / rel if rel != "." else dst
            target = src / rel if rel != "." else src
            target.parent.mkdir(parents=True, exist_ok=True)
            if source.exists() or source.is_symlink():
                os.rename(source, target)
            else:
                saved = backup_dir / move["old"] / rel if rel != "." else backup_dir / move["old"]
                shutil.copy2(saved, target, follow_symlinks=False)
        for rel in sorted(move["dirs"], key=lambda r: -len(Path(r).parts)):
            remove_empty_dirs(dst / rel, root)
        remove_empty_dirs(dst.parent, root)


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--root", required=True, type=Path)
    parser.add_argument("--private", action="store_true", help="migrate a private-config tree instead of a deploy root")
    stage = parser.add_mutually_exclusive_group()
    stage.add_argument("--plan", action="store_true")
    stage.add_argument("--apply", action="store_true")
    stage.add_argument("--finalize", action="store_true")
    stage.add_argument("--restore", metavar="RECEIPT")
    args = parser.parse_args()
    root = args.root.resolve()
    rules, retired = (PRIVATE_RULES, PRIVATE_RETIRED) if args.private else (DEPLOY_RULES, DEPLOY_RETIRED)
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%S%fZ")
    try:
        check_rule_paths(rules, retired)
        if not root.is_dir():
            raise Abort(f"{root} is not a directory")
        if args.restore:
            restore(root, args.restore)
            print(f"restored {args.restore}")
        elif args.apply:
            receipt = apply(root, rules, stamp)
            print(f"applied; receipt {receipt}" if receipt else "nothing to do")
        elif args.finalize:
            receipt = finalize(root, rules, retired, stamp)
            print(f"finalized; receipt {receipt}" if receipt else "nothing to do")
        else:
            moves, conflicts = plan(root, rules)
            for move in moves:
                print(f"move {move['old']} -> {move['new']} ({len(move['files'])} files)")
            for conflict in conflicts:
                print(f"CONFLICT {conflict}")
            if not moves:
                print("nothing to do")
            return 1 if conflicts else 0
    except Abort as exc:
        print(f"migrate-services-layout: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
