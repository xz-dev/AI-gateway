#!/usr/bin/env python3
import argparse
import json
import sys
import tempfile
from pathlib import Path

REQUIRED_FALLBACK_STATUSES = [400, 401, 402, 403, 404]


class PolicyError(Exception):
    pass


def validate(resources):
    if not isinstance(resources, dict):
        raise PolicyError("AISIX resources must be an object")
    models = resources.get("models")
    if not isinstance(models, list):
        raise PolicyError("AISIX resources must contain a models list")
    routes = 0
    for model in models:
        if not isinstance(model, dict):
            raise PolicyError("every AISIX model must be an object")
        routing = model.get("routing")
        if routing is None:
            continue
        routes += 1
        name = model.get("display_name")
        if not isinstance(name, str) or not name:
            name = "<unknown>"
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
    if routes == 0:
        raise PolicyError("AISIX resources must contain at least one routing model")
    return routes


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
                "routing": {"fallback_on_statuses": REQUIRED_FALLBACK_STATUSES.copy()},
            }
        ]
    }
    if validate(valid) != 1:
        raise PolicyError("valid policy self-test failed")
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
      fallback_on_statuses:
        - 400
        - 401
        - 402
        - 403
        - 404
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
    args = parser.parse_args()
    try:
        if args.self_test:
            self_test()
            print("aisix_resource_policy_self_test=passed")
            return
        resources = load_compose_json(args.compose_json) if args.compose_json else load_yaml(args.yaml)
        routes = validate(resources)
        print(
            "aisix_resource_policy=valid "
            f"routes={routes} statuses={','.join(map(str, REQUIRED_FALLBACK_STATUSES))}"
        )
    except PolicyError as error:
        print(f"AISIX resource policy validation failed: {error}", file=sys.stderr)
        raise SystemExit(1) from None


if __name__ == "__main__":
    main()
