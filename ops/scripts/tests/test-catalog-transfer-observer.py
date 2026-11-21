#!/usr/bin/env python3
"""Exercise the diagnostic Unix socket and the only persisted fields."""
import json
import os
from pathlib import Path
import socket
import stat
import subprocess
import sys
import tempfile
import time
import unittest

RECEIVER = Path(__file__).resolve().parents[1] / 'host/catalog-transfer-observer.py'
KEY = b'0123456789abcdef0123456789abcdef'
MESSAGE = (
    b'<190>Nov  9 03:10:32 front catalogdiag: 2026/11/09 03:10:32 [info] 25#25: *38 '
    b'client timed out (110: Connection timed out), client: 127.0.0.1, server: _, '
    b'request: "GET /v1/models?private=ignore&client_version=1 HTTP/1.1", '
    b'host: "mock.invalid", request_id: "' + KEY + b'"'
)


class DiagnosticSocket(unittest.TestCase):
    def test_allowlist_and_expiry(self):
        with tempfile.TemporaryDirectory() as work:
            directory = Path(work) / 'diag'
            output = Path(work) / 'events'
            endpoint = directory / 'events.sock'
            worker = subprocess.Popen([sys.executable, RECEIVER, directory, output,
                                       str(os.getgid()), str(int(time.time()) + 3)])
            try:
                deadline = time.monotonic() + 2
                while not output.exists() and worker.poll() is None and time.monotonic() < deadline:
                    time.sleep(0.02)
                self.assertTrue(output.exists())
                self.assertEqual(stat.S_IMODE(directory.stat().st_mode), 0o710)
                self.assertEqual(stat.S_IMODE(endpoint.stat().st_mode), 0o620)
                self.assertEqual(stat.S_IMODE(output.stat().st_mode), 0o600)
                with socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM) as stream:
                    for packet in (
                        MESSAGE.replace(b'/v1/models?', b'/v1/responses?'),
                        MESSAGE.replace(b'client_version=1', b'client_version='),
                        MESSAGE.replace(b'client timed out', b'[lua] client timed out'),
                        MESSAGE[:-1],
                        MESSAGE.replace(b'private=ignore', b'private=' + b'X' * 4000),
                        MESSAGE, MESSAGE,
                    ):
                        stream.sendto(packet, str(endpoint))
                self.assertEqual(worker.wait(4), 0)
                self.assertFalse(endpoint.exists())
                data, = map(json.loads, output.read_text().splitlines())
                self.assertEqual(data.pop('event'), 'front_catalog_native_timeout')
                self.assertEqual(data.pop('request_id'), KEY.decode())
                self.assertEqual(list(data), ['utc'])
                self.assertNotIn('private', output.read_text())
            finally:
                if worker.poll() is None:
                    worker.kill()
                    worker.wait(2)


if __name__ == '__main__':
    unittest.main()
