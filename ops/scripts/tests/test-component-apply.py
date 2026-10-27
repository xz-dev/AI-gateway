#!/usr/bin/env python3
"""Lane D local-inventory/fake-Docker harness, expanded for generic lifecycles.

Promoted from its task-local offline-operations.py: same real Ansible, JSON
Compose fixtures, local inventory and mutation log; never connects to production.
Needs ansible-playbook. Artifacts stay under --evidence; no privileged Docker or
network namespace changes. Existing HTTP probe code runs in this same namespace.
"""
import argparse
import copy
import http.server
import json
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import tempfile
import threading
import time

ROOT = Path(__file__).resolve().parents[3]
FAKE = r'''#!/usr/bin/python3
import json, os, pathlib, sys, time
path = pathlib.Path(__file__).with_name('state.json')
s = json.loads(path.read_text()); a = sys.argv[1:]
def save():
 tmp=path.with_suffix('.tmp');tmp.write_text(json.dumps(s));os.replace(tmp,path)
def image(ref):
 if ref not in s['images']:sys.exit(1)
 print(json.dumps({'Id':s['images'][ref], 'RepoDigests':['test/component@'+('sha256:'+'f'*64 if s.get('wrong_digest') else ref.rsplit('@',1)[-1])]}))
if a[0]=='version':print('27.5.1')
elif a[0]=='ps':print(' '.join(c['Id'] for c in s['containers']))
elif a[0]=='inspect':print(json.dumps([c for c in s['containers'] if c['Id'] in a[1:]]))
elif a[:2]==['image','inspect']:image(a[2])
elif a[:2]==['manifest','inspect']:print(json.dumps({'schemaVersion':2,'config':{'digest':'sha256:'+'c'*64}}))
elif a[0]=='pull':
 s['mutations'].append(['pull',a[1]]);save()
 if s.get('hold_pull'):
  path.with_name('pull-entered').touch()
  end=time.monotonic()+90
  while not path.with_name('release-pull').exists() and time.monotonic()<end:time.sleep(.1)
  s=json.loads(path.read_text())
 if not s.get('skip_pull'):s['images'][a[1]]='sha256:'+'c'*64
 if s.get('drift_on_pull'):pathlib.Path(s['live'],'.env').write_text('another writer\n')
 save()
elif a[0]=='compose':
 conf=json.loads(pathlib.Path(a[a.index('-f')+1]).read_text())
 if 'config' in a:
  if '--quiet' not in a:print(json.dumps(conf))
 elif 'pull' in a:pass  # Compose's pull_policy: never: success without a pull.
 elif 'up' in a:
  s.setdefault('attempts',[]).append('up');save()
  assert a[-1]==s['service']
  assert all(flag in a for flag in ['--force-recreate','--no-deps','--no-build','--pull'])
  assert a[a.index('--pull')+1]=='never'
  ref=conf['services'][s['service']]['image']
  if ref not in s['images']:sys.exit(42)
  s['mutations'].append(['up',s['service']])
  if s.get('fail_up'):save();sys.exit(43)
  c=s['containers'][0];c['Id']='target-'+str(len(s['mutations']));c['Image']=s['images'][ref];c['Config']['Image']=ref
  c['Config']['Env']=[k+'='+str(v) for k,v in conf['services'][s['service']].get('environment',{}).items()]
  c['State'].update(Running=True,Pid=s['pid'],StartedAt='2026-10-23T08:01:00Z')
  if s.get('drift_neighbour'):s['containers'][-1]['State']['StartedAt']='changed'
  save()
 else:sys.exit(44)
else:sys.exit(45)
'''


class Probe(http.server.BaseHTTPRequestHandler):
    calls = 0

    def do_GET(self):
        type(self).calls += 1
        self.send_response(404)
        self.end_headers()

    def log_message(self, *_):
        pass


