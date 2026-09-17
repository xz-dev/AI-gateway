#!/usr/bin/env python3
import argparse
import copy
import json
import re
import sys
from pathlib import Path


class PolicyError(Exception):
    pass


REQUIRED = {
    ("api.z.ai", "POST", r"^/api/mcp/(web_search_prime|web_reader|zread)/mcp($|[?])"),
    ("api.z.ai", "POST", r"^/api/auth/z/login($|[?])"),
    ("api.z.ai", "GET", r"^/api/biz/customer/getCustomerInfo($|[?])"),
    ("api.z.ai", "GET", r"^/api/biz/v1/organization/[^/?]+/projects/[^/?]+/api_keys($|[?])"),
    ("api.z.ai", "POST", r"^/api/biz/v1/organization/[^/?]+/projects/[^/?]+/api_keys($|[?])"),
    ("api.z.ai", "GET", r"^/api/biz/v1/organization/[^/?]+/projects/[^/?]+/api_keys/copy/[^/?]+($|[?])"),
    ("bigmodel.cn", "GET", r"^/api/biz/customer/getCustomerInfo($|[?])"),
    ("bigmodel.cn", "GET", r"^/api/biz/v1/organization/[^/?]+/projects/[^/?]+/api_keys($|[?])"),
    ("bigmodel.cn", "POST", r"^/api/biz/v1/organization/[^/?]+/projects/[^/?]+/api_keys($|[?])"),
    ("bigmodel.cn", "GET", r"^/api/biz/v1/organization/[^/?]+/projects/[^/?]+/api_keys/copy/[^/?]+($|[?])"),
    ("open.bigmodel.cn", "POST", r"^/api/mcp/(web_search_prime|web_reader|zread)/mcp($|[?])"),
    ("zcode.z.ai", "POST", r"^/api/v1/oauth/cli/init($|[?])"),
    ("zcode.z.ai", "GET", r"^/api/v1/oauth/cli/poll/[^/?]+($|[?])"),
    ("zcode.z.ai", "GET", r"^/api/v1/zcode-plan/billing/(balance|preview)($|[?])"),
    ("zcode.z.ai", "POST", r"^/api/v1/zcode-plan/billing/claim($|[?])"),
    ("zcode.z.ai", "GET", r"^/api/v1/off-peak/ticket/availability($|[?])"),
    ("zcode.z.ai", "POST", r"^/api/v1/off-peak/ticket($|[?])"),
    ("zcode.z.ai", "POST", r"^/api/v1/off-peak/ticket/status($|[?])"),
    ("zcode.z.ai", "POST", r"^/api/v1/off-peak/ticket/[^/?]+/settle($|[?])"),
    ("zcode.z.ai", "POST", r"^/api/v1/off-peak/anthropic/v1/messages($|[?])"),
}

REQUIRED_MATCHES = (
    ("zcode.z.ai", "POST", "/api/v1/oauth/cli/init"),
    ("zcode.z.ai", "GET", "/api/v1/oauth/cli/poll/flow_abc"),
    ("zcode.z.ai", "GET", "/api/v1/zcode-plan/billing/balance?app_version=4.6.5"),
    ("zcode.z.ai", "GET", "/api/v1/zcode-plan/billing/preview?app_version=4.6.5"),
    ("zcode.z.ai", "POST", "/api/v1/zcode-plan/billing/claim"),
    ("zcode.z.ai", "GET", "/api/v1/off-peak/ticket/availability"),
    ("zcode.z.ai", "POST", "/api/v1/off-peak/ticket"),
    ("zcode.z.ai", "POST", "/api/v1/off-peak/ticket/status"),
    ("zcode.z.ai", "POST", "/api/v1/off-peak/ticket/ticket_abc/settle"),
    ("zcode.z.ai", "POST", "/api/v1/off-peak/anthropic/v1/messages"),
    ("open.bigmodel.cn", "POST", "/api/mcp/web_search_prime/mcp"),
    ("open.bigmodel.cn", "POST", "/api/mcp/web_reader/mcp"),
    ("open.bigmodel.cn", "POST", "/api/mcp/zread/mcp"),
    ("api.z.ai", "POST", "/api/mcp/web_search_prime/mcp"),
    ("api.z.ai", "POST", "/api/mcp/web_reader/mcp"),
    ("api.z.ai", "POST", "/api/mcp/zread/mcp"),
    ("api.z.ai", "POST", "/api/auth/z/login"),
    ("api.z.ai", "GET", "/api/biz/customer/getCustomerInfo"),
    ("api.z.ai", "GET", "/api/biz/v1/organization/org_1/projects/project_1/api_keys"),
    ("api.z.ai", "POST", "/api/biz/v1/organization/org_1/projects/project_1/api_keys"),
    ("api.z.ai", "GET", "/api/biz/v1/organization/org_1/projects/project_1/api_keys/copy/key_1"),
    ("bigmodel.cn", "GET", "/api/biz/customer/getCustomerInfo"),
    ("bigmodel.cn", "GET", "/api/biz/v1/organization/org_1/projects/project_1/api_keys"),
    ("bigmodel.cn", "POST", "/api/biz/v1/organization/org_1/projects/project_1/api_keys"),
    ("bigmodel.cn", "GET", "/api/biz/v1/organization/org_1/projects/project_1/api_keys/copy/key_1"),
)

