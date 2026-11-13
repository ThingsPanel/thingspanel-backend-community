"""Read only runtime inventory for choosing reviewed exact baseline selectors.

No environment values, container commands, process arguments, logs or credentials.
No service mutation or daemon restart. Uses the existing deployment lock.
"""
import datetime,fcntl,json,pathlib,subprocess

def run(args):
    r=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,
                     universal_newlines=True,timeout=15)
    if r.returncode:raise RuntimeError('read-only inventory command failed')
    return r.stdout

def main():
    out={'operation':'Q09-readonly-runtime-names','at':datetime.datetime.utcnow().isoformat()+'Z',
         'changes':False,'containers':[],'pm2':[]}
    try:
        lock=pathlib.Path('/opt/tp-community-actions/deployment.lock').open('r')
        fcntl.flock(lock,fcntl.LOCK_SH|fcntl.LOCK_NB)
    except OSError:
        out['status']='BLOCKED-deployment-lock';print(json.dumps(out));return 2
    try:
        ids=run(['docker','ps','-aq','--no-trunc']).splitlines()
        if len(ids)>150:raise RuntimeError('unexpected inventory size')
        for start in range(0,len(ids),20):
            for c in json.loads(run(['docker','inspect']+ids[start:start+20])):
                labels=c['Config'].get('Labels') or {}
                out['containers'].append({'id':c['Id'],'name':c['Name'].lstrip('/'),
                    'image_id':c['Image'],'image_reference':c['Config']['Image'],
                    'running':c['State']['Running'],'status':c['State']['Status'],
                    'project':labels.get('com.docker.compose.project'),
                    'service':labels.get('com.docker.compose.service'),
                    'working_dir':labels.get('com.docker.compose.project.working_dir'),
                    'compose_refs':labels.get('com.docker.compose.project.config_files')})
        # Previously verified PM2 entrypoint; absence is an explicit blocker.
        pm2='/root/.local/share/pnpm/pm2'
        for p in json.loads(run([pm2,'jlist'])):
            env=p.get('pm2_env') or {}
            out['pm2'].append({'name':p.get('name'),'pm_id':p.get('pm_id'),
                 'pid':p.get('pid'),'status':env.get('status'),
                 'binary':env.get('pm_exec_path'),'cwd':env.get('pm_cwd')})
        out['pm2_command']=pm2;out['status']='READ_ONLY_COMPLETE'
    except Exception as e:
        out['status']='INCOMPLETE';out['error_type']=type(e).__name__
    finally:
        fcntl.flock(lock,fcntl.LOCK_UN);lock.close();out['lock_released']=True
    print(json.dumps(out))
    return 0 if out['status']=='READ_ONLY_COMPLETE' else 1

if __name__=='__main__':raise SystemExit(main())
