#!/usr/bin/env bash
set -euo pipefail
umask 077

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"

command -v openssl >/dev/null || { echo 'openssl is required' >&2; exit 1; }
[ ! -e .env ] || { echo '.env already exists; refusing to overwrite it' >&2; exit 1; }
[ ! -e data/cpa/conf/config.yaml ] || { echo 'data/cpa/conf/config.yaml already exists; refusing to overwrite it' >&2; exit 1; }

tmpdir=$(mktemp -d /tmp/ai-gateway-init.XXXXXX)
env_tmp=$tmpdir/.env
cpa_tmp=$tmpdir/config.yaml
mgmt_tmp=$tmpdir/mgmt.key
cleanup() { rm -rf -- "$tmpdir"; }
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
cp .env.example "$env_tmp"

rewrite_env() {
  local key=$1 value=$2 next found=0 line
  next=$(mktemp "$tmpdir/.env.next.XXXXXX")
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
printf '%s\n' "$cpa_management_key" >"$mgmt_tmp"

while IFS= read -r line || [ -n "$line" ]; do
  case "$line" in
    *replace-with-generated-cpa-management-key*) printf '  secret-key: "%s"\n' "$cpa_management_key" ;;
    *replace-with-generated-cpa-api-key*) printf '  - "%s"\n' "$cpa_api_key" ;;
    *) printf '%s\n' "$line" ;;
  esac
done <cpa/config.example.yaml >"$cpa_tmp"

install -d -m 700 data data/cpa data/cpa/conf data/cpa/auths data/cpa/logs \
  data/cpa/plugins data/cpa/runtime data/sub2api data/sub2api/app \
  data/sub2api/postgres data/sub2api/redis
# PostgreSQL 18 mounts this parent at /var/lib/postgresql; its postgres user
# must be able to traverse it before the entrypoint creates/chowns PGDATA.
chmod 1777 data/sub2api/postgres
install -m 600 "$env_tmp" .env
install -m 600 "$cpa_tmp" data/cpa/conf/config.yaml
install -m 600 "$mgmt_tmp" data/cpa/mgmt.key
./scripts/init-egress-proxy.sh >/dev/null
trap - EXIT

cat <<'EOF'
Private runtime files created without printing secrets.
Next:
  1. Set ADMIN_EMAIL in .env.
  2. Set CLOUDFLARED_TUNNEL_TOKEN in .env using an editor that does not expose it in shell history.
  3. Add only required domains, methods, and paths to data/egress-proxy/policy.json.
  4. Run ./scripts/init-egress-proxy.sh && ./scripts/validate.sh.
  5. Pull the digest-pinned upstream images, then run docker compose up -d --build --wait.
EOF
