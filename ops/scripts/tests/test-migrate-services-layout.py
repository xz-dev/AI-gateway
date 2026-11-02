#!/usr/bin/env python3
"""Fixture tests for ops/scripts/host/migrate-services-layout.py."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parents[1] / "host" / "migrate-services-layout.py"


def run(root, *args, private=False):
    command = [sys.executable, str(SCRIPT), "--root", str(root), *args]
    if private:
        command.insert(4, "--private")
    return subprocess.run(command, capture_output=True, text=True, check=False)


def write(root, rel, text, mode=0o644):
    path = root / rel
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text)
    os.chmod(path, mode)


def snapshot(root):
    """Map of every path below root (excluding migration state) to its content, mode or link target."""
    result = {}
    for directory, dirs, files in os.walk(root):
        dirs[:] = [d for d in dirs if d != ".layout-migration"]
        here = Path(directory)
        for name in files + [d for d in dirs if (here / d).is_symlink()]:
            path = here / name
            rel = str(path.relative_to(root))
            result[rel] = ("link", os.readlink(path)) if path.is_symlink() else (path.read_text(), path.stat().st_mode & 0o777)
        dirs[:] = [d for d in dirs if not (here / d).is_symlink()]
    return result


class MigrateLayoutTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(dir=os.environ.get("TMPDIR", "/var/tmp"))
        self.root = Path(self.tmp.name) / "deploy"
        files = {
            "compose.yaml": "services: {}\n",
            "apisix/apisix.yaml": "routes: []\n",
            "apisix/lua/model_list_intersection.lua": "return {}\n",
            "apisix-models/apisix.yaml": "routes: []\n",
            "aisix/config.yaml": "aisix\n",
            "aisix-status/README.md": "status\n",
            "models-enricher/config.yaml": "catalog\n",
            "cpa/config.example.yaml": "cpa\n",
            "cpa-model-sync/config.json": "{}\n",
            "egress-proxy/Dockerfile": "FROM scratch\n",
            "middleware/ai-sse-keepalive-proxy/go.mod": "module x\n",
            "scripts/start-apisix-models.sh": "#!/bin/sh\n",
            "scripts/ops/gateway-recovery.py": "print()\n",
            "scripts/test-netns-guard.sh": "#!/bin/sh\n",
            "scripts/validate.sh": "#!/bin/sh\n",
            "model-catalog-sidecar/templates/default.conf.template": "server {}\n",
            ".ai-ops-state/models-enricher__config.yaml.baseline": "catalog\n",
            "data/cpa/conf/config.yaml": "secret\n",
        }
        for rel, text in files.items():
            write(self.root, rel, text)
        os.chmod(self.root / "aisix/config.yaml", 0o444)
        os.chmod(self.root / "scripts/start-apisix-models.sh", 0o755)
        self.before = snapshot(self.root)

    def tearDown(self):
        self.tmp.cleanup()

    def receipts(self):
        return sorted((self.root / ".layout-migration").glob("*/receipt.json"))

    def test_plan_changes_nothing(self):
        result = run(self.root)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("move models-enricher -> services/routing/model-catalog", result.stdout)
        self.assertIn("move scripts/ops -> ops/scripts/host", result.stdout)
        self.assertEqual(snapshot(self.root), self.before)
        self.assertFalse((self.root / ".layout-migration").exists())

    def test_apply_moves_links_and_is_idempotent(self):
        result = run(self.root, "--apply")
        self.assertEqual(result.returncode, 0, result.stderr)
        after = snapshot(self.root)
        self.assertEqual(after["services/routing/model-catalog/config.yaml"], ("catalog\n", 0o644))
        self.assertEqual(after["services/routing/aisix/config.yaml"], ("aisix\n", 0o444))
        self.assertEqual(after["services/routing/apisix-models/start.sh"], ("#!/bin/sh\n", 0o755))
        self.assertEqual(after["ops/scripts/host/gateway-recovery.py"][0], "print()\n")
        self.assertEqual(after["ops/scripts/tests/test-netns-guard.sh"][0], "#!/bin/sh\n")
        self.assertEqual(after[".ai-ops-state/model-catalog__config.yaml.baseline"][0], "catalog\n")
        self.assertNotIn(".ai-ops-state/models-enricher__config.yaml.baseline", after)
        self.assertEqual(after["data/cpa/conf/config.yaml"], self.before["data/cpa/conf/config.yaml"])
        # Old bind-mount sources still resolve until finalize.
        self.assertEqual((self.root / "scripts/start-apisix-models.sh").read_text(), "#!/bin/sh\n")
        self.assertEqual((self.root / "models-enricher/config.yaml").read_text(), "catalog\n")
        self.assertTrue((self.root / "model-catalog-sidecar/templates/default.conf.template").is_file())
        backup = Path(json.loads(self.receipts()[0].read_text())["backup"])
        self.assertEqual((backup / "models-enricher/config.yaml").read_text(), "catalog\n")
        again = run(self.root, "--apply")
        self.assertEqual((again.returncode, again.stdout.strip()), (0, "nothing to do"), again.stderr)
        self.assertEqual(snapshot(self.root), after)
        self.assertEqual(len(self.receipts()), 1)
        self.assertEqual(run(self.root).stdout.strip(), "nothing to do")

    def test_conflict_aborts_before_any_change(self):
        write(self.root, "services/routing/model-catalog/config.yaml", "different\n")
        self.before = snapshot(self.root)
        result = run(self.root, "--apply")
        self.assertEqual(result.returncode, 1)
        self.assertIn("services/routing/model-catalog/config.yaml already exists with different content", result.stderr)
        self.assertEqual(snapshot(self.root), self.before)
        self.assertNotEqual(run(self.root).returncode, 0)

    def test_identical_destination_is_merged(self):
        write(self.root, "services/routing/model-catalog/config.yaml", "catalog\n")
        result = run(self.root, "--apply")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.root / "services/routing/model-catalog/config.yaml").read_text(), "catalog\n")

    def test_finalize_and_restore_round_trip(self):
        self.assertEqual(run(self.root, "--apply").returncode, 0)
        applied = snapshot(self.root)
        finalized = run(self.root, "--finalize")
        self.assertEqual(finalized.returncode, 0, finalized.stderr)
        tree = snapshot(self.root)
        self.assertFalse(any(kind == "link" for kind, _ in tree.values()), tree)
        for old in ("scripts", "models-enricher", "middleware", "model-catalog-sidecar"):
            self.assertFalse((self.root / old).exists() or (self.root / old).is_symlink(), old)
        self.assertEqual(run(self.root, "--finalize").stdout.strip(), "nothing to do")
        apply_receipt, finalize_receipt = self.receipts()
        self.assertEqual(run(self.root, "--restore", str(finalize_receipt)).returncode, 0)
        self.assertEqual(snapshot(self.root), applied)
        restored = run(self.root, "--restore", str(apply_receipt))
        self.assertEqual(restored.returncode, 0, restored.stderr)
        self.assertEqual(snapshot(self.root), self.before)
        for new in ("services", "ops"):
            self.assertFalse((self.root / new).exists(), new)

    def test_private_mode_moves_private_paths_only(self):
        root = Path(self.tmp.name) / "private"
        write(root, "models-enricher/config.yaml", "private\n", 0o600)
        write(root, "aisix/resources.yaml", "resources\n", 0o600)
        write(root, "compose.yaml", "services: {}\n")
        result = run(root, "--apply", private=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        tree = snapshot(root)
        self.assertEqual(tree["services/routing/model-catalog/config.yaml"], ("private\n", 0o600))
        self.assertEqual(tree["services/routing/aisix/resources.yaml"], ("resources\n", 0o600))
        self.assertFalse((root / "models-enricher").exists() or (root / "models-enricher").is_symlink())
        self.assertEqual(run(root, "--apply", private=True).stdout.strip(), "nothing to do")


if __name__ == "__main__":
    unittest.main(verbosity=1)
