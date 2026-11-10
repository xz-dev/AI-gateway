#!/usr/bin/env bash
# First-install prerequisite only. Never migrate a referenced/nonempty volume.
set -euo pipefail
umask 077
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
# shellcheck source=ops/scripts/container-runtime.sh
source "$root/ops/scripts/container-runtime.sh"
runtime=$AI_GATEWAY_RUNTIME
cd "$root"
env_file=${1:-.env}
[ "$#" -le 1 ] && [ -f "$env_file" ] || { echo 'usage: prepare-bridge-spool.sh [existing-env-file]' >&2; exit 1; }
work=$(mktemp -d /var/tmp/ai-gateway-bridge-spool.XXXXXX)
helper=
cleanup() {
  [ -z "$helper" ] || "$runtime" rm -f "$helper" >/dev/null
  rm -rf -- "$work"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
args=(-f "$root/compose.yaml")
[ -z "${COMPOSE_PROJECT_NAME:-}" ] || args+=(-p "$COMPOSE_PROJECT_NAME")
override=${AI_GATEWAY_COMPOSE_OVERRIDE:-}
if [ -z "$override" ] && [ -f "$root/compose.override.yaml" ]; then override=$root/compose.override.yaml; fi
[ -z "$override" ] || args+=(-f "$override")
if "${AI_GATEWAY_COMPOSE[@]}" "${args[@]}" --env-file "$env_file" config --format json >"$work/config" 2>/dev/null; then
  format=json
else
  "${AI_GATEWAY_COMPOSE[@]}" "${args[@]}" --env-file "$env_file" config >"$work/config"
  format=yaml
fi
python3 - "$work/config" "$format" >"$work/selection" <<'PY'
import json, os, re, sys
if sys.argv[2] == 'json':
    config = json.load(open(sys.argv[1]))
else:
    import yaml
    config = yaml.safe_load(open(sys.argv[1]))
key = 'responses-ws-sse-bridge-spool'
volume = config['volumes'][key]
# podman-compose config preserves the file's name even when -p overrides it.
project = os.environ.get('COMPOSE_PROJECT_NAME') or config['name']
name = volume.get('name') or f'{project}_{key}'
mounts = config['services']['responses-ws-sse-bridge']['volumes']
spool = [v for v in mounts if isinstance(v, dict) and v.get('target') == '/spool']
if (len(spool) != 1 or spool[0].get('type') != 'volume' or spool[0].get('source') != key
        or spool[0].get('read_only', False) or spool[0].get('volume', {}).get('nocopy') is not True
        or volume.get('external') or volume.get('driver', 'local') != 'local' or volume.get('driver_opts')):
    raise SystemExit('refusing unexpected bridge spool mount/volume; require local named volume with nocopy')
if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.-]*', name):
    raise SystemExit('invalid rendered bridge volume name')
image = json.load(open('ci/large-request/ci/compatibility-lock.json'))['images']['mock_base']
if not re.fullmatch(r'docker.io/library/alpine@sha256:[0-9a-f]{64}', image):
    raise SystemExit('preparation image must be the locked Alpine digest')
print(name)
print(project)
print(image)
PY
readarray -t selection <"$work/selection"
volume=${selection[0]} project=${selection[1]} image=${selection[2]}
# -a includes stopped containers. Fail closed before mounting any existing data.
references=$("$runtime" ps -aq --no-trunc --filter "volume=$volume")
[ -z "$references" ] || { echo 'bridge spool has container references; use a reviewed manual migration window' >&2; exit 1; }
"$runtime" info --format '{{json .}}' >"$work/runtime"
backend=$(python3 - "$work/runtime" <<'PY'
import json, sys
info = json.load(open(sys.argv[1]))
if 'host' in info and info['host'].get('serviceIsRemote') is False:
    print('podman')
elif info.get('DockerRootDir'):
    print('docker')
else:
    raise SystemExit('preparation requires a supported local runtime')
PY
)
if ! "$runtime" volume inspect "$volume" >"$work/volume" 2>/dev/null; then
  ownership=()
  # Otherwise Podman resets mode from the image on its first named-volume mount.
  [ "$backend" != podman ] || ownership=(--uid 65534 --gid 65534)
  "$runtime" volume create "${ownership[@]}" --label "com.docker.compose.project=$project" \
    --label com.docker.compose.volume=responses-ws-sse-bridge-spool "$volume" >/dev/null
  "$runtime" volume inspect "$volume" >"$work/volume"
fi
namespace=()
# Read mapped-owner path metadata without changing it on rootless Podman.
if [ "$backend" = podman ] && python3 -c 'import json,sys; sys.exit(not json.load(open(sys.argv[1]))["host"]["security"]["rootless"])' "$work/runtime"; then
  namespace=("$runtime" unshare)
