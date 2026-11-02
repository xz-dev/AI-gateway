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
The image SHALL provide the executable `/model-catalog` with these subcommands:
- `healthcheck`: exits 0 only when the local server's `/readyz` reports ready.
- `validate-config <path|-> [--script <path|->]`: reads each input from a file or at most 16 MiB from stdin; at most one input may be `-`. It validates the full configuration including shared chain rules and legacy-field rejection. With `--script`, it also loads the membership script in the sandbox and dry-runs it.
  - Valid input: exits 0 and prints exactly one JSON line to stdout: `{"version":1,"config_digest":"<lowercase hex SHA-256 of the config bytes>"}`, plus `"script_digest":"<lowercase hex SHA-256 of the script bytes>"` when `--script` is given.
  - Invalid or unreadable input, including a script that fails to load or dry-run: exits 1.
  - Usage errors, including two `-` inputs: exit 2.
  - Diagnostics go only to stderr, without YAML contents, script text or secrets.

The dry-run SHALL call `select` for a fixed synthetic candidate set that covers every source combination and every prefix mode, plus the configuration's static model slugs, under the same limits as a real collection. Every call SHALL return a valid value, and the dry-run SHALL NOT read CPA or AISIX. A dry-run in which the script rejects every synthetic candidate SHALL still exit 0 and SHALL print a WARN to stderr, because real candidates differ and an empty catalog is allowed.

Starting without a subcommand SHALL run the server. Startup with an invalid configuration, or with a membership script that fails to load or dry-run, SHALL exit non-zero before listening.

#### Scenario: Ansible validation keeps working
- **WHEN** the Ansible model-catalog operation pipes the private YAML to `/model-catalog validate-config -`
- **THEN** a valid file exits 0 and prints the version-1 digest JSON matching the file's `sha256sum`
- **AND** an invalid one exits 1 with the field path on stderr

#### Scenario: Candidate script validated with the live config
- **WHEN** the Ansible operation pipes a candidate `catalog.lua` to `/model-catalog validate-config /app/config.yaml --script -`
- **THEN** a valid script exits 0 and prints `script_digest` matching the file's `sha256sum`
- **AND** a script with a syntax error, a missing `select` or an invalid return value exits 1 with the error class on stderr and no script text

#### Scenario: Script that rejects everything passes with a warning
- **WHEN** a candidate script returns `false, reason` for every synthetic candidate
- **THEN** `validate-config --script` exits 0, prints the digest JSON, and writes a WARN naming the rejected count to stderr

#### Scenario: Compose healthcheck keeps working
- **WHEN** Docker runs `/model-catalog healthcheck` against a serving container
- **THEN** it exits 0

### Requirement: Membership script runs in a bounded sandbox
The membership script SHALL run in wasmoon, a Lua 5.4 runtime compiled to WebAssembly. Its exact version SHALL be pinned and recorded in `package-lock.json`.

The script path SHALL come from `CATALOG_SCRIPT_PATH`. When the variable is unset, the sidecar SHALL use `/app/catalog.lua`, the default script shipped in the image, built from the tracked sample `services/routing/model-catalog/catalog.lua`. When the variable is set, startup and `validate-config` SHALL fail if the file is missing, unreadable, larger than 256 KiB, or a directory.

The sandbox SHALL expose only:
- the Lua base functions, without code loading;
- the `string`, `table`, `math` and `utf8` libraries;
- the host `glob` helper.
It SHALL NOT expose `io`, `os`, `package`, `require`, `load`, `loadstring`, `loadfile`, `dofile`, `debug`, `coroutine`, `collectgarbage` or `print`. The host SHALL NOT expose JavaScript objects or proxies to Lua.

Each run SHALL have two phases:
- **Initialization.** Start the worker if no resident one exists, prepare the Lua runtime, create a fresh Lua state and install the trusted host prelude. Initialization SHALL NOT load or execute any part of the script. It SHALL take at most 10 seconds of wall time; past that the worker is terminated and the run fails with error class `init_timeout`.
- **Execution.** It starts only after the worker reports initialization complete and the host has started the execution clock. It covers loading the script, calling `select` for every candidate, host calls, checking the decisions, closing the Lua state and delivering the result.

