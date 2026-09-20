#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
if [[ ${KEEP_TMP:-0} == 1 ]]; then
    echo "fixture_tmp=$tmp" >&2
else
    trap 'rm -rf "$tmp"' EXIT
fi

state="$tmp/state"
deploy="$tmp/deploy"
private="$tmp/private"
mkdir -p "$state" "$deploy/.ai-ops-state" "$deploy/cpa-model-sync" "$private/cpa-model-sync"

cat >"$tmp/inventory" <<'EOF'
[gateway]
fixture ansible_connection=local ansible_host=127.0.0.1
EOF

cat >"$tmp/fake-runtime" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$FAKE_STATE/runtime-calls"
case "${1:-}" in
  version) echo fixture-runtime ;;
  inspect) echo sha256:fixture-image ;;
  *) exit 2 ;;
esac
EOF
chmod +x "$tmp/fake-runtime"

cat >"$tmp/fake-compose" <<'PY'
#!/usr/bin/env python3
import hashlib
import json
import os
import sys
from pathlib import Path

state = Path(os.environ["FAKE_STATE"])
deploy = Path(os.environ["FAKE_DEPLOY_ROOT"])
mode = os.environ.get("FAKE_MODE", "success")
args = sys.argv[1:]
with (state / "compose-calls").open("a") as out:
    out.write(f"mode={mode} " + " ".join(args) + "\n")

for i, arg in enumerate(args):
    if arg in {"ps", "exec", "up", "logs"}:
        command = arg
        rest = args[i + 1 :]
        break
else:
    raise SystemExit(2)

policy_path = deploy / "cpa-model-sync/config.json"
policy = policy_path.read_bytes() if policy_path.exists() else b""
is_old = b"^OLD$" in policy

old_state = "1" * 64
new_state = "2" * 64
bad_state = "3" * 64
source_state = "4" * 64
image = "sha256:fixture-image"

def preview(data: bytes):
    try:
        value = json.loads(data)
    except json.JSONDecodeError:
        raise SystemExit(2)
    if "unknown" in value or value.get("requests", {}).get("timeout_ms") == 0:
        raise SystemExit(2)
    include = value.get("channels", {}).get("fixture", {}).get("include", [])
    if "[" in include:
        raise SystemExit(2)
    old = "^OLD$" in include
    policy_digest = hashlib.sha256(data).hexdigest()
    current = old_state if is_old else new_state
    desired = old_state if old else new_state
    additions = [] if current == desired else (["OLD"] if old else ["NEW"])
    removals = [] if current == desired else (["NEW"] if old else ["OLD"])
    approval_input = f"{policy_digest}:{image}:{current}:{source_state}:{desired}"
    approval = hashlib.sha256(approval_input.encode()).hexdigest()
    result = {
        "version": 1,
        "policy_digest": policy_digest,
        "image_identity": image,
        "current_state_digest": current,
        "source_state_digest": source_state,
        "desired_state_digest": desired,
        "approval_digest": approval,
        "channels": [{
            "kind": "openai-compatibility", "prefix": "fixture", "skipped": False,
            "current_count": 1, "source_count": 2, "desired_count": 1,
            "additions": additions, "removals": removals, "unchanged": 0 if additions else 1,
            "current_digest": current, "source_digest": source_state,
            "desired_digest": desired,
        }],
    }
    (state / "preview.json").write_text(json.dumps(result))
    return result

if command == "ps":
    if "-q" in rest:
        if mode == "candidate-exit" and not is_old:
            raise SystemExit(0)
        print("fixture-container")
    else:
        print(json.dumps({"Service": "cpa-model-sync", "State": "running"}))
elif command == "up":
    (state / "activation-count").write_text(str(int((state / "activation-count").read_text() or "0") + 1) if (state / "activation-count").exists() else "1")
