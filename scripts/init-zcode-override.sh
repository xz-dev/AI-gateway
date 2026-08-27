#!/usr/bin/env bash
set -euo pipefail
umask 077

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
output=${1:-$root/compose.override.yaml}
[ ! -e "$output" ] || { echo "$output already exists; refusing to overwrite it" >&2; exit 1; }

tmp=$(mktemp /tmp/ai-gateway-zcode-override.XXXXXX)
cleanup() { rm -f -- "$tmp"; }
trap cleanup EXIT

cat >"$tmp" <<'YAML'
services:
  egress-proxy:
    networks:
      zcode-egress:
        ipv4_address: 172.25.0.3

  cpa-netns:
    extra_hosts:
      - "zcode-proxy:172.26.0.2"
    networks:
      zcode-cpa:
        ipv4_address: 172.26.0.3

  zcode-egress-tunnel:
    image: ${TUN2PROXY_IMAGE:?Set TUN2PROXY_IMAGE in .env}
    restart: unless-stopped
    depends_on:
      egress-proxy:
        condition: service_started
        restart: true
    pids_limit: 64
    mem_limit: 64m
    memswap_limit: 64m
    cpus: 0.5
    read_only: true
    tmpfs:
      - /tmp:rw,noexec,nosuid,nodev,size=4m
    volumes:
      - ./data/egress-proxy/tunnel-resolv.conf:/etc/resolv.conf:rw,z
    cap_drop:
      - ALL
    cap_add:
      - NET_ADMIN
    devices:
      - /dev/net/tun:/dev/net/tun
    security_opt:
      - no-new-privileges:true
    sysctls:
      net.ipv6.conf.all.disable_ipv6: "1"
      net.ipv6.conf.default.disable_ipv6: "1"
    command:
      - --proxy
      - http://172.25.0.3:3128
      - --dns
      - virtual
      - --bypass
      - 172.25.0.3/32
      - --tcp-timeout
      - "600"
      - --exit-on-fatal-error
      - --verbosity
      - info
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"
    networks:
      zcode-cpa:
        aliases:
          - zcode-proxy
        ipv4_address: 172.26.0.2
      zcode-egress:
        ipv4_address: 172.25.0.2

  zcode-proxy:
    image: ${ZCODE_PROXY_IMAGE:?Set ZCODE_PROXY_IMAGE in .env}
    network_mode: service:zcode-egress-tunnel
    restart: unless-stopped
    depends_on:
      zcode-egress-tunnel:
        condition: service_started
        restart: true
    pids_limit: 128
    mem_limit: 512m
    memswap_limit: 512m
    cpus: 1.0
    read_only: true
    cap_drop:
      - ALL
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"
    security_opt:
      - no-new-privileges:true
    tmpfs:
      - /tmp:rw,nosuid,nodev,size=64m
    environment:
      ZCODE_PROXY_CREDENTIAL_SECRET: ${ZCODE_PROXY_CREDENTIAL_SECRET:?Set ZCODE_PROXY_CREDENTIAL_SECRET in .env}
      ZCODE_PROXY_API_KEY: ${ZCODE_PROXY_API_KEY:?Set ZCODE_PROXY_API_KEY in .env}
      ZCODE_PROXY_CONFIG: /home/bun/.zcode-proxy/config.yaml
      NODE_EXTRA_CA_CERTS: /etc/ssl/certs/ai-gateway-ca-bundle.pem
      SSL_CERT_FILE: /etc/ssl/certs/ai-gateway-ca-bundle.pem
    volumes:
      - ./data/zcode:/home/bun/.zcode-proxy:ro,Z
      - ./data/egress-proxy/ca-bundle.pem:/etc/ssl/certs/ai-gateway-ca-bundle.pem:ro,z
      - ./data/egress-proxy/virtual-resolv.conf:/etc/resolv.conf:ro,z
    healthcheck:
      test:
        - CMD
        - bun
        - -e
        - >-
          import {lookup} from "node:dns/promises";
          Promise.all([fetch("http://127.0.0.1:8080/health",{headers:{"x-api-key":process.env.ZCODE_PROXY_API_KEY}}),lookup("zcode.z.ai")])
          .then(([response,dns])=>{if(!response.ok||!dns.address.startsWith("198.18."))throw new Error("health="+response.status+" dns="+dns.address)})
          .catch(error=>{console.error(error.message);process.exit(1)})
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 30s

  cli-proxy-api:
    depends_on:
      zcode-proxy:
        condition: service_started
        restart: true
    environment:
      NO_PROXY: 127.0.0.1,localhost,cli-proxy-api,sub2api,zcode-proxy

networks:
  zcode-cpa:
    internal: true
    ipam:
      config:
        - subnet: 172.26.0.0/29
  zcode-egress:
    internal: true
    ipam:
      config:
        - subnet: 172.25.0.0/29
YAML

install -m 600 "$tmp" "$output"
echo "Created ignored production ZCode override: $output"
