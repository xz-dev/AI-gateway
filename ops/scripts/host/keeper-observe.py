#!/usr/bin/env python3
"""One bounded keeper experiment; only low free disk authorizes a one-shot revert."""
import datetime as dt
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import time
from pathlib import Path

ROOT = Path('/root/AI-gateway')
STATE = Path('/var/lib/ai-gateway-keeper-observe')
SERVICE = 'openai-api-cache-keeper'
NAME = 'ai-gateway-' + SERVICE + '-1'
FILES = ('compose.yaml', 'compose.override.yaml', '.env')
COMPOSE = ['docker', 'compose', '-p', 'ai-gateway']
OUTLIER = ('2026-10-23T04:47:00+00:00', '2026-10-23T04:59:00+00:00')
READOPT = 'rsync -a --chmod=F600,D700 root@100.94.238.35:/root/AI-gateway/{compose.yaml,compose.override.yaml,.env} /home/xz/Code/ai/AI-gateway/ops/ansible/private-config/'
GIB = 1024 ** 3
MIB = 1024 ** 2
LINE = re.compile(r'transport=(http|ws) status=(\d+) previous=(true|false) stored=(true|false).*?store_entries=(\d+) store_bytes=(\d+)')


def now():
    return dt.datetime.now(dt.timezone.utc)


def stamp(value):
    return value.isoformat(timespec='microseconds')


def run(args, timeout=20):
    return subprocess.run(args, check=True, text=True, stdout=subprocess.PIPE,
                          stderr=subprocess.DEVNULL, timeout=timeout,
                          env={'PATH': os.environ['PATH']}).stdout


def private_write(path, text):
    fd, tmp = tempfile.mkstemp(dir=path.parent, prefix='.write-')
    try:
        with os.fdopen(fd, 'w') as f:
            os.fchmod(f.fileno(), 0o600)
            f.write(text)
            f.flush()
            os.fsync(f.fileno())
        os.replace(tmp, path)
        directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if os.path.exists(tmp):
            os.unlink(tmp)


def write_json(path, value):
    private_write(path, json.dumps(value, sort_keys=True) + '\n')


def hashes(root=None):
    root = ROOT if root is None else root
    return {f: hashlib.sha256((root / f).read_bytes()).hexdigest() for f in FILES}


def identity(c):
    return {'id': c['Id'], 'image': c['Image'], 'started': c['State']['StartedAt'],
            'running': c['State']['Running']}


def inspect_target():
    return json.loads(run(['docker', 'inspect', NAME]))[0]


def project_state():
    ids = run(['docker', 'ps', '-aq', '--filter', 'label=com.docker.compose.project=ai-gateway']).split()
    data = json.loads(run(['docker', 'inspect', *ids]))
    targets = [c for c in data if c['Name'] == '/' + NAME]
    assert len(targets) == 1, 'keeper identity is ambiguous'
    target = targets[0]
    return {'target': identity(target),
            'protected': {c['Id']: identity(c) for c in data if c['Id'] != target['Id']},
            'hashes': hashes()}


def counters():
    return dict(total=0, parent_requests=0, hits=0, unknown_parent_misses=0,
                full_replays=0, stored_true=0, stored_false=0)


def summarize(lines, start, end):
    result = {'all': counters(), 'without_outlier': counters(), 'outlier': counters(),
              'store_entries': None, 'store_bytes': None}
    outlier_start, outlier_end = map(dt.datetime.fromisoformat, OUTLIER)
    for line in lines:
        match = LINE.search(line)
        if not match:
            continue
        when = dt.datetime.fromisoformat(line.split(' ', 1)[0].replace('Z', '+00:00'))
        if not start <= when < end:
            continue
        _, status, previous, stored, entries, size = match.groups()
        parent = previous == 'true'
        # shortcut: these are hit proxies; add request telemetry before measuring saved bytes.
        values = dict(total=1, parent_requests=int(parent),
                      hits=int(parent and status == '200'),
                      unknown_parent_misses=int(parent and status == '400'),
                      full_replays=int(not parent), stored_true=int(stored == 'true'),
                      stored_false=int(stored == 'false'))
        group = 'outlier' if outlier_start <= when < outlier_end else 'without_outlier'
        for key, value in values.items():
            result['all'][key] += value
            result[group][key] += value
        result['store_entries'], result['store_bytes'] = int(entries), int(size)
    return result


