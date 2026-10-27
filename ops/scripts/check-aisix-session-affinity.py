#!/usr/bin/env python3
import argparse
import json
import sys
from pathlib import Path

AFFINITY_FORWARD_HEADERS = ["session_id"]
AFFINITY_HASH_ON = [
    {"type": "header", "name": "session_id"},
    {"type": "api_key"},
]


class PolicyError(Exception):
    pass


def load_resources(path):
    try:
        import yaml
    except ImportError:
        raise PolicyError("PyYAML is required to check AISIX affinity resources") from None
    try:
        document = yaml.safe_load(path.read_text(encoding="utf-8"))
    except (OSError, yaml.YAMLError) as error:
        raise PolicyError(f"cannot load AISIX resources: {error}") from None
    if not isinstance(document, dict):
        raise PolicyError("AISIX resources must be an object")
    return document


def route_map(resources):
    result = {}
    models = resources.get("models") if isinstance(resources, dict) else None
    if not isinstance(models, list):
        raise PolicyError("AISIX resources must contain models")
    for model in models:
        if isinstance(model, dict) and isinstance(model.get("routing"), dict):
            name = model.get("display_name")
            if isinstance(name, str) and name:
                result[name] = model["routing"]
    return result


def validate(resources):
    provider_keys = resources.get("provider_keys")
    if not isinstance(provider_keys, list):
        raise PolicyError("AISIX resources must contain provider_keys")
    pools = [
        item
        for item in provider_keys
        if isinstance(item, dict) and item.get("display_name") == "cpa-pool"
    ]
    if len(pools) != 1:
        raise PolicyError("AISIX resources must contain exactly one cpa-pool provider key")
    request = pools[0].get("request")
    if not isinstance(request, dict):
        raise PolicyError("cpa-pool must contain a request block")
    if request.get("forward_client_headers") != AFFINITY_FORWARD_HEADERS:
        raise PolicyError("cpa-pool must forward only session_id")
    defaults = request.get("default_headers", {})
    if not isinstance(defaults, dict):
        raise PolicyError("cpa-pool request.default_headers must be an object")
    if any(str(name).lower() == "session_id" for name in defaults):
        raise PolicyError("cpa-pool must not set a static session_id default")

    models = resources.get("models")
    if not isinstance(models, list):
        raise PolicyError("AISIX resources must contain models")
    affinity_routes = 0
    for model in models:
        if not isinstance(model, dict) or not isinstance(model.get("routing"), dict):
            continue
        name = model.get("display_name") or "<unknown>"
        routing = model["routing"]
        strategy = routing.get("strategy", "failover")
        hash_on = routing.get("hash_on")
        if strategy == "consistent_hash":
            affinity_routes += 1
            if hash_on != AFFINITY_HASH_ON:
                raise PolicyError(f"route {name} must hash on session_id then api_key")
            targets = routing.get("targets")
            if not isinstance(targets, list) or len(targets) < 2:
                raise PolicyError(f"route {name} needs at least two hash targets")
        elif hash_on is not None:
            raise PolicyError(f"non-hash route {name} must not declare hash_on")
    if affinity_routes == 0:
        raise PolicyError("AISIX resources must contain a consistent_hash route")
    return affinity_routes


def validate_candidate(baseline, candidate, affinity_routes, ordered_routes):
    before = route_map(baseline)
    after = route_map(candidate)
    requested = set(affinity_routes) | set(ordered_routes)
    missing = sorted(name for name in requested if name not in before or name not in after)
    if missing:
        raise PolicyError(f"candidate comparison is missing routes: {', '.join(missing)}")
    changed = {name for name in before if name in after and before[name] != after[name]}
    unexpected = sorted(changed - set(affinity_routes))
    if unexpected:
        raise PolicyError(f"candidate changed routes outside the affinity set: {', '.join(unexpected)}")
    for name in affinity_routes:
        old = before[name]
        new = after[name]
        if old.get("strategy") != "round_robin":
            raise PolicyError(f"baseline affinity route {name} is not round_robin")
        if new.get("strategy") != "consistent_hash" or new.get("hash_on") != AFFINITY_HASH_ON:
            raise PolicyError(f"candidate affinity route {name} lacks the approved hash policy")
        old_rest = {key: value for key, value in old.items() if key not in {"strategy", "hash_on"}}
        new_rest = {key: value for key, value in new.items() if key not in {"strategy", "hash_on"}}
        if old_rest != new_rest:
            raise PolicyError(f"candidate affinity route {name} changed fields beyond strategy/hash_on")
    for name in ordered_routes:
        if before[name] != after[name] or after[name].get("strategy") != "failover":
            raise PolicyError(f"ordered route {name} must remain unchanged failover")
    return len(affinity_routes)


