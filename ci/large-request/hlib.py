"""Harness helpers: cgroup memory per container, ledger/lease sampling, mock records. Local chain only."""
import glob, json, os, subprocess, threading, time

# harness root: LRC_HOME, else this file's directory (/var/tmp/lrc-chain-20261105 here; a checkout in CI)
T = os.environ.get("LRC_HOME") or os.path.dirname(os.path.abspath(__file__))
PFX = os.environ.get("PFX", "lrcchain2")
FS = os.environ.get("FS", "/run/media/xz/" + PFX)
HOPS = ["keeper", "bridge", "sub2api", "aisix", "cpa"]
# full-chain containers (stack.sh FULLCHAIN / FW / KA / SQUID): fw fwrelay keepalive squid cpasq s2asq
EXTRA = os.environ.get("EXTRA", "").split()
LEDGER = FS + "/ledger"


def sh(*a, timeout=180):
    a = list(a)
    if a and a[0] == "podman":   # podman always under timeout -k 5 (a hung podman call is killed, never left behind)
        a = ["timeout", "-k", "5", str(timeout)] + a
        return subprocess.run(a, capture_output=True, text=True, timeout=timeout + 10)
    return subprocess.run(a, capture_output=True, text=True, timeout=timeout)


def load():
    return float(open("/proc/loadavg").read().split()[0])


PARENT_CG = "/sys/fs/cgroup/" + PFX
MEMINFO = FS + "/meminfo/meminfo"


def meminfo_avail():
    try:
        return int(open(MEMINFO).read().split()[1]) * 1024
    except (OSError, IndexError, ValueError):
        return None


