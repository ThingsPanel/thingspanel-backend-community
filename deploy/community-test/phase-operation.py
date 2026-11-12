import os,json,pathlib,tarfile,hashlib,subprocess,sys,datetime
os.umask(0o077)
commit='5fb4c68e11b4ce3632abad32197a345e3d0f0f89'
archive=pathlib.Path('/opt/tp-community-actions/incoming/mqtt-publisher-'+commit+'.tar.gz')
root=pathlib.Path('/opt/tp-community-actions/mqtt-publisher-'+commit+'-stage-20261112100920')
allowed={'ThingsPanel-Go','broker-image.tar.gz','RELEASE_PROFILE.json','mqtt_publisher_release.py','manifest.json'}
result={'operation':'stage-only','commit':commit,'directory':str(root),'live_configuration_changed':False}
try:
 assert archive.is_file() and not archive.is_symlink(), 'Candidate archive missing'
 with tarfile.open(archive,'r:gz') as tf:
  members=tf.getmembers()
  assert {m.name for m in members}==allowed and len(members)==len(allowed), 'Unexpected archive entries'
  assert all(m.isfile() and m.size>0 for m in members) and sum(m.size for m in members)<1024**3,'Unsafe archive entries'
  tool=tf.extractfile('mqtt_publisher_release.py').read()
  assert hashlib.sha256(tool).hexdigest()=='70e2e2f16b09d4cc4a07777fc89b5265c41cb832d914e16c5b84fa16eac9199c','Candidate tool mismatch'
  manifest=json.load(tf.extractfile('manifest.json'))
  assert manifest['commit']==commit,'Candidate source mismatch'
  assert root.is_dir() and not root.is_symlink() and {p.name for p in root.iterdir()}==allowed, 'Previously extracted directory changed'
  for m in members:
   assert (root/m.name).is_file() and not (root/m.name).is_symlink(), 'Invalid extracted file'
   assert hashlib.sha256((root/m.name).read_bytes()).digest()==hashlib.sha256(tf.extractfile(m).read()).digest(), 'Extracted payload changed'
  for name,digest in manifest['sha256'].items():
   assert hashlib.sha256((root/name).read_bytes()).hexdigest()==digest,'Payload hash mismatch'
 lock=pathlib.Path('/opt/tp-community-actions/deployment.lock');lock.touch(exist_ok=True)
 p=subprocess.run(['python3',str(root/'mqtt_publisher_release.py'),'stage',str(root)],stdout=subprocess.PIPE,stderr=subprocess.PIPE,universal_newlines=True,timeout=90)
 if p.returncode:
  result['status']='stage-rejected';result['error']=p.stderr.strip().splitlines()[-1] if p.stderr.strip() else 'stage process failed'
 else:
  result.update(json.loads(p.stdout));s=json.loads((root/'stage.private.json').read_text())
  result['snapshot']={k:s[k] for k in ('old_version','next_version','images_before','container_ids','binary_before_sha256','config_sha256','policy_sha256','process_spec_sha256','compose_files')}
  result['candidate']={k:manifest[k] for k in ('commit','image_id','image_size_bytes','sha256','tool_sha256')}
except Exception as e:
 result['status']='stage-rejected';result['error']=type(e).__name__+': '+str(e)
print(json.dumps(result))
