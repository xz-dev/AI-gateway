#!/usr/bin/env python3
"""Temporary, cron-owned front APISIX diagnostics. State directory must be private."""
import argparse
import datetime as dt
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import stat
import subprocess
import sys
import tempfile
import time

PREFIX = '/usr/local/apisix/'
CONFIG = PREFIX + 'conf/nginx.conf'
RECEIVER = Path(__file__).with_name('catalog-transfer-observer.py')
MAX_WINDOW = 48 * 60 * 60
OBSERVE_INTERVAL = 10
NGINX = ['nginx', '-p', PREFIX]
HTTP = b'\nhttp {\n'
BASE_LOG = b'error_log logs/error.log warn;'
ID = re.compile(r'[0-9a-f]{64}')
TAG = re.compile(r'catalogdiag-[0-9a-f]{12}')


def run(*args):
    return subprocess.run(args, check=True, capture_output=True, timeout=15).stdout


def identity(cid):
    raw = run('docker', 'inspect', '-f', '{{.Id}} {{.State.Pid}} {{.State.StartedAt}} '
              '{{index .Config.Labels "com.docker.compose.service"}}', cid)
    observed_id, pid, started, service = raw.decode().split()
    if observed_id != cid or service != 'apisix' or int(pid) <= 1:
        raise RuntimeError('unexpected front identity')
    return dict(cid=cid, pid=int(pid), started=started)


def process_root(front):
    return Path('/proc') / str(front['pid']) / 'root'


def config_path(front):
    return process_root(front) / CONFIG.lstrip('/')


def candidate(baseline, tag):
    if not TAG.fullmatch(tag):
        raise ValueError('invalid diagnostic directory')
    if (baseline.count(HTTP) != 1 or baseline.splitlines().count(BASE_LOG) != 1
            or len(re.findall(rb'^\s*error_log\s', baseline, re.M)) != 1
            or len(re.findall(rb'^\s*send_timeout 10s;\s*$', baseline, re.M)) != 1):
        raise RuntimeError('unexpected baseline logging or timeout')
    extra = (f'    error_log logs/error.log warn;\n'
             f'    error_log syslog:server=unix:{PREFIX}logs/{tag}/events.sock,tag=catalogdiag info;\n').encode()
    return baseline.replace(HTTP, HTTP + extra)


def atomic(path, data, like=None):
    with tempfile.NamedTemporaryFile(dir=path.parent, delete=False) as temp:
        name = Path(temp.name)
        try:
            if like is not None:
                os.fchown(temp.fileno(), like.st_uid, like.st_gid)
                os.fchmod(temp.fileno(), stat.S_IMODE(like.st_mode))
            temp.write(data)
            temp.flush()
            os.fsync(temp.fileno())
            name.replace(path)
            parent = os.open(path.parent, os.O_DIRECTORY)
            try:
                os.fsync(parent)
            finally:
                os.close(parent)
        finally:
            name.unlink(missing_ok=True)


def save(state, payload):
    atomic(state / 'status.json', (json.dumps(payload, sort_keys=True) + '\n').encode())


def data(state):
    return json.loads((state / 'status.json').read_text())


def stage(state, payload, phase, **info):
    payload.update(phase=phase, **info)
    save(state, payload)


def digest(body):
    return hashlib.sha256(body).hexdigest()


def preview(state, cid, tunnel, seconds):
    if not (ID.fullmatch(cid) and ID.fullmatch(tunnel) and 1 <= seconds <= MAX_WINDOW):
        raise ValueError('invalid front, tunnel or observation limit')
    front = identity(cid)
    original = config_path(front).read_bytes()
    tag = 'catalogdiag-' + os.urandom(6).hex()
    pending = candidate(original, tag)
    state.mkdir(mode=0o700)
    atomic(state / 'baseline', original)
    atomic(state / 'candidate', pending)
    stage(state, dict(front=front, tunnel=tunnel, tag=tag, seconds=seconds, prepared=time.time(),
                      before=digest(original), after=digest(pending)), 'prepared')


