#!/usr/bin/env bash
set -euo pipefail

image=${1:-docker.io/alpine/socat@sha256:3d9e7966201dd3a065df591020a09fd3c70845de7e7086e3531ea69db774406b}
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
suffix=$$
source_net=ai-provider-sidecar-tls-source-$suffix
target_net=ai-provider-sidecar-tls-target-$suffix
source=ai-provider-sidecar-tls-source-$suffix
target=ai-provider-sidecar-tls-target-$suffix
relay=ai-provider-sidecar-tls-relay-$suffix
tmpdir=$(mktemp -d /tmp/ai-gateway-provider-sidecar-tls-test.XXXXXX)
token=$(openssl rand -hex 24)

cleanup() {
  local status=$? item
  if [ "$status" -ne 0 ] && docker inspect "$relay" >/dev/null 2>&1; then
    docker inspect "$relay" --format 'relay_state={{.State.Status}} exit={{.State.ExitCode}} oom={{.State.OOMKilled}}' >&2 || true
    docker logs --tail 20 "$relay" >&2 || true
  fi
  for item in "$source" "$target" "$relay"; do docker rm -f "$item" >/dev/null 2>&1 || true; done
  for item in "$source_net" "$target_net"; do docker network rm "$item" >/dev/null 2>&1 || true; done
  rm -rf -- "$tmpdir"
  exit "$status"
}
trap cleanup EXIT HUP INT TERM

for command in docker openssl python3; do command -v "$command" >/dev/null || { echo "$command is required" >&2; exit 1; }; done
docker image inspect "$image" >/dev/null 2>&1 || docker pull "$image" >/dev/null

mkdir -m 700 "$tmpdir/egress" "$tmpdir/auth"
printf 'fixture-system-trust\n' >"$tmpdir/egress/ca-bundle.pem"
openssl req -x509 -newkey rsa:2048 -nodes -days 365 -subj /CN=test-egress-ca \
  -addext 'basicConstraints=critical,CA:TRUE,pathlen:0' \
  -addext 'keyUsage=critical,keyCertSign,cRLSign' \
  -keyout "$tmpdir/egress/ca.key" -out "$tmpdir/egress/ca.crt" >/dev/null 2>&1
chmod 600 "$tmpdir/egress/ca.key"
chmod 444 "$tmpdir/egress/ca.crt"
cat "$tmpdir/egress/ca.crt" >>"$tmpdir/egress/ca-bundle.pem"
chmod 444 "$tmpdir/egress/ca-bundle.pem"
printf 'PROVIDER_SIDECAR_API_KEY=%s\n' "$token" >"$tmpdir/runtime.env"
chmod 600 "$tmpdir/runtime.env"

run_init() {
  PROVIDER_SIDECAR_TLS_DIR=$tmpdir/tls \
  PROVIDER_SIDECAR_CPA_AUTH_DIR=$tmpdir/auth \
  PROVIDER_SIDECAR_ENV_FILE=$tmpdir/runtime.env \
  PROVIDER_SIDECAR_EGRESS_CA_CERT=$tmpdir/egress/ca.crt \
  PROVIDER_SIDECAR_EGRESS_CA_BUNDLE=$tmpdir/egress/ca-bundle.pem \
    "$root/scripts/init-provider-sidecar-tls.sh" "$@"
}
expect_check_failure() {
  local label=$1
  if run_init --check >"$tmpdir/check.out" 2>&1; then
    echo "$label unexpectedly passed" >&2
    exit 1
  fi
  ! grep -Fq "$token" "$tmpdir/check.out" || { echo "$label leaked token" >&2; exit 1; }
}

run_init >/dev/null
run_init --check >/dev/null

cp "$tmpdir/tls/server.key" "$tmpdir/server.key.good"
cp "$tmpdir/tls/server.crt" "$tmpdir/server.crt.good"
openssl req -new -sha256 -key "$tmpdir/tls/server.key" -out "$tmpdir/near-expiry.csr" \
  -subj '/CN=provider-sidecar' >/dev/null 2>&1
cat >"$tmpdir/near-expiry.ext" <<'EOF'
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
subjectAltName=DNS:provider-sidecar
EOF
openssl x509 -req -sha256 -days 1 -in "$tmpdir/near-expiry.csr" \
  -CA "$tmpdir/tls/ca.crt" -CAkey "$tmpdir/tls/ca.key" -CAcreateserial \
  -extfile "$tmpdir/near-expiry.ext" -out "$tmpdir/near-expiry.crt" >/dev/null 2>&1