def meminfo_all():
    """Every line of the meminfo source in MiB: MemAvailable (what keeper reads) plus the comparison estimators."""
    try:
        return {l.split(":")[0]: int(l.split()[1]) // 1024 for l in open(MEMINFO) if l.strip()}
    except (OSError, IndexError, ValueError):
        return {}


def cgdir(hop):
    if hop == "parent":
        return PARENT_CG if os.path.exists(PARENT_CG + "/memory.current") else None
    p = sh("podman", "inspect", "-f", "{{.State.CgroupPath}}", PFX + "-" + hop).stdout.strip()
    d = "/sys/fs/cgroup" + p
    return d if os.path.exists(d + "/memory.current") else None


def vmstat_swap():
    """Host-wide pswpin / pswpout (pages) from /proc/vmstat."""
    kv = dict(l.split() for l in open("/proc/vmstat"))
    return {k: int(kv[k]) for k in ("pswpin", "pswpout")}


def psi_full(d):
    """memory.pressure 'full' line: (avg10, total_us)."""
    try:
        for l in open(d + "/memory.pressure"):
            if l.startswith("full"):
                f = dict(x.split("=") for x in l.split()[1:])
                return float(f["avg10"]), int(f["total"])
    except (OSError, KeyError, ValueError):
        pass
    return None, None


class Mem:
    """Per-rung memory.peak via a per-fd reset (kernel >= 6.12) plus memory.current sampling. Also (M2 gate 7.5):
    swap-in/out deltas (host /proc/vmstat and each cgroup's memory.stat pswpin/pswpout), PSI memory full avg10
    peak and full total delta per cgroup."""
    UNITS = HOPS + EXTRA + ["parent"]

    def __init__(self):
        self.dirs = {h: cgdir(h) for h in self.UNITS}

    def start(self):
        self.fds, self.cur_max, self.stat_max = {}, {h: 0 for h in self.UNITS}, {}
        self.stat_peak = {}  # delta 7.5: the whole memory.stat sample at the first highest memory.current
        self.stat0 = {h: self.stat(h) for h in self.UNITS if self.dirs[h]}
        self.mi0 = meminfo_all()
        self.avail0 = self.avail_min = meminfo_avail()
        for h, d in self.dirs.items():
            if d:
                try:
                    f = open(d + "/memory.peak", "r+"); f.write("reset\n"); f.flush(); self.fds[h] = f
                except OSError:
                    pass
        self.ev0 = {h: self.events(h) for h in self.UNITS}
        self.t0, self.vm0 = time.time(), vmstat_swap()
        self.psi0 = {h: psi_full(d) for h, d in self.dirs.items() if d}
        self.psi_max = {h: 0.0 for h in self.psi0}

    STAT = ("anon", "file", "file_dirty", "file_writeback", "kernel", "inactive_file", "shmem", "pswpin", "pswpout")

    def stat(self, h):
        d = self.dirs[h]
        kv = dict(l.split() for l in open(d + "/memory.stat"))
        st = {k: int(kv.get(k, 0)) for k in self.STAT}
        st["keeper_criterion"] = st["anon"] + st["file_dirty"] + st["file_writeback"] + st["kernel"]
        st["current"] = int(open(d + "/memory.current").read())
        st["working_set"] = st["current"] - st["inactive_file"]
        return st

    def sample(self):
        a = meminfo_avail()
        if a is not None and (self.avail_min is None or a < self.avail_min):
            self.avail_min = a
        snap = {"t": round(time.time(), 2), "mi": meminfo_all()}  # time series: WS turn 2 starts mid-rung
        self.series = getattr(self, "series", []); self.series.append(snap)
        for h, d in self.dirs.items():
            if d:
                try:
                    st = self.stat(h)
                except OSError:
                    continue
                snap[h] = [st["anon"] >> 20, st["working_set"] >> 20]
                a10, _ = psi_full(d)
                if a10 is not None:
                    self.psi_max[h] = max(self.psi_max.get(h, 0.0), a10)
                if st["current"] > self.cur_max[h]:
                    self.stat_peak[h] = dict(st, t=snap["t"])
                self.cur_max[h] = max(self.cur_max[h], st["current"])
                m = self.stat_max.setdefault(h, {})
                for k, v in st.items():
                    m[k] = max(m.get(k, 0), v)

    def events(self, h):
        d = self.dirs[h]
        if not d:
            return {}
        txt = open(d + "/memory.events").read()
        return {l.split()[0]: int(l.split()[1]) for l in txt.splitlines() if l.strip()}

    def stop(self):
        secs = max(time.time() - self.t0, 1e-3)
        vm = vmstat_swap()
        swap = {"secs": round(secs, 2), "host": {k: vm[k] - self.vm0[k] for k in vm}}
        swap["host_pswpin_per_s"] = round(swap["host"]["pswpin"] / secs, 2)
        out = {"_meminfo": {"avail_start": self.avail0, "avail_min": self.avail_min, "start_mib": self.mi0},
               "_series": getattr(self, "series", []), "_swap": swap, "_psi": {}}
        for h, (a0, t0) in self.psi0.items():
            a1, t1 = psi_full(self.dirs[h])
            out["_psi"][h] = {"full_avg10_max": max(self.psi_max.get(h, 0.0), a1 or 0.0),
                              "full_total_us_delta": (t1 - t0) if t0 is not None and t1 is not None else None}
        for h in self.UNITS:
            peak = None
            if h in self.fds:
                f = self.fds[h]; f.seek(0); peak = int(f.read().strip()); f.close()
            life = None
            if self.dirs[h]:
                try:
                    life = int(open(self.dirs[h] + "/memory.peak").read())
                except OSError:
                    pass
            ev = self.events(h)
            if self.dirs[h] and h in self.stat0:
                try:
                    s1 = self.stat(h)
                    swap[h] = {k: s1[k] - self.stat0[h][k] for k in ("pswpin", "pswpout")}
                except OSError:
                    pass
            out[h] = {"peak_rung": peak, "peak_life": life, "cur_max": self.cur_max[h],
                      "stat_start": self.stat0.get(h), "stat_max": self.stat_max.get(h), "stat_at_peak": self.stat_peak.get(h),
                      "events": ev, "events_delta": {k: ev.get(k, 0) - self.ev0.get(h, {}).get(k, 0) for k in ev}}
        return out


def ticket_inodes():
    out = {}
    for p in glob.glob(LEDGER + "/*.ticket"):
        try:
            out[os.path.basename(p)] = os.stat(p).st_ino
        except OSError:
            pass
    return out


class DecisionWatch(threading.Thread):
    """Keeper's decision-time MemAvailable, DERIVED (keeper 4ecf490 logs the value only in a refusal message).
    Every 10 ms: the meminfo source keeper reads (its value changes are kept, host time) and the ledger's
    tickets with their mtime. Keeper reads meminfo, then creates the ticket file under ledger.lock
    (gate_ticket.go admit), and rewrites it in place on a raise (resize), so a ticket's mtime marks a decision;
    the value in effect then is the last observed meminfo value at or before that mtime (writer period 0.2 s,
    observation lag <= 10 ms)."""

    def __init__(self):
        super().__init__(daemon=True)
        self.stop_ev, self.mi, self.tk = threading.Event(), [], {}

    def run(self):
        last = None
        while not self.stop_ev.is_set():
            now = time.time()
            m = meminfo_all()
            v = (m.get("MemAvailable"), m.get("MemAvailableKernel"), m.get("MemAvailableHarness"))
            if m and v != last:
                self.mi.append((now, *v)); last = v
            for p in glob.glob(LEDGER + "/*.ticket"):
                try:
                    mt = os.stat(p).st_mtime_ns
                except OSError:
                    continue
                self.tk.setdefault((os.path.basename(p), mt), now)
            time.sleep(0.01)

    def finish(self):
        self.stop_ev.set(); self.join(5)
        out = []
        for (name, mt), seen in sorted(self.tk.items(), key=lambda kv: kv[0][1]):
            t = mt / 1e9
            prior = [r for r in self.mi if r[0] <= t]
            r = prior[-1] if prior else None
            win = [x[1] for x in self.mi if t - 2 <= x[0] <= t and x[1] is not None]
            out.append({"ticket": name[:12], "mtime": round(t, 3), "first_seen_lag_s": round(seen - t, 3),
                        "fed_mib": r[1] if r else None, "kernel_mib": r[2] if r else None,
                        "harness_mib": r[3] if r else None, "fed_age_s": round(t - r[0], 3) if r else None,
                        "fed_min_2s_mib": min(win) if win else None, "derived": True})
        return out


def locks_for(inodes):
    """/proc/locks lines per ticket inode: [(type, mode)]. Loop device is 07:00."""
    res = {}
    want = {str(i): n for n, i in inodes.items()}
    for l in open("/proc/locks"):
        f = l.split()
        if f[1] == "->":
            f = f[1:]
        dev_ino = f[5] if len(f) > 5 else ""
        ino = dev_ino.rsplit(":", 1)[-1]
        if ino in want:
            res.setdefault(want[ino], []).append(f[1] + "/" + f[3])
    return res


def hop_fds(hop):
    """Count fds of any process in the hop that point at a *.ticket file."""
    r = sh("podman", "exec", PFX + "-" + hop, "sh", "-c",
           "for p in /proc/[0-9]*/fd/*; do readlink $p; done 2>/dev/null | grep -c '\\.ticket' || true", timeout=60)
    try:
        return int(r.stdout.strip().splitlines()[-1])
    except (ValueError, IndexError):
        return None


class Sampler(threading.Thread):
    """While a request runs: memory.current, ticket files, /proc/locks, and (slower) per-hop ticket fds."""
    def __init__(self, mem):
        super().__init__(daemon=True)
        self.mem, self.stop_ev = mem, threading.Event()
        self.tickets, self.locks, self.fds, self.max_sh = set(), [], {h: 0 for h in HOPS}, 0
        self.fd_thread = threading.Thread(target=self.fd_loop, daemon=True)

    def run(self):
        self.fd_thread.start()
        while not self.stop_ev.is_set():
            self.mem.sample()
            inos = ticket_inodes()
            self.tickets |= set(inos)
            if inos:
                lk = locks_for(inos)
                for t, ls in lk.items():
                    n_sh = sum(1 for x in ls if x.endswith("/READ"))
                    if n_sh > self.max_sh:
                        self.max_sh = n_sh; self.locks.append({"t": round(time.time(), 1), "ticket": t, "locks": ls})
            time.sleep(0.2)

    def fd_loop(self):
        while not self.stop_ev.is_set():
            if ticket_inodes():
                for h in HOPS:
                    n = hop_fds(h)
                    if n:
                        self.fds[h] = max(self.fds[h], n)
            else:
                time.sleep(0.3)

    def finish(self):
        self.stop_ev.set(); self.join(5); self.fd_thread.join(90)
        return {"tickets_seen": sorted(self.tickets), "max_shared_locks": self.max_sh,
                "lock_snapshots": self.locks[-3:], "hops_with_ticket_fd": {h: n for h, n in self.fds.items() if n}}


def mock_records(rec, since):
    out = []
    p = rec + "/requests.jsonl"
    if os.path.exists(p):
        for l in open(p):
            r = json.loads(l)
            if r["seq"] > since:
                out.append(r)
    return out


def residual_files():
    """Regular files left in spool dirs (anonymous spools must leave none) and tickets in the ledger."""
    out = {}
    for d in ["keeper-store", "bridge-spool", "sub2api-spool", "aisix-spool", "cpa-spool", "ledger"]:
        r = sh("podman", "unshare", "find", FS + "/" + d, "-type", "f", "-not", "-name", "*.db", "-not", "-name", "*.lock")
        files = [x for x in r.stdout.split() if x]
        if files:
            out[d] = files[:5]
    return out


def hop_pid(hop):
    r = sh("podman", "inspect", "-f", "{{.State.Pid}}", PFX + "-" + hop)
    try:
        return int(r.stdout.strip())
    except ValueError:
        return None


class NetFlow(threading.Thread):
    """Per-hop eth0 rx/tx byte counters (the hop's netns, read from the host) every 50 ms, with a
    monotonic timestamp. rx = bytes the hop received (request body from downstream, response from
    upstream); tx = bytes it sent. Request bodies dominate both by orders of magnitude."""
    def __init__(self, hops=HOPS, period=0.05):
        super().__init__(daemon=True)
        self.files = {}
        for h in hops:
            pid = hop_pid(h)
            if pid:
                self.files[h] = "/proc/%d/net/dev" % pid
        self.period, self.stop_ev, self.series = period, threading.Event(), {h: [] for h in self.files}

    @staticmethod
    def read(path):
        """rx = eth0 rx + lo rx (the client reaches keeper through rootlessport on the container's
        loopback); tx = eth0 tx (the hop's upstream send)."""
        rx = tx = None
        lo = 0
        for l in open(path):
            name, _, rest = l.partition(":")
            f = rest.split()
            if name.strip() == "eth0":
                rx, tx = int(f[0]), int(f[8])
            elif name.strip() == "lo":
                lo = int(f[0])
        return None if rx is None else (rx + lo, tx)

    def run(self):
        while not self.stop_ev.is_set():
            t = time.time()
            for h, p in self.files.items():
                try:
                    v = self.read(p)
                except OSError:
                    v = None
                if v:
                    self.series[h].append((t, v[0], v[1]))
            time.sleep(self.period)

    def finish(self):
        self.stop_ev.set(); self.join(5)
        return self.series


def self_period(s):
    return (s[-1][0] - s[0][0]) / max(1, len(s) - 1)


BULK = 64 * 1024   # bytes per sample that count as body bytes moving (ACKs and DB chatter stay below)


def flow_summary(series, t0, n_bytes, quiet=2048, t_end=None):
    """Per hop, from the counter series (t, rx, tx) and the client's first-byte time t0 (unix time).
    A sample is an rx-bulk sample when rx moved >= BULK and at least half as much as tx did (so the
    ACKs of a send do not count), and the same for tx.
      ingest  = first rx-bulk sample .. last rx-bulk sample of the run that crosses 0.9 n_bytes;
      send    = first tx-bulk sample .. last tx-bulk sample of the run that crosses 0.9 n_bytes;
      prep    = ingest end .. send start; wait = send end .. the upstream's first reply bytes (rx >= 256 B);
      longest_gap = longest span with no movement (rx and tx each < quiet per sample) between the hop's
      first ingest byte and last movement, the first reply bytes included.
    Times are seconds after t0; resolution is one sample period (about 50 ms)."""
    out = {}
    for h, s in series.items():
        s = [v for v in s if v[0] >= t0 - 0.2 and (t_end is None or v[0] <= t_end + 0.5)]
        if len(s) < 3:
            continue
        per = self_period(s)
        d = [(s[i][0], s[i][1] - s[i - 1][1], s[i][2] - s[i - 1][2]) for i in range(1, len(s))]
        r0, x0 = s[0][1], s[0][2]
        cum = {s[i][0]: (s[i][1] - r0, s[i][2] - x0) for i in range(len(s))}
        def bulk(k):
            o = 2 if k == 1 else 1
            return [t for t, *dv in d if dv[k - 1] >= BULK and dv[k - 1] >= 0.5 * dv[o - 1]]
        def span(k):
            b = bulk(k)
            if not b:
                return None, None
            cross = next((t for t in b if cum[t][k - 1] >= 0.9 * n_bytes), None)
            if cross is None:
                return b[0], None
            ts = [t for t, *_ in d]
            j = ts.index(cross)
            while j + 1 < len(d) and d[j + 1][k] >= BULK:
                j += 1
            return b[0], ts[j]
        rx_start, rx_end = span(1)
        tx_start, tx_end = span(2)
        reply = None
        if tx_end is not None:
            base = cum[tx_end][0]
            reply = next((t for t, *_ in d if t > tx_end and cum[t][0] - base >= 256), None)
        mv = [t for t, dr, dx in d if (dr >= quiet or dx >= quiet) and (rx_start is None or t >= rx_start)]
        pts = sorted(set(mv) | ({reply} if reply else set()))
        gap, gap_at = 0.0, None
        for a, b in zip(pts, pts[1:]):
            g = b - a - per
            if g > gap:
                gap, gap_at = g, round(a - t0, 2)
        rel = lambda t: None if t is None else round(t - t0, 2)
        dif = lambda a, b: None if a is None or b is None else round(b - a, 2)
        out[h] = {"rx_start": rel(rx_start), "rx_end": rel(rx_end), "tx_start": rel(tx_start), "tx_end": rel(tx_end),
                  "reply_first": rel(reply), "rx_total": s[-1][1] - r0, "tx_total": s[-1][2] - x0,
                  "ingest_s": dif(rx_start, rx_end), "prep_s": dif(rx_end, tx_start), "send_s": dif(tx_start, tx_end),
                  "wait_s": dif(tx_end, reply), "longest_gap_s": round(gap, 2), "longest_gap_at": gap_at,
                  "period_s": round(per, 3)}
    return out


AISIX_PROBE_CODE = r"""
import json, os, sys, time
pid, cg, period = int(sys.argv[1]), sys.argv[2], float(sys.argv[3])
keys = ("anon", "file", "active_file", "inactive_file", "file_dirty", "file_writeback", "shmem")
d = "/proc/%d/fd" % pid
ppid = os.getppid()
while os.getppid() == ppid:  # exit once the podman unshare parent is killed
    kv = dict(l.split() for l in open(cg + "/memory.stat"))
    st = {k: int(kv.get(k, 0)) >> 20 for k in keys}
    n = size = 0; links = set()
    for fd in os.listdir(d):
        try:
            l = os.readlink(d + "/" + fd)
        except OSError:
            continue
        if "aisix-spool" in l or "(deleted)" in l:
            n += 1; links.add(l)
            try:
                size += os.stat(d + "/" + fd).st_size
            except OSError:
                pass
    st.update(blobs=n, blob_mib=size >> 20, links=sorted(links)[:4], t=round(time.time(), 2))
    print(json.dumps(st), flush=True)
    time.sleep(period)
"""


class AisixProbe(threading.Thread):
    """While a rung runs, every 0.1 s: AISIX's cgroup memory.stat (anon vs file) and its open spool fds. A spool
    blob is an O_TMPFILE in /var/lib/aisix-spool, so its fd link reads '/var/lib/aisix-spool/#<ino> (deleted)';
    its size is the fstat of the fd link. AISIX runs as uid 10001, so the host user cannot read /proc/<pid>/fd;
    the sampler runs once per rung under `podman unshare` (root in the rootless user namespace)."""
    KEYS = ("anon", "file", "active_file", "inactive_file", "file_dirty", "file_writeback", "shmem")

    def __init__(self, period=0.1):
        super().__init__(daemon=True)
        self.pid, self.dir = hop_pid("aisix"), cgdir("aisix")
        self.series, self.err = [], None
        self.proc = subprocess.Popen(["podman", "unshare", "python3", "-c", AISIX_PROBE_CODE, str(self.pid), self.dir,
                                      str(period)], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)

    def run(self):
        for line in self.proc.stdout:
            try:
                self.series.append(json.loads(line))
            except ValueError:
                pass

    def finish(self):
        self.proc.kill()
        try:
            _, err = self.proc.communicate(timeout=10)
            self.err = (err or "")[-300:] or None
        except subprocess.TimeoutExpired:
            self.err = "probe did not exit"
        self.join(5)
        s = self.series
        if not s:
            return {"error": self.err, "pid": self.pid}
        mx = {k: max(x[k] for x in s) for k in self.KEYS + ("blobs", "blob_mib")}
        at = max(s, key=lambda x: x["anon"] + x["file"])
        return {"pid": self.pid, "samples": len(s), "error": self.err, "start": {k: s[0][k] for k in self.KEYS},
                "max": mx, "at_peak_anon_plus_file": {k: at[k] for k in self.KEYS + ("blobs", "blob_mib")},
                "links_seen": sorted({l for x in s for l in x["links"]})[:6],
                "timeline": [{k: x[k] for k in ("t", "anon", "file", "blobs", "blob_mib")} for x in s[::5]]}


def hop_snapshot():
    """Per hop and parent: anon, working set (current - inactive_file), current, file, in MiB; plus meminfo."""
    out = {"t": round(time.time(), 1), "meminfo": meminfo_all()}
    for h in HOPS + ["parent"]:
        d = cgdir(h)
        if not d:
            continue
        kv = dict(l.split() for l in open(d + "/memory.stat"))
        cur = int(open(d + "/memory.current").read())
        out[h] = {"anon": int(kv["anon"]) >> 20, "ws": (cur - int(kv["inactive_file"])) >> 20, "current": cur >> 20,
                  "file": int(kv["file"]) >> 20, "active_file": int(kv["active_file"]) >> 20}
    return out