def verify_state(state, info):
    before, after = (state / 'baseline').read_bytes(), (state / 'candidate').read_bytes()
    if (digest(before) != info['before'] or digest(after) != info['after']
            or after != candidate(before, info['tag'])):
        raise RuntimeError('unverified recovery material')
    return before, after


def workers(front):
    pid = front['pid']
    ids = (Path('/proc') / str(pid) / 'task' / str(pid) / 'children').read_text().split()
    result = set()
    for child in ids:
        proc = Path('/proc') / child
        try:
            if proc.joinpath('cmdline').read_bytes().startswith(b'nginx: worker process'):
                started = proc.joinpath('stat').read_text().rsplit(')', 1)[1].split()[19]
                result.add(f'{child}:{started}')
        except FileNotFoundError:
            continue
    return result


def configure(front, expected, replacement):
    cid = front['cid']
    config = config_path(front)
    if identity(cid) != front or config.read_bytes() != expected:
        raise RuntimeError('front drift before configuration write')
    old_workers = workers(front)
    cfg_stat = config.stat()
    name = '.catalogdiag-test-' + os.urandom(6).hex()
    test = config.with_name(name)
    try:
        atomic(test, replacement, cfg_stat)
        run('docker', 'exec', cid, *NGINX, '-t', '-c', PREFIX + 'conf/' + name)
    finally:
        test.unlink(missing_ok=True)
    if identity(cid) != front or config.read_bytes() != expected:
        raise RuntimeError('front drift after syntax check')
    atomic(config, replacement, cfg_stat)
    reload_config(front, old_workers)
    if config.read_bytes() != replacement:
        raise RuntimeError('configuration readback failed')
    return old_workers


def reload_config(front, old_workers):
    if identity(front['cid']) != front:
        raise RuntimeError('front drift before reload')
    run('docker', 'exec', front['cid'], *NGINX, '-s', 'reload', '-c', CONFIG)
    for _ in range(40):
        if workers(front) - old_workers:
            return
        time.sleep(0.25)
    raise RuntimeError('no fresh nginx worker')


def recover(state, info, reason):
    before, after = verify_state(state, info)
    try:
        front = identity(info['front']['cid'])
    except subprocess.CalledProcessError:
        # The pinned container was removed, possibly by a separate writer.
        # Never redirect rollback to a replacement container.
        stage(state, info, 'recovery-required', outcome='front-unavailable', reason=reason)
        return
    live = config_path(front).read_bytes()
    if front != info['front'] and live == before:
        stage(state, info, 'closed', outcome='restart-restored', reason=reason)
        return
    if front != info['front'] or live not in (before, after):
        stage(state, info, 'recovery-required', outcome='configuration-drift', reason=reason)
        return
    instrumented = workers(front)
    if live == after:
        instrumented = configure(front, after, before)
    elif info['phase'] != 'prepared':
        # A prior rollback might have died after publishing the baseline but
        # before sending HUP. Reload even if the file is already restored.
        reload_config(front, instrumented)
    for _ in range(20):
        if not (workers(front) & instrumented) or info['phase'] == 'prepared':
            stage(state, info, 'closed', outcome='restored', reason=reason)
            return
        time.sleep(0.25)
    stage(state, info, 'draining', reason=reason, old_workers=sorted(instrumented))


def docker_logs(cid, since):
    return subprocess.run(['docker', 'logs', '--since', str(int(since)), '--tail', '256', cid],
                          check=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                          timeout=15).stdout.splitlines()


CATALOG = rb'/v1/models\?(?:[^\s"\']*&)?client_version=[^&\s"\']+'


def tunnel_failure(cid, since):
    return any(b'unexpected eof' in line.lower() and re.search(CATALOG, line)
               for line in docker_logs(cid, since))