elif command == "logs":
    if not is_old:
        if mode == "candidate-no-summary":
            print("starting")
        elif mode == "candidate-malformed":
            print("{not-json")
        elif mode == "candidate-failed":
            print(json.dumps({"updated": 0, "unchanged": 0, "failed": 1, "unconfirmed": 0, "skipped": 0}))
        elif mode == "candidate-unconfirmed":
            print(json.dumps({"updated": 0, "unchanged": 0, "failed": 0, "unconfirmed": 1, "skipped": 0}))
        else:
            print(json.dumps({"updated": 1, "unchanged": 0, "failed": 0, "unconfirmed": 0, "skipped": 0}))
    else:
        print(json.dumps({"updated": 1, "unchanged": 0, "failed": 0, "unconfirmed": 0, "skipped": 0}))
elif command == "exec":
    data = sys.stdin.buffer.read() if rest[-1:] == ["-"] else policy
    result = preview(data)
    if mode in {"candidate-mismatch", "recovery-mismatch"} and not is_old:
        result["current_state_digest"] = bad_state
    if mode == "recovery-mismatch" and is_old and (state / "activation-count").exists():
        if int((state / "activation-count").read_text()) >= 2:
            result["current_state_digest"] = bad_state
    print(json.dumps(result, sort_keys=True, separators=(",", ":")))
PY
chmod +x "$tmp/fake-compose"

cat >"$tmp/no-hook.yml" <<'EOF'
---
- name: deploy-file compatibility without verification hook
  hosts: gateway
  gather_facts: false
  roles:
    - role: ai_ops
  tasks:
    - ansible.builtin.include_role:
        name: ai_ops
        tasks_from: deploy-file
      vars:
        ai_file_label: no-hook.txt
        ai_file_local: "{{ fixture_local }}"
        ai_file_remote: "{{ fixture_remote }}"
        ai_activate_cmd: /bin/true
EOF

old='{"cpa_url":"http://fixture","client_version":"test","channels":{"fixture":{"include":["^OLD$"]}}}'
new='{"cpa_url":"http://fixture","client_version":"test","channels":{"fixture":{"include":["^NEW$"]}}}'

write_old() { printf '%s' "$old" >"$deploy/cpa-model-sync/config.json"; }
write_new_local() { printf '%s' "$new" >"$private/cpa-model-sync/config.json"; }
set_baseline_old() { printf '%s' "$old" >"$deploy/.ai-ops-state/cpa-model-sync__config.json.baseline"; }
clear_calls() { : >"$state/compose-calls"; : >"$state/runtime-calls"; rm -f "$state/activation-count"; }
assert_no_match() {
    local pattern=$1 file=$2
    if grep -Eq "$pattern" "$file"; then
        echo "unexpected match '$pattern' in $file" >&2
        exit 1
    fi
}

playbook() {
    local tag=$1 mode_value=$2 output=$3
    shift 3
    FAKE_STATE="$state" FAKE_DEPLOY_ROOT="$deploy" FAKE_MODE="${FAKE_MODE:-success}" \
    ansible-playbook "$root/ansible/ops.yml" -i "$tmp/inventory" --tags "$tag" \
      -e "ai_ops_config_dir=$private" \
      -e "ai_ops_deploy_root=$deploy" \
      -e "ai_ops_compose_cmd=$tmp/fake-compose" \
      -e "ai_ops_runtime=$tmp/fake-runtime" \
      -e "ai_ops_mode=$mode_value" \
      -e ai_ops_model_sync_verify_retries=1 \
      -e ai_ops_model_sync_verify_delay_seconds=0 \
      "$@" >"$output" 2>&1
}

# Existing deploy-file callers retain upload/activation/baseline behavior without a verifier hook.
printf old >"$deploy/no-hook.txt"
printf old >"$deploy/.ai-ops-state/no-hook.txt.baseline"
printf new >"$private/no-hook.txt"
ANSIBLE_ROLES_PATH="$root/ansible/roles" ansible-playbook "$tmp/no-hook.yml" -i "$tmp/inventory" \
  -e "ai_ops_deploy_root=$deploy" -e ai_ops_mode=apply \
  -e "fixture_local=$private/no-hook.txt" -e "fixture_remote=$deploy/no-hook.txt" \
  >"$tmp/no-hook.out" 2>&1
cmp -s "$deploy/no-hook.txt" <(printf new)
cmp -s "$deploy/.ai-ops-state/no-hook.txt.baseline" <(printf new)

