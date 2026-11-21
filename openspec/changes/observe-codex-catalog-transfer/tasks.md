## 1. Build the bounded observer

- [x] 1.1 Implement the temporary local receiver with private permissions, a strict event schema, bounded storage and explicit coverage/fault reporting. Do not persist datagrams or forward them to the host journal.
- [x] 1.2 Rehearse against the pinned front image: intact and truncated identity/gzip responses; matching access IDs; unrelated, malformed and sensitive inputs dropped; ordinary log stays at `warn`.
- [x] 1.3 Rehearse early rollback, expiry, receiver failure, controller loss and intervening configuration drift. Verify restoration and removal without overwriting unrelated work.

## 2. Open the window

- [ ] 2.1 Obtain green CI and independent review. Verify one writer, protected backups, a fresh preview, rotation/headroom, receiver readiness and the armed host-side rollback before activation.
- [ ] 2.2 Apply only the reviewed logging delta. Read back the two destinations, `10s` timeout, baseline health and unrelated container identities. Record verified UTC start and hard stop. Generate no production requests.

## 3. Close and report

- [ ] 3.1 Observe independent natural catalog failure signals and native events. Classify with front access ID, sent bytes and timing; record coverage gaps. No timeout change or automatic mitigation.
- [ ] 3.2 At the first confirmed transfer failure, deadline or fault, restore the baseline, remove temporary resources, verify readback, and record the result (including inconclusive results). Validate, review the receipt and archive only after verified closure.
