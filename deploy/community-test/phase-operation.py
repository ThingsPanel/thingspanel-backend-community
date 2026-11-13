"""Unified c3fd acceptance candidate for root review; generated locally only. Never invoked automatically by candidate CI.

Only encore-api image may change. Exact fresh-baseline checks, private snapshots,
and existing deployment.lock protect apply/rollback; no migration or DB access.
Python 3.6 compatible. stdout never includes compose/env or subprocess errors.
"""
import argparse,copy,datetime,fcntl,gzip,hashlib,json,os,pathlib,re,shutil,subprocess,tarfile,time,urllib.request,urllib.error

ROOT=pathlib.Path('/opt/tp-community-main-20261028')
COMPOSE=ROOT/'compose.encore.private.json'
LOCK=pathlib.Path('/opt/tp-community-actions/deployment.lock')
PROJECT='tp-community-main-47'
SERVICE='encore-api'
NAME='tp-community-main-47-encore-api-1'
BASE='ece5c7ef870acd54b712123831d615ed6e3858c2'
ISOLATION='ce9097abf20b13f7c7f072f64d435018801b926a'
OWNER='47405bfd51eba9f436231744de9a123d4278152d'
SERVICES={'authn','thingmodel','devicemetadata','fileassets','connectorcatalog','automation','notification','marketinstall','platformconfig'}

CANDIDATE='c3fd6ec0e89c38a305c1adbd92e7de7015fbc424'
PREVIOUS_TOOL='db078b3e47af72338c856d38dd676b55e20660fa'
PREVIOUS_IMAGE='sha256:d284d82d112a0f12c1038aa4dfb7fd2427b38a4c0a49bb6c51078242733e9154'
CANDIDATE_IMAGE='sha256:65331c77f28c13a98ecca2385db0f4508da8e62f4e88090fae7fe9cfa1f86a63'
CHECKSUMS='8ca019abe1e90b9c202c3ddca695ac2239395e72926a71b5b71bc1cab179cf22'
COMPOSITION='5aab47c93d824fd7c3a70a744c2c34ca60ffca795c84c2ebd0c26407a93c3475'
DIRECTORY_GIN_VERSION='gin-cas-command-efb3f95f45310206fa484efb59402eb4bf7aa62f'
DIRECTORY_GIN_BINARY='564d846dcfa2138c15b6536a40abc54d8e94956408dde60b9ea8bbd9dfc748a6'
PREVIOUS_STAGE=pathlib.Path('/opt/tp-community-actions/q09-encore-db078-ci43350641612')
DIRECTORY_CASES={'body tenant selector forbidden', '200 raw IDs allowed and deduped', 'wrong service token', '201 IDs rejected before dedupe', 'A unknown omitted', 'A old list pagination compatibility', 'B mixed own foreign unknown dedupe', 'empty IDs', 'wrong deployment', 'B foreign only omitted', 'A own two fixtures', 'anonymous', 'B old list pagination compatibility', 'A mixed own foreign unknown dedupe', 'B own two fixtures', 'A foreign only omitted', 'B unknown omitted', 'missing tenant'}
PRESERVED_NAMES=set(['tp-agent-prepared-042b14d0ac36d9c9ee07192454bce740752ff625', 'tp-agent-prepared-e7963c7eb6adc58d5936946a78ad5c6d7a903bce', 'tp-community-main-47-broker-mosquitto-1', 'tp-community-main-47-broker-owner-1', 'tp-community-main-47-notification-core-1', 'tp-main47-ha-agent', 'tp-main47-ha-bridge', 'tp-main47-ha-demo', 'tp-main47-http-ingress', 'tp-notification-app-unipush-1'])
PRESERVED_PATHS=set(['/opt/tp-community-actions/mqtt-publisher-1af192946db407e05909827ce627f1f82755640e-stage-20261113-qa-fixes/compose.next.private.json', '/opt/tp-community-main-20261028/active-broker-plugin-token.private', '/opt/tp-community-main-20261028/active-broker-services.private.json', '/opt/tp-community-main-20261028/active-broker-tokens.private.json', '/opt/tp-community-main-20261028/ha-runtime/broker_scope_roles.py', '/opt/tp-community-main-20261028/ha-runtime/scope.py', '/opt/tp-community-main-20261028/http-runtime/broker_scope_roles.py', '/opt/tp-community-main-20261028/http-runtime/scope.py', '/opt/tp-community-main-20261028/mosquitto.active.conf', '/opt/tp-community-main-20261028/mqtt-server.crt', '/opt/tp-community-main-20261028/mqtt-server.key', '/opt/tp-community-main-20261028/runtime.encore.private.json'])
DEADLINE=None

