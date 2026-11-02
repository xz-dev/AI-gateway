## MODIFIED Requirements

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
