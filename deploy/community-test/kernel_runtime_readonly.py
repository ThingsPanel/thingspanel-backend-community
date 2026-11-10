import pathlib,json,fcntl,os,time,shutil
root=pathlib.Path('/root/.cache/encore-build')
assert not root.is_symlink() and str(root.resolve())=='/root/.cache/encore-build'
lock=open('/opt/tp-community-actions/deployment.lock','a');fcntl.flock(lock,fcntl.LOCK_EX)
referenced=set()
for proc in pathlib.Path('/proc').iterdir():
 if not proc.name.isdigit():continue
 for ref in ['exe','cwd']:
  try:referenced.add(os.readlink(str(proc/ref)))
  except FileNotFoundError:pass
 try:
  for line in (proc/'maps').read_text().splitlines():
   parts=line.split(None,5)
   if len(parts)==6:referenced.add(parts[5])
  for fd in (proc/'fd').iterdir():
   try:referenced.add(os.readlink(str(fd)))
   except FileNotFoundError:pass
 except (FileNotFoundError,ProcessLookupError):pass
items=[]
for child in root.iterdir():
 if child.is_symlink() or not child.is_dir() or child.resolve().parent!=root:continue
 build=child/'build'
 if build.is_symlink() or not build.is_dir() or build.resolve().parent!=child:continue
 f=build/'encore_app_out'
 if f.is_symlink() or not f.is_file() or f.resolve().parent!=build:continue
 with f.open('rb') as stream:
  if stream.read(4)!=b'\x7fELF':continue
 items.append(f)
items.sort(key=lambda p:p.stat().st_mtime,reverse=True)
removed=[];before=shutil.disk_usage('/').free
for f in items[2:]:
 if time.time()-f.stat().st_mtime<86400 or str(f) in referenced:continue
 size=f.stat().st_size
 f.unlink();removed.append({'path':str(f),'bytes':size})
print(json.dumps({'removed_reproducible_builds':removed,'free_before':before,'free_after':shutil.disk_usage('/').free}))
