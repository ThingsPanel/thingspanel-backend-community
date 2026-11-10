import pathlib,json,os,time,subprocess
root=pathlib.Path('/root/.cache/encore-build')
rows=[]
for p in root.rglob('*'):
 if p.is_file() and p.stat().st_size>1024*1024:
  with p.open('rb') as f:magic=f.read(4).hex()
  rows.append({'path':str(p),'bytes':p.stat().st_size,'age_days':round((time.time()-p.stat().st_mtime)/86400,2),'magic':magic,'symlink':p.is_symlink()})
print(json.dumps({'large_files':rows}))
