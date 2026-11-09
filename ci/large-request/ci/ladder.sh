#!/usr/bin/env bash
# Throwaway full-stack capacity ladder. Images are immutable digests from LOCK.
# NIGHTLY=1 raises every request cap to 1 GiB; explicit RUNG arguments replace the default ladder.
# RUN_ROOT and LADDER_FS must resolve below disk-backed /var/tmp (never tmpfs /tmp).
# Container groups wait up to 30 minutes for >=4 GiB MemAvailable and load <=12.
set -uo pipefail
H=$(cd "$(dirname "$0")/.." && pwd)
DEFAULT_RUNGS="http-image-1m ws-image-1m http-text-16m ws-text-16m http-image-64m ws-image-64m http-image-256m ws-image-256m http-small-16m http-tiny-16m"
REPO_ROOT=$(cd "$H/../.." && pwd)
export LRC_HOME=$H REPO_ROOT PFX=${PFX:-lrcchain8-ci}
export EXTRA="" ENTRY_URL=http://127.0.0.1:18085 S2A_UPSTREAM_URL=http://aisix:3000
LOCK=${LOCK:-$H/ci/compatibility-lock.json}
COMPOSE_FILES=(-f "$H/ci/compose.yaml")
read -ra COMPOSE_CMD <<< "${COMPOSE:-podman compose}"
RUNGS=("$@")
export CAP_BYTES=268435456
if [ "${NIGHTLY:-0}" = 1 ]; then
  export CAP_BYTES=1073741824 EXTRA="firewall apisix-models squid"
  export ENTRY_URL=http://127.0.0.1:${FW_PORT:-18080} S2A_UPSTREAM_URL=http://apisix-models:9080
  LOCK=${NIGHTLY_LOCK:-$H/ci/nightly-compatibility-lock.json}
  COMPOSE_FILES+=(-f "$H/ci/compose.full-stack.yaml")
  DEFAULT_RUNGS+=" http-image-1g ws-image-1g"
