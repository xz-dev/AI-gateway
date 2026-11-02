# aisix-status

**Where:** Compose service `aisix-status` (Go), published on `127.0.0.1:3001/status` through the `aisix-netns` namespace. It talks only to the AISIX Admin API over the `aisix-status-admin` network.

**What:** a server-rendered status page for AISIX routes and targets, with no page JavaScript.

**How:** open `http://127.0.0.1:3001/status`. Build and test: `go test ./...` in this directory. The image `ghcr.io/xz-dev/ai-gateway-aisix-status` is published by `publish-aisix-images.yml` and pinned by `AISIX_STATUS_IMAGE`. Recovery observation: [gateway health and recovery](../../../docs/gateway-health-and-recovery.md).
