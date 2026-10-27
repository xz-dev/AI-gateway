#!/usr/bin/env bash
# Routing test for the catalog edges (restructure-services-layout D3):
# - front door: /models-table and its refresh reach model-catalog only on the
#   9083 operator listener, never on the public 9080 listener;
# - apisix-models: /v1/models (with and without client_version) reaches
#   model-catalog, the models table is never forwarded to it.
# Each plane runs with --network none; stub upstreams share its loopback.
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
# shellcheck source=ops/scripts/container-runtime.sh
source "$root/ops/scripts/container-runtime.sh"
RUNTIME=("$AI_GATEWAY_RUNTIME")

apisix_image=${1:-docker.io/apache/apisix:3.19.0-debian}
python_image=docker.io/library/python:3.13.15-alpine3.24
plane=ai-catalog-routes-$$
work=$(mktemp -d /var/tmp/ai-catalog-routes.XXXXXX)

# The stub shares the plane's namespace, so remove it first.
remove_plane() {
  "${RUNTIME[@]}" rm -f "$plane-stub" >/dev/null 2>&1 || true
  "${RUNTIME[@]}" rm -f "$plane" >/dev/null 2>&1 || true
}

cleanup() {
  local status=$?
  remove_plane
  rm -rf "$work"
  exit "$status"
}
trap cleanup EXIT HUP INT TERM

"${RUNTIME[@]}" image inspect "$python_image" >/dev/null 2>&1 || "${RUNTIME[@]}" pull "$python_image" >/dev/null

cat >"$work/stub.py" <<'PY'
import http.server, signal, sys, threading
signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))
NAMES = {8090: "model-catalog", 8317: "cpa", 8080: "sub2api"}
def handler(name):
    class H(http.server.BaseHTTPRequestHandler):
        def reply(self):
            body = f"{name} {self.command} {self.path}".encode()
            self.send_response(200)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        do_GET = do_POST = reply
        def log_message(self, *args):
            pass
    return H
servers = [http.server.ThreadingHTTPServer(("127.0.0.1", port), handler(name)) for port, name in NAMES.items()]
for server in servers[1:]:
    threading.Thread(target=server.serve_forever, daemon=True).start()
servers[0].serve_forever()
PY

cat >"$work/probe.py" <<'PY'
import sys, time, urllib.error, urllib.request
def fetch(method, port, path):
    request = urllib.request.Request(f"http://127.0.0.1:{port}{path}", method=method, data=b"" if method == "POST" else None)
    try:
        with urllib.request.urlopen(request, timeout=5) as response:
            return response.status, response.read().decode()
    except urllib.error.HTTPError as error:
        return error.code, ""
deadline = time.time() + 60
while True:
    try:
        fetch("GET", 9080, "/healthz")
        break
    except OSError:
        if time.time() > deadline:
            raise SystemExit("APISIX did not start")
        time.sleep(0.5)
failed = 0
for case in sys.argv[1:]:
    method, port, path, want = case.split(" ", 3)
    status, body = fetch(method, int(port), path)
    upstream = body.split(" ", 1)[0] if status == 200 else str(status)
    ok = upstream == want
    failed += not ok
    print(("PASS" if ok else "FAIL"), method, port, path, "->", upstream, "want", want)
raise SystemExit(1 if failed else 0)
PY

# Rewrite relay-target listeners to loopback; keep each plane's ports.
python3 - "$root" "$work" <<'PY'
import re, sys
from pathlib import Path
root, work = Path(sys.argv[1]), Path(sys.argv[2])
for plane in ("services/firewall/apisix", "services/routing/apisix-models"):
    text = (root / plane / "config.yaml").read_text(encoding="utf-8")
    ports = sorted(set(re.findall(r"(?m)^\s+port: (\d+)$", text)))
    listen = "".join(f"    - ip: 127.0.0.1\n      port: {port}\n" for port in ports)
    text = re.sub(r"(?m)^  node_listen:\n(?:    .*\n)+", "  node_listen:\n" + listen, text, count=1)
    (work / (plane.rsplit("/", 1)[1] + ".config.yaml")).write_text(text, encoding="utf-8")
PY
sed 's|__CPA_API_KEY__|fixture-cpa-key|g' "$root/services/routing/apisix-models/apisix.yaml" >"$work/apisix-models.yaml"
chmod -R a+rX "$work"

run_plane() {
  local name=$1 routes=$2 lua_mount=$3
  shift 3
  "${RUNTIME[@]}" run -d --name "$plane" --network none \
    --add-host model-catalog:127.0.0.1 --add-host cpa:127.0.0.1 --add-host aisix:127.0.0.1 \
    --add-host apisix-models:127.0.0.1 --add-host ai-sse-keepalive-ingress-relay:127.0.0.1 \
    -e APISIX_STAND_ALONE=true \
    -v "$work/$name.config.yaml:/usr/local/apisix/conf/config.yaml:ro" \
    -v "$routes:/usr/local/apisix/conf/apisix.yaml:ro" \
    -v "$lua_mount" \
    "$apisix_image" >/dev/null
  "${RUNTIME[@]}" run -d --name "$plane-stub" --network "container:$plane" \
    -v "$work/stub.py:/stub.py:ro" "$python_image" python3 /stub.py >/dev/null
  "${RUNTIME[@]}" run --rm --network "container:$plane" \
    -v "$work/probe.py:/probe.py:ro" "$python_image" python3 /probe.py "$@"
  remove_plane
}

run_plane apisix "$root/services/firewall/apisix/apisix.yaml" "$root/services/firewall/apisix/lua:/opt/apisix/custom:ro" \
  "GET 9083 /models-table model-catalog" \
  "POST 9083 /models-table/refresh model-catalog" \
  "GET 9083 /models-table/refresh 404" \
  "GET 9080 /models-table 404" \
  "POST 9080 /models-table/refresh 404"

run_plane apisix-models "$work/apisix-models.yaml" \
  "$root/services/routing/apisix-models/routing_selector.lua:/opt/apisix/custom/routing_selector.lua:ro" \
  "GET 9080 /v1/models model-catalog" \
  "GET 9080 /v1/models?client_version=1.0.0 model-catalog" \
  "GET 9080 /models-table cpa" \
  "POST 9080 /models-table/refresh cpa"

echo "catalog_routes=valid"
