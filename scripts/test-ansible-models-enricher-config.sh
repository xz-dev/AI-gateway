#!/usr/bin/env bash
# Fixture rehearsal for deploy-models-enricher-config / rollback-models-enricher-config.
# Uses a fake compose/runtime harness; exercises drift gate, candidate validation,
# force-recreate scope, digest/readiness verification, recovery, and rollback.
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
mkdir -p "$state" "$deploy/.ai-ops-state" "$deploy/models-enricher" "$private/models-enricher"

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
    if arg in {"ps", "exec", "up", "logs", "config"}:
        command = arg
        rest = args[i + 1 :]
        break
else:
    raise SystemExit(2)

config_path = deploy / "models-enricher/config.yaml"
config = config_path.read_bytes() if config_path.exists() else b""
old = b"OLD-CONFIG" in config
new = b"NEW-CONFIG" in config
protected_services = [
    "models-enricher-netns", "enricher-cpa-relay", "enricher-squid-relay",
    "apisix-models-enricher-relay", "apisix-models",
]

def validate(data: bytes):
    """Simulate /models-enricher validate-config: reject malformed/invalid."""
    try:
        text = data.decode()
    except UnicodeDecodeError:
        raise SystemExit(1)
    if "cpa_base_url" not in text:
        raise SystemExit(1)
    if "[invalid-regex" in text or "INVALID" in text:
        raise SystemExit(1)
    digest = hashlib.sha256(data).hexdigest()
    return {"version": 1, "config_digest": digest}

def healthcheck_ok():
    """Candidate container readiness: new config fails readiness in failure modes."""
    if mode == "recovery-fails":
        return False
    if mode == "candidate-unready":
        return old
    return True

if command == "ps":
    if "-q" in rest:
        service = rest[-1]
        if service == "models-enricher":
            if mode == "candidate-exit" and new:
                raise SystemExit(0)  # empty output: container gone
            # Fresh ID after each activation: count `up` calls.
            activations = int((state / "activation-count").read_text() or "0") if (state / "activation-count").exists() else 0
            print(f"me-container-v{activations}")
        elif service in protected_services:
            activations = int((state / "activation-count").read_text() or "0") if (state / "activation-count").exists() else 0
            if mode == "protected-drift" and service == "enricher-cpa-relay" and activations > 0:
                print(f"{service}-drifted")
            else:
                print(f"{service}-stable")
        else:
            raise SystemExit(2)
    else:
        print(json.dumps({"Service": "models-enricher", "State": "running"}))
elif command == "up":
    n = int((state / "activation-count").read_text() or "0") if (state / "activation-count").exists() else 0
    (state / "activation-count").write_text(str(n + 1))
elif command == "exec":
    # rest: -T <service> <cmd...>
    service = rest[1]
    cmd = rest[2:]
    if service != "models-enricher":
        raise SystemExit(2)
    if cmd[:2] == ["/models-enricher", "validate-config"]:
        if cmd[2] == "-":
            data = sys.stdin.buffer.read()
        else:
            data = config
        print(json.dumps(validate(data), sort_keys=True, separators=(",", ":")))
    elif cmd[:2] == ["/models-enricher", "healthcheck"]:
        raise SystemExit(0 if healthcheck_ok() else 1)
    else:
        raise SystemExit(2)
elif command == "config":
    print(json.dumps({"services": {"models-enricher": {"image": "fixture-image"}}}))
elif command == "logs":
    print("starting")
PY
chmod +x "$tmp/fake-compose"

old_cfg='cpa_base_url: http://cpa
# OLD-CONFIG
channels:
  demo: {fetch_models: false}
'
new_cfg='cpa_base_url: http://cpa
# NEW-CONFIG
channels:
  demo: {fetch_models: false, source_priority: [models.dev/openai]}
'
bad_cfg='cpa_base_url: http://cpa
channels:
  demo: {exclude: ["[invalid-regex"]}
'

write_old() { printf '%s' "$old_cfg" >"$deploy/models-enricher/config.yaml"; }
write_new_local() { printf '%s' "$new_cfg" >"$private/models-enricher/config.yaml"; }
set_baseline_old() { printf '%s' "$old_cfg" >"$deploy/.ai-ops-state/models-enricher__config.yaml.baseline"; }
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
      -e ai_ops_models_enricher_verify_retries=2 \
      -e ai_ops_models_enricher_verify_delay_seconds=0 \
      "$@" >"$output" 2>&1
}

# --- Plan: validate candidate, show diff, no upload or recreation ---
write_old
write_new_local
set_baseline_old
clear_calls
playbook deploy-models-enricher-config plan "$tmp/plan.out"
grep -q 'NEW-CONFIG' "$tmp/plan.out"           # diff shown
grep -q 'config_digest' "$tmp/plan.out"         # validation digest shown
cmp -s "$deploy/models-enricher/config.yaml" <(printf '%s' "$old_cfg")
assert_no_match ' up ' "$state/compose-calls"

# --- Approved apply: upload, recreate only models-enricher, verify ---
clear_calls
playbook deploy-models-enricher-config apply "$tmp/apply.out"
cmp -s "$deploy/models-enricher/config.yaml" <(printf '%s' "$new_cfg")
cmp -s "$deploy/.ai-ops-state/models-enricher__config.yaml.baseline" <(printf '%s' "$new_cfg")
grep -q 'up -d --force-recreate --no-deps --no-build models-enricher' "$state/compose-calls"
# Protected services were queried for identity but never recreated.
assert_no_match ' up .*enricher-cpa-relay' "$state/compose-calls"
assert_no_match ' up .*models-enricher-netns' "$state/compose-calls"
receipt=$(find "$deploy/.ai-ops-state" -name 'receipt-models-enricher-[0-9]*.txt' | head -1)
grep -q 'outcome=success' "$receipt"
assert_no_match 'OLD-CONFIG|NEW-CONFIG|cpa_base_url' "$receipt"
test "$(stat -c %a "$receipt")" = 600
test "$(stat -c %a "$deploy/models-enricher/config.yaml")" = 644

