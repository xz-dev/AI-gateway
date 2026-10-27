#!/usr/bin/env python3
"""Fast guards; full isolated Ansible lifecycle: test-component-apply.py."""
import configparser
import importlib.util
import json
import os
from pathlib import Path
import re
import socket
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[3]


def load(name):
    spec = importlib.util.spec_from_file_location(name, ROOT / 'ops/scripts/host' / (name + '.py'))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


IMAGE = load('component-image')
LOCK = load('component-lock')
PIN = 'sha256:' + 'a' * 64
ID = 'sha256:' + 'b' * 64
REF = 'example.test/component:version@' + PIN


class ImageProof(unittest.TestCase):
    def test_manifest_digest_is_not_assumed_to_equal_config_id(self):
        self.assertEqual(IMAGE.verify(REF, {'Id': ID, 'RepoDigests': ['example.test/component@' + PIN]})['image_id'], ID)

    def test_mismatched_digest_is_refused(self):
        with self.assertRaisesRegex(AssertionError, 'pinned digest'):
            IMAGE.verify(REF, {'Id': ID, 'RepoDigests': ['example.test/component@' + ID]})

    def test_missing_image_after_skipped_pull_is_refused(self):
        with patch.object(IMAGE, 'run') as command:
            command.return_value.returncode = 1
            with self.assertRaisesRegex(AssertionError, 'not present'):
                IMAGE.inspect('docker', REF, 'local')
            self.assertEqual(command.call_count, 1)

    def test_plan_checks_registry_without_pull(self):
        with patch.object(IMAGE, 'run') as command:
            command.side_effect = [type('Result', (), {'returncode': 1})(), type('Result', (), {
                'returncode': 0, 'stdout': json.dumps({'schemaVersion': 2, 'config': {}})})()]
            self.assertEqual(IMAGE.inspect('docker', REF, 'plan')['source'], 'registry_manifest')
            self.assertEqual(command.call_args.args[0], ['docker', 'manifest', 'inspect', REF])

    def test_preload_never_contacts_registry(self):
        with patch.object(IMAGE, 'run') as command:
            command.return_value.returncode = 1
            with self.assertRaises(AssertionError):
                IMAGE.inspect('docker', 'localhost/image:preloaded', 'plan', True)
            self.assertEqual(command.call_count, 1)

    def test_unpinned_requires_preload_profile(self):
        with self.assertRaises(AssertionError):
            IMAGE.verify('localhost/image:tag', {'Id': ID})
        self.assertEqual(IMAGE.verify('localhost/image:tag', {'Id': ID}, True)['source'], 'local')

    def test_archive_pin_requires_preload_even_when_present(self):
        for image in ('rollback-20261015/cloudflared:2026.10.0@' + PIN,
                      'localhost/component:version@' + PIN):
            with self.subTest(image=image), patch.object(IMAGE, 'run') as command:
                command.return_value.returncode = 0
                command.return_value.stdout = json.dumps({'Id': ID, 'RepoDigests': [image]})
                with self.assertRaisesRegex(AssertionError, 'preloaded image requires pull: false'):
                    IMAGE.inspect('docker', image, 'plan')
                self.assertEqual(IMAGE.inspect('docker', image, 'plan', True)['image_id'], ID)
                self.assertTrue(all(call.args[0][1:3] == ['image', 'inspect']
                                    for call in command.call_args_list))

    def test_preloaded_profiles_disable_pull(self):
        defaults = (ROOT / 'ops/ansible/roles/ai_ops/defaults/main.yml').read_text()
        for service in ('cloudflared', 'cpa-model-sync', 'egress-proxy'):
            with self.subTest(service=service):
                profile = re.search(r'^  ' + re.escape(service) + r':.*\n(?:    .*\n)*', defaults, re.M)
                self.assertIsNotNone(profile)
                self.assertIn('\n    pull: false', profile.group())


