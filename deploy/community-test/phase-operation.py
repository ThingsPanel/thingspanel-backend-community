"""Deploy a verified frontend fix while retaining the exact staged device profile."""
import fcntl,json,os,pathlib,shutil,subprocess,sys,urllib.request
sys.path.insert(0,'/opt/tp-community-actions/incoming')
from deploy_control_plane import unpack,verify_files,validate_frontend
sha='4d68e406a7d7af5fbf8b03783d943c1cc6abfdf5'
expected='b0ee3fd6478dbf7d14fd6b9d332b8b31511a4227'
root=pathlib.Path('/opt/tp-community-actions')
target=pathlib.Path('/home/thingspanel/nginx/www/dist')
os.umask(0o077)
with (root/'deployment.lock').open('a') as lock:
 fcntl.flock(lock,fcntl.LOCK_EX)
 if not target.is_symlink():raise RuntimeError('Expected an existing rollback-capable release')
 old=target.resolve()
 if (old/'SOURCE_COMMIT').read_text().strip()!=expected:raise RuntimeError('Online frontend changed')
 old_profile=json.loads((old/'RELEASE_PROFILE.json').read_text())
 validate_frontend(old_profile,expected)
 package=root/'incoming'/('control-plane-frontend-'+sha+'.tar.gz')
 if not package.is_file():raise RuntimeError('Verified candidate not staged')
 if shutil.disk_usage(str(root)).free<1024**3:raise RuntimeError('Insufficient release and rollback capacity')
 release=root/'frontend-fix-releases'/sha
 if release.exists():raise RuntimeError('Release exists; inspect before retry')
 site=release/'site'
 site.mkdir(parents=True)
 unpack(package,site)
 manifest=json.loads((site/'manifest.json').read_text())
 if manifest.get('commit')!=sha or manifest.get('profile')!='control-plane':raise RuntimeError('Candidate identity mismatch')
 verify_files(site,manifest)
 profile=json.loads((site/'RELEASE_PROFILE.json').read_text())
 validate_frontend(profile,sha)
 if profile['overrides']!=old_profile['overrides']:raise RuntimeError('Device implementation changed')
 if (site/'SOURCE_COMMIT').read_text().strip()!=sha:raise RuntimeError('Source marker mismatch')
 for parent in [release.parent,release,site]:parent.chmod(0o755)
 for p in site.rglob('*'):p.chmod(0o755 if p.is_dir() else 0o644)
 link=target.with_name('dist.next-'+sha)
 if os.path.lexists(str(link)):raise RuntimeError('Switch path exists')
 def switch(path):
  os.symlink(str(path),str(link));os.replace(str(link),str(target))
 def get(name):
  req=urllib.request.Request('http://127.0.0.1/'+name,headers={'Host':'c.thingspanel.cn','Cache-Control':'no-cache'})
  with urllib.request.urlopen(req,timeout=15) as r:return r.read()
 subprocess.check_call(['nginx','-t'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
 try:
  switch(site)
  for name in ['SOURCE_COMMIT','index.html','mp-device/index.html','RELEASE_PROFILE.json']:
   if get(name)!=(site/name).read_bytes():raise RuntimeError('HTTP release check failed')
 except Exception:
  switch(old)
  (release/'rollback.json').write_text(json.dumps({'restored':True}))
  raise
 result={'commit':sha,'previous':str(old),'profile':'control-plane','backend_changed':False,'database_changed':False}
 (release/'deployment.json').write_text(json.dumps(result))
 print(json.dumps(result))
