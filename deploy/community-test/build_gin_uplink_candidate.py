"""Build-only Gin composition: immutable efb source recipe + command compatibility + uplink.
No server access, runtime restart, credentials or migration execution.
"""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import types

BASE_TOOL='efb3f95f45310206fa484efb59402eb4bf7aa62f'
COMPAT_SOURCE='34d6e18971f15b74c0b02fb7f69fae2fc87f876a'
COMPAT_BASE='47405bfd51eba9f436231744de9a123d4278152d'
UPLINK_SOURCE='7e2407913250aa231e162c4d97f028f27c52d1b0'
UPLINK_PARENT='9766c01000960cd8ef66a9f1eb2a0bfe2a02328f'
OWNER_SOURCE='cbb627f5423919e09ba9c1442d382a869b7cb3d1'
OWNER_PARENT='1334f9a120b1d90b0a4fca7ffc09576f35c05121'
COMPAT_FILES=('internal/brokersemantic/command.go','internal/brokersemantic/command_test.go','internal/service/native_device_commands.go','internal/service/native_device_commands_test.go')
COMPAT_ENCORE=('encore-api/thingmodel/core/command.go','encore-api/thingmodel/app/command_namespace_test.go')
UPLINK_FILES=('internal/app/application.go',
 'internal/app/broker_ingress.go',
 'internal/app/uplink.go',
 'internal/app/uplink_contract.go',
 'internal/app/uplink_contract_test.go',
 'internal/brokerledger/prepared_postgres_test.go',
 'internal/brokerledger/store.go',
 'internal/brokersemantic/testdata/uplink-core-parity.json',
 'internal/brokersemantic/uplink.go',
 'internal/brokersemantic/uplink_scalar.go',
 'internal/brokersemantic/uplink_test.go',
 'internal/diagnostics/collector.go',
 'internal/diagnostics/logical.go',
 'internal/diagnostics/logical_redis_test.go',
 'internal/diagnostics/metrics.go',
 'internal/diagnostics/types.go',
 'internal/storage/direct_model_binding.go',
 'internal/storage/direct_writer.go',
 'internal/storage/telemetry_broker.go',
 'internal/storage/telemetry_model_binding.go',
 'internal/storage/telemetry_model_binding_postgres_test.go',
 'internal/storage/telemetry_writer.go',
 'internal/storage/types.go',
 'internal/uplink/attribute.go',
 'internal/uplink/event.go',
 'internal/uplink/observations.go',
 'internal/uplink/prepared_bus.go',
 'internal/uplink/prepared_bus_postgres_test.go',
 'internal/uplink/reliable.go',
 'internal/uplink/reliable_prepared.go',
 'internal/uplink/reliable_prepared_postgres_test.go',
 'internal/uplink/storage_representation.go',
 'internal/uplink/telemetry.go',
 'internal/uplinkbinding/binding.go',
 'internal/uplinkbinding/binding_test.go',
 'internal/uplinkcontract/cache.go',
 'internal/uplinkcontract/client.go',
 'internal/uplinkcontract/client_test.go')
OWNER_FILES=('encore-api/thingmodel/owner_uplink_contract.go',
 'encore-api/thingmodel/owner_uplink_contract_test.go',
 'encore-api/thingmodel/owner_uplink_parity_test.go',
 'encore-api/thingmodel/testdata/uplink-core-parity.json')

def git(repo,*args):
    result=subprocess.run(['git','-C',str(repo),*args],stdout=subprocess.PIPE,stderr=subprocess.PIPE)
    if result.returncode:
        raise ValueError('Git source operation failed: '+result.stderr.decode(errors='replace')[-1200:])
    return result.stdout

def digest(raw):
    return hashlib.sha256(raw).hexdigest()

def baseline_tool(repo):
    raw=git(repo,'show',BASE_TOOL+':deploy/community-test/build_gin_integrated_candidate.py')
    module=types.ModuleType('immutable_efb_composer')
    module.__file__=str(Path(repo)/'deploy/community-test/build_gin_integrated_candidate.py')
    exec(compile(raw,module.__file__,'exec'),module.__dict__)
    return module, digest(raw)

def verify_source_scope(repo):
    for sha in (BASE_TOOL,COMPAT_BASE,COMPAT_SOURCE,UPLINK_PARENT,UPLINK_SOURCE,OWNER_PARENT,OWNER_SOURCE):
        git(repo,'cat-file','-e',sha+'^{commit}')
    git(repo,'merge-base','--is-ancestor',BASE_TOOL,'HEAD')
    for base,source,allowed,check_parent in (
        (COMPAT_BASE,COMPAT_SOURCE,set(COMPAT_FILES)|set(COMPAT_ENCORE),False),
        (UPLINK_PARENT,UPLINK_SOURCE,set(UPLINK_FILES),True),
        (OWNER_PARENT,OWNER_SOURCE,set(OWNER_FILES),True),
    ):
        if check_parent and git(repo,'rev-parse',source+'^').decode().strip()!=base:
            raise ValueError('Reviewed source parent changed')
        changed=set(git(repo,'diff','--name-only',base,source).decode().splitlines())
        if changed!=allowed:
            raise ValueError('Source commit exceeds exact reviewed paths: '+str(sorted(changed^allowed)))
    if set(COMPAT_FILES)&set(UPLINK_FILES):
        raise ValueError('Overlapping deltas require explicit review')