class OwnedLock(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='component-lock-test-', dir='/var/tmp')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        (self.root / '.ai-ops-state').mkdir(mode=0o700)
        self.request = {'transaction': 'a' * 32, 'controller_host': socket.gethostname(),
                        'controller_pid': 99999999, 'service': 'models-enricher',
                        'operation': 'deploy', 'approval_digest': 'b' * 64}
        self.recovery = {'transaction': self.request['transaction'], 'controller_clear': True}

    def test_plan_leaves_no_lock_or_receipt(self):
        self.assertEqual(LOCK.info(self.root), {'held': False})
        self.assertEqual(list((self.root / '.ai-ops-state').iterdir()), [])

    def test_durable_progress_modes_and_owned_release(self):
        result = LOCK.acquire(self.root, self.request)
        LOCK.progress(self.root, {**self.recovery, 'stage': 'activating'})
        self.assertEqual(json.loads(Path(result['receipt']).read_text())['stage'], 'activating')
        self.assertEqual(Path(result['receipt']).stat().st_mode & 0o777, 0o600)
        with self.assertRaises(AssertionError):
            LOCK.release(self.root, self.recovery)
        LOCK.progress(self.root, {**self.recovery, 'stage': 'complete', 'outcome': 'success'})
        self.assertTrue(LOCK.release(self.root, self.recovery)['released'])

    def test_other_writer_lock_is_never_removed(self):
        lock = self.root / '.ai-ops-state/front-component.lock'
        lock.mkdir(mode=0o700)
        with self.assertRaises(FileExistsError):
            LOCK.acquire(self.root, self.request)
        self.assertTrue(lock.exists())

    def test_recovery_refuses_recent_live_or_mismatched_owner(self):
        LOCK.acquire(self.root, self.request)
        with self.assertRaisesRegex(AssertionError, 'too recent'):
            LOCK.release(self.root, self.recovery, True)
        later = time.time() + 901
        with patch.object(LOCK.time, 'time', return_value=later), patch.object(LOCK, 'live_writers', return_value=[123]):
            with self.assertRaisesRegex(AssertionError, 'still alive'):
                LOCK.release(self.root, self.recovery, True)
        _, receipt, _, value = LOCK.owned(self.root, self.request['transaction'])
        value['owner']['controller_pid'] += 1
        LOCK.atomic_json(receipt, value)
        with self.assertRaisesRegex(AssertionError, 'mismatch'):
            LOCK.release(self.root, self.recovery, True)

    def test_killed_controller_can_be_recovered_only_with_receipt(self):
        LOCK.acquire(self.root, self.request)
        with patch.object(LOCK.time, 'time', return_value=time.time() + 901), patch.object(LOCK, 'live_writers', return_value=[]):
            self.assertTrue(LOCK.release(self.root, self.recovery, True)['released'])
        path = LOCK.paths(self.root, self.request['transaction'])[1]
        self.assertEqual(json.loads(path.read_text())['outcome'], 'recovery-required')

    def test_controller_must_be_dead_and_original_host(self):
        with self.assertRaises(AssertionError):
            LOCK.controller_clear({**self.request, 'controller_pid': os.getpid()})
        with self.assertRaises(AssertionError):
            LOCK.controller_clear({**self.request, 'controller_host': 'another-host'})

    def test_process_scan_does_not_match_quoted_source_but_does_match_writers(self):
        proc = self.root / 'proc'
        for pid, argv in [(1, ['python3', '-c', 'ansible-playbook docker compose']),
                          (2, ['docker', 'compose', 'pull']),
                          (3, ['python3', '/x/AnsiballZ_command.py'])]:
            p = proc / str(pid)
            p.mkdir(parents=True)
            (p / 'cmdline').write_bytes(b'\0'.join(s.encode() for s in argv))
        self.assertEqual(set(LOCK.live_writers(proc, set())), {2, 3})


