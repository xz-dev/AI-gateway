#!/usr/bin/env python3
"""Owned component lock and durable progress; unknown ownership never authorizes removal."""
import json
import os
from pathlib import Path
import re
import socket
import sys
import tempfile
import time


def atomic_json(path, value):
    fd, temporary = tempfile.mkstemp(prefix='.' + path.name, dir=path.parent)
    try:
        with os.fdopen(fd, 'w') as stream:
            os.fchmod(stream.fileno(), 0o600)
            json.dump(value, stream, sort_keys=True)
            stream.write('\n')
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
        directory = os.open(path.parent, os.O_DIRECTORY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def ancestors():
    ignored = {os.getpid()}
    pid = os.getppid()
    while pid > 1 and pid not in ignored:
        ignored.add(pid)
        pid = int(Path(f'/proc/{pid}/stat').read_text().rsplit(')', 1)[1].split()[1])
    return ignored


def live_writers(proc=Path('/proc'), ignored=None):
    ignored = ancestors() if ignored is None else ignored
    found = []
    for directory in proc.iterdir():
        if not directory.name.isdigit() or int(directory.name) in ignored:
            continue
        try:
            args = [a.decode(errors='replace') for a in (directory / 'cmdline').read_bytes().split(b'\0') if a]
        except FileNotFoundError:
            continue
        # Match executable/script tokens, not the text of this Python program.
        if any(Path(a).name == 'ansible-playbook' or (Path(a).name.startswith('AnsiballZ_') and a.endswith('.py')) for a in args[:3]) or (
                any(Path(a).name == 'docker' for a in args[:2]) and any(a in ('compose', 'pull') for a in args[1:4])):
            found.append(int(directory.name))
    return found


def paths(root, transaction):
    assert re.fullmatch(r'[0-9a-f]{32}', transaction), 'invalid transaction ID'
    state = Path(root) / '.ai-ops-state'
    return state / 'front-component.lock', state / ('transaction-' + transaction + '.json')


def owned(root, transaction):
    lock, receipt_path = paths(root, transaction)
    owner = json.loads((lock / 'owner.json').read_text())
    receipt = json.loads(receipt_path.read_text())
    assert owner['transaction'] == transaction and owner == receipt['owner'], 'lock owner/receipt mismatch'
    assert owner['root'] == str(Path(root)), 'lock belongs to another deployment'
    assert lock.stat().st_mode & 0o777 == 0o700, 'lock permissions changed'
    assert {p.name for p in lock.iterdir()} == {'owner.json'}, 'unknown lock contents'
    return lock, receipt_path, owner, receipt


def info(root):
    lock = Path(root) / '.ai-ops-state/front-component.lock'
    if not lock.exists():
        return {'held': False}
    try:
        owner = json.loads((lock / 'owner.json').read_text())
        return {'held': True, 'owner': owner}
    except (OSError, ValueError):
        return {'held': True, 'owner': 'unknown; manual review required'}


def acquire(root, request):
    lock, receipt_path = paths(root, request['transaction'])
    owner = {**request, 'root': str(Path(root)), 'started': time.time()}
    assert not receipt_path.exists(), 'transaction ID already used'
    lock.mkdir(mode=0o700)  # Never delete another writer's lock after mkdir fails.
    atomic_json(lock / 'owner.json', owner)
    atomic_json(receipt_path, {'owner': owner, 'stage': 'locked', 'outcome': 'in-progress', 'updated': time.time()})
    return {'receipt': str(receipt_path), 'owner': owner}


def progress(root, request):
    _, path, _, receipt = owned(root, request['transaction'])
    receipt.update({k: v for k, v in request.items() if k != 'transaction'})
    receipt['updated'] = time.time()
    atomic_json(path, receipt)
    return {'receipt': str(path), 'stage': receipt['stage']}


def release(root, request, recover=False):
    lock, path, owner, receipt = owned(root, request['transaction'])
    if recover:
        assert time.time() - owner['started'] >= max(900, request.get('min_age_seconds', 900)), 'lock is too recent'
        assert request.get('controller_clear') is True, 'controller liveness not checked'
        assert not live_writers(), 'remote deployment controller still alive'
        receipt.update(stage='stale-lock-recovered', outcome='recovery-required', updated=time.time())
        atomic_json(path, receipt)
    else:
        assert receipt['outcome'] in ('success', 'failed'), 'write final receipt before unlocking'
    (lock / 'owner.json').unlink()
    lock.rmdir()
    return {'released': True, 'transaction': owner['transaction'], 'receipt': str(path)}


def rollback_guard(root, request):
    state = Path(root) / '.ai-ops-state'
    service, snapshot, live = request['service'], request['snapshot'], request['live']
    after = None

    def refuse(reason):
        raise AssertionError('Rollback refused: ' + reason + '; live=' + json.dumps(live, sort_keys=True) +
                             '; snapshot=' + snapshot + '; after=' + json.dumps(after, sort_keys=True))

    try:
        target = Path(snapshot)
        if target.parent != state or not target.name.startswith('front-' + service + '-'):
            refuse('snapshot does not belong to this profile')
        own_transaction = request.get('transaction')
        if own_transaction:
            _, _, owner, _ = owned(root, own_transaction)
            assert owner['operation'] == 'rollback' and owner['service'] == service
        receipts = []
        for path in state.glob('transaction-*.json'):
            receipt = json.loads(path.read_text())
            owner = receipt['owner']
            if owner['service'] == service and owner['transaction'] != own_transaction:
                assert owner['root'] == str(Path(root))
                assert path == paths(root, owner['transaction'])[1]
                receipts.append(receipt)
        if not receipts:
            refuse('no transaction provenance; reconcile manually')
        latest = max(receipts, key=lambda r: r['owner']['started'])
        if latest.get('snapshot') != snapshot:
            refuse('newest transaction ' + latest['owner']['transaction'] +
                   ' has snapshot=' + str(latest.get('snapshot') or 'none') +
                   '; last.json is stale')
        if latest['owner']['operation'] != 'deploy':
            refuse('newest transaction is not the selected deployment')
        if latest['outcome'] == 'success':
            result = json.loads((target / 'receipt.json').read_text())
        elif latest['outcome'] == 'failed':
            result = json.loads((state / ('front-' + service + '-failure.json')).read_text())
            assert result['transaction'] == latest['owner']['transaction']
        else:
            refuse('newest transaction has no terminal readback; reconcile manually')
        assert result['service'] == service and result['snapshot'] == snapshot
        after = result.get('after')
        if not after or live != after:
            refuse('live identity differs from recorded after identity')
        return {'snapshot': snapshot, 'after': after}
    except AssertionError as exc:
        if str(exc).startswith('Rollback refused:'):
            raise
        refuse('invalid transaction/readback provenance; reconcile manually')
    except (OSError, KeyError, ValueError, TypeError):
        refuse('missing or invalid transaction/readback receipt; reconcile manually')


def controller_identity():
    controller = None
    pid = os.getpid()
    while pid > 1:
        proc = Path('/proc', str(pid))
        args = proc.joinpath('cmdline').read_bytes().split(b'\0')
        if any(Path(a.decode(errors='replace')).name == 'ansible-playbook' for a in args[:3] if a):
            controller = pid  # Outermost Ansible parent, not its per-task worker.
        pid = int(proc.joinpath('stat').read_text().rsplit(')', 1)[1].split()[1])
    assert controller is not None, 'no Ansible controller ancestor'
    return {'controller_host': socket.gethostname(), 'controller_pid': controller}


def controller_clear(owner):
    assert owner['controller_host'] == socket.gethostname(), 'recover from the original controller host'
    assert not Path('/proc', str(int(owner['controller_pid']))).exists(), 'original controller PID is still alive'
    assert not live_writers(), 'another local deployment controller is alive'
    return {'controller_clear': True}


if __name__ == '__main__':
    os.umask(0o077)
    try:
        action, root = sys.argv[1:]
        request = json.load(sys.stdin)
        if action == 'info':
            result = info(root)
        elif action == 'acquire':
            result = acquire(root, request)
        elif action == 'progress':
            result = progress(root, request)
        elif action == 'rollback-guard':
            result = rollback_guard(root, request)
        elif action == 'controller-identity':
            result = controller_identity()
        elif action == 'controller-check':
            result = controller_clear(request)
        elif action in ('release', 'recover'):
            result = release(root, request, action == 'recover')
        else:
            raise AssertionError('unknown lock action')
        print(json.dumps(result))
    except (AssertionError, OSError, KeyError, ValueError) as exc:
        sys.exit(str(exc) if action == 'rollback-guard' and str(exc).startswith('Rollback refused:') else
                 'component lock operation refused; inspect ownership/progress before recovery')
