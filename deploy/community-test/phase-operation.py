import pathlib,json,re,subprocess,hashlib,urllib.parse
out={'source':{},'config':{},'log_signals':[]}
r=pathlib.Path('/home/plugin/access-service-modbus-tcp')
for f in ['internal/platform/platform.go','internal/service/modbus_service.go']:
 lines=(r/f).read_text(errors='replace').splitlines();selected=[]
 for i,l in enumerate(lines):
  if any(k in l for k in ['Publish','SendTelemetry','SendData','dataReportLoop','DataReport','ReportData','MQTT']):
   selected.extend(str(j+1)+': '+lines[j] for j in range(max(0,i-2),min(len(lines),i+12)) if not re.search('password|secret|token',lines[j],re.I))
 out['source'][f]=list(dict.fromkeys(selected))
config=(r/'configs/config.yaml').read_text(errors='replace')
logpaths=[]
for line in config.splitlines():
 m=re.match(r'\s*(url|mqtt_broker|service_identifier|filePath|base_dir):\s*(.*?)\s*(?:#.*)?$',line)
 if m:
  value=m.group(2).strip('\"\' ')
  if m.group(1) in ['url','mqtt_broker']:value=re.sub(r'//[^/@]+@','//[redacted]@',value)
  out['config'][m.group(1)]=value
  if m.group(1)=='filePath':logpaths += [r/value,r/'cmd'/value]
for root in ['/root/go/pkg/mod/github.com/!things!panel','/home/plugin']:
 p=pathlib.Path(root)
 for d in list(p.glob('tp-protocol-sdk-go*'))[:4]:
  for f in d.rglob('*.go'):
   if f.stat().st_size>100000:continue
   lines=f.read_text(errors='replace').splitlines()
   hits=[]
   for i,l in enumerate(lines):
    if re.search(r'(/telemetry|/status|Publish\(|MQTTBroker|device/data|attributes)',l):
     hits.extend(str(j+1)+': '+lines[j] for j in range(max(0,i-2),min(len(lines),i+7)) if not re.search('password|secret|token',lines[j],re.I))
   if hits:out['source'][str(f)]=list(dict.fromkeys(hits))[:100]
for p in logpaths:
 if not p.is_file():continue
 raw=p.open('rb');raw.seek(max(0,p.stat().st_size-80000));lines=raw.read().decode(errors='replace').splitlines();counts={}
 for word in ['not authorized','not authorised','connection refused','Publish','publish','MQTT','error','Error','failed','disconnected','connected']:
  counts[word]=sum(word in l for l in lines)
 out['log_signals'].append({'path':str(p),'counts':counts})
p=subprocess.run(['docker','logs','--since','30m','--tail','400','tp-community-broker'],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=10)
lines=p.stdout.decode(errors='replace').splitlines();out['broker_signals']={'line_count':len(lines),'matches':[]}
for l in lines:
 if 'ModbusTCP' in l or ('not author' in l.lower()):
  out['broker_signals']['matches'].append(re.sub(r'(?i)(password|secret|token)[^ ,]*[=: ]+[^ ,]+',r'\1=[redacted]',l)[:300])
print(json.dumps(out))
