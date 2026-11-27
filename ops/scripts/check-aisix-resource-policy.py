#!/usr/bin/env python3
import argparse
import io
from http.client import HTTPConnection, HTTPException
import json
import sys
import tempfile
from pathlib import Path
from uuid import UUID

REQUIRED_FALLBACK_STATUSES = [400, 401, 402, 403, 404]
# Every routing group shares one retry contract; only strategy/hash_on/targets differ.
REQUIRED_ROUTING = {"retries": 0, "retry_on_429": True}
# Every direct model shares one cooldown. trigger_statuses replaces AISIX defaults,
# so it keeps them and adds CPA's target-local 400/402/403/404.
REQUIRED_COOLDOWN = {
    "enabled": True,
    "default_seconds": 60,
    "max_seconds": 300,
    "honor_retry_after": True,
    "trigger_statuses": [400, 401, 402, 403, 404, 408, 429, 500, 502, 503, 504],
}
# Per-model overrides would bypass the group contract (target retries win over routing.retries).
FORBIDDEN_DIRECT_KEYS = ("retries", "timeout", "stream_timeout")
# The single priority fallback: AISIX resolves exact names, then longer globs, then this row.
STAR = "*"
STAR_ROW = {"provider": "openai", "model_name": STAR}


class PolicyError(Exception):
    pass


def cpa_pool_reference(resources):
    version = resources.get("_format_version", "1")
    if version == "1":
        return "provider_key", "cpa-pool"
    if version != "2":
        raise PolicyError("unsupported AISIX resource format")
    providers = resources.get("provider_keys")
    if not isinstance(providers, list):
        raise PolicyError("AISIX v2 resources must contain provider keys")
    matching = [p.get("id") for p in providers if isinstance(p, dict) and p.get("display_name") == "cpa-pool"]
    if len(matching) == 1 and isinstance(matching[0], str):
        try:
            if str(UUID(matching[0])) == matching[0]:
                return "provider_key_id", matching[0]
        except ValueError:
            pass
    raise PolicyError("AISIX v2 resources must declare one cpa-pool with a canonical UUID")


def validate(resources):
    if not isinstance(resources, dict):
        raise PolicyError("AISIX resources must be an object")
    models = resources.get("models")
    if not isinstance(models, list):
        raise PolicyError("AISIX resources must contain a models list")
    ref_field, ref_value = cpa_pool_reference(resources)
    star_row = STAR_ROW | {ref_field: ref_value}
    routes = 0
    stars = 0
    for model in models:
        if not isinstance(model, dict):
            raise PolicyError("every AISIX model must be an object")
        routing = model.get("routing")
        name = model.get("display_name")
        if not isinstance(name, str) or not name:
            name = "<unknown>"
        if routing is None:
            if name == STAR:
                stars += 1
                for key, value in star_row.items():
                    if model.get(key) != value:
                        raise PolicyError(f"fallback model * must declare {key}: {value}")
            if model.get("cooldown") != REQUIRED_COOLDOWN:
                raise PolicyError(f"direct model {name} must use the shared cooldown policy")
            for key in FORBIDDEN_DIRECT_KEYS:
                if key in model:
                    raise PolicyError(f"direct model {name} must not override {key}")
            continue
        routes += 1
        if not isinstance(routing, dict):
            raise PolicyError(f"routing model {name} must contain a routing object")
        statuses = routing.get("fallback_on_statuses")
        if (
            not isinstance(statuses, list)
            or len(statuses) != len(REQUIRED_FALLBACK_STATUSES)
            or any(not isinstance(status, int) or isinstance(status, bool) for status in statuses)
            or set(statuses) != set(REQUIRED_FALLBACK_STATUSES)
        ):
            raise PolicyError(
                f"routing model {name} must declare fallback_on_statuses: "
                "[400, 401, 402, 403, 404]"
            )
        for key, value in REQUIRED_ROUTING.items():
            if routing.get(key) != value or type(routing.get(key)) is not type(value):
                raise PolicyError(f"routing model {name} must declare {key}: {json.dumps(value)}")
        if name == STAR:
            raise PolicyError("model * must be the direct fallback row, not a routing model")
        targets = routing.get("targets")
        if isinstance(targets, list) and any(
            isinstance(target, dict) and target.get("model") == STAR for target in targets
        ):
            raise PolicyError(f"routing model {name} must not target the * fallback")
    if routes == 0:
        raise PolicyError("AISIX resources must contain at least one routing model")
    if stars != 1:
        raise PolicyError(f"AISIX resources must contain exactly one * fallback model, found {stars}")
    return routes


