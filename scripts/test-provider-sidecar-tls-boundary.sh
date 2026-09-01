#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=scripts/container-runtime.sh
source "$root/scripts/container-runtime.sh"
RUNTIME=("$AI_GATEWAY_RUNTIME")

image=${1:-docker.io/alpine/socat:1.8.1.3}
suffix=$$
source_net=ai-provider-sidecar-tls-source-$suffix
target_net=ai-provider-sidecar-tls-target-$suffix
source=ai-provider-sidecar-tls-source-$suffix
target=ai-provider-sidecar-tls-target-$suffix
relay=ai-provider-sidecar-tls-relay-$suffix
tmpdir=$(mktemp -d /tmp/ai-gateway-provider-sidecar-tls-test.XXXXXX)

cleanup() {
  local status=$? item
  if [ "$status" -ne 0 ] && "${RUNTIME[@]}" inspect "$relay" >/dev/null 2>&1; then
    "${RUNTIME[@]}" logs --tail 20 "$relay" >&2 || true
  fi
  for item in "$source" "$target" "$relay"; do "${RUNTIME[@]}" rm -f "$item" >/dev/null 2>&1 || true; done
  for item in "$source_net" "$target_net"; do "${RUNTIME[@]}" network rm "$item" >/dev/null 2>&1 || true; done
  rm -rf -- "$tmpdir"
  exit "$status"
}
trap cleanup EXIT HUP INT TERM

command -v openssl >/dev/null || { echo 'openssl is required' >&2; exit 1; }
"${RUNTIME[@]}" image inspect "$image" >/dev/null 2>&1 || "${RUNTIME[@]}" pull "$image" >/dev/null

mkdir -m 700 "$tmpdir/egress"
openssl req -x509 -newkey rsa:2048 -nodes -days 365 -subj /CN=test-egress-ca \
  -addext 'basicConstraints=critical,CA:TRUE,pathlen:0' \
  -addext 'keyUsage=critical,keyCertSign,cRLSign' \
  -keyout "$tmpdir/egress/ca.key" -out "$tmpdir/egress/ca.crt" >/dev/null 2>&1
cp "$tmpdir/egress/ca.crt" "$tmpdir/egress/ca-bundle.pem"
chmod 600 "$tmpdir/egress/ca.key"
chmod 444 "$tmpdir/egress/ca.crt" "$tmpdir/egress/ca-bundle.pem"

run_init() {
  PROVIDER_SIDECAR_TLS_DIR=$tmpdir/tls \
  PROVIDER_SIDECAR_EGRESS_CA_CERT=$tmpdir/egress/ca.crt \
  PROVIDER_SIDECAR_EGRESS_CA_BUNDLE=$tmpdir/egress/ca-bundle.pem \
    "$root/scripts/init-provider-sidecar-tls.sh" "$@"
}
run_init >/dev/null
run_init --check >/dev/null

openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=wrong-ca \
  -keyout "$tmpdir/wrong.key" -out "$tmpdir/wrong.crt" >/dev/null 2>&1
chmod 444 "$tmpdir/wrong.crt"

"${RUNTIME[@]}" network create --internal --subnet 172.30.250.0/29 "$source_net" >/dev/null
"${RUNTIME[@]}" network create --internal --subnet 172.30.251.0/29 "$target_net" >/dev/null

"${RUNTIME[@]}" run -d --name "$target" --network "$target_net" --ip 172.30.251.2 \
  --user 65534:65534 --read-only --cap-drop ALL --security-opt no-new-privileges \
  "$image" TCP4-LISTEN:8080,bind=172.30.251.2,reuseaddr,fork EXEC:/bin/cat >/dev/null

"${RUNTIME[@]}" run -d --name "$source" --network "$source_net" --ip 172.30.250.2 \
  --user 65534:65534 --read-only --cap-drop ALL --security-opt no-new-privileges \
  -v "$tmpdir/tls/ca.crt:/tls/ca.crt:ro" -v "$tmpdir/wrong.crt:/tls/wrong.crt:ro" \
  --entrypoint /bin/sh "$image" -c 'exec socat TCP4-LISTEN:8081,bind=172.30.250.2,reuseaddr,fork EXEC:/bin/cat' >/dev/null

