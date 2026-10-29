#!/usr/bin/env bash
# Routing test for the catalog edges and the inner constant pipe:
# - front door: /models-table and its refresh reach model-catalog only on the
#   9083 operator listener, never on the public 9080 listener;
# - apisix-models: /healthz is local, GET /v1/models (with and without
#   client_version) reaches model-catalog, and everything else reaches AISIX
#   with the caller credential and a byte-identical body (including
#   prompt_cache_retention, multipart edits and a 64 MiB Responses body). SSE
#   arrives incrementally, 64 MiB + 1 gets 413, and nothing reaches CPA.
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
import hashlib, http.server, json, signal, sys, threading, time
signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))
NAMES = {8090: "model-catalog", 8317: "cpa", 8080: "sub2api", 3000: "aisix"}
hits = {name: 0 for name in NAMES.values()}
def handler(name):
    class H(http.server.BaseHTTPRequestHandler):
        def reply(self):
            if self.path == "/__hits":
                body = json.dumps(hits).encode()
            else:
                hits[name] += 1
                data = self.rfile.read(int(self.headers.get("Content-Length") or 0))
                try:
                    stream = json.loads(data).get("stream") is True
                except Exception:
                    stream = False
                if stream:
                    self.send_response(200)
                    self.send_header("Content-Type", "text/event-stream")
                    self.send_header("Connection", "close")
                    self.end_headers()
                    for index in range(3):
                        self.wfile.write(f"data: {name} {index}\n\n".encode())
                        self.wfile.flush()
                        time.sleep(0.6)
                    return
                body = json.dumps({
                    "upstream": name, "method": self.command, "path": self.path,
                    "auth": self.headers.get("Authorization"),
                    "type": self.headers.get("Content-Type"),
                    "bytes": len(data), "sha256": hashlib.sha256(data).hexdigest(),
                }).encode()
            self.send_response(200)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        do_GET = do_POST = do_HEAD = reply
        def log_message(self, *args):
            pass
    return H
servers = [http.server.ThreadingHTTPServer(("127.0.0.1", port), handler(name)) for port, name in NAMES.items()]
for server in servers[1:]:
    threading.Thread(target=server.serve_forever, daemon=True).start()
servers[0].serve_forever()
PY

cat >"$work/probe.py" <<'PY'
import json, sys, time, urllib.error, urllib.request
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
    upstream = json.loads(body)["upstream"] if status == 200 else str(status)
    ok = upstream == want
    failed += not ok
    print(("PASS" if ok else "FAIL"), method, port, path, "->", upstream, "want", want)
raise SystemExit(1 if failed else 0)
PY

cat >"$work/pipe.py" <<'PY'
import hashlib, http.client, json, sys, time
AUTH = "Bearer caller-fixture-key"
failed = 0
def check(label, ok, detail=""):
    global failed
    failed += not ok
    print("PASS" if ok else "FAIL", label, detail)
def send(method, path, body=None, ctype="application/json", auth=AUTH):
    conn = http.client.HTTPConnection("127.0.0.1", 9080, timeout=120)
    headers = {"Authorization": auth}
    if body is not None:
        headers["Content-Type"] = ctype
    conn.request(method, path, body=body, headers=headers)
    response = conn.getresponse()
    return conn, response
def forwarded(label, method, path, body=None, ctype="application/json", want="aisix"):
    conn, response = send(method, path, body, ctype)
    data = response.read()
    conn.close()
    if response.status != 200:
        check(label, False, f"status {response.status}")
        return
    got = json.loads(data)
    ok = got["upstream"] == want and got["path"] == path and got["method"] == method
    if want == "aisix":
        ok = ok and got["auth"] == AUTH
    if body is not None:
        ok = ok and got["sha256"] == hashlib.sha256(body).hexdigest() and got["type"] == ctype
    check(label, ok, f"-> {got['upstream']} {got['bytes']} bytes")
def hits():
    conn = http.client.HTTPConnection("127.0.0.1", 3000, timeout=5)
    conn.request("GET", "/__hits")
    value = json.loads(conn.getresponse().read())
    conn.close()
    return value
deadline = time.time() + 30
while True:
    try:
        before = hits()
        break
    except OSError:
        if time.time() > deadline:
            raise SystemExit("stub did not start")
        time.sleep(0.2)
for method in ("GET", "HEAD"):
    conn, response = send(method, "/healthz")
    body = response.read()
    conn.close()
    check(f"{method} /healthz is local", response.status == 200 and (method == "HEAD" or json.loads(body) == {"status": "ok"}))
check("/healthz reached no upstream", hits() == before, json.dumps(hits()))
forwarded("GET /v1/models", "GET", "/v1/models", want="model-catalog")
forwarded("GET /v1/models?client_version", "GET", "/v1/models?client_version=1.0.0", want="model-catalog")
retention = json.dumps({"model": "provider/x", "input": "hi", "prompt_cache_retention": "24h"}).encode()
for path in ("/v1/responses", "/v1/chat/completions", "/v1/images/generations", "/v1/embeddings"):
    forwarded(f"POST {path}", "POST", path, retention)
