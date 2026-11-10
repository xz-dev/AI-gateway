#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
# shellcheck source=ops/scripts/container-runtime.sh
source "$root/ops/scripts/container-runtime.sh"
runtime=$AI_GATEWAY_RUNTIME
work=$(mktemp -d /var/tmp/ai-gateway-bridge-spool-test.XXXXXX)
containers=() volumes=()
cleanup() {
  for id in "${containers[@]}"; do "$runtime" rm -f "$id" >/dev/null 2>&1 || true; done
  for name in "${volumes[@]}"; do "$runtime" volume rm "$name" >/dev/null 2>&1 || true; done
  rm -rf -- "$work"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
cd "$root"
readarray -t images < <(python3 - <<'PY'
import json
lock = json.load(open('ci/large-request/ci/compatibility-lock.json'))
print(lock['images']['mock_base'])
print(lock['images']['bridge'])
PY
)
alpine=${images[0]} bridge=${images[1]}
export COMPOSE_PROJECT_NAME="bridge-spool-test-$$"
# Do not load a deployment's private override during these isolated tests.
volume="${COMPOSE_PROJECT_NAME}_custom-spool"
printf 'services: {}\nvolumes:\n  responses-ws-sse-bridge-spool:\n    name: %s\n' "$volume" >"$work/override.yaml"
export AI_GATEWAY_COMPOSE_OVERRIDE="$work/override.yaml"
prepare() { "$root/ops/scripts/prepare-bridge-spool.sh" "$root/.env.example"; }
volumes+=("$volume")
prepare >"$work/prepare.log"
mountpoint=$("$runtime" volume inspect "$volume" --format '{{.Mountpoint}}')
# Exact read-only bind avoids Podman's automatic named-volume ownership changes.
metadata() {
  "$runtime" run --rm --network none --read-only --mount "type=bind,source=$mountpoint,target=/spool,readonly" "$alpine" \
    sh -ec 'stat -c "%u:%g:%a:%d:%i" /spool; find /spool -mindepth 1 -maxdepth 1 -exec stat -c "%n:%u:%g:%a:%s" {} \;'
}
[[ $(metadata) == 65534:65534:700:* ]]
prepare >"$work/repeat.log"
id=$("$runtime" create --network none --read-only --user 65534:65534 \
  --cap-drop ALL --security-opt no-new-privileges -v "$volume:/spool:nocopy" \
  -e UPSTREAM_URL=http://127.0.0.1:8080 -e LISTEN_ADDR=127.0.0.1:8086 \
  -e PORT=8086 -e SPOOL_DIR=/spool "$bridge")
containers+=("$id")
before=$(metadata)
# Even a stopped/never-started container reference must block preparation.
if prepare >"$work/stopped.log" 2>&1; then echo 'accepted a stopped reference' >&2; exit 1; fi
[ "$(metadata)" = "$before" ]
"$runtime" start "$id" >/dev/null
healthy() {
  for _attempt in $(seq 1 20); do
    if "$runtime" exec "$id" /responses-ws-sse-bridge healthcheck >/dev/null 2>&1; then return; fi
    sleep 0.2
  done
  "$runtime" logs "$id" >&2
  return 1
}
healthy
if prepare >"$work/running.log" 2>&1; then echo 'accepted a running reference' >&2; exit 1; fi
[ "$(metadata)" = "$before" ]
"$runtime" restart "$id" >/dev/null
healthy
"$runtime" rm -f "$id" >/dev/null
containers=()
# A hidden legacy sentinel must survive refused preparation byte-for-byte/mode/owner.
"$runtime" run --rm --network none --mount "type=bind,source=$mountpoint,target=/spool" "$alpine" \
  sh -ec 'printf "legacy-spool-sentinel\n" > /spool/.legacy; chmod 640 /spool/.legacy; chown 123:456 /spool; chmod 711 /spool'
sentinel_before=$("$runtime" run --rm --network none --mount "type=bind,source=$mountpoint,target=/spool,readonly" "$alpine" \
  sh -ec 'stat -c "%d:%i:%u:%g:%a" /spool /spool/.legacy; sha256sum /spool/.legacy')
if prepare >"$work/nonempty.log" 2>&1; then echo 'accepted nonempty volume' >&2; exit 1; fi
sentinel_after=$("$runtime" run --rm --network none --mount "type=bind,source=$mountpoint,target=/spool,readonly" "$alpine" \
  sh -ec 'stat -c "%d:%i:%u:%g:%a" /spool /spool/.legacy; sha256sum /spool/.legacy')
[ "$sentinel_after" = "$sentinel_before" ]
echo 'bridge_spool=0700_uid65534 fresh_start=healthy restart=healthy stopped_reference=refused running_reference=refused legacy_sentinel=unchanged'
