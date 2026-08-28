#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
work=$(mktemp -d /tmp/ai-gateway-provider-sidecar-compose.XXXXXX)
cleanup() { rm -rf -- "$work"; }
trap cleanup EXIT HUP INT TERM

cp "$root/.env.example" "$work/runtime.env"
python3 - "$work/runtime.env" <<'PY'
import sys
from pathlib import Path

path = Path(sys.argv[1])
text = path.read_text()
text += """
PROVIDER_SIDECAR_IMAGE=docker.io/library/alpine@sha256:4bcff63911fcb4448bd4fdacec207030997caf25e9bea4045fa6c8c44de311d1
PROVIDER_SIDECAR_USER=65534:65534
PROVIDER_SIDECAR_API_KEY=test-only-provider-sidecar-api-key
"""
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

PROVIDER_SIDECAR_TLS_DIR=$work/tls \
PROVIDER_SIDECAR_CPA_AUTH_DIR=$work/auth \
PROVIDER_SIDECAR_ENV_FILE=$work/runtime.env \
PROVIDER_SIDECAR_EGRESS_CA_CERT=$work/egress/ca.crt \
PROVIDER_SIDECAR_EGRESS_CA_BUNDLE=$work/egress/ca-bundle.pem \
  "$root/scripts/init-provider-sidecar-tls.sh" >/dev/null
"$root/scripts/init-provider-sidecar-override.sh" "$work/compose.override.yaml" >/dev/null
PROVIDER_SIDECAR_TLS_DIR=$work/tls \
PROVIDER_SIDECAR_CPA_AUTH_DIR=$work/auth \
PROVIDER_SIDECAR_ENV_FILE=$work/runtime.env \
PROVIDER_SIDECAR_EGRESS_CA_CERT=$work/egress/ca.crt \
PROVIDER_SIDECAR_EGRESS_CA_BUNDLE=$work/egress/ca-bundle.pem \
AI_GATEWAY_COMPOSE_OVERRIDE=$work/compose.override.yaml \
  "$root/scripts/validate.sh" "$work/runtime.env"