chmod 444 "$tmpdir/near-expiry.crt"
mv -f "$tmpdir/near-expiry.crt" "$tmpdir/tls/server.crt"
expect_check_failure 'near-expiry certificate'
rm -f "$tmpdir/tls/server.key" "$tmpdir/tls/server.crt"
cp "$tmpdir/server.key.good" "$tmpdir/tls/server.key"
cp "$tmpdir/server.crt.good" "$tmpdir/tls/server.crt"
chmod 444 "$tmpdir/tls/server.key" "$tmpdir/tls/server.crt"

cat >"$tmpdir/extra-key-usage.ext" <<'EOF'
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment,keyAgreement
extendedKeyUsage=serverAuth
subjectAltName=DNS:provider-sidecar
EOF
openssl x509 -req -sha256 -days 825 -in "$tmpdir/near-expiry.csr" \
  -CA "$tmpdir/tls/ca.crt" -CAkey "$tmpdir/tls/ca.key" -CAcreateserial \
  -extfile "$tmpdir/extra-key-usage.ext" -out "$tmpdir/extra-key-usage.crt" >/dev/null 2>&1
chmod 444 "$tmpdir/extra-key-usage.crt"
mv -f "$tmpdir/extra-key-usage.crt" "$tmpdir/tls/server.crt"
expect_check_failure 'leaf certificate with extra key usage'
grep -Fq 'provider-sidecar server keyUsage profile mismatch' "$tmpdir/check.out" || {
  echo 'extra leaf key usage failed for the wrong reason' >&2
  exit 1
}
rm -f "$tmpdir/tls/server.crt"
cp "$tmpdir/server.crt.good" "$tmpdir/tls/server.crt"
chmod 444 "$tmpdir/tls/server.crt"

cat >"$tmpdir/extra-extended-key-usage.ext" <<'EOF'
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth,clientAuth
subjectAltName=DNS:provider-sidecar
EOF
openssl x509 -req -sha256 -days 825 -in "$tmpdir/near-expiry.csr" \
  -CA "$tmpdir/tls/ca.crt" -CAkey "$tmpdir/tls/ca.key" -CAcreateserial \
  -extfile "$tmpdir/extra-extended-key-usage.ext" -out "$tmpdir/extra-extended-key-usage.crt" >/dev/null 2>&1
chmod 444 "$tmpdir/extra-extended-key-usage.crt"
mv -f "$tmpdir/extra-extended-key-usage.crt" "$tmpdir/tls/server.crt"
expect_check_failure 'leaf certificate with extra extended key usage'
grep -Fq 'provider-sidecar server extendedKeyUsage profile mismatch' "$tmpdir/check.out" || {
  echo 'extra leaf extended key usage failed for the wrong reason' >&2
  exit 1
}
rm -f "$tmpdir/tls/server.crt"
cp "$tmpdir/server.crt.good" "$tmpdir/tls/server.crt"
chmod 444 "$tmpdir/tls/server.crt"
run_init --check >/dev/null

openssl req -new -sha256 -key "$tmpdir/tls/ca.key" -out "$tmpdir/shared-key-leaf.csr" \
  -subj '/CN=provider-sidecar' >/dev/null 2>&1
openssl x509 -req -sha256 -days 825 -in "$tmpdir/shared-key-leaf.csr" \
  -CA "$tmpdir/tls/ca.crt" -CAkey "$tmpdir/tls/ca.key" -CAcreateserial \
  -extfile "$tmpdir/near-expiry.ext" -out "$tmpdir/shared-key-leaf.crt" >/dev/null 2>&1
