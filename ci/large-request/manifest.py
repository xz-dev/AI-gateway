#!/usr/bin/env python3
"""manifest.py: write $RUN/manifest.json for one run (M2 gate 7.5), right after the stack is up: image digests,
git SHAs, Go version and build flags per binary, config / compose hashes (production sources and the rendered
run copies), cgroup path, memory.max / swap.max, cpu.max / cpuset, GOMEMLIMIT and entrypoint per container, the
parent cgroup, THP mode, run flags, and the mock source hash."""
import glob, hashlib, json, os, platform, subprocess, time
import hlib

T, RUN, PFX = hlib.T, os.environ["RUN"], hlib.PFX
PROD = os.environ.get("PROD_GW", "/var/tmp/ai-gateway-transparent-pipes-20261103/gateway")
FLAGS = ("PFX", "FULLCHAIN", "FW", "KA", "SQUID", "FW_BODY", "ENTRY_URL", "EXTRA", "CPU_MODE", "THP_OFF", "MEMINFO_MODE",
         "MEMINFO_LOW_KB", "AVAIL_TARGET_MIB", "CPA_GOMEMLIMIT", "MOCK_PORT", "PROV_SUBNET", "GATE_RETRIES", "RAWDIR", "GUARD", "FW_PORT", "KA_PORT", "EMB", "AISIX_RES")


def sh(*a):
    a = (["timeout", "-k", "5", "50"] if a and a[0] == "podman" else []) + list(a)
    r = subprocess.run(a, capture_output=True, text=True, timeout=60)
    return r.stdout.strip()


def sha(p):
    try:
        h = hashlib.sha256()
        with open(p, "rb") as f:
            for b in iter(lambda: f.read(1 << 20), b""):
                h.update(b)
        return h.hexdigest()
    except OSError:
        return None


def rd(p):
    try:
        return open(p).read().strip()
    except OSError:
        return None


def gobuild(p):
    info = {"path": p, "sha256": sha(p)}
    out = sh("go", "version", "-m", p)
    if not out:
        return info
    lines = out.splitlines()
    info["go"] = lines[0].split(":", 1)[1].strip()
    for l in lines[1:]:
        f = l.strip().split("\t")
        if f[0] == "path":
            info["module"] = f[1]
        elif f[0] == "build" and len(f) > 1:
            k, _, v = f[1].partition("=")
            if k in ("vcs.revision", "vcs.modified", "vcs.time", "-ldflags", "-trimpath", "CGO_ENABLED", "GOEXPERIMENT",
                     "-tags", "GOAMD64", "-buildmode"):
                info[k] = v
    return info


bins = {"keeper": os.environ.get("KEEPER_BIN", T + "/bin/keeper"), "cpa": os.environ.get("CPA_BIN", T + "/bin/cpa"),
        "sub2api": os.environ.get("S2A_BIN", T + "/bin/sub2api"), "bridge": os.environ.get("BRIDGE_BIN", T + "/bin/bridge"), "mock": T + "/bin/mock",
        "wsclient": T + "/bin/wsclient", "thpoff": T + "/bin/thpoff"}
if os.environ.get("EMB") == "1":  # delta 7.5 group emb: the embeddings / rerank provider behind AISIX
    bins["mock-emb"] = T + "/bin/mock-emb"
m = {"written": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), "run": RUN,
     "flags": {k: os.environ.get(k) for k in FLAGS},
     "host": {"kernel": platform.release(), "nproc": os.cpu_count(), "load": rd("/proc/loadavg"),
              "go": sh("go", "version"), "podman": sh("podman", "--version"),
              "thp_enabled": rd("/sys/kernel/mm/transparent_hugepage/enabled"),
              "thp_defrag": rd("/sys/kernel/mm/transparent_hugepage/defrag"), "swaps": rd("/proc/swaps")},
     "harness": {"git": sh("git", "-C", T, "rev-parse", "HEAD"),
                 "dirty_files": sh("git", "-C", T, "status", "--porcelain", "--untracked-files=no").splitlines()},
     "binaries": {k: gobuild(v) for k, v in bins.items()},
     "aisix": {"path": T + "/bin/aisix", "sha256": sha(T + "/bin/aisix"),
               "source_head": sh("git", "-C", T + "/src/aisix", "rev-parse", "HEAD")},
     "s2a_resources": os.environ.get("S2A_RES"),
     "aisix_resources": {"path": os.environ.get("AISIX_RES", T + "/cfg/aisix-resources.yaml"),
                         "sha256": sha(os.environ.get("AISIX_RES", T + "/cfg/aisix-resources.yaml"))},
     "mock_source_sha256": sha(T + "/mock/main.go")}