class RollbackSelection(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='rollback-selection-', dir='/var/tmp')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.state = self.root / '.ai-ops-state'
        self.state.mkdir(mode=0o700)
        self.snapshot = self.state / 'front-models-enricher-original'
        self.snapshot.mkdir(mode=0o700)
        self.live = {'id': 'container-new', 'image': ID, 'started': '2026-10-25T01:00:00Z',
                     'running': True, 'paused': False, 'restarting': False}
        self.owner = {'transaction': 'a'*32, 'root': str(self.root), 'service': 'models-enricher',
                      'operation': 'deploy', 'started': 10}
        self.receipt = {'owner': self.owner, 'snapshot': str(self.snapshot), 'outcome': 'success'}
        LOCK.atomic_json(LOCK.paths(self.root, self.owner['transaction'])[1], self.receipt)
        self.result = {'service': 'models-enricher', 'snapshot': str(self.snapshot), 'after': self.live}
        LOCK.atomic_json(self.snapshot/'receipt.json', self.result)
        self.request = {'service': 'models-enricher', 'snapshot': str(self.snapshot), 'live': self.live}

    def test_exact_success_after_identity_and_unrelated_profile(self):
        other = {**self.receipt, 'owner': {**self.owner, 'service': 'other', 'transaction': 'b'*32, 'started': 20}}
        LOCK.atomic_json(LOCK.paths(self.root, 'b'*32)[1], other)
        self.assertEqual(LOCK.rollback_guard(self.root, self.request)['after'], self.live)

    def test_newer_failed_or_aborted_attempt_without_snapshot_refuses_stale_last(self):
        for outcome in ('failed', 'in-progress', 'recovery-required'):
            with self.subTest(outcome=outcome):
                newest = {**self.receipt, 'owner': {**self.owner, 'transaction': 'b'*32, 'started': 20},
                          'outcome': outcome, 'snapshot': ''}
                LOCK.atomic_json(LOCK.paths(self.root, 'b'*32)[1], newest)
                with self.assertRaisesRegex(AssertionError, r'Rollback refused:.*snapshot=none.*live=.*snapshot='):
                    LOCK.rollback_guard(self.root, self.request)

    def test_owned_rollback_rechecks_predecessor_without_hiding_an_intervening_attempt(self):
        LOCK.acquire(self.root, {**self.owner, 'transaction': 'c'*32, 'operation': 'rollback'})
        request = {**self.request, 'transaction': 'c'*32}
        self.assertEqual(LOCK.rollback_guard(self.root, request)['after'], self.live)
        newest = {**self.receipt, 'owner': {**self.owner, 'transaction': 'b'*32, 'started': 20},
                  'outcome': 'failed', 'snapshot': ''}
        LOCK.atomic_json(LOCK.paths(self.root, 'b'*32)[1], newest)
        with self.assertRaisesRegex(AssertionError, 'last.json is stale'):
            LOCK.rollback_guard(self.root, request)
        with self.assertRaisesRegex(AssertionError, 'invalid transaction/readback provenance'):
            LOCK.rollback_guard(self.root, {**self.request, 'transaction': 'b'*32})

    def test_same_image_replacement_restart_or_state_change_refused(self):
        for key, value in [('id', 'unrelated'), ('started', 'later'), ('running', False), ('image', PIN)]:
            with self.subTest(key=key), self.assertRaisesRegex(AssertionError, 'live identity differs.*live=.*after='):
                LOCK.rollback_guard(self.root, {**self.request, 'live': {**self.live, key: value}})

    def test_failed_activation_needs_its_own_recorded_after_identity(self):
        self.receipt['outcome'] = 'failed'
        LOCK.atomic_json(LOCK.paths(self.root, 'a'*32)[1], self.receipt)
        failure = self.state/'front-models-enricher-failure.json'
        for after in (None, {**self.live, 'id': 'other'}, self.live):
            LOCK.atomic_json(failure, {**self.result, 'transaction': 'a'*32, 'after': after})
            if after == self.live:
                self.assertEqual(LOCK.rollback_guard(self.root, self.request)['after'], self.live)
            else:
                with self.assertRaisesRegex(AssertionError, 'live identity differs'):
                    LOCK.rollback_guard(self.root, self.request)
        LOCK.atomic_json(failure, {**self.result, 'transaction': 'b'*32})
        with self.assertRaisesRegex(AssertionError, 'invalid transaction/readback provenance'):
            LOCK.rollback_guard(self.root, self.request)

    def test_missing_corrupt_or_nonterminal_evidence_fails_closed(self):
        for outcome in ('in-progress', 'recovery-required'):
            LOCK.atomic_json(LOCK.paths(self.root, 'a'*32)[1], {**self.receipt, 'outcome': outcome})
            with self.assertRaisesRegex(AssertionError, 'no terminal readback'):
                LOCK.rollback_guard(self.root, self.request)
        LOCK.atomic_json(LOCK.paths(self.root, 'a'*32)[1], self.receipt)
        (self.snapshot/'receipt.json').write_text('{}')
        with self.assertRaisesRegex(AssertionError, 'missing or invalid'):
            LOCK.rollback_guard(self.root, self.request)
        LOCK.paths(self.root, 'a'*32)[1].unlink()
        with self.assertRaisesRegex(AssertionError, 'no transaction provenance'):
            LOCK.rollback_guard(self.root, self.request)


