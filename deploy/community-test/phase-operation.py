import pathlib,json,subprocess,os
lock=pathlib.Path('/opt/tp-community-actions/deployment.lock');inode=str(lock.stat().st_ino);out={'locks':[],'containers':[]}
for l in pathlib.Path('/proc/locks').read_text().splitlines():
 fields=l.split()
 if any(v.endswith(':'+inode) for v in fields):
  pid=fields[4];entry={'pid':pid,'lock':l}
  p=pathlib.Path('/proc')/pid
  try:
   entry['comm']=(p/'comm').read_text().strip();entry['cwd']=str((p/'cwd').resolve());entry['script_names']=[pathlib.Path(a).name for a in (p/'cmdline').read_bytes().decode(errors='replace').split('\0') if a.endswith('.py')]
  except Exception:pass
  out['locks'].append(entry)
for n in ['tp-community-main-47-broker-owner-1','tp-community-main-47-broker-mosquitto-1']:
 x=json.loads(subprocess.check_output(['docker','inspect',n]))[0]
 out['containers'].append({'name':n,'image':x['Image'],'status':x['State']['Status'],'restart_count':x['RestartCount'],'entrypoint':x['Config']['Entrypoint'],'cmd':x['Config']['Cmd'],'mounts':[{'source':m['Source'],'dest':m['Destination']} for m in x['Mounts']]})
print(json.dumps(out))
