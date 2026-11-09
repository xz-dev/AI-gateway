#!/usr/bin/env python3
"""Render the production APISIX configs for the throwaway, single-network CI stack."""
import json
import os
from pathlib import Path
import subprocess
import yaml


def render(repo, run, cap, dns):
    for name, source in (("fw", "services/firewall/apisix"),
                         ("apisix-models", "services/routing/apisix-models")):
        dest = run / name
        dest.mkdir(parents=True, exist_ok=True)
        cfg = yaml.safe_load((repo / source / "config.yaml").read_text())
        cfg["apisix"]["node_listen"] = [{"port": 9080}]
        cfg["apisix"]["dns_resolver"] = [dns]
        nginx = cfg["nginx_config"]
        nginx["http"]["client_max_body_size"] = f"{cap // (1 << 20)}m"
        snippet = nginx.get("http_server_configuration_snippet", "")
        snippet = snippet.replace("Payload too large", f"Payload too large (limit {cap} bytes)")
        nginx["http_server_configuration_snippet"] = snippet + "\nproxy_request_buffering off;\n"
        (dest / "config.yaml").write_text(yaml.safe_dump(cfg, sort_keys=False))
        routes = yaml.safe_load((repo / source / "apisix.yaml").read_text())
        if name == "fw":
            routes["global_rules"][0]["plugins"]["real-ip"]["trusted_addresses"] = ["192.0.2.1/32"]
            for upstream in routes["upstreams"]:
                if upstream["id"] == "sub2api":
                    upstream["nodes"] = {"keeper:8085": 1}
        # The catalog is not exercised here; avoid resolving its absent production service.
        for upstream in routes["upstreams"]:
            if upstream["id"] == "model-catalog":
                upstream["nodes"] = {"mock:18080": 1}
        (dest / "apisix.yaml").write_text(yaml.safe_dump(routes, sort_keys=False) + "#END\n")
    cpa = (repo / "ci/large-request/cfg/cpa-config.yaml").read_text()
    (run / "cpa-config.yaml").write_text(cpa.replace("${CAP_BYTES}", str(cap)))


if __name__ == "__main__":
    networks = json.loads(subprocess.check_output(["timeout", "-k", "5", "60", "podman", "network", "inspect",
                                                   os.environ["PFX"] + "_chain"]))
    dns = networks[0]["subnets"][0]["gateway"]
    render(Path(os.environ["REPO_ROOT"]), Path(os.environ["RUN"]), int(os.environ["CAP_BYTES"]), dns)
