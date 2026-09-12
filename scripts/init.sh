#!/usr/bin/env bash
set -euo pipefail
umask 077

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"

command -v openssl >/dev/null || { echo 'openssl is required' >&2; exit 1; }
[ ! -e .env ] || { echo '.env already exists; refusing to overwrite it' >&2; exit 1; }
[ ! -e data/cpa/conf/config.yaml ] || { echo 'data/cpa/conf/config.yaml already exists; refusing to overwrite it' >&2; exit 1; }
for path in aisix/config.yaml aisix/resources.yaml aisix/caller-keys.json; do
  [ ! -e "$path" ] || { echo "$path already exists; refusing to overwrite it" >&2; exit 1; }
done

tmpdir=$(mktemp -d /tmp/ai-gateway-init.XXXXXX)
env_tmp=$tmpdir/.env
cpa_tmp=$tmpdir/cpa-config.yaml
mgmt_tmp=$tmpdir/mgmt.key
aisix_config_tmp=$tmpdir/aisix-config.yaml
aisix_resources_tmp=$tmpdir/aisix-resources.yaml
aisix_callers_tmp=$tmpdir/aisix-caller-keys.json
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
aisix_admin_key=$(openssl rand -hex 32)
aisix_sub2api_key=$(openssl rand -hex 32)
aisix_catalog_key=$(openssl rand -hex 32)
aisix_sub2api_hash=$(printf '%s' "$aisix_sub2api_key" | openssl dgst -sha256 -r | awk '{print $1}')
aisix_catalog_hash=$(printf '%s' "$aisix_catalog_key" | openssl dgst -sha256 -r | awk '{print $1}')
rewrite_env CPA_API_KEY "$cpa_api_key"
rewrite_env CPA_MANAGEMENT_KEY "$cpa_management_key"
rewrite_env AISIX_CATALOG_KEY "$aisix_catalog_key"
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

while IFS= read -r line || [ -n "$line" ]; do
  case "$line" in
    *replace-with-generated-aisix-admin-key*) printf '    - %s\n' "$aisix_admin_key" ;;
    *) printf '%s\n' "$line" ;;
  esac
done <aisix/config.example.yaml >"$aisix_config_tmp"
while IFS= read -r line || [ -n "$line" ]; do
  case "$line" in
    *replace-with-cpa-client-key*) printf '    api_key: %s\n' "$cpa_api_key" ;;
    *replace-with-sha256-of-sub2api-caller-key*) printf '    key_hash: %s\n' "$aisix_sub2api_hash" ;;
    *replace-with-sha256-of-catalog-caller-key*) printf '    key_hash: %s\n' "$aisix_catalog_hash" ;;
    *) printf '%s\n' "$line" ;;
  esac
done <aisix/resources.example.yaml >"$aisix_resources_tmp"
printf '{"sub2api":"%s","enricher_catalog":"%s"}\n' "$aisix_sub2api_key" "$aisix_catalog_key" >"$aisix_callers_tmp"

install -d -m 700 aisix data data/cpa data/cpa/conf data/cpa/auths data/cpa/logs \
  data/cpa/plugins data/cpa/runtime data/sub2api data/sub2api/app \
  data/sub2api/postgres data/sub2api/redis
# PostgreSQL 18 mounts this parent at /var/lib/postgresql; its postgres user
# must be able to traverse it before the entrypoint creates/chowns PGDATA.
chmod 1777 data/sub2api/postgres
install -m 600 "$env_tmp" .env
install -m 600 "$cpa_tmp" data/cpa/conf/config.yaml
install -m 600 "$mgmt_tmp" data/cpa/mgmt.key
# AISIX runs as UID 10001 and receives these files as direct read-only mounts.
# Their mode is readable in-container; the host directory is mode 0700.
install -m 444 "$aisix_config_tmp" aisix/config.yaml
install -m 444 "$aisix_resources_tmp" aisix/resources.yaml
install -m 600 "$aisix_callers_tmp" aisix/caller-keys.json
./scripts/init-egress-proxy.sh >/dev/null
trap - EXIT

cat <<'EOF'
Private runtime files created without printing secrets.
Next:
  1. Set ADMIN_EMAIL in .env.
  2. Set CLOUDFLARED_TUNNEL_TOKEN in .env using an editor that does not expose it in shell history.
  3. Replace the example models in aisix/resources.yaml with the approved concrete CPA targets and logical routes.
  4. Use the private sub2api value in aisix/caller-keys.json when configuring CPA-backed Sub2API accounts; never print it.
  5. Add only required domains, methods, and paths to data/egress-proxy/policy.json.
  6. Keep AISIX Admin on 172.30.68.2:3002; publish only the status page with AISIX_STATUS_BIND_ADDRESS/AISIX_STATUS_PORT.
  7. Run ./scripts/init-egress-proxy.sh && ./scripts/validate.sh.
  8. Source scripts/container-runtime.sh, pull images with "${AI_GATEWAY_COMPOSE[@]}", then run "${AI_GATEWAY_COMPOSE[@]}" up -d --build.
EOF
