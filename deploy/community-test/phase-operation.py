import pathlib,json,subprocess,os
lock=pathlib.Path('/opt/tp-community-actions/deployment.lock');inode=str(lock.stat().st_ino);out={'locks':[],'containers':[]}
for l in pathlib.Path('/proc/locks').read_text().splitlines():
 fields=l.split()
 if any(v.endswith(':'+inode) for v in fields):
  pid=fields[4];entry={'pid':pid,'lock':l}
  p=pathlib.Path('/proc')/pid
  try:
   entry['comm']=(p/'comm').read_text().strip();entry['cwd']=str((p/'cwd').resolve());entry['script_paths']=[a for a in (p/'cmdline').read_bytes().decode(errors='replace').split('\0') if a.endswith('.py')];entry['start_ticks']=(p/'stat').read_text().split()[21]
  except Exception:pass
  out['locks'].append(entry)
for n in ['tp-community-main-47-broker-owner-1','tp-community-main-47-broker-mosquitto-1']:
 x=json.loads(subprocess.check_output(['docker','inspect',n]))[0]
 out['containers'].append({'name':n,'image':x['Image'],'status':x['State']['Status'],'restart_count':x['RestartCount'],'entrypoint':x['Config']['Entrypoint'],'cmd':x['Config']['Cmd'],'mounts':[{'source':m['Source'],'dest':m['Destination']} for m in x['Mounts']]})
import hashlib
root=pathlib.Path('/opt/tp-community-actions/mqtt-publisher-5fb4c68e11b4ce3632abad32197a345e3d0f0f89-stage-20261112100920');state=json.loads((root/'stage.private.json').read_text())
for key in ['binary','config_path','policy_path']:
 p=pathlib.Path(state[key]);out[key+'_sha256']=hashlib.sha256(p.read_bytes()).hexdigest()
expected=json.loads((root/'services.next.private.json').read_text());current=json.loads(pathlib.Path(state['policy_path']).read_text());diff=[]
for index,x in enumerate(current):
 y=expected[index] if index<len(expected) else {}
 changed=[k for k in set(x)|set(y) if x.get(k)!=y.get(k)]
 if changed:diff.append({'index':index,'changed_fields':sorted(changed),'platform_role':x.get('target_tenant_scope'),'device_count':len(x.get('allowed_device_ids',[])),'tenant_count':len(x.get('allowed_tenant_ids',[]))})
out['policy_diff_from_candidate']=diff;out['service_counts']=[len(expected),len(current)]
print(json.dumps(out))
