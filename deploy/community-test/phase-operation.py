#!/usr/bin/env python3
"""Stage signed HTTP candidate only: no load, activate, restart, API write, or live config edit."""
import argparse, fcntl, hashlib, io, json, os, pathlib, shutil, subprocess, tarfile, zipfile
EXPECTED = {'agent_id': '251ea9ea56ae048c6728f9407056d56b13b8bdf6cfb803e617acc532a46a08df', 'http_id': '407aea5e5f19a7ab99fe23e7b80b76a749860b2a315cab12442b180915a53c73', 'old_image': 'sha256:ae73719fae5eb291f098ccf1dbea5bc23092969b8ef8c8f2175c1fc319926a9f', 'old_catalog_sha': 'a2d85aaeaedd716feb61bfa44af2d219b5b0caf3281c0d9be553589f8237645f', 'old_sig_sha': '8866a0bec0cb8187d77894f784c91092bed08f0c8e343c4fcffefb5b02631cc1', 'catalog_path': '/opt/tp-community-main-20261028/ha-runtime/bundle-public/plugins/plugin-catalog.json', 'trust_path': '/opt/tp-community-main-20261028/ha-runtime/installer/config/native/plugin-trust.pem', 'trust_sha': '2e6b4e5f748a6145ebadf228471da257c578cff498fb0a6ea7b899eed1aeb7f3', 'agent_path': '/opt/tp-community-main-20261028/ha-runtime/bundle-public/bin/tp-agent', 'agent_sha': 'fbae4ca150617ea761ed27ce487be62264a01f14ccbff4b64b189ed2604200ca', 'state_path': '/opt/tp-community-main-20261028/ha-runtime/state/state.json', 'new_image': 'sha256:606ddaad2557e909808aeb1d12c67d947d9923357fcf97633468784459914510', 'new_catalog_sha': '94e7f34e7850d13eee294a72b072910c34044acf3e3943297e48adc61dbb2d0f', 'new_zip_sha': '72fb293bf0d3c8c56ab2d12ae0781e514b451787b494a4e271666adc74dc2498', 'source': '247579be25b7125dd2f9a5c7b4f975e56ccdb695', 'workflow': 'bd639a0c7484ea70bfd3a90a2b5cbb6a7d125229'}
FILES = {'catalog-linux-amd64.json','catalog-linux-amd64.json.sig','http-image-linux-amd64.tar','ha-image-linux-amd64.tar','tp-agent-linux-amd64','provenance-linux-amd64.json'}

def digest(path):
    return hashlib.sha256(pathlib.Path(path).read_bytes()).hexdigest()

def inspect(name):
    return json.loads(subprocess.check_output(['docker','inspect',name],universal_newlines=True,timeout=30))[0]

def require(ok, message):
    if not ok: raise RuntimeError(message)

def verify_signature(catalog, signature):
    p=subprocess.run(['openssl','dgst','-sha256','-verify',EXPECTED['trust_path'],'-signature',str(signature),str(catalog)],stdout=subprocess.PIPE,stderr=subprocess.PIPE,universal_newlines=True,timeout=15)
    require(p.returncode==0,'signature verification failed')

def baseline():
    a=inspect('tp-main47-ha-agent');h=inspect('tp-agent-prepared-ac01d43ee3f1859518c788843b51690757b1fdc7')
    require(a['Id']==EXPECTED['agent_id'] and a['State']['Running'],'Agent baseline changed')
    require(h['Id']==EXPECTED['http_id'] and h['Image']==EXPECTED['old_image'] and h['State']['Running'],'HTTP baseline changed')
    for path,key in [(EXPECTED['catalog_path'],'old_catalog_sha'),(EXPECTED['catalog_path']+'.sig','old_sig_sha'),(EXPECTED['trust_path'],'trust_sha'),(EXPECTED['agent_path'],'agent_sha')]:
        require(digest(path)==EXPECTED[key],'baseline file changed: '+path)
    state=json.loads(pathlib.Path(EXPECTED['state_path']).read_text())
    held={(x.get('instance'),x.get('generation')) for x in state.get('requests',{}).values() if x.get('state')=='held'}
    require(held=={('homeassistant',2),('http-adapter',3)},'active generation changed')
    return {'agent_id':a['Id'],'http_id':h['Id'],'http_generation':3,'ha_generation':2}