fi
[ ${#RUNGS[@]} != 0 ] || read -ra RUNGS <<< "$DEFAULT_RUNGS"
RUN=${RUN_ROOT:-/var/tmp/ai-gateway-ladder/logs/ci}/$(date -u +%Y%m%dT%H%M%SZ)-$$
export RUN
export FS=${LADDER_FS:-$RUN/fs} BODIES=$RUN/bodies CPA_CONFIG=$RUN/cpa-config.yaml
mkdir -p "$RUN"/logs "$RUN"/rec "$FS" "$BODIES"
for path in "$RUN" "$FS"; do
  case $(realpath "$path") in /var/tmp/*) ;; *) echo "refusing non-/var/tmp path: $path"; exit 3;; esac
  case $(findmnt -n -o FSTYPE --target "$path") in tmpfs|ramfs) echo "refusing RAM filesystem: $path"; exit 3;; esac
done
if [ "${NIGHTLY:-0}" = 1 ]; then
  avail=$(awk '/^MemAvailable:/{print int($2/1024)}' /proc/meminfo)
  disk=$(df -Pk "$FS" | awk 'NR==2{print $4}')
  echo "nightly precheck: MemAvailable=${avail} MiB; ledger free=${disk} KiB"
  [ "$avail" -ge 1216 ] && [ "$disk" -ge 8388608 ] || { echo "NIGHTLY needs >=1216 MiB MemAvailable and >=8 GiB ledger free"; exit 3; }
fi
# Reject tags and placeholder digests before any container is started.
envs=$(python3 - "$LOCK" <<'EOF'
import json, os, re, sys
imgs = json.load(open(sys.argv[1]))["images"]
env = {"keeper": "KEEPER_IMAGE", "bridge": "BRIDGE_IMAGE", "sub2api": "SUB2API_IMAGE", "aisix": "AISIX_IMAGE",
       "cpa": "CPA_IMAGE", "postgres": "POSTGRES_IMAGE", "redis": "REDIS_IMAGE", "mock_base": "MOCK_BASE_IMAGE",
       }
if os.environ.get("NIGHTLY") == "1":
    env.update(firewall="APISIX_IMAGE", squid="SQUID_IMAGE")
if os.environ.get("NIGHTLY") == "1":
    assert imgs.get("apisix-models") == imgs.get("firewall"), "APISIX hop digests must match"
for k, v in env.items():
    ref = imgs.get(k, "")
    if not re.fullmatch(r"[a-z0-9][a-z0-9._/:-]*@sha256:[0-9a-f]{64}", ref) or ref.endswith("0" * 64):
        sys.exit(f"lock {sys.argv[1]}: images.{k} is not a real name@sha256 digest: {ref!r}")
    print(f"export {v}={ref}")
EOF
) || exit 2
eval "$envs"
P() { timeout -k 5 300 podman "$@"; }
C() { timeout -k 5 600 "${COMPOSE_CMD[@]}" "${COMPOSE_FILES[@]}" "$@"; }
resources() {
  local deadline=$((SECONDS + 1800))
  while :; do
    free -m; uptime
    if awk '/^MemAvailable:/{exit !($2 >= 4194304)}' /proc/meminfo &&
       awk '{exit !($1 <= 12)}' /proc/loadavg; then return 0; fi
    [ "$SECONDS" -lt "$deadline" ] || { echo "resource wait exceeded 30 min"; return 1; }
    sleep 30
  done
}
echo "ladder: run $RUN, lock $LOCK, rungs ${RUNGS[*]}, cap $CAP_BYTES"
cp "$LOCK" "$RUN"/lock-used.json
for t in mock wsclient; do
  [ -x "$H/bin/$t" ] || (cd "$H/$t" && CGO_ENABLED=0 go build -trimpath -o "$H/bin/$t" .) || { echo "build $t failed"; exit 2; }
done
images=(KEEPER_IMAGE BRIDGE_IMAGE SUB2API_IMAGE AISIX_IMAGE CPA_IMAGE POSTGRES_IMAGE REDIS_IMAGE MOCK_BASE_IMAGE)
[ "${NIGHTLY:-0}" != 1 ] || images+=(APISIX_IMAGE SQUID_IMAGE)
for v in "${images[@]}"; do
  P image exists "${!v}" || P pull -q "${!v}" >/dev/null || { echo "pull ${!v} failed"; exit 2; }
done
mkdir -p "$FS"/{ledger,keeper-store,bridge-spool,sub2api-spool,aisix-spool,cpa-spool,s2a-data,cpa-auth,cpa-logs}
chmod 0750 "$FS"/ledger; chmod 0700 "$FS"/{keeper-store,bridge-spool,sub2api-spool,aisix-spool,cpa-spool}
P unshare chown 65534:65534 "$FS"/ledger "$FS"/keeper-store "$FS"/bridge-spool
P unshare chown 1000:1000 "$FS"/sub2api-spool "$FS"/s2a-data
P unshare chown 10001:10001 "$FS"/aisix-spool
P unshare setfacl -m u:0:rx "$FS"/ledger || echo "setfacl failed: ticket sampling will see no tickets"
echo ok > "$RUN"/rec/mode; chmod 0777 "$RUN"/rec
# shellcheck disable=SC2329 # Invoked by the EXIT trap.
finish() {
  for c in keeper bridge sub2api aisix cpa mock pg redis $EXTRA; do P logs "$PFX-$c" > "$RUN/logs/$c.log" 2>&1; done
  C down -t 2 > "$RUN"/logs/compose-down.log 2>&1
  P unshare rm -rf "$FS"/ledger "$FS"/keeper-store "$FS"/bridge-spool "$FS"/sub2api-spool "$FS"/aisix-spool \
    "$FS"/cpa-spool "$FS"/s2a-data "$FS"/cpa-auth "$FS"/cpa-logs 2>/dev/null
  rm -f "$BODIES"/*
}
trap finish EXIT
trap 'exit 130' INT TERM
wait_http() {
  local i; for i in $(seq "$3"); do curl -fsS -o /dev/null --max-time 2 "$2" 2>/dev/null && return 0; sleep 1; done
  echo "$1 not healthy after $3 s"; P logs --tail 40 "$PFX-$1"; return 1
}
# Compose parses all bind mounts even for the first group; render initially with loopback DNS.
python3 - "$REPO_ROOT" "$RUN" "$CAP_BYTES" <<'EOF'
import sys
from pathlib import Path
sys.path.insert(0, str(Path(sys.argv[1]) / "ci/large-request/ci"))
from render_hops import render
render(Path(sys.argv[1]), Path(sys.argv[2]), int(sys.argv[3]), "127.0.0.1")
EOF
C down -t 2 > /dev/null 2>&1
resources || exit 4
C up -d mock pg redis > "$RUN"/logs/compose-up.log 2>&1 || { tail -30 "$RUN"/logs/compose-up.log; exit 2; }
if [ "${NIGHTLY:-0}" = 1 ]; then python3 "$H"/ci/render_hops.py || exit 2; fi
for i in $(seq 60); do P exec "$PFX-pg" pg_isready -q -U postgres -d sub2api && break; sleep 1; done
resources || exit 4
middle=(cpa aisix sub2api)
[ "${NIGHTLY:-0}" != 1 ] || middle+=(squid apisix-models)
C up -d "${middle[@]}" >> "$RUN"/logs/compose-up.log 2>&1 || { tail -30 "$RUN"/logs/compose-up.log; exit 2; }
wait_http sub2api http://127.0.0.1:18090/health 180 || exit 2
python3 "$H"/s2a_setup.py > "$RUN"/s2a-setup.txt 2>&1 || { tail -20 "$RUN"/s2a-setup.txt; exit 2; }
resources || exit 4
front=(bridge keeper)
[ "${NIGHTLY:-0}" != 1 ] || front+=(firewall)
C up -d "${front[@]}" >> "$RUN"/logs/compose-up.log 2>&1 || { tail -30 "$RUN"/logs/compose-up.log; exit 2; }
wait_http keeper http://127.0.0.1:18085/healthz 60 || exit 2
if [ "${NIGHTLY:-0}" = 1 ]; then
  # The production front-door health endpoint is a native 404, not a 200.
  ready=0
  for ((i=0; i<60; i++)); do
    [ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "$ENTRY_URL/healthz")" != 404 ] || { ready=1; break; }
    sleep 1
  done
  [ "$ready" = 1 ] || { echo "firewall not ready"; exit 2; }
fi
echo "healthy $(date -u +%FT%TZ); settling ${LADDER_SETTLE_S:-10} s"; sleep "${LADDER_SETTLE_S:-10}"
python3 "$H"/manifest.py || exit 2
for rung in "${RUNGS[@]}"; do
  python3 "$H"/rungs.py "$rung" | tee -a "$RUN"/rungs.out || exit 2
  rm -f "$BODIES"/*
done
python3 - "$RUN" > "$RUN"/restarts.json <<'EOF'
import json, os, subprocess
out = {}
for c in ["keeper", "bridge", "sub2api", "aisix", "cpa", "mock", "pg", "redis"] + os.environ["EXTRA"].split():
    r = subprocess.run(["timeout", "-k", "5", "60", "podman", "inspect", "-f", "{{.RestartCount}} {{.State.Status}}",
                        os.environ["PFX"] + "-" + c], capture_output=True, text=True)
    n, _, st = r.stdout.strip().partition(" ")
    out[c] = int(n) if st == "running" and n.isdigit() else -1
print(json.dumps(out))
EOF
python3 "$H"/ci/verdict.py "$RUN" "${RUNGS[@]}"; rc=$?
exit $rc