def entries(repo,base,source,allowed):
    result={}
    for name in allowed:
        exists=bool(git(repo,'ls-tree',base,'--',name).strip())
        result[name]=(git(repo,'show',base+':'+name) if exists else None,git(repo,'show',source+':'+name))
    return result

def apply_exact(root,changes,allowed):
    root=Path(root).resolve()
    if set(changes)!=set(allowed):
        raise ValueError('Overlay does not match exact allowed paths')
    # Verify every preimage before the first write. Never normalize unexpected
    # bytes or overwrite a same-named new file from another patch.
    for name,(before,after) in changes.items():
        target=root/name
        if target.is_symlink() or root not in target.resolve().parents:
            raise ValueError('Invalid overlay path')
        if (target.read_bytes() if target.exists() else None)!=before:
            raise ValueError('Overlay baseline mismatch: '+name)
    result=[]
    for name,(before,after) in sorted(changes.items()):
        target=root/name;target.parent.mkdir(parents=True,exist_ok=True);target.write_bytes(after)
        result.append({'path':name,'before_sha256':digest(before) if before is not None else None,'after_sha256':digest(after)})
    return result

def compose(repo,destination):
    repo=Path(repo).resolve();destination=Path(destination).resolve()
    verify_source_scope(repo)
    module,tool_hash=baseline_tool(repo)
    baseline=module.compose(repo,destination)
    source=destination/'source/backend'
    before=module.snapshot(source);broker=module.snapshot(destination/'source/broker')
    compatibility=apply_exact(source,entries(repo,COMPAT_BASE,COMPAT_SOURCE,COMPAT_FILES),COMPAT_FILES)
    after_compat=module.snapshot(source)
    module.verify_only_changes(before,after_compat,COMPAT_FILES)
    uplink=apply_exact(source,entries(repo,UPLINK_PARENT,UPLINK_SOURCE,UPLINK_FILES),UPLINK_FILES)
    after=module.snapshot(source)
    module.verify_only_changes(after_compat,after,UPLINK_FILES)
    changes=module.verify_only_changes(before,after,set(COMPAT_FILES)|set(UPLINK_FILES))
    if module.snapshot(destination/'source/broker')!=broker:
        raise ValueError('Broker source changed')
    profile={
        'profile':'gin-bounded-directory-command-compat-uplink-validation',
        'candidate_commit':git(repo,'rev-parse','HEAD').decode().strip(),
        'base_tool_source':BASE_TOOL,'base_tool_sha256':tool_hash,
        'base_composition':baseline,'base_source_snapshot_sha256':digest(json.dumps(before,sort_keys=True,separators=(',',':')).encode()),
        'command_compat_source':COMPAT_SOURCE,'command_compat_base':COMPAT_BASE,'command_compat_changes':compatibility,
        'uplink_source':UPLINK_SOURCE,'uplink_parent':UPLINK_PARENT,'uplink_changes':uplink,
        'changes':changes,'unchanged_file_count':len(set(before)&set(after))-sum(x['before_sha256'] is not None for x in changes),
        'unchanged_broker_file_count':len(broker),'broker_changed':False,'encore_changed':False,'database_owner_changed':False,
        'anchor_merged':False,'migrations_required':False,
        'required_encore_contract':'authenticated POST /internal/thingmodel/validate-command and /internal/thingmodel/uplink-contract',
        'required_encore_uplink_source':OWNER_SOURCE,
        'required_encore_uplink_files':{name:digest(git(repo,'show',OWNER_SOURCE+':'+name)) for name in OWNER_FILES},
        'required_encore_command_compat_files':{name:digest(git(repo,'show',COMPAT_SOURCE+':'+name)) for name in COMPAT_ENCORE},
        'deployment_order':['Verify combined Encore owner contract and immutable source pins','Deploy only Gin binary preserving existing environment','Run fresh native uplink and diagnostics acceptance'],
        'rollback_order':['Restore prior Gin binary before removing Encore owner contracts'],
    }
    (destination/'SOURCE_PROVENANCE.json').write_text(json.dumps(profile,indent=2),encoding='utf-8')
    (destination/'SOURCE_FILES.json').write_text(json.dumps(after,indent=2),encoding='utf-8')
    return profile

if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('--repo',default=str(Path(__file__).resolve().parents[2]))
    sub=p.add_subparsers(dest='operation',required=True)
    c=sub.add_parser('compose');c.add_argument('destination')
    b=sub.add_parser('package');b.add_argument('source');b.add_argument('binary');b.add_argument('output');b.add_argument('commit')
    a=p.parse_args()
    if a.operation=='compose':
        result=compose(a.repo,a.destination)
        print(json.dumps({'candidate_commit':result['candidate_commit'],'changes':len(result['changes']),'uplink_source':result['uplink_source']}))
    else:
        module,_=baseline_tool(a.repo)
        print(json.dumps(module.package(a.source,a.binary,a.output,a.commit)))
