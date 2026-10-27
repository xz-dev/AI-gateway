# Deploy progress (apisix-models canary)

Done: 2026-09-03
Scope: tasks 2.9 + 4.1 only. NO cutover (4.3). Legacy sub2api-cpa-relay stays.

- [x] 0 read artifacts + compose
- [x] 1 host git clean, no apisix-models yet
- [x] 1b remap 172.30.21-24 -> 33-36 (provider-sidecar collision)
- [x] 2 rsync compose.yaml apisix-models models-enricher README.md .env.example
- [x] 3 CPA_MANAGEMENT_KEY already in host .env (not echoed)
- [x] 4 image: local static Go + distroless, podman save | docker load (host docker build failed: no docker0)
- [x] 5 compose up --no-deps --no-build ten new services; network connect cpa-netns; no existing recreate
- [x] 6 canary: prefix-only 62, passthrough sha-equal, chat 200; models.dev fill blocked (no enricher squid policy)
- [x] 7 deploy-canary-report.md
