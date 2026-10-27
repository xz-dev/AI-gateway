FROM docker.io/library/golang:1.27-bookworm@sha256:972b4110ba0f47539a975c31e52c64765e1ff19f2d74469ae4a595c6676e80a5 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG STATUS_VERSION=0.2.2
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.version=${STATUS_VERSION}" -o /out/aisix-status .

FROM gcr.io/distroless/static-debian12:nonroot@sha256:52dcfbabb7457ea47c82f6e13af8c8a4a1d9f7b0145142b3ecab20f2b888411d
ARG STATUS_VERSION=0.2.2
ARG VCS_REF=unknown
LABEL org.opencontainers.image.title="AI-gateway AISIX status" \
      org.opencontainers.image.version="${STATUS_VERSION}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.source="https://github.com/xz-dev/AI-gateway"
COPY --from=build /out/aisix-status /aisix-status
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/aisix-status"]
