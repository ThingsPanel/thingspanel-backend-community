import pathlib,json,subprocess,shutil,time
root=pathlib.Path('/opt/tp-community-actions/encore-releases/70bd7a1d31c11a02f3b32a106b0290ebe722b580')
out={'free_bytes':shutil.disk_usage('/').free,'logs':{},'containers':subprocess.run(['docker','ps','--format','{{.Names}} {{.Image}} {{.Status}}'],stdout=subprocess.PIPE).stdout.decode()}
for name in ['build.private.log','failure.private.log']:
 p=root/name
 if p.exists():out['logs'][name]={'bytes':p.stat().st_size,'seconds_since_write':int(time.time()-p.stat().st_mtime),'tail':p.read_text(errors='replace').splitlines()[-15:]}
p=root/'deployment.json'
if p.exists():
 d=json.loads(p.read_text());out['receipt']={k:d[k] for k in ['commit','image','migrations_applied'] if k in d}
out['build_processes']=[line for line in subprocess.run(['ps','-eo','pid,ppid,comm,etime,pcpu,pmem'],stdout=subprocess.PIPE).stdout.decode().splitlines() if any(word in line.split() for word in ['compile','link','go','encore'])]
out['memory']=subprocess.run(['free','-m'],stdout=subprocess.PIPE).stdout.decode()
print(json.dumps(out))
