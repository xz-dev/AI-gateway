#!/usr/bin/env python3
"""manifest -> lock emitter (task 8.2): write compatibility-lock.json from a finished ladder run.

usage: lock_from_manifest.py RUN_DIR [OUT]      (OUT default RUN_DIR/compatibility-lock.json)
Reads RUN_DIR/manifest.json (manifest.py), RUN_DIR/summary.json (ci/verdict.py) and RUN_DIR/results.jsonl.
A red run publishes no lock: exit 1 and nothing written unless summary.json says verdict "pass".
Exit 2 when a hop has no image digest, or runs a plain base image with a mounted binary (gate 7.5 style runs):
such a run names no component image, so it cannot enter a lock."""
import hashlib, json, os, sys, time

MIB = 1 << 20
HOP_KEYS = {"keeper": "keeper", "bridge": "bridge", "sub2api": "sub2api", "aisix": "aisix", "cpa": "cpa",
            "pg": "postgres", "redis": "redis", "mock": "mock_base"}
COMPONENTS = ("keeper", "bridge", "sub2api", "aisix", "cpa")
BASE_IMAGES = ("docker.io/library/alpine", "docker.io/library/debian")
# design D4 / keeper gate.go footprint(): the factors keeper's gate uses today (basis "declared")
FOOTPRINT = {"basis": "declared",
             "k_disk": {"keeper": 2, "bridge": 1, "sub2api": 1, "aisix": 1, "cpa": 2},
             "k_mem": {"keeper": 0, "bridge": 0, "sub2api": 0.1, "aisix": 0.1, "cpa": 0.1},
             "k_skel": {"keeper": 0, "bridge": 0, "sub2api": 17, "aisix": 5, "cpa": 9},
             "s_ticket_bytes": 8 * MIB, "p_h_bytes": 16 * MIB}
# design D2: S = 16 MiB without a ticket, 8 MiB with one; L per fallback path is measured by CI (null until then)
INLINE = {h: {"untick": 16 * MIB, "ticketed": 8 * MIB} for h in ("sub2api", "aisix", "cpa")}
FALLBACK = {"/v1/chat/completions": None, "/responses/compact": None}


def sha(p):
    return hashlib.sha256(open(p, "rb").read()).hexdigest()


def repo(ref):
    """image reference without tag or digest"""
    name = ref.split("@", 1)[0]
    last = name.rsplit("/", 1)
    return name.rsplit(":", 1)[0] if ":" in last[-1] else name


def main():
    run = sys.argv[1]
    out = sys.argv[2] if len(sys.argv) > 2 else os.path.join(run, "compatibility-lock.json")
    summary = json.load(open(os.path.join(run, "summary.json")))
    if summary.get("verdict") != "pass":
        print(f"verdict {summary.get('verdict')!r}: red run, no lock written", file=sys.stderr)
        return 1
    man = json.load(open(os.path.join(run, "manifest.json")))
    cons, images, errs = man.get("containers") or {}, {}, []
    for c, key in HOP_KEYS.items():
        x = cons.get(c)
        if not x:
            errs.append(f"{c}: not in the manifest"); continue
        dig = x.get("image_digest") or ""
        if not dig.startswith("sha256:") or len(dig) != 71:
            errs.append(f"{c}: no image digest ({x.get('image')})"); continue
        if c in COMPONENTS and repo(x.get("image") or "") in BASE_IMAGES:
            errs.append(f"{c}: base image {x.get('image')} with a mounted binary, not a component image"); continue
        images[key] = repo(x["image"]) + "@" + dig
    if errs:
        print("no lock: " + "; ".join(errs), file=sys.stderr)
        return 2
    peaks = {}
    for line in open(os.path.join(run, "results.jsonl")):
        row = json.loads(line)
        for h, v in (row.get("mem") or {}).items():
            if not h.startswith("_") and h in COMPONENTS + ("pg", "redis") and isinstance(v, dict):
                p = v.get("peak_rung")
                if p is not None:
                    peaks[h] = max(peaks.get(h, 0), p)
    limits = {h: int(cons[h]["memory.max"]) for h in COMPONENTS + ("pg", "redis")
              if h in cons and str(cons[h].get("memory.max", "")).isdigit()}
    lock = {"schema": "ai-gateway/compatibility-lock/v1",
            "generated": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            "source": {"run_id": summary.get("run_id") or os.path.basename(os.path.normpath(run)), "verdict": "pass",
                       "rungs": [r["rung"] for r in summary.get("rungs", [])], "nightly": bool(summary.get("nightly")),
                       "manifest_sha256": sha(os.path.join(run, "manifest.json")),
                       "summary_sha256": sha(os.path.join(run, "summary.json")),
                       "harness_git": (man.get("harness") or {}).get("git")},
            "images": images,
            "config_sha256": {k: v for k, v in (man.get("file_sha256") or {}).items() if v},
            "cap_bytes": int(summary.get("cap_bytes") or 256 * MIB),
            "peaks_bytes": peaks, "mem_limits_bytes": limits, "footprint": FOOTPRINT,
            "inline_limits_bytes": INLINE, "fallback_limits_bytes": FALLBACK}
    try:
        import jsonschema
        schema = json.load(open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "compatibility-lock.schema.json")))
        jsonschema.validate(lock, schema)
    except ImportError:
        print("jsonschema not installed: lock written without schema validation", file=sys.stderr)
    with open(out, "w") as f:
        json.dump(lock, f, indent=2); f.write("\n")
    print(f"lock: {len(images)} images, {len(peaks)} peaks -> {out}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