# Plan: show complete changed IDs, no upload or recreation.
write_old
write_new_local
set_baseline_old
clear_calls
playbook deploy-model-sync plan "$tmp/plan.out"
grep -q 'NEW' "$tmp/plan.out"
grep -q 'OLD' "$tmp/plan.out"
cmp -s "$deploy/cpa-model-sync/config.json" <(printf '%s' "$old")
assert_no_match ' up ' "$state/compose-calls"
approval=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["approval_digest"])' "$state/preview.json")

# Stale approval blocks before upload.
clear_calls
if playbook deploy-model-sync apply "$tmp/stale.out" -e ai_ops_model_sync_approved_digest="$(printf 'f%.0s' {1..64})"; then
    echo 'stale approval unexpectedly succeeded' >&2
    exit 1
fi
cmp -s "$deploy/cpa-model-sync/config.json" <(printf '%s' "$old")
assert_no_match ' up ' "$state/compose-calls"

# Approved apply uploads, recreates only the sidecar, and verifies read-back.
clear_calls
FAKE_MODE=success playbook deploy-model-sync apply "$tmp/apply.out" -e ai_ops_model_sync_approved_digest="$approval"
cmp -s "$deploy/cpa-model-sync/config.json" <(printf '%s' "$new")
cmp -s "$deploy/.ai-ops-state/cpa-model-sync__config.json.baseline" <(printf '%s' "$new")
grep -q 'up -d --force-recreate --no-deps --no-build cpa-model-sync' "$state/compose-calls"
# The sidecar (65534:65534) must always be able to read the uploaded policy,
# regardless of the local private file's mode.
printf '%s' "$new" >"$private/cpa-model-sync/config.json"
chmod 600 "$private/cpa-model-sync/config.json"
write_old
set_baseline_old
clear_calls
FAKE_MODE=success playbook deploy-model-sync plan "$tmp/mode-plan.out" >/dev/null
approval_mode=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["approval_digest"])' "$state/preview.json")
FAKE_MODE=success playbook deploy-model-sync apply "$tmp/mode-apply.out" -e ai_ops_model_sync_approved_digest="$approval_mode"
test "$(stat -c %a "$deploy/cpa-model-sync/config.json")" = 644
printf '%s' "$new" >"$private/cpa-model-sync/config.json"
receipt=$(find "$deploy/.ai-ops-state" -name 'receipt-model-sync-*.txt' | head -1)
grep -q 'outcome=success' "$receipt"
assert_no_match 'synthetic-management|additions|removals|NEW|OLD' "$receipt"
test "$(stat -c %a "$receipt")" = 600
test "$(stat -c %a "$deploy/.ai-ops-state/model-sync-transition.json")" = 600

# First adoption and a second no-op establish/retain baseline without preview or recreation.
write_old
printf '%s' "$old" >"$private/cpa-model-sync/config.json"
rm -f "$deploy/.ai-ops-state/cpa-model-sync__config.json.baseline"
clear_calls
playbook deploy-model-sync apply "$tmp/adopt.out"
cmp -s "$deploy/.ai-ops-state/cpa-model-sync__config.json.baseline" <(printf '%s' "$old")
assert_no_match 'exec -T cpa-model-sync' "$state/compose-calls"
assert_no_match ' up ' "$state/compose-calls"
clear_calls
playbook deploy-model-sync apply "$tmp/noop.out"
assert_no_match 'exec -T cpa-model-sync' "$state/compose-calls"
assert_no_match ' up ' "$state/compose-calls"