cp "$tmpdir/tls/ca.key" "$tmpdir/shared-key-leaf.key"
chmod 444 "$tmpdir/shared-key-leaf.key" "$tmpdir/shared-key-leaf.crt"
mv -f "$tmpdir/shared-key-leaf.key" "$tmpdir/tls/server.key"
mv -f "$tmpdir/shared-key-leaf.crt" "$tmpdir/tls/server.crt"
expect_check_failure 'CA key reused as leaf key'
grep -Fq 'leaf key must be distinct from the dedicated CA key' "$tmpdir/check.out" || {
  echo 'CA/leaf key reuse failed for the wrong reason' >&2
  exit 1
}
rm -f "$tmpdir/tls/server.key" "$tmpdir/tls/server.crt"
cp "$tmpdir/server.key.good" "$tmpdir/tls/server.key"
cp "$tmpdir/server.crt.good" "$tmpdir/tls/server.crt"
chmod 444 "$tmpdir/tls/server.key" "$tmpdir/tls/server.crt"

openssl req -new -sha256 -key "$tmpdir/egress/ca.key" -out "$tmpdir/egress-key-leaf.csr" \
  -subj '/CN=provider-sidecar' >/dev/null 2>&1
openssl x509 -req -sha256 -days 825 -in "$tmpdir/egress-key-leaf.csr" \
  -CA "$tmpdir/tls/ca.crt" -CAkey "$tmpdir/tls/ca.key" -CAcreateserial \
  -extfile "$tmpdir/near-expiry.ext" -out "$tmpdir/egress-key-leaf.crt" >/dev/null 2>&1
cp "$tmpdir/egress/ca.key" "$tmpdir/egress-key-leaf.key"
chmod 444 "$tmpdir/egress-key-leaf.key" "$tmpdir/egress-key-leaf.crt"
mv -f "$tmpdir/egress-key-leaf.key" "$tmpdir/tls/server.key"
mv -f "$tmpdir/egress-key-leaf.crt" "$tmpdir/tls/server.crt"
expect_check_failure 'egress CA key reused as leaf key'
grep -Fq 'leaf key must not reuse the egress inspection CA key' "$tmpdir/check.out" || {
  echo 'egress-CA/leaf key reuse failed for the wrong reason' >&2
  exit 1
}
rm -f "$tmpdir/tls/server.key" "$tmpdir/tls/server.crt"
cp "$tmpdir/server.key.good" "$tmpdir/tls/server.key"
cp "$tmpdir/server.crt.good" "$tmpdir/tls/server.crt"
chmod 444 "$tmpdir/tls/server.key" "$tmpdir/tls/server.crt"

cp "$tmpdir/tls/ca.crt" "$tmpdir/ca.crt.good"
openssl x509 -in "$tmpdir/tls/ca.crt" -outform DER -out "$tmpdir/ca-corrupt.der"
python3 - "$tmpdir/ca-corrupt.der" <<'PY'
import sys
from pathlib import Path

path = Path(sys.argv[1])
raw = bytearray(path.read_bytes())
raw[-1] ^= 1
path.write_bytes(raw)
PY
openssl x509 -inform DER -in "$tmpdir/ca-corrupt.der" -out "$tmpdir/ca-corrupt.crt"
chmod 444 "$tmpdir/ca-corrupt.crt"
mv -f "$tmpdir/ca-corrupt.crt" "$tmpdir/tls/ca.crt"
if run_init >"$tmpdir/ca-signature.out" 2>&1; then
  echo 'corrupted CA self-signature unexpectedly passed' >&2
  exit 1
fi
grep -Fq 'CA certificate self-signature is invalid' "$tmpdir/ca-signature.out" || {
  echo 'corrupted CA self-signature failed for the wrong reason' >&2
  exit 1
}
! grep -Fq "$token" "$tmpdir/ca-signature.out" || { echo 'CA self-signature rejection leaked token' >&2; exit 1; }
mv -f "$tmpdir/ca.crt.good" "$tmpdir/tls/ca.crt"
run_init >/dev/null
run_init --check >/dev/null

cp "$tmpdir/tls/ca.crt" "$tmpdir/ca.crt.good"
openssl req -new -x509 -sha256 -days 3650 -key "$tmpdir/tls/ca.key" \
  -subj '/CN=AI Gateway CPA Provider Sidecar Internal CA' \
  -addext 'basicConstraints=critical,CA:TRUE,pathlen:0' \
  -addext 'keyUsage=critical,digitalSignature,keyCertSign,cRLSign' \
  -out "$tmpdir/extra-ca-key-usage.crt" >/dev/null 2>&1
