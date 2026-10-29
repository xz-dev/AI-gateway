## ADDED Requirements

### Requirement: AISIX memory admits one maximum Responses body

The AISIX container SHALL have a 512 MiB memory limit with no swap (`memswap_limit` equal to `mem_limit`). One 64 MiB Responses request SHALL complete under that limit. The tracked AISIX image pin SHALL equal the digest production runs. Concurrent maximum-size requests are not guaranteed: two concurrent 64 MiB requests can exceed the limit, and that residual risk SHALL be recorded in the deployment record.

#### Scenario: One 64 MiB request succeeds
- **WHEN** a single 64 MiB Responses request passes through AISIX `*` to CPA
- **THEN** it succeeds without an OOM kill, and the peak cgroup memory is recorded

#### Scenario: Pin drift is visible
- **WHEN** an operator compares tracked `compose.yaml`, `.env.example` and the running AISIX image
- **THEN** all three name `ghcr.io/api7/aisix:1.6.0@sha256:26eae08b5c94291050f179d3c0a56ca154f40751f5c755b5922b044efc523690` until a reviewed upgrade changes them together
