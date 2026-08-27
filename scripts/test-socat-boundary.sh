#!/usr/bin/env bash
set -euo pipefail

image=${1:-docker.io/alpine/socat@sha256:3d9e7966201dd3a065df591020a09fd3c70845de7e7086e3531ea69db774406b}
suffix=$$
source_net=ai-socat-source-$suffix
target_net=ai-socat-target-$suffix
source=ai-socat-source-$suffix
target=ai-socat-target-$suffix
relay=ai-socat-relay-$suffix
supported_connections=256

cleanup() {
  local status=$? item
  if [ "$status" -ne 0 ] && docker inspect "$relay" >/dev/null 2>&1; then
    docker inspect "$relay" --format 'relay_state={{.State.Status}} exit={{.State.ExitCode}} oom={{.State.OOMKilled}}' >&2 || true
    docker logs --tail 20 "$relay" >&2 || true
  fi
  for item in "$source" "$target" "$relay"; do docker rm -f "$item" >/dev/null 2>&1 || true; done
  for item in "$source_net" "$target_net"; do docker network rm "$item" >/dev/null 2>&1 || true; done
  exit "$status"
}
trap cleanup EXIT HUP INT TERM

for command in docker python3; do command -v "$command" >/dev/null || { echo "$command is required" >&2; exit 1; }; done
docker image inspect "$image" >/dev/null 2>&1 || docker pull "$image" >/dev/null

docker network create --internal --subnet 172.30.250.0/29 "$source_net" >/dev/null
docker network create --internal --subnet 172.30.251.0/29 "$target_net" >/dev/null
docker run -d --name "$target" --network "$target_net" --ip 172.30.251.2 \
  docker.io/library/python@sha256:540c7d91f98ff6880174c40e99067bf5941eb54d818a7a5e094d188b196a934d \
  python3 -u -c 'import socket,threading

def echo(c):
 try:
  while data:=c.recv(4096): c.sendall(data)
 finally: c.close()

def serve(kind, sock):
 while True:
  if kind=="tcp":
   c,_=sock.accept(); threading.Thread(target=echo,args=(c,),daemon=True).start()
  else:
   data,addr=sock.recvfrom(4096); sock.sendto(data,addr)
t=socket.socket(); t.bind(("172.30.251.2",18080)); t.listen()
u=socket.socket(socket.AF_INET,socket.SOCK_DGRAM); u.bind(("172.30.251.2",18081))
threading.Thread(target=serve,args=("tcp",t),daemon=True).start(); serve("udp",u)' >/dev/null

docker run -d --name "$source" --network "$source_net" --ip 172.30.250.2 \
  docker.io/library/python@sha256:540c7d91f98ff6880174c40e99067bf5941eb54d818a7a5e094d188b196a934d sleep infinity >/dev/null

docker run -d --name "$relay" --network "$source_net" --ip 172.30.250.3 \
  --user 65534:65534 --read-only --cap-drop ALL --security-opt no-new-privileges \
  --pids-limit 512 --memory 64m --memory-swap 64m --cpus .2 \
  --entrypoint /bin/sh "$image" -ec '
    socat TCP4-LISTEN:18080,bind=172.30.250.3,reuseaddr,fork TCP4:172.30.251.2:18080,connect-timeout=5 & p1=$!
    socat UDP4-LISTEN:18081,bind=172.30.250.3,reuseaddr,fork UDP4:172.30.251.2:18081 & p2=$!
    trap "kill $p1 $p2 2>/dev/null; wait $p1 $p2 2>/dev/null" EXIT INT TERM
    wait -n
  ' >/dev/null
docker network connect --ip 172.30.251.3 "$target_net" "$relay"

ready=
for _ in $(seq 1 40); do
  if docker exec "$source" python3 -c 'import socket;s=socket.create_connection(("172.30.250.3",18080),1);s.sendall(b"tcp-ok");assert s.recv(6)==b"tcp-ok"' >/dev/null 2>&1; then ready=1; break; fi
  sleep .1
done
[ "$ready" = 1 ] || { echo 'TCP relay did not become ready' >&2; exit 1; }
docker exec "$source" python3 -c 'import socket;s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);s.settimeout(2);s.sendto(b"udp-ok",("172.30.250.3",18081));assert s.recv(6)==b"udp-ok"'
docker exec -i "$source" python3 - "$supported_connections" <<'PY'
import socket,sys
sockets=[]
try:
 for i in range(int(sys.argv[1])):
  s=socket.create_connection(("172.30.250.3",18080),2)
  payload=i.to_bytes(4,"big")
  s.sendall(payload)
  assert s.recv(4)==payload
  sockets.append(s)
finally:
 for s in sockets: s.close()
PY
[ "$(docker inspect "$relay" --format '{{.State.Running}}')" = true ] || {
  echo "relay did not sustain $supported_connections concurrent connections" >&2; exit 1;
}
if docker exec "$target" python3 -c 'import socket;socket.create_connection(("172.30.250.2",18080),1)' >/dev/null 2>&1; then
  echo 'target unexpectedly initiated a connection to source' >&2; exit 1
fi
uid=$(docker exec "$relay" id -u)
cap=$(docker exec "$relay" awk '/^CapEff:/{print $2}' /proc/1/status)
readonly=$(docker inspect "$relay" --format '{{.HostConfig.ReadonlyRootfs}}')
[ "$uid:$cap:$readonly" = '65534:0000000000000000:true' ] || { echo "relay hardening mismatch: $uid:$cap:$readonly" >&2; exit 1; }
source_members=$(docker network inspect "$source_net" --format '{{len .Containers}}')
target_members=$(docker network inspect "$target_net" --format '{{len .Containers}}')
[ "$source_members:$target_members" = '2:2' ] || { echo "network member mismatch: $source_members:$target_members" >&2; exit 1; }
echo "socat_tcp=valid socat_udp=valid concurrent=$supported_connections reverse_initiation=blocked privileges=dropped pairwise_networks=valid"
