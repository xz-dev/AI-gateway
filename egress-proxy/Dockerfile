FROM docker.io/library/debian:13.6

ARG SQUID_OPENSSL_VERSION=6.13-2+deb13u2
ARG UNBOUND_VERSION=1.22.0-2+deb13u3
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates \
      "squid-openssl=${SQUID_OPENSSL_VERSION}" \
      "unbound=${UNBOUND_VERSION}" \
 && rm -rf /var/lib/apt/lists/* \
 && install -d -o proxy -g proxy /var/lib/squid/ssl_db

COPY --chmod=0644 unbound.conf /etc/unbound/ai-gateway.conf
RUN /usr/sbin/unbound-checkconf /etc/unbound/ai-gateway.conf
COPY --chmod=0755 entrypoint.sh /usr/local/bin/ai-gateway-squid-entrypoint
COPY --chmod=0755 exact-host-helper.sh /usr/local/bin/ai-gateway-exact-host-helper

USER proxy
ENTRYPOINT ["/usr/local/bin/ai-gateway-squid-entrypoint"]
CMD ["/usr/sbin/squid", "-NYC", "-f", "/etc/squid/generated/squid.conf"]
