#!/usr/bin/env bash
# Fixture rehearsal for deploy-ws-keepalive / rollback-ws-keepalive.
# Fake compose/runtime harness; exercises plan diff, approved-digest gate,
# pre-upload pull, scoped recreate, image/revision/health verification,
# protected-container drift, recovery, and no-op idempotency.
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
mkdir -p "$state" "$deploy/.ai-ops-state" "$private"

cat >"$tmp/inventory" <<'EOF'
[gateway]
fixture ansible_connection=local ansible_host=127.0.0.1
EOF

IMAGE="ghcr.io/xz-dev/ai-sse-keepalive-proxy@sha256:deadd00d"
OLD_IMAGE="ghcr.io/xz-dev/ai-sse-keepalive-proxy@sha256:11111111"
REVISION="4467d5a2dd54a0b62b23a530f18fdfdacb52d145"

cat >"$tmp/fake-runtime" <<EOF
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "\$*" >>"$state/runtime-calls"
case "\${1:-}" in
  version) echo fixture-runtime ;;
  pull) echo "pulled" ;;
  inspect)
    shift
    case "\$*" in
      *revision*) echo "$REVISION" ;;
      # container inspect: return env-bound image ID (stale overrides)
      *) if [[ -f "$state/stale" ]]; then echo sha256:stale-id; else cat "$state/current-image-id" 2>/dev/null || echo sha256:img-id-old; fi ;;
    esac ;;
  image)
    # image inspect <ref>: return the pulled image ID for that ref
    shift
    case "\$*" in
      *deadd00d*) echo sha256:img-id-new ;;
      *) echo sha256:img-id-old ;;
    esac ;;
  *) exit 2 ;;
esac
EOF
chmod +x "$tmp/fake-runtime"

cat >"$tmp/fake-compose" <<PY
#!/usr/bin/env python3
import json, os, sys
from pathlib import Path

state = Path(os.environ["FAKE_STATE"])
deploy = Path(os.environ["FAKE_DEPLOY_ROOT"])
mode = os.environ.get("FAKE_MODE", "success")
args = sys.argv[1:]
with (state / "compose-calls").open("a") as out:
    out.write(f"mode={mode} " + " ".join(args) + "\n")

envfile = deploy / ".env"
envtext = envfile.read_text() if envfile.exists() else ""

protected = ["ai-sse-keepalive-proxy-netns", "apisix-ai-sse-relay",
             "ai-sse-sub2api-relay", "sub2api"]

def activations():
    p = state / "activation-count"
    return int(p.read_text() or "0") if p.exists() else 0

def bump():
    p = state / "activation-count"
    p.write_text(str(activations() + 1))

def healthy():
    if mode == "recovery-fails":
        return False
    if mode == "candidate-unready":
        # candidate container fails readiness; recovered (old) container is fine
        return not (state / "candidate-live").exists()
    return True

for i, arg in enumerate(args):
    if arg in {"ps", "exec", "up", "config", "pull"}:
        command = arg
        rest = args[i + 1 :]
        break
else:
    raise SystemExit(2)

if command == "ps":
    if "-q" in rest:
        service = rest[-1]
        if service == "ai-sse-keepalive-proxy":
            print(f"wsk-container-v{activations()}")
        elif service in protected:
            if mode == "protected-drift" and service == "apisix-ai-sse-relay" and activations() > 0:
                print(f"{service}-drifted")
            else:
                print(f"{service}-stable")
        else:
            raise SystemExit(0)
    else:
        print(json.dumps({"Service": "ai-sse-keepalive-proxy", "State": "running"}))
elif command == "up":
    bump()
    # candidate (NEW digest) is unready; restored OLD env is fine
    if mode == "candidate-unready" and "deadd00d" in envtext:
        (state / "candidate-live").write_text("1")
    else:
        (state / "candidate-live").unlink(missing_ok=True)
    # a recreate converges the running image to the env-selected digest
    img = next((l.split("=",1)[1] for l in envtext.splitlines() if l.startswith("AI_SSE_KEEPALIVE_PROXY_IMAGE=")), "")
    (state / "current-image").write_text(img)
    (state / "current-image-id").write_text("sha256:img-id-new" if "deadd00d" in img else "sha256:img-id-old")
    (state / "stale").unlink(missing_ok=True)
elif command == "exec":
    service = rest[1]
    if service != "ai-sse-keepalive-proxy":
        raise SystemExit(2)
    if rest[2:3] == ["/ai-sse-keepalive-proxy"]:
        if "healthcheck" in rest:
            raise SystemExit(0 if healthy() else 1)
    raise SystemExit(2)
elif command == "config":
    print(json.dumps({"services": {"ai-sse-keepalive-proxy": {"image": "$IMAGE", "pull_policy": "never"}}}))
elif command == "pull":
    pass
PY
chmod +x "$tmp/fake-compose"