def validate_fallback_capabilities(resources, native_ids):
    ref_field, ref_value = cpa_pool_reference(resources)
    models = {}
    for model in resources["models"]:
        name = model.get("display_name")
        if not isinstance(name, str) or not name or name in models:
            raise PolicyError("AISIX model names must be non-empty and unique")
        models[name] = model
    missing = []
    for name, model in models.items():
        if model.get("routing") is None:
            continue
        targets = model["routing"].get("targets")
        if not isinstance(targets, list) or not targets:
            raise PolicyError(f"routing model {name} must declare a non-empty targets list")
        for target in targets[1:]:
            target_name = target.get("model") if isinstance(target, dict) else None
            if not isinstance(target_name, str) or not target_name:
                raise PolicyError(f"routing model {name} has a malformed fallback target")
            direct = models.get(target_name)
            if not direct or "model_name" not in direct:
                missing.append(f"{name} -> {target_name} (not a direct model)")
            elif direct.get(ref_field) == ref_value:
                upstream = direct["model_name"]
                if not isinstance(upstream, str) or upstream not in native_ids:
                    missing.append(f"{name} -> {target_name} (missing native CPA model)")
    if missing:
        raise PolicyError("unverified fallback targets: " + "; ".join(missing))


def load_native_ids(stream):
    try:
        document = json.load(stream)
        # CPA serves the Codex shape when client_version is set.
        if "models" in document:
            rows, field = document["models"], "slug"
        else:
            rows, field = document["data"], "id"
        if not isinstance(rows, list) or not rows:
            raise ValueError
        ids = {row[field] for row in rows}
        if not all(isinstance(model_id, str) and model_id for model_id in ids):
            raise ValueError
        return ids
    except (ValueError, AttributeError, KeyError, TypeError):
        raise PolicyError("missing or invalid native CPA model inventory") from None


def fetch_native_inventory(env_path):
    # Use the native host binding, not AISIX or the supplemented catalog. Do not follow redirects.
    try:
        env = dict(
            line.split("=", 1) for line in env_path.read_text().splitlines()
            if "=" in line and not line.lstrip().startswith("#")
        )
        host = env.get("CPA_BIND_ADDRESS") or "127.0.0.1"
        if host == "0.0.0.0":
            host = "127.0.0.1"
        token = env.get("CPA_CLIENT_KEY") or env["CPA_API_KEY"]
        conn = HTTPConnection(host, int(env.get("CPA_PORT") or "8317"), timeout=15)
        try:
            conn.request("GET", "/v1/models?client_version=1", headers={"Authorization": "Bearer " + token})
            with conn.getresponse() as response:
                if response.status != 200:
                    raise PolicyError(f"native CPA inventory request returned HTTP {response.status}")
                return {"data": [{"id": model_id} for model_id in sorted(load_native_ids(response))]}
        finally:
            conn.close()
    except (OSError, HTTPException, ValueError, KeyError):
        raise PolicyError("native CPA inventory request failed") from None


def load_compose_json(path):
    try:
        document = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as error:
        raise PolicyError(
            f"{path}: invalid normalized JSON at line {error.lineno}, column {error.colno}"
        ) from None
    except OSError:
        raise PolicyError(f"cannot read normalized AISIX resources: {path}") from None
    resources = document.get("x-aisix-resources") if isinstance(document, dict) else None
    if not isinstance(resources, dict):
        raise PolicyError("normalized Compose document lacks x-aisix-resources")
    return resources


def load_yaml(path):
    try:
        import yaml
    except ImportError:
        raise PolicyError(
            "the active Compose provider lacks JSON output and PyYAML is unavailable"
        ) from None
    try:
        document = yaml.safe_load(path.read_text(encoding="utf-8"))
    except yaml.YAMLError as error:
        mark = getattr(error, "problem_mark", None)
        location = ""
        if mark is not None:
            location = f" at line {mark.line + 1}, column {mark.column + 1}"
        raise PolicyError(f"{path}: invalid YAML{location}") from None
    except OSError:
        raise PolicyError(f"cannot read AISIX resources: {path}") from None
    if isinstance(document, dict) and "x-aisix-resources" in document:
        resources = document.get("x-aisix-resources")
        if not isinstance(resources, dict):
            raise PolicyError("normalized Compose document has invalid x-aisix-resources")
        return resources
    return document


