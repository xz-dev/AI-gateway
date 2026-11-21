#!/usr/bin/env python3
"""Verify passive window lifecycle without a production host."""
import importlib.util
import os
from pathlib import Path
import signal
import subprocess
import tempfile
import time
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('window', Path(__file__).resolve().parents[1] / 'host/catalog-transfer-window.py')
w = importlib.util.module_from_spec(spec)
spec.loader.exec_module(w)
FRONT = dict(cid='a' * 64, pid=7, started='before')
BASE = b'error_log logs/error.log warn;\nhttp {\n    send_timeout 10s;\n}\n'


class Window(unittest.TestCase):
    def setUp(self):
        self.tmp = self.enterContext(tempfile.TemporaryDirectory())
        self.state = Path(self.tmp) / 'state'
        self.live = Path(self.tmp) / 'nginx.conf'
        self.live.write_bytes(BASE)
        self.identity = self.enterContext(patch.object(w, 'identity', return_value=FRONT))
        self.enterContext(patch.object(w, 'config_path', return_value=self.live))
        self.workers = self.enterContext(patch.object(w, 'workers', return_value=set()))
        self.reload = self.enterContext(patch.object(w, 'reload_config'))
        self.enterContext(patch.object(w, 'run'))
        w.preview(self.state, FRONT['cid'], 'b' * 64, 60)
        self.info = w.data(self.state)
        self.pending = (self.state / 'candidate').read_bytes()

    def activate(self):
        self.live.write_bytes(self.pending)
        w.stage(self.state, self.info, 'active')

    def test_preview_does_not_modify_live_and_keeps_timeout(self):
        self.assertEqual(self.live.read_bytes(), BASE)
        self.assertEqual(self.pending.count(b'send_timeout 10s;'), 1)
        self.assertIn(b'syslog:server=unix:', self.pending)
        self.assertEqual(self.pending.count(b'error_log logs/error.log warn;'), 2)
        with self.assertRaises(RuntimeError):
            w.candidate(BASE.replace(b'10s', b'60s'), self.info['tag'])

    def test_lost_controller_reverts_and_does_not_rearm(self):
        self.activate()
        (self.state / 'arm').touch()
        with patch.object(w, 'retire_orphan') as cut, patch.object(w, 'unschedule') as schedule:
            w.tick(self.state)
            cut.assert_called_once()
            schedule.assert_called_once()
            self.assertEqual(self.live.read_bytes(), BASE)
            self.assertEqual(w.data(self.state)['reason'], 'supervisor-lost')
            w.tick(self.state)
            self.assertEqual(w.data(self.state)['phase'], 'closed')
            self.reload.assert_called_once()

    def test_restart_reads_back_baseline_without_reloading(self):
        self.activate()
        self.live.write_bytes(BASE)
        self.identity.return_value = dict(FRONT, pid=99, started='after')
        w.recover(self.state, self.info, 'observer-or-front-changed')
        self.reload.assert_not_called()
        self.assertEqual(w.data(self.state)['outcome'], 'restart-restored')

    def test_unrelated_drift_is_never_overwritten(self):
        self.activate()
        third = self.pending.replace(b'send_timeout 10s', b'send_timeout 60s')
        self.live.write_bytes(third)
        w.recover(self.state, self.info, 'deadline')
        self.assertEqual(self.live.read_bytes(), third)
        self.assertEqual(w.data(self.state)['phase'], 'recovery-required')
        self.reload.assert_not_called()

    def test_deadline_and_receiver_failure_restore(self):
        for mode in ('deadline', 'receiver-fault'):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory() as temp:
                scratch = Path(temp)
                state = scratch / 'lease'
                endpoint = scratch / 'private'
                self.live.write_bytes(BASE)
                w.preview(state, FRONT['cid'], 'b' * 64, 2)

                def reload_front(*_):
                    if mode == 'receiver-fault' and self.live.read_bytes() != BASE:
                        pid, _ = w.data(state)['receiver']
                        os.kill(pid, signal.SIGTERM)
                    return set()

                with patch.object(w, 'socket_directory', return_value=endpoint), \
                        patch.object(w, 'tunnel_failure', return_value=False), \
                        patch.object(w, 'OBSERVE_INTERVAL', .025), \
                        patch.object(w, 'reload_config', side_effect=reload_front):
                    w.supervise(state, w.data(state))
                self.assertEqual(self.live.read_bytes(), BASE)
                final = w.data(state)
                self.assertEqual(final['phase'], 'closed')
                self.assertEqual(final['reason'], 'deadline' if mode == 'deadline' else 'observer-or-front-changed')
                self.assertFalse(endpoint.exists())

    def test_draining_is_not_closed_until_old_workers_disappear(self):
        self.activate()
        self.workers.return_value = {'10:100'}
        with patch.object(w.time, 'sleep'):
            w.recover(self.state, self.info, 'deadline')
            self.assertEqual(w.data(self.state)['phase'], 'draining')
            w.tick(self.state)
            self.assertEqual(w.data(self.state)['phase'], 'draining')
            self.workers.return_value = {'11:101'}
            w.tick(self.state)
            self.assertEqual(w.data(self.state)['phase'], 'closed')

    def test_scheduler_retires_exact_entry_and_leaves_neighbors(self):
        entry = w.cron_entry(self.state)
        before = b'# Existing job\n* * * * * /bin/true\n'
        original = before + entry + b'\n'
        with patch.object(w, 'run', side_effect=[original, original, before]), \
                patch.object(w.subprocess, 'run') as cmd:
            w.unschedule(self.state)
            self.assertEqual(cmd.call_args.kwargs['input'], before)
        with patch.object(w, 'run', side_effect=[original, original + b'# drift\n']), \
                patch.object(w.subprocess, 'run') as cmd, self.assertRaises(RuntimeError):
            w.unschedule(self.state)
            cmd.assert_not_called()

    def test_front_confirmation_needs_matching_request_id_and_catalog_200(self):
        key = '1' * 32
        events = self.state / 'events.jsonl'
        events.write_text('{"request_id": "' + key + '"}\n')
        access = (b'prefix "GET /v1/models?client_version=1 HTTP/1.1" 200 180061 10.000 "-" "' + key.encode() + b'"')
        with patch.object(w, 'docker_logs', return_value=[access]):
            self.assertTrue(w.front_failure(FRONT['cid'], time.time(), events))
        for wrong in (access.replace(key.encode(), b'2' * 32), access.replace(b' 200 ', b' 502 '), access.replace(b'/v1/models', b'/v1/responses')):
            with patch.object(w, 'docker_logs', return_value=[wrong]):
                self.assertFalse(w.front_failure(FRONT['cid'], time.time(), events))

    def test_tunnel_signal_reads_stderr_and_is_not_a_timeout_match(self):
        with patch.object(w.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, b'ERR reason="unexpected EOF" dest=/v1/models?client_version=1\n')) as cmd:
            self.assertTrue(w.tunnel_failure('b' * 64, time.time()))
            self.assertEqual(cmd.call_args.kwargs['stderr'], subprocess.STDOUT)
        with patch.object(w.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, b'ERR reason="unexpected EOF" dest=/v1/responses\n')):
            self.assertFalse(w.tunnel_failure('b' * 64, time.time()))


if __name__ == '__main__':
    unittest.main()
