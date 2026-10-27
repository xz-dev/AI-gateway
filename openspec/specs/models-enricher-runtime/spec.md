# models-enricher-runtime Specification

## Purpose
Define the resource, isolation, data-provenance and operational-interface contract of the TypeScript `model-catalog` service (formerly `models-enricher`; this capability ID is kept as a history key), within its fixed container limits.

## Requirements

### Requirement: Memory stays within the measured baseline
The sidecar SHALL stay within these memory bounds:
- Steady-state resident memory SHALL NOT exceed the Go implementation's steady-state baseline. The baseline is measured on production before cutover and recorded in this change.
- Peak resident memory during a generation build SHALL stay below the 384 MiB container limit, with headroom for native buffers.

When no build is running, the process SHALL hold only:
- the current generation's index (IDs, timestamps, file references);
- the bounded caches below.

It SHALL NOT hold parsed catalogs or serialized response bodies in memory. Published catalogs and models-table bodies SHALL be written as files in the cache directory, and responses SHALL stream from those files with fixed-size buffers.

Every store SHALL have a fixed bound:
- management and authority read cache: 32 MiB of payload;
- persistent `@cpa-upstream` cache: 100,000,000 bytes on disk;
- published output files: those of the current generation and of in-flight readers only, keyed by generation, client version and format.

Concurrent upstream reads SHALL be limited by the configured concurrency. Responses SHALL be limited by the existing per-endpoint size limits. Repeated builds SHALL NOT cause resident memory to grow without bound.

#### Scenario: Repeated forced refreshes do not leak
- **WHEN** an operator triggers 50 consecutive forced refreshes with unchanged inputs
- **THEN** resident memory after the last refresh settles within the steady-state baseline rather than trending upward

#### Scenario: Peak under limit during a cold build
- **WHEN** the sidecar starts with an empty cache and builds the first generation of the production catalog
- **THEN** peak resident memory stays below 384 MiB and the container is not OOM-killed

#### Scenario: Idle process holds no catalog
- **WHEN** the sidecar has published a generation and served catalog requests, and no build is running
- **THEN** resident memory returns to near the runtime's empty-process level plus the index, and catalog responses are served by streaming the published files

#### Scenario: Oversized upstream body rejected
- **WHEN** a management or api-call response exceeds its size limit
- **THEN** the read fails as invalid without buffering the body beyond the limit

### Requirement: Metadata tables are compiled in an isolated, short-lived worker
Each generation build that needs bundled metadata SHALL compile in a worker thread created for that build. The worker SHALL receive only configuration and the compile-set IDs, SHALL return only the compiled result, and SHALL exit afterwards. The main thread SHALL NOT keep the bundled metadata tables.

Worker and main thread SHALL share no mutable state. Record-source rows SHALL be resolved on the main thread.

A worker failure or timeout SHALL fail that build only: the previous published generation remains served, and the failure is logged. Concurrent requests for the same generation SHALL share one build.

#### Scenario: Tables released after a build
- **WHEN** a generation build completes
- **THEN** its worker has exited and the main thread holds only compiled chains, fields and the published catalogs

#### Scenario: Worker crash keeps previous catalog
- **WHEN** the worker exits abnormally during a scheduled build
- **THEN** `/v1/models` keeps serving the previous generation and the table shows the failed attempt

### Requirement: Bundled metadata provenance and no runtime egress
models.dev and modelparams.dev data SHALL come from exactly pinned package versions, converted at image build time. The models table and the startup log SHALL report both package versions and the models.dev snapshot generation time.

The sidecar SHALL make no network request to models.dev, modelparams.dev, a package registry or any other internet host. All upstream reads SHALL go to CPA management, CPA api-call or the configured AISIX endpoint.

#### Scenario: Data versions visible
- **WHEN** an operator opens `/models-table`
- **THEN** the page shows the bundled `@opencode-ai/models` and `modelparams` versions and the snapshot time

#### Scenario: No internet access needed
- **WHEN** the sidecar runs without any proxy or internet route
- **THEN** catalog builds succeed using CPA, AISIX and bundled data

### Requirement: Operational command interface is preserved
The image SHALL provide the executable `/models-enricher` with these subcommands:
- `healthcheck`: exits 0 only when the local server's `/readyz` reports ready.
- `validate-config <path|->`: reads a file or at most 16 MiB from stdin, and validates the full configuration including shared chain rules and legacy-field rejection.
  - Valid input: exits 0 and prints exactly one JSON line `{"version":1,"config_digest":"<lowercase hex SHA-256 of the input bytes>"}` to stdout.
  - Invalid or unreadable input: exits 1.
  - Usage errors: exit 2.
  - Diagnostics go only to stderr, without YAML contents or secrets.

Starting without a subcommand SHALL run the server. Startup with an invalid configuration SHALL exit non-zero before listening.

#### Scenario: Ansible validation keeps working
- **WHEN** the Ansible models-enricher operation pipes the private YAML to `/models-enricher validate-config -`
- **THEN** a valid file exits 0 and prints the version-1 digest JSON matching the file's `sha256sum`
- **AND** an invalid one exits 1 with the field path on stderr

#### Scenario: Compose healthcheck keeps working
- **WHEN** Docker runs `/models-enricher healthcheck` against a serving container
- **THEN** it exits 0
