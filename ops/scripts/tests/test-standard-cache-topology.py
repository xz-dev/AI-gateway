#!/usr/bin/env python3
"""Source-level guards for the shared standard Responses cache path."""

import ast
import copy
import ipaddress
import json
import re
import subprocess
import textwrap
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]


def section(text, marker):
    if text.count(marker) != 1:
        raise AssertionError(f"expected one section: {marker.strip()}")
    start = text.index(marker)
    end = re.search(r"^  (?:- id: |[a-zA-Z][\w-]*:)", text[start + len(marker):], re.MULTILINE)
    return text[start:] if end is None else text[start:start + len(marker) + end.start()]


class StandardCacheTopology(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.compose = (ROOT / "compose.yaml").read_text()
        cls.front = (ROOT / "services/firewall/apisix/apisix.yaml").read_text()
        cls.inner = (ROOT / "services/routing/apisix-models/apisix.yaml").read_text()

    def test_standard_read_paths_are_exact(self):
        for name, paths, rejected in (
            ("ai-api-v1-get", ("/v1/responses", "/v1/responses/resp_test"), ("/v1/responses/resp_test/extra", "/v1/responses/resp_test/cancel", "/v1/responses/resp_native/input_items")),
            ("ai-api-root-compatible-get", ("/responses", "/responses/resp_test"), ("/responses/resp_test/extra", "/rk/responses/resp_test", "/responses/resp_native/input_items")),
        ):
            route = section(self.front, f"  - id: {name}\n")
            match = re.search(r"\[uri, \"~~\", '(.*)'\]", route)
            self.assertIsNotNone(match)
            pattern = match.group(1)
            for path in paths:
                self.assertRegex(path, pattern)
            for path in rejected:
                self.assertNotRegex(path, pattern)
            self.assertIn("upstream_id: sub2api", route)
        for name in ("ai-api-v1-post", "ai-api-root-compatible-post"):
            route = section(self.front, f"  - id: {name}\n")
            pattern = re.search(r"\[uri, \"~~\", '(.*)'\]", route).group(1)
            prefix = "/v1" if name == "ai-api-v1-post" else ""
            self.assertRegex(prefix + "/responses/resp_test/cancel", pattern)

    def test_inner_routes_have_no_private_or_duplicate_keeper(self):
        self.assertNotIn("/rk", self.inner)
        self.assertIn("client_max_body_size: 64m", (ROOT / "services/routing/apisix-models/config.yaml").read_text())
        self.assertNotIn("upstream_id: response-keeper", self.inner)
        self.assertNotIn('"127.0.0.1:8085"', self.inner)
        route = section(self.inner, "  - id: responses-routing-selector\n")
        for value in ("uri: /v1/responses", "methods: [POST]", "max_body_size: 67108864", 'require("routing_selector").run', "disable_proxy_buffering: true"):
            self.assertIn(value, route)
        native = section(self.inner, "  - id: catch-all\n")
        self.assertIn("enable_websocket: true", native)
        self.assertIn("upstream_id: cpa", native)

    def test_chain_has_one_front_keeper(self):
        keepalive = section(self.compose, "  ai-sse-keepalive-proxy:\n")
        self.assertIn("UPSTREAM_URL: http://127.0.0.1:8085", keepalive)
        self.assertIn("openai-api-cache-keeper: {condition: service_healthy", keepalive)
        keeper = section(self.compose, "  openai-api-cache-keeper:\n")
        for value in ("network_mode: service:ai-sse-keepalive-proxy-netns", "UPSTREAM_URL: http://127.0.0.1:8086", "LISTEN_ADDR: 127.0.0.1:8085", "STORE_DIR: /var/tmp/openai-api-cache-keeper", 'IGNORE_CLIENT_STORE_FALSE: "true"', "responses-ws-sse-bridge: {condition: service_healthy"):
            self.assertIn(value, keeper)
        for value in ("ROUTING_INDEX_URL", "UPSTREAM_CPA_URL", "UPSTREAM_AISIX_URL", "CPA_API_KEY"):
            self.assertNotIn(value, keeper)
        self.assertNotIn("  response-keeper:\n", self.compose)

    def test_keeper_limits_and_private_cache_are_preserved(self):
        keeper = section(self.compose, "  openai-api-cache-keeper:\n")
        for value in ('user: "65534:65534"', "pids_limit: 64", "mem_limit: 384m", "memswap_limit: 384m", "cpus: 0.5", "read_only: true", "cap_drop: [ALL]", "no-new-privileges:true", 'REQUIRE_NO_DEFAULT_ROUTE: "true"', "openai-api-cache-keeper-cache:/var/tmp/openai-api-cache-keeper", "[CMD, /openai-api-cache-keeper, healthcheck]"):
            self.assertIn(value, keeper)
        self.assertNotIn("tmpfs:", keeper)
        self.assertNotIn("    ports:", keeper)
        self.assertNotIn("    networks:", keeper)
        example = (ROOT / ".env.example").read_text()
        for name, value in (("STORE_TTL", "10m"), ("STORE_MAX_ENTRIES", "100"), ("STORE_MAX_BYTES", "0")):
            self.assertIn(f"{name}: ${{OPENAI_API_CACHE_KEEPER_{name}:-{value}}}", keeper)
            self.assertIn(f"OPENAI_API_CACHE_KEEPER_{name}={value}\n", example)

    def test_bridge_is_stateless_private_and_bounded(self):
        bridge = section(self.compose, "  responses-ws-sse-bridge:\n")
        for value in ("network_mode: service:ai-sse-keepalive-proxy-netns", "UPSTREAM_URL: http://sub2api-ai-sse-relay:8080", "LISTEN_ADDR: 127.0.0.1:8086", "SPOOL_DIR: /spool", "GOMEMLIMIT: 192MiB", 'user: "65534:65534"', "pids_limit: 64", "mem_limit: 256m", "memswap_limit: 256m", "cpus: 0.5", "read_only: true", "cap_drop: [ALL]", "no-new-privileges:true", "responses-ws-sse-bridge-spool:/spool"):
            self.assertIn(value, bridge)
        for value in ("tmpfs:", "    ports:", "    networks:", "STORE_DIR", "CPA_API_KEY"):
            self.assertNotIn(value, bridge)
        # Healthcheck reads PORT (default 8080 = keepalive in this netns); a wrong port deadlocks boot.
        listen = re.search(r"LISTEN_ADDR: 127\.0\.0\.1:(\d+)", bridge).group(1)
        self.assertRegex(bridge, r"\n      PORT: \"?" + listen + r"\"?\n")
        reference = re.search(r"image: \$\{RESPONSES_WS_SSE_BRIDGE_IMAGE:-([^}]+)\}", bridge).group(1)
        self.assertRegex(reference, r"^ghcr\.io/xz-dev/responses-ws-sse-bridge:[0-9a-f]{40}@sha256:[0-9a-f]{64}$")
        self.assertIn(f"RESPONSES_WS_SSE_BRIDGE_IMAGE={reference}\n", (ROOT / ".env.example").read_text())

    def test_generic_component_prepares_image_before_no_pull_recreate(self):
        ops = (ROOT / "ops/ansible/ops.yml").read_text()
        self.assertNotIn('Pull exact image for {{ service }}', ops)
        self.assertIn("when: service | default('') in ai_ops_profiles", ops)
        shared = (ROOT / 'ops/ansible/roles/ai_ops/tasks/front-component.yml').read_text()
        prepare, recreate = shared.split('- name: Recreate only selected front component', 1)
        self.assertIn('Verify exact candidate digest locally', prepare)
        self.assertIn('argv: ["{{ _runtime }}", pull, "{{ _front_scope.image }}"]', prepare)
        self.assertIn("ai_ops_profiles[service].get('pull', true)", prepare)
        self.assertIn("'--no-deps', '--no-build', '--pull', 'never'", recreate)

    def test_published_images_match_gitlinks_and_environment(self):
        env = (ROOT / ".env.example").read_text()
        modules = (ROOT / ".gitmodules").read_text()
        for name, key in (
            ("openai-api-cache-keeper", "OPENAI_API_CACHE_KEEPER_IMAGE"),
            ("ai-sse-keepalive-proxy", "AI_SSE_KEEPALIVE_PROXY_IMAGE"),
        ):
            revision = subprocess.check_output(["git", "-C", str(ROOT / "services/middleware" / name), "rev-parse", "HEAD"], text=True).strip()
            service = section(self.compose, f"  {name}:\n")
            reference = re.search(r"image: \$\{" + key + r":-([^}]+)\}", service).group(1)
            self.assertRegex(reference, rf"^ghcr\.io/xz-dev/{name}:{revision}@sha256:[0-9a-f]{{64}}$")
            self.assertIn(f"{key}={reference}\n", env)
            self.assertIn(f"path = services/middleware/{name}\n", modules)
            self.assertIn(f"url = https://github.com/xz-dev/{name}.git", modules)
        keepalive = section(self.compose, "  ai-sse-keepalive-proxy:\n")
        for value in ("mem_limit: 128m", "cpus: 0.5", "pids_limit: 128", "GOMEMLIMIT: ${AI_SSE_KEEPALIVE_PROXY_GOMEMLIMIT:-96MiB}", "MAX_INSPECT_BODY_BYTES: ${AI_SSE_KEEPALIVE_PROXY_MAX_INSPECT_BODY_BYTES:-8388608}"):
            self.assertIn(value, keepalive)
        script = (ROOT / "ops/scripts/validate.sh").read_text()
        self.assertIn("AI_SSE_KEEPALIVE_PROXY_IMAGE=ai-gateway-ai-sse-keepalive-proxy:validate \\\nOPENAI_API_CACHE_KEEPER_IMAGE=ai-gateway-openai-api-cache-keeper:validate \\\nBUILDAH_FORMAT=docker", script)
        self.assertEqual(script.count("python3 ops/scripts/tests/test-standard-cache-topology.py\n"), 1)

    def test_disposable_volumes_have_exact_owners_targets_and_cache_dir(self):
        script = (ROOT / "ops/scripts/validate.sh").read_text()
        program = script.split('services = compose.get("services") or {}\n', 1)[1].split("def service_networks", 1)[0]
        program = 'services = compose.get("services") or {}\n' + program
        keeper = "openai-api-cache-keeper"
        catalog = "model-catalog"
        bridge = "responses-ws-sse-bridge"
        cache = {
            "services": {
                keeper: {"volumes": [keeper + "-cache:/var/tmp/" + keeper]},
                bridge: {"volumes": [bridge + "-spool:/spool"]},
                catalog: {"volumes": [catalog + "-cache:/var/cache/" + catalog], "environment": {"CACHE_DIR": "/var/cache/" + catalog}},
            },
            "volumes": {keeper + "-cache": {}, catalog + "-cache": {}, bridge + "-spool": {}},
            "networks": {"isolated": {}},
        }
        exec(program, {"compose": cache})
        cases = []
        for volume, owner, target in ((keeper + "-cache", keeper, "/var/tmp/" + keeper), (catalog + "-cache", catalog, "/var/cache/" + catalog), (bridge + "-spool", bridge, "/spool")):
            for name in ("foreign-owner", "wrong-target", "missing-mount", "missing-volume"):
                bad = copy.deepcopy(cache)
                if name == "foreign-owner":
                    bad["services"]["intruder"] = {"volumes": [{"source": volume, "target": target}]}
                elif name == "wrong-target":
                    bad["services"][owner]["volumes"] = [volume + ":/wrong"]
                elif name == "missing-mount":
                    bad["services"][owner]["volumes"] = []
                else:
                    del bad["volumes"][volume]
                cases.append((name + ":" + owner, bad))
        bad = copy.deepcopy(cache)
        bad["services"][catalog]["environment"]["CACHE_DIR"] = "/tmp/cache"
        cases.append(("wrong-cache-dir", bad))
        bad = copy.deepcopy(cache)
        bad["volumes"]["unreviewed"] = {}
        cases.append(("unreviewed-volume", bad))
        bad = copy.deepcopy(cache)
        bad["networks"]["default"] = {}
        cases.append(("default-network", bad))
        for name, bad in cases:
            with self.subTest(name=name), self.assertRaises(SystemExit):
                exec(program, {"compose": bad})

    def test_legacy_ansible_tags_fail_before_runtime_or_writes(self):
        preflight = (ROOT / "ops/ansible/roles/ai_ops/tasks/preflight.yml").read_text()
        guard = preflight.split("- name: Detect Compose provider", 1)[0]
        self.assertIn("ansible.builtin.assert:", guard)
        retired = ("deploy-response-keeper", "deploy-responses-path", "restart-response-keeper",
                   "deploy-standard-cache-path", "restart-openai-api-cache-keeper", "rollback-standard-cache-path")
        for tag in retired:
            self.assertIn(tag, guard)
        self.assertNotIn("ansible.builtin.shell:", guard)
        self.assertNotIn("ansible.builtin.file:", guard)
        # Retired tags must not reach any task; only the preflight guard may name them.
        for path in (ROOT / "ops/ansible").rglob("*.yml"):
            if path.name != "preflight.yml":
                for tag in retired[3:]:
                    self.assertNotIn(tag, path.read_text(), path)

    def test_front_profiles_reject_unrelated_or_unpinned_changes(self):
        tasks = (ROOT / "ops/ansible/roles/ai_ops/tasks/front-component.yml").read_text()
        program = textwrap.dedent(tasks.split("      - |\n", 1)[1].split("    stdin:", 1)[0])
        keeper, bridge = "openai-api-cache-keeper", "responses-ws-sse-bridge"
        for service, expected in (
            (keeper, {"LISTEN_ADDR": "127.0.0.1:8085", "UPSTREAM_URL": "http://127.0.0.1:8086", "IGNORE_CLIENT_STORE_FALSE": "true"}),
            (bridge, {"LISTEN_ADDR": "127.0.0.1:8086", "PORT": "8086", "UPSTREAM_URL": "http://sub2api-ai-sse-relay:8080"}),
        ):
            config = {"services": {service: {
                "image": "ghcr.io/xz-dev/" + service + ":" + "a" * 40 + "@sha256:" + "b" * 64,
                "environment": expected, "network_mode": "service:ai-sse-keepalive-proxy-netns"},
                "apisix": {"image": "unchanged"}}, "volumes": {"retained": {}}}
            candidate = copy.deepcopy(config)
            candidate["services"][service]["image"] = config["services"][service]["image"].replace("b" * 64, "c" * 64)
            candidate["services"][service]["environment"]["GOMEMLIMIT"] = "192MiB"

            def check(value, accepted):
                result = subprocess.run(["python3", "-c", program], input=json.dumps({
                    "service": service, "current": config, "candidate": value, "expected_env": expected, "front_cache": True, "pull": True}),
                    text=True, capture_output=True, timeout=3)
                self.assertEqual(result.returncode == 0, accepted, result.stderr)
                self.assertNotIn("secret-fixture", result.stdout + result.stderr)

            check(candidate, True)
            for fault in ("unrelated", "volume", "public", "wrong-port", "tag", "routing", "capabilities"):
                bad = copy.deepcopy(candidate)
                target = bad["services"][service]
                if fault == "unrelated":
                    bad["services"]["apisix"]["environment"] = {"PASSWORD": "secret-fixture"}
                elif fault == "volume":
                    bad["volumes"] = {}
                elif fault == "public":
                    target["ports"] = ["8086:8086"]
                elif fault == "wrong-port":
                    target["environment"]["LISTEN_ADDR"] = "127.0.0.1:8080"
                elif fault == "tag":
                    target["image"] = "ghcr.io/xz-dev/" + service + ":latest"
                elif fault == "routing":
                    target["environment"]["CPA_API_KEY"] = "secret-fixture"
                elif fault == "capabilities":
                    target["cap_add"] = ["NET_ADMIN"]
                with self.subTest(service=service, fault=fault):
                    check(bad, False)
        profiles = (ROOT / "ops/ansible/roles/ai_ops/defaults/main.yml").read_text()
        for service in (keeper, bridge):
            self.assertRegex(profiles, rf"\n  {service}:\n    front_cache: true\n")
        self.assertIn("'--no-deps', '--no-build', '--pull', 'never', service", tasks)
        self.assertIn("env -i PATH=\"$PATH\"", tasks)
        self.assertLess(tasks.index("Require the exact approved front plan"), tasks.index("Pull only the exact pinned"))
        self.assertLess(tasks.index("Refuse changed live state immediately before"), tasks.index("Upload or restore only"))
        self.assertIn("mode: '0700'", tasks)
        self.assertIn("mode: '0600'", tasks)

    def test_operator_uses_shared_keepalive_not_public_apisix(self):
        owner = section(self.compose, "  operator-host-netns:\n")
        for value in ("<<: *netns-owner", '${OPERATOR_BIND_ADDRESS:-127.0.0.1}:${OPERATOR_PORT:-8087}:8080', "operator-host-source:", "host-operator-target:"):
            self.assertIn(value, owner)
        relay = section(self.compose, "  operator-host-relay:\n")
        for value in ("<<: *socat-relay", "network_mode: service:operator-host-netns", "TCP4-LISTEN:8080,bind=172.30.72.2,", "TCP4:172.30.73.2:8080,", "ai-sse-keepalive-proxy: {condition: service_started}"):
            self.assertIn(value, relay)
        self.assertNotIn("apisix", relay)
        self.assertNotIn("    ports:", relay)
        for endpoint in ("TCP4-LISTEN:8080,bind=172.30.72.2,", "TCP4:172.30.73.2:8080,"):
            self.assertIn(endpoint, (ROOT / "ops/scripts/validate.sh").read_text())
        shared = section(self.compose, "  ai-sse-keepalive-proxy-netns:\n")
        self.assertIn("host-operator-target:", shared)
        self.assertIn("ipv4_address: 172.30.73.2", shared)
        env = (ROOT / ".env.example").read_text()
        self.assertIn("OPERATOR_BIND_ADDRESS=127.0.0.1", env)
        self.assertIn("OPERATOR_PORT=8087", env)

    def test_operator_binding_guard_rejects_public_and_implicit_hosts(self):
        script = (ROOT / "ops/scripts/validate.sh").read_text()
        marker = 'python3 - "$rendered" "$rendered_format" <<\'PY\'\n'
        program = script.split(marker, 1)[1].split("\nPY", 1)[0]
        function = next(node for node in ast.parse(program).body if isinstance(node, ast.FunctionDef) and node.name == "private_host_binding")
        scope = {"ipaddress": ipaddress}
        exec(compile(ast.Module(body=[function], type_ignores=[]), "binding-guard", "exec"), scope)
        binding = scope["private_host_binding"]
        for port, expected in (
            ("127.0.0.1:8087:8080", True),
            ("100.64.0.1:8087:8080", True),
            ("100.127.255.254:8443:8080", True),
            ("[::1]:8087:8080", True),
            ({"host_ip": "127.0.0.1", "published": "8087", "target": 8080}, True),
            ("0.0.0.0:8087:8080", False),
            ("[::]:8087:8080", False),
            ("8087:8080", False),
            ("203.0.113.1:8087:8080", False),
            ("10.0.0.1:8087:8080", False),
            ("100.63.255.255:8087:8080", False),
            ("100.128.0.1:8087:8080", False),
            ("127.0.0.1:0:8080", False),
            ("127.0.0.1:65536:8080", False),
            ("127.0.0.1:8087:8085", False),
            ("127.0.0.1:8087:8080/udp", False),
            ({"published": "8087", "target": 8080}, False),
        ):
            with self.subTest(port=port):
                self.assertEqual(binding(port, 8080), expected)
        self.assertTrue(binding("127.0.0.1:9083:9080", 9080, 9083))
        self.assertFalse(binding("127.0.0.1:9084:9080", 9080, 9083))


if __name__ == "__main__":
    unittest.main()
