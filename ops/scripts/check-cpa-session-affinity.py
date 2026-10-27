#!/usr/bin/env python3
import argparse
import sys
from pathlib import Path

EXPECTED_ROUTING = {"strategy": "fill-first", "session-affinity": True, "session-affinity-ttl": "1h"}


class ConfigError(Exception):
    pass


def load_yaml(path):
    try:
        import yaml
    except ImportError:
        raise ConfigError("PyYAML is required to check CPA session affinity") from None
    try:
        document = yaml.safe_load(path.read_text(encoding="utf-8"))
    except (OSError, yaml.YAMLError) as error:
        raise ConfigError(f"cannot load CPA config: {error}") from None
    if not isinstance(document, dict):
        raise ConfigError("CPA config must be a YAML object")
    return document


def validate(path, document):
    routing = document.get("routing")
    if not isinstance(routing, dict):
        raise ConfigError(f"{path} must contain a routing block")
    for key, expected in EXPECTED_ROUTING.items():
        if routing.get(key) != expected:
            raise ConfigError(
                f"{path} routing.{key} must be {expected!r}, got {routing.get(key)!r}"
            )
    allowed = {"strategy", "session-affinity", "session-affinity-ttl"}
    unknown = sorted(set(routing) - allowed)
    if unknown:
        raise ConfigError(f"{path} routing has unsupported keys: {', '.join(unknown)}")
    return routing


def self_test():
    valid = {
        "routing": {
            "strategy": "fill-first",
            "session-affinity": True,
            "session-affinity-ttl": "1h",
        }
    }
    validate("<memory>", valid)
    for key, value in (
        ("strategy", "round-robin"),
        ("session-affinity", False),
        ("session-affinity-ttl", "30m"),
        ("session-affinity-ttl", 60),
    ):
        candidate = {
            "routing": {
                "strategy": "fill-first",
                "session-affinity": True,
                "session-affinity-ttl": "1h",
            }
        }
        candidate["routing"][key] = value
        try:
            validate("<memory>", candidate)
        except ConfigError:
            continue
        raise ConfigError(f"invalid routing.{key} was accepted: {value!r}")


def main():
    parser = argparse.ArgumentParser()
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument("--config", type=Path)
    group.add_argument("--self-test", action="store_true")
    args = parser.parse_args()
    try:
        if args.self_test:
            self_test()
            print("cpa_session_affinity_self_test=passed")
            return
        validate(args.config, load_yaml(args.config))
        print(
            "cpa_session_affinity=valid "
            "strategy=fill-first affinity=true ttl=1h"
        )
    except ConfigError as error:
        print(f"CPA session affinity validation failed: {error}", file=sys.stderr)
        raise SystemExit(1) from None


if __name__ == "__main__":
    main()