def activity(start, end):
    # Docker's bounded historical read goes to disk, not an unbounded Python buffer.
    with tempfile.TemporaryFile(mode='w+', dir=STATE) as f:
        subprocess.run(['docker', 'logs', '--timestamps', '--since', stamp(start),
                        '--until', stamp(end), NAME], check=True, stdout=f, stderr=f,
                       timeout=30, env={'PATH': os.environ['PATH']})
        f.seek(0)
        return summarize(f, start, end)


def memory_metrics(current, inactive_file, maximum, rss):
    limit = None if maximum == 'max' else int(maximum)
    return {'memory_current_bytes': current, 'inactive_file_bytes': inactive_file,
            'working_set_bytes': max(0, current - inactive_file), 'rss_bytes': rss,
            'memory_limit_bytes': limit,
            'memory_stop_bytes': limit * 9 // 10 if limit is not None else 345 * MIB}


def resources(c):
    assert c['State']['Running'] and not c['State']['OOMKilled'], 'keeper is not running safely'
    pid = c['State']['Pid']
    group = next(l.split('::', 1)[1] for l in Path(f'/proc/{pid}/cgroup').read_text().splitlines() if l.startswith('0::'))
    cg = (Path('/sys/fs/cgroup') / group.lstrip('/')).resolve()
    assert cg.is_relative_to('/sys/fs/cgroup'), 'invalid cgroup path'
    cpu = dict(l.split() for l in (cg / 'cpu.stat').read_text().splitlines())
    quota, period = (cg / 'cpu.max').read_text().split()
    limits = [1.0]
    if quota != 'max':
        limits.append(0.8 * int(quota) / int(period))
    nano = c['HostConfig'].get('NanoCpus', 0)
    if nano:
        limits.append(0.8 * nano / 1e9)
    cache = next(Path(m['Source']) for m in c['Mounts'] if m['Destination'] == '/var/tmp/openai-api-cache-keeper')
    assert cache == Path('/var/lib/docker/volumes/ai-gateway_openai-api-cache-keeper-cache/_data') and cache.is_dir(), 'unexpected cache volume'
    size = count = 0
    for root, _, files in os.walk(cache):
        for name in files:
            try:
                size += os.stat(Path(root) / name, follow_symlinks=False).st_size
                count += 1
            except FileNotFoundError:
                pass  # A concurrent request can remove its disposable spool.
    rss = re.search(r'^VmRSS:\s+(\d+) kB$', Path(f'/proc/{pid}/status').read_text(), re.M)
    assert rss, 'RSS unavailable'
    free = {p: os.statvfs(p).f_bavail * os.statvfs(p).f_frsize for p in ['/', '/var/lib/docker']}
    memory = dict(l.split() for l in (cg / 'memory.stat').read_text().splitlines())
    return {'cache_bytes': size, 'cache_files': count, 'free_bytes': free,
            **memory_metrics(int((cg / 'memory.current').read_text()), int(memory['inactive_file']),
                             (cg / 'memory.max').read_text().strip(), int(rss[1]) * 1024),
            'cpu_usage_usec': int(cpu['usage_usec']), 'cpu_limit_cores': min(limits),
            'cpu_cores': None, 'cpu_interval_seconds': None}


def breaches(sample):
    reasons = []
    if min(sample['free_bytes'].values()) < 4 * GIB:
        reasons.append('free_disk')
    if max(sample['working_set_bytes'], sample['rss_bytes']) >= sample['memory_stop_bytes']:
        reasons.append('memory')
    if (sample['cpu_interval_seconds'] or 0) >= 60 and sample['cpu_cores'] > sample['cpu_limit_cores']:
        reasons.append('cpu')
    return reasons


def disk_only(reasons):
    return reasons == ['free_disk']


def fallback_env(text):
    for key, old, new in [('TTL', '24h', '10m'), ('MAX_ENTRIES', '10000', '100')]:
        name = 'OPENAI_API_CACHE_KEEPER_STORE_' + key
        assert len(re.findall('^' + name + '=', text, re.M)) == 1, 'missing or duplicate keeper override'
        text, count = re.subn('^' + name + '=' + old + '$', name + '=' + new, text, flags=re.M)
        assert count == 1, 'keeper override drift'
    assert re.findall(r'^OPENAI_API_CACHE_KEEPER_STORE_MAX_BYTES=(.*)$', text, re.M) == ['0'], 'payload cap drift'
    return text


def render(root):
    return json.loads(run(COMPOSE + ['-f', str(root / FILES[0]), '-f', str(root / FILES[1]),
                                    '--env-file', str(root / '.env'), 'config', '--no-path-resolution', '--format', 'json']))


def check_fallback_scope(before, after):
    before, after = json.loads(json.dumps(before)), json.loads(json.dumps(after))
    for config, ttl, entries in [(before, '24h', '10000'), (after, '10m', '100')]:
        env = config['services'][SERVICE]['environment']
        assert env.pop('STORE_TTL') == ttl and str(env.pop('STORE_MAX_ENTRIES')) == entries
        assert str(env['STORE_MAX_BYTES']) == '0'
    assert before == after, 'fallback changes unrelated effective configuration'


def revert(arm):
    lock = ROOT / '.ai-ops-state/front-component.lock'
    lock.mkdir(mode=0o700)  # Never remove another writer's lock.
    try:
        assert project_state() == arm['expected'], 'fallback CAS failed'
        assert run(['docker', 'image', 'inspect', arm['image_ref'], '--format={{.Id}}']).strip() == arm['expected']['target']['image'], 'image drift'
        backup, candidate = STATE / 'revert-before', STATE / 'revert-candidate'
        for dest in [backup, candidate]:
            dest.mkdir(mode=0o700)
            for name in FILES:
                shutil.copyfile(ROOT / name, dest / name)
                (dest / name).chmod(0o600)
        assert hashes(backup) == arm['expected']['hashes'], 'snapshot drift'
        private_write(candidate / '.env', fallback_env((backup / '.env').read_text()))
        check_fallback_scope(render(ROOT), render(candidate))
        assert project_state() == arm['expected'], 'fallback CAS changed before write'
        private_write(ROOT / '.env', (candidate / '.env').read_text())
        assert hashes() == hashes(candidate), 'fallback upload mismatch'
        fresh = project_state()
        assert fresh['target'] == arm['expected']['target'] and fresh['protected'] == arm['expected']['protected'], 'fallback runtime changed before activation'
        run(COMPOSE + ['-f', str(ROOT / FILES[0]), '-f', str(ROOT / FILES[1]), '--env-file', str(ROOT / '.env'),
                       'up', '-d', '--force-recreate', '--no-deps', '--no-build', '--pull', 'never', SERVICE], timeout=90)
        c = None
        for _ in range(30):
            c = inspect_target()
            if c['State']['Running'] and c['State'].get('Health', {}).get('Status') == 'healthy':
                break
            time.sleep(2)
        assert c and c['State']['Running'] and c['State'].get('Health', {}).get('Status') == 'healthy', 'fallback health failed'
        env = dict(v.split('=', 1) for v in c['Config']['Env'])
        assert c['Image'] == arm['expected']['target']['image'] and c['Config']['Image'] == arm['image_ref'], 'fallback image mismatch'
        assert [env.get(k) for k in ['STORE_TTL', 'STORE_MAX_ENTRIES', 'STORE_MAX_BYTES']] == ['10m', '100', '0'], 'fallback environment mismatch'
        assert project_state()['protected'] == arm['expected']['protected'], 'fallback neighbour changed'
        return {'after': identity(c), 'production_hashes': hashes()}
    finally:
        lock.rmdir()


def event(kind, **fields):
    result = {'event': kind, 'utc': stamp(now()), 'production_hashes': hashes(), 're_adopt_command': READOPT, **fields}
    write_json(STATE / 'EVENT.json', result)
    return result


def baseline():
    assert not (STATE / 'BASELINE.json').exists(), 'baseline already exists'
    c = inspect_target()
    end = now()
    value = {'start': '2026-10-23T03:33:00+00:00', 'end': stamp(end), 'image_ref': c['Config']['Image'],
             'profile': '10m TTL / 200MiB payload cap / entries unlimited (not configured)',
             'outlier_interval': OUTLIER, 'resources_at_end': resources(c),
             'activity': activity(dt.datetime.fromisoformat('2026-10-23T03:33:00+00:00'), end),
             'client_avoided_bytes': None, 'bytes_note': 'Keeper logs contain no request/expanded byte counts.'}
    write_json(STATE / 'BASELINE.json', value)
    print(json.dumps(value))


def observe():
    arm = json.loads((STATE / 'arm.json').read_text())
    start, end = map(dt.datetime.fromisoformat, [arm['start'], arm['end']])
    assert 0 < (end - start).total_seconds() <= 86400, 'invalid observation window'
    assert not (STATE / 'EVENT.json').exists() and not (STATE / 'samples.jsonl').exists(), 'window already started'
    finish = time.monotonic() + max(0, (end - now()).total_seconds())
    previous, previous_time, cursor = None, None, start
    last_entries = last_bytes = None
    cache_note_written = False
    try:
        while True:
            tick = time.monotonic()
            c = inspect_target()
            if c['RestartCount'] or c['State']['OOMKilled']:
                event('keeper_restarted', restart_count=c['RestartCount'], oom_killed=c['State']['OOMKilled'], production_changed=False)
                return 1
            assert identity(c) == arm['expected']['target'], 'keeper changed during observation'
            assert hashes() == arm['expected']['hashes'], 'configuration changed during observation'
            cost = resources(c)
            if previous is not None:
                seconds = tick - previous_time
                cost['cpu_interval_seconds'] = seconds
                cost['cpu_cores'] = max(0, cost['cpu_usage_usec'] - previous) / 1e6 / seconds
            previous, previous_time = cost['cpu_usage_usec'], tick
            until = now()
            value = activity(cursor, until)
            if value['store_entries'] is not None:
                last_entries, last_bytes = value['store_entries'], value['store_bytes']
            value['store_entries'], value['store_bytes'] = last_entries, last_bytes
            sample = {'start': stamp(cursor), 'end': stamp(until), 'resources': cost, 'activity': value,
                      'outlier_interval': OUTLIER, 'client_avoided_bytes': None}
            cursor = until
            if cost['cache_bytes'] >= GIB and not cache_note_written:
                sample['notes'] = ['cache_1gib_crossed']
                cache_note_written = True
            with (STATE / 'samples.jsonl').open('a') as f:
                os.fchmod(f.fileno(), 0o600)
                f.write(json.dumps(sample, sort_keys=True) + '\n')
                f.flush()
                os.fsync(f.fileno())
            reasons = breaches(cost)
            if reasons:
                if disk_only(reasons):
                    with (STATE / 'REVERT-ATTEMPT.json').open('x') as f:
                        os.fchmod(f.fileno(), 0o600)
                        f.write(json.dumps({'utc': stamp(now()), 'breaches': reasons}) + '\n')
                        f.flush()
                        os.fsync(f.fileno())
                    try:
                        event('disk_reverted', breaches=reasons, **revert(arm))
                    except Exception as exc:
                        event('disk_revert_failed', breaches=reasons, failure_type=type(exc).__name__)
                        return 1
                else:
                    event('resource_stop', breaches=reasons, production_changed=False)
                return 0
            if time.monotonic() >= finish:
                event('finished', production_changed=False)
                return 0
            time.sleep(min(60, max(0, finish - time.monotonic())))
    except Exception as exc:
        event('monitor_error', failure_type=type(exc).__name__, production_changed=None)
        return 1


if __name__ == '__main__':
    os.umask(0o077)
    STATE.mkdir(mode=0o700, exist_ok=True)
    assert STATE.stat().st_mode & 0o777 == 0o700, 'observation state must be private'
    if sys.argv[1:] == ['baseline']:
        baseline()
    elif sys.argv[1:] == ['observe']:
        sys.exit(observe())
    else:
        sys.exit('usage: keeper-observe.py baseline|observe')