REQUIRED_DENIALS = (
    ("zcode.z.ai", "GET", "/api/v1/oauth/cli/init"),
    ("zcode.z.ai", "GET", "/api/v1/oauth/cli/poll/"),
    ("zcode.z.ai", "GET", "/api/v1/oauth/cli/poll/a/b"),
    ("zcode.z.ai", "POST", "/api/v1/zcode-plan/billing/balance"),
    ("zcode.z.ai", "GET", "/api/v1/zcode-plan/billing/claim"),
    ("zcode.z.ai", "POST", "/api/v1/off-peak/ticket/a/b/settle"),
    ("zcode.z.ai", "GET", "/api/v1/off-peak/anthropic/v1/messages"),
    ("zcode.z.ai", "GET", "/api/v1/undeclared"),
    ("open.bigmodel.cn", "GET", "/api/mcp/web_reader/mcp"),
    ("open.bigmodel.cn", "POST", "/api/mcp/unknown/mcp"),
    ("api.z.ai", "POST", "/api/mcp/web_reader/mcp/extra"),
    ("api.z.ai", "GET", "/api/auth/z/login"),
    ("api.z.ai", "GET", "/api/biz/v1/organization/a/b/projects/c/api_keys"),
    ("api.z.ai", "GET", "/api/biz/v1/organization/a/projects/b/api_keys/copy/a/b"),
    ("bigmodel.cn", "POST", "/api/biz/customer/getCustomerInfo"),
    ("bigmodel.cn", "GET", "/api/biz/v1/organization/a/projects/b/api_keys/extra"),
)


def load_policy(path):
    try:
        document = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise PolicyError(f"cannot load policy: {error}") from None
    services = document.get("services") if isinstance(document, dict) else None
    if not isinstance(services, dict):
        raise PolicyError("policy must contain a services object")
    return document


def zcode_entries(policy):
    zcode = policy["services"].get("zcode")
    if not isinstance(zcode, dict):
        raise PolicyError("policy must contain services.zcode")
    destinations = zcode.get("destinations")
    if not isinstance(destinations, list):
        raise PolicyError("services.zcode.destinations must be a list")
    entries = []
    for item in destinations:
        if not isinstance(item, dict) or item.get("tls") != "bump":
            continue
        domain = item.get("domain")
        methods = item.get("methods")
        paths = item.get("paths")
        if not isinstance(domain, str) or not isinstance(methods, list) or not isinstance(paths, list):
            raise PolicyError("every bumped ZCode destination needs domain, methods, and paths")
        for method in methods:
            for path in paths:
                try:
                    expression = re.compile(path)
                except re.error as error:
                    raise PolicyError(f"invalid path expression {path!r}: {error}") from None
                entries.append((domain, method, path, expression))
    return entries


def allows(entries, domain, method, path):
    return any(
        entry_domain == domain and entry_method == method and expression.search(path)
        for entry_domain, entry_method, _pattern, expression in entries
    )


def validate(policy):
    entries = zcode_entries(policy)
    triples = {(domain, method, pattern) for domain, method, pattern, _expression in entries}
    missing = sorted(REQUIRED - triples)
    if missing:
        raise PolicyError(f"missing required ZCode rules: {missing!r}")
    for domain, method, path in REQUIRED_MATCHES:
        if not allows(entries, domain, method, path):
            raise PolicyError(f"required request is denied: {method} https://{domain}{path}")
    for domain, method, path in REQUIRED_DENIALS:
        if allows(entries, domain, method, path):
            raise PolicyError(f"undeclared request is allowed: {method} https://{domain}{path}")
    for _domain, _method, pattern, _expression in entries:
        if pattern in (r"^/api/v1/", r"^/api/mcp/") or pattern in (r"^/.*", r"^/"):
            raise PolicyError(f"broad ZCode path is forbidden: {pattern}")
    return len(entries)


def self_test():
    valid = {"services": {"zcode": {"destinations": []}}}
    grouped = {}
    for domain, method, pattern in sorted(REQUIRED):
        grouped.setdefault((domain, method), []).append(pattern)
    valid["services"]["zcode"]["destinations"] = [
        {"domain": domain, "tls": "bump", "methods": [method], "paths": paths}
        for (domain, method), paths in grouped.items()
    ]
    validate(valid)

    for mutation in ("missing", "broad", "wrong-method"):
        candidate = copy.deepcopy(valid)
        if mutation == "missing":
            candidate["services"]["zcode"]["destinations"][0]["paths"].pop()
        elif mutation == "broad":
            candidate["services"]["zcode"]["destinations"].append(
                {"domain": "zcode.z.ai", "tls": "bump", "methods": ["GET"], "paths": [r"^/api/v1/"]}
            )
        else:
            item = next(
                item
                for item in candidate["services"]["zcode"]["destinations"]
                if item["domain"] == "zcode.z.ai" and r"^/api/v1/oauth/cli/init($|[?])" in item["paths"]
            )
            item["methods"] = ["GET"]
        try:
            validate(candidate)
        except PolicyError:
            continue
        raise PolicyError(f"invalid {mutation} policy was accepted")


def main():
    parser = argparse.ArgumentParser()
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument("--policy", type=Path)
    group.add_argument("--self-test", action="store_true")
    args = parser.parse_args()
    try:
        if args.self_test:
            self_test()
            print("zcode_egress_policy_self_test=passed")
            return
        count = validate(load_policy(args.policy))
        print(f"zcode_egress_policy=valid acl_pairs={count}")
    except PolicyError as error:
        print(f"ZCode egress policy validation failed: {error}", file=sys.stderr)
        raise SystemExit(1) from None


if __name__ == "__main__":
    main()
