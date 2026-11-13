TOOLSHA='ae5a9ed1ffa7b222c8ab15565c005a9c245dfd4aaa8389b96efb89e99f9313aa'
import hashlib,importlib.util,json,os,pathlib,shutil,stat,sys,zipfile,fcntl
if sys.version_info<(3,11):raise RuntimeError('explicit-python311-required')
P=pathlib.Path('/opt/tp-community-actions')
if P.is_symlink() or P.resolve()!=P:raise RuntimeError('operation-root-invalid')
stage=P/'q04-q09-encore-c3fd-ci43366233046'
tool=P/'q04-q09-encore-c3fd-ci43366233046-release.py'
lockpath=P/'deployment.lock'
if lockpath.is_symlink() or stage.is_symlink() or tool.is_symlink():raise RuntimeError('operation-symlink-refused')
def load_tool():
 if not tool.is_file() or hashlib.sha256(tool.read_bytes()).hexdigest()!=TOOLSHA:raise RuntimeError('tool-identity-mismatch')
 s=importlib.util.spec_from_file_location('reviewed_encore_release',tool)
 module=importlib.util.module_from_spec(s);s.loader.exec_module(module)
 return module

with lockpath.open('r') as lock:
 fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
 release=load_tool()
 result=release.apply_release(stage,False)
print(json.dumps(result))
if result['status']!='APPLIED_INITIAL_HEALTH':raise SystemExit(1)