write_env() { printf 'AI_SSE_KEEPALIVE_PROXY_IMAGE=%s\nAI_SSE_KEEPALIVE_PROXY_PULL_POLICY=never\nWS_PING_INTERVAL=15s\n' "$IMAGE" >"$1"; }
# shellcheck disable=SC2016 # literal ${...} written into fixture compose on purpose
write_compose() { printf 'services:\n  ai-sse-keepalive-proxy:\n    image: ${AI_SSE_KEEPALIVE_PROXY_IMAGE}\n' >"$1"; }
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
      -e service=ai-sse-keepalive-proxy \
      -e ai_ops_ws_keepalive_verify_retries=2 \
      -e ai_ops_ws_keepalive_verify_delay_seconds=0 \
      "$@" >"$output" 2>&1
}

DIGEST="${IMAGE##*@}"

# --- Plan: shows desired image/digest/protected map, no mutation ---
write_env "$deploy/.env"
write_env "$private/.env"
write_compose "$deploy/compose.yaml"
write_compose "$private/compose.yaml"
clear_calls
playbook deploy-ws-keepalive plan "$tmp/plan.out"
grep -q 'desired_image' "$tmp/plan.out"
grep -q 'ai-sse-keepalive-proxy' "$tmp/plan.out"
assert_no_match ' pull ' "$state/runtime-calls"
assert_no_match ' up ' "$state/compose-calls"

# --- Apply without approved digest: refused ---
clear_calls
if playbook deploy-ws-keepalive apply "$tmp/noauth.out"; then
    echo 'unapproved digest unexpectedly succeeded' >&2
    exit 1
fi
grep -q 'not approved or became stale' "$tmp/noauth.out"
assert_no_match ' pull ' "$state/runtime-calls"
assert_no_match ' up ' "$state/compose-calls"

# --- Approved apply: pull digest, recreate only the proxy, verify ---
# Remote still carries the OLD digest so upload->activate->verify fires.
printf 'AI_SSE_KEEPALIVE_PROXY_IMAGE=%s\nAI_SSE_KEEPALIVE_PROXY_PULL_POLICY=never\nWS_PING_INTERVAL=15s\n' "$OLD_IMAGE" >"$deploy/.env"
rm -f "$deploy/.ai-ops-state/.env.baseline" "$deploy/.ai-ops-state/compose.yaml.baseline"
write_env "$private/.env"
write_compose "$deploy/compose.yaml"
write_compose "$private/compose.yaml"
clear_calls
playbook deploy-ws-keepalive apply "$tmp/apply.out" \
  -e "ai_ops_ws_keepalive_approved_digest=$DIGEST" \
  -e "ai_ops_ws_keepalive_expected_revision=$REVISION"
grep -q "pull $IMAGE" "$state/runtime-calls" || grep -q "pull.*sha256" "$state/runtime-calls"
grep -q 'up -d --no-deps --no-build --pull never ai-sse-keepalive-proxy' "$state/compose-calls"
assert_no_match ' up .*sub2api' "$state/compose-calls"
assert_no_match ' up .*apisix-ai-sse-relay' "$state/compose-calls"
receipt=$(find "$deploy/.ai-ops-state" -name 'receipt-ws-keepalive-*.txt' | head -1)
grep -q 'operation=ws-keepalive-deploy' "$receipt"
test "$(stat -c %a "$receipt")" = 600

# --- No-op: remote already matches private; nothing activated ---
# Runtime is already converged on the new image (container runs img-id-new).
write_env "$deploy/.env"
write_env "$private/.env"
write_compose "$deploy/compose.yaml"
write_compose "$private/compose.yaml"
rm -f "$deploy/.ai-ops-state/.env.baseline"
clear_calls
echo sha256:img-id-new >"$state/current-image-id"
playbook deploy-ws-keepalive apply "$tmp/noop.out" \
  -e "ai_ops_ws_keepalive_approved_digest=$DIGEST" \
  -e "ai_ops_ws_keepalive_expected_revision=$REVISION"
assert_no_match ' up ' "$state/compose-calls"

# --- Protected container drift fails the deploy ---
# Remote still carries the OLD digest (pre-deploy baseline = OLD) so
# upload->activate->verify fires; protected map drifts during verify.
printf 'AI_SSE_KEEPALIVE_PROXY_IMAGE=%s\nAI_SSE_KEEPALIVE_PROXY_PULL_POLICY=never\nWS_PING_INTERVAL=15s\n' "$OLD_IMAGE" >"$deploy/.env"
printf 'AI_SSE_KEEPALIVE_PROXY_IMAGE=%s\nAI_SSE_KEEPALIVE_PROXY_PULL_POLICY=never\nWS_PING_INTERVAL=15s\n' "$OLD_IMAGE" >"$deploy/.ai-ops-state/.env.baseline"
write_env "$private/.env"
write_compose "$deploy/compose.yaml"
write_compose "$private/compose.yaml"
clear_calls
export FAKE_MODE=protected-drift
if playbook deploy-ws-keepalive apply "$tmp/drift.out" \
  -e "ai_ops_ws_keepalive_approved_digest=$DIGEST" \
  -e "ai_ops_ws_keepalive_expected_revision=$REVISION"; then
    echo 'protected-drift unexpectedly succeeded' >&2
    exit 1
