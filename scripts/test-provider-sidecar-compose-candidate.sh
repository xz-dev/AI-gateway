#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=scripts/container-runtime.sh
source "$root/scripts/container-runtime.sh"
COMPOSE=("${AI_GATEWAY_COMPOSE[@]}")
work=$(mktemp -d /tmp/ai-gateway-provider-sidecar-compose.XXXXXX)
trap 'rm -rf "$work"' EXIT HUP INT TERM

cp "$root/.env.example" "$work/runtime.env"
cat >>"$work/runtime.env" <<'EOF'
PROVIDER_SIDECAR_IMAGE=docker.io/library/alpine:3.22.5
PROVIDER_SIDECAR_USER=65534:65534
PROVIDER_SIDECAR_API_KEY=test-only-provider-sidecar-api-key
EOF
chmod 600 "$work/runtime.env"
"$root/scripts/init-provider-sidecar-override.sh" "$work/compose.override.yaml" >/dev/null
"${COMPOSE[@]}" -f "$root/compose.yaml" -f "$work/compose.override.yaml" \
  --env-file "$work/runtime.env" config --quiet

echo 'provider_sidecar_compose=valid'