chmod 444 "$tmpdir/extra-ca-key-usage.crt"
mv -f "$tmpdir/extra-ca-key-usage.crt" "$tmpdir/tls/ca.crt"
if run_init >"$tmpdir/ca-key-usage.out" 2>&1; then
  echo 'CA certificate with extra key usage unexpectedly passed' >&2
  exit 1
fi
grep -Fq 'provider-sidecar CA keyUsage profile mismatch' "$tmpdir/ca-key-usage.out" || {
  echo 'extra CA key usage failed for the wrong reason' >&2
  exit 1
}
! grep -Fq "$token" "$tmpdir/ca-key-usage.out" || { echo 'CA key-usage rejection leaked token' >&2; exit 1; }
mv -f "$tmpdir/ca.crt.good" "$tmpdir/tls/ca.crt"
run_init >/dev/null
run_init --check >/dev/null

chmod 600 "$tmpdir/tls/cpa-ca-bundle.pem"
printf '\nstale-fixture\n' >>"$tmpdir/tls/cpa-ca-bundle.pem"
chmod 444 "$tmpdir/tls/cpa-ca-bundle.pem"
expect_check_failure 'stale trust bundle'
run_init >/dev/null
run_init --check >/dev/null

cp "$tmpdir/egress/ca.crt" "$tmpdir/egress-ca.crt.good"
openssl req -new -x509 -sha256 -days 365 -key "$tmpdir/tls/ca.key" \
  -subj /CN=reissued-egress-ca \
  -addext 'basicConstraints=critical,CA:TRUE,pathlen:0' \
  -addext 'keyUsage=critical,keyCertSign,cRLSign' \
  -out "$tmpdir/reissued-egress.crt" >/dev/null 2>&1
chmod 444 "$tmpdir/reissued-egress.crt"
mv -f "$tmpdir/reissued-egress.crt" "$tmpdir/egress/ca.crt"
if run_init >"$tmpdir/ca-reuse.out" 2>&1; then
  echo 'egress CA public-key reuse unexpectedly passed' >&2
  exit 1
fi
grep -Fq 'must not reuse the egress inspection CA key' "$tmpdir/ca-reuse.out" || {
  echo 'egress CA public-key reuse failed for the wrong reason' >&2
  exit 1
}
! grep -Fq "$token" "$tmpdir/ca-reuse.out" || { echo 'CA-reuse rejection leaked token' >&2; exit 1; }
mv -f "$tmpdir/egress-ca.crt.good" "$tmpdir/egress/ca.crt"
run_init >/dev/null
run_init --check >/dev/null

write_auth() {
  python3 - "$tmpdir/auth/$1" "$2" "$3" "$4" "$5" <<'PY'
import json
import sys
from pathlib import Path

path, provider, base_url, disabled, token = sys.argv[1:]
value = {"type": provider, "base_url": base_url, "token": token, "proxy_url": "direct"}
if disabled:
    value["disabled"] = disabled == "true"
Path(path).write_text(json.dumps(value, separators=(",", ":")) + "\n", encoding="utf-8")
PY
  chmod 600 "$tmpdir/auth/$1"
}

for spec in \
  'variant-dash.json|openai-compatible-provider-sidecar|https://PROVIDER-SIDECAR:8080/v1/|' \
  'variant-colon.json| OpenAI-Compatibility:provider-sidecar |https://provider-sidecar:8080/v1||' \
  'variant-case.json| OPENAI-COMPATIBILITY |https://provider-sidecar:8080/v1||'; do
  IFS='|' read -r name provider base disabled <<EOF
$spec
EOF
  write_auth "$name" "$provider" "$base" "$disabled" "$token"
  expect_check_failure "planner provider variant $name"
  rm -f "$tmpdir/auth/$name"
