import datetime,fcntl,hashlib,json,pathlib,shutil,subprocess,os
root=pathlib.Path('/opt/tp-community-main-20261028')
out={'at':datetime.datetime.utcnow().isoformat()+'Z','operation':'owner-fix-readonly-baseline','changes':False}
def run(args):
    p=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,universal_newlines=True,timeout=12)
    if p.returncode:raise RuntimeError('readonly command failed: '+args[0])
    return p.stdout
def hashfile(path):
    p=pathlib.Path(path)
    if not p.exists():return {'path':str(p),'exists':False}
    if p.is_symlink():return {'path':str(p),'exists':True,'symlink':True}
    h=hashlib.sha256()
    with p.open('rb') as f:
        for chunk in iter(lambda:f.read(1024*1024),b''):h.update(chunk)
    return {'path':str(p),'exists':True,'sha256':h.hexdigest(),'size':p.stat().st_size}
lockpath=pathlib.Path('/opt/tp-community-actions/deployment.lock')
try:
    lock=lockpath.open('r')
    fcntl.flock(lock,fcntl.LOCK_SH|fcntl.LOCK_NB)
except (OSError,BlockingIOError):
    out['status']='blocked-deployment-lock';print(json.dumps(out));raise SystemExit(0)
try:
    out['lock']='shared nonblocking lock acquired for consistent read snapshot'
    out['controllers']=[hashfile(root/f) for f in ['http-runtime/scope.py','ha-runtime/scope.py','http-runtime/broker_scope_roles.py','ha-runtime/broker_scope_roles.py']]
    out['containers']=[]
    for name in ['tp-community-main-47-broker-owner-1','tp-community-main-47-broker-mosquitto-1','tp-community-main-47-encore-api-1']:
        c=json.loads(run(['docker','inspect',name]))[0]
        labels=c['Config'].get('Labels') or {}
        mounts=[{'source':m['Source'],'destination':m['Destination'],'type':m['Type'],'rw':m['RW']} for m in c['Mounts']]
        refs=[]
        for f in labels.get('com.docker.compose.project.config_files','').split(','):
            if f:refs.append(hashfile(f))
        out['containers'].append({'name':name,'id':c['Id'],'image_id':c['Image'],'image_reference':c['Config']['Image'],'running':c['State']['Running'],'health':c['State'].get('Health',{}).get('Status'),'restart_count':c['RestartCount'],'started_at':c['State']['StartedAt'],'compose_project':labels.get('com.docker.compose.project'),'compose_service':labels.get('com.docker.compose.service'),'compose_workdir':labels.get('com.docker.compose.project.working_dir'),'compose_files':refs,'mount_references':mounts,'source_labels':{k:v for k,v in labels.items() if k in ['org.opencontainers.image.revision','org.opencontainers.image.source']}})
    procs=json.loads(run(['pm2','jlist']))
    matched=[p for p in procs if p.get('name')=='thingspanel-backend-community']
    if len(matched)!=1:raise RuntimeError('community Gin process not unique')
    p=matched[0];env=p['pm2_env'];binary=env['pm_exec_path'];cwd=env['pm_cwd']
    out['gin']={'process':p['name'],'pid':p['pid'],'status':env.get('status'),'restarts':env.get('restart_time'),'cwd':cwd,'binary':hashfile(binary),'version':run([binary,'--version']).strip(),'config':hashfile(pathlib.Path(cwd)/'configs/conf.yml')}
    out['policy_file']=hashfile(root/'active-broker-services.private.json')
    policy=json.loads((root/'active-broker-services.private.json').read_text())
    out['policy_summary']=[{'index':i,'principal_kind':s.get('principal',{}).get('principal_kind'),'plugin_socket':bool(s.get('plugin_socket')),'target_scoped_publisher':bool(s.get('target_scoped_publisher')),'target_tenant_scope':s.get('target_tenant_scope'),'device_count':len(s.get('allowed_device_ids',[])),'tenant_count':len(s.get('allowed_tenant_ids',[])),'publish_rule_count':len(s.get('policy',{}).get('publish',[])),'subscribe_rule_count':len(s.get('policy',{}).get('subscribe',[]))} for i,s in enumerate(policy)]
    mem=dict(line.split(':',1) for line in pathlib.Path('/proc/meminfo').read_text().splitlines())
    disk=shutil.disk_usage(str(root))
    out['capacity']={'mem_available_kib':int(mem['MemAvailable'].split()[0]),'swap_total_kib':int(mem['SwapTotal'].split()[0]),'swap_free_kib':int(mem['SwapFree'].split()[0]),'disk_total_bytes':disk.total,'disk_free_bytes':disk.free}
    out['running_pm2']=[{'name':p.get('name'),'pid':p.get('pid'),'status':p.get('pm2_env',{}).get('status')} for p in procs]
    out['systemd_jobs']=run(['systemctl','list-jobs','--no-legend','--no-pager']).strip().splitlines()
    out['managed_scope_units']={}
    for unit in ['tp-http-scope.service','tp-ha-scope.service','tp-http-scope.timer','tp-ha-scope.timer']:
        data=run(['systemctl','show',unit,'--property=ActiveState,SubState,MainPID,ExecMainStatus,LastTriggerUSec,NextElapseUSecRealtime'])
        out['managed_scope_units'][unit]=dict(line.split('=',1) for line in data.splitlines() if '=' in line)
    out['running_task_names']=run(['ps','-eo','pid=,comm=']).strip().splitlines()
    out['status']='read-only-snapshot-complete'
except Exception as e:
    out['status']='read-only-snapshot-incomplete';out['error_type']=type(e).__name__
finally:
    fcntl.flock(lock,fcntl.LOCK_UN);lock.close()
    out['lock_released']=True
print(json.dumps(out))