def budget(limit):
    if DEADLINE is None:return limit
    remaining=DEADLINE-time.monotonic()
    if remaining<=0:raise RuntimeError('operation-deadline-exhausted')
    return min(limit,remaining)

def sha(path):
    h=hashlib.sha256()
    with pathlib.Path(path).open('rb') as f:
        for block in iter(lambda:f.read(1048576),b''):h.update(block)
    return h.hexdigest()

def fingerprint(value):
    return hashlib.sha256(json.dumps(value,sort_keys=True,separators=(',',':')).encode()).hexdigest()

def run(args,timeout=30):
    p=subprocess.run([str(a) for a in args],stdout=subprocess.PIPE,stderr=subprocess.PIPE,
                     universal_newlines=True,timeout=budget(timeout))
    budget(timeout)
    if p.returncode:raise RuntimeError('subprocess-failed')
    return p.stdout.strip()

def inspect(name):return json.loads(run(['docker','inspect',name]))[0]

def observe_target():
    """Absence is valid only when both the exact service and name are absent."""
    ids=run(['docker','ps','-aq','--no-trunc','--filter','label=com.docker.compose.project='+PROJECT,
             '--filter','label=com.docker.compose.service='+SERVICE]).splitlines()
    if not ids:
        names=run(['docker','ps','-aq','--no-trunc','--filter','name=^/'+NAME+'$']).splitlines()
        if names:raise RuntimeError('target-name-owned-by-another-service')
        return None
    if len(ids)!=1:raise RuntimeError('ambiguous-target-service')
    c=inspect(ids[0]);labels=c['Config'].get('Labels') or {}
    if c['Name'].lstrip('/')!=NAME or labels.get('com.docker.compose.project')!=PROJECT or labels.get('com.docker.compose.service')!=SERVICE or labels.get('com.docker.compose.project.config_files')!=str(COMPOSE):
        raise RuntimeError('target-service-identity-drift')
    return c

