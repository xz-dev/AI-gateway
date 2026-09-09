FROM docker.io/library/rust:1.96.0-slim-bookworm AS build
WORKDIR /build
COPY Cargo.toml Cargo.lock ./
COPY src ./src
RUN cargo build --release --locked

FROM docker.io/library/debian:12.11-slim
COPY --from=build /build/target/release/cpa-model-sync /usr/local/bin/cpa-model-sync
USER 65534:65534
ENTRYPOINT ["/usr/local/bin/cpa-model-sync"]
CMD ["/app/config.json"]
