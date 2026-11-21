#!/usr/bin/env python3
"""Allowlisted native nginx catalog diagnostics. Never persist datagrams."""
import argparse
import datetime as dt
import json
import os
from pathlib import Path
import re
import signal
import socket
import time

SOCKET_NAME = 'events.sock'
MAX_EVENTS = 256
# Require the native nginx message prefix and request-context suffix. Lua log
# diagnostics, timeout text in user-controlled request values, and syslog
# messages truncated before request_id are not events.
NATIVE = re.compile(
    rb'\A<\d{1,3}>[A-Z][a-z]{2} [ 0-9]\d \d\d:\d\d:\d\d [^ \r\n]+ catalogdiag: '
    rb'\d{4}/\d\d/\d\d \d\d:\d\d:\d\d \[info\] \d+#\d+: \*\d+ '
    rb'client timed out \(110: Connection timed out\)(?: while sending (?:response )?to client)?'
    rb', client: [^,\r\n]+, server: [^,\r\n]*, request: "GET (/v1/models\?[^"\s]+) HTTP/1\.[01]"'
    rb', host: "[^"\r\n]+", request_id: "([0-9a-f]{32})"\Z'
)


def catalog_query(target):
    return any(field.startswith(b'client_version=') and len(field) > 15
               for field in target.split(b'?', 1)[1].split(b'&'))


def parse(packet):
    match = NATIVE.fullmatch(packet)
    if match and catalog_query(match[1]):
        return match[2].decode('ascii')
    return None


def collect(directory, output, writer_gid, expires):
    # The directory is new, directly under the intended container's logs dir.
    # Its random name is only disclosed to that container. No stale endpoint
    # is overwritten, including after an observer crash.
    endpoint = directory / SOCKET_NAME
    os.umask(0o077)
    directory.mkdir(mode=0o700)
    try:
        with socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM) as receiver:
            receiver.bind(str(endpoint))
            os.chown(endpoint, -1, writer_gid)
            os.chmod(endpoint, 0o620)
            os.chown(directory, -1, writer_gid)
            os.chmod(directory, 0o710)
            receiver.settimeout(0.5)
            with output.open('x', encoding='ascii', buffering=1) as events:
                seen = set()
                while time.time() < expires:
                    try:
                        packet, _, flags, _ = receiver.recvmsg(4096)
                    except socket.timeout:
                        continue
                    if time.time() >= expires:
                        break
                    request_id = None if flags & socket.MSG_TRUNC else parse(packet)
                    if request_id is None or request_id in seen:
                        continue
                    if len(seen) == MAX_EVENTS:
                        return 3  # The owner must stop the window on coverage loss.
                    seen.add(request_id)
                    events.write(json.dumps({
                        'utc': dt.datetime.now(dt.timezone.utc).isoformat(timespec='milliseconds'),
                        'event': 'front_catalog_native_timeout',
                        'request_id': request_id,
                    }, separators=(',', ':')) + '\n')
    finally:
        endpoint.unlink(missing_ok=True)
        directory.rmdir()
    return 0


if __name__ == '__main__':
    cli = argparse.ArgumentParser(description=__doc__)
    cli.add_argument('directory', type=Path)
    cli.add_argument('output', type=Path)
    cli.add_argument('writer_gid', type=int)
    cli.add_argument('expires', type=float)
    args = cli.parse_args()
    # SIGTERM is used by the window owner on early closure. Leave no socket
    # behind and use a distinct status from ordinary expiry.
    def stop(*_):
        raise SystemExit(2)

    signal.signal(signal.SIGTERM, stop)
    raise SystemExit(collect(args.directory, args.output, args.writer_gid, args.expires))