def front_failure(cid, since, events):
    ids = {row['request_id'].encode() for row in (json.loads(line) for line in events.read_text().splitlines() if line.endswith('}'))}
    if not ids:
        return False
    for line in docker_logs(cid, since):
        if re.search(rb'"GET ' + CATALOG + rb'[^"\s]* HTTP/1\.[01]" 200 \d+ [\d.]+ ', line):
            key = re.search(rb' "([0-9a-f]{32})"$', line)
            if key and key[1] in ids:
                return True
    return False


def socket_directory(info):
    return process_root(info['front']) / PREFIX.lstrip('/') / 'logs' / info['tag']


def process_start(pid):
    return Path(f'/proc/{pid}/stat').read_text().rsplit(')', 1)[1].split()[19]


def retire_orphan(info):
    receiver = info.get('receiver')
    if receiver:
        pid, born = receiver
        try:
            command = Path(f'/proc/{pid}/cmdline').read_bytes().split(b'\0')
            if (process_start(pid) == born and os.fsencode(RECEIVER) in command
                    and os.fsencode(socket_directory(info)) in command):
                os.kill(pid, signal.SIGTERM)
        except (FileNotFoundError, ProcessLookupError):
            pass
    directory = socket_directory(info)
    if not directory.exists():
        return
    try:
        same_front = identity(info['front']['cid']) == info['front']
    except subprocess.CalledProcessError:
        same_front = False
    if not same_front:
        raise RuntimeError('stale receiver path after front replacement')
    for _ in range(24):
        if not directory.exists():
            return
        time.sleep(.125)
    # SIGKILL outside Python's cleanup can leave a socket inode. Under the
    # private unique directory, remove only our expected stale socket.
    endpoint = directory / 'events.sock'
    if (directory.is_symlink() or directory.stat().st_uid != os.geteuid()
            or set(directory.iterdir()) not in (set(), {endpoint})):
        raise RuntimeError('unexpected diagnostic directory')
    if endpoint.exists():
        if not stat.S_ISSOCK(endpoint.lstat().st_mode) or endpoint.stat().st_uid != os.geteuid():
            raise RuntimeError('unexpected diagnostic endpoint')
        endpoint.unlink()
    directory.rmdir()


