import hashlib,json,pathlib,subprocess
pm2='/root/.local/share/pnpm/pm2'
rows=json.loads(subprocess.check_output([pm2,'jlist']))
p=next(x['pm2_env'] for x in rows if x['name']=='thingspanel-backend-community')
exe=pathlib.Path(p['pm_exec_path'])
print(json.dumps({'cwd':p['pm_cwd'],'executable':str(exe),'is_symlink':exe.is_symlink(),
 'version':subprocess.check_output([str(exe),'--version'],cwd=p['pm_cwd']).decode().strip(),
 'binary_sha256':hashlib.sha256(exe.read_bytes()).hexdigest(),'status':p['status'],
 'disk':subprocess.check_output(['df','-h','/opt']).decode()}))