files = {"prod:compose.yaml": PROD + "/compose.yaml",
         "prod:firewall/config.yaml": PROD + "/services/firewall/apisix/config.yaml",
         "prod:firewall/apisix.yaml": PROD + "/services/firewall/apisix/apisix.yaml",
         "prod:egress/policy.example.json": PROD + "/services/firewall/egress-proxy/policy.example.json",
         "prod:egress/unbound.conf": PROD + "/services/firewall/egress-proxy/unbound.conf",
         "prod:egress/Dockerfile": PROD + "/services/firewall/egress-proxy/Dockerfile",
         "prod:render-egress-policy.py": PROD + "/ops/scripts/render-egress-policy.py",
         "run:fw/config.yaml": RUN + "/fw/config.yaml", "run:fw/apisix.yaml": RUN + "/fw/apisix.yaml",
         "run:squid/policy.json": RUN + "/squid/policy.json", "run:squid/squid.conf": RUN + "/squid/generated/squid.conf",
         "run:squid/unbound.conf": RUN + "/squid/unbound.conf", "run:cpa-config.yaml": RUN + "/cpa-config.yaml",
         "cfg/cpa-config.yaml": T + "/cfg/cpa-config.yaml", "cfg/aisix-config.yaml": T + "/cfg/aisix-config.yaml",
         "cfg/aisix-resources.yaml": T + "/cfg/aisix-resources.yaml"}
for f in ("stack.sh", "rungs.py", "hlib.py", "meminfo.sh", "milog.py", "fullchain.py", "s2a_setup.py", "gen.py",
          "wsclient/main.go", "mock/main.go", "gate75.sh", "ci/ladder.sh", "ci/compose.yaml", "ci/verdict.py",
          "ci/lock_from_manifest.py", "gen_small.py", "gen_tiny.py"):
    files["harness:" + f] = T + "/" + f
for f in sorted(glob.glob(PROD + "/services/firewall/apisix/lua/*.lua")):
    files["prod:firewall/lua/" + os.path.basename(f)] = f
m["file_sha256"] = {k: sha(v) for k, v in files.items() if os.path.exists(v)}

p = hlib.PARENT_CG
m["parent_cgroup"] = {"path": p, "memory.max": rd(p + "/memory.max"), "memory.swap.max": rd(p + "/memory.swap.max"),
                      "subtree_control": rd(p + "/cgroup.subtree_control"), "parent.txt": rd(RUN + "/parent.txt")}
cons = {}
names = sh("podman", "ps", "--format", "{{.Names}}").split()
for n in sorted(x for x in names if x.startswith(PFX + "-")):
    j = json.loads(sh("podman", "inspect", n))[0]
    d = "/sys/fs/cgroup" + j["State"].get("CgroupPath", "")
    env = dict(e.split("=", 1) for e in j["Config"].get("Env") or [] if "=" in e)
    cons[n[len(PFX) + 1:]] = {
        "image": j.get("ImageName"), "image_id": j.get("Image"), "image_digest": j.get("ImageDigest"),
        "entrypoint": j["Config"].get("Entrypoint"), "cmd": j["Config"].get("Cmd"),
        "gomemlimit": env.get("GOMEMLIMIT"), "cgroup": d, "memory.max": rd(d + "/memory.max"),
        "memory.swap.max": rd(d + "/memory.swap.max"), "cpu.max": rd(d + "/cpu.max"),
        "cpuset.cpus": rd(d + "/cpuset.cpus"), "cpuset.cpus.effective": rd(d + "/cpuset.cpus.effective"),
        "pids.max": rd(d + "/pids.max"), "in_parent": d.startswith(p + "/"),
        "thp_off": "/thpoff" in json.dumps([j["Config"].get("Entrypoint"), j["Config"].get("Cmd")])}
m["containers"] = cons
json.dump(m, open(RUN + "/manifest.json", "w"), indent=1)
print(f"manifest: {len(cons)} containers, {len(m['file_sha256'])} file hashes -> {RUN}/manifest.json")