def private_write(path,data):
    fd=os.open(str(path),os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
    with os.fdopen(fd,'wb') as f:f.write(data);f.flush();os.fsync(f.fileno())

def write_record(path,value):
    private_write(path,json.dumps(value,sort_keys=True,indent=2).encode())

def mounted(c):
    return [{'type':m['Type'],'source':m['Source'],'destination':m['Destination'],'rw':m['RW']} for m in c['Mounts']]

def contract(c):
    cfg=c['Config']
    return fingerprint({'Env':cfg.get('Env'),'Cmd':cfg.get('Cmd'),'Entrypoint':cfg.get('Entrypoint'),
                        'HostConfig':c['HostConfig'],'Mounts':mounted(c)})

def env_map(values):
    out={}
    for value in values or []:
        key,sep,item=value.partition('=')
        if not sep or key in out:raise RuntimeError('ambiguous-environment-definition')
        out[key]=item
    return out

def runtime_config(c):
    """Only irrelevant ordering is normalized; no settings are dropped."""
    cfg=c['Config'];host=copy.deepcopy(c['HostConfig'])
    if isinstance(host.get('Binds'),list):host['Binds']=sorted(host['Binds'])
    return {'Env':env_map(cfg.get('Env')),'Cmd':cfg.get('Cmd'),'Entrypoint':cfg.get('Entrypoint'),
            'HostConfig':host,'Mounts':sorted(mounted(c),key=lambda m:(m['destination'],m['source']))}

def same_image_defaults(old,new):
    a,b=old['Config'],new['Config']
    # Fail before replacing a container if the new image brings unreviewed
    # defaults. Compose may override these, but that requires separate review.
    if env_map(a.get('Env'))!=env_map(b.get('Env')) or any(a.get(k)!=b.get(k) for k in ('Cmd','Entrypoint','User','WorkingDir','ExposedPorts','Volumes','Healthcheck','StopSignal')):
        raise RuntimeError('candidate-image-defaults-require-review')

def unchanged_file(record):
    p=pathlib.Path(record['path'])
    if record.get('exists') is False:
        if p.exists() or p.is_symlink():raise RuntimeError('unexpected-preserved-file')
    elif p.is_symlink() or not p.is_file() or sha(p)!=record['sha256']:
        raise RuntimeError('preserved-file-drift')

def process_contract(p):
 e=p['pm2_env'];env=dict(e.get('env') or {})
 env.update({k:v for k,v in e.items() if k.upper()==k and isinstance(v,(str,int,float))})
 fixed={k:e.get(k) for k in ('pm_exec_path','pm_cwd','exec_interpreter','exec_mode','args','node_args','autorestart','restart_delay','max_memory_restart','min_uptime','max_restarts','kill_timeout','wait_ready','listen_timeout','merge_logs','log_date_format','uid','gid')}
 return hashlib.sha256(json.dumps({'name':p['name'],'pm_id':p['pm_id'],'env':env,'fixed':fixed},sort_keys=True,separators=(',',':')).encode()).hexdigest()


def preserve(spec):
    for expected in spec['preserved_containers']:
        c=inspect(expected['name'])
        if c['Id']!=expected['id'] or c['Image']!=expected['image_id'] or not c['State']['Running']:
            raise RuntimeError('non-target-container-drift')
        if 'restart_count' in expected and c['RestartCount']!=expected['restart_count']:
            raise RuntimeError('non-target-container-restarted')
    for record in spec['preserved_files']:unchanged_file(record)
    pm=json.loads(run(['/root/.local/share/pnpm/pm2','jlist']))
    gin=[p for p in pm if p.get('name')==spec['gin']['name']]
    if len(gin)!=1 or gin[0]['pid']!=spec['gin']['pid'] or gin[0].get('pm_id')!=spec['gin']['pm_id'] or gin[0]['pm2_env'].get('status')!='online':
        raise RuntimeError('Gin-identity-drift')
    unchanged_file(spec['gin']['binary'])
    unchanged_file(spec['gin']['config'])
    if process_contract(gin[0])!='4adb685623ed8f8fcd8ed38be4f6a95817a8dc7aa2aa2d040122f0ced964ee5a' or os.readlink('/proc/'+str(gin[0]['pid'])+'/exe')!=spec['gin']['binary']['path']:raise RuntimeError('Gin-process-contract-drift')
    directory_gate(spec)

def directory_gate(spec):
    receipt=spec.get('directory_receipt') or {}
    unchanged_file(receipt)
    value=json.loads(pathlib.Path(receipt['path']).read_text())
    if value.get('status')!='PASS' or value.get('version')!=DIRECTORY_GIN_VERSION or value.get('binary_sha256')!=DIRECTORY_GIN_BINARY or value.get('pid')!=spec['gin']['pid']:raise RuntimeError('directory-acceptance-required')
    if value.get('receipt_schema')!=1 or value.get('candidate')!=DIRECTORY_GIN_VERSION[len('gin-cas-command-'):] or value.get('pm_id')!=25 or value.get('process_contract_sha256')!='4adb685623ed8f8fcd8ed38be4f6a95817a8dc7aa2aa2d040122f0ced964ee5a' or value.get('required_case_count')!=18 or len(value.get('checks',[]))!=18 or any(x.get('status')!='PASS' for x in value['checks']) or {x.get('name') for x in value['checks']}!=DIRECTORY_CASES or value.get('read_only') is not True or value.get('fixture_writes')!=0 or value.get('device_commands')!=0:raise RuntimeError('directory-acceptance-incomplete')
    if spec['gin']['binary']['sha256']!=DIRECTORY_GIN_BINARY:raise RuntimeError('directory-Gin-binary-required')
    with urllib.request.urlopen('http://127.0.0.1:9999/api/v1/sys_version',timeout=budget(3)) as response:
        if json.load(response).get('data',{}).get('version')!=DIRECTORY_GIN_VERSION:raise RuntimeError('directory-Gin-version-drift')


def baseline(spec):
    if {x['name'] for x in spec.get('preserved_containers',[])}!=PRESERVED_NAMES or len(spec['preserved_containers'])!=len(PRESERVED_NAMES) or {x['path'] for x in spec.get('preserved_files',[])}!=PRESERVED_PATHS or len(spec['preserved_files'])!=len(PRESERVED_PATHS):raise RuntimeError('preservation-inventory-incomplete')
    if spec.get('ready_for_stage') is not True or spec['encore']['image_id']!=PREVIOUS_IMAGE:raise RuntimeError('fresh-db078-baseline-required')
    old=json.loads((PREVIOUS_STAGE/'state.private.json').read_text())
    applied=json.loads((PREVIOUS_STAGE/'applied.private.json').read_text())
    if old['tooling_commit']!=PREVIOUS_TOOL or old['image_id']!=PREVIOUS_IMAGE or applied['container_id']!=spec['encore']['id'] or applied['image_id']!=PREVIOUS_IMAGE:raise RuntimeError('previous-reviewed-release-identity-drift')
    preserve(spec);unchanged_file(spec['compose'])
    c=inspect(NAME);e=spec['encore']
    if c['Id']!=e['id'] or c['Image']!=e['image_id'] or not c['State']['Running'] or contract(c)!=e['runtime_contract_sha256']:
        raise RuntimeError('Encore-baseline-drift')
    labels=c['Config'].get('Labels') or {}
    if labels.get('com.docker.compose.project')!=PROJECT or labels.get('com.docker.compose.service')!=SERVICE or labels.get('com.docker.compose.project.config_files')!=str(COMPOSE):
        raise RuntimeError('Encore-compose-identity-drift')
    for record in e['mounted_file_hashes']:unchanged_file(record)
    return c

def verify_bundle(bundle,expected_checksums,tooling):
    if tooling!=CANDIDATE or expected_checksums!=CHECKSUMS:raise RuntimeError('unreviewed-candidate')
    if sha(bundle/'checksums.json')!=CHECKSUMS:raise RuntimeError('artifact-checksum-manifest-mismatch')
    sums=json.loads((bundle/'checksums.json').read_text())
    required={'image.tar.gz','image.json','source.tar.gz','composition.json','test-gate.json','automation-tests.jsonl','owner-contract-tests.jsonl'}
    if set(sums)!=required:raise RuntimeError('artifact-file-set-mismatch')
    for name,digest in sums.items():
        p=bundle/name
        if p.is_symlink() or not p.is_file() or sha(p)!=digest:raise RuntimeError('artifact-file-mismatch')
    image=json.loads((bundle/'image.json').read_text());composition=json.loads((bundle/'composition.json').read_text())
    if image.get('tooling_commit')!=CANDIDATE or composition.get('tooling_commit')!=CANDIDATE or image.get('bootstrap_owner_commit')!=OWNER:raise RuntimeError('artifact-source-identity-mismatch')
    if (composition.get('base_commit'),composition.get('isolation_commit'),composition.get('owner_commit'))!=(BASE,ISOLATION,OWNER):raise RuntimeError('artifact-base-or-patch-mismatch')
    if image.get('os')!='linux' or image.get('architecture')!='amd64' or image.get('composition_sha256')!=COMPOSITION or composition.get('composition_sha256')!=COMPOSITION:raise RuntimeError('artifact-platform-or-source-mismatch')
    if set(image.get('services',[]))!=SERVICES or set(composition.get('services',[]))!=SERVICES:raise RuntimeError('artifact-service-set-mismatch')
    if len(composition.get('migrations',{}))!=61 or len(composition.get('delta_files',[]))!=29:raise RuntimeError('artifact-scope-mismatch')
    tests=json.loads((bundle/'test-gate.json').read_text())
    if tests.get('required_test_count')!=73 or set(tests.get('passed_tests',[]))!=set(composition['required_tests']) or any(tests.get(k) for k in ('missing','failed','skipped','incomplete_packages')):raise RuntimeError('candidate-required-tests-not-passed')
    source={}
    with tarfile.open(str(bundle/'source.tar.gz'),'r:gz') as archive:
        for item in archive:
            if not item.isfile() or item.name.startswith('/') or '..' in pathlib.PurePosixPath(item.name).parts or item.name in source:raise RuntimeError('invalid-source-archive')
            source[item.name]=hashlib.sha256(archive.extractfile(item).read()).hexdigest()
    if len(source)!=744 or source!=composition['source_sha256'] or any(source.get(n)!=v for n,v in composition['migrations'].items()):raise RuntimeError('source-or-migration-checksum-mismatch')
    if hashlib.sha256(json.dumps(source,sort_keys=True).encode()).hexdigest()!=COMPOSITION:raise RuntimeError('complete-source-fingerprint-mismatch')
    with tarfile.open(str(bundle/'image.tar.gz'),'r:gz') as archive:
        manifests=json.load(archive.extractfile('manifest.json'))
        if len(manifests)!=1:raise RuntimeError('multiple-images-in-candidate')
        raw=archive.extractfile(manifests[0]['Config']).read();config_id='sha256:'+hashlib.sha256(raw).hexdigest();config=json.loads(raw)
        if config_id!=CANDIDATE_IMAGE or image.get('docker_image_id')!=config_id or config.get('architecture')!='amd64' or config.get('os')!='linux':raise RuntimeError('image-config-identity-mismatch')
    return image,config_id


def next_compose(original,image):
    value=json.loads(original)
    if SERVICE not in value.get('services',{}) or 'notification-core' not in value['services']:
        raise RuntimeError('expected-split-runtime-services-absent')
    changed=copy.deepcopy(value);changed['services'][SERVICE]['image']=image
    check=copy.deepcopy(changed);check['services'][SERVICE]['image']=value['services'][SERVICE]['image']
    if check!=value:raise RuntimeError('compose-change-exceeds-image')
    return (json.dumps(changed,indent=2)+'\n').encode()

def compose_up():
    run(['docker','compose','--project-directory',ROOT,'-p',PROJECT,'-f',COMPOSE,
         'up','-d','--no-deps','--pull','never',SERVICE],timeout=90)

def replace_compose(data,expected_hash,stage):
    if COMPOSE.is_symlink() or sha(COMPOSE)!=expected_hash:raise RuntimeError('compose-drift-before-write')
    temp=ROOT/('.q09-compose-'+stage.name+'.tmp')
    private_write(temp,data)
    os.replace(str(temp),str(COMPOSE))

def ready(image,spec,require_owner,expected_runtime=None,expected_id=None):
    global DEADLINE
    previous=DEADLINE
    DEADLINE=time.monotonic()+50
    try:return ready_loop(image,spec,require_owner,expected_runtime,expected_id)
    finally:DEADLINE=previous

def ready_loop(image,spec,require_owner,expected_runtime=None,expected_id=None):
    deadline=DEADLINE
    while time.monotonic()<deadline:
        try:
            c=observe_target()
            if c is None or c['Image']!=image or not c['State']['Running'] or c['RestartCount'] or (expected_id and c['Id']!=expected_id):
                raise RuntimeError('candidate-container-not-stable')
            if expected_runtime is not None and runtime_config(c)!=expected_runtime:
                raise RuntimeError('candidate-runtime-contract-drift')
            with urllib.request.urlopen('http://127.0.0.1:14000/v1/health',timeout=budget(3)) as response:
                if json.load(response).get('status')!='ok':raise RuntimeError('native-health-not-ready')
            if require_owner:
                try:
                    urllib.request.urlopen(urllib.request.Request('http://127.0.0.1:14000/internal/thingmodel/validate-command',data=b'{}',headers={'Content-Type':'application/json'}),timeout=budget(3))
                except urllib.error.HTTPError as e:
                    if e.code!=401:raise RuntimeError('owner-contract-route-or-auth-not-ready')
                else:raise RuntimeError('owner-contract-accepted-anonymous')
                try:urllib.request.urlopen(urllib.request.Request('http://127.0.0.1:14000/internal/thingmodel/uplink-contract',data=b'{}',headers={'Content-Type':'application/json'}),timeout=budget(3))
                except urllib.error.HTTPError as e:
                    if e.code!=401:raise RuntimeError('uplink-contract-route-or-auth-not-ready')
                else:raise RuntimeError('uplink-contract-accepted-anonymous')
            preserve(spec)
            return c
        except Exception:
            time.sleep(max(0,min(2,deadline-time.monotonic())))
    raise RuntimeError('bounded-readiness-failed')

def stage_release(bundle,stage,spec,checksum,tooling):
    original_container=baseline(spec)
    image,config_id=verify_bundle(bundle,checksum,tooling)
    free=shutil.disk_usage(str(ROOT)).free
    if free<2*image['image_size']+1024**3:raise RuntimeError('insufficient-stage-capacity')
    memory=dict((line.split(':',1)[0],line.split(':',1)[1].strip()) for line in pathlib.Path('/proc/meminfo').read_text().splitlines())
    if int(memory['MemAvailable'].split()[0])*1024<512*1024**2:raise RuntimeError('insufficient-memory-headroom')
    if stage.exists():raise RuntimeError('refusing-existing-stage')
    stage.mkdir(mode=0o700)
    original=COMPOSE.read_bytes()
    # docker load changes only local image cache; no running service is replaced.
    run(['docker','load','-i',bundle/'image.tar.gz'],timeout=120)
    loaded=json.loads(run(['docker','image','inspect',config_id]))[0]
    if loaded['Id']!=config_id:raise RuntimeError('loaded-config-image-mismatch')
    old_image=json.loads(run(['docker','image','inspect',spec['encore']['image_id']]))[0]
    same_image_defaults(old_image,loaded)
    next_data=next_compose(original,config_id)
    private_write(stage/'compose.before.private.json',original)
    private_write(stage/'compose.next.private.json',next_data)
    state={'phase':'staged','image_id':config_id,'old_image_id':spec['encore']['image_id'],
           'before_sha256':hashlib.sha256(original).hexdigest(),'next_sha256':hashlib.sha256(next_data).hexdigest(),
           'checksums_sha256':checksum,'tooling_commit':tooling,'spec':spec}
    state['tool_sha256']=sha(__file__)
    state['expected_runtime']=runtime_config(original_container)
    write_record(stage/'state.private.json',state)
    baseline(spec)
    return {'status':'STAGED_NOT_APPLIED','image_id':config_id,'compose_next_sha256':state['next_sha256']}

def may_restore_target(current,allowed_ids,old_image,new_image):
    if current is None:return True
    return current['Id'] in allowed_ids and current['Image'] in (old_image,new_image)

def apply_release(stage,rollback=False):
    state=json.loads((stage/'state.private.json').read_text());spec=state['spec']
    if state['tooling_commit']!=CANDIDATE or state['checksums_sha256']!=CHECKSUMS or state['image_id']!=CANDIDATE_IMAGE or state['old_image_id']!=PREVIOUS_IMAGE or state.get('tool_sha256')!=sha(__file__):raise RuntimeError('private-stage-identity-drift')
    before=stage/'compose.before.private.json';after=stage/'compose.next.private.json'
    if sha(before)!=state['before_sha256'] or sha(after)!=state['next_sha256']:raise RuntimeError('private-stage-file-drift')
    if rollback:
        preserve(spec)
        current=observe_target()
        if sha(COMPOSE)!=state['next_sha256']:
            raise RuntimeError('rollback-refuses-intervening-deployment')
        applied=json.loads((stage/'applied.private.json').read_text())
        if not may_restore_target(current,{applied['container_id']},state['old_image_id'],state['image_id']):
            raise RuntimeError('rollback-container-identity-drift')
        replace_compose(before.read_bytes(),state['next_sha256'],stage);compose_up()
        ready(state['old_image_id'],spec,False,state['expected_runtime'])
        return {'status':'ROLLED_BACK','image_id':state['old_image_id']}
    if any((stage/n).exists() for n in ('activation.private.json','attempt.private.json','applied.private.json')):raise RuntimeError('activation-already-attempted-reconcile-only')
    baseline(spec)
    write_record(stage/'activation.private.json',{'phase':'submitting','at':datetime.datetime.utcnow().isoformat()+'Z'})
    replace_compose(after.read_bytes(),state['before_sha256'],stage)
    owned_id=None
    try:
        compose_up()
        created=observe_target()
        if created is None or created['Image']!=state['image_id']:raise RuntimeError('target-not-created')
        owned_id=created['Id']
        write_record(stage/'attempt.private.json',{'container_id':owned_id,'image_id':created['Image']})
        c=ready(state['image_id'],spec,True,state['expected_runtime'],owned_id)
        write_record(stage/'applied.private.json',{'container_id':c['Id'],'image_id':c['Image'],'at':datetime.datetime.utcnow().isoformat()+'Z'})
        return {'status':'APPLIED_INITIAL_HEALTH','image_id':c['Image'],'acceptance':'PENDING_AUTHENTICATED_POST_LOCK_TESTS'}
    except Exception as original_error:
        # Roll back only our exact compose and only our old/new image. Never
        # override another deployment or try a broad whole-stack recovery.
        current=observe_target()
        allowed_ids={spec['encore']['id']}
        if owned_id:allowed_ids.add(owned_id)
        if sha(COMPOSE)!=state['next_sha256'] or not may_restore_target(current,allowed_ids,state['old_image_id'],state['image_id']):
            raise RuntimeError('apply-failed-rollback-blocked-by-drift')
        preserve(spec)
        replace_compose(before.read_bytes(),state['next_sha256'],stage);compose_up()
        ready(state['old_image_id'],spec,False,state['expected_runtime'])
        return {'status':'APPLY_FAILED_ROLLED_BACK','cause_type':type(original_error).__name__}

def reconcile(stage):
    state=json.loads((stage/'state.private.json').read_text())
    current=observe_target()
    return {'status':'READ_ONLY_RECONCILIATION_NO_RETRY','compose_sha256':sha(COMPOSE),'container':None if current is None else {'id':current['Id'],'image_id':current['Image'],'running':current['State']['Running'],'restart_count':current['RestartCount']},'candidate_image':state['image_id'],'previous_image':state['old_image_id'],'activation_recorded':(stage/'activation.private.json').exists(),'applied_recorded':(stage/'applied.private.json').exists()}


def main():
    p=argparse.ArgumentParser();p.add_argument('mode',choices=['stage','apply','rollback','reconcile'])
    p.add_argument('--stage',required=True);p.add_argument('--bundle');p.add_argument('--spec');p.add_argument('--checksums-sha');p.add_argument('--tooling-commit')
    args=p.parse_args();stage=pathlib.Path(args.stage)
    if stage.parent!=pathlib.Path('/opt/tp-community-actions') or stage.parent.is_symlink() or stage.is_symlink() or not re.fullmatch('q04-q09-encore-c3fd-[a-zA-Z0-9-]+',stage.name):
        raise RuntimeError('stage-must-be-an-explicit-Q09-release-directory')
    with LOCK.open('r') as lock:
        fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
        if args.mode=='stage':
            if not all([args.bundle,args.spec,re.fullmatch('[0-9a-f]{64}',args.checksums_sha or ''),re.fullmatch('[0-9a-f]{40}',args.tooling_commit or '')]):raise RuntimeError('explicit-stage-inputs-required')
            result=stage_release(pathlib.Path(args.bundle),stage,json.loads(pathlib.Path(args.spec).read_text()),args.checksums_sha,args.tooling_commit)
        elif args.mode=='reconcile':result=reconcile(stage)
        else:result=apply_release(stage,args.mode=='rollback')
    print(json.dumps(result))
    if result['status']=='APPLY_FAILED_ROLLED_BACK':raise SystemExit(1)


# Root-executed read-only operation. No files, images, services or DB are changed.
def capture_baseline():
    def record(path):
        path=pathlib.Path(path)
        if path.is_symlink():raise RuntimeError('preflight-symlink-refused')
        if not path.exists():return {'path':str(path),'exists':False}
        if not path.is_file():raise RuntimeError('preflight-regular-file-required')
        return {'path':str(path),'exists':True,'sha256':sha(path),'size':path.stat().st_size}
    directory_stage=pathlib.Path('/opt/tp-community-actions/q09-gin-directory-efb-ci43365065896')
    ds=json.loads((directory_stage/'state.private.json').read_text())
    da=json.loads((directory_stage/'applied.private.json').read_text())
    if ds['candidate']!=DIRECTORY_GIN_VERSION[len('gin-cas-command-'):] or da['version']!=DIRECTORY_GIN_VERSION or da['binary_sha256']!=DIRECTORY_GIN_BINARY:raise RuntimeError('directory-stage-identity-drift')
    pm=[p for p in json.loads(run(['/root/.local/share/pnpm/pm2','jlist'])) if p.get('name')=='thingspanel-backend-community']
    if len(pm)!=1 or pm[0]['pid']!=da['pid']:raise RuntimeError('directory-PM2-identity-drift')
    p=pm[0]
    c=observe_target()
    if c is None or c['Image']!=PREVIOUS_IMAGE:raise RuntimeError('previous-Encore-required')
    spec={'ready_for_stage':True,'purpose':'Only Encore db078 image to c3fd acceptance composition','compose':record(COMPOSE),'gin':{'name':p['name'],'pm_id':p['pm_id'],'pid':p['pid'],'binary':record(p['pm2_env']['pm_exec_path']),'config':record(ds['config']['path'])},'encore':{'name':NAME,'id':c['Id'],'image_id':c['Image'],'runtime_contract_sha256':contract(c),'mounted_file_hashes':[record(m['Source']) for m in c['Mounts'] if m['Type']=='bind' and pathlib.Path(m['Source']).is_file()]},'preserved_containers':ds['spec']['preserved_containers'],'preserved_files':ds['spec']['preserved_files'],'directory_receipt':record(directory_stage/'directory-readonly-acceptance.json')}
    if spec['gin']['config']['sha256']!=ds['config']['sha256']:raise RuntimeError('Gin-config-changed-since-reviewed-directory-release')
    baseline(spec)
    return {'status':'READ_ONLY_BASELINE_VERIFIED','mutated':False,'at':datetime.datetime.utcnow().isoformat()+'Z','spec':spec,'free_bytes':shutil.disk_usage(str(ROOT)).free,'release_tool_sha256':'c1fb9d9cdbf8705cdbce9a5c6bd149433e32d768b2b79c65a85cd278dba78312'}

if __name__=='__main__':
    with LOCK.open('r') as lock:
        fcntl.flock(lock,fcntl.LOCK_SH|fcntl.LOCK_NB)
        print(json.dumps(capture_baseline()))
