#!/usr/bin/env python3
"""Provision the throwaway local Sub2API: admin login, compliance acknowledgment (local only, loopback guard),
one openai apikey account pointing at AISIX, one group, one API key. Fake credentials only (from k7/h/s2a.py)."""
import json, sys, time, urllib.request, urllib.error

BASE = "http://127.0.0.1:18090"
APIKEY = "sk-capacity-local-test-key-000000"


def req(method, path, body=None, tok=None):
    r = urllib.request.Request(BASE + path, method=method, data=None if body is None else json.dumps(body).encode(),
                               headers={"Content-Type": "application/json", **({"Authorization": "Bearer " + tok} if tok else {})})
    with urllib.request.urlopen(r, timeout=30) as f:
        return json.loads(f.read())


def main():
    assert BASE.startswith("http://127.0.0.1:"), "compliance ack refused: not a local throwaway instance"
    for i in range(30):
        try:
            login = req("POST", "/api/v1/auth/login", {"email": "admin@example.test", "password": "LocalTest-12345"})
            break
        except Exception as e:  # setup may still be seeding the admin
            time.sleep(2)
    else:
        sys.exit("s2a login failed")
    tok = login["data"].get("access_token") or login["data"].get("token")
    req("POST", "/api/v1/admin/compliance/accept", {"phrase": "I have read, understood, and agree to the Sub2API "
        "Deployment and Operation Compliance Commitment", "language": "en"}, tok)
    g = req("POST", "/api/v1/admin/groups", {"name": "cap", "platform": "openai", "rate_multiplier": 1}, tok)["data"]
    req("POST", "/api/v1/admin/accounts", {"name": "aisix", "platform": "openai", "type": "apikey", "concurrency": 10,
        "credentials": {"api_key": "capacity-test-local-key", "base_url": "http://aisix:3000"},
        # pre-run 2: the account's decisive flags match production (2026-11-04 production backup: type apikey,
        # openai_responses_mode force_responses), which is also Sub2API's raw-list measured account
        # (request_raw_list.go "Extra->ShouldUseResponsesAPI"). Without it every large request is off-list.
        "extra": {"openai_responses_mode": "force_responses"},
        "group_ids": [g["id"]]}, tok)
    req("POST", "/api/v1/keys", {"name": "cap", "group_id": g["id"], "custom_key": APIKEY}, tok)
    # standard run mode (production) bills the key's owner: give the local admin a fake balance
    user = login["data"].get("user") or req("GET", "/api/v1/auth/me", None, tok)["data"]
    req("POST", f"/api/v1/admin/users/{user['id']}/balance", {"balance": 100000, "operation": "set",
        "notes": "local capacity chain"}, tok)
    print("s2a provisioned group", g["id"])


if __name__ == "__main__":
    main()
