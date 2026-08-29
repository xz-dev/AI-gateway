#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=scripts/container-runtime.sh
source "$root/scripts/container-runtime.sh"
RUNTIME=("$AI_GATEWAY_RUNTIME")

cd "$root"
image=${1:-ai-gateway-squid:6.13-2-deb13u2}
tun2proxy_image=${2:-ghcr.io/tun2proxy/tun2proxy@sha256:562a4208ecf1f53e3c790af512bcc1ce2656f1d10d3541614173eed8b3185708}
tun_test_mode=${AI_GATEWAY_TUN_TEST_MODE:-exact-capabilities}
case "$tun_test_mode" in exact-capabilities|ci-privileged) ;; *) echo 'AI_GATEWAY_TUN_TEST_MODE must be exact-capabilities or ci-privileged' >&2; exit 1;; esac
tmpdir=$(mktemp -d /tmp/ai-gateway-egress-test.XXXXXX)
suffix=$$
proxy=ai-egress-test-proxy-$suffix
dns=ai-egress-test-dns-$suffix
tls=ai-egress-test-tls-$suffix
tunnel=ai-egress-test-tunnel-$suffix
tunnel_sibling=ai-egress-test-sibling-$suffix
client_network=ai-egress-test-client-$suffix
egress_network=ai-egress-test-out-$suffix
python_image=docker.io/library/python@sha256:540c7d91f98ff6880174c40e99067bf5941eb54d818a7a5e094d188b196a934d

cleanup() {
  local status=$? container network
  if [ "$status" -ne 0 ]; then
    "${RUNTIME[@]}" logs "$proxy" 2>&1 || true
    "${RUNTIME[@]}" logs "$tunnel" 2>&1 || true
  fi
  for container in "$tunnel_sibling" "$tunnel" "$proxy" "$dns" "$tls"; do
    "${RUNTIME[@]}" rm -f "$container" >/dev/null 2>&1 || true
  done
  for network in "$client_network" "$egress_network"; do
    "${RUNTIME[@]}" network rm "$network" >/dev/null 2>&1 || true
  done
  rm -rf -- "$tmpdir"
}
trap cleanup EXIT

for command in "${RUNTIME[@]}" openssl python3 timeout; do
  command -v "$command" >/dev/null || { echo "$command is required" >&2; exit 1; }
done
"${RUNTIME[@]}" image inspect "$image" >/dev/null 2>&1 || "${RUNTIME[@]}" build --quiet --tag "$image" egress-proxy >/dev/null
"${RUNTIME[@]}" image inspect "$tun2proxy_image" >/dev/null 2>&1 || "${RUNTIME[@]}" pull "$tun2proxy_image" >/dev/null
[ -c /dev/net/tun ] || { echo '/dev/net/tun is required for namespace egress validation' >&2; exit 1; }

cat >"$tmpdir/policy.json" <<'JSON'
{"services":{"test":{"listen":"172.29.0.3:3128","client":"172.29.0.2/32","destinations":[
  {"domain":"global.filter.example.net","tls":"splice"},
  {"domain":"bump.filter.example.net","tls":"bump","methods":["GET"],"paths":["^/allowed$"]},
  {"domain":"mixed.filter.example.net","tls":"splice"},
  {"domain":"rebind.filter.example.net","tls":"splice"},
  {"domain":"private.filter.example.net","tls":"splice"},
  {"domain":"mapped.filter.example.net","tls":"splice"},
  {"domain":"six.filter.example.net","tls":"splice"}
]}}}
JSON
python3 scripts/render-egress-policy.py "$tmpdir/policy.json" "$tmpdir/generated"
cp egress-proxy/unbound.conf "$tmpdir/unbound.conf"
cat >>"$tmpdir/unbound.conf" <<'CONF'

forward-zone:
  name: "filter.example.net."
  forward-addr: 203.1.1.3
CONF
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=egress-test-origin-ca \
  -addext 'basicConstraints=critical,CA:TRUE' \
  -addext 'keyUsage=critical,keyCertSign,cRLSign' \
  -keyout "$tmpdir/origin-ca.key" -out "$tmpdir/origin-ca.crt" >/dev/null 2>&1
openssl req -new -newkey rsa:2048 -nodes -subj /CN=bump.filter.example.net \
  -keyout "$tmpdir/server.key" -out "$tmpdir/server.csr" >/dev/null 2>&1