# --- No-op: matching local/remote/baseline skips everything ---
clear_calls
playbook deploy-models-enricher-config apply "$tmp/noop.out"
assert_no_match 'exec -T models-enricher' "$state/compose-calls"
assert_no_match ' up ' "$state/compose-calls"

# --- First adoption: matching local/remote establishes baseline without recreation ---
write_old
printf '%s' "$old_cfg" >"$private/models-enricher/config.yaml"
rm -f "$deploy/.ai-ops-state/models-enricher__config.yaml.baseline"
clear_calls
playbook deploy-models-enricher-config apply "$tmp/adopt.out"
cmp -s "$deploy/.ai-ops-state/models-enricher__config.yaml.baseline" <(printf '%s' "$old_cfg")
assert_no_match 'exec -T models-enricher' "$state/compose-calls"
assert_no_match ' up ' "$state/compose-calls"

# --- Invalid candidate fails validation; nothing uploaded/recreated ---
write_old
set_baseline_old
printf '%s' "$bad_cfg" >"$private/models-enricher/config.yaml"
clear_calls
if playbook deploy-models-enricher-config apply "$tmp/invalid.out"; then
    echo 'invalid config unexpectedly succeeded' >&2
    exit 1
fi
cmp -s "$deploy/models-enricher/config.yaml" <(printf '%s' "$old_cfg")
assert_no_match ' up ' "$state/compose-calls"

# --- Candidate readiness failure: restore old config, prove recovery ---
write_old
write_new_local
set_baseline_old
clear_calls
rm -f "$deploy/.ai-ops-state"/receipt-models-enricher-failed-*.txt
export FAKE_MODE=candidate-unready
if playbook deploy-models-enricher-config apply "$tmp/fail.out"; then
    echo 'unready candidate unexpectedly succeeded' >&2
    exit 1
fi
unset FAKE_MODE
cmp -s "$deploy/models-enricher/config.yaml" <(printf '%s' "$old_cfg")
cmp -s "$deploy/.ai-ops-state/models-enricher__config.yaml.baseline" <(printf '%s' "$old_cfg")
test "$(cat "$state/activation-count")" -eq 2
grep -q 'recovery verification=passed' "$tmp/fail.out"
failure_receipt=$(find "$deploy/.ai-ops-state" -name 'receipt-models-enricher-failed-*.txt' | head -1)
grep -q 'outcome=candidate-failed-recovered' "$failure_receipt"
test "$(stat -c %a "$failure_receipt")" = 600

# --- Recovery also unready: recovery-required ---
write_old
write_new_local
set_baseline_old
clear_calls
rm -f "$deploy/.ai-ops-state"/receipt-models-enricher-failed-*.txt
export FAKE_MODE=recovery-fails
if playbook deploy-models-enricher-config apply "$tmp/recfail.out"; then
    echo 'recovery-fails unexpectedly succeeded' >&2
    exit 1
fi
unset FAKE_MODE
grep -q 'recovery verification=failed' "$tmp/recfail.out"
recovery_receipt=$(find "$deploy/.ai-ops-state" -name 'receipt-models-enricher-failed-*.txt' | head -1)
grep -q 'outcome=recovery-required' "$recovery_receipt"

# --- Protected container drift during verification fails the deploy ---
write_old
write_new_local
set_baseline_old
clear_calls
export FAKE_MODE=protected-drift
if playbook deploy-models-enricher-config apply "$tmp/drift.out"; then
    echo 'protected-drift unexpectedly succeeded' >&2
    exit 1
fi
unset FAKE_MODE
grep -q 'protected container' "$tmp/drift.out" || grep -q 'enricher-cpa-relay' "$tmp/drift.out"

# --- Guarded rollback restores the protected config ---
printf '%s' "$new_cfg" >"$deploy/models-enricher/config.yaml"
printf '%s' "$new_cfg" >"$deploy/.ai-ops-state/models-enricher__config.yaml.baseline"
printf '%s' "$old_cfg" >"$deploy/.ai-ops-state/models-enricher__config.yaml.predeploy"
printf '%s' "$old_cfg" >"$private/models-enricher/config.yaml"
clear_calls
playbook rollback-models-enricher-config apply "$tmp/rollback.out"
cmp -s "$deploy/models-enricher/config.yaml" <(printf '%s' "$old_cfg")
cmp -s "$deploy/.ai-ops-state/models-enricher__config.yaml.baseline" <(printf '%s' "$old_cfg")
rollback_receipt=$(find "$deploy/.ai-ops-state" -name 'receipt-models-enricher-rollback-*.txt' | head -1)
grep -q 'outcome=success' "$rollback_receipt"

# --- Independent remote drift blocks rollback ---
printf '%s' "$new_cfg" >"$deploy/.ai-ops-state/models-enricher__config.yaml.baseline"
printf 'cpa_base_url: http://drifted\n' >"$deploy/models-enricher/config.yaml"
clear_calls
if playbook rollback-models-enricher-config apply "$tmp/rollback-drift.out"; then
    echo 'rollback over independent drift unexpectedly succeeded' >&2
    exit 1
fi
grep -q 'changed independently' "$tmp/rollback-drift.out"
cmp -s "$deploy/models-enricher/config.yaml" <(printf 'cpa_base_url: http://drifted\n')
assert_no_match ' up ' "$state/compose-calls"

echo 'ansible_models_enricher_config=passed'
