import pathlib,json,subprocess
p=pathlib.Path('/root/.cache/encore-build')
print(json.dumps({'path':str(p.resolve()),'symlink':p.is_symlink(),'entries':[x.name for x in p.iterdir()][:30],'readme':(p/'README').read_text() if (p/'README').exists() else None,'build_processes':subprocess.run(['pgrep','-x','go|compile|link|encore'],stdout=subprocess.PIPE).stdout.decode()}))
