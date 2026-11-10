import pathlib,json,shutil,subprocess,os
cache=pathlib.Path('/root/.npm')
assert not cache.is_symlink() and str(cache.resolve())=='/root/.npm'
assert not (cache/'_cacache').is_symlink()
npm=shutil.which('npm')
if not npm:
 candidates=list(pathlib.Path('/root/.nvm/versions/node').glob('*/bin/npm'))
 assert candidates,'npm executable unavailable'
 npm=str(candidates[-1])
env=dict(os.environ);env['PATH']=str(pathlib.Path(npm).parent)+':'+env.get('PATH','')
before=shutil.disk_usage('/').free
p=subprocess.run([npm,'cache','clean','--force','--cache',str(cache)],env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=120)
assert p.returncode==0,'npm cache cleanup failed'
print(json.dumps({'action':'npm-download-cache-clean','free_before':before,'free_after':shutil.disk_usage('/').free}))
