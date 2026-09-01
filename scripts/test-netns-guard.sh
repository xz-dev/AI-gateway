#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=scripts/container-runtime.sh
source "$root/scripts/container-runtime.sh"
RUNTIME=("$AI_GATEWAY_RUNTIME")
COMPOSE=("${AI_GATEWAY_COMPOSE[@]}")

cd "$root"
guard_image=${1:-docker.io/library/alpine:3.22.5}
python_image=docker.io/library/python:3.13.15-alpine3.24
suffix=$$
project=ai-netns-test-$suffix
internal_network=$project-internal
host_network=$project-host
tmpdir=$(mktemp -d /tmp/ai-gateway-netns-test.XXXXXX)
compose=$tmpdir/compose.yaml

cleanup() {
  "${COMPOSE[@]}" -p "$project" -f "$compose" down -v --remove-orphans >/dev/null 2>&1 || true
  for service in app netns; do
    container=$("${RUNTIME[@]}" ps -aq \
      --filter "label=com.docker.compose.project=$project" \
      --filter "label=com.docker.compose.service=$service")
    [ -z "$container" ] || "${RUNTIME[@]}" rm -f "$container" >/dev/null 2>&1 || true
  done
  "${RUNTIME[@]}" network rm "$internal_network" "$host_network" >/dev/null 2>&1 || true
  rm -rf -- "$tmpdir"
}
trap cleanup EXIT

for command in "${RUNTIME[@]}" python3 timeout; do
  command -v "$command" >/dev/null || { echo "$command is required" >&2; exit 1; }
done
"${RUNTIME[@]}" image inspect "$guard_image" >/dev/null 2>&1 || "${RUNTIME[@]}" pull "$guard_image" >/dev/null
"${RUNTIME[@]}" image inspect "$python_image" >/dev/null 2>&1 || "${RUNTIME[@]}" pull "$python_image" >/dev/null
port=$(python3 - <<'PY'
import socket
with socket.socket() as sock:
    sock.bind(("127.0.0.1", 0))
    print(sock.getsockname()[1])
PY
)

cat >"$tmpdir/server.py" <<'PY'
from http.server import BaseHTTPRequestHandler, HTTPServer
class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.send_header("Content-Length", "2")
        self.end_headers()
        self.wfile.write(b"ok")
    def log_message(self, *_):
        pass
HTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
PY

cat >"$compose" <<YAML
services:
  netns:
    image: $guard_image
    restart: unless-stopped
    read_only: true
    cap_drop: [ALL]
    cap_add: [NET_ADMIN, SETGID, SETUID]
    security_opt: [no-new-privileges:true]
    command:
      - /bin/sh
      - -ec
      - >-
        while ip -4 route show default | grep -q .; do ip -4 route del default; done;
        while ip -6 route show default | grep -q .; do ip -6 route del default; done;
        test -z "\$\$(ip -4 route show default)";
        test -z "\$\$(ip -6 route show default)";
        exec su nobody -s /bin/sh -c 'exec sleep infinity'
    ports:
      - 127.0.0.1:$port:8080
    networks:
      internal:
        aliases: [guarded-app]
      host-access:
  app:
    image: $python_image
    network_mode: service:netns
    depends_on:
      netns:
        condition: service_started
        restart: true
    read_only: true
    cap_drop: [ALL]
    security_opt: [no-new-privileges:true]
    volumes:
      - $tmpdir/server.py:/server.py:ro
    entrypoint: [/bin/sh, -ec]
    command:
      - >-
        while awk 'NR > 1 && \$\$2 == "00000000" && \$\$8 == "00000000" {found=1} END {exit(found ? 0 : 1)}' /proc/net/route ||
        awk '\$\$1 == "00000000000000000000000000000000" && \$\$2 == "00" && \$\$10 != "lo" {found=1} END {exit(found ? 0 : 1)}' /proc/net/ipv6_route;
        do sleep 0.05; done;
        exec python3 /server.py
networks:
  internal:
    name: $internal_network
    internal: true
  host-access:
    name: $host_network
YAML

"${COMPOSE[@]}" -p "$project" -f "$compose" config --quiet
"${COMPOSE[@]}" -p "$project" -f "$compose" up -d app >/dev/null
owner=$("${RUNTIME[@]}" ps -q --filter "label=com.docker.compose.project=$project" --filter label=com.docker.compose.service=netns)
app=$("${RUNTIME[@]}" ps -q --filter "label=com.docker.compose.project=$project" --filter label=com.docker.compose.service=app)
if [ -z "$owner" ] || [ -z "$app" ]; then
  echo 'namespace guard services did not start' >&2
  exit 1
fi

python3 - "$port" <<'PY'
import sys, time, urllib.request
url = f"http://127.0.0.1:{sys.argv[1]}/"
for _ in range(80):
    try:
        if urllib.request.urlopen(url, timeout=1).read() == b"ok":
            break
    except OSError:
        time.sleep(0.1)
else:
    raise SystemExit("guarded host publication did not become ready")
PY

[ -z "$("${RUNTIME[@]}" exec "$owner" ip -4 route show default)" ] || { echo 'namespace owner retained an IPv4 default route' >&2; exit 1; }
[ "$("${RUNTIME[@]}" exec "$owner" sh -c "awk '/^CapEff:/ {print \$2}' /proc/1/status")" = 0000000000000000 ] || {
  echo 'namespace owner did not drop effective capabilities' >&2
  exit 1
}
[ -z "$("${RUNTIME[@]}" exec "$app" sh -c "awk 'NR > 1 && \$2 == \"00000000\" && \$8 == \"00000000\" {print}' /proc/net/route")" ] || {
  echo 'application started before namespace hardening' >&2
  exit 1
}
internal=$("${RUNTIME[@]}" run --rm --network "$internal_network" "$python_image" python3 -c \
  'import urllib.request; print(urllib.request.urlopen("http://guarded-app:8080", timeout=3).read().decode())')
[ "$internal" = ok ] || { echo 'pairwise internal path failed' >&2; exit 1; }
if timeout 5 "${RUNTIME[@]}" exec "$app" python3 -c 'import socket; socket.create_connection(("1.1.1.1", 443), 3)' >/dev/null 2>&1; then
  echo 'guarded application retained direct Internet egress' >&2
  exit 1
fi

echo 'namespace_guard=valid host_publication=valid direct_egress=blocked privileges=dropped'
