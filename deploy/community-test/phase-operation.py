import pathlib,json,subprocess,tempfile,os
out={};p=pathlib.Path('/opt/tp-community-main-20261028/ha-runtime/ha-demo.credentials.private.json');x=json.loads(p.read_text());out['ha_keys']=list(x)
for k,v in x.items():
 if isinstance(v,str) and ('url' in k.lower() or k.lower()=='base'):out[k]=v
for name in ['qa-tenant.private.json','qa-platform-admin.private.json']:
 p=pathlib.Path('/opt/tp-community-main-20261028')/name
 if p.exists():
  v=json.loads(p.read_text());out[name]={'keys':list(v),'email':v.get('email')}
d=pathlib.Path(tempfile.mkdtemp(prefix='qa-q08-session-'));os.chmod(str(d),0o700)
subprocess.run(['openssl','genpkey','-algorithm','RSA','-pkeyopt','rsa_keygen_bits:2048','-out',str(d/'private.pem')],stdout=subprocess.PIPE,stderr=subprocess.PIPE,check=True);os.chmod(str(d/'private.pem'),0o600)
pub=subprocess.check_output(['openssl','pkey','-in',str(d/'private.pem'),'-pubout']);out['public_key']=pub.decode();out['key_directory']=str(d)
print(json.dumps(out))
