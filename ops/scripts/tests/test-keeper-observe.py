#!/usr/bin/env python3
"""Offline stop-line, accounting and single-service fallback checks; no Docker calls."""
import copy
import datetime as dt
import importlib.util
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

spec = importlib.util.spec_from_file_location('keeper_observe', Path(__file__).resolve().parents[1] / 'host/keeper-observe.py')
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)
ENV = 'KEEP_EXISTING=private-fixture\nOPENAI_API_CACHE_KEEPER_STORE_TTL=24h\nOPENAI_API_CACHE_KEEPER_STORE_MAX_ENTRIES=10000\nOPENAI_API_CACHE_KEEPER_STORE_MAX_BYTES=0\n'


def normal():
    return dict(cache_bytes=123, cache_files=3, free_bytes={'/': 5*m.GIB, '/var/lib/docker': 5*m.GIB},
                **m.memory_metrics(30*m.MIB, 10*m.MIB, str(384*m.MIB), 20*m.MIB), cpu_interval_seconds=None,
                cpu_cores=None, cpu_limit_cores=0.4, cpu_usage_usec=1)


class ObservationTests(unittest.TestCase):
    def test_retained_log_counts_and_outlier_do_not_claim_real_hit_bytes(self):
        lines = [f'2026-10-23T{clock}Z transport=ws status={status} previous={previous} stored={stored} duration=1s store_entries={i} store_bytes={i*100}'
                 for i, (clock, status, previous, stored) in enumerate([
                     ('04:46:59.000000000', '200', 'false', 'true'),
                     ('04:47:00.000000000', '200', 'true', 'false'),
                     ('04:58:43.000000000', '400', 'true', 'false'),
                     ('04:59:00.000000000', '200', 'false', 'true')])]
        start, end = map(dt.datetime.fromisoformat, ['2026-10-23T04:47:00+00:00', '2026-10-23T05:00:00+00:00'])
        value = m.summarize(lines, start, end)
        self.assertEqual(value['all'], dict(total=3, parent_requests=2, hits=1, unknown_parent_misses=1, full_replays=1, stored_true=1, stored_false=2))
        self.assertEqual(value['outlier']['total'], 2)
        self.assertEqual(value['without_outlier']['full_replays'], 1)
        self.assertEqual((value['store_entries'], value['store_bytes']), (3, 300))
        self.assertNotIn('client_avoided_bytes', value)

    def test_disk_boundaries_and_memory_cpu_never_authorize_revert(self):
        s = normal()
        s['free_bytes']['/'] = 4*m.GIB
        self.assertEqual(m.breaches(s), [])
        s['free_bytes']['/'] -= 1
        self.assertEqual(m.breaches(s), ['free_disk'])
        self.assertTrue(m.disk_only(m.breaches(s)))
        s['cache_bytes'] = m.GIB
        self.assertEqual(m.breaches(s), ['free_disk'])
        s['working_set_bytes'] = s['memory_stop_bytes']
        self.assertFalse(m.disk_only(m.breaches(s)))
        self.assertIn('memory', m.breaches(s))

    def test_reclaimable_page_cache_is_not_working_set_but_resident_heap_is(self):
        s = normal()
        s.update(m.memory_metrics(384*m.MIB, 330*m.MIB, str(384*m.MIB), 20*m.MIB))
        self.assertEqual(s['working_set_bytes'], 54*m.MIB)
        self.assertEqual(m.breaches(s), [])
        s.update(m.memory_metrics(380*m.MIB, 20*m.MIB, str(384*m.MIB), 20*m.MIB))
        self.assertEqual(m.breaches(s), ['memory'])
        self.assertEqual(m.memory_metrics(1, 2, 'max', 0)['memory_stop_bytes'], 345*m.MIB)
        self.assertEqual(m.memory_metrics(1, 2, 'max', 0)['working_set_bytes'], 0)

    def test_cpu_requires_full_interval_and_exceeding_quota_eighty_percent(self):
        s = normal()
        s.update(cpu_interval_seconds=59, cpu_cores=0.41)
        self.assertEqual(m.breaches(s), [])
        s['cpu_interval_seconds'] = 60
        self.assertEqual(m.breaches(s), ['cpu'])
        self.assertFalse(m.disk_only(m.breaches(s)))
        s['cpu_cores'] = 0.4
        self.assertEqual(m.breaches(s), [])

    def test_only_two_fallback_overrides_change_and_drift_is_rejected(self):
        text = m.fallback_env(ENV)
        self.assertIn('STORE_TTL=10m\n', text)
        self.assertIn('STORE_MAX_ENTRIES=100\n', text)
        self.assertIn('KEEP_EXISTING=private-fixture\n', text)
        self.assertIn('STORE_MAX_BYTES=0\n', text)
        for value in [ENV.replace('=24h', '=1h'), ENV + 'OPENAI_API_CACHE_KEEPER_STORE_TTL=24h\n', ENV.replace('STORE_MAX_BYTES=0', 'STORE_MAX_BYTES=1')]:
            with self.assertRaises(AssertionError):
                m.fallback_env(value)

    def test_effective_scope_rejects_adjacent_service_change(self):
        before = {'services': {m.SERVICE: {'image': 'same', 'environment': {'STORE_TTL': '24h', 'STORE_MAX_ENTRIES': '10000', 'STORE_MAX_BYTES': '0'}}, 'bridge': {'image': 'protected'}}}
        after = copy.deepcopy(before)
        after['services'][m.SERVICE]['environment'].update(STORE_TTL='10m', STORE_MAX_ENTRIES='100')
        m.check_fallback_scope(before, after)
        after['services']['bridge']['image'] = 'drift'
        with self.assertRaises(AssertionError):
            m.check_fallback_scope(before, after)


class TransactionTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(dir='/var/tmp')
        self.addCleanup(self.tmp.cleanup)
        self.root, self.state = Path(self.tmp.name)/'root', Path(self.tmp.name)/'state'
        self.root.mkdir(mode=0o700)
        self.state.mkdir(mode=0o700)
        (self.root/'.ai-ops-state').mkdir(mode=0o700)
        for name in m.FILES:
            (self.root/name).write_text(ENV if name == '.env' else 'fixture\n')
        self.c = {'Id': 'old', 'Image': 'image', 'RestartCount': 0,
                  'State': {'Running': True, 'StartedAt': 'start', 'Health': {'Status': 'healthy'}, 'OOMKilled': False},
                  'Config': {'Image': 'pinned', 'Env': ['STORE_TTL=24h', 'STORE_MAX_ENTRIES=10000', 'STORE_MAX_BYTES=0']}}
        self.expected = {'target': m.identity(self.c), 'protected': {'adjacent': {'id': 'adjacent'}}, 'hashes': m.hashes(self.root)}
        self.arm = {'expected': self.expected, 'image_ref': 'pinned', 'start': m.stamp(m.now()), 'end': m.stamp(m.now()+dt.timedelta(minutes=3))}
        for field, value in [('ROOT', self.root), ('STATE', self.state)]:
            patch = mock.patch.object(m, field, value)
            patch.start()
            self.addCleanup(patch.stop)

    def test_disk_fallback_recreates_only_keeper_and_keeps_protected_state(self):
        active = copy.deepcopy(self.c)
        commands = []
        def runtime(args, **kwargs):
            commands.append(args)
            if args[:3] == ['docker', 'image', 'inspect']:
                return 'image\n'
            self.assertIn('up', args)
            active['Id'] = 'new'
            active['Config']['Env'] = ['STORE_TTL=10m', 'STORE_MAX_ENTRIES=100', 'STORE_MAX_BYTES=0']
            return ''
        def state():
            return {'target': m.identity(active), 'protected': self.expected['protected'], 'hashes': m.hashes()}
        def render(root):
            env = dict(l.split('=', 1) for l in (root/'.env').read_text().splitlines())
            return {'services': {m.SERVICE: {'image': 'pinned', 'environment': {k: env['OPENAI_API_CACHE_KEEPER_'+k] for k in ['STORE_TTL', 'STORE_MAX_ENTRIES', 'STORE_MAX_BYTES']}}}}
        with mock.patch.object(m, 'run', side_effect=runtime), mock.patch.object(m, 'project_state', side_effect=state), mock.patch.object(m, 'render', side_effect=render), mock.patch.object(m, 'inspect_target', side_effect=lambda: active):
            receipt = m.revert(self.arm)
        self.assertEqual(receipt['after']['id'], 'new')
        self.assertEqual((self.root/'.env').read_text(), m.fallback_env(ENV))
        self.assertEqual((self.state/'revert-before/.env').read_text(), ENV)
        up = [c for c in commands if 'up' in c]
        self.assertEqual(len(up), 1)
        self.assertEqual(up[0][-8:], ['up', '-d', '--force-recreate', '--no-deps', '--no-build', '--pull', 'never', m.SERVICE])
        self.assertFalse((self.root/'.ai-ops-state/front-component.lock').exists())
        self.assertEqual((self.state/'revert-before').stat().st_mode & 0o777, 0o700)
        self.assertEqual((self.state/'revert-before/.env').stat().st_mode & 0o777, 0o600)

    def test_cas_and_existing_writer_refuse_before_production_write(self):
        with mock.patch.object(m, 'project_state', return_value={}), mock.patch.object(m, 'run') as command:
            with self.assertRaises(AssertionError):
                m.revert(self.arm)
            command.assert_not_called()
        self.assertEqual((self.root/'.env').read_text(), ENV)
        lock = self.root/'.ai-ops-state/front-component.lock'
        lock.mkdir()
        with self.assertRaises(FileExistsError):
            m.revert(self.arm)
        self.assertTrue(lock.exists())

    def test_memory_even_with_disk_breach_stops_without_revert(self):
        m.write_json(self.state/'arm.json', self.arm)
        cost = normal()
        cost.update(working_set_bytes=cost['memory_stop_bytes'], cache_bytes=m.GIB)
        with mock.patch.object(m, 'inspect_target', return_value=self.c), mock.patch.object(m, 'resources', return_value=cost), mock.patch.object(m, 'activity', return_value=m.summarize([], m.now(), m.now())), mock.patch.object(m, 'revert') as revert:
            self.assertEqual(m.observe(), 0)
            revert.assert_not_called()
        event = json.loads((self.state/'EVENT.json').read_text())
        self.assertEqual(event['event'], 'resource_stop')
        self.assertFalse(event['production_changed'])
        self.assertEqual((self.state/'EVENT.json').stat().st_mode & 0o777, 0o600)
        self.assertEqual((self.root/'.env').read_text(), ENV)

    def test_large_cache_is_record_only_and_one_note_does_not_stop_window(self):
        m.write_json(self.state/'arm.json', self.arm)
        cost = normal()
        cost['cache_bytes'] = m.GIB
        self.assertEqual(m.breaches(cost), [])
        with mock.patch.object(m, 'inspect_target', return_value=self.c), mock.patch.object(m, 'resources', return_value=cost), mock.patch.object(m, 'activity', return_value=m.summarize([], m.now(), m.now())), mock.patch.object(m, 'revert') as revert, mock.patch.object(m.time, 'sleep') as sleep, mock.patch.object(m.time, 'monotonic', side_effect=[0, 0, 1, 1, 60, 181]):
            self.assertEqual(m.observe(), 0)
            revert.assert_not_called()
            sleep.assert_called_once()
        samples = [json.loads(l) for l in (self.state/'samples.jsonl').read_text().splitlines()]
        self.assertEqual(len(samples), 2)
        self.assertEqual(samples[0]['notes'], ['cache_1gib_crossed'])
        self.assertNotIn('notes', samples[1])
        self.assertEqual(json.loads((self.state/'EVENT.json').read_text())['event'], 'finished')

    def test_restart_or_oom_has_explicit_event_and_no_monitor_mutation(self):
        m.write_json(self.state/'arm.json', self.arm)
        self.c['RestartCount'], self.c['State']['OOMKilled'] = 1, True
        with mock.patch.object(m, 'inspect_target', return_value=self.c), mock.patch.object(m, 'revert') as revert:
            self.assertEqual(m.observe(), 1)
            revert.assert_not_called()
        value = json.loads((self.state/'EVENT.json').read_text())
        self.assertEqual(value['event'], 'keeper_restarted')
        self.assertEqual((value['restart_count'], value['oom_killed']), (1, True))

    def test_disk_breach_is_one_attempt_and_receipt_has_production_hashes(self):
        m.write_json(self.state/'arm.json', self.arm)
        cost = normal()
        cost['free_bytes']['/'] = 4*m.GIB - 1
        with mock.patch.object(m, 'inspect_target', return_value=self.c), mock.patch.object(m, 'resources', return_value=cost), mock.patch.object(m, 'activity', return_value=m.summarize([], m.now(), m.now())), mock.patch.object(m, 'revert', return_value={'after': {}, 'production_hashes': self.expected['hashes']}) as revert:
            self.assertEqual(m.observe(), 0)
            revert.assert_called_once_with(self.arm)
            with self.assertRaises(AssertionError):
                m.observe()
        event = json.loads((self.state/'EVENT.json').read_text())
        self.assertEqual(event['event'], 'disk_reverted')
        self.assertEqual(event['production_hashes'], self.expected['hashes'])
        self.assertTrue((self.state/'REVERT-ATTEMPT.json').exists())


if __name__ == '__main__':
    unittest.main()
