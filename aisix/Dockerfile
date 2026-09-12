# syntax=docker/dockerfile:1.7

# AISIX v1.2.0 plus the upstream cooldown-exclusion deadlock fix.
# Keep the upstream commit and patch bytes immutable so an official release
# containing the fix can replace this image.
FROM docker.io/library/rust:1.93-trixie AS builder

ARG AISIX_BASE_SHA=adcf0523b9f84deed64fbdea311df292c5bc541b
ARG AISIX_PATCH_SHA=7d6d14bbf5f5ec4577466da48ce7763d519d81bb
ARG AISIX_PATCH_FILE_SHA256=3908eefe1d81f4325b3d953a9d773b6d50f0645d76b55f722d39528f6e9f7a0a

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates git protobuf-compiler \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
RUN git init \
    && git remote add origin https://github.com/api7/aisix.git \
    && git fetch --depth=1 origin "${AISIX_BASE_SHA}" \
    && git checkout --detach FETCH_HEAD \
    && test "$(git rev-parse HEAD)" = "${AISIX_BASE_SHA}"

COPY patches/7d6d14bbf5f5-exclusion-log-deadlock.patch /tmp/aisix.patch
RUN echo "${AISIX_PATCH_FILE_SHA256}  /tmp/aisix.patch" | sha256sum -c - \
    && git apply --check /tmp/aisix.patch \
    && git apply /tmp/aisix.patch \
    && git diff --check \
    && grep -q 'match self.exclusion_log.entry(key)' crates/aisix-proxy/src/health.rs

ENV AISIX_BUILD_VERSION=1.2.0+deadlockfix.7d6d14b
ENV AISIX_BUILD_SHA=adcf052-7d6d14b

# The upstream regression uses a separate two-second watchdog, so the
# unpatched lock-up fails this build instead of hanging the test runner.
RUN cargo test --locked -p aisix-proxy exclusion_log_gate -- --nocapture \
    && cargo build --locked --release --bin aisix \
    && install -m 0755 target/release/aisix /usr/local/bin/aisix

FROM docker.io/library/debian:trixie-slim AS runtime

ARG AISIX_BASE_SHA=adcf0523b9f84deed64fbdea311df292c5bc541b
ARG AISIX_PATCH_SHA=7d6d14bbf5f5ec4577466da48ce7763d519d81bb
LABEL io.modelcontextprotocol.server.name="io.github.api7/aisix" \
      org.opencontainers.image.version="1.2.0+deadlockfix.7d6d14b" \
      org.opencontainers.image.revision="${AISIX_BASE_SHA}+${AISIX_PATCH_SHA}" \
      org.opencontainers.image.source="https://github.com/api7/aisix"

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tini libcap2-bin \
    && useradd --system --uid 10001 --no-create-home --shell /usr/sbin/nologin aisix \
    && mkdir -p /etc/aisix/tls /var/lib/aisix \
    && chown -R aisix:aisix /etc/aisix /var/lib/aisix \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /usr/local/bin/aisix /usr/local/bin/aisix
RUN setcap 'cap_net_bind_service=+ep' /usr/local/bin/aisix \
    && apt-get purge -y --auto-remove libcap2-bin \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /src/config.managed.yaml /etc/aisix/config.managed.yaml
COPY --from=builder /src/docker/entrypoint.sh /usr/local/bin/aisix-entrypoint
RUN chmod 0755 /usr/local/bin/aisix-entrypoint

EXPOSE 3000 3002 9090
USER aisix
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/aisix-entrypoint"]