def self_test():
    valid = {
        "models": [
            {
                "display_name": "route",
                "routing": {
                    "fallback_on_statuses": REQUIRED_FALLBACK_STATUSES.copy(),
                    **REQUIRED_ROUTING,
                },
            },
            {"display_name": "direct", "cooldown": json.loads(json.dumps(REQUIRED_COOLDOWN))},
            {
                "display_name": STAR,
                **STAR_ROW,
                "provider_key": "cpa-pool",
                "cooldown": json.loads(json.dumps(REQUIRED_COOLDOWN)),
            },
        ]
    }
    if validate(valid) != 1:
        raise PolicyError("valid policy self-test failed")

    def rejected(candidate, label):
        try:
            validate(candidate)
        except PolicyError:
            return
        raise PolicyError(f"invalid policy self-test was accepted: {label}")

    v2 = json.loads(json.dumps(valid))
    cpa_id = "7ae8ddac-b66f-5f6e-ae9c-7e71958780c9"
    v2["_format_version"] = "2"
    v2["provider_keys"] = [{"display_name": "cpa-pool", "id": cpa_id}]
    v2["models"][2].pop("provider_key")
    v2["models"][2]["provider_key_id"] = cpa_id
    v2["models"][1].update(model_name="direct", provider_key_id=cpa_id)
    v2["models"][0]["routing"]["targets"] = [{"model": "direct"}, {"model": "direct"}]
    if validate(v2) != 1:
        raise PolicyError("valid v2 policy self-test failed")
    validate_fallback_capabilities(v2, {"direct"})
    try:
        validate_fallback_capabilities(v2, set())
    except PolicyError:
        pass
    else:
        raise PolicyError("v2 fallback inventory self-test accepted an absent CPA model")
    v2["models"][2]["provider_key_id"] = "cpa-pool"
    rejected(v2, "v2 fallback with a name where a UUID reference is required")

    star = valid["models"][2]
    candidate = json.loads(json.dumps(valid))
    candidate["models"].pop(2)
    rejected(candidate, "missing * fallback")
    candidate = json.loads(json.dumps(valid))
    candidate["models"].append(json.loads(json.dumps(star)))
    rejected(candidate, "duplicate * fallback")
    for key, value in (("model_name", "x"), ("provider_key", "other"), ("provider", "azure")):
        candidate = json.loads(json.dumps(valid))
        candidate["models"][2][key] = value
        rejected(candidate, f"* fallback {key}={value}")
    candidate = json.loads(json.dumps(valid))
    candidate["models"][2].pop("cooldown")
    rejected(candidate, "* fallback without cooldown")
    candidate = json.loads(json.dumps(valid))
    candidate["models"][0]["routing"]["targets"] = [{"model": "direct"}, {"model": STAR}]
    rejected(candidate, "route targeting *")
    candidate = json.loads(json.dumps(valid))
    candidate["models"].append(
        {"display_name": STAR, "routing": json.loads(json.dumps(valid["models"][0]["routing"]))}
    )
    rejected(candidate, "* as a routing model")
    # Narrower single-* globs stay legal: their precedence over * is the feature.
    candidate = json.loads(json.dumps(valid))
    candidate["models"].append(
        {
            "display_name": "devin/*",
            "provider": "openai",
            "model_name": "devin/*",
            "provider_key": "cpa-pool",
            "cooldown": json.loads(json.dumps(REQUIRED_COOLDOWN)),
        }
    )
    validate(candidate)
    for index, key, value in (
        (0, "retries", 1),
        (0, "retries", None),
        (0, "retry_on_429", False),
        (0, "retry_on_429", None),
        (1, "cooldown", None),
        (1, "retries", 10),
        (1, "timeout", 15000),
    ):
        candidate = json.loads(json.dumps(valid))
        target = candidate["models"][index]
        target = target["routing"] if index == 0 else target
        if value is None:
            target.pop(key)
        else:
            target[key] = value
        try:
            validate(candidate)
        except PolicyError:
            pass
        else:
            raise PolicyError(f"invalid policy self-test was accepted: {key}={value}")
    candidate = json.loads(json.dumps(valid))
    candidate["models"][1]["cooldown"]["trigger_statuses"] = [429]
    try:
        validate(candidate)
    except PolicyError:
        pass
    else:
        raise PolicyError("invalid policy self-test was accepted: narrowed trigger_statuses")
    for statuses in (
        None,
        [418],
        [400, 401, 403, 404],
        [400, 400, 401, 402, 403, 404],
    ):
        candidate = json.loads(json.dumps(valid))
        if statuses is None:
            candidate["models"][0]["routing"].pop("fallback_on_statuses")
        else:
            candidate["models"][0]["routing"]["fallback_on_statuses"] = statuses
        try:
            validate(candidate)
        except PolicyError:
            pass
        else:
            raise PolicyError(f"invalid policy self-test was accepted: {statuses}")

    candidate = json.loads(json.dumps(valid))
    candidate["models"][0]["routing"]["targets"] = [{"model": "primary"}, {"model": "direct"}]
    candidate["models"][1].update(model_name="provider/registered", provider_key="cpa-pool")
    validate_fallback_capabilities(candidate, {"provider/registered"})
    try:
        validate_fallback_capabilities(candidate, {"direct"})
    except PolicyError as error:
        if "route -> direct" not in str(error):
            raise
    else:
        raise PolicyError("model display name incorrectly passed native validation")
    if load_native_ids(io.StringIO('{"models":[{"slug":"provider/registered"}]}')) != {"provider/registered"}:
        raise PolicyError("native Codex model inventory was not recognized")
    for payload in ("", "{}", '{"data":[]}', '{"data":[{"id":null}]}'):
        try:
            load_native_ids(io.StringIO(payload))
        except PolicyError:
            continue
        raise PolicyError("invalid native inventory passed validation")

    reordered = json.loads(json.dumps(valid))
    reordered["models"][0]["routing"]["fallback_on_statuses"] = [404, 403, 402, 401, 400]
    validate(reordered)

    try:
        import yaml  # noqa: F401
    except ImportError:
        return
    block_yaml = """models:
  - display_name: route
    routing:
      retries: 0
      retry_on_429: true
      fallback_on_statuses:
        - 400
        - 401
        - 402
        - 403
        - 404
  - display_name: "*"
    provider: openai
    model_name: "*"
    provider_key: cpa-pool
    cooldown:
      enabled: true
      default_seconds: 60
      max_seconds: 300
      honor_retry_after: true
      trigger_statuses: [400, 401, 402, 403, 404, 408, 429, 500, 502, 503, 504]
"""
    with tempfile.NamedTemporaryFile("w", encoding="utf-8") as stream:
        stream.write(block_yaml)
        stream.flush()
        validate(load_yaml(Path(stream.name)))

    sentinel = "REVIEW_SECRET_SENTINEL"
    with tempfile.NamedTemporaryFile("w", encoding="utf-8") as stream:
        stream.write(f"provider_keys:\n  - api_key: [{sentinel}\n")
        stream.flush()
        try:
            load_yaml(Path(stream.name))
        except PolicyError as error:
            if sentinel in str(error):
                raise PolicyError("malformed-YAML error leaked source content") from None
        else:
            raise PolicyError("malformed YAML self-test was accepted")


def main():
    parser = argparse.ArgumentParser()
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument("--compose-json", type=Path)
    group.add_argument("--yaml", type=Path)
    group.add_argument("--self-test", action="store_true")
    group.add_argument("--native-cpa-env", type=Path)
    parser.add_argument("--cpa-models-stdin", action="store_true")
    args = parser.parse_args()
    try:
        if args.native_cpa_env:
            print(json.dumps(fetch_native_inventory(args.native_cpa_env)))
            return
        if args.self_test:
            self_test()
            print("aisix_resource_policy_self_test=passed")
            return
        resources = load_compose_json(args.compose_json) if args.compose_json else load_yaml(args.yaml)
        routes = validate(resources)
        if args.cpa_models_stdin:
            validate_fallback_capabilities(resources, load_native_ids(sys.stdin))
        print(
            "aisix_resource_policy=valid "
            f"routes={routes} statuses={','.join(map(str, REQUIRED_FALLBACK_STATUSES))}"
        )
    except PolicyError as error:
        print(f"AISIX resource policy validation failed: {error}", file=sys.stderr)
        raise SystemExit(1) from None


if __name__ == "__main__":
    main()
