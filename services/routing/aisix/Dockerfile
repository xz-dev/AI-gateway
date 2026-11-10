# syntax=docker/dockerfile:1.7
# Legacy PR1387 source build, not the current deployed capacity-fork image.
# Runtime defaults follow ci/large-request/ci/compatibility-lock.json.
FROM docker.io/library/rust:1.93-trixie AS builder
ARG AISIX_SOURCE_REV=06e8075266119380844aa4e9fcb9fb80468715eb
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates git protobuf-compiler \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
RUN git init \
    && git remote add origin https://github.com/xz-dev/aisix.git \
    && git fetch --depth=1 origin "${AISIX_SOURCE_REV}" \
    && git checkout --detach FETCH_HEAD \
    && test "$(git rev-parse HEAD)" = "${AISIX_SOURCE_REV}"
# Reuse the upstream non-PGO image build, without altering application inputs.
RUN AISIX_BUILD_SHA="${AISIX_SOURCE_REV}" AISIX_BUILD_VERSION=1.8.0-pr1387 \
    scripts/build-image-binary.sh thin

FROM docker.io/library/debian:trixie-slim AS runtime
ARG AISIX_SOURCE_REV=06e8075266119380844aa4e9fcb9fb80468715eb
LABEL io.modelcontextprotocol.server.name="io.github.api7/aisix" \
      org.opencontainers.image.version="1.8.0-pr1387" \
      org.opencontainers.image.revision="${AISIX_SOURCE_REV}" \
      org.opencontainers.image.source="https://github.com/xz-dev/aisix"
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tini \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --no-create-home --shell /usr/sbin/nologin aisix \
    && mkdir -p /etc/aisix/tls /var/lib/aisix \
    && chown -R aisix:aisix /etc/aisix /var/lib/aisix
RUN --mount=type=bind,from=builder,source=/src/bin,target=/mnt/bin \
    apt-get update \
    && apt-get install -y --no-install-recommends libcap2-bin \
    && install -m 0755 /mnt/bin/aisix-amd64 /usr/local/bin/aisix \
    && /usr/local/bin/aisix --version \
    && setcap 'cap_net_bind_service=+ep' /usr/local/bin/aisix \
    && apt-get purge -y --auto-remove libcap2-bin \
    && rm -rf /var/lib/apt/lists/*
COPY --from=builder /src/config.managed.yaml /etc/aisix/config.managed.yaml
COPY --from=builder /src/docker/entrypoint.sh /usr/local/bin/aisix-entrypoint
RUN chmod 0755 /usr/local/bin/aisix-entrypoint
EXPOSE 3000 3001 9090
USER 10001
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/aisix-entrypoint"]