# Candidate failures restore old policy and prove recovery; baseline remains old.
for failure in candidate-mismatch candidate-no-summary candidate-malformed candidate-failed candidate-unconfirmed candidate-exit; do
    write_old
    write_new_local
    set_baseline_old
    clear_calls
    FAKE_MODE=success playbook deploy-model-sync plan "$tmp/$failure-plan.out"
    approval=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["approval_digest"])' "$state/preview.json")
    clear_calls
    rm -f "$deploy/.ai-ops-state"/receipt-model-sync-failed-*.txt
    export FAKE_MODE=$failure
    if playbook deploy-model-sync apply "$tmp/$failure.out" -e ai_ops_model_sync_approved_digest="$approval"; then
        echo "$failure unexpectedly succeeded" >&2
        exit 1
    fi
    unset FAKE_MODE
    cmp -s "$deploy/cpa-model-sync/config.json" <(printf '%s' "$old")
    cmp -s "$deploy/.ai-ops-state/cpa-model-sync__config.json.baseline" <(printf '%s' "$old")
    test "$(cat "$state/activation-count")" -eq 2
    grep -q 'recovery verification=passed' "$tmp/$failure.out"
    failure_receipt=$(find "$deploy/.ai-ops-state" -name 'receipt-model-sync-failed-*.txt' | head -1)
    grep -q 'outcome=candidate-failed-recovered' "$failure_receipt"
    test "$(stat -c %a "$failure_receipt")" = 600
done

# Recovery mismatch is reported, never success.
write_old
write_new_local
set_baseline_old
clear_calls
FAKE_MODE=success playbook deploy-model-sync plan "$tmp/recovery-plan.out"
approval=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["approval_digest"])' "$state/preview.json")
clear_calls
rm -f "$deploy/.ai-ops-state"/receipt-model-sync-failed-*.txt
export FAKE_MODE=recovery-mismatch
if playbook deploy-model-sync apply "$tmp/recovery-mismatch.out" -e ai_ops_model_sync_approved_digest="$approval"; then
    echo 'recovery mismatch unexpectedly succeeded' >&2
    exit 1
fi
unset FAKE_MODE
grep -q 'recovery verification=failed' "$tmp/recovery-mismatch.out"
cmp -s "$deploy/cpa-model-sync/config.json" <(printf '%s' "$old")
recovery_receipt=$(find "$deploy/.ai-ops-state" -name 'receipt-model-sync-failed-*.txt' | head -1)
grep -q 'outcome=recovery-required' "$recovery_receipt"

# Invalid policy fails during read-only preview and never uploads.
write_old
set_baseline_old
printf '%s' '{invalid' >"$private/cpa-model-sync/config.json"
clear_calls
if playbook deploy-model-sync plan "$tmp/invalid.out"; then
    echo 'invalid policy unexpectedly succeeded' >&2
    exit 1
fi
cmp -s "$deploy/cpa-model-sync/config.json" <(printf '%s' "$old")
assert_no_match ' up ' "$state/compose-calls"

# Guarded manual rollback restores the protected policy and proves old CPA state.
printf '%s' "$new" >"$deploy/cpa-model-sync/config.json"
printf '%s' "$new" >"$deploy/.ai-ops-state/cpa-model-sync__config.json.baseline"
printf '%s' "$old" >"$deploy/.ai-ops-state/cpa-model-sync__config.json.predeploy"
python3 - "$deploy/.ai-ops-state/model-sync-transition.json" <<'PY'
import json, sys
json.dump({"current_state_digest":"1"*64,"image_identity":"sha256:fixture-image"}, open(sys.argv[1], "w"))
PY
clear_calls
playbook rollback-model-sync apply "$tmp/rollback.out"
cmp -s "$deploy/cpa-model-sync/config.json" <(printf '%s' "$old")
cmp -s "$deploy/.ai-ops-state/cpa-model-sync__config.json.baseline" <(printf '%s' "$old")

# Independent remote drift blocks rollback before overwrite or recreation.
printf '%s' "$new" >"$deploy/.ai-ops-state/cpa-model-sync__config.json.baseline"
printf '%s' '{"independent":true}' >"$deploy/cpa-model-sync/config.json"
clear_calls
if playbook rollback-model-sync apply "$tmp/rollback-drift.out"; then
    echo 'rollback over independent drift unexpectedly succeeded' >&2
    exit 1
fi
grep -q 'changed independently' "$tmp/rollback-drift.out"
cmp -s "$deploy/cpa-model-sync/config.json" <(printf '%s' '{"independent":true}')
assert_no_match ' up ' "$state/compose-calls"

echo 'ansible_model_sync=passed'
