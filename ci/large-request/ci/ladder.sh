#!/usr/bin/env bash
# ci/ladder.sh: CI capacity ladder (OpenSpec large-request-capacity task 8.2 skeleton). Portable: no host paths, no
# loop filesystem, no THP tool, rootless podman + a compose provider (podman compose / podman-compose).
#   ci/ladder.sh [RUNG...]     default rungs: $DEFAULT_RUNGS below; NIGHTLY=1 adds the 1 GiB rungs
# Images come from the lock (LOCK, default ci/compatibility-lock.json) as name@sha256 digests; placeholder (all-zero)
# digests are refused. Rungs run with the existing harness (rungs.py, the mock, wsclient, gen*.py, manifest.py).
# Output: $RUN/summary.json, $RUN/junit.xml, results.jsonl, manifest.json, logs/; exit 0 = every rung passed.
# Environment (all optional):
#   LOCK=path              lock file to read the images from
#   NIGHTLY=1              add http-image-1g / ws-image-1g, raise the cap to 1 GiB on this stack only (CAP_BYTES);
#                          refuses to start with < 12 GiB MemAvailable or < 12 GiB free disk (spec 8.2)
#   LADDER_FS=dir          spool / ledger root, e.g. a size-limited ext4 mount (default: $RUN/fs, plain directory)
#   LADDER_MAX_LOAD=N      do not start containers while the 1-min load is >= N
#   PFX=name               container / project prefix (default lrcchain8-ci)
#   RUN_ROOT=dir           where run directories go (default logs/ci in the harness root)
#   COMPOSE="cmd"          compose command (default: podman compose)
#   LADDER_SETTLE_S=N      idle seconds after the stack is healthy (default 10)
set -u
H=$(cd "$(dirname "$0")/.." && pwd)
DEFAULT_RUNGS="http-image-1m ws-image-1m http-text-16m ws-text-16m http-image-64m ws-image-64m http-image-256m ws-image-256m http-small-16m http-tiny-16m"
export LRC_HOME=$H PFX=${PFX:-lrcchain8-ci} EXTRA="" ENTRY_URL=http://127.0.0.1:18085
LOCK=${LOCK:-$H/ci/compatibility-lock.json}
COMPOSE=${COMPOSE:-podman compose}
RUNGS=("$@"); [ ${#RUNGS[@]} = 0 ] && read -ra RUNGS <<< "$DEFAULT_RUNGS"
export CAP_BYTES=268435456
if [ "${NIGHTLY:-0}" = 1 ]; then
  export CAP_BYTES=1073741824; RUNGS+=(http-image-1g ws-image-1g)
  avail=$(awk '/^MemAvailable:/{print int($2/1048576)}' /proc/meminfo); disk=$(df -Pk "${RUN_ROOT:-$H}" | awk 'NR==2{print int($4/1048576)}')
  [ "$avail" -ge 12 ] && [ "$disk" -ge 12 ] || { echo "NIGHTLY needs >= 12 GiB free memory and disk (have ${avail} / ${disk} GiB)"; exit 3; }
fi
# images from the lock: name@sha256:<64 hex> only, no placeholders
envs=$(python3 - "$LOCK" <<'EOF'
import json, re, sys
imgs = json.load(open(sys.argv[1]))["images"]
env = {"keeper": "KEEPER_IMAGE", "bridge": "BRIDGE_IMAGE", "sub2api": "SUB2API_IMAGE", "aisix": "AISIX_IMAGE",
       "cpa": "CPA_IMAGE", "postgres": "POSTGRES_IMAGE", "redis": "REDIS_IMAGE", "mock_base": "MOCK_BASE_IMAGE"}
for k, v in env.items():
    ref = imgs.get(k, "")
    if not re.fullmatch(r"[a-z0-9][a-z0-9._/:-]*@sha256:[0-9a-f]{64}", ref) or ref.endswith("0" * 64):
        sys.exit(f"lock {sys.argv[1]}: images.{k} is not a real name@sha256 digest: {ref!r}")
    print(f"export {v}={ref}")
EOF
) || exit 2
eval "$envs"
export RUN=${RUN_ROOT:-$H/logs/ci}/$(date -u +%Y%m%dT%H%M%SZ)-$([ "${NIGHTLY:-0}" = 1 ] && echo nightly-)$$
export FS=${LADDER_FS:-$RUN/fs}
mkdir -p "$RUN"/logs "$RUN"/rec "$FS"
P() { timeout -k 5 300 podman "$@"; }
C() { timeout -k 5 600 $COMPOSE -f "$H"/ci/compose.yaml "$@"; }

echo "ladder: run $RUN, lock $LOCK, rungs ${RUNGS[*]}, cap $CAP_BYTES"
cp "$LOCK" "$RUN"/lock-used.json

# harness tools from source (Go): the mock provider and the WS client
for t in mock wsclient; do
  [ -x "$H/bin/$t" ] || (cd "$H/$t" && CGO_ENABLED=0 go build -trimpath -o "$H/bin/$t" .) || { echo "build $t failed"; exit 2; }
done

if [ -n "${LADDER_MAX_LOAD:-}" ] && ! awk -v m="$LADDER_MAX_LOAD" '{exit !($1 < m)}' /proc/loadavg; then
  echo "load $(cut -d' ' -f1 /proc/loadavg) >= $LADDER_MAX_LOAD: not starting containers"; exit 4
fi
for v in KEEPER_IMAGE BRIDGE_IMAGE SUB2API_IMAGE AISIX_IMAGE CPA_IMAGE POSTGRES_IMAGE REDIS_IMAGE MOCK_BASE_IMAGE; do
  P image exists "${!v}" || P pull -q "${!v}" >/dev/null || { echo "pull ${!v} failed"; exit 2; }
done

# spool and ledger dirs, owned by each hop's user inside the rootless user namespace (as stack.sh fsprep)
mkdir -p "$FS"/{ledger,keeper-store,bridge-spool,sub2api-spool,aisix-spool,cpa-spool,s2a-data,cpa-auth,cpa-logs}
chmod 0750 "$FS"/ledger; chmod 0700 "$FS"/{keeper-store,bridge-spool,sub2api-spool,aisix-spool,cpa-spool}
P unshare chown 65534:65534 "$FS"/ledger "$FS"/keeper-store "$FS"/bridge-spool
P unshare chown 1000:1000 "$FS"/sub2api-spool "$FS"/s2a-data
P unshare chown 10001:10001 "$FS"/aisix-spool
# harness only: the host user (container uid 0) lists tickets for lease sampling
P unshare setfacl -m u:0:rx "$FS"/ledger || echo "setfacl failed: ticket sampling will see no tickets"
echo ok > "$RUN"/rec/mode; chmod 0777 "$RUN"/rec

finish() {
  for c in keeper bridge sub2api aisix cpa mock pg redis; do P logs "$PFX-$c" > "$RUN/logs/$c.log" 2>&1; done
  C down -t 2 > "$RUN"/logs/compose-down.log 2>&1
  P unshare rm -rf "$FS"/ledger "$FS"/keeper-store "$FS"/bridge-spool "$FS"/sub2api-spool "$FS"/aisix-spool \
    "$FS"/cpa-spool "$FS"/s2a-data "$FS"/cpa-auth "$FS"/cpa-logs 2>/dev/null
}
trap finish EXIT
wait_http() {  # wait_http NAME URL SECS
  local i; for i in $(seq "$3"); do curl -fsS -o /dev/null --max-time 2 "$2" 2>/dev/null && return 0; sleep 1; done
  echo "$1 not healthy after $3 s"; P logs --tail 40 "$PFX-$1"; return 1
}

C down -t 2 > /dev/null 2>&1
C up -d mock pg redis > "$RUN"/logs/compose-up.log 2>&1 || { tail -30 "$RUN"/logs/compose-up.log; exit 2; }
for i in $(seq 60); do P exec "$PFX-pg" pg_isready -q -U postgres -d sub2api && break; sleep 1; done
C up -d cpa aisix sub2api >> "$RUN"/logs/compose-up.log 2>&1 || { tail -30 "$RUN"/logs/compose-up.log; exit 2; }
wait_http sub2api http://127.0.0.1:18090/health 180 || exit 2
python3 "$H"/s2a_setup.py > "$RUN"/s2a-setup.txt 2>&1 || { tail -20 "$RUN"/s2a-setup.txt; exit 2; }
C up -d bridge keeper >> "$RUN"/logs/compose-up.log 2>&1 || { tail -30 "$RUN"/logs/compose-up.log; exit 2; }
wait_http keeper http://127.0.0.1:18085/healthz 60 || exit 2
echo "healthy $(date -u +%FT%TZ); settling ${LADDER_SETTLE_S:-10} s"; sleep "${LADDER_SETTLE_S:-10}"

python3 "$H"/manifest.py || echo "manifest failed"
python3 "$H"/rungs.py "${RUNGS[@]}" | tee "$RUN"/rungs.out

# restart counts (-1 = not running at the end), then the verdict
python3 - "$RUN" > "$RUN"/restarts.json <<'EOF'
import json, os, subprocess, sys
out = {}
for c in ("keeper", "bridge", "sub2api", "aisix", "cpa", "mock", "pg", "redis"):
    r = subprocess.run(["timeout", "-k", "5", "60", "podman", "inspect", "-f", "{{.RestartCount}} {{.State.Status}}",
                        os.environ["PFX"] + "-" + c], capture_output=True, text=True)
    n, _, st = r.stdout.strip().partition(" ")
    out[c] = int(n) if st == "running" and n.isdigit() else -1
print(json.dumps(out))
EOF
python3 "$H"/ci/verdict.py "$RUN" "${RUNGS[@]}"; rc=$?
exit $rc
