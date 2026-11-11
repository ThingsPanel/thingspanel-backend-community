import json,pathlib,os,subprocess
items=[]
for entry in pathlib.Path('/proc').iterdir():
 if not entry.name.isdigit():continue
 try:
  if (entry/'comm').read_text().strip() not in ('encore','encored'):continue
  args=(entry/'cmdline').read_bytes().split(b'\x00')
  words=[a.decode(errors='replace') for a in args if a]
  items.append({'pid':int(entry.name),'executable':os.readlink(str(entry/'exe')),'cwd':os.readlink(str(entry/'cwd')),'argv_first_two':words[:2], 'flags':[a for a in words[2:] if a.startswith('-') and '=' not in a]})
 except (OSError,IOError):continue
print(json.dumps({'processes':items,'time':subprocess.check_output(['date','-u','+%FT%TZ']).decode().strip()}))
