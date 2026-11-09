#!/usr/bin/env python3
"""Ladder verdict (task 8.2 skeleton): RUN/results.jsonl (rungs.py) -> RUN/summary.json + RUN/junit.xml; exit 0 = pass.

usage: verdict.py RUN_DIR RUNG...     (the rungs that were asked for; a rung with no result row fails)
Per row, as in the spec scenario "Ladder step passes":
  - HTTP: status 200, the response completed, and the mock got the body (sha equal, or every input item equal and
    the media parts equal: Sub2API adds top-level fields, so the whole-body sha differs on this chain);
  - WS: rc 0, 2 turns completed, turn 1 items equal at the mock, turn 2 (keeper-expanded) starts with turn 1's items;
  - every hop: oom_kill delta 0, memory.peak during the rung >= 32 MiB under its memory.max (manifest.json);
    keeper is the documented D3 exception (REPORT2.md/REPORT3.md "D3 keeper exception"; hlib.py Mem.stat()
    keeper_criterion = anon + file_dirty + file_writeback + kernel): its memory.max is routinely reached by
    reclaimable bbolt-store page cache (inactive_file), so keeper is margin-checked on keeper_criterion, not
    raw memory.peak; AISIX uses the same working-set criterion under decision (a);
  - the ticket was released and no spool or ticket file remains;
  - no container restarted (RUN/restarts.json from ladder.sh).
Not checked yet (needs the production-RAM parent cgroup): MemAvailable >= floor."""
import json, os, sys
from xml.sax.saxutils import escape, quoteattr

MIB = 1 << 20
MARGIN = 32 * MIB


def items_equal(c):
    if not c or not c.get("mock_saw"):
        return False, "mock did not see the body"
    if c.get("sha_equal"):
        return True, "sha equal"
    i = c.get("input")
    if not isinstance(i, dict):
        return False, f"input not comparable: {i}"
    if i.get("items_differ") != 0:
        return False, f"{i.get('items_differ')} items differ (first {i.get('first_diff')})"
    if c.get("media_parts_equal") is False:
        return False, "media parts differ"
    return True, f"{i.get('mock_items')} items equal"


def check(row, limits, restarts):
    fails, res = [], row.get("result") or {}
    if row.get("harness_error"):
        return [f"harness error {row['harness_error']}"]
    if "completed_turns" in res:   # WS
        if res.get("rc") != 0 or res.get("completed_turns") != 2:
            fails.append(f"ws rc={res.get('rc')} completed_turns={res.get('completed_turns')}")
        ok, why = items_equal(row.get("compare_turn1"))
        ok or fails.append("turn 1: " + why)
        c2 = row.get("compare_turn2_prefix")
        ok, why = items_equal(c2) if c2 else (False, "turn 2 not at the mock")
        ok or fails.append("turn 2: " + why)
    else:   # HTTP
        if res.get("status") != 200 or not res.get("completed"):
            fails.append(f"status {res.get('status')} completed={res.get('completed')} {str(res.get('body_head'))[:200]}")
        ok, why = items_equal(row.get("compare"))
        ok or fails.append(why)
    for h, v in (row.get("mem") or {}).items():
        if h.startswith("_") or not isinstance(v, dict):
            continue
        if (v.get("events_delta") or {}).get("oom_kill"):
            fails.append(f"{h}: oom_kill {v['events_delta']['oom_kill']}")
        peak = v.get("peak_rung")
        label = "memory.peak"
        if h in ("keeper", "aisix"):   # Decision (a): reclaimable file cache is not capacity usage
            kc = (v.get("stat_max") or {}).get("keeper_criterion")
            if kc is not None:
                peak, label = kc, "keeper_criterion (anon+file_dirty+file_writeback+kernel)"
        lim = limits.get(h)
        if lim and peak is not None and peak > lim - MARGIN:
            fails.append(f"{h}: {label} {peak // MIB} MiB > memory.max {lim // MIB} - 32 MiB")
    if row.get("ticket_released") is False:
        fails.append("ticket not released")
    if row.get("residual_files"):
        fails.append(f"residual files {row['residual_files']}")
    bad = {c: n for c, n in restarts.items() if n}
    if bad:
        fails.append(f"restarts {bad}")
    return fails


def main():
    run, asked = sys.argv[1], sys.argv[2:]
    rows = [json.loads(l) for l in open(os.path.join(run, "results.jsonl"))] if os.path.exists(os.path.join(run, "results.jsonl")) else []
    rows = [r for r in rows if "result" in r or "harness_error" in r]
    man = json.load(open(os.path.join(run, "manifest.json"))) if os.path.exists(os.path.join(run, "manifest.json")) else {}
    limits = {c: int(v["memory.max"]) for c, v in (man.get("containers") or {}).items() if str(v.get("memory.max", "")).isdigit()}
    rp = os.path.join(run, "restarts.json")
    restarts = json.load(open(rp)) if os.path.exists(rp) else {"<restarts.json missing>": 1}
    out = []
    for r in rows:
        f = check(r, limits, restarts)
        res = r.get("result") or {}
        out.append({"rung": r["rung"], "pass": not f, "failures": f, "status": res.get("status", res.get("rc")),
                    "secs": res.get("secs"), "peaks_mib": {h: (v.get("peak_rung") or 0) // MIB for h, v in (r.get("mem") or {}).items()
                                                            if not h.startswith("_") and isinstance(v, dict)}})
    seen = {r["rung"].split("-gap")[0] for r in rows}
    for a in asked:
        if a not in seen:
            out.append({"rung": a, "pass": False, "failures": ["no result row"]})
    verdict = "pass" if out and all(o["pass"] for o in out) else "fail"
    summary = {"run_id": os.path.basename(os.path.normpath(run)), "verdict": verdict, "nightly": os.environ.get("NIGHTLY") == "1",
               "cap_bytes": int(os.environ.get("CAP_BYTES", 256 * MIB)), "asked": asked, "restarts": restarts, "rungs": out}
    json.dump(summary, open(os.path.join(run, "summary.json"), "w"), indent=1)
    nf = sum(not o["pass"] for o in out)
    with open(os.path.join(run, "junit.xml"), "w") as x:
        x.write(f'<?xml version="1.0" encoding="UTF-8"?>\n<testsuite name="capacity-ladder" tests="{len(out)}" failures="{nf}">\n')
        for o in out:
            x.write(f'  <testcase classname="ladder" name={quoteattr(o["rung"])} time="{o.get("secs") or 0}">')
            if not o["pass"]:
                x.write(f'<failure message={quoteattr("; ".join(o["failures"])[:500])}>{escape(json.dumps(o["failures"]))}</failure>')
            x.write("</testcase>\n")
        x.write("</testsuite>\n")
    for o in out:
        print(f"{'PASS' if o['pass'] else 'FAIL'} {o['rung']}: status={o.get('status')} secs={o.get('secs')} {'; '.join(o['failures'])}")
    print(f"verdict {verdict}: {len(out) - nf}/{len(out)} rungs pass -> {run}/summary.json, junit.xml")
    return 0 if verdict == "pass" else 1


if __name__ == "__main__":
    sys.exit(main())
