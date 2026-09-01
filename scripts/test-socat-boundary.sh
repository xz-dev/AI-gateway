#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=scripts/container-runtime.sh
source "$root/scripts/container-runtime.sh"
RUNTIME=("$AI_GATEWAY_RUNTIME")

image=${1:-docker.io/alpine/socat:1.8.1.3}
python_image=docker.io/library/python:3.13.15-alpine3.24
suffix=$$
source_net=ai-socat-source-$suffix
target_net=ai-socat-target-$suffix
source=ai-socat-source-$suffix
target=ai-socat-target-$suffix
relay=ai-socat-relay-$suffix

cleanup() {
  local status=$? item
  for item in "$source" "$target" "$relay"; do "${RUNTIME[@]}" rm -f "$item" >/dev/null 2>&1 || true; done
  for item in "$source_net" "$target_net"; do "${RUNTIME[@]}" network rm "$item" >/dev/null 2>&1 || true; done
  exit "$status"
}
trap cleanup EXIT HUP INT TERM

"${RUNTIME[@]}" image inspect "$image" >/dev/null 2>&1 || "${RUNTIME[@]}" pull "$image" >/dev/null
"${RUNTIME[@]}" image inspect "$python_image" >/dev/null 2>&1 || "${RUNTIME[@]}" pull "$python_image" >/dev/null
"${RUNTIME[@]}" network create --internal --subnet 172.30.250.0/29 "$source_net" >/dev/null
"${RUNTIME[@]}" network create --internal --subnet 172.30.251.0/29 "$target_net" >/dev/null

server='import socket
s=socket.socket(); s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1); s.bind(("0.0.0.0",int(__import__("sys").argv[1]))); s.listen()
while True:
 c,_=s.accept()
 with c:
  while data:=c.recv(4096): c.sendall(data)'
"${RUNTIME[@]}" run -d --name "$target" --network "$target_net" --ip 172.30.251.2 \
  "$python_image" python3 -u -c "$server" 18080 >/dev/null
"${RUNTIME[@]}" run -d --name "$source" --network "$source_net" --ip 172.30.250.2 \
  "$python_image" python3 -u -c "$server" 18082 >/dev/null
"${RUNTIME[@]}" run -d --name "$relay" --network "$source_net" --ip 172.30.250.3 \
  --user 65534:65534 --read-only --cap-drop ALL --security-opt no-new-privileges \
  --pids-limit 64 --memory 32m --memory-swap 32m --cpus .2 \
  "$image" TCP4-LISTEN:18080,bind=172.30.250.3,reuseaddr,fork TCP4:172.30.251.2:18080,connect-timeout=5 >/dev/null
"${RUNTIME[@]}" network connect --ip 172.30.251.3 "$target_net" "$relay"

ready=
for _ in $(seq 1 40); do
  if "${RUNTIME[@]}" exec "$source" python3 -c 'import socket;s=socket.create_connection(("172.30.250.3",18080),1);s.sendall(b"ok");assert s.recv(2)==b"ok"' >/dev/null 2>&1; then
    ready=1; break
  fi
  sleep .1
done
[ "$ready" = 1 ] || { echo 'forward relay did not become ready' >&2; exit 1; }

source_listener=$(printf live | "${RUNTIME[@]}" exec -i "$relay" socat - TCP4:172.30.250.2:18082 2>/dev/null)
[ "$source_listener" = live ] || { echo 'source listener is not live' >&2; exit 1; }
if "${RUNTIME[@]}" exec "$target" python3 -c 'import socket;socket.create_connection(("172.30.250.2",18082),1)' >/dev/null 2>&1; then
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
if "${RUNTIME[@]}" exec "$source" python3 -c 'import socket;socket.create_connection(("172.30.251.2",18080),1)' >/dev/null 2>&1; then
  echo 'source unexpectedly bypassed stopped relay' >&2; exit 1
fi

echo 'socat_tcp=valid reverse_initiation=blocked relay_bypass=blocked privileges=dropped pairwise_networks=valid'
