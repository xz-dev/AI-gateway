#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
work=$(mktemp -d /tmp/ai-gateway-zcode-compose.XXXXXX)
cleanup() { rm -rf -- "$work"; }
trap cleanup EXIT HUP INT TERM

cp "$root/.env.example" "$work/runtime.env"
python3 - "$work/runtime.env" <<'PY'
import sys
from pathlib import Path

path = Path(sys.argv[1])
text = path.read_text()
text = text.replace("ZCODE_PROXY_API_KEY=replace-with-zcode-api-key", "ZCODE_PROXY_API_KEY=test-only-zcode-api-key")
if "ZCODE_PROXY_API_KEY=" not in text:
    text += "\nZCODE_PROXY_API_KEY=test-only-zcode-api-key\n"
if "ZCODE_PROXY_CREDENTIAL_SECRET=" not in text:
    text += "ZCODE_PROXY_CREDENTIAL_SECRET=test-only-zcode-credential-secret\n"
path.write_text(text)
PY
chmod 600 "$work/runtime.env"
mkdir -m 700 "$work/auth" "$work/egress"
printf 'fixture-trust\n' >"$work/egress/ca-bundle.pem"
openssl req -x509 -newkey rsa:2048 -nodes -days 365 -subj /CN=test-egress-ca \
  -addext 'basicConstraints=critical,CA:TRUE,pathlen:0' \
  -addext 'keyUsage=critical,keyCertSign,cRLSign' \
  -keyout "$work/egress/ca.key" -out "$work/egress/ca.crt" >/dev/null 2>&1
cat "$work/egress/ca.crt" >>"$work/egress/ca-bundle.pem"
chmod 444 "$work/egress/ca.crt" "$work/egress/ca-bundle.pem"

ZCODE_TLS_DIR=$work/tls \
ZCODE_CPA_AUTH_DIR=$work/auth \
ZCODE_ENV_FILE=$work/runtime.env \
ZCODE_EGRESS_CA_CERT=$work/egress/ca.crt \
ZCODE_EGRESS_CA_BUNDLE=$work/egress/ca-bundle.pem \
  "$root/scripts/init-zcode-tls.sh" >/dev/null
"$root/scripts/init-zcode-override.sh" "$work/compose.override.yaml" >/dev/null
ZCODE_TLS_DIR=$work/tls \
ZCODE_CPA_AUTH_DIR=$work/auth \
ZCODE_ENV_FILE=$work/runtime.env \
ZCODE_EGRESS_CA_CERT=$work/egress/ca.crt \
ZCODE_EGRESS_CA_BUNDLE=$work/egress/ca-bundle.pem \
AI_GATEWAY_COMPOSE_OVERRIDE=$work/compose.override.yaml \
  "$root/scripts/validate.sh" "$work/runtime.env"
