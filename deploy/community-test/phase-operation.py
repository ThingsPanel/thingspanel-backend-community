import os,json,pathlib,tarfile,hashlib,subprocess
os.umask(0o077)
commit='1af192946db407e05909827ce627f1f82755640e'
tool_sha='fa7f0f0efed0741403ea2dfe95e3c32f93e3a57bbf77827458f551ad6e448b21'
archive=pathlib.Path('/opt/tp-community-actions/incoming/mqtt-publisher-'+commit+'.tar.gz')
root=pathlib.Path('/opt/tp-community-actions/mqtt-publisher-'+commit+'-stage-20261113-qa-fixes')
allowed={'ThingsPanel-Go','broker-image.tar.gz','RELEASE_PROFILE.json','mqtt_publisher_release.py','manifest.json'}
result={'operation':'stage-only-diagnostics-http-deadline','commit':commit,'directory':str(root),'live_configuration_changed':False}
def digest(path):
 h=hashlib.sha256()
 with path.open('rb') as f:
  for block in iter(lambda:f.read(1024*1024),b''):h.update(block)
 return h.hexdigest()
try:
 assert archive.is_file() and not archive.is_symlink(),'Candidate archive missing'
 assert not root.is_symlink(),'Refusing symlink stage'
 with tarfile.open(archive,'r:gz') as tf:
  members=tf.getmembers()
  assert {m.name for m in members}==allowed and len(members)==len(allowed),'Unexpected archive entries'
  assert all(m.isfile() and m.size>0 for m in members) and sum(m.size for m in members)<1024**3,'Unsafe archive entries'
  assert hashlib.sha256(tf.extractfile('mqtt_publisher_release.py').read()).hexdigest()==tool_sha,'Candidate tool mismatch'
  manifest=json.load(tf.extractfile('manifest.json'));assert manifest['commit']==commit,'Candidate source mismatch'
  if root.exists():
   assert root.is_dir() and {p.name for p in root.iterdir()}==allowed,'Previously extracted stage changed'
  else:
   root.mkdir(mode=0o700)
   tf.extractall(str(root))
 assert all((root/n).is_file() and not (root/n).is_symlink() for n in allowed),'Invalid extracted file'
 for name,sha in manifest['sha256'].items():assert name in allowed and digest(root/name)==sha,'Payload hash mismatch'
 assert digest(root/'mqtt_publisher_release.py')==tool_sha
 p=subprocess.run(['/usr/bin/python3.11',str(root/'mqtt_publisher_release.py'),'stage',str(root)],stdout=subprocess.PIPE,stderr=subprocess.PIPE,universal_newlines=True,timeout=90)
 if p.returncode:
  result['status']='stage-rejected';result['error']=p.stderr.strip().splitlines()[-1] if p.stderr.strip() else 'stage process failed'
 else:
  result.update(json.loads(p.stdout));state=json.loads((root/'stage.private.json').read_text())
  result['snapshot']={k:state[k] for k in ('old_version','next_version','images_before','container_ids','binary_before_sha256','config_sha256','policy_sha256','process_spec_sha256','compose_files','controller_files')}
  result['candidate']={k:manifest[k] for k in ('commit','image_id','image_size_bytes','sha256','tool_sha256')}
except Exception as e:result.update(status='stage-rejected',error=type(e).__name__+': '+str(e))
print(json.dumps(result))