fi
"${namespace[@]}" python3 - "$work/volume" "$volume" "$project" "$work/runtime" "$runtime" >"$work/mount" <<'PY'
import hashlib, json, os, stat, subprocess, sys
from pathlib import Path
volumes = json.load(open(sys.argv[1]))
if len(volumes) != 1:
    raise SystemExit('unexpected volume inspection')
v = volumes[0]
labels = v.get('Labels') or {}
if (v.get('Name') != sys.argv[2] or v.get('Driver') != 'local' or v.get('Options')
        or labels.get('com.docker.compose.project', sys.argv[3]) != sys.argv[3]
        or labels.get('com.docker.compose.volume', 'responses-ws-sse-bridge-spool') != 'responses-ws-sse-bridge-spool'):
    raise SystemExit('refusing unexpected volume driver/options/ownership labels')
info = json.load(open(sys.argv[4]))
if 'host' in info and info['host'].get('serviceIsRemote') is False:
    if v.get('NeedsChown') or v.get('UID') != 65534 or v.get('GID') != 65534:
        raise SystemExit('Podman legacy volume needs a manual initialization window; refusing automatic owner/mode reset')
    base = Path(info['store']['volumePath'])
elif info.get('DockerRootDir'):
    context = json.loads(subprocess.check_output([sys.argv[5], 'context', 'inspect']))
    endpoint = context[0]['Endpoints']['docker']['Host']
    if not os.environ.get('DOCKER_CONTEXT'):
        endpoint = os.environ.get('DOCKER_HOST') or endpoint
    if not endpoint.startswith('unix://'):
        raise SystemExit('preparation requires a local engine, not a remote context')
    base = Path(info['DockerRootDir']) / 'volumes'
else:
    raise SystemExit('cannot establish a supported local engine volume path')
mount = Path(v.get('Mountpoint', ''))
try:
    if (not mount.is_absolute() or any(c in str(mount) for c in ',\n')
            or mount != base / sys.argv[2] / '_data'
            or mount.resolve(strict=True) != mount):
        raise SystemExit('refusing unexpected or symlinked volume mountpoint')
    identity = mount.lstat()
except OSError as error:
    raise SystemExit('cannot safely inspect local volume path; run on the engine host with path access') from error
if not stat.S_ISDIR(identity.st_mode):
    raise SystemExit('volume mountpoint is not a directory')
print(mount)
print(f'{identity.st_dev}:{identity.st_ino}')
print('ai-gateway-prepare-bridge-spool-' + hashlib.sha256(sys.argv[2].encode()).hexdigest()[:16])
PY
readarray -t mount <"$work/mount"
# The exact bind avoids Podman's automatic named-volume chown before our checks.
# A deterministic helper name serializes this prerequisite, not unrelated Compose operations.
# shellcheck disable=SC2016 # These checks run inside the helper, not the host shell.
helper=$("$runtime" create --name "${mount[2]}" --network none --read-only --user 0:0 --cap-drop ALL \
  --cap-add CHOWN --cap-add FOWNER --cap-add DAC_OVERRIDE --security-opt no-new-privileges \
  --mount "type=bind,source=${mount[0]},target=/spool" --entrypoint /bin/sh "$image" -ec '
    [ "$(stat -c %d:%i /spool)" = "$1" ] || { echo "volume mountpoint identity changed" >&2; exit 1; }
    # Linux filesystem magic: ext2/3/4, XFS, Btrfs, ZFS; no RAM/network/unknown FS.
    case "$(stat -f -c %t /spool)" in
      ef53|58465342|9123683e|2fc12fc1) ;;
      *) echo "spool must use a supported local disk filesystem" >&2; exit 1;;
    esac
    entries=$(find /spool -mindepth 1 -maxdepth 1 -print -quit)
    [ -z "$entries" ] || { echo "bridge spool is nonempty; refusing ownership/mode changes" >&2; exit 1; }
    chown 65534:65534 /spool
    chmod 0700 /spool
    [ "$(stat -c %u:%g:%a /spool)" = 65534:65534:700 ]
  ' sh "${mount[1]}")
references=$("$runtime" ps -aq --no-trunc --filter "volume=$volume")
[ -z "$references" ] || { echo 'bridge spool acquired a container reference; refusing preparation' >&2; exit 1; }
"$runtime" start --attach "$helper" >/dev/null
"$runtime" rm "$helper" >/dev/null
helper=
printf 'bridge_spool_prepared=%s uid=65534 gid=65534 mode=0700\n' "$volume"
