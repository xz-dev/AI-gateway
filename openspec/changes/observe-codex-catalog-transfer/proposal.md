## Why

Two public Codex catalog responses ended after HTTP 200. Existing front and tunnel logs show partial delivery but not which peer closed first. The native front timeout message reproduced offline is logged at `info`; the ordinary `warn` log cannot distinguish this mechanism.

## What Changes

- Observe natural requests for at most 48 hours. Route front nginx HTTP diagnostics to a temporary private Unix datagram receiver. Keep the ordinary Docker log at `warn`.
- Save only a timestamp, a fixed catalog event class, and a validated front request ID. Use existing access and tunnel records to investigate failures independently of the receiver.
- Prepare automatic rollback before activation; stop at the first confirmed failed catalog transfer or the deadline. Report an inconclusive result if the available evidence cannot identify the closer.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `production-operations-control`: gate temporary catalog observation on bounded retention, independent failure detection and verified removal.

## Impact

Front APISIX logging and a temporary host-owned receiver only. No API, response, timeout, credential, inference, catalog membership or routing changes. No active production requests or packet capture. [Incident evidence](../../../docs/records/codex-catalog-transfer-2026-11-20.md).
