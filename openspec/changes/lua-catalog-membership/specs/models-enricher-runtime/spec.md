## MODIFIED Requirements

### Requirement: Operational command interface is preserved
The image SHALL provide the executable `/model-catalog` with these subcommands:
- `healthcheck`: exits 0 only when the local server's `/readyz` reports ready.
- `validate-config <path|-> [--script <path|->]`: reads each input from a file or at most 16 MiB from stdin; at most one input may be `-`. It validates the full configuration including shared chain rules and legacy-field rejection. With `--script`, it also loads the membership script in the sandbox and dry-runs it.
  - Valid input: exits 0 and prints exactly one JSON line to stdout: `{"version":1,"config_digest":"<lowercase hex SHA-256 of the config bytes>"}`, plus `"script_digest":"<lowercase hex SHA-256 of the script bytes>"` when `--script` is given.
  - Invalid or unreadable input, including a script that fails to load or dry-run: exits 1.
  - Usage errors, including two `-` inputs: exit 2.
  - Diagnostics go only to stderr, without YAML contents, script text or secrets.

The dry-run SHALL call `select` for a fixed synthetic candidate set that covers every source combination and every prefix mode, plus the configuration's static model slugs, under the same limits as a real collection. Every call SHALL return a valid value, and the dry-run SHALL NOT read CPA or AISIX.

Starting without a subcommand SHALL run the server. Startup with an invalid configuration, or with a membership script that fails to load or dry-run, SHALL exit non-zero before listening.

#### Scenario: Ansible validation keeps working
- **WHEN** the Ansible model-catalog operation pipes the private YAML to `/model-catalog validate-config -`
- **THEN** a valid file exits 0 and prints the version-1 digest JSON matching the file's `sha256sum`
- **AND** an invalid one exits 1 with the field path on stderr

#### Scenario: Candidate script validated with the live config
- **WHEN** the Ansible operation pipes a candidate `catalog.lua` to `/model-catalog validate-config /app/config.yaml --script -`
- **THEN** a valid script exits 0 and prints `script_digest` matching the file's `sha256sum`
- **AND** a script with a syntax error, a missing `select` or an invalid return value exits 1 with the error class on stderr and no script text

#### Scenario: Compose healthcheck keeps working
- **WHEN** Docker runs `/model-catalog healthcheck` against a serving container
- **THEN** it exits 0

## ADDED Requirements

### Requirement: Membership script runs in a bounded sandbox
The membership script SHALL run in wasmoon, a Lua 5.4 runtime compiled to WebAssembly, at a pinned exact version recorded in `package-lock.json`.

The script path SHALL come from `CATALOG_SCRIPT_PATH`. When the variable is unset, the sidecar SHALL use `/app/catalog.lua`, the default script shipped in the image, built from the tracked sample `services/routing/model-catalog/catalog.lua`. When it is set and the file is missing, unreadable, larger than 256 KiB or a directory, startup and `validate-config` SHALL fail.

The sandbox SHALL expose only the Lua base functions without code loading, and the `string`, `table`, `math` and `utf8` libraries, plus the host `glob` helper. It SHALL NOT expose `io`, `os`, `package`, `require`, `load`, `loadstring`, `loadfile`, `dofile`, `debug`, `coroutine`, `collectgarbage` or `print`. The host SHALL NOT expose JavaScript objects or proxies to Lua.

Each run SHALL be bounded:
- at most 1,000,000 Lua VM instructions to load the script;
- at most 100,000 instructions per `select` call;
- at most 20,000,000 instructions per collection;
- at most 16 MiB of Lua allocations;
- at most 2 seconds of wall time per collection, enforced by terminating the worker.

Exceeding any bound SHALL be a script error.

The script SHALL run in a worker thread created for one authority collection or one `validate-config` run. The worker SHALL create one Lua state, load the script once, call `select` for every candidate, close the state and exit. It SHALL receive only the script text and the candidate tables, and SHALL return only the decisions. Its V8 heap SHALL be limited to 32 MiB. The main thread SHALL NOT hold a Lua state.

The worker and the Lua state SHALL count against the memory baseline: peak resident memory with a collection and a generation build running together SHALL stay below the 384 MiB container limit, and repeated collections SHALL NOT grow resident memory.

#### Scenario: Sandbox has no host access
- **WHEN** a script calls `io.open`, `os.getenv`, `require`, `load` or `debug.sethook`
- **THEN** the call fails because the name is `nil`, and the collection fails as a script error

#### Scenario: Endless loop is stopped
- **WHEN** `select` contains `while true do end`
- **THEN** the call stops at the per-call instruction bound, the collection fails as a script error, and the server keeps serving the previous generation

#### Scenario: Long C call is stopped
- **WHEN** `select` runs one `string.find` that takes longer than the wall-time bound
- **THEN** the worker is terminated, the collection fails as a script error, and the main thread kept serving requests during the call

#### Scenario: Memory bomb is stopped
- **WHEN** `select` builds a table of 16 MiB of strings
- **THEN** allocation fails at the Lua memory bound and the collection fails as a script error

#### Scenario: Repeated collections do not leak
- **WHEN** an operator triggers 50 consecutive forced refreshes with unchanged inputs and the default script
- **THEN** resident memory after the last refresh settles within the steady-state baseline rather than trending upward