"${RUNTIME[@]}" run -d --name "$relay" --network "$source_net" --ip 172.30.250.3 \
  --user 65534:65534 --read-only --cap-drop ALL --security-opt no-new-privileges \
  --pids-limit 64 --memory 32m --memory-swap 32m --cpus .2 \
  -v "$tmpdir/tls/server.crt:/run/provider-sidecar-tls/server.crt:ro" \
  -v "$tmpdir/tls/server.key:/run/provider-sidecar-tls/server.key:ro" \
  "$image" \
  'OPENSSL-LISTEN:8080,bind=172.30.250.3,reuseaddr,fork,cert=/run/provider-sidecar-tls/server.crt,key=/run/provider-sidecar-tls/server.key,verify=0,openssl-min-proto-version=TLS1.2' \
  'TCP4:172.30.251.2:8080,connect-timeout=10' >/dev/null
"${RUNTIME[@]}" network connect --ip 172.30.251.3 "$target_net" "$relay"

ready=
for _ in $(seq 1 50); do
  if result=$(printf 'trusted-forwarding' | "${RUNTIME[@]}" exec -i "$source" socat - \
    OPENSSL-CONNECT:172.30.250.3:8080,cafile=/tls/ca.crt,verify=1,snihost=provider-sidecar,commonname=provider-sidecar,openssl-min-proto-version=TLS1.2 2>/dev/null) && \
    [ "$result" = trusted-forwarding ]; then ready=1; break; fi
  sleep .1
done
[ "$ready" = 1 ] || { echo 'trusted TLS forwarding did not become ready' >&2; exit 1; }

if printf x | "${RUNTIME[@]}" exec -i "$source" socat - \
  OPENSSL-CONNECT:172.30.250.3:8080,cafile=/tls/wrong.crt,verify=1,snihost=provider-sidecar,commonname=provider-sidecar >/dev/null 2>&1; then
  echo 'wrong CA unexpectedly trusted' >&2; exit 1
fi
if printf x | "${RUNTIME[@]}" exec -i "$source" socat - \
  OPENSSL-CONNECT:172.30.250.3:8080,cafile=/tls/ca.crt,verify=1,snihost=wrong.invalid,commonname=wrong.invalid >/dev/null 2>&1; then
  echo 'wrong server name unexpectedly trusted' >&2; exit 1
fi
plaintext=$(printf plaintext | "${RUNTIME[@]}" exec -i "$source" socat - TCP4:172.30.250.3:8080 2>/dev/null || true)
[ "$plaintext" != plaintext ] || { echo 'plaintext unexpectedly crossed TLS listener' >&2; exit 1; }

source_listener=$(printf live | "${RUNTIME[@]}" exec -i "$relay" socat - TCP4:172.30.250.2:8081 2>/dev/null)
[ "$source_listener" = live ] || { echo 'source listener is not live' >&2; exit 1; }
if "${RUNTIME[@]}" exec "$target" socat -T1 - TCP4:172.30.250.2:8081 >/dev/null 2>&1; then
  echo 'target unexpectedly initiated a connection to source' >&2; exit 1
fi
uid=$("${RUNTIME[@]}" exec "$relay" id -u)
# shellcheck disable=SC2016
cap=$("${RUNTIME[@]}" exec "$relay" awk '/^CapEff:/{print $2}' /proc/1/status)
readonly=$("${RUNTIME[@]}" inspect "$relay" --format '{{.HostConfig.ReadonlyRootfs}}')
[ "$uid:$cap:$readonly" = '65534:0000000000000000:true' ] || {
  echo "relay hardening mismatch: $uid:$cap:$readonly" >&2; exit 1;
}
[ "$("${RUNTIME[@]}" network inspect "$source_net" --format '{{len .Containers}}')" = 2 ]
[ "$("${RUNTIME[@]}" network inspect "$target_net" --format '{{len .Containers}}')" = 2 ]

"${RUNTIME[@]}" stop -t 2 "$relay" >/dev/null
if printf x | "${RUNTIME[@]}" exec -i "$source" socat -T1 - TCP4:172.30.251.2:8080 >/dev/null 2>&1; then
  echo 'source unexpectedly bypassed stopped relay' >&2; exit 1
fi

echo 'provider_sidecar_tls=valid trusted_forwarding=valid wrong_ca=blocked wrong_name=blocked plaintext=blocked reverse_initiation=blocked relay_bypass=blocked privileges=dropped'
