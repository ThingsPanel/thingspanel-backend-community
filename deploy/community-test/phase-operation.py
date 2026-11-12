import pathlib,json,re,subprocess
out={'broker':{},'qa_logs':[],'sdk_dirs':[]}
for name in ['tp-community-main-47-broker-mosquitto-1','tp-community-main-47-broker-owner-1']:
 p=subprocess.run(['docker','logs','--since','25m','--tail','1500',name],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=10);lines=p.stdout.decode(errors='replace').splitlines();matches=[]
 for l in lines:
  if any(w in l for w in ['ModbusTCP','16fb8743-bc16-a53a-f6b1-5e82b07a760b','plugin','denied','not author','fenced']):
   if re.search(r'password|secret|token|payload|credential',l,re.I):continue
   matches.append(l[:500])
 out['broker'][name]={'total':len(lines),'matched':matches[-80:]}
p=pathlib.Path('/home/plugin/access-service-modbus-tcp/cmd/logs/app.log')
f=p.open('rb');f.seek(max(0,p.stat().st_size-300000));lines=f.read().decode(errors='replace').splitlines()
for l in lines:
 if '16fb8743-bc16-a53a-f6b1-5e82b07a760b' in l:
  if re.search(r'password|secret|token|payload|credential',l,re.I):continue
  out['qa_logs'].append(l[:500])
out['qa_logs']=out['qa_logs'][-15:]
r=pathlib.Path('/home/plugin/access-service-modbus-tcp/internal/platform/platform.go');lines=r.read_text(errors='replace').splitlines();out['publish_envelope']=[str(i+1)+': '+l for i,l in enumerate(lines) if 258<=i+1<=283]
p=subprocess.run(['ss','-tnp'],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=5);out['mqtt_sockets']=[l for l in p.stdout.decode(errors='replace').splitlines() if ':1883' in l and ('access-service' in l or '1599577' in l)]
print(json.dumps(out))
