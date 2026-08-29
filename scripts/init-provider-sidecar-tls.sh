#!/usr/bin/env bash
set -euo pipefail
umask 077

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"

mode=${1:-init}
[ "$mode" = init ] || [ "$mode" = --check ] || {
  echo 'usage: init-provider-sidecar-tls.sh [--check]' >&2
  exit 2
}
command -v openssl >/dev/null || { echo 'openssl is required' >&2; exit 1; }

resolve_path() {
  case "$1" in /*) printf '%s\n' "$1" ;; *) printf '%s/%s\n' "$root" "$1" ;; esac
}

tls_dir=$(resolve_path "${PROVIDER_SIDECAR_TLS_DIR:-data/provider-sidecar-tls}")
egress_ca=$(resolve_path "${PROVIDER_SIDECAR_EGRESS_CA_CERT:-data/egress-proxy/ca.crt}")
egress_bundle=$(resolve_path "${PROVIDER_SIDECAR_EGRESS_CA_BUNDLE:-data/egress-proxy/ca-bundle.pem}")
expected=(ca.key ca.crt server.key server.crt cpa-ca-bundle.pem)

[ -f "$egress_ca" ] || { echo "missing egress public CA: $egress_ca" >&2; exit 1; }
[ -f "$egress_bundle" ] || { echo "missing egress public trust bundle: $egress_bundle" >&2; exit 1; }

file_mode() { stat -c %a "$1" 2>/dev/null; }
public_id_from_key() { openssl pkey -in "$1" -pubout -outform DER 2>/dev/null | openssl dgst -sha256; }
public_id_from_cert() { openssl x509 -in "$1" -pubkey -noout 2>/dev/null | openssl pkey -pubin -outform DER 2>/dev/null | openssl dgst -sha256; }

build_bundle() {
  local tmp
  tmp=$(mktemp "$tls_dir/.cpa-ca-bundle.XXXXXX")
  cat "$egress_bundle" >"$tmp"
  [ ! -s "$tmp" ] || [ "$(tail -c 1 "$tmp" | wc -l)" = 1 ] || printf '\n' >>"$tmp"
  cat "$tls_dir/ca.crt" >>"$tmp"
  chmod 444 "$tmp"
  mv -f "$tmp" "$tls_dir/cpa-ca-bundle.pem"
}

validate_tls() {
  [ -d "$tls_dir" ] || { echo "missing provider-sidecar TLS directory: $tls_dir" >&2; return 1; }
  [ "$(file_mode "$tls_dir")" = 700 ] || { echo 'provider-sidecar TLS directory must have mode 700' >&2; return 1; }
  local name
  for name in "${expected[@]}"; do
    [ -f "$tls_dir/$name" ] || { echo "incomplete provider-sidecar TLS state: missing $name" >&2; return 1; }
  done
  [ "$(file_mode "$tls_dir/ca.key")" = 600 ] || { echo 'provider-sidecar CA key must have mode 600' >&2; return 1; }
  for name in ca.crt server.key server.crt cpa-ca-bundle.pem; do
    [ "$(file_mode "$tls_dir/$name")" = 444 ] || { echo "$name must have mode 444 for direct read-only mounts" >&2; return 1; }
  done
  [ "$(public_id_from_key "$tls_dir/ca.key")" = "$(public_id_from_cert "$tls_dir/ca.crt")" ] || {
    echo 'provider-sidecar CA key and certificate do not match' >&2; return 1;
  }
  [ "$(public_id_from_key "$tls_dir/server.key")" = "$(public_id_from_cert "$tls_dir/server.crt")" ] || {
    echo 'provider-sidecar server key and certificate do not match' >&2; return 1;
  }
  [ "$(public_id_from_key "$tls_dir/ca.key")" != "$(public_id_from_key "$tls_dir/server.key")" ] || {
    echo 'provider-sidecar leaf key must be distinct from the dedicated CA key' >&2; return 1;
  }
  [ "$(public_id_from_cert "$tls_dir/ca.crt")" != "$(public_id_from_cert "$egress_ca")" ] || {
    echo 'provider-sidecar CA must be separate from the egress inspection CA' >&2; return 1;
  }
  [ "$(public_id_from_key "$tls_dir/server.key")" != "$(public_id_from_cert "$egress_ca")" ] || {
    echo 'provider-sidecar leaf key must not reuse the egress inspection CA key' >&2; return 1;
  }
  openssl verify -check_ss_sig -CAfile "$tls_dir/ca.crt" "$tls_dir/ca.crt" >/dev/null || {
    echo 'provider-sidecar CA certificate self-signature is invalid' >&2; return 1;
  }
  openssl verify -purpose sslserver -CAfile "$tls_dir/ca.crt" "$tls_dir/server.crt" >/dev/null || {
    echo 'provider-sidecar server certificate chain or purpose is invalid' >&2; return 1;
  }
  openssl x509 -in "$tls_dir/server.crt" -noout -checkhost provider-sidecar >/dev/null || {
    echo 'provider-sidecar server certificate SAN mismatch' >&2; return 1;
  }
  openssl x509 -in "$tls_dir/ca.crt" -noout -checkend 2592000 >/dev/null || {
    echo 'provider-sidecar CA expires within 30 days' >&2; return 1;
  }
  openssl x509 -in "$tls_dir/server.crt" -noout -checkend 2592000 >/dev/null || {
    echo 'provider-sidecar server certificate expires within 30 days' >&2; return 1;
  }
}

if [ ! -e "$tls_dir" ]; then
  [ "$mode" = init ] || { echo "missing provider-sidecar TLS directory: $tls_dir" >&2; exit 1; }
  parent=$(dirname "$tls_dir")
  install -d -m 700 "$parent"
  tmpdir=$(mktemp -d "$parent/.provider-sidecar-tls.XXXXXX")
  cleanup() { rm -rf -- "$tmpdir"; }
  trap cleanup EXIT HUP INT TERM
  openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:3072 -out "$tmpdir/ca.key" >/dev/null 2>&1
  openssl req -new -x509 -sha256 -days 3650 -key "$tmpdir/ca.key" -out "$tmpdir/ca.crt" \
    -subj '/CN=AI Gateway CPA Provider Sidecar Internal CA' \
    -addext 'basicConstraints=critical,CA:TRUE,pathlen:0' \
    -addext 'keyUsage=critical,keyCertSign,cRLSign' >/dev/null 2>&1
  openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:3072 -out "$tmpdir/server.key" >/dev/null 2>&1
  openssl req -new -sha256 -key "$tmpdir/server.key" -out "$tmpdir/server.csr" \
    -subj '/CN=provider-sidecar' >/dev/null 2>&1
  cat >"$tmpdir/server.ext" <<'EOF'
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
subjectAltName=DNS:provider-sidecar
EOF
  openssl x509 -req -sha256 -days 825 -in "$tmpdir/server.csr" \
    -CA "$tmpdir/ca.crt" -CAkey "$tmpdir/ca.key" -CAcreateserial \
    -extfile "$tmpdir/server.ext" -out "$tmpdir/server.crt" >/dev/null 2>&1
  rm -f "$tmpdir/server.csr" "$tmpdir/server.ext" "$tmpdir/ca.srl"
  chmod 700 "$tmpdir"
  chmod 600 "$tmpdir/ca.key"
  chmod 444 "$tmpdir/ca.crt" "$tmpdir/server.key" "$tmpdir/server.crt"
  mv "$tmpdir" "$tls_dir"
  trap - EXIT HUP INT TERM
elif [ ! -d "$tls_dir" ]; then
  echo "provider-sidecar TLS path is not a directory: $tls_dir" >&2
  exit 1
fi

if [ "$mode" = init ]; then
  build_bundle
else
  expected_bundle=$(mktemp /tmp/ai-gateway-provider-sidecar-bundle.XXXXXX)
  trap 'rm -f -- "$expected_bundle"' EXIT HUP INT TERM
  cat "$egress_bundle" >"$expected_bundle"
  [ ! -s "$expected_bundle" ] || [ "$(tail -c 1 "$expected_bundle" | wc -l)" = 1 ] || printf '\n' >>"$expected_bundle"
  cat "$tls_dir/ca.crt" >>"$expected_bundle"
  cmp -s "$expected_bundle" "$tls_dir/cpa-ca-bundle.pem" || {
    echo 'provider-sidecar CPA trust bundle is stale or mismatched' >&2
    exit 1
  }
fi

validate_tls
echo 'Dedicated CPA-to-provider-sidecar TLS identity and trust bundle are valid.'
