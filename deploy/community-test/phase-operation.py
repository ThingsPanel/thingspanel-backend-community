import pathlib,json,shutil
p=pathlib.Path('/opt/tp-community-actions/migration-tools/5a1a755b51e1a7c7d466305f41d8dc00bec29d98')
print(json.dumps({'files':{x.name:x.stat().st_size for x in p.iterdir()} if p.exists() else {},'free_bytes':shutil.disk_usage('/opt').free}))