class Fixture:
    def __init__(self, evidence, label, service='models-enricher', native=True):
        self.tmp = tempfile.TemporaryDirectory(prefix='component-apply-', dir='/var/tmp')
        self.home = Path(self.tmp.name)
        self.label, self.evidence, self.service = label, evidence, service
        self.repo, self.live, self.source, self.bin = [self.home / p for p in ('repo', 'live', 'source', 'bin')]
        shutil.copytree(ROOT / 'ops/ansible', self.repo / 'ops/ansible', ignore=shutil.ignore_patterns('private-config', '*.log', '__pycache__'))
        shutil.copytree(ROOT / 'ops/scripts/host', self.repo / 'ops/scripts/host', ignore=shutil.ignore_patterns('__pycache__'))
        for d in (self.live, self.source, self.bin):
            d.mkdir()
        self.exe = self.bin / 'docker'
        self.exe.write_text(FAKE)
        self.exe.chmod(0o700)
        self.old = 'test/component:old@sha256:' + 'b' * 64
        self.new = 'test/component:new@sha256:' + 'd' * 64
        front = service in ('responses-ws-sse-bridge', 'openai-api-cache-keeper')
        if front:
            self.old = 'ghcr.io/xz-dev/' + service + ':' + 'a'*40 + '@sha256:' + 'b'*64
            self.new = self.old.replace('b'*64, 'd'*64)
        if service == 'cloudflared':
            self.old = self.old.replace('test/component', 'rollback-20261015/cloudflared')
            self.new = self.new.replace('test/component', 'rollback-20261015/cloudflared')
        env = {'MODE': 'old'}
        if service == 'responses-ws-sse-bridge':
            env = {'PORT':'8086','LISTEN_ADDR':'127.0.0.1:8086','UPSTREAM_URL':'http://sub2api-ai-sse-relay:8080'}
        elif service == 'openai-api-cache-keeper':
            env = {'LISTEN_ADDR':'127.0.0.1:8085','UPSTREAM_URL':'http://127.0.0.1:8086','IGNORE_CLIENT_STORE_FALSE':'true'}
        config = {'services': {service: {'image': self.old, 'environment': env, 'pull_policy': 'never'}, 'protected': {'image': 'untouched'}}}
        if front:
            config['services'][service]['network_mode'] = 'service:ai-sse-keepalive-proxy-netns'
        for d in (self.live, self.source):
            (d / 'compose.yaml').write_text(json.dumps(config))
            (d / 'compose.override.yaml').write_text('{}\n')
            (d / '.env').write_text('SECRET=secret-fixture\n')
        candidate = copy.deepcopy(config)
        candidate['services'][service]['image'] = self.new
        candidate['services'][service]['environment']['MODE'] = 'new'
        (self.source / 'compose.yaml').write_text(json.dumps(candidate))
        def container(name, ident, image, reference):
            return {'Id':ident,'Image':image,'Name':'/ai-gateway-'+name+'-1',
                    'Config':{'Labels':{'com.docker.compose.project':'ai-gateway','com.docker.compose.service':name},'Image':reference,
                              'Env':[k+'='+v for k,v in env.items()] if name==service else []},
                    'State':{'Running':True,'Pid':os.getpid(),'Paused':False,'Restarting':False,'StartedAt':'2026-10-23T08:00:00Z'},
                    'HostConfig':{'NetworkMode':'container:owner' if front and name==service else 'bridge'}}
        target = container(service, 'target-old', 'sha256:'+'a'*64, self.old)
        if native:
            target['State']['Health'] = {'Status':'healthy'}
        containers = [target]
        if front:
            containers.append(container('ai-sse-keepalive-proxy-netns','owner','owner-image','owner-ref'))
        containers.append(container('protected','protected','protected-image','protected-ref'))
        self.statefile = self.bin / 'state.json'
        self.save({'service':service,'pid':os.getpid(),'live':str(self.live),'containers':containers,'images':{self.old:target['Image']},'mutations':[]})
        state = self.live / '.ai-ops-state'
        state.mkdir(mode=0o700)
        for file in ('compose.yaml','compose.override.yaml','.env'):
            shutil.copyfile(self.live/file, state/(file+'.baseline'))
        self.cpa = self.live / 'data/cpa/conf/config.yaml'
        self.cpa.parent.mkdir(parents=True)
        shutil.copyfile(ROOT / 'services/cpa/config/config.example.yaml', self.cpa)
        self.initial_baselines = self.baselines()
        inventory = self.home / 'inventory'
        inventory.write_text('[gateway]\nfixture ansible_connection=local ansible_python_interpreter=/usr/bin/python3\n')
        self.inventory = inventory
        self.env = dict(os.environ, PATH=str(self.bin)+':'+os.environ['PATH'], ANSIBLE_NOCOLOR='1', ANSIBLE_LOCAL_TEMP=str(self.home/'ansible-tmp'))
        self.variables = {'service':service,'ai_ops_mode':'plan','ai_ops_runtime':'docker','ai_ops_config_dir':str(self.source),
                          'ai_ops_deploy_root':str(self.live),'ai_ops_compose_cmd':'docker compose','ai_ops_local_compose_cmd':str(self.exe)+' compose',
                          'ai_ops_front_verify_retries':1,'ai_ops_front_verify_delay_seconds':0,'ai_ops_min_mem_mb':0,'ai_ops_min_disk_mb':0}

    def save(self, state):
        self.statefile.write_text(json.dumps(state))

    def state(self):
        return json.loads(self.statefile.read_text())

    def baselines(self):
        return {p.name:p.read_bytes() for p in (self.live/'.ai-ops-state').glob('*.baseline')}

    def argv(self, op='deploy', apply=False, digest=None, **extra):
        variables = dict(self.variables, ai_ops_mode='apply' if apply else 'plan', **extra)
        if digest:
            variables['ai_ops_front_approved_digest'] = digest
        return ['ansible-playbook','-i',str(self.inventory),'ops.yml','--tags',op,'-e',json.dumps(variables)]

    def run(self, label, op='deploy', apply=False, digest=None, success=True, **extra):
        result = subprocess.run(self.argv(op, apply, digest, **extra), cwd=self.repo/'ops/ansible', env=self.env, capture_output=True, text=True, timeout=360)
        text = result.stdout + result.stderr
        (self.evidence / (self.label+'-'+label+'.log')).write_text(text)
        assert 'secret-fixture' not in text, 'Ansible leaked a secret'
        assert (result.returncode==0)==success, label+' unexpected rc='+str(result.returncode)+'\n'+text[-3500:]
        match = re.search(r'"approval_digest": "([0-9a-f]{64})"', text)
        return match[1] if match else None

    def close(self):
        if hasattr(self, 'controller') and self.controller.poll() is None:
            os.killpg(self.controller.pid, signal.SIGKILL)
            self.controller.wait(timeout=15)
        if (self.bin/'pull-entered').exists():
            (self.bin/'release-pull').touch()
            end = time.monotonic() + 5
            while self.new not in self.state()['images'] and time.monotonic() < end:
                time.sleep(.1)
        self.tmp.cleanup()

    def mutate(self, name):
        tasks = self.repo/'ops/ansible/roles/ai_ops/tasks'
        path = tasks/'front-component.yml'
        text = path.read_text()
        if name == 'no-image-proof':
            old = "    - name: Verify exact candidate digest locally (rollback and preload never pull)\n      ansible.builtin.include_tasks: component-image.yml\n      vars:\n        _front_image_mode: local\n"
            assert old in text
            text = text.replace(old, '')
        elif name == 'early-baseline':
            marker = '    - name: Record stage before selected component activation'
            text = text.replace(marker, "    - name: MUTATION advance baseline before recreate\n      ansible.builtin.copy:\n        src: '{{ ai_ops_deploy_root }}/{{ item }}'\n        dest: '{{ ai_ops_deploy_root }}/.ai-ops-state/{{ item }}.baseline'\n        remote_src: true\n      loop: [compose.yaml, compose.override.yaml, .env]\n\n" + marker)
        elif name == 'no-group-cas':
            marker = '    - name: Refuse changed live state after image preparation'
            text = text.replace(marker, "    - name: MUTATION bless changed live state\n      ansible.builtin.set_fact:\n        _front_before_hashes: '{{ _front_live_hashes }}'\n\n" + marker)
        elif name == 'no-neighbour-check':
            text = '\n'.join(line for line in text.splitlines() if '_front_runtime.identity.protected == _front_before_runtime.identity.protected' not in line) + '\n'
        elif name == 'no-progress':
            path = self.repo/'ops/scripts/host/component-lock.py'
            text = path.read_text().replace('result = progress(root, request)', "result = {'skipped': True}")
        elif name == 'no-native-probe':
            path = tasks/'front-runtime.yml'
            text = path.read_text()
            start = text.index("                ok = state.get('Running')")
            end = text.index("                return 'recovery_probe'", start)
            text = text[:start] + '                ok = True  # MUTATION unverified readiness\n' + text[end:]
        else:
            raise AssertionError('unknown mutation')
        path.write_text(text)


