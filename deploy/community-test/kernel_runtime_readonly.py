import pathlib,json,subprocess,os
p=pathlib.Path('/root/.cache/encore-build')
out={'entries':[],'processes':[]}
for x in p.iterdir():
 out['entries'].append({'name':x.name,'symlink':x.is_symlink(),'files':[c.name for c in x.iterdir()][:12] if x.is_dir() else [],'bytes':subprocess.run(['du','-sk',str(x)],stdout=subprocess.PIPE).stdout.decode().split()[0]})
for pid in subprocess.run(['pgrep','-x','go|compile|link|encore'],stdout=subprocess.PIPE).stdout.decode().split():
 d=pathlib.Path('/proc')/pid
 out['processes'].append({'pid':pid,'comm':(d/'comm').read_text().strip(),'cwd':os.readlink(str(d/'cwd')),'exe':os.readlink(str(d/'exe'))})
print(json.dumps(out))
