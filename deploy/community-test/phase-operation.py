import pathlib,json,re,subprocess
root=pathlib.Path('/home/plugin/access-service-modbus-tcp');out={'files':[],'references':[],'config_keys':[],'logs':[]}
for p in root.rglob('*'):
 if not p.is_file() or '.git' in p.parts:continue
 if p.suffix in ['.go','.yaml','.yml','.json','.toml']:
  out['files'].append(str(p.relative_to(root)))
  if p.stat().st_size>200000:continue
  lines=p.read_text(errors='replace').splitlines()
  if p.suffix=='.go':
   for i,l in enumerate(lines):
    if '/api/' in l or re.search(r'func .*?(Refresh|Load|Init|Start|Notify|Device)',l):
     if not re.search(r'password|secret|token|api.?key',l,re.I):out['references'].append({'file':str(p.relative_to(root)),'line':i+1,'text':l[:240]})
  else:
   for l in lines:
    m=re.match(r'\s*([A-Za-z_][\w.-]*)\s*[:=]',l)
    if m:out['config_keys'].append({'file':str(p.relative_to(root)),'key':m.group(1),'configured':bool(l.split(m.group(1),1)[1].strip(' :=\"\''))})
p=subprocess.run(['/root/.local/share/pnpm/pm2','jlist'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=10)
for x in json.loads(p.stdout):
 if x.get('name')!='access-service-modbus-tcp':continue
 for k in ['pm_out_log_path','pm_err_log_path']:
  path=x.get('pm2_env',{}).get(k)
  if path and pathlib.Path(path).exists():
   lines=pathlib.Path(path).read_text(errors='replace').splitlines()[-100:]
   for l in lines:
    if any(w in l.lower() for w in ['error','fail','错误','失败','warn']):
     l=re.sub(r'\{.*','[payload omitted]',l)
     l=re.sub(r'(?i)(token|password|secret|api.?key)\s*[:=]\s*\S+',r'\1=[redacted]',l)
     out['logs'].append(l[:300])
out['references']=out['references'][:100];out['files']=out['files'][:120]
print(json.dumps(out))