def main():
    ap=argparse.ArgumentParser();ap.add_argument('--bundle',required=True);ap.add_argument('--stage',required=True);args=ap.parse_args()
    stage=pathlib.Path(args.stage).resolve();root=pathlib.Path('/opt/tp-community-actions/http-expiry-releases')
    require(stage.parent==root and stage.name=='signed-bd639a0','unexpected stage directory')
    lock=open('/opt/tp-community-actions/deployment.lock','r');fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
    before=baseline()
    require(shutil.disk_usage('/opt').free>256*1024*1024,'insufficient staging disk')
    require(digest(args.bundle)==EXPECTED['new_zip_sha'],'artifact zip digest mismatch')
    with zipfile.ZipFile(args.bundle) as archive:
        require(len(archive.infolist())==len(FILES) and set(archive.namelist())==FILES,'unexpected bundle members')
        require(sum(i.file_size for i in archive.infolist())<100*1024*1024,'bundle oversized')
        root.mkdir(mode=0o700,exist_ok=True);stage.mkdir(mode=0o700)
        for name in sorted(FILES):
            with open(str(stage/name),'xb') as f: f.write(archive.read(name))
    cpath=stage/'catalog-linux-amd64.json';sig=stage/'catalog-linux-amd64.json.sig'
    require(digest(cpath)==EXPECTED['new_catalog_sha'],'candidate catalog digest mismatch')
    verify_signature(cpath,sig)
    c=json.loads(cpath.read_text());old=json.loads(pathlib.Path(EXPECTED['catalog_path']).read_text())
    require(c['version']=='1.2.1' and c['target_platform']=='linux/amd64','candidate version/platform mismatch')
    require(c['agent_sha256']==old['agent_sha256']==digest(stage/'tp-agent-linux-amd64'),'Agent changed')
    byid={x['id']:x for x in c['plugins']};oldbyid={x['id']:x for x in old['plugins']}
    require(set(byid)==set(oldbyid)=={'http-adapter','homeassistant'},'plugin set changed')
    require(byid['homeassistant']==oldbyid['homeassistant'],'HA entry changed')
    http=byid['http-adapter'];require(http['source_commit']==EXPECTED['source'] and http['image_id']==EXPECTED['new_image'],'HTTP source/image mismatch')
    for ident,file in [('http-adapter','http-image-linux-amd64.tar'),('homeassistant','ha-image-linux-amd64.tar')]:
        require(digest(stage/file)==byid[ident]['image_archive_sha256'],'archive mismatch')
    with tarfile.open(str(stage/'http-image-linux-amd64.tar')) as tar:
        m=json.load(tar.extractfile('manifest.json'));require(len(m)==1,'unexpected image count')
        cfg=tar.extractfile(m[0]['Config']).read();require('sha256:'+hashlib.sha256(cfg).hexdigest()==EXPECTED['new_image'],'image config mismatch')
    provenance=json.loads((stage/'provenance-linux-amd64.json').read_text())
    require(provenance['source_commit']==EXPECTED['source'] and provenance['workflow_commit']==EXPECTED['workflow'] and provenance['catalog_sha256']==EXPECTED['new_catalog_sha'],'provenance mismatch')
    baseline()
    for src,dst in [(EXPECTED['catalog_path'],'previous-catalog.json'),(EXPECTED['catalog_path']+'.sig','previous-catalog.json.sig')]:
        with open(str(stage/dst),'xb') as f: f.write(pathlib.Path(src).read_bytes())
    verify_signature(stage/'previous-catalog.json',stage/'previous-catalog.json.sig')
    result={'staged':True,'activated':False,'stage':str(stage),'baseline':before,'expected':EXPECTED,'artifact_sha256':digest(args.bundle),'candidate_source':http['source_commit'],'candidate_image':http['image_id'],'signature':'verified','live_config_unchanged':True}
    with open(str(stage/'stage-result.json'),'x') as f: json.dump(result,f,indent=2)
    print(json.dumps(result,indent=2))

if __name__=='__main__':main()
