#!/usr/bin/env bash
set -euo pipefail

PREFIX=/usr/local/apisix
CONFIG_TEMPLATE=/opt/apisix.yaml.tpl
APISIX_CONFIG=${PREFIX}/conf/apisix.yaml
NGINX_CONFIG=${PREFIX}/conf/nginx.conf
STALE_OFF='            proxy_cache_use_stale               off;'
STALE_ON='            proxy_cache_use_stale               error timeout updating http_502 http_503 http_504;'
VALID_OFF='            proxy_cache_valid                   any 10s;'
VALID_ON='            proxy_cache_valid                   any 120s;'

case "${CPA_API_KEY:-}" in
  *'&'*|*'/'*|*'\\'*)
    echo 'CPA_API_KEY has unsafe chars for sed' >&2
    exit 1
    ;;
esac
sed "s|__CPA_API_KEY__|${CPA_API_KEY}|g" "${CONFIG_TEMPLATE}" > "${APISIX_CONFIG}"

while awk 'NR > 1 && $2 == "00000000" && $8 == "00000000" {found=1} END {exit(found ? 0 : 1)}' /proc/net/route ||
      awk '$1 == "00000000000000000000000000000000" && $2 == "00" && $10 != "lo" {found=1} END {exit(found ? 0 : 1)}' /proc/net/ipv6_route; do
  sleep 0.05
done

if [ ! -f "${PREFIX}/conf/config.yaml" ]; then
  cat > "${PREFIX}/conf/config.yaml" <<'EOF'
deployment:
  role: data_plane
  role_data_plane:
    config_provider: yaml
EOF
fi

if [ ! -f "${APISIX_CONFIG}" ]; then
  cat > "${APISIX_CONFIG}" <<'EOF'
routes:
  -
#END
EOF
fi

/usr/bin/apisix init

rm -f "${PREFIX}/conf/config_listen.sock" \
      "${PREFIX}/logs/worker_events.sock" \
      "${PREFIX}/logs/stream_worker_events.sock"

count=$(grep -Fxc "${STALE_OFF}" "${NGINX_CONFIG}" || true)
if [ "${count}" -ne 1 ]; then
  echo "expected exactly one proxy_cache_use_stale off line, found ${count}" >&2
  exit 1
fi
valid_count=$(grep -Fxc "${VALID_OFF}" "${NGINX_CONFIG}" || true)
if [ "${valid_count}" -ne 1 ]; then
  echo "expected exactly one proxy_cache_valid any 10s line, found ${valid_count}" >&2
  exit 1
fi
sed -i \
  -e "s|^${STALE_OFF}$|${STALE_ON}|" \
  -e "s|^${VALID_OFF}$|${VALID_ON}|" \
  "${NGINX_CONFIG}"
grep -Fqx "${STALE_ON}" "${NGINX_CONFIG}"
grep -Fqx "${VALID_ON}" "${NGINX_CONFIG}"

exec /usr/local/openresty/bin/openresty -p "${PREFIX}" -g 'daemon off;'