def supervise(state, info):
    front = info['front']
    before, after = verify_state(state, info)
    if time.time() - info['prepared'] > 900 or identity(front['cid']) != front:
        stage(state, info, 'closed', outcome='stale-preview')
        return
    end = time.time() + info['seconds']
    endpoint = socket_directory(info)
    gid = config_path(front).stat().st_gid
    output = state / 'events.jsonl'
    stage(state, info, 'arming', deadline=end)
    reason = 'activation-fault'
    receiver = subprocess.Popen([sys.executable, RECEIVER, endpoint, output, str(gid), str(end)],
                                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        stage(state, info, 'arming', receiver=[receiver.pid, process_start(receiver.pid)])
        for _ in range(40):
            if receiver.poll() is not None:
                raise RuntimeError('receiver exited before activation')
            if output.exists() and (endpoint / 'events.sock').is_socket():
                break
            time.sleep(.1)
        else:
            raise RuntimeError('receiver not ready')
        configure(front, before, after)
        started = time.time()
        stage(state, info, 'active', started=started)
        checked = started
        reason = 'observation-fault'
        while time.time() < end:
            if (state / 'stop').exists():
                reason = 'operator-stop'
                break
            if receiver.poll() is not None or identity(front['cid']) != front:
                reason = 'observer-or-front-changed'
                break
            now = time.time()
            if front_failure(front['cid'], started, output):
                reason = 'native-catalog-timeout'
                break
            if tunnel_failure(info['tunnel'], max(started, checked - 1)):
                reason = 'natural-tunnel-eof'
                break
            checked = now
            time.sleep(min(OBSERVE_INTERVAL, max(0, end - time.time())))
        else:
            reason = 'deadline'
    finally:
        try:
            recover(state, info, reason)
        finally:
            if receiver.poll() is None:
                receiver.terminate()
                try:
                    receiver.wait(timeout=3)
                except subprocess.TimeoutExpired:
                    receiver.kill()
                    receiver.wait(timeout=3)
            clean_receiver(state, info)


def clean_receiver(state, info):
    try:
        retire_orphan(info)
        atomic(state / 'receiver-removed', b'\n')
    except (OSError, RuntimeError, subprocess.SubprocessError):
        stage(state, info, 'recovery-required', outcome='receiver-cleanup-failed')
        raise


def tick(state):
    # crond owns this process. If it dies, the next tick sees the durable
    # arming/active phase and restores; it never starts another window.
    with (state / 'tick.lock').open('a') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            return
        info = data(state)
        if info['phase'] == 'prepared' and (state / 'arm').is_file():
            if (state / 'stop').exists():
                stage(state, info, 'closed', outcome='stopped-before-activation')
            else:
                supervise(state, info)
        elif info['phase'] == 'draining':
            front = identity(info['front']['cid'])
            before, _ = verify_state(state, info)
            if config_path(front).read_bytes() != before:
                stage(state, info, 'recovery-required', outcome='configuration-drift')
            elif front != info['front'] or not workers(front).intersection(info['old_workers']):
                stage(state, info, 'closed', outcome='restored')
        elif info['phase'] in ('arming', 'active'):
            try:
                recover(state, info, 'supervisor-lost')
            finally:
                clean_receiver(state, info)
        if (state / 'arm').is_file() and info['phase'] == 'closed':
            if 'deadline' in info and not (state / 'receiver-removed').exists():
                clean_receiver(state, info)
            try:
                unschedule(state)
            except (OSError, RuntimeError, subprocess.SubprocessError):
                stage(state, info, 'closed', scheduler_cleanup='unverified')
                raise
            stage(state, info, 'closed', scheduler_cleanup='verified')


def cron_entry(state):
    paths = [sys.executable, str(Path(__file__).resolve()), str(state)]
    if any(not re.fullmatch(r'/[a-zA-Z0-9_./-]+', path) for path in paths):
        raise ValueError('unsupported cron paths')
    return ('* * * * * ' + ' '.join(paths) + ' tick >/dev/null 2>&1').encode()


def unschedule(state):
    # Only remove our exact entry. Never restore an older crontab snapshot.
    line = cron_entry(state)
    original = run('crontab', '-l')
    if line not in original.splitlines():
        return
    pending = b''.join(row for row in original.splitlines(keepends=True) if row.rstrip(b'\r\n') != line)
    if run('crontab', '-l') != original:
        raise RuntimeError('scheduler drift')
    subprocess.run(['crontab', '-'], input=pending, check=True, capture_output=True, timeout=15)
    if run('crontab', '-l') != pending:
        raise RuntimeError('scheduler readback failed')


def main():
    os.umask(0o077)
    cli = argparse.ArgumentParser(description=__doc__)
    cli.add_argument('state', type=Path)
    sub = cli.add_subparsers(dest='action', required=True)
    prep = sub.add_parser('preview')
    prep.add_argument('front')
    prep.add_argument('tunnel')
    prep.add_argument('seconds', type=int)
    sub.add_parser('tick')
    sub.add_parser('status')
    sub.add_parser('cron-line')
    args = cli.parse_args()
    state = args.state.resolve()
    if args.action == 'preview':
        preview(state, args.front, args.tunnel, args.seconds)
    else:
        if state.stat().st_uid != os.geteuid() or stat.S_IMODE(state.stat().st_mode) != 0o700:
            raise RuntimeError('observation state is not private')
        if args.action == 'cron-line':
            print(cron_entry(state).decode())
            return
        if args.action == 'tick':
            def stop(*_):
                raise SystemExit(2)
            signal.signal(signal.SIGTERM, stop)
            tick(state)
    print(json.dumps(data(state), sort_keys=True))


if __name__ == '__main__':
    try:
        main()
    except (OSError, RuntimeError, ValueError, subprocess.SubprocessError):
        # Exception text can contain private paths or configuration output.
        sys.exit('catalog window operation failed; inspect sanitized status')