Execution SHALL be bounded:
- at most 1,000,000 Lua VM instructions to load the script;
- at most 100,000 instructions per `select` call;
- at most 20,000,000 instructions per collection;
- at most 16 MiB of Lua allocations;
- at most 2 seconds of wall time, enforced by terminating the worker; error class `wall_time`.

Exceeding any bound SHALL be a script error.

The script SHALL run in one resident worker thread per server process. That worker is reused across authority collections, and runs execute one at a time. Each run SHALL use a new Lua state:
- it is created during that run's initialization;
- the script is loaded into it once;
- `select` is called for every candidate;
- it is closed before the result is returned.

No Lua state, global variable, upvalue, instruction counter or glob cache SHALL survive from one run to the next. The worker SHALL receive only the script text and the candidate tables, and SHALL return only the decisions. Its V8 heap SHALL be limited to 32 MiB of old generation and 8 MiB of young generation, and it SHALL collect garbage after each reply, outside the execution bound. The main thread SHALL NOT hold a Lua state.

After any failed run, the host SHALL terminate the worker and wait for it to exit. Failures include `init_timeout`, `wall_time`, an instruction or memory bound, a runtime or load error, an invalid return and a worker exit. The host SHALL do the same when the script digest differs from the one the resident worker was started with. The next run then starts a new worker under the initialization bound. A `validate-config` run SHALL use the same two phases and bounds in a worker that exits when the run finishes.

The resident worker and the Lua state SHALL count against the memory baseline. Peak resident memory with a collection and a generation build running together SHALL stay below the 384 MiB container limit, and repeated collections SHALL NOT grow resident memory.

#### Scenario: Sandbox has no host access
- **WHEN** a script calls `io.open`, `os.getenv`, `require`, `load` or `debug.sethook`
- **THEN** the call fails because the name is `nil`, and the collection fails as a script error

#### Scenario: Endless loop is stopped
- **WHEN** `select` contains `while true do end`
- **THEN** the call stops at the per-call instruction bound, the collection fails as a script error, and the server keeps serving the previous generation

#### Scenario: Long C call is stopped
- **WHEN** `select` runs one `string.find` that takes longer than the wall-time bound
- **THEN** the worker is terminated, the collection fails with error class `wall_time`, and the main thread kept serving requests during the call

#### Scenario: Memory bomb is stopped
- **WHEN** `select` builds a table of 16 MiB of strings
- **THEN** allocation fails at the Lua memory bound and the collection fails as a script error

#### Scenario: Repeated collections do not leak
- **WHEN** an operator triggers 50 consecutive forced refreshes with unchanged inputs and the default script
- **THEN** resident memory after the last refresh settles within the steady-state baseline rather than trending upward

#### Scenario: Slow initialization does not use the execution budget
- **WHEN** initialization of a run takes 1.5 seconds and the script then needs 1 second of execution
- **THEN** the collection succeeds, because the 2-second wall-time bound started only after the worker reported initialization complete

#### Scenario: Stalled initialization is stopped
- **WHEN** initialization does not complete within 10 seconds
- **THEN** the worker is terminated, the collection fails with error class `init_timeout`, no script code has run, and the server keeps serving the previous generation

#### Scenario: Execution bound applies to a warm worker
- **WHEN** a run on a reused resident worker executes a `select` that blocks past the wall-time bound
- **THEN** the worker is terminated, the collection fails with error class `wall_time`, and the next collection starts a new worker under the initialization bound

#### Scenario: No state survives between collections
- **WHEN** a script increments a global counter in `select` and keeps a candidate only while the counter is 1, and two collections run on the same resident worker
- **THEN** both collections return identical decisions, because each run uses a new Lua state

#### Scenario: Worker is recreated after a failure
- **WHEN** a collection fails with a runtime error and the next collection runs with a valid script
- **THEN** the failed worker has exited, the next collection runs on a new worker, and it succeeds

#### Scenario: Changed script reloads the worker
- **WHEN** the script digest changes between two collections
- **THEN** the worker started with the old digest is terminated, and the second collection runs the new script on a new worker
