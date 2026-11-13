import json,subprocess,pathlib,shutil
p=pathlib.Path('/opt/tp-community-actions/incoming/rollout_preflight.py')
r=subprocess.run(['python3',str(p)],stdout=subprocess.PIPE,stderr=subprocess.PIPE,universal_newlines=True,timeout=120)
try: report=json.loads(r.stdout)
except Exception: report={'ready':False,'issues':['preflight execution failed'],'exit_code':r.returncode}
print(json.dumps({'preflight':report,'disk_free_bytes':shutil.disk_usage('/opt').free,'memory_available_kib':next(x.split()[1] for x in pathlib.Path('/proc/meminfo').read_text().splitlines() if x.startswith('MemAvailable:'))}))
