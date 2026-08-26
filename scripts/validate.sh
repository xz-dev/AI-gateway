#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"
env_file=${1:-}
if [ -z "$env_file" ]; then
  if [ -f .env ]; then env_file=.env; else env_file=.env.example; fi
fi
[ -f "$env_file" ] || { echo "missing $env_file" >&2; exit 1; }

value_from_env() {
  local key=$1
  awk -F= -v key="$key" '$1 == key {sub(/^[^=]*=/, ""); print; exit}' "$env_file"
}

if [ "$env_file" = .env ] || [ "$env_file" = "$root/.env" ]; then
  ! grep -Eq '^[A-Z0-9_]+=replace-with-' "$env_file" || { echo '.env still contains placeholders' >&2; exit 1; }
  [ "$(value_from_env ADMIN_EMAIL)" != admin@example.invalid ] || { echo 'set ADMIN_EMAIL in .env' >&2; exit 1; }
  for path in data/cpa/conf/config.yaml data/cpa/mgmt.key data/zcode/config.yaml; do
    [ -f "$path" ] || { echo "missing runtime file: $path" >&2; exit 1; }
  done
  [ "$(stat -c %a data/sub2api/postgres 2>/dev/null)" = 1777 ] || {
    echo 'data/sub2api/postgres must have mode 1777 for PostgreSQL 18' >&2
    exit 1
  }
fi

for path in .env cpa/config.yaml secrets/cloudflare-tunnel-token data; do
  if git ls-files --error-unmatch "$path" >/dev/null 2>&1; then
    echo "private runtime path is tracked: $path" >&2
    exit 1
  fi
done

docker compose --env-file "$env_file" config --quiet
apisix_image=$(value_from_env APISIX_IMAGE)
[ -n "$apisix_image" ] || apisix_image=docker.io/apache/apisix:3.18.0-debian
docker run --rm \
  -e APISIX_STAND_ALONE=true \
  -v "$root/apisix/config.yaml:/usr/local/apisix/conf/config.yaml:ro" \
  -v "$root/apisix/apisix.yaml:/usr/local/apisix/conf/apisix.yaml:ro" \
  -v "$root/apisix/lua:/opt/apisix/custom:ro" \
  "$apisix_image" apisix test >/dev/null

if command -v shellcheck >/dev/null; then shellcheck scripts/*.sh; fi
echo 'compose=valid apisix=valid private_paths=untracked'
