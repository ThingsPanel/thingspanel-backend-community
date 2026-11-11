import pathlib,json
result={}
for literal in ['/opt/tp-community-actions/encore-build-cache','/root/.cache/encore-build']:
 p=pathlib.Path(literal)
 result[literal]={'resolved':str(p.resolve()),'entries':[{'name':x.name,'directory':x.is_dir(),'symlink':x.is_symlink()} for x in p.iterdir()]}
print(json.dumps(result))
