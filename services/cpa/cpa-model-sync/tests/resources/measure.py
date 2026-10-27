#!/usr/bin/env python3
"""Measure the candidate alone; the synthetic CPA has its own container/cgroup."""
import json
import os
from pathlib import Path
import queue
import subprocess
import tempfile
import threading
import time
import uuid

root = Path(__file__).resolve().parents[2]
image = os.environ.get("SYNC_IMAGE", "localhost/cpa-model-sync:v0.1.0")
work = Path(tempfile.mkdtemp(prefix="cpa-sync-memory-", dir="/tmp"))
name = "cpa-sync-memory-" + uuid.uuid4().hex[:10]
network, mock, sync = name, name + "-mock", name + "-rust"


def run(*args):
    return subprocess.check_output(args, text=True, stderr=subprocess.PIPE).strip()


subprocess.run(["go", "build", "-o", str(work / "mock"), str(root / "tests/resources/probe.go")],
               env={**os.environ, "CGO_ENABLED": "0"}, check=True)
(work / "config.json").write_text(json.dumps({
    "cpa_url": "http://cpa:8317", "client_version": "test", "interval_seconds": 10,
}))
results = {"image": image, "rounds": [], "limit": "256 MiB", "cpu_limit": 0.5}
stop = threading.Event()
logs = None
sampler = None
try:
    run("podman", "network", "create", "--internal", network)
    run("podman", "run", "-d", "--name", mock, "--network", network,
        "--network-alias", "cpa", "--read-only", "--cap-drop=ALL", "--user", "65534:65534",
        "-v", str(work / "mock") + ":/mock:ro", "--entrypoint", "/mock", image)
    start = time.monotonic()
    run("podman", "run", "-d", "--name", sync, "--network", network,
        "--read-only", "--cap-drop=ALL", "--pids-limit=16", "--memory=256m", "--memory-swap=256m", "--cpus=0.5",
        "-v", str(work / "config.json") + ":/app/config.json:ro",
        "-e", "CPA_MANAGEMENT_KEY=synthetic-management", image)
    pid = run("podman", "inspect", "--format", "{{.State.Pid}}", sync)
    status = Path("/proc") / pid / "status"
    samples = []

    def sample():
        while not stop.is_set():
            try:
                fields = dict(line.split(":", 1) for line in status.read_text().splitlines() if ":" in line)
                samples.append((time.monotonic() - start, int(fields["VmRSS"].split()[0]), int(fields["VmHWM"].split()[0])))
            except (OSError, KeyError):
                break
            stop.wait(0.02)

    sampler = threading.Thread(target=sample)
    sampler.start()
    messages = queue.Queue()
    logs = subprocess.Popen(["podman", "logs", "-f", sync], stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)

    def read_logs():
        for line in logs.stdout:
            try:
                value = json.loads(line)
                if "updated" in value:
                    messages.put(value)
            except ValueError:
                pass
        messages.put(None)

    reader = threading.Thread(target=read_logs, daemon=True)
    reader.start()
    for number in range(5):
        summary = messages.get(timeout=90)
        if summary is None:
            raise RuntimeError("candidate exited: " + run("podman", "inspect", "--format", "{{.State.OOMKilled}} {{.State.ExitCode}}", sync))
        elapsed = time.monotonic() - start
        time.sleep(0.25)
        expected = {"updated": 2 if number == 0 else 0, "unchanged": 0 if number == 0 else 2, "failed": 0, "unconfirmed": 0}
        if summary != expected:
            raise RuntimeError("unexpected round: " + json.dumps(summary))
        results["rounds"].append({"seconds": round(elapsed, 3), "summary": summary,
                                  "idle_rss_kib": samples[-1][1],
                                  "container_memory": run("podman", "stats", "--no-stream", "--format", "{{.MemUsage}}", sync)})
    results["first_sample_rss_kib"] = samples[0][1]
    results["peak_rss_kib"] = max(s[2] for s in samples)
    results["mock_shape"] = run("podman", "logs", mock)
    results["separate_mock_memory"] = run("podman", "stats", "--no-stream", "--format", "{{.MemUsage}}", mock)
except Exception as error:
    results["error"] = str(error)
finally:
    stop.set()
    if sampler:
        sampler.join()
    if logs:
        logs.terminate()
        logs.wait()
    for container in (sync, mock):
        subprocess.run(["podman", "rm", "-f", container], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    subprocess.run(["podman", "network", "rm", network], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    (work / "result.json").write_text(json.dumps(results, indent=2))
    print(json.dumps(results, indent=2))
    print("Evidence:", work)
if "error" in results:
    raise SystemExit(1)
