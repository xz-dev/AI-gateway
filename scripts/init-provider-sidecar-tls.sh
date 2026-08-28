#!/usr/bin/env bash
set -euo pipefail
umask 077

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"

mode=${1:-init}
[ "$mode" = init ] || [ "$mode" = --check ] || { echo 'usage: init-provider-sidecar-tls.sh [--check]' >&2; exit 2; }
for command in openssl python3; do
  command -v "$command" >/dev/null || { echo "$command is required" >&2; exit 1; }
done

resolve_path() {
  case "$1" in /*) printf '%s\n' "$1" ;; *) printf '%s/%s\n' "$root" "$1" ;; esac
}

tls_dir=$(resolve_path "${PROVIDER_SIDECAR_TLS_DIR:-data/provider-sidecar-tls}")
auth_dir=$(resolve_path "${PROVIDER_SIDECAR_CPA_AUTH_DIR:-data/cpa/auths}")
env_file=$(resolve_path "${PROVIDER_SIDECAR_ENV_FILE:-.env}")
egress_ca=$(resolve_path "${PROVIDER_SIDECAR_EGRESS_CA_CERT:-data/egress-proxy/ca.crt}")
egress_bundle=$(resolve_path "${PROVIDER_SIDECAR_EGRESS_CA_BUNDLE:-data/egress-proxy/ca-bundle.pem}")
auth_file=$auth_dir/provider-sidecar-planner.json
expected=(ca.key ca.crt server.key server.crt cpa-ca-bundle.pem)

[ -f "$env_file" ] || { echo "missing runtime environment: $env_file" >&2; exit 1; }
[ -f "$egress_ca" ] || { echo "missing egress public CA: $egress_ca" >&2; exit 1; }
[ -f "$egress_bundle" ] || { echo "missing egress public trust bundle: $egress_bundle" >&2; exit 1; }

python3 - "$env_file" <<'PY'
import sys
from pathlib import Path

matches = []
for line in Path(sys.argv[1]).read_text(encoding="utf-8").splitlines():
    if line.startswith("PROVIDER_SIDECAR_API_KEY="):
        matches.append(line.split("=", 1)[1])
if len(matches) != 1:
    raise SystemExit("runtime environment must contain exactly one PROVIDER_SIDECAR_API_KEY")
if not matches[0] or matches[0].startswith("replace-with-"):
    raise SystemExit("PROVIDER_SIDECAR_API_KEY is empty or still a placeholder")
PY

file_mode() { stat -c %a "$1" 2>/dev/null; }
public_id_from_key() { openssl pkey -in "$1" -pubout -outform DER 2>/dev/null | openssl dgst -sha256; }
public_id_from_cert() { openssl x509 -in "$1" -pubkey -noout 2>/dev/null | openssl pkey -pubin -outform DER 2>/dev/null | openssl dgst -sha256; }

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
  [ "$(public_id_from_key "$tls_dir/server.key")" != "$(public_id_from_cert "$egress_ca")" ] || {
    echo 'provider-sidecar leaf key must not reuse the egress inspection CA key' >&2; return 1;
  }
  openssl verify -check_ss_sig -CAfile "$tls_dir/ca.crt" "$tls_dir/ca.crt" >/dev/null || {
    echo 'provider-sidecar CA certificate self-signature is invalid' >&2; return 1;
  }
  openssl verify -purpose sslserver -CAfile "$tls_dir/ca.crt" "$tls_dir/server.crt" >/dev/null || {
    echo 'provider-sidecar server certificate chain or purpose is invalid' >&2; return 1;
  }
  openssl x509 -in "$tls_dir/ca.crt" -noout -checkend 2592000 >/dev/null || {
    echo 'provider-sidecar CA expires within 30 days' >&2; return 1;
  }
  openssl x509 -in "$tls_dir/server.crt" -noout -checkend 2592000 >/dev/null || {
    echo 'provider-sidecar server certificate expires within 30 days' >&2; return 1;
  }
  python3 - "$tls_dir/ca.crt" "$tls_dir/server.crt" <<'PY'
import subprocess
import sys

def extension(path, name):
    output = subprocess.run(
        ["openssl", "x509", "-in", path, "-noout", "-ext", name],
        check=True, capture_output=True, text=True,
    ).stdout
    lines = [line.strip() for line in output.splitlines() if line.strip()]
    if not lines:
        return False, []
    critical = "critical" in lines[0].lower()
    items = [item.strip() for line in lines[1:] for item in line.split(",") if item.strip()]
    return critical, items

def require_profile(path, name, critical, items, label):
    actual_critical, actual_items = extension(path, name)
    if actual_critical != critical or actual_items != items:
        raise SystemExit(
            f"{label} mismatch: critical={actual_critical} values={actual_items}"
        )

ca_path, leaf_path = sys.argv[1:]
require_profile(ca_path, "basicConstraints", True, ["CA:TRUE", "pathlen:0"], "provider-sidecar CA basicConstraints profile")
require_profile(ca_path, "keyUsage", True, ["Certificate Sign", "CRL Sign"], "provider-sidecar CA keyUsage profile")
require_profile(leaf_path, "basicConstraints", True, ["CA:FALSE"], "provider-sidecar server basicConstraints profile")
require_profile(leaf_path, "keyUsage", True, ["Digital Signature", "Key Encipherment"], "provider-sidecar server keyUsage profile")
require_profile(leaf_path, "extendedKeyUsage", False, ["TLS Web Server Authentication"], "provider-sidecar server extendedKeyUsage profile")
require_profile(leaf_path, "subjectAltName", False, ["DNS:provider-sidecar"], "provider-sidecar server SAN profile")
PY
  [ "$(public_id_from_cert "$tls_dir/ca.crt")" != "$(public_id_from_cert "$egress_ca")" ] || {
    echo 'provider-sidecar CA must not reuse the egress inspection CA key' >&2; return 1;
  }
  [ "$(openssl x509 -in "$tls_dir/ca.crt" -noout -fingerprint -sha256)" != \
    "$(openssl x509 -in "$egress_ca" -noout -fingerprint -sha256)" ] || {
    echo 'provider-sidecar CA must be separate from egress inspection CA' >&2; return 1;
  }
}

validate_auth() {
  [ -d "$auth_dir" ] || { echo "missing CPA auth directory: $auth_dir" >&2; return 1; }
  [ "$(file_mode "$auth_dir")" = 700 ] || { echo 'CPA auth directory must have mode 700' >&2; return 1; }
  [ -f "$auth_file" ] || { echo "missing provider-sidecar planner auth: $auth_file" >&2; return 1; }
  [ "$(file_mode "$auth_file")" = 600 ] || { echo 'provider-sidecar planner auth must have mode 600' >&2; return 1; }
  python3 - "$auth_dir" "$auth_file" "$env_file" <<'PY'
import json
import sys
from pathlib import Path
from urllib.parse import urlsplit, urlunsplit

auth_dir, expected_path, env_path = map(Path, sys.argv[1:])

def strict_object(pairs):
    value = {}
    seen = set()
    for key, item in pairs:
        folded = key.casefold()
        if folded in seen:
            raise ValueError("duplicate JSON key")
        seen.add(folded)
        value[key] = item
    return value

def load(raw):
    return json.loads(raw, object_pairs_hook=strict_object)

def accepted_type(value):
    if not isinstance(value, str):
        return False
    value = value.strip().lower()
    return value == "openai-compatibility" or value.startswith("openai-compatible-") or value.startswith("openai-compatibility:")

def normalize_base_url(raw):
    if not isinstance(raw, str):
        raise ValueError("missing base_url")
    try:
        parsed = urlsplit(raw.strip())
        port = parsed.port
        host = parsed.hostname
    except ValueError as error:
        raise ValueError("malformed base_url") from error
    if parsed.scheme.lower() != "https" or not host or parsed.username is not None or parsed.password is not None or parsed.query or parsed.fragment:
        raise ValueError("base_url must be absolute HTTPS without credentials, query, or fragment")
    host = host.lower()
    if ":" in host:
        host = f"[{host}]"
    if port not in (None, 443):
        host += f":{port}"
    path = "" if parsed.path == "/" else parsed.path.rstrip("/")
    return urlunsplit(("https", host, path, "", ""))

tokens = [line.split("=", 1)[1] for line in env_path.read_text(encoding="utf-8").splitlines() if line.startswith("PROVIDER_SIDECAR_API_KEY=")]
if len(tokens) != 1 or not tokens[0] or tokens[0].startswith("replace-with-"):
    raise SystemExit("runtime provider-sidecar token is unavailable")
expected = {
    "type": "openai-compatibility",
    "base_url": "https://provider-sidecar:8080/v1",
    "token": tokens[0],
    "proxy_url": "direct",
}
try:
    actual = load(expected_path.read_text(encoding="utf-8"))
except (OSError, json.JSONDecodeError, ValueError) as error:
    raise SystemExit(f"invalid provider-sidecar planner auth: {error}") from error
if actual != expected:
    raise SystemExit("provider-sidecar planner auth content mismatch")
matches = []
for path in auth_dir.glob("*.json"):
    try:
        raw = path.read_text(encoding="utf-8")
        loose = json.loads(raw)
    except (OSError, json.JSONDecodeError):
        continue
    if not isinstance(loose, dict) or loose.get("disabled") is True or not accepted_type(loose.get("type")):
        continue
    try:
        value = load(raw)
    except (json.JSONDecodeError, ValueError) as error:
        raise SystemExit(f"active OpenAI-compatible planner auth is not strict JSON: {path.name}") from error
    try:
        base = normalize_base_url(value.get("base_url"))
    except ValueError as error:
        raise SystemExit(f"active OpenAI-compatible planner auth has invalid base_url: {path.name}") from error
    if base == expected["base_url"]:
        matches.append(path.name)
if matches != [expected_path.name]:
    raise SystemExit(f"provider-sidecar planner auth must be unique: {sorted(matches)}")
PY
}

build_bundle() {
  local tmp
  tmp=$(mktemp "$tls_dir/.cpa-ca-bundle.XXXXXX")
  cat "$egress_bundle" >"$tmp"
  [ ! -s "$tmp" ] || [ "$(tail -c 1 "$tmp" | wc -l)" = 1 ] || printf '\n' >>"$tmp"
  cat "$tls_dir/ca.crt" >>"$tmp"
  chmod 444 "$tmp"
  mv -f "$tmp" "$tls_dir/cpa-ca-bundle.pem"
}

generated=0
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
    -subj '/CN=provider-sidecar' \
    -addext 'subjectAltName=DNS:provider-sidecar' \
    -addext 'basicConstraints=critical,CA:FALSE' \
    -addext 'keyUsage=critical,digitalSignature,keyEncipherment' \
    -addext 'extendedKeyUsage=serverAuth' >/dev/null 2>&1
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
  generated=1
  trap - EXIT HUP INT TERM
elif [ ! -d "$tls_dir" ]; then
  echo "provider-sidecar TLS path is not a directory: $tls_dir" >&2
  exit 1
fi

if [ "$generated" = 1 ]; then build_bundle; fi
validate_tls
if [ "$mode" = init ]; then
  build_bundle
else
  expected_bundle=$(mktemp /tmp/ai-gateway-provider-sidecar-bundle.XXXXXX)
  trap 'rm -f -- "$expected_bundle"' EXIT HUP INT TERM
  cat "$egress_bundle" >"$expected_bundle"
  [ ! -s "$expected_bundle" ] || [ "$(tail -c 1 "$expected_bundle" | wc -l)" = 1 ] || printf '\n' >>"$expected_bundle"
  cat "$tls_dir/ca.crt" >>"$expected_bundle"
  cmp -s "$expected_bundle" "$tls_dir/cpa-ca-bundle.pem" || { echo 'provider-sidecar CPA trust bundle is stale or mismatched' >&2; exit 1; }
  rm -f "$expected_bundle"
  trap - EXIT HUP INT TERM
fi

if [ ! -e "$auth_dir" ]; then
  [ "$mode" = init ] || { echo "missing CPA auth directory: $auth_dir" >&2; exit 1; }
  install -d -m 700 "$auth_dir"
elif [ ! -d "$auth_dir" ]; then
  echo "CPA auth path is not a directory: $auth_dir" >&2
  exit 1
fi
[ "$(file_mode "$auth_dir")" = 700 ] || { echo 'CPA auth directory must have mode 700' >&2; exit 1; }

if [ ! -e "$auth_file" ]; then
  [ "$mode" = init ] || { echo "missing provider-sidecar planner auth: $auth_file" >&2; exit 1; }
  tmp_auth=$(mktemp "$auth_dir/.provider-sidecar-planner.XXXXXX")
  trap 'rm -f -- "$tmp_auth"' EXIT HUP INT TERM
  python3 - "$tmp_auth" "$env_file" <<'PY'
import json
import sys
from pathlib import Path

output, env_path = map(Path, sys.argv[1:])
tokens = [line.split("=", 1)[1] for line in env_path.read_text(encoding="utf-8").splitlines() if line.startswith("PROVIDER_SIDECAR_API_KEY=")]
if len(tokens) != 1 or not tokens[0]:
    raise SystemExit("runtime provider-sidecar token is unavailable")
# The selected Core's stock /api-call resolves file metadata.token. File
# metadata.api_key is not projected into the runtime auth Attributes map.
output.write_text(json.dumps({
    "type": "openai-compatibility",
    "base_url": "https://provider-sidecar:8080/v1",
    "token": tokens[0],
    "proxy_url": "direct",
}, separators=(",", ":")) + "\n", encoding="utf-8")
PY
  chmod 600 "$tmp_auth"
  mv "$tmp_auth" "$auth_file"
  tmp_auth=
  trap - EXIT HUP INT TERM
fi
validate_auth

echo 'Dedicated CPA-to-provider-sidecar TLS identity, trust bundle, and planner auth are valid.'