fi
unset FAKE_MODE
grep -q 'protected container' "$tmp/drift.out" || grep -q 'apisix-ai-sse-relay' "$tmp/drift.out"

# --- Candidate readiness failure: restore old .env, recovery verify passes ---
printf 'AI_SSE_KEEPALIVE_PROXY_IMAGE=%s\nAI_SSE_KEEPALIVE_PROXY_PULL_POLICY=never\nWS_PING_INTERVAL=15s\n' "$OLD_IMAGE" >"$deploy/.env"
printf 'AI_SSE_KEEPALIVE_PROXY_IMAGE=%s\nAI_SSE_KEEPALIVE_PROXY_PULL_POLICY=never\nWS_PING_INTERVAL=15s\n' "$OLD_IMAGE" >"$deploy/.ai-ops-state/.env.baseline"
write_env "$private/.env"
write_compose "$deploy/compose.yaml"
write_compose "$private/compose.yaml"
clear_calls
rm -f "$deploy/.ai-ops-state"/receipt-ws-keepalive-failed-*.txt
export FAKE_MODE=candidate-unready
if playbook deploy-ws-keepalive apply "$tmp/fail.out" \
  -e "ai_ops_ws_keepalive_approved_digest=$DIGEST" \
  -e "ai_ops_ws_keepalive_expected_revision=$REVISION"; then
    echo 'unready candidate unexpectedly succeeded' >&2
    exit 1
fi
unset FAKE_MODE
grep -q 'recovery verification=passed' "$tmp/fail.out"
receipt=$(find "$deploy/.ai-ops-state" -name 'receipt-ws-keepalive-failed-*.txt' | head -1)
grep -q 'outcome=candidate-failed-recovered' "$receipt"
test "$(stat -c %a "$receipt")" = 600

# --- Recovery also unready: recovery-required ---
printf 'AI_SSE_KEEPALIVE_PROXY_IMAGE=%s\nAI_SSE_KEEPALIVE_PROXY_PULL_POLICY=never\nWS_PING_INTERVAL=15s\n' "$OLD_IMAGE" >"$deploy/.env"
printf 'AI_SSE_KEEPALIVE_PROXY_IMAGE=%s\nAI_SSE_KEEPALIVE_PROXY_PULL_POLICY=never\nWS_PING_INTERVAL=15s\n' "$OLD_IMAGE" >"$deploy/.ai-ops-state/.env.baseline"
write_env "$private/.env"
write_compose "$deploy/compose.yaml"
write_compose "$private/compose.yaml"
clear_calls
rm -f "$deploy/.ai-ops-state"/receipt-ws-keepalive-failed-*.txt
export FAKE_MODE=recovery-fails
if playbook deploy-ws-keepalive apply "$tmp/recfail.out" \
  -e "ai_ops_ws_keepalive_approved_digest=$DIGEST" \
  -e "ai_ops_ws_keepalive_expected_revision=$REVISION"; then
    echo 'recovery-fails unexpectedly succeeded' >&2
    exit 1
fi
unset FAKE_MODE
grep -q 'recovery verification=failed' "$tmp/recfail.out"
receipt=$(find "$deploy/.ai-ops-state" -name 'receipt-ws-keepalive-failed-*.txt' | head -1)
grep -q 'outcome=recovery-required' "$receipt"

# --- Stale runtime container with matching files: corrected ---
# Files match (no upload) but the RUNNING image identity has drifted
# (fixture inspect returns the OLD digest under stale-runtime mode).
# The apply must still converge: pull + scoped recreate to restore identity.
write_env "$deploy/.env"
write_env "$private/.env"
write_compose "$deploy/compose.yaml"
write_compose "$private/compose.yaml"
cp "$deploy/.env" "$deploy/.ai-ops-state/.env.baseline"
cp "$deploy/compose.yaml" "$deploy/.ai-ops-state/compose.yaml.baseline"
clear_calls
: >"$state/stale"
export FAKE_MODE=stale-runtime
playbook deploy-ws-keepalive apply "$tmp/stale.out" \
  -e "ai_ops_ws_keepalive_approved_digest=$DIGEST" \
  -e "ai_ops_ws_keepalive_expected_revision=$REVISION"
unset FAKE_MODE
grep -q ' up -d --no-deps --no-build --pull never ai-sse-keepalive-proxy' "$state/compose-calls"
grep -q 'runtime drifted' "$tmp/stale.out" || grep -q 'observed=sha256:11111111' "$tmp/stale.out"

echo "ws-keepalive fixture: all checks passed"
