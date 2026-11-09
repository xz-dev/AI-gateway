import tempfile
import unittest
from pathlib import Path

import yaml
from render_hops import render


class RenderHopsTest(unittest.TestCase):
    def test_pr_is_five_hops_and_nightly_uses_production_worker_quota(self):
        here = Path(__file__).resolve().parent
        base = yaml.safe_load((here / "compose.yaml").read_text())["services"]
        nightly = yaml.safe_load((here / "compose.full-stack.yaml").read_text())["services"]
        for hop in ("firewall", "apisix-models", "squid"):
            self.assertNotIn(hop, base)
            self.assertIn(hop, nightly)
        production = yaml.safe_load((here.parents[2] / "compose.yaml").read_text())["services"]
        names = {"pg": "postgres", "redis": "redis", "cpa": "cli-proxy-api", "aisix": "aisix",
                 "sub2api": "sub2api", "bridge": "responses-ws-sse-bridge", "keeper": "openai-api-cache-keeper",
                 "firewall": "apisix", "apisix-models": "apisix-models", "squid": "egress-proxy"}
        for hop, prod in names.items():
            service = base[hop] if hop in base else nightly[hop]
            self.assertEqual(service["cpus"], production[prod]["cpus"], hop)
        self.assertEqual(base["aisix"]["mem_limit"], "512m")
        self.assertEqual(base["sub2api"]["environment"]["GATEWAY_BODY_SPOOL_TICKET_FORWARD_HOSTS"], "aisix:3000")
        self.assertEqual(nightly["sub2api"]["environment"]["GATEWAY_BODY_SPOOL_TICKET_FORWARD_HOSTS"], "apisix-models:9080")

    def test_both_caps_render_production_routes_and_streaming_limits(self):
        repo = Path(__file__).resolve().parents[3]
        for cap in (268435456, 1073741824):
            with self.subTest(cap=cap), tempfile.TemporaryDirectory(dir="/var/tmp") as tmp:
                run = Path(tmp)
                render(repo, run, cap, "192.0.2.53")
                for hop in ("fw", "apisix-models"):
                    cfg = yaml.safe_load((run / hop / "config.yaml").read_text())
                    self.assertEqual(cfg["apisix"]["node_listen"], [{"port": 9080}])
                    self.assertEqual(cfg["apisix"]["dns_resolver"], ["192.0.2.53"])
                    self.assertEqual(cfg["nginx_config"]["http"]["client_max_body_size"], f"{cap // (1 << 20)}m")
                    self.assertIn("proxy_request_buffering off;", cfg["nginx_config"]["http_server_configuration_snippet"])
                    routes = yaml.safe_load((run / hop / "apisix.yaml").read_text())
                    self.assertTrue(routes["routes"])
                    self.assertTrue((run / hop / "apisix.yaml").read_text().endswith("#END\n"))
                    upstreams = {u["id"]: u["nodes"] for u in routes["upstreams"]}
                    self.assertEqual(upstreams["sub2api" if hop == "fw" else "aisix"],
                                     {"keeper:8085" if hop == "fw" else "aisix:3000": 1})
                cpa = yaml.safe_load((run / "cpa-config.yaml").read_text())
                self.assertEqual(cpa["server"]["max-request-body-bytes"], cap)
                self.assertIn(str(cap), (run / "fw/config.yaml").read_text())


if __name__ == "__main__":
    unittest.main()
