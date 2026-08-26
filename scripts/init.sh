#!/usr/bin/env bash
set -euo pipefail
umask 077

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"

command -v openssl >/dev/null || { echo 'openssl is required' >&2; exit 1; }
[ ! -e .env ] || { echo '.env already exists; refusing to overwrite it' >&2; exit 1; }
[ ! -e cpa/config.yaml ] || { echo 'cpa/config.yaml already exists; refusing to overwrite it' >&2; exit 1; }

env_tmp=$(mktemp "$root/.env.tmp.XXXXXX")
cpa_tmp=$(mktemp "$root/cpa/config.yaml.tmp.XXXXXX")
cleanup() { rm -f "$env_tmp" "$cpa_tmp"; }
trap cleanup EXIT
cp .env.example "$env_tmp"

rewrite_env() {
  local key=$1 value=$2 next found=0 line
  next=$(mktemp "$root/.env.tmp.XXXXXX")
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
      "$key="*) printf '%s=%s\n' "$key" "$value"; found=1 ;;
      *) printf '%s\n' "$line" ;;
    esac
  done <"$env_tmp" >"$next"
  [ "$found" = 1 ] || { rm -f "$next"; echo "missing $key in .env.example" >&2; exit 1; }
  mv "$next" "$env_tmp"
}

cpa_api_key=$(openssl rand -hex 32)
cpa_management_key=$(openssl rand -hex 32)
rewrite_env CPA_API_KEY "$cpa_api_key"
rewrite_env CPA_MANAGEMENT_KEY "$cpa_management_key"
rewrite_env POSTGRES_PASSWORD "$(openssl rand -hex 32)"
rewrite_env REDIS_PASSWORD "$(openssl rand -hex 32)"
rewrite_env JWT_SECRET "$(openssl rand -hex 32)"
rewrite_env TOTP_ENCRYPTION_KEY "$(openssl rand -hex 32)"
rewrite_env ADMIN_PASSWORD "$(openssl rand -hex 32)"
rewrite_env CPA_CONFIG_FILE ./cpa/config.yaml

while IFS= read -r line || [ -n "$line" ]; do
  case "$line" in
    *replace-with-generated-cpa-management-key*) printf '  secret-key: "%s"\n' "$cpa_management_key" ;;
    *replace-with-generated-cpa-api-key*) printf '  - "%s"\n' "$cpa_api_key" ;;
    *) printf '%s\n' "$line" ;;
  esac
done <cpa/config.example.yaml >"$cpa_tmp"

install -d -m 700 data data/cpa data/cpa/auths data/cpa/logs \
  data/sub2api data/sub2api/app data/sub2api/postgres data/sub2api/redis
chmod 600 "$env_tmp" "$cpa_tmp"
mv "$env_tmp" .env
mv "$cpa_tmp" cpa/config.yaml
trap - EXIT

cat <<'EOF'
Private runtime files created without printing secrets.
Next:
  1. Set ADMIN_EMAIL in .env.
  2. Set CLOUDFLARED_TUNNEL_TOKEN in .env using an editor that does not expose it in shell history.
  3. Run ./scripts/validate.sh, then docker compose pull && docker compose up -d.
EOF