boundary = "fixture-boundary"
multipart = (f"--{boundary}\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\ngpt-image-2\r\n"
             f"--{boundary}\r\nContent-Disposition: form-data; name=\"image\"; filename=\"a.png\"\r\n"
             f"Content-Type: image/png\r\n\r\n").encode() + bytes(range(256)) + f"\r\n--{boundary}--\r\n".encode()
forwarded("POST /v1/images/edits multipart", "POST", "/v1/images/edits", multipart, f"multipart/form-data; boundary={boundary}")
forwarded("GET /models-table", "GET", "/models-table")
for path in ("/v1/responses", "/v1/chat/completions"):
    conn, response = send("POST", path, json.dumps({"model": "m", "stream": True}).encode())
    start, arrivals = time.time(), []
    while True:
        line = response.readline()
        if not line:
            break
        if line.startswith(b"data: "):
            arrivals.append(time.time() - start)
    conn.close()
    check(f"SSE {path} incremental", len(arrivals) == 3 and arrivals[0] < 0.5 and arrivals[2] - arrivals[0] > 1.0,
          " ".join(f"{t:.2f}s" for t in arrivals))
limit = 64 * 1024 * 1024
prefix = b'{"model":"m","input":"'
large = prefix + b"x" * (limit - len(prefix) - 2) + b'"}'
forwarded("POST /v1/responses 64 MiB", "POST", "/v1/responses", large)
conn, response = send("POST", "/v1/responses", large[:-2] + b'x"}')
response.read()
conn.close()
check("POST /v1/responses 64 MiB + 1 -> 413", response.status == 413, str(response.status))
check("no request reached CPA", hits()["cpa"] == 0, json.dumps(hits()))
raise SystemExit(1 if failed else 0)
PY

cat >"$work/health.py" <<'PY'
import http.client, json, time
# Runs before any stub exists: a /healthz that fell through to an upstream would get 502.
deadline = time.time() + 60
while True:
    try:
        conn = http.client.HTTPConnection("127.0.0.1", 9080, timeout=5)
        conn.request("GET", "/healthz")
        response = conn.getresponse()
        status, body = response.status, response.read()
        conn.close()
        if status != 404:
            break
    except OSError:
        pass
    if time.time() > deadline:
        raise SystemExit("APISIX did not start")
    time.sleep(0.5)
ok = status == 200 and json.loads(body) == {"status": "ok"}
print("PASS" if ok else "FAIL", "GET /healthz with every upstream down ->", status)
raise SystemExit(0 if ok else 1)
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
cp "$root/services/routing/apisix-models/apisix.yaml" "$work/apisix-models.yaml"
chmod -R a+rX "$work"

# run_plane NAME ROUTES PROBE [VOLUME...] -- [PROBE ARG...]
run_plane() {
  local name=$1 routes=$2 probe=$3
  shift 3
  local volumes=()
  while [ "$1" != -- ]; do
    volumes+=(-v "$1")
    shift
  done
  shift
  "${RUNTIME[@]}" run -d --name "$plane" --network none \
    --add-host model-catalog:127.0.0.1 --add-host cpa:127.0.0.1 --add-host aisix:127.0.0.1 \
    --add-host apisix-models:127.0.0.1 --add-host ai-sse-keepalive-ingress-relay:127.0.0.1 \
    -e APISIX_STAND_ALONE=true \
    -v "$work/$name.config.yaml:/usr/local/apisix/conf/config.yaml:ro" \
    -v "$routes:/usr/local/apisix/conf/apisix.yaml:ro" \
    "${volumes[@]}" \
    "$apisix_image" >/dev/null
  if [ "$name" = apisix-models ]; then
    "${RUNTIME[@]}" run --rm --network "container:$plane" \
      -v "$work/health.py:/probe.py:ro" "$python_image" python3 /probe.py
  fi
  "${RUNTIME[@]}" run -d --name "$plane-stub" --network "container:$plane" \
    -v "$work/stub.py:/stub.py:ro" "$python_image" python3 /stub.py >/dev/null
  "${RUNTIME[@]}" run --rm --network "container:$plane" \
    -v "$work/$probe:/probe.py:ro" "$python_image" python3 /probe.py "$@"
  remove_plane
}

run_plane apisix "$root/services/firewall/apisix/apisix.yaml" probe.py \
  "$root/services/firewall/apisix/lua:/opt/apisix/custom:ro" -- \
  "GET 9083 /models-table model-catalog" \
  "POST 9083 /models-table/refresh model-catalog" \
  "GET 9083 /models-table/refresh 404" \
  "GET 9080 /models-table 404" \
  "POST 9080 /models-table/refresh 404"

run_plane apisix-models "$work/apisix-models.yaml" pipe.py --

echo "catalog_routes=valid"
