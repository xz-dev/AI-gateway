## Purpose

Define CPA request admission after the operator removes its application-level memory-pressure gate, while preserving the deployment's other rejection and resource contracts.

## ADDED Requirements

### Requirement: CPA does not reject requests on application memory estimates

CPA SHALL NOT reject an otherwise valid request because of cgroup memory headroom, outstanding request-memory charges, an application memory margin, or absent/unreadable cgroup memory files. CPA SHALL NOT emit its former `503 insufficient_memory` memory-charge refusal. This rule supersedes only the CPA per-hop memory-admission rule in `large-request-capacity`.

#### Scenario: Previously refused 9 MiB growth

- **WHEN** an otherwise valid request charges 9,437,184 bytes with reported headroom of 41,365,504, 8,695,808 or 25,743,360 bytes
- **THEN** CPA does not refuse that step on memory-pressure grounds

#### Scenario: Concurrent charges exceed the former margin

- **WHEN** concurrent valid requests have accounting totals above the former cgroup-headroom allowance
- **THEN** CPA does not produce an application memory-charge rejection and still releases each finished request's accounting exactly once

#### Scenario: Cgroup memory files are absent

- **WHEN** the spool directory and other required configuration are valid but cgroup memory files are absent
- **THEN** CPA starts body spooling and does not reject request accounting because those files are unavailable

### Requirement: Non-memory protections remain unchanged

CPA SHALL preserve its existing request, inline and fallback byte limits, disk-space and spool-directory checks, ticket validation, cleanup and reclamation behaviour. The deployment SHALL preserve CPA's container RAM/swap/CPU limits and the existing Go memory target. Admission behaviour at other gateway hops SHALL remain unchanged.

#### Scenario: A request violates another protection

- **WHEN** a request violates an existing body-size, inline-size, fallback-size, disk-space or ticket rule
- **THEN** the corresponding existing refusal remains in force despite removal of the CPA memory gate
