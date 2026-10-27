#!/usr/bin/env python3
"""Check whether an AISIX route edit leaves direct models orphaned.

Given the *candidate* resources YAML (already edited locally), report:
- which adopted direct models are no longer referenced by any retained route,
- minus the explicit standalone-retention list,
- and verify no route references a missing/undefined direct model.

Exit 0 with a report; exit 1 on structural problems (duplicates, unresolved
references, a retained route left with zero targets).
"""
import argparse
import json
import sys

import yaml


def die(msg):
    sys.exit(f"aisix-orphan-check: {msg}")


def main():
    p = argparse.ArgumentParser()
    p.add_argument("resources", help="candidate resources.yaml")
    p.add_argument("--adopted", required=True,
                   help="JSON list of display_names adopted as managed direct models")
    p.add_argument("--standalone", default="[]",
                   help="JSON list of display_names explicitly retained standalone")
    args = p.parse_args()

    with open(args.resources, encoding="utf-8") as fh:
        doc = yaml.safe_load(fh)
    if not isinstance(doc, dict) or not isinstance(doc.get("models"), list):
        die("resources must contain a models list")
    models = doc["models"]
    names = [m.get("display_name") for m in models if isinstance(m, dict)]
    if any(not n for n in names):
        die("every model needs display_name")
    if len(names) != len(set(names)):
        dupes = sorted({n for n in names if names.count(n) > 1})
        die(f"duplicate display_name(s): {dupes}")
    by_name = {m["display_name"]: m for m in models}

    adopted = set(json.loads(args.adopted))
    standalone = set(json.loads(args.standalone))
    unknown_adopted = adopted - set(names)
    if unknown_adopted:
        die(f"adopted direct models missing from resources: {sorted(unknown_adopted)}")

    referenced = set()
    routed = []
    for m in models:
        routing = m.get("routing")
        if routing is None:
            continue
        routed.append(m["display_name"])
        targets = routing.get("targets")
        if not isinstance(targets, list) or not targets:
            die(f"retained route {m['display_name']} has no targets; refusing")
        for t in targets:
            if not isinstance(t, dict) or not isinstance(t.get("model"), str):
                die(f"route {m['display_name']}: target entries need a model string")
            if t["model"] not in by_name:
                die(f"route {m['display_name']} references undefined model {t['model']}")
            referenced.add(t["model"])

    orphans = sorted((adopted - referenced) - standalone)
    report = {
        "routed_models": routed,
        "referenced_direct": sorted(referenced),
        "standalone_retained": sorted(standalone & adopted),
        "orphan_candidates": orphans,
    }
    print(json.dumps(report, indent=2))
    if orphans:
        print(f"\n{len(orphans)} orphan candidate(s): deletion requires explicit approval.", file=sys.stderr)


if __name__ == "__main__":
    main()
