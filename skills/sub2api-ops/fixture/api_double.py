#!/usr/bin/env python3
"""Local double demonstrating the Sub2API ops flow on a fake account store.

Flow: inspect -> propose -> approve (simulated) -> fresh read -> minimal update
-> read-back. Includes a lost-response retry that reads before re-writing.
Run: python3 skills/sub2api-ops/fixture/api_double.py  (exit 0 = pass)
"""
import copy
import json
import sys

STORE = {
    "accounts": {
        "acc-1": {
            "name": "production-main",
            "credentials": {"base_url": "http://aisix:3000/v1", "key": "sk-secret"},
            "pool": {"retry_count": 1, "timeout": 60},
            "notes": "owner: ops",
        }
    }
}


def api_read(account_id):
    return copy.deepcopy(STORE["accounts"][account_id])


def api_write(account_id, resource):
    # simulate a full-object replacement API (worst case for preservation)
    STORE["accounts"][account_id] = copy.deepcopy(resource)


def update_retry(account_id, new_retry):
    # 1. fresh read
    current = api_read(account_id)
    # 2. minimal update on the full object (preserves credentials/notes)
    current["pool"]["retry_count"] = new_retry
    # 3. write
    api_write(account_id, current)
    # 4. read back
    after = api_read(account_id)
    assert after["pool"]["retry_count"] == new_retry
    assert after["credentials"]["key"] == "sk-secret", "credentials lost"
    assert after["notes"] == "owner: ops", "unrelated field lost"
    return after


def main():
    baseline = api_read("acc-1")
    print("inspect:", json.dumps({k: v for k, v in baseline.items() if k != "credentials"}))
    proposed = 3
    print(f"propose: pool.retry_count {baseline['pool']['retry_count']} -> {proposed}")
    # approval gate simulated; in real use, stop here until the owner approves

    result = update_retry("acc-1", proposed)

    # lost-response path: read before retry instead of blind re-write
    lost = api_read("acc-1")  # simulates "did the previous write land?"
    if lost["pool"]["retry_count"] == proposed:
        print("lost-response check: write had landed; no retry needed")
    else:
        update_retry("acc-1", proposed)

    assert result["pool"]["retry_count"] == 3
    assert result["credentials"]["key"] == "sk-secret"
    assert result["notes"] == "owner: ops"
    print("fixture passed: unrelated fields preserved, read-back verified")


if __name__ == "__main__":
    main()