done
cat >"$tmpdir/auth/duplicate-keys.json" <<EOF
{"type":"openai-compatibility","base_url":"https://other.invalid/v1","BASE_URL":"https://provider-sidecar:8080/v1","token":"$token","proxy_url":"direct"}
EOF
chmod 600 "$tmpdir/auth/duplicate-keys.json"
expect_check_failure 'planner auth with case-insensitive duplicate JSON key'
rm -f "$tmpdir/auth/duplicate-keys.json"
write_auth disabled.json openai-compatible-provider-sidecar https://PROVIDER-SIDECAR:8080/v1/ true "$token"
run_init --check >/dev/null
rm -f "$tmpdir/auth/disabled.json"
write_auth default-port.json openai-compatible-provider-sidecar https://provider-sidecar:443/v1/ '' "$token"
run_init --check >/dev/null
rm -f "$tmpdir/auth/default-port.json"
write_auth malformed.json openai-compatible-provider-sidecar 'https://[broken' '' "$token"
expect_check_failure 'malformed active planner identity'
rm -f "$tmpdir/auth/malformed.json"
write_auth token-drift.json openai-compatibility https://other.invalid/v1 '' drift-token
python3 - "$tmpdir/auth/provider-sidecar-planner.json" <<'PY'
import json
import sys
from pathlib import Path
path = Path(sys.argv[1])
value = json.loads(path.read_text())
value["token"] = "drift-token"
path.write_text(json.dumps(value, separators=(",", ":")) + "\n")
PY
expect_check_failure 'required planner token drift'
rm -f "$tmpdir/auth/token-drift.json" "$tmpdir/auth/provider-sidecar-planner.json"
run_init >/dev/null
run_init --check >/dev/null

openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=wrong-ca \
  -keyout "$tmpdir/wrong.key" -out "$tmpdir/wrong.crt" >/dev/null 2>&1
chmod 444 "$tmpdir/wrong.crt"

docker network create --internal --subnet 172.30.250.0/29 "$source_net" >/dev/null
docker network create --internal --subnet 172.30.251.0/29 "$target_net" >/dev/null

docker run -d --name "$target" --network "$target_net" --ip 172.30.251.2 \
  --user 65534:65534 --read-only --cap-drop ALL --security-opt no-new-privileges \
  "$image" TCP4-LISTEN:8080,bind=172.30.251.2,reuseaddr,fork EXEC:/bin/cat >/dev/null

docker run -d --name "$source" --network "$source_net" --ip 172.30.250.2 \
  --user 65534:65534 --read-only --cap-drop ALL --security-opt no-new-privileges \
  -v "$tmpdir/tls/ca.crt:/tls/ca.crt:ro" -v "$tmpdir/wrong.crt:/tls/wrong.crt:ro" \
  --entrypoint /bin/sh "$image" -c 'exec socat TCP4-LISTEN:8081,bind=172.30.250.2,reuseaddr,fork EXEC:/bin/cat' >/dev/null

docker run -d --name "$relay" --network "$source_net" --ip 172.30.250.3 \
  --user 65534:65534 --read-only --cap-drop ALL --security-opt no-new-privileges \
  --pids-limit 512 --memory 64m --memory-swap 64m --cpus .2 \
  -v "$tmpdir/tls/server.crt:/run/provider-sidecar-tls/server.crt:ro" \
  -v "$tmpdir/tls/server.key:/run/provider-sidecar-tls/server.key:ro" \
  "$image" \
  'OPENSSL-LISTEN:8080,bind=172.30.250.3,reuseaddr,fork,cert=/run/provider-sidecar-tls/server.crt,key=/run/provider-sidecar-tls/server.key,verify=0,openssl-min-proto-version=TLS1.2' \
  'TCP4:172.30.251.2:8080,connect-timeout=10' >/dev/null
docker network connect --ip 172.30.251.3 "$target_net" "$relay"

ready=
for _ in $(seq 1 50); do
  if result=$(printf 'trusted-forwarding' | docker exec -i "$source" socat - \
    OPENSSL-CONNECT:172.30.250.3:8080,cafile=/tls/ca.crt,verify=1,snihost=provider-sidecar,commonname=provider-sidecar,openssl-min-proto-version=TLS1.2 2>/dev/null) && \
    [ "$result" = trusted-forwarding ]; then ready=1; break; fi
  sleep .1
done
[ "$ready" = 1 ] || { echo 'trusted TLS forwarding did not become ready' >&2; exit 1; }

if printf 'wrong-ca' | docker exec -i "$source" socat - \
  OPENSSL-CONNECT:172.30.250.3:8080,cafile=/tls/wrong.crt,verify=1,snihost=provider-sidecar,commonname=provider-sidecar,openssl-min-proto-version=TLS1.2 >/dev/null 2>&1; then
  echo 'wrong CA unexpectedly trusted' >&2; exit 1
