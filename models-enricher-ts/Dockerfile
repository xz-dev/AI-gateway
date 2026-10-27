# Build context: models-enricher-ts with the vendor/openai-api-pi-extension submodule checked out.
ARG NODE_IMAGE=docker.io/library/node:26-bookworm-slim@sha256:1884ec272c7c06aba7696262fb822dc602f8fa5eb978d03617d6bfe487d7ebac

FROM ${NODE_IMAGE} AS build
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY . .
RUN test -f vendor/openai-api-pi-extension/metadata-core.ts || { echo "vendor/openai-api-pi-extension submodule is missing" >&2; exit 1; }
RUN npm run build

FROM ${NODE_IMAGE}
# MALLOC_ARENA_MAX=2: an exited build worker's glibc arenas would otherwise stay resident.
# The entrypoint caps the main heap and exposes gc() for the post-build memory-reducing collection.
ENV NODE_ENV=production \
    NODE_COMPILE_CACHE=/app/.node-compile-cache \
    MALLOC_ARENA_MAX=2
COPY --from=build /src/dist/server.mjs /src/dist/worker.mjs /app/dist/
COPY --from=build /src/data/source-tables.json /app/data/source-tables.json
COPY config.yaml /app/config.yaml
# A named volume inherits owner and mode from the mount point on first creation.
RUN install -d -m 0700 -o 65532 -g 65532 /var/cache/models-enricher \
 && install -d -o 65532 -g 65532 /app/.node-compile-cache \
 && printf '#!/bin/sh\nexec node --max-old-space-size=96 --expose-gc /app/dist/server.mjs "$@"\n' > /models-enricher \
 && chmod 0755 /models-enricher \
 && rm -rf /usr/local/lib/node_modules /usr/local/bin/npm /usr/local/bin/npx /usr/local/bin/corepack /opt/yarn-* /usr/local/bin/yarn /usr/local/bin/yarnpkg
USER 65532:65532
# Node keys the compile cache by UID, so the warm run uses the runtime user.
RUN /models-enricher warm-compile-cache && test -n "$(ls -A /app/.node-compile-cache/*-65532)"
EXPOSE 8090
ENTRYPOINT ["/models-enricher"]
