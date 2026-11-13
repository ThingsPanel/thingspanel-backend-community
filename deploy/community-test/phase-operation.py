import pathlib,json
r=pathlib.Path('/opt/tp-community-actions/group-migration/20261113T104323Z')
print(json.dumps({'failure':(r/'63a4b529.private.failure.private.log').read_text()[:1200]}))
