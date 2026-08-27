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
      zcode-squid-target:
        ipv4_address: 172.30.24.2

  cpa-netns:
    extra_hosts:
      - "cpa-egress-relay:172.30.17.3"
      - "zcode-proxy:172.30.21.3"
    networks:
      cpa-zcode-source:
        ipv4_address: 172.30.21.2

  cpa-zcode-relay:
    image: ${SOCAT_IMAGE:?Set SOCAT_IMAGE in .env}
    user: "65534:65534"
    restart: unless-stopped
    depends_on:
      zcode-proxy:
        condition: service_started
    pids_limit: 64
    mem_limit: 32m
    memswap_limit: 32m
    cpus: 0.2
    read_only: true
    cap_drop: [ALL]
    security_opt: [no-new-privileges:true]
    command: ["TCP4-LISTEN:8080,bind=172.30.21.3,reuseaddr,fork", "TCP4:172.30.22.2:8080,connect-timeout=10"]
    logging:
      driver: json-file
      options: {max-size: "10m", max-file: "3"}
    networks:
      cpa-zcode-source:
        aliases: [zcode-proxy]
        ipv4_address: 172.30.21.3
      cpa-zcode-target:
        ipv4_address: 172.30.22.3

  zcode-egress-tunnel:
    image: ${TUN2PROXY_IMAGE:?Set TUN2PROXY_IMAGE in .env}
    restart: unless-stopped
    depends_on:
      zcode-squid-relay:
        condition: service_started
        restart: true
    pids_limit: 64
    mem_limit: 64m
    memswap_limit: 64m
    cpus: 0.5
    read_only: true
    tmpfs: ["/tmp:rw,noexec,nosuid,nodev,size=4m"]
    volumes:
      - ./data/egress-proxy/tunnel-resolv.conf:/etc/resolv.conf:rw,z
    cap_drop: [ALL]
    cap_add: [NET_ADMIN]
    devices: [/dev/net/tun:/dev/net/tun]
    security_opt: [no-new-privileges:true]
    sysctls:
      net.ipv6.conf.all.disable_ipv6: "1"
      net.ipv6.conf.default.disable_ipv6: "1"
    command:
      - --proxy
      - http://172.30.23.3:3128
      - --dns
      - virtual
      - --bypass
      - 172.30.23.3/32
      - --tcp-timeout
      - "600"
      - --exit-on-fatal-error
      - --verbosity
      - info
    logging:
      driver: json-file
      options: {max-size: "10m", max-file: "3"}
    networks:
      cpa-zcode-target:
        aliases: [zcode-proxy-target]
        ipv4_address: 172.30.22.2
      zcode-squid-source:
        ipv4_address: 172.30.23.2

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
    cap_drop: [ALL]
    logging:
      driver: json-file
      options: {max-size: "10m", max-file: "3"}
    security_opt: [no-new-privileges:true]
    tmpfs: ["/tmp:rw,nosuid,nodev,size=64m"]
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
      test: [CMD, bun, -e, 'import {lookup} from "node:dns/promises"; Promise.all([fetch("http://127.0.0.1:8080/health",{headers:{"x-api-key":process.env.ZCODE_PROXY_API_KEY}}),lookup("zcode.z.ai")]).then(([r,d])=>{if(!r.ok||!d.address.startsWith("198.18."))throw new Error("health="+r.status+" dns="+d.address)}).catch(e=>{console.error(e.message);process.exit(1)})']
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 30s

  zcode-squid-relay:
    image: ${SOCAT_IMAGE:?Set SOCAT_IMAGE in .env}
    user: "65534:65534"
    restart: unless-stopped
    depends_on:
      egress-proxy:
        condition: service_started
    pids_limit: 64
    mem_limit: 32m
    memswap_limit: 32m
    cpus: 0.2
    read_only: true
    cap_drop: [ALL]
    security_opt: [no-new-privileges:true]
    command: ["TCP4-LISTEN:3128,bind=172.30.23.3,reuseaddr,fork", "TCP4:172.30.24.2:3128,connect-timeout=10"]
    logging:
      driver: json-file
      options: {max-size: "10m", max-file: "3"}
    networks:
      zcode-squid-source:
        aliases: [zcode-egress-relay]
        ipv4_address: 172.30.23.3
      zcode-squid-target:
        ipv4_address: 172.30.24.3

  cli-proxy-api:
    depends_on:
      cpa-zcode-relay:
        condition: service_started
        restart: true
    environment:
      NO_PROXY: 127.0.0.1,localhost,cli-proxy-api,cpa-egress-relay,zcode-proxy

networks:
  cpa-zcode-source:
    internal: true
    ipam: {config: [{subnet: 172.30.21.0/29}]}
  cpa-zcode-target:
    internal: true
    ipam: {config: [{subnet: 172.30.22.0/29}]}
  zcode-squid-source:
    internal: true
    ipam: {config: [{subnet: 172.30.23.0/29}]}
  zcode-squid-target:
    internal: true
    ipam: {config: [{subnet: 172.30.24.0/29}]}
YAML

install -m 600 "$tmp" "$output"
echo "Created ignored production ZCode override: $output"
