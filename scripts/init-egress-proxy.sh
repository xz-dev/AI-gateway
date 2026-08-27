#!/usr/bin/env bash
set -euo pipefail
umask 077

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"

command -v openssl >/dev/null || { echo 'openssl is required' >&2; exit 1; }
command -v python3 >/dev/null || { echo 'python3 is required' >&2; exit 1; }
system_bundle=/etc/ssl/certs/ca-certificates.crt
[ -r "$system_bundle" ] || { echo "missing system CA bundle: $system_bundle" >&2; exit 1; }

runtime=data/egress-proxy
key=$runtime/ca.key
cert=$runtime/ca.crt
policy=$runtime/policy.json
generated=$runtime/generated
install -d -m 700 data "$runtime"

if { [ -e "$key" ] && [ ! -e "$cert" ]; } || { [ -e "$cert" ] && [ ! -e "$key" ]; }; then
  echo 'incomplete egress CA pair; refusing to replace either file' >&2
  exit 1
fi

tmpdir=$(mktemp -d /tmp/ai-gateway-egress-init.XXXXXX)
cleanup() { rm -rf -- "$tmpdir"; }
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

if [ ! -e "$key" ]; then
  openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:3072 -out "$tmpdir/ca.key" >/dev/null 2>&1
  openssl req -new -x509 -sha256 -days 3650 \
    -key "$tmpdir/ca.key" -out "$tmpdir/ca.crt" \
    -subj '/CN=AI Gateway Egress Inspection CA' \
    -addext 'basicConstraints=critical,CA:TRUE,pathlen:0' \
    -addext 'keyUsage=critical,keyCertSign,cRLSign' >/dev/null 2>&1
  # Parent directory is mode 0700. Mode 0444 lets the unprivileged proxy UID
  # read the individually mounted inode without exposing it through the host path.
  install -m 444 "$tmpdir/ca.key" "$key"
  install -m 444 "$tmpdir/ca.crt" "$cert"
fi

key_id=$(openssl pkey -in "$key" -pubout -outform DER 2>/dev/null | openssl dgst -sha256)
cert_id=$(openssl x509 -in "$cert" -pubkey -noout 2>/dev/null | openssl pkey -pubin -outform DER 2>/dev/null | openssl dgst -sha256)
[ "$key_id" = "$cert_id" ] || { echo 'egress CA key and certificate do not match' >&2; exit 1; }
openssl x509 -in "$cert" -noout -checkend 2592000 >/dev/null || { echo 'egress CA expires within 30 days' >&2; exit 1; }

if [ ! -e "$policy" ]; then
  install -m 600 egress-proxy/policy.example.json "$policy"
fi
cat "$system_bundle" >"$tmpdir/ca-bundle.pem"
printf '\n' >>"$tmpdir/ca-bundle.pem"
cat "$cert" >>"$tmpdir/ca-bundle.pem"
install -m 444 "$tmpdir/ca-bundle.pem" "$runtime/ca-bundle.pem"
printf 'nameserver 198.18.0.1\noptions ndots:0\n' >"$tmpdir/virtual-resolv.conf"
install -m 444 "$tmpdir/virtual-resolv.conf" "$runtime/virtual-resolv.conf"
printf 'nameserver 10.0.0.1\n' >"$tmpdir/tunnel-resolv.conf"
install -m 600 "$tmpdir/tunnel-resolv.conf" "$runtime/tunnel-resolv.conf"
python3 scripts/render-egress-policy.py "$policy" "$generated"
chmod 700 "$runtime"

echo 'Egress proxy CA, deny-by-default policy, trust bundle, virtual DNS, and rendered config are ready.'