def self_test():
    valid = {
        "provider_keys": [
            {
                "display_name": "cpa-pool",
                "request": {"forward_client_headers": ["session_id"]},
            }
        ],
        "models": [
            {
                "display_name": "dynamic",
                "routing": {
                    "strategy": "consistent_hash",
                    "hash_on": AFFINITY_HASH_ON,
                    "targets": [{"model": "a"}, {"model": "b"}],
                },
            },
            {
                "display_name": "ordered",
                "routing": {
                    "strategy": "failover",
                    "targets": [{"model": "a"}, {"model": "b"}],
                },
            },
        ],
    }
    if validate(valid) != 1:
        raise PolicyError("valid affinity policy self-test failed")

    invalid = []
    for headers in ([], ["session_id", "authorization"], ["*"]):
        candidate = json.loads(json.dumps(valid))
        candidate["provider_keys"][0]["request"]["forward_client_headers"] = headers
        invalid.append(candidate)
    candidate = json.loads(json.dumps(valid))
    candidate["provider_keys"][0]["request"]["default_headers"] = {
        "session_id": "static"
    }
    invalid.append(candidate)
    candidate = json.loads(json.dumps(valid))
    candidate["models"][0]["routing"]["hash_on"] = [{"type": "api_key"}]
    invalid.append(candidate)
    candidate = json.loads(json.dumps(valid))
    candidate["models"][0]["routing"]["targets"] = [{"model": "a"}]
    invalid.append(candidate)
    candidate = json.loads(json.dumps(valid))
    candidate["models"][1]["routing"]["hash_on"] = AFFINITY_HASH_ON
    invalid.append(candidate)
    for candidate in invalid:
        try:
            validate(candidate)
        except PolicyError:
            continue
        raise PolicyError("invalid affinity policy self-test was accepted")

    baseline = json.loads(json.dumps(valid))
    baseline["models"][0]["routing"].pop("hash_on")
    baseline["models"][0]["routing"]["strategy"] = "round_robin"
    validate_candidate(baseline, valid, ["dynamic"], ["ordered"])
    invalid_candidate = json.loads(json.dumps(valid))
    invalid_candidate["models"][0]["routing"]["retries"] = 9
    try:
        validate_candidate(baseline, invalid_candidate, ["dynamic"], ["ordered"])
    except PolicyError:
        pass
    else:
        raise PolicyError("candidate comparison accepted an unrelated route change")


def main():
    parser = argparse.ArgumentParser()
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument("--yaml", type=Path)
    group.add_argument("--self-test", action="store_true")
    group.add_argument("--baseline", type=Path)
    parser.add_argument("--candidate", type=Path)
    parser.add_argument("--affinity-route", action="append", default=[])
    parser.add_argument("--ordered-route", action="append", default=[])
    args = parser.parse_args()
    try:
        if args.self_test:
            self_test()
            print("aisix_session_affinity_self_test=passed")
            return
        if args.baseline:
            if not args.candidate or not args.affinity_route:
                raise PolicyError("candidate comparison requires --candidate and --affinity-route")
            count = validate_candidate(
                load_resources(args.baseline),
                load_resources(args.candidate),
                args.affinity_route,
                args.ordered_route,
            )
            print(f"aisix_session_affinity_candidate=valid routes={count}")
            return
        count = validate(load_resources(args.yaml))
        print(
            "aisix_session_affinity=valid "
            f"routes={count} forward_headers=session_id hash_fallback=api_key"
        )
    except PolicyError as error:
        print(f"AISIX session affinity validation failed: {error}", file=sys.stderr)
        raise SystemExit(1) from None


if __name__ == "__main__":
    main()
