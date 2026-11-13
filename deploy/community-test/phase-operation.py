import json,pathlib,os,fcntl
os.umask(0o077)
root=pathlib.Path('/opt/tp-community-actions')
with (root/'deployment.lock').open('a') as lock:
 fcntl.flock(lock,fcntl.LOCK_EX)
 value={'mode':'new-devices-preserve-legacy','source_database':'ThingsPanel','native_database':'native_main_20261028','legacy_data_preserved':True}
 path=root/'rollout-mode.private.json'
 if path.exists():
  assert json.loads(path.read_text())==value
 else:
  with path.open('x') as f: json.dump(value,f)
 path.chmod(0o600)
 print(json.dumps({'mode':value['mode'],'legacy_data_modified':False,'services_restarted':False}))
