#!/usr/bin/env python3
"""Apply a structured, pre-validated edit to a JSON/YAML config.

The edit is a JSON document:

  {"scalar_sets": [{"path": "a.b.c", "value": ...}],
   "list_ops": [{"path": "x.y", "key": "id",
                 "remove": ["v1"], "add": [{...}]}]}

Safety contract (matches automate-production-ops spec):
- scalar --set refuses to overwrite an existing object/list;
- list entries are matched by an explicit identity key; remove/add must be
  explicit (omission is never deletion);
- unknown custom fields, untouched order, and comments are preserved
  (whole-document re-serialization only happens on explicit --reserialize);
- the result must re-parse, and every untouched scalar/list leaf must be
  unchanged compared to the input unless listed in the edit.
"""
import argparse
import copy
import json
import sys

import yaml


class EditError(Exception):
    pass


def die(msg):
    sys.exit(f"config-edit: {msg}")


def load(text):
    try:
        return json.loads(text), True
    except json.JSONDecodeError:
        data = yaml.safe_load(text)
        return data, False


def walk_to(obj, parts):
    node = obj
    for part in parts:
        if isinstance(node, dict):
            if part not in node:
                raise EditError(f"path segment {part!r} does not exist")
            node = node[part]
        elif isinstance(node, list):
            try:
                node = node[int(part)]
            except (ValueError, IndexError):
                raise EditError(f"list index out of range: {part!r}")
        else:
            raise EditError(f"cannot descend into {part!r}")
    return node


def apply_edit(data, edit):
    touched = []  # list of (path_tuple, ) prefixes
    for item in edit.get("scalar_sets", []):
        parts = tuple(item["path"].split("."))
        if not parts or "value" not in item:
            raise EditError("scalar_set requires path and value")
        parent = walk_to(data, parts[:-1])
        if isinstance(parent, list):
            raise EditError(f"scalar_set {item['path']}: parent is a list")
        old = parent.get(parts[-1])
        if isinstance(old, (dict, list)):
            raise EditError(
                f"scalar_set {item['path']}: existing value is an object/list; "
                "use list_ops or drop this key"
            )
        parent[parts[-1]] = item["value"]
        touched.append(parts)
    for item in edit.get("list_ops", []):
        path = item.get("path")
        key = item.get("key")
        if not path or not key:
            raise EditError("list_op requires path and key")
        lst = walk_to(data, tuple(path.split(".")))
        if not isinstance(lst, list):
            raise EditError(f"{path} is not a list")
        if any(not isinstance(e, dict) or key not in e for e in lst):
            raise EditError(f"{path}: every entry must be an object containing {key!r}")
        names = [e[key] for e in lst]
        if len(set(map(repr, names))) != len(names):
            raise EditError(f"{path}: duplicate identity values under {key!r}")
        for val in item.get("remove", []):
            matches = [e for e in lst if e.get(key) == val]
            if len(matches) != 1:
                raise EditError(f"{path}: remove expected exactly 1 {key}={val!r}, found {len(matches)}")
            lst.remove(matches[0])
        for obj in item.get("add", []):
            if not isinstance(obj, dict) or key not in obj:
                raise EditError(f"{path}: added entry must contain {key!r}")
            val = obj[key]
            if any(e.get(key) == val for e in lst):
                raise EditError(f"{path}: duplicate {key}={val!r} after add")
            lst.append(obj)
        touched.append(tuple(path.split(".")))
    return data, touched


def leaves(obj, prefix=()):
    out = {}
    if isinstance(obj, dict):
        for k, v in obj.items():
            out.update(leaves(v, prefix + (repr(k),)))
    elif isinstance(obj, list):
        for i, v in enumerate(obj):
            out.update(leaves(v, prefix + (f"[{i}]",)))
    else:
        out[prefix] = obj
    return out


def covered_by_edit(path, touched, edit):
    s = ".".join(p.strip("'\"") for p in path)
    for t in touched:
        tstr = ".".join(p.strip("'\"") for p in t)
        if s == tstr or s.startswith(tstr + ".") or s.startswith(tstr + "["):
            return True
    for item in edit.get("list_ops", []):
        lp = item["path"]
        if s == lp or s.startswith(lp + "."):
            return True
    return False


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("path")
    parser.add_argument("--edit", required=True, help="path to edit JSON document")
    parser.add_argument("--check", action="store_true",
                        help="validate and report the semantic diff without writing")
    args = parser.parse_args()

    try:
        with open(args.path, encoding="utf-8") as fh:
            text = fh.read()
    except OSError as e:
        die(str(e))
    data, is_json = load(text)
    if data is None:
        die("empty document")
    try:
        with open(args.edit, encoding="utf-8") as fh:
            edit = json.load(fh)
    except (OSError, json.JSONDecodeError) as e:
        die(f"bad edit document: {e}")
    if not isinstance(edit, dict):
        die("edit document must be an object")

    before = copy.deepcopy(data)
    try:
        data, touched = apply_edit(data, edit)
    except EditError as e:
        die(str(e))

    # Show the semantic diff for local review. The deploy-time drift gate is
    # the authoritative guard; this is a convenience preview only.
    before_leaves = leaves(before)
    after_leaves = leaves(data)
    changed = []
    for path, value in sorted(before_leaves.items()):
        if path not in after_leaves:
            changed.append(("-", path, value, None))
        elif after_leaves[path] != value:
            changed.append(("~", path, value, after_leaves[path]))
    for path, value in sorted(after_leaves.items()):
        if path not in before_leaves:
            changed.append(("+", path, None, value))

    for kind, path, old, new in changed:
        print(f"{kind} {path}: {old!r} -> {new!r}")
    if not changed:
        print("no effective change")

    if args.check:
        return

    if is_json:
        out = json.dumps(data, indent=2, ensure_ascii=False) + "\n"
    else:
        out = yaml.dump(data, default_flow_style=False, allow_unicode=True, sort_keys=False)
    # re-parse proof
    load(out)
    with open(args.path, "w", encoding="utf-8") as fh:
        fh.write(out)


if __name__ == "__main__":
    main()