def main():
    os.umask(0o077)
    parser=argparse.ArgumentParser()
    parser.add_argument('--evidence', type=Path, required=True)
    parser.add_argument('--mutation', choices=['no-image-proof','early-baseline','no-progress','no-native-probe','no-group-cas','no-neighbour-check'])
    parser.add_argument('--case', choices=['skip','digest','rollback','kill','native','generic','cas','neighbour','front','preload','worker','cpa-config','keeper-config','rollback-stale','rollback-identity'], action='append')
    args=parser.parse_args()
    args.evidence.mkdir(parents=True,exist_ok=True,mode=0o700)
    server=http.server.ThreadingHTTPServer(('127.0.0.1',0),Probe)
    threading.Thread(target=server.serve_forever,daemon=True).start()
    for case in args.case or ['skip','digest','rollback','kill','native','generic','cas','neighbour','front','preload','worker','cpa-config','keeper-config','rollback-stale','rollback-identity']:
        f=Fixture(args.evidence,case,'aisix-status' if case=='native' else 'responses-ws-sse-bridge' if case=='front' else 'cloudflared' if case=='preload' else 'cpa-model-sync' if case=='worker' else 'cli-proxy-api' if case=='cpa-config' else 'openai-api-cache-keeper' if case=='keeper-config' else 'models-enricher',native=case not in ('native','worker'))
        try:
            if case in ('worker','cpa-config','keeper-config'):
                shutil.copyfile(f.live/'compose.yaml',f.source/'compose.yaml')
            if case=='worker':
                scripts=f.live/'ops/scripts/host';scripts.mkdir(parents=True)
                progress=scripts/'gateway-recovery.py'
                progress.write_text('OBSERVE_HTTP={}\ndef worker_health(c, now):\n return {"ok":True,"ready":False,"dependencyFailures":2}\n')
                f.run('plan')
                text=(args.evidence/'worker-plan.log').read_text()
                assert '"activation_required": false' in text and '"ready": true' in text, 'upstream failure forced worker activation'
                assert '"upstream_ready": false' in text, 'upstream readiness is not reported separately'
                progress.write_text('OBSERVE_HTTP={}\ndef worker_health(c, now):\n return {"ok":False,"ready":False}\n')
                f.run('stalled-plan')
                assert '"activation_required": true' in (args.evidence/'worker-stalled-plan.log').read_text(), 'stalled worker was treated as healthy'
                assert not f.state()['mutations']
                print('PASS',case,flush=True)
                continue
            if case in ('cpa-config','keeper-config'):
                seed=f.source/'data/cpa/conf/config.yaml';seed.parent.mkdir(parents=True)
                seed.write_text(f.cpa.read_text())
                baseline=f.live/'.ai-ops-state/data__cpa__conf__config.yaml.baseline'
                baseline.write_text('historical config\n')
                live={'config-version':8,'server':{'host':'','port':8317},'management':{'secret-key':'secret-fixture'},'access':{'api-keys':['secret-fixture']},'api-keys':{'openai-compatibility':[{'models':[{'name':'new-model'}]}]},'routing':{'strategy':'fill-first','session-affinity':True,'session-affinity-ttl':'1h'}}
                f.cpa.write_text(json.dumps(live));original=f.cpa.read_bytes()
                digest=f.run('drift-plan')
                assert '"activation_required": false' in (args.evidence/(case+'-drift-plan.log')).read_text()
                seed.unlink()
                f.run('noop-apply',apply=True,digest=digest)
                assert f.cpa.read_bytes()==original and baseline.read_text()=='historical config\n'
                assert not f.state()['mutations']
                # Schema changes remain worker-owned; only CPA profiles need parseable YAML.
                f.cpa.write_text('{"config-version":99,"new-layout":{}}')
                f.run('new-schema')
                for label,invalid in [('invalid-yaml','broken: ['),('not-mapping','[]')]:
                    f.cpa.write_text(invalid)
                    f.run(label,success=case=='keeper-config')
                f.cpa.unlink()
                f.run('missing-live',success=case=='keeper-config')
                assert not f.cpa.exists(), 'preflight bootstrapped without explicit initialization'
                print('PASS',case,flush=True)
                continue
            if case=='preload':
                state=f.state();state['images'][f.new]='sha256:'+'c'*64;f.save(state)
            if args.mutation:
                f.mutate(args.mutation)
            if case=='native':
                scripts=f.live/'ops/scripts/host';scripts.mkdir(parents=True)
                shutil.copyfile(ROOT/'ops/scripts/host/gateway_health.py',scripts/'gateway_health.py')
                (scripts/'gateway-recovery.py').write_text('import runpy, os\nglobals().update(runpy.run_path('+repr(str(ROOT/'ops/scripts/host/gateway-recovery.py'))+'))\nos.setns=lambda *args:None\nOBSERVE_HTTP["aisix-status"]=("127.0.0.1",'+str(server.server_port)+',"/",404)\n')
            digest=f.run('plan');assert digest and not f.state()['mutations']
            state=f.state()
            if case in ('rollback-stale','rollback-identity'):
                f.run('apply',apply=True,digest=digest)
                state=f.state()
                if case=='rollback-stale':
                    # A newer attempt fails before snapshot; last.json still points backwards.
                    candidate=json.loads((f.source/'compose.yaml').read_text())
                    candidate['services'][f.service]['image']=f.new.replace('d'*64,'e'*64)
                    (f.source/'compose.yaml').write_text(json.dumps(candidate))
                    state['skip_pull']=True;f.save(state)
                    fresh=f.run('new-plan')
                    f.run('failed-before-snapshot',apply=True,digest=fresh,success=False)
                else:
                    # Same image, different container: image-only checks cannot prove provenance.
                    state['containers'][0]['Id']='unrelated-replacement'
                    state['containers'][0]['State']['StartedAt']='2026-10-25T00:00:00Z'
                    f.save(state)
                mutations=f.state()['mutations'][:]
                baselines=f.baselines()
                f.run('refused',op='rollback',success=False)
                text=(args.evidence/(case+'-refused.log')).read_text()
                assert 'Rollback refused:' in text and 'live=' in text and 'snapshot=' in text
                assert f.state()['mutations']==mutations and f.baselines()==baselines
                assert not (f.live/'.ai-ops-state/front-component.lock').exists()
            elif case in ('skip','digest','rollback','cas','neighbour'):
                state[{'skip':'skip_pull','digest':'wrong_digest','rollback':'fail_up','cas':'drift_on_pull','neighbour':'drift_neighbour'}[case]]=True;f.save(state)
                f.run('apply',apply=True,digest=digest,success=False)
                assert f.baselines()==f.initial_baselines, 'failed apply advanced a baseline'
                assert not (f.live/'.ai-ops-state/front-component.lock').exists()
                if case in ('skip','digest','cas'):
                    assert 'up' not in f.state().get('attempts', []), 'unsafe activation was attempted before proof/CAS'
                if case=='rollback':
                    state=f.state();state['fail_up']=False;f.save(state)
                    rollback=f.run('rollback-plan',op='rollback')
                    f.run('rollback-apply',op='rollback',apply=True,digest=rollback)
                    assert f.baselines()==f.initial_baselines
                    assert f.state()['containers'][0]['Config']['Image']==f.old
            elif case=='kill':
                state['hold_pull']=True;f.save(state)
                log=open(args.evidence/'kill-controller.log','w')
                controller=subprocess.Popen(f.argv(apply=True,digest=digest),cwd=f.repo/'ops/ansible',env=f.env,stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
                f.controller = controller
                deadline=time.monotonic()+120
                while not (f.bin/'pull-entered').exists() and time.monotonic()<deadline:
                    assert controller.poll() is None, 'controller stopped before kill point'
                    time.sleep(.2)
                assert (f.bin/'pull-entered').exists(), 'did not reach kill point'
                owner=json.loads((f.live/'.ai-ops-state/front-component.lock/owner.json').read_text())
                assert owner['controller_pid']==controller.pid, 'owner PID is a task worker, not the controller'
                os.killpg(controller.pid,signal.SIGKILL);controller.wait(timeout=15);log.close()
                receipt=f.live/'.ai-ops-state'/('transaction-'+owner['transaction']+'.json')
                assert json.loads(receipt.read_text())['stage']=='preparing-image'
                assert f.baselines()==f.initial_baselines
                f.run('recent-refused',op='recover-stale-lock',apply=True,success=False,ai_ops_lock_transaction=owner['transaction'])
                (f.bin/'release-pull').touch()
                # The isolated async pull is allowed to finish, not re-created or retried.
                deadline=time.monotonic()+15
                while f.new not in f.state()['images'] and time.monotonic()<deadline:time.sleep(.1)
                assert f.new in f.state()['images']
                owner['started']-=901
                (f.live/'.ai-ops-state/front-component.lock/owner.json').write_text(json.dumps(owner))
                value=json.loads(receipt.read_text());value['owner']=owner;receipt.write_text(json.dumps(value))
                f.run('mismatch-refused',op='recover-stale-lock',apply=True,success=False,ai_ops_lock_transaction='f'*32)
                f.run('recover',op='recover-stale-lock',apply=True,ai_ops_lock_transaction=owner['transaction'])
                assert not (f.live/'.ai-ops-state/front-component.lock').exists()
                assert json.loads(receipt.read_text())['outcome']=='recovery-required'
            else:
                f.run('apply',apply=True,digest=digest)
                current=f.state()
                assert current['containers'][0]['Config']['Image']==f.new
                assert current['containers'][-1]==state['containers'][-1], 'protected container changed'
                for name,value in f.baselines().items():assert value==(f.live/name[:-9]).read_bytes()
                if case=='native':assert Probe.calls>0, 'existing HTTP probe did not run'
                if case=='generic':
                    unchanged=f.state()
                    again=f.run('noop-plan')
                    f.run('noop-apply',apply=True,digest=again)
                    assert f.state()==unchanged, 'matching desired state was unnecessarily recreated'
                rollback=f.run('rollback-plan',op='rollback')
                f.run('rollback',op='rollback',apply=True,digest=rollback)
                assert f.baselines()==f.initial_baselines
                assert len([m for m in f.state()['mutations'] if m[0]=='pull'])==(0 if case=='preload' else 1), 'preload or rollback pulled an image'
            print(case+': PASS',flush=True)
        finally:
            f.close()
    server.shutdown()


if __name__=='__main__':main()