class SSHModuleOutput(unittest.TestCase):
    def test_automation_does_not_request_a_terminal(self):
        config = configparser.ConfigParser()
        config.read(ROOT / 'ops/ansible/ansible.cfg')
        self.assertFalse(config.getboolean('ssh_connection', 'usetty', fallback=True))

    @unittest.skipUnless(importlib.util.find_spec('ansible'), 'requires installed Ansible SSH plugin')
    def test_async_json_is_not_prefixed_by_pam_terminal_context(self):
        # Exercise the real SSH argv selection and JSON filter, without a server.
        probe = r'''
import json
from unittest.mock import patch
from ansible.playbook.play_context import PlayContext
from ansible.plugins.action import _filter_non_json_lines
from ansible.plugins.loader import connection_loader
context = PlayContext()
context.remote_addr, context.remote_user = 'fixture', 'root'
connection = connection_loader.get('ssh', context)
connection.set_options()
body = b'{"started":true,"ansible_job_id":"fixture"}'
start = b'\x1b]3008;start=fixture;comm=su;type=session\x1b\\'
end = b'\x1b]3008;end=fixture\x1b\\'
def remote(command, *args, **kwargs):
    return 0, start + body + b'\r\n' + end if '-tt' in command else body, b''
def parse():
    _, stdout, _ = connection.exec_command('async_wrapper')
    filtered, _ = _filter_non_json_lines(stdout.decode(), objects_only=True)
    return json.loads(filtered)
with patch.object(connection, '_build_command', side_effect=lambda exe, subsystem, *args: list(args)), patch.object(connection, '_run', side_effect=remote):
    assert parse()['started'] is True
    connection.set_options(direct={'use_tty': True})
    try:
        parse()
    except ValueError:
        pass
    else:
        raise AssertionError('forced-PTY negative control did not reject OSC-prefixed JSON')
'''
        env = dict(os.environ, ANSIBLE_CONFIG=str(ROOT / 'ops/ansible/ansible.cfg'))
        env.pop('ANSIBLE_SSH_USETTY', None)
        result = subprocess.run([sys.executable, '-c', probe], env=env, text=True,
                                stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=20)
        self.assertEqual(result.returncode, 0, result.stdout)


class TaskOrdering(unittest.TestCase):
    def test_baseline_follows_readiness_and_group_readback(self):
        text = (ROOT / 'ops/ansible/roles/ai_ops/tasks/front-component.yml').read_text()
        self.assertLess(text.index('Wait for selected component health'), text.index('Advance baselines only after'))
        self.assertLess(text.index('Refuse post-activation drift'), text.index('Advance baselines only after'))
        self.assertLess(text.index('Record stage before selected component activation'), text.index('Recreate only selected'))
        self.assertIn("'--no-deps', '--no-build', '--pull', 'never'", text)
        helper = (ROOT / 'ops/ansible/roles/ai_ops/tasks/deploy-file.yml').read_text()
        self.assertNotIn('Adopt matching remote into baseline', helper)
        tail = helper.split('- name: "Record new baseline', 1)[1]
        self.assertIn('when: _ai_commit_baseline', tail)
        self.assertIn('ai_activate_cmd is defined and _ai_upload is changed', helper)
        self.assertIn('ai_defer_baseline', helper)
        self.assertIn('Read back activated file', helper)

    def test_generic_and_front_share_one_transaction(self):
        ops = (ROOT / 'ops/ansible/ops.yml').read_text()
        self.assertIn("when: service | default('') in ai_ops_profiles", ops)
        self.assertNotIn('Pull exact image for {{ service }}', ops)
        self.assertNotIn('Assert {{ service }} appears in ps output', ops)

    def test_commands_have_bounded_async_time_and_keep_preload_identity(self):
        text = (ROOT / 'ops/ansible/roles/ai_ops/tasks/front-component.yml').read_text()
        defaults = (ROOT / 'ops/ansible/roles/ai_ops/defaults/main.yml').read_text()
        self.assertEqual(text.count('async: "{{ ai_ops_component_command_timeout }}"'), 2)
        self.assertIn('ai_ops_component_command_timeout: 600', defaults)
        self.assertIn('_front_candidate_image_id == _front_planned_image_id', text)
        self.assertIn('Validate candidate Compose explicitly', text)
        self.assertIn('Validate uploaded candidate using the exact apply Compose argv', text)

    def test_rollback_guard_precedes_any_mutation(self):
        text = (ROOT / 'ops/ansible/roles/ai_ops/tasks/front-component.yml').read_text()
        self.assertLess(text.index('Refuse stale rollback selection'), text.index('Acquire exclusive owned'))
        self.assertEqual(text.count('rollback-guard'), 2)
        self.assertLess(text.index('Recheck rollback provenance under the owned lock'), text.index('Allocate immutable private snapshot'))
        self.assertIn("'after': _front_failure_after | default(none)", text)

    def test_existing_native_probe_is_reused_not_reimplemented(self):
        runtime = (ROOT / 'ops/ansible/roles/ai_ops/tasks/front-runtime.yml').read_text()
        self.assertIn('module.namespace_probe', runtime)
        self.assertIn('module.OBSERVE_HTTP', runtime)
        self.assertIn('module.worker_health', runtime)
        self.assertIn("return 'missing', False", runtime)


if __name__ == '__main__':
    unittest.main()
