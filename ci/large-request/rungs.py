#!/usr/bin/env python3
"""Run chain rungs against the live local stack (stack.sh up). usage: rungs.py RUNG... ; results -> $RUN/results.jsonl"""
import datetime, glob, hashlib, json, os, re, socket, subprocess, sys, threading, time, urllib.request
import hlib
from hlib import T, sh, load, HOPS, FS, PFX

RUN = os.environ["RUN"]
os.makedirs(T + "/bodies", exist_ok=True)
REC = RUN + "/rec"
KEY = "sk-capacity-local-test-key-000000"
# KEEPER = the client entry: keeper, or keepalive / firewall APISIX in full-chain mode (stack.sh ENTRY_URL).
# KEEPER_DIRECT always reaches keeper's own published port.
KEEPER_DIRECT, S2A = "http://127.0.0.1:18085", "http://127.0.0.1:18090"
KEEPER = os.environ.get("ENTRY_URL", KEEPER_DIRECT)
MIB = 1 << 20
KIB = 1 << 10
GREP = re.compile(r"ticket|lease|capacity|spool|request_too_large|insufficient|413|503|507|panic|error", re.I)


def body(shape, n, ws=False, api="responses"):
    p = f"{T}/bodies/{shape}-{n}{'-ws' if ws else ''}{'-chat' if api == 'chat' else ''}.json"
    # Sub2API D10 corpora (copied from the Sub2API M2 acceptance kit): d10-utext (gen_utext.py), d10-small-items
    # (~90-byte items, gen_small.py), d10-tiny-msgs (~30-byte items, gen_tiny.py)
    gen = {"utext": "gen_utext.py", "small": "gen_small.py", "tiny": "gen_tiny.py"}.get(shape)
    if shape == "sdk" and not os.path.exists(p + ".meta"):   # OpenAI Python SDK-shaped Responses body (gen_sdk.py)
        assert api == "responses"
        args = [sys.executable, f"{T}/gen_sdk.py", str(n), p] + (["--ws"] if ws else [])
        open(p + ".meta", "w").write(subprocess.run(args, check=True, capture_output=True, text=True).stdout)
    if not os.path.exists(p + ".meta") and gen:
        assert n % MIB == 0 and not ws and api == "responses"
        out = subprocess.run([sys.executable, f"{T}/{gen}", str(n // MIB), p], check=True, capture_output=True, text=True).stdout
        open(p + ".meta", "w").write(json.dumps({"out": p, "shape": shape, "bytes": os.path.getsize(p), "gen": out.strip()}))
    if not os.path.exists(p + ".meta"):
        args = [sys.executable, f"{T}/gen.py", shape, str(n), p, "--api", api] + (["--ws"] if ws else [])
        open(p + ".meta", "w").write(subprocess.run(args, check=True, capture_output=True, text=True).stdout)
    return p, json.loads(open(p + ".meta").read())


def cont_body(n=32768):
    p = f"{T}/bodies/cont-{n}.json"
    head = '{"type":"response.create","model":"gpt-5.1-codex","stream":true,"store":false,"previous_response_id":"{{PREV}}","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"'
    tail = '"}]}]}'
    open(p, "w").write(head + "z" * (n - len(head) - len(tail)) + tail)
    return p


def http(path, bpath, base=KEEPER, timeout=900, headers=None):
    """Timed HTTP/1.1 POST over a raw socket (Connection: close). The body goes out in 64 KiB pieces and
    every send and receive completion is timestamped (unix time). t0 = just before the first request byte.
    Returns status, body head, and timing: upload_end, first_resp, terminal (terminal SSE event, or EOF for
    a plain body), and the longest span with no byte moving either way (lower bound, 64 KiB send pieces)."""
    host, port = base.split("//")[1].split(":")
    size = os.path.getsize(bpath)
    head = (f"POST {path} HTTP/1.1\r\nHost: {host}:{port}\r\nAuthorization: Bearer {KEY}\r\n"
            f"Content-Type: application/json\r\n{''.join(f'{k}: {v}{chr(13)}{chr(10)}' for k, v in (headers or {}).items())}"
            f"Content-Length: {size}\r\nConnection: close\r\n\r\n").encode()
    ev, resp, term, first, err, marks = [], bytearray(), None, None, None, []
    s = socket.create_connection((host, int(port)), timeout=timeout)
    t0 = time.time()
    try:
        s.sendall(head); ev.append(time.time())
        with open(bpath, "rb") as f:
            while True:
                b = f.read(65536)
                if not b:
                    break
                s.sendall(b); ev.append(time.time())
    except OSError as e:
        err = "send: " + repr(e)
    upload_end = time.time()
    try:
        while True:
            b = s.recv(65536)
            if not b:
                break
            now = time.time(); ev.append(now)
            first = first or now
            # frames the mock never sends: SSE comments (": ..."), response.in_progress (keepalive startup frame)
            for m in re.findall(rb"(?m)^:[^\n]{0,40}|response\.in_progress", b):
                if len(marks) < 20:
                    marks.append([round(now - t0, 3), m.decode("utf-8", "replace")])
            if len(resp) < 1 << 20:
                resp += b
            if term is None and re.search(rb"response\.completed|response\.failed|event: error|\[DONE\]|\"type\":\"error\"", resp):
                term = now
    except OSError as e:
        err = (err or "") + " recv: " + repr(e)
    end = time.time()
    s.close()
    term = term or end
    txt = resp.decode("utf-8", "replace")
    hdr, _, body = txt.partition("\r\n\r\n")
    try:
        st = int(hdr.split()[1])
    except (IndexError, ValueError):
        st = 0
    if "chunked" in hdr.lower():   # de-chunk for the body head
        out, rest = [], body
        while rest:
            ln, _, rest = rest.partition("\r\n")
            try:
                n = int(ln.strip() or "0", 16)
            except ValueError:
                break
            if n == 0:
                break
            out.append(rest[:n]); rest = rest[n + 2:]
        body = "".join(out)
    pts = [t0] + [t for t in ev if t <= term] + [term]
    gap, gap_at = 0.0, 0.0
    for a, b in zip(pts, pts[1:]):
        if b - a > gap:
            gap, gap_at = b - a, a - t0
    return {"status": st, "secs": round(end - t0, 3), "body_head": body[:700], "body_len": len(body), "error": err,
            "completed": "response.completed" in body or "[DONE]" in body, "synthetic_marks": marks,
            "timing": {"t0_unix": t0, "upload_end": round(upload_end - t0, 3), "first_resp": round(first - t0, 3) if first else None,
                       "terminal": round(term - t0, 3), "longest_gap": round(gap, 3), "longest_gap_at": round(gap_at, 3)}}


def ws(frames, path="/v1/responses", env=None):
    t0 = time.time()
    url = "ws://" + KEEPER.split("//", 1)[1] + path
    r = subprocess.run([f"{T}/bin/wsclient", url, *frames], capture_output=True,
                       text=True, timeout=1000, env={**os.environ, "WS_KEY": KEY, **(env or {})})
    lines = [l[:600] for l in (r.stdout + r.stderr).splitlines() if not l.startswith("timing=")]
    timing = [json.loads(l[7:]) for l in r.stdout.splitlines() if l.startswith("timing=")]
    return {"rc": r.returncode, "secs": round(time.time() - t0, 1), "lines": lines, "timing": timing,
            "completed_turns": sum("terminal={\"type\":\"response.completed\"" in l for l in lines),
            "refusals_503": sum(" retry=" in l for l in lines),
            "retry_outcomes": [l for l in lines if "retry-outcome=" in l]}


def refused_mem(res):
    return res.get("status") == 503 and "insufficient_memory" in res.get("body_head", "")


def http_retry(path, bpath, retries=5, wait=2.0, on_refusal=None, **kw):
    """HTTP POST; a retryable 503 insufficient_memory is sent again after `wait` s, up to `retries` times. The
    result is the last attempt's, with every refused attempt (status, body head, timing) under "refusals".
    on_refusal(res), if given, runs as soon as a refused attempt's response is in (before the wait)."""
    refusals = []
    while True:
        res = http(path, bpath, **kw)
        if not refused_mem(res) or len(refusals) >= retries:
            break
        refusals.append({k: res[k] for k in ("status", "body_head", "secs", "timing")})
        if on_refusal:
            on_refusal(res)
        time.sleep(wait)
    res["refusals_503"] = refusals
    res["retry_outcome"] = ("completed" if res.get("completed") else f"status {res.get('status')}") if refusals else None
    return res


def jload(p):
    return json.load(open(p, "rb"))


def compare(cpath, rec, prefix_of=None):
    """sha first; else semantic: top-level key diffs and input item diffs (no content printed)."""
    if not rec:
        return {"mock_saw": False}
    csha = hashlib.sha256(open(cpath, "rb").read()).hexdigest()
    res = {"mock_saw": True, "client_bytes": os.path.getsize(cpath), "mock_bytes": rec["bytes"], "sha_equal": csha == rec["sha256"]}
    if res["sha_equal"]:
        return res
    try:
        c, m = jload(cpath), jload(f"{REC}/{rec['seq']}.body")
    except Exception as e:
        res["semantic"] = f"unparsable: {e}"; return res
    c.pop("type", None)
    ci, mi = c.pop("input", None), m.pop("input", None)
    res["top_changed"] = sorted(k for k in set(c) | set(m) if c.get(k, "<absent>") != m.get(k, "<absent>"))
    res["top_detail"] = {k: [str(c.get(k, "<absent>"))[:60], str(m.get(k, "<absent>"))[:60]] for k in res["top_changed"]
                         if k != "instructions"}
    if prefix_of is not None:   # continuation: expanded input must start with turn-1 input
        ci = prefix_of
        mi = mi[:len(ci)] if isinstance(mi, list) else mi
    if isinstance(ci, list) and isinstance(mi, list):
        diff = [i for i, (a, b) in enumerate(zip(ci, mi)) if a != b]
        res["input"] = {"client_items": len(ci), "mock_items": len(mi), "items_differ": len(diff), "first_diff": diff[:3]}
        if diff:
            a, b = ci[diff[0]], mi[diff[0]]
            res["input"]["first_diff_keys"] = sorted(k for k in set(a) | set(b) if a.get(k) != b.get(k)) if isinstance(a, dict) and isinstance(b, dict) else "type"
        media_c = [p.get("image_url") for it in ci if isinstance(it, dict) for p in (it.get("content") or []) if isinstance(p, dict) and p.get("type") == "input_image"]
        media_m = [p.get("image_url") for it in mi if isinstance(it, dict) for p in (it.get("content") or []) if isinstance(p, dict) and p.get("type") == "input_image"]
        res["media_parts_equal"] = media_c == media_m and len(media_c) > 0 if media_c else None
    elif ci is not None or mi is not None:
        res["input"] = "messages/other shape"
        res["messages_equal"] = c.get("messages") == m.get("messages")
    return res


def hop_logs(since):
    out = {}
    for h in HOPS:
        r = sh("podman", "logs", "--since", since, PFX + "-" + h, timeout=120)
        ls = [l[:300] for l in (r.stdout + r.stderr).splitlines() if GREP.search(l)]
        if ls:
            out[h] = ls[-6:]
    return out


def seq_now():
    p = REC + "/requests.jsonl"
    return sum(1 for _ in open(p)) if os.path.exists(p) else 0


def uptime():
    return sh("uptime").stdout.strip()


def settle(secs=30):
    time.sleep(secs)


def milog_at(decisions):
    """Add the 0.5 s logger's reading to each derived decision: fed / kernel MiB of the last milog.tsv row at or
    before the ticket mtime (also derived; the 10 ms DecisionWatch value is the closer one)."""
    p = RUN + "/milog.tsv"
    if not decisions or not os.path.exists(p):
        return decisions
    rows = [l.rstrip("\n").split("\t") for l in open(p)][1:]
    for d in decisions:
        prior = [r for r in rows if r[0] and float(r[0]) <= d["mtime"]]
        if prior:
            d["milog_fed_mib"], d["milog_kernel_mib"] = prior[-1][2], prior[-1][4]
            d["milog_age_s"] = round(d["mtime"] - float(prior[-1][0]), 2)
    return decisions


def measured(name, fn, expect, n_bytes=None):
    since = datetime.datetime.now().astimezone().isoformat()
    row = {"rung": name, "load_before": load(), "uptime_before": uptime(), "expect": expect}
    s0 = seq_now(); mem = hlib.Mem(); mem.start(); smp = hlib.Sampler(mem); smp.start()
    dw = hlib.DecisionWatch(); dw.start()
    flow = hlib.NetFlow(); flow.start()
    probe = hlib.AisixProbe() if os.environ.get("AISIX_PROBE") else None
    if probe:
        probe.start()
    try:
        row["result"] = fn()
    finally:
        row["leases"] = smp.finish()
        row["decision_memavail"] = milog_at(dw.finish())
        series = flow.finish()
        if probe:
            row["aisix_probe"] = probe.finish()
    row["uptime_after"] = uptime()
    json.dump(series, open(f"{RUN}/flow-{name}.json", "w"))
    res = row.get("result") or {}
    if n_bytes and isinstance(res, dict):
        if "timing" in res and isinstance(res["timing"], dict):
            tm = res["timing"]
            row["flow"] = hlib.flow_summary(series, tm["t0_unix"], n_bytes, t_end=tm["t0_unix"] + tm["terminal"])
        elif isinstance(res.get("timing"), list):
            row["flow_turns"] = [hlib.flow_summary(series, tm["t0_unix"], tm["frame"], t_end=tm["t0_unix"] + tm["terminal"])
                                 for tm in res["timing"]]
    t0 = time.time()
    while hlib.ticket_inodes() and time.time() - t0 < 120:
        time.sleep(0.5)
    row["ticket_left_after_secs"] = round(time.time() - t0, 1)
    row["ticket_released"] = not hlib.ticket_inodes()
    row["load_after"] = load()
    row["mem"] = mem.stop()
    row["mock"] = hlib.mock_records(REC, s0)
    row["residual_files"] = hlib.residual_files()
    row["hop_logs"] = hop_logs(since)
    # Sub2API raw-list decisions in this rung: count of the event name only (no line content kept)
    r = sh("podman", "logs", "--since", since, PFX + "-sub2api", timeout=120)
    row["s2a_off_list"] = sum("large_request.off_list" in l for l in (r.stdout + r.stderr).splitlines())
    LAST_END.update(rung=name, t=time.time())
    return row


LAST_END = {"rung": None, "t": None}   # delta 7.5 sizeretry: the previous measured() rung and when it returned


def drop_mock_bodies(rows):
    for r in rows:
        try:
            os.unlink(f"{REC}/{r['seq']}.body")
        except OSError:
            pass


def strip_rec(rows):
    """Mock records without the full header map (kept: hits, bytes, sha, path)."""
    return [{k: r[k] for k in ("seq", "path", "bytes", "sha256", "body_hits", "header_hits", "mode")} for r in rows]


def http_rung(name, shape, n, expect, path="/v1/responses", retries=0):
    bp, meta = body(shape, n)
    row = measured(name, lambda: http_retry(path, bp, retries=retries), expect, n_bytes=n)
    row["gen"] = meta; row["client_path"] = path
    resp_recs = [r for r in row["mock"] if r["path"].endswith("/responses")]
    row["compare"] = compare(bp, resp_recs[-1] if resp_recs else None)
    drop_mock_bodies(row["mock"])
    row["mock"] = strip_rec(row["mock"])
    return row


def ws_rung(name, shape, n, expect, turn1=None, env=None, path="/v1/responses"):
    t1, meta = body(shape, turn1 or n - 33792, ws=True)
    t2 = cont_body()
    row = measured(name, lambda: ws([t1, t2], path=path, env=env), expect, n_bytes=n)
    row["ws_env"] = env; row["client_path"] = path
    row["gen_turn1"] = meta
    recs = [r for r in row["mock"] if r["path"].endswith("/responses")]
    row["compare_turn1"] = compare(t1, recs[0] if recs else None)
    if len(recs) > 1:
        c1 = jload(t1)
        row["compare_turn2_prefix"] = compare(t2, recs[1], prefix_of=c1["input"])
        row["turn2_mock_bytes"] = recs[1]["bytes"]
    drop_mock_bodies(row["mock"])
    row["mock"] = strip_rec(row["mock"])
    return row


def chat17(name):
    bp, meta = body("text", 17 * MIB, api="chat")
    a = measured(name + "-keeper", lambda: http("/v1/chat/completions", bp), "same as sub2api direct")
    b = measured(name + "-sub2api-direct", lambda: http("/v1/chat/completions", bp, base=S2A), "413 request_too_large naming path and L")
    for r in (a, b):
        r["gen"] = meta; r["mock"] = strip_rec(r["mock"])
    a["same_as_direct"] = {"status": a["result"]["status"] == b["result"]["status"],
                           "body": a["result"]["body_head"] == b["result"]["body_head"]}
    return [a, b]


def set_mode(m):
    open(REC + "/mode", "w").write(m)


def direct_rung(name, shape, n, expect, base, port_label):
    bp, meta = body(shape, n)
    row = measured(name, lambda: http("/v1/responses", bp, base=base), expect)
    row["gen"] = meta; row["entry"] = port_label; row["mock"] = strip_rec(row["mock"])
    return row


PROVIDER_413 = '{"error":{"message":"mock provider: request entity too large","type":"invalid_request_error","code":"provider_request_too_large"}}'
PROVIDER_413_CTX = '{"error":{"message":"mock provider: your input exceeds the context window of this model","type":"invalid_request_error","code":"context_length_exceeded"}}'
POST_PY = """import sys, urllib.request, urllib.error
url, key, f = sys.argv[1:4]
h = {"Authorization": "Bearer " + key, "Content-Type": "application/json"}
h.update(x.split(": ", 1) for x in sys.argv[4:])
r = urllib.request.Request(url, data=open(f, "rb").read(), headers=h)
try:
    with urllib.request.urlopen(r, timeout=300) as x: print(x.status); print(x.read(700).decode("utf-8", "replace"))
except urllib.error.HTTPError as e: print(e.code); print(e.read(700).decode("utf-8", "replace"))
"""
DIRECT = {"cpa": ("http://cli-proxy-api:8317/v1/responses", "cap-cpa-local-key"),
          "aisix": ("http://aisix:3000/v1/responses", "capacity-test-local-key"),
          "sub2api": ("http://sub2api:8080/v1/responses", KEY),
          "bridge": ("http://bridge:8086/v1/responses", KEY)}


def direct_post(hop, bpath, headers=()):
    """POST a body straight to one inner hop from a throwaway container on the chain network."""
    open(RUN + "/post.py", "w").write(POST_PY)
    url, key = DIRECT[hop]
    r = sh("podman", "run", "--rm", "--network", PFX + "-net", "-v", RUN + "/post.py:/post.py:ro",
           "-v", os.path.dirname(bpath) + ":/b:ro", "docker.io/library/python:3.13-alpine", "python3", "/post.py", url, key,
           "/b/" + os.path.basename(bpath), *headers, timeout=400)
    out = r.stdout.splitlines()
    try:
        return {"status": int(out[0]), "body_head": "\n".join(out[1:])[:700]}
    except (IndexError, ValueError):
        return {"status": 0, "body_head": (r.stdout + r.stderr)[-500:]}


def provider413(name, modes=("413", "413ctx"), sizes=(MIB, 64 * MIB)):
    """Provider 413 by cause: a byte-size 413 must reach the client unchanged; a context-length 413 is
    relabelled once, at CPA (code context_too_large), and then passes AISIX, Sub2API, bridge and keeper
    unchanged. Each mode: chain 1 MiB and 64 MiB, plus the same 1 MiB body straight to CPA, AISIX and Sub2API."""
    rows = []
    for mode, prov in (("413", PROVIDER_413), ("413ctx", PROVIDER_413_CTX)):
        if mode not in modes:
            continue
        set_mode(mode)
        try:
            rs = [http_rung(f"{name}-{mode}-{n // MIB}m", "image", n, "413; byte-size cause unchanged; context cause relabelled at CPA only; ticket released")
                  for n in sizes]
            bp, _ = body("image", MIB)
            direct = {h: direct_post(h, bp) for h in ("cpa", "aisix", "sub2api")}
        finally:
            set_mode("ok")
        for r in rs:
            b = r["result"]["body_head"].strip()
            r["provider_body"] = prov
            r["direct"] = direct
            r["client_equals_provider"] = b == prov
            r["client_equals_cpa_direct"] = b == direct["cpa"]["body_head"].strip()
            r["cpa_relabelled"] = direct["cpa"]["body_head"].strip() != prov
            r["mock_attempts"] = len([m for m in r["mock"] if m["path"].endswith("/responses")])
        rows += rs
    return rows


def inner503(name, only=None):
    """Shrink an inner hop's cgroup (no swap) so its per-hop memory charge refuses; restore after."""
    rows = []
    for hop, mem, full, shape, n in (("cpa", os.environ.get("CPA_SHRINK", "110m"), "1g", "image", 64 * MIB), ("sub2api", "200m", "320m", "text", 16 * MIB)):
        if only and hop != only:
            continue
        up = sh("podman", "update", "--memory", mem, "--memory-swap", mem, PFX + "-" + hop)
        applied = open(hlib.cgdir(hop) + "/memory.max").read().strip()
        try:
            r = http_rung(f"{name}-{hop}@{mem}", shape, n, f"503 insufficient_memory hop {hop} with limit_bytes, unchanged at client")
        finally:
            sh("podman", "update", "--memory", full, "--memory-swap", full, PFX + "-" + hop)
        r["shrunk"] = {hop: mem, "memory.max_during": applied, "update_rc": up.returncode, "update_err": up.stderr[-200:]}
        rows.append(r)
    return rows


def inner507(name, extra_mib):
    st = os.statvfs(FS)
    free = st.f_bavail * st.f_frsize
    fill = free - (1 << 30) - extra_mib * MIB
    path = FS + "/filler.bin"
    sh("fallocate", "-l", str(fill), path)
    try:
        row = http_rung(f"{name}-free-reserve+{extra_mib}m", "image", 16 * MIB, "507 insufficient_storage from an inner hop, unchanged")
    finally:
        os.unlink(path)
    row["filler_bytes"] = fill
    return [row]


def fresh_s2a_text16(name):
    """Restart Sub2API (same container, same parent cgroup), send text 16 MiB as soon as /health answers."""
    t0 = time.time()
    sh("podman", "restart", "-t", "5", PFX + "-sub2api", timeout=120)
    for _ in range(300):
        r = sh("curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "-m", "2", S2A + "/health")
        if r.stdout == "200":
            break
        time.sleep(0.2)
    healthy = round(time.time() - t0, 1)
    row = http_rung(name, "text", 16 * MIB, "200, or 503 insufficient_memory hop sub2api (named)")
    row["s2a_restart_to_health_s"] = healthy
    res = row["result"]
    row["accepted"] = res["status"] == 200 or (res["status"] == 503 and '"hop":"sub2api"' in res["body_head"]
                                                and '"code":"insufficient_memory"' in res["body_head"])
    return [row]


def seq5(name):
    """5 sequential HTTP image 256 MiB requests on one stack; each row keeps every hop's starting memory."""
    return [http_rung(f"{name}-{i}", "image", 256 * MIB, "200 every time; starting working set recorded") for i in range(1, 6)]


def seq5_wait(name, need=(600 + 232) * MIB, max_wait=180):
    """5 sequential HTTP image 256 MiB requests on one stack. Before turns 2..5, wait (up to max_wait s) until
    keeper's meminfo source shows the D4 need for one 256 MiB ticket (R_mem 600 MiB + 232 MiB floors)."""
    rows = []
    for i in range(1, 6):
        waited, t0 = None, time.time()
        if i > 1:
            while time.time() - t0 < max_wait and (hlib.meminfo_avail() or 0) < need:
                time.sleep(2)
            waited = round(time.time() - t0, 1)
        avail = hlib.meminfo_avail()
        r = http_rung(f"{name}-{i}", "image", 256 * MIB, "200 every time after MemAvailable recovers; starting working set recorded")
        r["waited_s"], r["avail_at_send_mib"] = waited, (avail or 0) // MIB
        rows.append(r)
    return rows


def decay(name, offsets=(0, 10, 30, 60)):
    """Retention after the last turn: per-hop anon and working set at +0, +10, +30, +60 s (no traffic)."""
    t0, snaps = time.time(), []
    for off in offsets:
        time.sleep(max(0, t0 + off - time.time()))
        s = hlib.hop_snapshot(); s["offset_s"] = off; snaps.append(s)
    return {"rung": name + "-decay", "result": {"status": "decay"}, "decay": snaps}


def diag_seq5(name):
    """seq5 256 MiB with no wait between turns, then the retention samples. Idle snapshot first."""
    idle = {"rung": name + "-idle", "result": {"status": "idle"}, "decay": [hlib.hop_snapshot()]}
    return [idle] + seq5(name) + [decay(name)]


def diag_ws245(name):
    idle = {"rung": name + "-idle", "result": {"status": "idle"}, "decay": [hlib.hop_snapshot()]}
    r = ws_rung(name, "image", MB245, "turn 1 and the continuation turn 2 both admitted", turn1=MB245)
    return [idle, r, decay(name)]


def diag_seq5_idle60(name, offsets=(0, 30, 60)):
    """Part 3: seq5 256 MiB with no wait, then 60 s idle with snapshots at +0/+30/+60 s, then turn 6 at +60 s,
    then the same snapshots after turn 6."""
    idle = {"rung": name + "-idle", "result": {"status": "idle"}, "decay": [hlib.hop_snapshot()]}
    rows = [idle] + seq5(name)
    d = decay(name, offsets); d["rung"] = name + "-idle60"; rows.append(d)
    rows.append(http_rung(f"{name}-6", "image", 256 * MIB, "turn 6 at +60 s after turn 5"))
    rows.append(decay(name, offsets))
    return rows


def diag_ws245_d(name, offsets=(0, 30, 60)):
    idle = {"rung": name + "-idle", "result": {"status": "idle"}, "decay": [hlib.hop_snapshot()]}
    r = ws_rung(name, "image", MB245, "turn 1 and the continuation turn 2 both admitted", turn1=MB245)
    return [idle, r, decay(name, offsets)]


TINY = '{"model":"gpt-5.1-codex","stream":true,"store":false,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}'


def concurrency(name, tiny_n=100):
    """1 ticketed 256 MiB request; 1 s after its first byte, 100 tiny requests at once (one thread each)."""
    tp = f"{T}/bodies/tiny.json"
    open(tp, "w").write(TINY)
    bp, meta = body("image", 256 * MIB)
    tiny = []

    def fire():
        out = {}
        def one(i):
            out[i] = http(f"/v1/responses", tp, timeout=600)
        big = {}
        tb = threading.Thread(target=lambda: big.setdefault("r", http("/v1/responses", bp)))
        tb.start()
        time.sleep(1.0)
        ths = [threading.Thread(target=one, args=(i,)) for i in range(tiny_n)]
        for t in ths:
            t.start()
        for t in ths:
            t.join()
        tb.join()
        tiny.extend(out[i] for i in sorted(out))
        return big["r"]

    row = measured(name, fire, "ticketed 256 MiB 200; tiny requests 200 (or a named refusal)", n_bytes=256 * MIB)
    resp_recs = [r for r in row["mock"] if r["path"].endswith("/responses") and r["bytes"] > MIB]
    row["compare"] = compare(bp, resp_recs[-1] if resp_recs else None)
    drop_mock_bodies(row["mock"])
    row["mock_count"] = len(row["mock"])
    row["mock"] = strip_rec(row["mock"])
    st = {}
    for r in tiny:
        k = str(r["status"]) + ("" if r["status"] == 200 else " " + (re.search(r'"code":"([^"]+)"', r["body_head"]) or [None, "?"])[1])
        st[k] = st.get(k, 0) + 1
    lat = sorted(r["timing"]["terminal"] for r in tiny)
    row["tiny"] = {"n": len(tiny), "status": st, "completed": sum(r["completed"] for r in tiny),
                   "terminal_s_p50": lat[len(lat) // 2] if lat else None, "terminal_s_max": lat[-1] if lat else None,
                   "samples_non200": [r["body_head"][:300] for r in tiny if r["status"] != 200][:3]}
    row["gen"] = meta
    return [row]


MB245 = 245_000_000

# ---- M2 exit gate, OpenSpec task 7.5 -------------------------------------------------------------------------------
RETRIES = int(os.environ.get("GATE_RETRIES", "5"))   # client retries of a 503 insufficient_memory, 2 s apart


def gate_seq5(name, offsets=(0, 30, 60)):
    """seq5 256 MiB on one stack: turns 2..5 start 2-3 s after the previous turn ends; then 60 s idle with
    snapshots at +0/+30/+60 s, turn 6 at +60 s, the same snapshots. A retryable 503 insufficient_memory is
    retried by the client (2 s, up to RETRIES); refusals and the retry outcome are kept per turn."""
    idle = {"rung": name + "-idle", "result": {"status": "idle"}, "decay": [hlib.hop_snapshot()]}
    rows = [idle] + [http_rung(f"{name}-{i}", "image", 256 * MIB, "200; a turn-2 503 insufficient_memory is allowed and retried",
                               retries=RETRIES) for i in range(1, 6)]
    d = decay(name, offsets); d["rung"] = name + "-idle60"; rows.append(d)
    rows.append(http_rung(f"{name}-6", "image", 256 * MIB, "turn 6 at +60 s after turn 5", retries=RETRIES))
    rows.append(decay(name, offsets))
    return rows


def gate_ws245(name, offsets=(0, 30, 60)):
    """WS-245 continuation twice, each on a fresh socket and conversation: turn 2 sent 1 s after turn 1 ends,
    then turn 2 sent 60 s after turn 1 ends. A refused turn 2 is retried on a new socket (2 s, up to RETRIES)."""
    idle = {"rung": name + "-idle", "result": {"status": "idle"}, "decay": [hlib.hop_snapshot()]}
    env = {"WS_RETRY": str(RETRIES), "WS_RETRY_WAIT_S": "2"}
    a = ws_rung(name + "-gap1", "image", MB245, "both turns admitted (turn 2 1 s after turn 1; a 503 is retried)",
                turn1=MB245, env={**env, "WS_GAP_S": "1"})
    da = decay(name + "-gap1", offsets)
    b = ws_rung(name + "-gap60", "image", MB245, "both turns admitted (turn 2 60 s after turn 1; a 503 is retried)",
                turn1=MB245, env={**env, "WS_GAP_S": "60"})
    return [idle, a, da, b, decay(name + "-gap60", offsets)]


# ---- rung sdk: OpenAI Python SDK-shaped Responses bodies (gen_sdk.py) -----------------------------------------------

def sdk_cont_body(n=32768):
    p = f"{T}/bodies/sdk-cont-{n}-ws.json"
    if not os.path.exists(p + ".meta"):
        args = [sys.executable, f"{T}/gen_sdk.py", str(n), p, "--ws", "--cont"]
        open(p + ".meta", "w").write(subprocess.run(args, check=True, capture_output=True, text=True).stdout)
    return p, json.loads(open(p + ".meta").read())


# Allowed item differences at the mock (gate 7.5 decision, rung sdk): CPA drops `id` and `encrypted_content` from
# every `reasoning` input item (REPORT3 "Rung sdk"). Any other difference fails the rung.
SDK_ALLOWED = {"reasoning.id:absent in mock", "reasoning.encrypted_content:absent in mock"}


def item_diff_kinds(ci, mi):
    """Semantic (parsed JSON) item diff: "<item type>.<key>:<absent in mock|added by chain|changed>" -> count."""
    kinds, differ = {}, 0
    for a, b in zip(ci, mi):
        if a == b:
            continue
        differ += 1
        if not (isinstance(a, dict) and isinstance(b, dict)):
            kinds["non-object"] = kinds.get("non-object", 0) + 1; continue
        for k in set(a) | set(b):
            if a.get(k, "<absent>") != b.get(k, "<absent>"):
                kd = f"{a.get('type', 'message')}.{k}:" + ("absent in mock" if k not in b else "added by chain" if k not in a else "changed")
                kinds[kd] = kinds.get(kd, 0) + 1
    return kinds, differ


def sdk_check(cpath, meta, rec, where):
    """Items of an SDK-shaped body at the mock. where="all": the mock input is the client's items; "prefix": the
    mock input starts with them (turn 1 at the head of an expanded turn 2); "end": it ends with them (turn 2's own).
    pass = same item count and every item semantically equal, except the SDK_ALLOWED differences.
    Information only (bytes): byte_equal = the client's raw item region (gen_sdk input_span) appears in the mock body
    unchanged; items_found_bytes = items whose raw bytes (gen_sdk writes each item as openapi_dumps does) are found
    in order in the mock body."""
    if not rec:
        return {"mock_saw": False, "pass": False}
    c, m = jload(cpath), jload(f"{REC}/{rec['seq']}.body")
    ci, mi = c.pop("input"), m.pop("input", None)
    if not isinstance(mi, list):
        return {"mock_saw": True, "pass": False, "mock_input": "not a list"}
    sel = mi if where == "all" else mi[:len(ci)] if where == "prefix" else mi[max(0, len(mi) - len(ci)):]
    kinds, differ = item_diff_kinds(ci, sel)
    res = {"where": where, "client_items": len(ci), "mock_items": len(mi), "items_differ": differ, "diff_kinds": kinds,
           "allowed": sorted(SDK_ALLOWED & set(kinds))}
    res["pass"] = len(sel) == len(ci) and (where != "all" or len(mi) == len(ci)) and set(kinds) <= SDK_ALLOWED
    if where != "end":
        c.pop("type", None)
        res["top_changed"] = sorted(k for k in set(c) | set(m) if c.get(k, "<absent>") != m.get(k, "<absent>"))
    del c, m, mi, sel
    a, b = meta["input_span"]
    with open(cpath, "rb") as f:
        f.seek(a); region = f.read(b - a)
    raw = open(f"{REC}/{rec['seq']}.body", "rb").read()
    if where == "end":
        ok = raw.rfind(b"," + region + b"]") >= 0
    else:
        i = raw.find(b'"input":[' + region)
        j = i + 9 + len(region)
        ok = i >= 0 and raw[j:j + 1] == (b"]" if where == "all" else b",")
    found, pos = 0, raw.find(b'"input":[')
    for it in ci:
        r = json.dumps(it, ensure_ascii=False, separators=(",", ":")).encode()
        k = raw.find(r, pos)
        if k >= 0:
            found, pos = found + 1, k + len(r)
    res.update(byte_equal=ok, items_found_bytes=found)
    return res


def sdk_http_rung(name, n, expect, retries=0):
    bp, meta = body("sdk", n)
    row = measured(name, lambda: http_retry("/v1/responses", bp, retries=retries), expect, n_bytes=n)
    row["gen"] = meta; row["client_path"] = "/v1/responses"
    recs = [r for r in row["mock"] if r["path"].endswith("/responses")]
    row["items"] = sdk_check(bp, meta, recs[-1] if recs else None, "all")
    res = row["result"]
    row["pass"] = res.get("status") == 200 and bool(res.get("completed")) and row["items"]["pass"] \
        and len(recs) == 1 and row["ticket_released"] and not row["residual_files"]
    drop_mock_bodies(row["mock"])
    row["mock"] = strip_rec(row["mock"])
    return row


def sdk_ws_rung(name, n, expect, env=None):
    """WS through the bridge on /v1/responses: turn 1 = one SDK-shaped frame of exactly N bytes, turn 2 = an
    SDK-shaped continuation (previous_response_id, item_reference + user message) that keeper expands to ~N."""
    t1, meta = body("sdk", n, ws=True)
    t2, meta2 = sdk_cont_body()
    row = measured(name, lambda: ws([t1, t2], env=env), expect, n_bytes=n)
    row["ws_env"] = env; row["gen_turn1"] = meta; row["gen_turn2"] = meta2
    recs = [r for r in row["mock"] if r["path"].endswith("/responses")]
    row["items_turn1"] = sdk_check(t1, meta, recs[0] if recs else None, "all")
    if len(recs) > 1:
        row["items_turn2_prefix"] = sdk_check(t1, meta, recs[-1], "prefix")
        row["items_turn2_own"] = sdk_check(t2, meta2, recs[-1], "end")
        row["turn2_mock_bytes"] = recs[-1]["bytes"]
    w = row["result"]
    row["pass"] = w.get("rc") == 0 and w.get("completed_turns") == 2 and len(recs) == 2 \
        and all(row.get(k, {}).get("pass") for k in ("items_turn1", "items_turn2_prefix", "items_turn2_own")) \
        and row["ticket_released"] and not row["residual_files"]
    drop_mock_bodies(row["mock"])
    row["mock"] = strip_rec(row["mock"])
    return row


def gate_sdk(name, sizes=(16 * MIB, MB245), offsets=(0, 30, 60)):
    """Rung sdk (final 7.5): SDK-shaped HTTP at 16 MiB and 245 MB, then WS 245 MB (turn 1 frame = 245,000,000 B)
    with the turn-2 continuation 1 s after turn 1. 200 / both turns completed, items equal at the mock (sdk_check).
    A retryable 503 insufficient_memory is retried by the client (2 s, up to RETRIES), as in g-seq5 / g-ws245."""
    idle = {"rung": name + "-idle", "result": {"status": "idle"}, "decay": [hlib.hop_snapshot()]}
    rows = [idle] + [sdk_http_rung(f"{name}-http-{mb(n)}", n, "200, items equal at the mock", retries=RETRIES)
                     for n in sizes]
    rows.append(sdk_ws_rung(f"{name}-ws-{mb(sizes[-1])}", sizes[-1], "both turns completed, items equal at the mock",
                            env={"WS_RETRY": str(RETRIES), "WS_RETRY_WAIT_S": "2", "WS_GAP_S": "1"}))
    rows.append(decay(name, offsets))
    return rows


def sdk_hops(name, n=MIB):
    """Diagnostic: the SDK-shaped body straight at each inner hop (cpa, aisix, sub2api, bridge). Per hop: status,
    sdk_check at the mock, and the key order of the first item of each type (client vs mock; key names only)."""
    bp, meta = body("sdk", n)
    rows = []
    for hop in ("cpa", "aisix", "sub2api", "bridge"):
        row = measured(f"{name}-{hop}", lambda: direct_post(hop, bp), "200, items equal at the mock")
        recs = [r for r in row["mock"] if r["path"].endswith("/responses")]
        rec = recs[-1] if recs else None
        row["items"] = sdk_check(bp, meta, rec, "all")
        if rec:
            ci = jload(bp)["input"]
            mi = dict(json.load(open(f"{REC}/{rec['seq']}.body", "rb"), object_pairs_hook=lambda kv: kv))["input"]
            first = {ci[i].get("type", "message"): i for i in reversed(range(min(len(ci), len(mi))))}
            row["key_order"] = {t: [list(ci[i]), [k for k, _ in mi[i]]] for t, i in first.items()}
        row["pass"] = row["result"].get("status") == 200 and row["items"]["pass"]
        drop_mock_bodies(row["mock"]); row["mock"] = strip_rec(row["mock"])
        rows.append(row)
    return rows


def mb(n):
    return f"{n // MIB}m" if n % MIB == 0 else f"{n // 1_000_000}mb"


KEEPER_MEM_FLOORS = (200 << 20) + (32 << 20)   # keeper gate_ledger.go available(): memFloor + memMargin
RELEASE_HOPS = ("keeper", "bridge", "sub2api", "aisix", "cpa")


class ReleaseWatch(threading.Thread):
    """Charge-lag sampler (7.5 follow-up, Q2), every 0.1 s: the meminfo line keeper reads (MemAvailable, the fed
    si value; the writer refreshes it every 0.2 s), keeper's ledger (ticket names, size; r_mem when the file is
    readable) and anon / memory.current of each inner hop. Rows go to $RUN/release-<name>.tsv."""

    def __init__(self, name, period=0.1):
        super().__init__(daemon=True)
        self.name, self.period, self.stop_ev, self.rows = name, period, threading.Event(), []
        self.dirs = {h: hlib.cgdir(h) for h in RELEASE_HOPS}

    def _hop(self, d):
        try:
            anon = next(int(l.split()[1]) for l in open(d + "/memory.stat") if l.startswith("anon "))
            return anon, int(open(d + "/memory.current").read())
        except (OSError, TypeError, StopIteration, ValueError):
            return None, None

    def sample(self):
        # keeper's own ledger view (GET /metrics/capacity, gate_ledger.go serveTickets); the ledger dir listing
        # if the endpoint does not answer within 0.5 s (r_mem then unknown)
        try:
            with urllib.request.urlopen(KEEPER_DIRECT + "/metrics/capacity", timeout=0.5) as x:
                m = json.loads(x.read())
            rows = m.get("tickets", m) if isinstance(m, dict) else m
            tk = [(str(r.get("id"))[:12], r.get("r_mem")) for r in rows]
        except (OSError, ValueError, AttributeError, TypeError):
            tk = [(os.path.basename(p)[:12], None) for p in sorted(glob.glob(hlib.LEDGER + "/*.ticket"))]
        row = {"t": time.time(), "fed": hlib.meminfo_avail(), "tickets": tk}
        for h, d in self.dirs.items():
            row[h] = self._hop(d) if d else (None, None)
        return row

    def run(self):
        while not self.stop_ev.is_set():
            t = time.time()
            self.rows.append(self.sample())
            self.stop_ev.wait(max(0.0, self.period - (time.time() - t)))

    def wait_clear(self, need, t_from, hold=1.0, max_s=240.0):
        """Block until keeper's memory view (fed MemAvailable - floors) has covered NEED for HOLD s, counting from
        T_FROM, or until MAX_S after T_FROM. Returns the first sample time of that covering run, or None."""
        while time.time() - t_from < max_s:
            c = self.clear_at(need, t_from, hold)
            if c is not None:
                return c
            time.sleep(0.2)
        return None

    def clear_at(self, need, t_from, hold=1.0):
        start = None
        for r in [r for r in self.rows if r["t"] >= t_from and r["fed"] is not None]:
            if r["fed"] - KEEPER_MEM_FLOORS >= need:
                start = start or r["t"]
                if r["t"] - start >= hold:
                    return start
            else:
                start = None
        return None

    def finish(self, t0, marks, need=None):
        """Stop; write the TSV (times relative to T0) and summarise around MARKS (label -> unix time)."""
        self.stop_ev.set(); self.join(5)
        mib = lambda b: None if b is None else round(b / MIB, 1)
        with open(f"{RUN}/release-{self.name}.tsv", "w") as f:
            f.write("t_rel\tfed_mib\tavail_mib\ttickets\t" + "\t".join(f"anon_{h}\tcur_{h}" for h in RELEASE_HOPS) + "\n")
            for r in self.rows:
                f.write("\t".join([f"{r['t'] - t0:.2f}", str(mib(r['fed'])), str(mib(r['fed'] and r['fed'] - KEEPER_MEM_FLOORS)),
                                   ";".join(f"{n}:{rm}" for n, rm in r["tickets"]) or "-"]
                                  + [str(mib(v)) for h in RELEASE_HOPS for v in r[h]]) + "\n")

        def at(t):
            prior = [r for r in self.rows if r["t"] <= t]
            r = prior[-1] if prior else (self.rows[0] if self.rows else None)
            if not r:
                return None
            return {"t_rel": round(r["t"] - t0, 2), "fed_mib": mib(r["fed"]), "tickets": r["tickets"],
                    **{f"anon_{h}_mib": mib(r[h][0]) for h in RELEASE_HOPS}}
        out = {"period_s": self.period, "samples": len(self.rows), "tsv": f"release-{self.name}.tsv",
               "floors_bytes": KEEPER_MEM_FLOORS, "need_bytes": need, "marks": {k: at(v) for k, v in marks.items() if v}}
        if need and marks.get("ws_end"):
            te = marks["ws_end"]
            c = self.clear_at(need, te)
            gone = next((r["t"] for r in self.rows if r["t"] >= te and not r["tickets"]), None)
            s2a = [r["sub2api"][0] for r in self.rows if r["t"] >= te and r["sub2api"][0] is not None]
            out.update({"clear_after_ws_end_s": round(c - te, 2) if c else None, "clear": at(c) if c else None,
                        "tickets_gone_after_ws_end_s": round(gone - te, 2) if gone is not None else None,
                        "sub2api_anon_max_after_ws_end_mib": mib(max(s2a)) if s2a else None,
                        "watched_after_ws_end_s": round(self.rows[-1]["t"] - te, 1) if self.rows else None})
        return out


def refusal_need(text):
    m = re.search(r'"code":"insufficient_memory","hop":"keeper","limit_bytes":(\d+)', text or "")
    return int(m.group(1)) if m else None


def ws_rung_retry(name, shape, n, expect, turn1=None):
    """ws_rung with the client 503 retry (insufficient_memory: new socket, 2 s, up to RETRIES) and the 0.1 s
    charge-lag sampler. After a refusal the sampler runs on until keeper's view covers the refused need (<= 240 s)."""
    rw = ReleaseWatch(name); rw.start()
    t0 = time.time()
    row = ws_rung(name, shape, n, expect, turn1=turn1, env={"WS_RETRY": str(RETRIES), "WS_RETRY_WAIT_S": "2"})
    w = row["result"]
    tm = w.get("timing") or []
    marks = {"t1_end": tm[0]["t0_unix"] + tm[0]["terminal"] if tm else None}
    for i, x in enumerate(tm[1:]):
        marks[f"t2_try{i}"] = x["t0_unix"]
    need = next((refusal_need(l) for l in w["lines"] if refusal_need(l)), None)
    marks["ws_end"] = marks["t1_end"]
    if need and marks["t1_end"]:
        rw.wait_clear(need, marks["t1_end"])
    row["release"] = rw.finish(t0, marks, need)
    row["retry"] = {"retries_allowed": RETRIES, "refusals": w["refusals_503"], "outcome": w["retry_outcomes"] or None}
    return row


def ws_fallback(name, n, ws_path="/backend-api/codex/responses", retry=False):
    """One WS turn whose upstream fails, then the same turn as an HTTP Responses POST (the client's fallback).
    The socket opens on the codex Responses path: firewall (enable_websocket route) and keepalive pass the
    upgrade, keeper dials its upstream, and the bridge tunnels non-/v1/responses upgrades natively to Sub2API,
    whose OpenAI API-key accounts run WS mode off (production invariant). No component is altered: the WS
    upstream failure is the chain's own. If the WS turn completes anyway, the row says so (ws_failed false)."""
    t1, meta = body("image", n, ws=True)
    hp, _ = body("image", n)

    def fn():
        rw = ReleaseWatch(name) if retry else None
        if rw:
            rw.start()
        t0 = time.time()
        w = ws([t1], path=ws_path)
        failed = w["completed_turns"] == 0
        if not failed:
            fb = {"status": "ws-completed"}
        elif retry:   # client 503 retry: 2 s, up to RETRIES (http_retry)
            fb = http_retry(ws_path, hp, retries=RETRIES)
        else:
            fb = http(ws_path, hp)
        out = {**fb, "ws_attempt": w, "ws_failed": failed}
        if rw:
            tm = w.get("timing") or []
            ws_end = tm[0]["t0_unix"] + tm[0]["terminal"] if tm else None
            tries = [x["timing"]["t0_unix"] for x in fb.get("refusals_503") or []] + [fb.get("timing", {}).get("t0_unix")]
            marks = {"ws_end": ws_end, **{f"http_try{i}": t for i, t in enumerate(tries)}}
            need = next((refusal_need(x["body_head"]) for x in fb.get("refusals_503") or []), None) \
                or refusal_need(fb.get("body_head"))
            if need and ws_end:
                rw.wait_clear(need, ws_end)
            out["release"] = rw.finish(t0, marks, need)
            out["retry"] = {"retries_allowed": RETRIES, "refusals": len(fb.get("refusals_503") or []),
                            "outcome": fb.get("retry_outcome")}
        return out
    row = measured(name, fn, "WS upgrade/turn fails upstream; the HTTP Responses fallback returns 200 completed"
                   + (" (a 503 is retried, 2 s, up to RETRIES)" if retry else ""), n_bytes=n)
    row["gen"] = meta; row["client_path"] = ws_path
    resp_recs = [r for r in row["mock"] if r["path"].endswith("/responses")]
    row["compare"] = compare(hp, resp_recs[-1] if resp_recs else None)
    drop_mock_bodies(row["mock"]); row["mock"] = strip_rec(row["mock"])
    return row


def ws_fail_idle(name, n, ws_path="/backend-api/codex/responses", need=617427872, max_s=180.0):
    """Charge-lag probe (7.5 follow-up, Q2): the fallback rung's failing WS turn alone, then no request at all; the
    0.1 s sampler runs until keeper's view (fed MemAvailable - floors) covers NEED (keeper's R_mem for the 245 MB
    HTTP fallback, from the pre-run refusal) for 1 s, or MAX_S. Shows how long the failed turn's memory stays
    out of MemAvailable when no later request triggers a release."""
    t1, meta = body("image", n, ws=True)

    def fn():
        rw = ReleaseWatch(name); rw.start()
        t0 = time.time()
        w = ws([t1], path=ws_path)
        tm = w.get("timing") or []
        ws_end = tm[0]["t0_unix"] + tm[0]["terminal"] if tm else time.time()
        rw.wait_clear(need, ws_end, max_s=max_s)
        return {"status": "ws-only", "ws_attempt": w, "release": rw.finish(t0, {"ws_end": ws_end}, need)}
    row = measured(name, fn, f"WS turn fails at the inner hop; then idle until keeper's memory view covers {need} B (<= {max_s:.0f} s)")
    row["gen"] = meta; row["client_path"] = ws_path; row["mock"] = strip_rec(row["mock"])
    return row


# ---- gate 7.5 retryable-503 rung (deterministic): ballast + client retry ------------------------------------------
ALP = "docker.io/library/alpine:3.24.2"
SELF_RETRY = re.compile(r"failover|failing over|retrying|retry attempt|switch(ing)? account|next account|attempt [2-9]", re.I)


def keeper_rmem(n):
    """keeper gate.go footprint(): R_mem for a ticketed body of n bytes (keeper 4ecf490 constants; D4)."""
    r = 0
    for flat, k_skel, k_mem, overlap in ((16 << 20, 0, False, False), (16 << 20, 0, False, False),   # keeper, bridge
                                         (0, 17, True, False), (0, 5, True, True), (0, 9, True, True)):  # s2a, aisix, cpa
        c = flat or k_skel * (8 << 20) + (16 << 20) + ((n + 9) // 10 if k_mem else 0)
        r += 2 * c if overlap else c
    return r


class Ballast:
    """$PFX-ballast: bin/ballast (ballast/main.go) in its own container and cgroup inside the parent cgroup, so its
    anon pages lower the fed MemAvailable keeper reads. Driven over stdin: hold N / release. No timer."""

    def __init__(self, limit="512m"):
        self.name = PFX + "-ballast"
        sh("podman", "rm", "-f", "-t", "0", self.name)
        self.p = subprocess.Popen(["timeout", "-k", "5", "1200", "podman", "run", "--rm", "-i", "--name", self.name,
                                   "--network", "none", f"--cgroup-parent=/{PFX}", "--memory", limit, "--memory-swap", limit,
                                   "-v", f"{T}/bin/ballast:/ballast:ro", ALP, "/ballast"],
                                  stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True, bufsize=1)
        self.log, self.dir = [], None

    def cmd(self, line):
        t = time.time()
        self.p.stdin.write(line + "\n"); self.p.stdin.flush()
        reply = self.p.stdout.readline().strip()
        if self.dir is None:
            self.dir = hlib.cgdir("ballast")
        self.log.append({"t": round(t, 3), "cmd": line, "reply": reply, "secs": round(time.time() - t, 3)})
        return reply

    def cg(self):
        """The ballast cgroup's memory.current, memory.peak (life) and memory.events."""
        if self.dir is None and self.p.poll() is None:
            self.dir = hlib.cgdir("ballast")
        if not self.dir:
            return None
        rd = lambda f: open(f"{self.dir}/{f}").read()
        try:
            return {"cgroup": self.dir, "current": int(rd("memory.current")), "peak": int(rd("memory.peak")),
                    "events": {l.split()[0]: int(l.split()[1]) for l in rd("memory.events").splitlines() if l.strip()}}
        except OSError as e:
            return {"cgroup": self.dir, "error": repr(e)}

    def close(self):
        try:
            self.p.stdin.close(); self.p.wait(timeout=30)
        except (OSError, subprocess.TimeoutExpired):
            pass
        sh("podman", "rm", "-f", "-t", "0", self.name)


def fed_median(secs=1.0, period=0.1):
    """Median of the fed MemAvailable (the line keeper reads) over secs, sampled every period."""
    v = []
    for _ in range(int(secs / period)):
        a = hlib.meminfo_avail()
        if a:
            v.append(a)
        time.sleep(period)
    v.sort()
    return v[len(v) // 2] if v else None, (v[0], v[-1]) if v else None


def self_retry_lines(since):
    """Hop log lines since `since` that look like a hop failing over or retrying on its own."""
    out = {}
    for h in HOPS:
        r = sh("podman", "logs", "--since", since, PFX + "-" + h, timeout=120)
        ls = [l[:300] for l in (r.stdout + r.stderr).splitlines() if SELF_RETRY.search(l)]
        if ls:
            out[h] = ls[-6:]
    return out


MSG_NEED = re.compile(r"needs (\d+) memory bytes; (-?\d+) are available")


def g_retry503(name, short_mib=65, window=(50, 80), gap_s=3.0, settle_s=0.6):
    """Forced retryable 503 (gate 7.5 follow-up A). seq5 turns 1 and 2 (HTTP image 256 MiB). After turn 1, a ballast
    is sized so the fed MemAvailable sits short_mib below turn 2's need (R_mem + floors). Turn 2 is sent with the
    client retry (2 s, up to RETRIES). The first named 503 triggers the ballast release (an event in this rung, no
    timer anywhere). Checks: first attempt = keeper's named 503 (limit_bytes = R_mem), a later attempt 200 and
    byte-exact at the mock, oom_kill 0 in every hop, the parent and the ballast, exactly one upstream request for
    turn 2 and no hop log line of a failover or a hop's own retry."""
    since = datetime.datetime.now().astimezone().isoformat()
    idle = {"rung": name + "-idle", "result": {"status": "idle"}, "decay": [hlib.hop_snapshot()]}
    t1 = http_rung(f"{name}-1", "image", 256 * MIB, "200 (turn 1, no ballast)")
    bp, meta = body("image", 256 * MIB)
    n = meta["bytes"]; rm = keeper_rmem(n); need = rm + KEEPER_MEM_FLOORS
    time.sleep(gap_s)
    bal, sizing, rel, size = Ballast(), [], {}, 0
    lo, hi = window
    try:
        for it in range(5):   # size the ballast against the live fed value; aim at short_mib, accept window +-10 MiB
            fed, rng = fed_median()
            short = need - fed
            sizing.append({"iter": it, "ballast_bytes": size, "fed_median_mib": fed // MIB, "fed_range_mib": [x // MIB for x in rng],
                           "short_mib": round(short / MIB, 1)})
            if lo + 5 <= short / MIB <= hi - 5:
                break
            size = max(0, size + short_mib * MIB - short)
            sizing[-1]["reply"] = bal.cmd(f"hold {size}")
            time.sleep(settle_s)
        held_cg = bal.cg()
        rw = ReleaseWatch(name); rw.start(); t0 = time.time()

        def on_refusal(res):
            if rel:
                return
            rel["t_503"] = time.time()
            rel["refusal_end"] = res["timing"]["t0_unix"] + res["secs"]
            rel["cg_before"] = bal.cg()
            rel["reply"] = bal.cmd("release")
            rel["t_released"] = time.time()

        t2 = measured(f"{name}-2", lambda: http_retry("/v1/responses", bp, retries=RETRIES, on_refusal=on_refusal),
                      "first attempt 503 insufficient_memory (keeper, limit_bytes = R_mem); ballast released on it; "
                      "a later attempt 200, byte-exact at the mock", n_bytes=n)
        res = t2["result"]
        marks = {"t2_try0": res["refusals_503"][0]["timing"]["t0_unix"] if res["refusals_503"] else None,
                 "ballast_released": rel.get("t_released"), "t2_final": res["timing"]["t0_unix"]}
        t2["release"] = rw.finish(t0, marks, rm)
    finally:
        end_cg = bal.cg()
        bal.close()
    t2["gen"] = meta; t2["client_path"] = "/v1/responses"
    recs = [r for r in t2["mock"] if r["path"].endswith("/responses")]
    t2["compare"] = compare(bp, recs[-1] if recs else None)
    drop_mock_bodies(t2["mock"]); t2["mock"] = strip_rec(t2["mock"])
    first = (res["refusals_503"] or [{}])[0]
    fb = first.get("body_head", "")
    m = MSG_NEED.search(fb)
    ev = lambda row: {h: (v.get("events") or {}).get("oom_kill") for h, v in row["mem"].items() if not h.startswith("_")}
    oom = {"t1": ev(t1), "t2": ev(t2), "ballast_held": (held_cg or {}).get("events", {}).get("oom_kill"),
           "ballast_end": (end_cg or {}).get("events", {}).get("oom_kill")}
    self_retry = self_retry_lines(since)
    t2["ballast"] = {"need_bytes": need, "r_mem": rm, "floors": KEEPER_MEM_FLOORS, "sizing": sizing, "held_cg": held_cg,
                     "release": rel, "end_cg": end_cg, "log": bal.log}
    t2["assert"] = a = {
        "first_named_503": first.get("status") == 503 and f'"code":"insufficient_memory","hop":"keeper","limit_bytes":{rm}' in fb,
        "first_limit_bytes": refusal_need(fb), "r_mem_expected": rm,
        "decision_short_mib": round((int(m.group(1)) - int(m.group(2))) / MIB, 1) if m else None,
        "decision_memavail_mib": round((int(m.group(2)) + KEEPER_MEM_FLOORS) / MIB, 1) if m else None,
        "refusals": len(res["refusals_503"]), "attempts": len(res["refusals_503"]) + 1,
        "final_status": res.get("status"), "final_completed": res.get("completed"),
        # items byte-exact: every input item equal and media parts equal (the body sha differs on this chain because
        # Sub2API adds top-level include / parallel_tool_calls / prompt_cache_key / tools; see compare.top_changed)
        "final_items_exact": (t2["compare"].get("input") or {}).get("items_differ") == 0
                              and t2["compare"]["input"]["client_items"] == t2["compare"]["input"]["mock_items"]
                              and t2["compare"].get("media_parts_equal") is not False,
        "final_sha_equal": t2["compare"].get("sha_equal"),
        "release_after_503_s": round(rel["t_released"] - rel["refusal_end"], 3) if rel else None,
        "oom_kill": oom, "oom_zero": all(v in (0, None) for d in (oom["t1"], oom["t2"]) for v in d.values())
                                    and oom["ballast_held"] == 0 and oom["ballast_end"] in (0, None),
        "upstream_requests_turn2": len(recs), "self_retry_log_lines": self_retry}
    a["short_in_window"] = a["decision_short_mib"] is not None and lo <= a["decision_short_mib"] <= hi
    a["no_hop_failover_or_retry"] = len(recs) == 1 and not self_retry
    a["pass"] = bool(a["first_named_503"] and a["refusals"] >= 1 and a["final_status"] == 200 and a["final_items_exact"]
                     and a["oom_zero"] and a["no_hop_failover_or_retry"])
    return [idle, t1, t2]


def conc16(name, shape, n_big=3, n_tiny=3, tiny_n=64 * KIB):
    """Spec 7.5 concurrent inline passthrough at Sub2API (k_skel 6 admits about 2-3 bodies of 16 MiB in 320m), with
    the prompt guard in blocking mode (stack GUARD=blocking): n_big concurrent 16 MiB bodies of SHAPE (text | utext)
    through the client entry, then n_tiny tiny text requests 0.5 s later, all in flight together. Each must get 200
    or a named 503; Sub2API memory.events max and oom_kill must stay 0 (row mem.sub2api.events_delta)."""
    bp, meta = body(shape, 16 * MIB)
    tp, _ = body("text", tiny_n)
    g0 = sh("podman", "logs", PFX + "-guard").stdout.count("guard_call")

    def fn():
        out = [None] * (n_big + n_tiny)

        def one(i, path):
            try:
                out[i] = http("/v1/responses", path)
            except Exception as e:
                out[i] = {"status": 0, "error": repr(e)}
        th = [threading.Thread(target=one, args=(i, bp)) for i in range(n_big)]
        for t in th:
            t.start()
        time.sleep(0.5)
        th2 = [threading.Thread(target=one, args=(n_big + i, tp)) for i in range(n_tiny)]
        for t in th2:
            t.start()
        for t in th + th2:
            t.join()
        for i, r in enumerate(out):
            m = re.search(r'"code"\s*:\s*"([^"]+)"', r.get("body_head", "") or "")
            r["kind"] = "big" if i < n_big else "tiny"
            r["named"] = m.group(1) if m else None
            r.pop("body_head", None) if r.get("status") == 200 else None
        return {"status": "/".join(str(r["status"]) for r in out), "requests": out,
                "all_200_or_named_503": all(r["status"] == 200 or (r["status"] == 503 and r["named"]) for r in out)}
    row = measured(name, fn, f"{n_big} x 16 MiB {shape} + {n_tiny} tiny concurrent: each 200 or a named 503; "
                   "Sub2API memory.events max 0, oom_kill 0")
    row["gen"] = meta
    big_sha = hashlib.sha256(open(bp, "rb").read()).hexdigest()
    tiny_sha = hashlib.sha256(open(tp, "rb").read()).hexdigest()
    recs = [r for r in row["mock"] if r["path"].endswith("/responses")]
    row["compare"] = {"mock_big_sha_equal": sum(r["sha256"] == big_sha for r in recs),
                      "mock_tiny_sha_equal": sum(r["sha256"] == tiny_sha for r in recs), "mock_records": len(recs),
                      "big_first": compare(bp, next((r for r in recs if r["bytes"] > 8 * MIB), None))}
    row["guard_calls"] = sh("podman", "logs", PFX + "-guard").stdout.count("guard_call") - g0
    drop_mock_bodies(row["mock"]); row["mock"] = strip_rec(row["mock"])
    return row


GZ = {"Content-Encoding": "gzip"}


def gzip_body(n):
    import gzip, shutil
    bp, meta = body("text", n)
    gz = bp + ".gz"
    if not os.path.exists(gz):
        with open(bp, "rb") as f, gzip.open(gz + ".tmp", "wb", compresslevel=6) as g:
            shutil.copyfileobj(f, g)
        os.rename(gz + ".tmp", gz)
    return gz, meta


def gzip_check(name, sizes=(8 * MIB, 17 * MIB)):
    """Content-Encoding: gzip on the covered path /v1/responses, at the client entry, at keeper's own port and
    straight at the bridge. Decoded bytes are charged: decoded 8 MiB (below every hop's fallback L; Sub2API's L
    is 11.19 MiB) must return 200; decoded 17 MiB (above L = 16 MiB) a named 413 request_too_large."""
    rows = []
    for n in sizes:
        gz, meta = gzip_body(n)
        exp = "200" if n < 11 * MIB else "named 413 request_too_large (hop, limit_bytes = L, decoded bytes)"
        entries = (("entry", lambda gz=gz: http("/v1/responses", gz, headers=GZ)),
                   ("keeper", lambda gz=gz: http("/v1/responses", gz, base=KEEPER_DIRECT, headers=GZ)),
                   ("bridge", lambda gz=gz: direct_post("bridge", gz, headers=("Content-Encoding: gzip",))))
        for entry, fn in entries:
            if entry == "entry" and KEEPER == KEEPER_DIRECT:
                continue
            # spec 7.5 (frozen 2026-11-05): keeper refuses a gzip body on a covered path with its existing 400;
            # behind keeper (straight at the bridge) Sub2API charges decoded bytes (200 below L, named 413 above)
            r = measured(f"{name}-{entry}-{n // MIB}m", fn, exp if entry == "bridge" else "400 (keeper's existing refusal)")
            r.update(decoded_bytes=n, encoded_bytes=os.path.getsize(gz), entry=entry, gen=meta)
            drop_mock_bodies(r["mock"]); r["mock"] = strip_rec(r["mock"])
            rows.append(r)
    return rows

# ---- final 7.5 item 4: per-class limits through the full chain (Sub2API f3db26d98a kConvClasses) -----------------
# Sub2API memory.max 320m (stack.sh mem_of): room = 320 MiB - ChargeMargin 32 MiB - IdleBaseline 48 MiB - 32 KiB read
# buffer; responses S_conv = floor((room / 114 - 24 KiB) / 4 KiB) x 4 KiB = 2179072 B (about 2.08 MiB, under the
# 16 MiB inline limit). /chat/completions is class chat (k 260, charged from the first byte, L_conv about 0.9 MiB).
S_CONV_RESPONSES = (lambda room: (int(room / 114) - 24 * KIB) // (4 * KIB) * (4 * KIB))(320 * MIB - 32 * MIB - 48 * MIB - 32 * KIB)


def lim_chat(name, n=512 * KIB):
    """0.5 MiB /v1/chat/completions on the verified (force_responses) account: 200."""
    bp, meta = body("text", n, api="chat")
    row = measured(name, lambda: http("/v1/chat/completions", bp), "200 (class chat, k_conv 260)", n_bytes=n)
    row["gen"] = meta; row["client_path"] = "/v1/chat/completions"
    drop_mock_bodies(row["mock"]); row["mock"] = strip_rec(row["mock"])
    res = row["result"]
    row["pass"] = res.get("status") == 200 and bool(row["mock"]) and row["ticket_released"] and not row["residual_files"]
    return row


def offlist_prompt_body(n):
    """A pi text body of n bytes with a top-level `prompt` template member (off the verified-raw list)."""
    p = f"{T}/bodies/offlist-prompt-{n}.json"
    if not os.path.exists(p + ".meta"):
        ins = '"prompt":{"id":"pmpt_gate75final","version":"1"},'
        src, meta = body("text", n - len(ins))
        raw = open(src, "rb").read()
        assert raw[:1] == b"{"
        open(p, "wb").write(b"{" + ins.encode() + raw[1:])
        json.loads(open(p, "rb").read())
        open(p + ".meta", "w").write(json.dumps({"out": p, "shape": "text+prompt", "bytes": os.path.getsize(p), "base": meta}))
    return p, json.loads(open(p + ".meta").read())


def lim_offlist(name, n=3 * MIB):
    """Off-list Responses shape (`prompt` template) above S_conv: a named 413 from hop sub2api, limit_bytes =
    responses S_conv, and one large_request.off_list line."""
    bp, meta = offlist_prompt_body(n)
    row = measured(name, lambda: http("/v1/responses", bp), f"413 hop sub2api limit_bytes {S_CONV_RESPONSES}; 1 off_list line", n_bytes=n)
    row["gen"] = meta; row["client_path"] = "/v1/responses"
    drop_mock_bodies(row["mock"]); row["mock"] = strip_rec(row["mock"])
    res = row["result"]; head = res.get("body_head", "")
    m = re.search(r'"limit_bytes":(\d+)', head)
    row["refusal"] = {"hop_sub2api": '"hop":"sub2api"' in head, "limit_bytes": int(m.group(1)) if m else None,
                      "expected_s_conv": S_CONV_RESPONSES}
    row["pass"] = (res.get("status") == 413 and row["refusal"]["hop_sub2api"] and row["refusal"]["limit_bytes"] == S_CONV_RESPONSES
                   and row["s2a_off_list"] == 1 and not row["mock"] and row["ticket_released"] and not row["residual_files"])
    return row


# ---- delta 7.5 (Sub2API 569aeca2c4): embeddings / rerank classes and a Responses web_search subpath, host-port style
# (straight at Sub2API's published port, as Hindsight calls it). Group emb runs with EMB=1: AISIX routes the two
# models to bin/mock-emb (records in $RUN/erec). The web_search subpath is not the bare Responses path, so it is not
# covered; its matched route /v1/responses/*subpath is not in kConvRouteClasses, so it is class unmeasured (k 260),
# charged from the first byte: L_conv = floor((room / (260 + 3.5) - 24 KiB) / 4 KiB) x 4 KiB, room = 320 MiB - 32 MiB
# margin - 48 MiB idle - 512 KiB guard fixed - 9.6 MiB decoder state = 888832 B. Class web_search (k 65) would give
# 3493888 B, so a 2 MiB body is refused at 888832 only when the charge class is unmeasured, never web_search.
EREC = RUN + "/erec"
_ROOM_FB = 320 * MIB - 32 * MIB - 48 * MIB - 512 * KIB - (8 * MIB + 1536 * KIB + 64 * KIB + 8 * KIB)
L_CONV = {k: (int(_ROOM_FB / (k + 3.5)) - 24 * KIB) // (4 * KIB) * (4 * KIB) for k in (260, 65)}


def erec_lines():
    try:
        return [json.loads(l) for l in open(EREC + "/requests.jsonl")]
    except FileNotFoundError:
        return []


def emb_body(kind, n):
    """embeddings: an array of strings; rerank: one query and many documents. n bytes, plain JSON."""
    p = f"{T}/bodies/delta-{kind}-{n}.json"
    if not os.path.exists(p):
        words = "the quick brown fox jumps over the lazy dog memory gateway capacity ".split()
        items, size, i = [], 0, 0
        while size < n - 300:
            t = " ".join(words[(i + j) % len(words)] for j in range(60)) + f" {i}"
            items.append(t); size += len(t) + 4; i += 1
        if kind == "emb":
            doc = {"model": "text-embedding-v4", "input": items, "encoding_format": "float"}
        else:
            doc = {"model": "gte-rerank-v2", "query": "which text mentions the gateway", "documents": items, "top_n": 3}
        raw = json.dumps(doc, separators=(",", ":"))
        pad = n - len(raw.encode())
        assert pad >= 0
        items[-1] += "x" * pad
        raw = json.dumps(doc, separators=(",", ":"))
        assert len(raw.encode()) == n, (len(raw.encode()), n)
        open(p, "w").write(raw)
    d = json.loads(open(p).read())
    return p, {"bytes": os.path.getsize(p), "items": len(d.get("input") or d.get("documents"))}


def emb_rung(name, kind, path, n):
    bp, meta = emb_body(kind, n)
    e0 = len(erec_lines())
    row = measured(name, lambda: http(path, bp, base=S2A), "200 (host port, class " + kind + ")", n_bytes=n)
    er = erec_lines()[e0:]
    row["gen"] = meta; row["client_path"] = path; row["entry"] = S2A
    row["emock"] = [{k: x.get(k) for k in ("path", "bytes", "sha256", "read_err")} for x in er]
    drop_mock_bodies(row["mock"]); row["mock"] = strip_rec(row["mock"])
    for x in er:
        try:
            os.remove(f"{EREC}/{x['seq']}.body")
        except OSError:
            pass
    res = row["result"]
    row["pass"] = (res.get("status") == 200 and len(er) == 1 and er[0]["path"].endswith("/rerank" if kind == "rerank" else "/embeddings")
                   and row["ticket_released"] and not row["residual_files"] and row["s2a_off_list"] == 0)
    row["upstream_bytes"] = er[0]["bytes"] if er else None
    return row


def wsub_rung(name, n, path="/v1/responses/x/web_search"):
    """Responses web_search subpath at the host port. 64 KiB: status recorded (forwarded or not). 2 MiB: a named
    413 from hop sub2api with limit_bytes = L_conv(k 260, unmeasured), which a web_search charge (k 65) would admit."""
    bp, meta = body("text", n)
    exp = "status recorded" if n < MIB else f"413 hop sub2api limit_bytes {L_CONV[260]} (unmeasured; web_search would be {L_CONV[65]})"
    e0 = len(erec_lines())
    row = measured(name, lambda: http(path, bp, base=S2A), exp, n_bytes=n)
    row["gen"] = meta; row["client_path"] = path; row["entry"] = S2A
    row["emock_n"] = len(erec_lines()) - e0
    drop_mock_bodies(row["mock"]); row["mock_paths"] = [m.get("path") for m in row["mock"]]; row["mock"] = strip_rec(row["mock"])
    res = row["result"]; head = res.get("body_head", "")
    m = re.search(r'"limit_bytes":(\d+)', head)
    lim = int(m.group(1)) if m else None
    row["refusal"] = {"hop_sub2api": '"hop":"sub2api"' in head, "limit_bytes": lim, "l_conv_unmeasured": L_CONV[260],
                      "l_conv_web_search": L_CONV[65]}
    row["charge_class"] = {L_CONV[260]: "unmeasured", L_CONV[65]: "web_search"}.get(lim, "admitted (not refused by size)" if res.get("status") != 413 else f"other ({lim})")
    if n < MIB:
        row["pass"] = (res.get("status") is not None and res.get("status") not in (413, 503, 507) and not row["refusal"]["hop_sub2api"]
                       and row["ticket_released"] and not row["residual_files"])
    else:
        row["pass"] = (res.get("status") == 413 and row["refusal"]["hop_sub2api"] and lim == L_CONV[260] and not row["mock"]
                       and row["ticket_released"] and not row["residual_files"])
    return row


# ---- delta 7.5 group sizeretry (supervisor decision): the size sequence with the client 503 retry on every rung.
# Retry 503 insufficient_memory only, 2 s apart, up to GATE_RETRIES (4: at most 5 attempts). Keeper sets Retry-After
# only on capacity_busy, and a WS refusal is an in-band frame without headers, so no Retry-After is ever present.
# Per attempt: status, keeper need (limit_bytes) and available bytes, the gap since the previous attempt ended, and
# the time since the previous rung ended (measured() return).
_AVAIL = re.compile(r'needs (\d+) memory bytes; (\d+) are available')


def _attempt(text, t0, prev_end, rung_prev):
    a = {"status": 503 if refusal_need(text) else None, "need": refusal_need(text), "t0_unix": t0,
         "gap_since_prev_attempt_s": round(t0 - prev_end, 2) if t0 and prev_end else None,
         "since_prev_rung_end_s": round(t0 - rung_prev["t"], 2) if t0 and rung_prev.get("t") else None,
         "prev_rung": rung_prev.get("rung")}
    m = _AVAIL.search(text or "")
    if m:
        a["available"] = int(m.group(2)); a["short_mib"] = round((int(m.group(1)) - int(m.group(2))) / MIB, 1)
    return a


def sr_http(name, n):
    prev = dict(LAST_END)
    row = http_rung(name, "image", n, "200; a 503 insufficient_memory is retried (2 s, up to RETRIES)", retries=RETRIES)
    res = row["result"]; att, last = [], None
    for x in res.get("refusals_503") or []:
        tm = x["timing"]; a = _attempt(x["body_head"], tm["t0_unix"], last, prev); a["status"] = x["status"]
        att.append(a); last = tm["t0_unix"] + tm["terminal"]
    tm = res.get("timing") or {}
    a = _attempt(res.get("body_head", ""), tm.get("t0_unix"), last, prev); a["status"] = res.get("status")
    row["attempts"] = att + [a]
    row["retry"] = {"retries_allowed": RETRIES, "refusals": len(att), "outcome": res.get("retry_outcome")}
    return row


def sr_ws(name, n, turn1=None):
    prev = dict(LAST_END)
    row = ws_rung_retry(name, "image", n, "2 turns completed; a 503 insufficient_memory on either turn is retried "
                        "(new socket, 2 s, up to RETRIES)", turn1=turn1)
    w = row["result"]; tm = w.get("timing") or []
    sent = [l for l in w["lines"] if re.match(r"turn=\d+ frame=", l)]
    att, last = [], None
    for i, l in enumerate(sent):
        t = tm[i] if i < len(tm) else {}
        a = _attempt(l, t.get("t0_unix"), last, prev)
        a["turn"] = int(re.match(r"turn=(\d+)", l).group(1))
        a["status"] = a["status"] or ("completed" if "response.completed" in l else "other")
        att.append(a)
        if t.get("t0_unix") is not None and t.get("terminal") is not None:
            last = t["t0_unix"] + t["terminal"]
    row["attempts"] = att
    row["attempts_timing_aligned"] = len(sent) == len(tm)
    return row

RUNGS = {
    "sr-http-image-245m": lambda: [sr_http("sr-http-image-245m", MB245)],
    "sr-http-image-256m": lambda: [sr_http("sr-http-image-256m", 256 * MIB)],
    "sr-ws-image-245m": lambda: [sr_ws("sr-ws-image-245m", MB245, turn1=MB245)],
    "sr-ws-image-256m": lambda: [sr_ws("sr-ws-image-256m", 256 * MIB)],
    "emb-64k": lambda: [emb_rung("emb-64k", "emb", "/v1/embeddings", 64 * KIB)],
    "rerank-256k": lambda: [emb_rung("rerank-256k", "rerank", "/v1/rerank", 256 * KIB)],
    "wsub-64k": lambda: [wsub_rung("wsub-64k", 64 * KIB)],
    "wsub-2m": lambda: [wsub_rung("wsub-2m", 2 * MIB)],
    "lim-chat-512k": lambda: [lim_chat("lim-chat-512k")],
    "lim-offlist-prompt-3m": lambda: [lim_offlist("lim-offlist-prompt-3m")],
    "http-image-1m": lambda: [http_rung("http-image-1m", "image", MIB, "200")],
    "http-image-64m": lambda: [http_rung("http-image-64m", "image", 64 * MIB, "200 with ticket")],
    "http-image-245m": lambda: [http_rung("http-image-245m", "image", MB245, "200 with ticket")],
    "http-image-256m": lambda: [http_rung("http-image-256m", "image", 256 * MIB, "200 with ticket")],
    "ws-image-1m": lambda: [ws_rung("ws-image-1m", "image", MIB, "2 turns completed")],
    "ws-image-64m": lambda: [ws_rung("ws-image-64m", "image", 64 * MIB, "both turns ticketed (turn1 frame = 64 MiB exactly)", turn1=64 * MIB)],
    "ws-image-245m": lambda: [ws_rung("ws-image-245m", "image", MB245, "both turns ticketed (turn1 frame = 245,000,000 B exactly)", turn1=MB245)],
    "ws-image-256m": lambda: [ws_rung("ws-image-256m", "image", 256 * MIB, "2 turns completed, both ticketed (turn 2 expands to ~256 MiB)")],
    "codex-image-1m": lambda: [http_rung("codex-image-1m", "image", MIB, "200", path="/backend-api/codex/responses")],
    # pre-run 2: Codex WS on the codex path, converted to POST by the bridge (3a9c76b), no 64 MiB WS-ingress cap
    "codex-ws-245m": lambda: [ws_rung("codex-ws-245m", "image", MB245, "2 turns completed on /backend-api/codex/responses (turn1 frame = 245,000,000 B; bridge WS->POST, no 64 MiB 413)", turn1=MB245, path="/backend-api/codex/responses")],
    "codex-image-64m": lambda: [http_rung("codex-image-64m", "image", 64 * MIB, "ticket admitted, spooled end to end, 200", path="/backend-api/codex/responses")],
    "http-text-16m-settled": lambda: [settle(30) or http_rung("http-text-16m-settled", "text", 16 * MIB, "200 (settled: healthy + 30 s)")],
    "ws-text-16m-settled": lambda: [settle(30) or ws_rung("ws-text-16m-settled", "text", 16 * MIB, "2 turns completed (settled: 30 s idle)")],
    "http-text-16m-fresh-s2a": lambda: fresh_s2a_text16("http-text-16m-fresh-s2a"),
    "http-text-32m": lambda: [http_rung("http-text-32m", "text", 32 * MIB, "413 request_too_large naming the 16 MiB inline limit")],
    "ws-text-32m": lambda: [ws_rung("ws-text-32m", "text", 32 * MIB, "error event 413 request_too_large naming 16 MiB")],
    "seq5-256m": lambda: seq5("seq5-256m"),
    "conc-256m-100tiny": lambda: concurrency("conc-256m-100tiny"),
    "a4-chat17": lambda: chat17("a4-chat17"),
    "a5-provider413": lambda: provider413("a5-provider413"),
    "a5-inner503": lambda: inner503("a5-inner503"),
    "a5-inner507-40": lambda: inner507("a5-inner507", 40),
    "a5-inner507-8": lambda: inner507("a5-inner507", 8),
    "seq5-wait-256m": lambda: seq5_wait("seq5-wait-256m"),
    "p413-size-64m": lambda: provider413("p413", modes=("413",), sizes=(64 * MIB,)),
    "p413-ctx-64m": lambda: provider413("p413", modes=("413ctx",), sizes=(64 * MIB,)),
    "inner503-cpa": lambda: inner503("a5-inner503", only="cpa"),
    "settle30": lambda: [settle(30) or {"rung": "settle30", "note": "30 s idle"}],
    "diag-seq5-256m": lambda: diag_seq5("diag-seq5-256m"),
    "diag-ws245": lambda: diag_ws245("diag-ws245"),
    "tsi-seq5": lambda: diag_seq5_idle60("tsi-seq5"),
    "tsi-ws245": lambda: diag_ws245_d("tsi-ws245"),
    # M2 gate 7.5
    "http-text-16m": lambda: [http_rung("http-text-16m", "text", 16 * MIB, "200")],
    "ws-text-16m": lambda: [ws_rung("ws-text-16m", "text", 16 * MIB, "2 turns completed")],
    "g-seq5": lambda: gate_seq5("g-seq5"),
    "g-ws245": lambda: gate_ws245("g-ws245"),
    "ws-fallback-1m": lambda: [ws_fallback("ws-fallback-1m", MIB)],
    "ws-fallback-245m": lambda: [ws_fallback("ws-fallback-245m", MB245)],
    # 7.5 follow-up: the two rungs with the client 503 retry and the 0.1 s charge-lag sampler
    "ws-image-256m-retry": lambda: [ws_rung_retry("ws-image-256m-retry", "image", 256 * MIB,
                                                  "2 turns completed; a turn-2 503 insufficient_memory is retried (2 s, up to RETRIES)")],
    "ws-fallback-245m-retry": lambda: [ws_fallback("ws-fallback-245m-retry", MB245, retry=True)],
    "ws-fail-idle-245m": lambda: [ws_fail_idle("ws-fail-idle-245m", MB245)],
    # 7.5 follow-up A: forced retryable 503 (ballast released on the first 503, client retry 2 s x RETRIES)
    "g-retry503": lambda: g_retry503("g-retry503"),
    # CI ladder (task 8.2 skeleton, ci/ladder.sh): structure-heavy corpora and the nightly 1 GiB rungs (CI-only cap)
    "http-small-16m": lambda: [http_rung("http-small-16m", "small", 16 * MIB, "200, items equal at the mock")],
    "http-tiny-16m": lambda: [http_rung("http-tiny-16m", "tiny", 16 * MIB, "200, items equal at the mock")],
    "http-image-1g": lambda: [http_rung("http-image-1g", "image", 1 << 30, "200 under the CI-only 1 GiB cap")],
    "ws-image-1g": lambda: [ws_rung("ws-image-1g", "image", 1 << 30, "2 turns completed under the CI-only 1 GiB cap")],
    "gzip-check": lambda: gzip_check("gzip"),
    "gzip-check-1m": lambda: gzip_check("gzip", sizes=(MIB,)),
    "conc16-text": lambda: [conc16("conc16-text", "text")],
    "conc16-utext": lambda: [conc16("conc16-utext", "utext")],
    # rung sdk (final 7.5): OpenAI Python SDK-shaped bodies; sdk-*-1m / -16m are the mechanical check
    "g-sdk": lambda: gate_sdk("g-sdk"),
    "sdk-http-1m": lambda: [sdk_http_rung("sdk-http-1m", MIB, "200, items equal at the mock")],
    "sdk-http-16m": lambda: [sdk_http_rung("sdk-http-16m", 16 * MIB, "200, items equal at the mock")],
    "g-sdk-small": lambda: gate_sdk("g-sdk-small", sizes=(MIB, 16 * MIB)),   # the g-sdk code path at 1 / 16 MiB
    "sdk-ws-1m": lambda: [sdk_ws_rung("sdk-ws-1m", MIB, "both turns completed, items equal at the mock")],
    "sdk-ws-16m": lambda: [sdk_ws_rung("sdk-ws-16m", 16 * MIB, "both turns completed, items equal at the mock")],
    "sdk-hops-1m": lambda: sdk_hops("sdk-hops-1m"),
}

if __name__ == "__main__":
    with open(RUN + "/results.jsonl", "a") as out:
        for name in sys.argv[1:]:
            print(f"== {name} load={load()}", flush=True)
            try:
                rows = RUNGS[name]()
            except Exception as e:
                rows = [{"rung": name, "harness_error": repr(e)}]
            for r in rows:
                if "result" not in r and "harness_error" not in r:
                    continue
                out.write(json.dumps(r) + "\n"); out.flush()
                res = r.get("result", {})
                print(f"   {r['rung']}: status={res.get('status', res.get('rc'))} secs={res.get('secs')} "
                      f"timing={res.get('timing') if isinstance(res.get('timing'), dict) else [ (t.get('terminal'), t.get('longest_gap')) for t in res.get('timing') or []]} "
                      f"load={r.get('load_before')} tickets={r.get('leases', {}).get('tickets_seen')} "
                      f"fds={r.get('leases', {}).get('hops_with_ticket_fd')} released={r.get('ticket_released')} "
                      f"sha={r.get('compare', {}).get('sha_equal')} pass={r.get('pass')} offlist={r.get('s2a_off_list')} err={r.get('harness_error', '')}", flush=True)
