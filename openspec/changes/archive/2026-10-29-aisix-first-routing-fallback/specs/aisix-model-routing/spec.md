## ADDED Requirements

### Requirement: AISIX memory admits one maximum Responses body

The AISIX container SHALL have a 512 MiB memory limit with no swap (`memswap_limit` equal to `mem_limit`). One 64 MiB Responses request SHALL complete under that limit. The tracked AISIX image pin SHALL equal the digest production runs. Concurrent maximum-size requests are not guaranteed: two concurrent 64 MiB requests can exceed the limit, and that residual risk SHALL be recorded in the deployment record.

#### Scenario: One 64 MiB request succeeds
- **WHEN** a single 64 MiB Responses request passes through AISIX `*` to CPA
- **THEN** it succeeds without an OOM kill, and the peak cgroup memory is recorded

#### Scenario: Pin drift is visible
- **WHEN** an operator compares tracked `compose.yaml`, `.env.example` and the running AISIX image
- **THEN** all three name `ghcr.io/api7/aisix:1.8.0-rc.1@sha256:ebcd2bbba9b7a3cc7cfb1318e4d3ba64cf9f93cbf2b19b8486ead6093da1ba5a` until a reviewed upgrade changes them together
