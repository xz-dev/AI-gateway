FROM docker.io/library/golang:1.24-bookworm@sha256:98d673f18a1aac43da744209873cb79323e11706f909251bcfb131828b95559d AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG STATUS_VERSION=0.1.1
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.version=${STATUS_VERSION}" -o /out/aisix-status .

FROM gcr.io/distroless/static-debian12:nonroot@sha256:52dcfbabb7457ea47c82f6e13af8c8a4a1d9f7b0145142b3ecab20f2b888411d
ARG STATUS_VERSION=0.1.1
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