cat >"$tmpdir/server.ext" <<'EXT'
subjectAltName=DNS:bump.filter.example.net
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
EXT
openssl x509 -req -days 1 -sha256 -in "$tmpdir/server.csr" \
  -CA "$tmpdir/origin-ca.crt" -CAkey "$tmpdir/origin-ca.key" -CAcreateserial \
  -extfile "$tmpdir/server.ext" -out "$tmpdir/server.crt" >/dev/null 2>&1
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=validation \
  -addext 'basicConstraints=critical,CA:TRUE' \
  -addext 'keyUsage=critical,keyCertSign,cRLSign' \
  -keyout "$tmpdir/ca.key" -out "$tmpdir/ca.crt" >/dev/null 2>&1
chmod 644 "$tmpdir"/*.key "$tmpdir"/*.crt

"${RUNTIME[@]}" network create --internal --subnet 172.29.0.0/29 "$client_network" >/dev/null
"${RUNTIME[@]}" network create --subnet 203.1.1.0/29 "$egress_network" >/dev/null
"${RUNTIME[@]}" run -d --name "$dns" --network "$egress_network" --ip 203.1.1.3 \
  -v "$root/egress-proxy/testdata/dns-answer-server.py:/server.py:ro" \
  "$python_image" python3 /server.py >/dev/null
"${RUNTIME[@]}" run -d --name "$tls" --network "$egress_network" --ip 203.1.1.2 \
  -v "$root/egress-proxy/testdata/tls-accept-server.py:/server.py:ro" \
  -v "$tmpdir:/cert:ro" \
  "$python_image" python3 /server.py >/dev/null
"${RUNTIME[@]}" run -d --name "$proxy" --network "$client_network" --ip 172.29.0.3 \
  --sysctl net.ipv4.ip_unprivileged_port_start=0 \
  --read-only --cap-drop ALL --security-opt no-new-privileges \
  --tmpfs /tmp:rw,noexec,nosuid,nodev,mode=1777 \
  --tmpfs /var/lib/squid/ssl_db:rw,noexec,nosuid,nodev,mode=1777 \
  -v "$tmpdir/generated:/etc/squid/generated:ro" \
  -v "$tmpdir/unbound.conf:/etc/unbound/ai-gateway.conf:ro" \
  -v "$tmpdir/origin-ca.crt:/etc/ssl/certs/ca-certificates.crt:ro" \
  -v "$tmpdir/ca.crt:/etc/squid/ca.crt:ro" \
  -v "$tmpdir/ca.key:/etc/squid/ca.key:ro" \
  "$image" >/dev/null
"${RUNTIME[@]}" network connect "$egress_network" "$proxy"

ready=
for _ in $(seq 1 80); do
  if "${RUNTIME[@]}" exec "$proxy" /usr/sbin/unbound-control -c /etc/unbound/ai-gateway.conf status >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 0.25
done
[ "$ready" = 1 ] || { echo 'egress proxy did not become ready' >&2; exit 1; }

query_count() {
  # shellcheck disable=SC2016
  "${RUNTIME[@]}" exec "$proxy" perl -MIO::Socket::INET -e '
    $name=shift; $type=shift; $question="";
    for (split(/\./,$name)) { $question .= pack("C",length).$_ }
    $question .= "\0".pack("nn",$type,1);
    $socket=IO::Socket::INET->new(PeerAddr=>"127.0.0.1",PeerPort=>53,Proto=>"udp",Timeout=>3) or die;
    $socket->send(pack("n6",1234,256,1,0,0,0).$question);
    $socket->recv($response,4096); @header=unpack("n6",substr($response,0,12)); print $header[3];
  ' "$1" "$2"
}

base=filter.example.net
global_count=$(query_count global.$base 1)
mixed_count=$(query_count mixed.$base 1)
private_count=$(query_count private.$base 1)
mapped_count=$(query_count mapped.$base 28)
six_count=$(query_count six.$base 28)
[ "$global_count:$mixed_count:$private_count:$mapped_count:$six_count" = '1:1:0:0:0' ] || {
  echo "unexpected filtered DNS counts: $global_count:$mixed_count:$private_count:$mapped_count:$six_count" >&2
  exit 1
}

probe_tls() {
  local connect_name=$1 server_name=${2:-$1}
  "${RUNTIME[@]}" run --rm --network "$client_network" --ip 172.29.0.2 \
    --entrypoint /usr/bin/openssl "$image" s_client -brief \
    -proxy 172.29.0.3:3128 -connect "$connect_name:443" -servername "$server_name" \
    </dev/null >/dev/null 2>&1
}
tls_origin_ready=
for _ in $(seq 1 80); do
  if probe_tls global.$base; then
    tls_origin_ready=1
    break
  fi
  sleep 0.25
done
[ "$tls_origin_ready" = 1 ] || { echo 'TLS origin fixture did not become ready' >&2; exit 1; }
probe_tls mixed.$base
for name in private mapped six; do
  if probe_tls "$name.$base"; then echo "$name address unexpectedly connected" >&2; exit 1; fi
done
if probe_tls global.$base mixed.$base; then
  echo 'CONNECT authority and TLS SNI mismatch unexpectedly connected' >&2
  exit 1
fi
probe_tls rebind.$base
"${RUNTIME[@]}" restart "$proxy" >/dev/null
ready=
for _ in $(seq 1 80); do
  if "${RUNTIME[@]}" exec "$proxy" /usr/sbin/unbound-control -c /etc/unbound/ai-gateway.conf status >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 0.25
done
[ "$ready" = 1 ] || { echo 'egress proxy did not recover for rebinding test' >&2; exit 1; }
if probe_tls rebind.$base; then
  echo 'private DNS rebinding answer unexpectedly connected' >&2
  exit 1
fi

bump_request() {
  local method=$1 path=$2 host=${3:-bump.$base} response
  response=$(
    printf '%s %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n' "$method" "$path" "$host" \
      | "${RUNTIME[@]}" run --rm -i --network "$client_network" --ip 172.29.0.2 \
          -v "$tmpdir/ca.crt:/inspection-ca.crt:ro" \
          --entrypoint /usr/bin/openssl "$image" s_client -quiet -verify_return_error \
          -CAfile /inspection-ca.crt -proxy 172.29.0.3:3128 \
          -connect bump.$base:443 -servername bump.$base 2>/dev/null \
      || true
  )
  printf '%s' "${response%%$'\n'*}" | tr -d '\r'
}
allowed=$(bump_request GET /allowed)
wrong_method=$(bump_request POST /allowed)
wrong_path=$(bump_request GET /forbidden)
wrong_host=$(bump_request GET /allowed global.$base)
direct_ip=$(
  printf 'GET /allowed HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n' global.$base \
    | "${RUNTIME[@]}" run --rm -i --network "$client_network" --ip 172.29.0.2 \
        -v "$tmpdir/ca.crt:/inspection-ca.crt:ro" \
        --entrypoint /usr/bin/openssl "$image" s_client -quiet -verify_return_error \
        -CAfile /inspection-ca.crt -proxy 172.29.0.3:3128 \
        -connect 203.1.1.2:443 -servername global.$base 2>/dev/null \
    || true
)
direct_ip=${direct_ip%%$'\n'*}
direct_ip=${direct_ip//$'\r'/}
[[ "$allowed" == *" 200 "* ]] || { echo "allowed bump request failed: $allowed" >&2; exit 1; }
[[ "$direct_ip" == *" 403 "* || "$direct_ip" == *" 503 "* ]] || {
  echo "direct-IP policy bypass: $direct_ip" >&2
  exit 1
}
for result in "$wrong_method" "$wrong_path"; do
  [[ "$result" == *" 403 "* ]] || { echo "bump policy bypass: $result" >&2; exit 1; }
done
[[ "$wrong_host" == *" 403 "* || "$wrong_host" == *" 503 "* ]] || {
  echo "bump Host/SNI binding bypass: $wrong_host" >&2
  exit 1
}
"${RUNTIME[@]}" logs "$proxy" 2>&1 | grep -q "method=GET host=global.$base path=/allowed .*bump=- upstream=203.1.1.2" || {
  echo 'bump Host/SNI mismatch did not produce the expected policy denial' >&2
  exit 1
}

printf 'nameserver 198.18.0.1\noptions ndots:0\n' >"$tmpdir/virtual-resolv.conf"
tunnel_security=(--device /dev/net/tun:/dev/net/tun --cap-drop ALL --cap-add NET_ADMIN --security-opt no-new-privileges)
if [ "$tun_test_mode" = ci-privileged ]; then
  tunnel_security=(--privileged)
  echo 'tun_runtime_mode=ci-privileged production_compose_validation=exact-NET_ADMIN' >&2
fi
"${RUNTIME[@]}" run -d --name "$tunnel" --network "$client_network" --ip 172.29.0.2 \
  "${tunnel_security[@]}" \
  --tmpfs /tmp:rw,noexec,nosuid,nodev,size=4m \
  --sysctl net.ipv6.conf.all.disable_ipv6=1 \
  --sysctl net.ipv6.conf.default.disable_ipv6=1 \
  "$tun2proxy_image" \
  --proxy http://172.29.0.3:3128 --dns virtual --bypass 172.29.0.3/32 \
  --tcp-timeout 600 --exit-on-fatal-error --verbosity debug >/dev/null
tunnel_ready=
for _ in $(seq 1 40); do
  if "${RUNTIME[@]}" logs "$tunnel" 2>&1 | grep -q 'tun2proxy .* starting'; then
    tunnel_ready=1
    break
  fi
  sleep 0.25
done
[ "$tunnel_ready" = 1 ] || { echo 'namespace egress tunnel did not become ready' >&2; exit 1; }
probe_tunnel_tls() {
  local name=$1
  "${RUNTIME[@]}" run --rm --network "container:$tunnel" \
    -e HTTP_PROXY= -e HTTPS_PROXY= -e ALL_PROXY= \
    -v "$tmpdir/virtual-resolv.conf:/etc/resolv.conf:ro" \
    --entrypoint /usr/bin/openssl "$image" s_client -brief \
    -connect "$name:443" -servername "$name" \
    </dev/null >/dev/null 2>&1
}
probe_tunnel_tls global.$base
probe_tunnel_tls mixed.$base
for name in private mapped six; do
  if probe_tunnel_tls "$name.$base"; then
    echo "$name address unexpectedly escaped through the namespace tunnel" >&2
    exit 1
  fi
done
"${RUNTIME[@]}" run -d --name "$tunnel_sibling" --network "container:$tunnel" \
  -e HTTP_PROXY= -e HTTPS_PROXY= -e ALL_PROXY= \
  -v "$tmpdir/virtual-resolv.conf:/etc/resolv.conf:ro" \
  --entrypoint /bin/sleep "$image" 60 >/dev/null
"${RUNTIME[@]}" exec "$tunnel_sibling" /usr/bin/openssl s_client -brief \
  -connect global.$base:443 -servername global.$base \
  </dev/null >/dev/null 2>&1
"${RUNTIME[@]}" kill --signal KILL "$tunnel" >/dev/null
[ "$("${RUNTIME[@]}" inspect -f '{{.State.Running}}' "$tunnel_sibling")" = true ] || {
  echo 'shared namespace sibling stopped with the tunnel owner' >&2
  exit 1
}
if timeout 5 "${RUNTIME[@]}" exec "$tunnel_sibling" /usr/bin/openssl s_client -brief \
  -connect global.$base:443 -servername global.$base \
  </dev/null >/dev/null 2>&1; then
  echo 'shared namespace retained Internet access after tunnel death' >&2
  exit 1
fi
"${RUNTIME[@]}" kill --signal KILL "$tunnel_sibling" >/dev/null
"${RUNTIME[@]}" rm "$tunnel_sibling" >/dev/null
"${RUNTIME[@]}" rm "$tunnel" >/dev/null

# shellcheck disable=SC2016
"${RUNTIME[@]}" exec "$proxy" sh -c 'kill "$(cat /tmp/unbound.pid)"'
for _ in $(seq 1 20); do
  [ "$("${RUNTIME[@]}" inspect -f '{{.State.Running}}' "$proxy")" = false ] && break
  sleep 0.25
done
[ "$("${RUNTIME[@]}" inspect -f '{{.State.Running}}' "$proxy")" = false ] || {
  echo 'proxy container survived resolver failure' >&2
  exit 1
}

echo 'filtered_dns=valid mixed_answers=valid rebind=valid direct_ip=blocked sni_binding=valid bump_policy=valid namespace_tunnel=valid tunnel_failure=valid supervision=valid'
