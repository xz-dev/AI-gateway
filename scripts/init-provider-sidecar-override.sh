#!/usr/bin/env bash
set -euo pipefail
umask 077

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
output=${1:-$root/compose.override.yaml}
[ ! -e "$output" ] || { echo "$output already exists; refusing to overwrite it" >&2; exit 1; }

tmp=$(mktemp /tmp/ai-gateway-provider-sidecar-override.XXXXXX)
cleanup() { rm -f -- "$tmp"; }
trap cleanup EXIT

cat >"$tmp" <<'YAML'
services:
  egress-proxy:
    networks:
      provider-sidecar-squid-target:
        ipv4_address: 172.30.24.2

  cpa-netns:
    extra_hosts:
      - "cpa-egress-relay:172.30.17.3"
      - "provider-sidecar:172.30.21.3"
    networks:
      cpa-provider-sidecar-source:
        ipv4_address: 172.30.21.2

  cpa-provider-sidecar-relay:
    image: ${SOCAT_IMAGE:?Set SOCAT_IMAGE in .env}
    user: "65534:65534"
    restart: unless-stopped
    depends_on:
      provider-sidecar:
        condition: service_started
    pids_limit: 512
    mem_limit: 64m
    memswap_limit: 64m
    cpus: 0.2
    read_only: true
    cap_drop: [ALL]
    security_opt: [no-new-privileges:true]
    command: ["OPENSSL-LISTEN:8080,bind=172.30.21.3,reuseaddr,fork,cert=/run/provider-sidecar-tls/server.crt,key=/run/provider-sidecar-tls/server.key,verify=0,openssl-min-proto-version=TLS1.2", "TCP4:172.30.22.2:8080,connect-timeout=10"]
    volumes:
      # Parent host directory stays 0700. Direct bind-mounted leaf certificate
      # and key are 0444 so unprivileged UID 65534 can read mounted inodes.
      - ./data/provider-sidecar-tls/server.crt:/run/provider-sidecar-tls/server.crt:ro,z
      - ./data/provider-sidecar-tls/server.key:/run/provider-sidecar-tls/server.key:ro,z
    logging:
      driver: json-file
      options: {max-size: "10m", max-file: "3"}
    networks:
      cpa-provider-sidecar-source:
        aliases: [provider-sidecar]
        ipv4_address: 172.30.21.3
      cpa-provider-sidecar-target:
        ipv4_address: 172.30.22.3

  provider-sidecar-tunnel:
    image: ${TUN2PROXY_IMAGE:?Set TUN2PROXY_IMAGE in .env}
    restart: "no"
    depends_on:
      provider-sidecar-squid-relay:
        condition: service_started
        restart: true
    pids_limit: 64
    mem_limit: 64m
    memswap_limit: 64m
    cpus: 0.5
    tmpfs: ["/tmp:rw,noexec,nosuid,nodev,size=4m"]
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
      cpa-provider-sidecar-target:
        aliases: [provider-sidecar-target]
        ipv4_address: 172.30.22.2
      provider-sidecar-squid-source:
        ipv4_address: 172.30.23.2

  # This is the transport/security contract only. Add runtime-specific environment,
  # volumes, command, and healthcheck fields in this ignored runtime override.
  provider-sidecar:
    image: ${PROVIDER_SIDECAR_IMAGE:?Set a digest-pinned PROVIDER_SIDECAR_IMAGE in .env}
    user: "${PROVIDER_SIDECAR_USER:?Set the non-root PROVIDER_SIDECAR_USER in .env}"
    network_mode: service:provider-sidecar-tunnel
    restart: unless-stopped
    depends_on:
      provider-sidecar-tunnel:
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
      SSL_CERT_FILE: /etc/ssl/certs/ai-gateway-ca-bundle.pem
    volumes:
      - ./data/egress-proxy/ca-bundle.pem:/etc/ssl/certs/ai-gateway-ca-bundle.pem:ro,z
      - ./data/egress-proxy/virtual-resolv.conf:/etc/resolv.conf:ro,z

  provider-sidecar-squid-relay:
    image: ${SOCAT_IMAGE:?Set SOCAT_IMAGE in .env}
    user: "65534:65534"
    restart: unless-stopped
    depends_on:
      egress-proxy:
        condition: service_started
    pids_limit: 512
    mem_limit: 64m
    memswap_limit: 64m
    cpus: 0.2
    read_only: true
    cap_drop: [ALL]
    security_opt: [no-new-privileges:true]
    command: ["TCP4-LISTEN:3128,bind=172.30.23.3,reuseaddr,fork", "TCP4:172.30.24.2:3128,connect-timeout=10"]
    logging:
      driver: json-file
      options: {max-size: "10m", max-file: "3"}
    networks:
      provider-sidecar-squid-source:
        aliases: [provider-sidecar-egress-relay]
        ipv4_address: 172.30.23.3
      provider-sidecar-squid-target:
        ipv4_address: 172.30.24.3

  cli-proxy-api:
    depends_on:
      cpa-provider-sidecar-relay:
        condition: service_started
        restart: true
    environment:
      NO_PROXY: 127.0.0.1,localhost,cli-proxy-api,cpa-egress-relay,provider-sidecar
    volumes:
      - ./data/provider-sidecar-tls/cpa-ca-bundle.pem:/etc/ssl/certs/ai-gateway-ca-bundle.pem:ro,z

networks:
  cpa-provider-sidecar-source:
    internal: true
    ipam: {config: [{subnet: 172.30.21.0/29}]}
  cpa-provider-sidecar-target:
    internal: true
    ipam: {config: [{subnet: 172.30.22.0/29}]}
  provider-sidecar-squid-source:
    internal: true
    ipam: {config: [{subnet: 172.30.23.0/29}]}
  provider-sidecar-squid-target:
    internal: true
    ipam: {config: [{subnet: 172.30.24.0/29}]}
YAML

install -m 600 "$tmp" "$output"
echo "Created ignored provider-sidecar override: $output"
