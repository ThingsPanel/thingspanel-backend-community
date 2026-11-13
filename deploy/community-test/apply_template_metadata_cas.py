"""Exact stage CAS transformation; never replace stage with main owner files."""
import hashlib
import json
from pathlib import Path

BASE_HASHES = {
 'internal/middleware/cors.go':'360c9d2566f52e6be1d4baf53f61802bee3b9fa2e2fe7b29ac7818cfdf5a17d8',
 'internal/api/device_config.go':'69d85cb625845d2bcfd12d2e97020942fd890a440528b314f06508f7a3a5ca28',
 'internal/service/device_config.go':'aeda279e9d8ee15062c26853815e44d1103287610f71764e9e37297a5accf603',
 'internal/dal/device_config.go':'739ab8ba7cfb1dddace262804e4c2cd4af960412be4f5f6f48d75993c2c4f849',
}
COPIED = [
 'internal/model/device_config_version.go', 'internal/model/device_config_version_test.go',
 'internal/dal/device_config_metadata.go', 'internal/dal/device_config_metadata_postgres_test.go',
 'internal/api/device_config_metadata_test.go',
]

def digest(raw): return hashlib.sha256(raw).hexdigest()
def function(source, signature):
 start=source.index(signature)
 end=source.find('\nfunc ',start+len(signature))
 # Include only the function, excluding the next function's doc comment.
 closing=source.index('\n}',start)+2
 return source[start:closing]

def apply(repo, destination):
 repo,destination=Path(repo),Path(destination)
 for name,sha in BASE_HASHES.items():
  if digest((destination/name).read_bytes())!=sha: raise ValueError('Stage source drift: '+name)
 result=[]
 def write(name,raw):
  target=destination/name
  before=digest(target.read_bytes()) if target.exists() else None
  target.parent.mkdir(parents=True,exist_ok=True);target.write_bytes(raw)
  result.append({'path':name,'before':before,'after':digest(raw)})
 for name in COPIED:
  if (destination/name).exists(): raise ValueError('New stage source already exists: '+name)
  write(name,(repo/name).read_bytes())
 name='internal/api/device_config.go'
 original=(destination/name).read_text(encoding='utf-8');reviewed=(repo/name).read_text(encoding='utf-8')
 patched=original.replace('import (','import (\n\t"errors"\n\t"net/http"\n\t"strings"',1)
 for sig in ['func (*DeviceConfigApi) UpdateDeviceConfig(', 'func (*DeviceConfigApi) HandleDeviceConfigById(']:
  patched=patched.replace(function(original,sig),function(reviewed,sig),1)
 patched+='\n\n'+function(reviewed,'func setDeviceConfigVersion(')+'\n'
 write(name,patched.encode())
 name='internal/service/device_config.go'
 original=(destination/name).read_text(encoding='utf-8');reviewed=(repo/name).read_text(encoding='utf-8')
 new=function(reviewed,'func (*DeviceConfig) UpdateDeviceConfig(')
 # Those mainline ownership errors do not exist in the stage; all original
 # stage checks plus metadata CAS remain, with no d78 helper dependency.
 for sentinel in ['ErrDeviceConfigurationConflict','ErrDeviceBindingOutcomeUnknown','ErrDeviceBindingInvalid']:
  start=new.index('\t\tif errors.Is(err, dal.'+sentinel+') {')
  end=new.index('\n\t\t}',start)+4
  new=new[:start]+new[end:]
 write(name,original.replace(function(original,'func (*DeviceConfig) UpdateDeviceConfig('),new,1).encode())
 name='internal/middleware/cors.go';original=(destination/name).read_text(encoding='utf-8')
 marker='Content-Type, New-Token, New-Expires-At"'
 if original.count(marker)!=1: raise ValueError('Stage CORS boundary changed')
 write(name,original.replace(marker,marker[:-1]+', ETag"').encode())
 name='internal/dal/device_config_metadata_owner.go'
 if (destination/name).exists(): raise ValueError('Stage owner source already exists')
 write(name,(repo/'deploy/community-test/template_metadata_stage_owner.go.txt').read_bytes())
 return {'profile':'template-metadata-cas-stage','owner':'legacy-stage-retained','base_files':BASE_HASHES,'changes':result}

if __name__=='__main__':
 import sys
 print(json.dumps(apply(sys.argv[1],sys.argv[2]),indent=2))