fi
if printf 'wrong-name' | docker exec -i "$source" socat - \
  OPENSSL-CONNECT:172.30.250.3:8080,cafile=/tls/ca.crt,verify=1,snihost=wrong.invalid,commonname=wrong.invalid,openssl-min-proto-version=TLS1.2 >/dev/null 2>&1; then
  echo 'wrong server name unexpectedly trusted' >&2; exit 1
fi
if printf 'plaintext' | docker exec -i "$source" socat - TCP4:172.30.250.3:8080 >/dev/null 2>&1; then
  echo 'plaintext unexpectedly crossed TLS listener' >&2; exit 1
fi
if printf 'tls11' | docker exec -i "$source" socat - \
  OPENSSL-CONNECT:172.30.250.3:8080,cafile=/tls/ca.crt,verify=1,snihost=provider-sidecar,commonname=provider-sidecar,openssl-max-proto-version=TLS1.1 >/dev/null 2>&1; then
  echo 'TLS 1.1 unexpectedly crossed TLS 1.2 minimum' >&2; exit 1
fi

source_listener=$(printf 'source-listener-live' | docker exec -i "$relay" socat - TCP4:172.30.250.2:8081 2>/dev/null)
[ "$source_listener" = source-listener-live ] || { echo 'source listener is not live' >&2; exit 1; }
if docker exec "$target" socat -T1 - TCP4:172.30.250.2:8081 >/dev/null 2>&1; then
  echo 'target unexpectedly initiated a connection to known-live source listener' >&2; exit 1
fi
uid=$(docker exec "$relay" id -u)
cap=$(docker exec "$relay" awk '/^CapEff:/{print $2}' /proc/1/status)
readonly=$(docker inspect "$relay" --format '{{.HostConfig.ReadonlyRootfs}}')
[ "$uid:$cap:$readonly" = '65534:0000000000000000:true' ] || {
  echo "relay hardening mismatch: $uid:$cap:$readonly" >&2; exit 1;
}
source_members=$(docker network inspect "$source_net" --format '{{len .Containers}}')
target_members=$(docker network inspect "$target_net" --format '{{len .Containers}}')
[ "$source_members:$target_members" = '2:2' ] || {
  echo "network member mismatch: $source_members:$target_members" >&2; exit 1;
}
relay_mounts=$(docker inspect "$relay" --format '{{range .Mounts}}{{println .Source "->" .Destination}}{{end}}')
[ "$(printf '%s\n' "$relay_mounts" | wc -l)" = 2 ] || { echo 'relay must mount exactly leaf certificate and key' >&2; exit 1; }
printf '%s\n' "$relay_mounts" | grep -q '/server.crt -> /run/provider-sidecar-tls/server.crt$' || { echo 'leaf certificate mount missing' >&2; exit 1; }
printf '%s\n' "$relay_mounts" | grep -q '/server.key -> /run/provider-sidecar-tls/server.key$' || { echo 'leaf key mount missing' >&2; exit 1; }
if printf '%s\n' "$relay_mounts" | grep -q 'ca.key\|ca.crt'; then
  echo 'dedicated CA material unexpectedly mounted in relay' >&2; exit 1
fi

docker stop -t 2 "$relay" >/dev/null
if printf 'relay-stopped' | docker exec -i "$source" socat -T1 - \
  OPENSSL-CONNECT:172.30.250.3:8080,cafile=/tls/ca.crt,verify=1,snihost=provider-sidecar,commonname=provider-sidecar,openssl-min-proto-version=TLS1.2 >/dev/null 2>&1; then
  echo 'stopped relay unexpectedly accepted TLS' >&2; exit 1
fi
if printf 'bypass' | docker exec -i "$source" socat -T1 - TCP4:172.30.251.2:8080 >/dev/null 2>&1; then
  echo 'source unexpectedly bypassed stopped relay to backend target' >&2; exit 1
fi

echo 'provider_sidecar_tls=valid exact_cert_profiles=valid ca_self_signature=valid ca_key_reuse=blocked pairwise_key_separation=valid planner_auth_adversarial=valid near_expiry=blocked stale_bundle=refreshed trusted_forwarding=valid wrong_ca=blocked wrong_name=blocked plaintext=blocked tls_min=1.2 source_listener=live reverse_initiation=blocked relay_bypass=blocked privileges=dropped cleanup=armed'
