import json,pathlib,subprocess,collections
source='tp_upgrade_rehearsal_20261111160912'
query="""BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SELECT json_build_object(
'groups',(SELECT json_agg(g) FROM groups g),
'devices',(SELECT json_agg(t) FROM (SELECT d.id,d.tenant_id,d.name,d.label,d.location,d.additional_info,d.sub_device_addr,d.parent_id,d.device_number,d.product_id,d.description,d.batch_number,d.device_config_id,c.device_type,d.voucher,c.voucher_type,d.protocol_config FROM devices d LEFT JOIN device_configs c ON c.id=d.device_config_id)t),
'templates',(SELECT json_agg(t) FROM (SELECT id,tenant_id,name,device_type,protocol_type,protocol_config,device_conn_type,additional_info,other_config,auto_register,device_template_id,native_thing_model_id FROM device_configs)t));ROLLBACK;"""
r=subprocess.run(['docker','exec','-i','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1','-U','postgres','-d',source],input=query.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=90)
if r.returncode:raise RuntimeError('read-only conversion profiling failed')
s=json.loads(r.stdout)
by_name=collections.defaultdict(list)
for g in s['groups']:
 if g['tenant_id']:by_name[(g['tenant_id'],g['parent_id'] if g['parent_id'] not in ('0','',None) else '',g['name'])].append(g['id'])
out={'duplicate_groups':[{'tenant':k[0],'parent':k[1],'name':k[2],'ids':v} for k,v in by_name.items() if len(v)>1],
'groups_missing_timestamps':[g['id'] for g in s['groups'] if g['tenant_id'] and (not g['created_at'] or not g['updated_at'])]}
profiles=collections.defaultdict(collections.Counter)
issues=collections.defaultdict(list)
shapes={}
for field in ('label','location','additional_info','protocol_config','voucher'):
 for d in s['devices']:
  if not d['tenant_id']:continue
  value=d[field]
  if value in (None,'','{}','[]'):profiles[field]['empty']+=1;continue
  try:
   v=json.loads(value) if isinstance(value,str) else value
   if isinstance(v,dict):
    # Credential values never leave the server. Report only known parser fields.
    if field=='voucher':keys=[k for k in sorted(v) if k in ('username','password','client_id','clientId','token','access_token')];kind='object:'+','.join(keys)
    else:kind='object:'+','.join(sorted(v.keys())[:20])
   else:kind=type(v).__name__
  except (ValueError,TypeError):kind='text'
  profiles[field][kind]+=1
  if field=='location' and kind=='text':
   try:
    parts=value.split(',');assert len(parts)==2
    lon,lat=map(float,parts);assert -180<=lon<=180 and -90<=lat<=90
   except (ValueError,AssertionError):issues['unparsed_location'].append(d['id'])
for d in s['devices']:
 if not d['tenant_id']:continue
 if not d['name'] or not d['name'].strip():issues['missing_device_name'].append(d['id'])
 if d['parent_id'] and not d['sub_device_addr']:issues['gateway_missing_address'].append(d['id'])
 if d['parent_id'] and d['device_type'] not in ('2','3'):issues['gateway_invalid_child_kind'].append(d['id'])
 if d['device_config_id'] and d['device_type'] not in ('1','2','3'):issues['unknown_device_kind'].append(d['id'])
out['device_field_shapes']={k:dict(v) for k,v in profiles.items()}
out['device_conversion_issues']={k:{'count':len(v),'ids':v[:30]} for k,v in issues.items()}
out['template_protocols']=dict(collections.Counter((t['protocol_type'] or '<empty>') for t in s['templates'] if t['tenant_id']))
for t in s['templates']:
 for field in ('protocol_config','additional_info','other_config'):
  v=t[field]
  if v in (None,'','{}','[]'):continue
  try:
   value=json.loads(v) if isinstance(v,str) else v;key=','.join(sorted(value)) if isinstance(value,dict) else type(value).__name__
  except (ValueError,TypeError):key='text'
  profiles['template_'+field][key]+=1
out['template_field_shapes']={k:dict(v) for k,v in profiles.items() if k.startswith('template_')}
out['location_review']=[{k:d[k] for k in ('id','name','location')} for d in s['devices'] if d['id'] in issues['unparsed_location']]
out['hierarchy_review']=[{k:d[k] for k in ('id','name','tenant_id','parent_id','device_type','device_config_id','sub_device_addr')} for d in s['devices'] if d['id'] in issues['gateway_invalid_child_kind']]
import re
log=pathlib.Path('/opt/tp-community-actions/upgrade-rehearsal/native-20261111165830/group-conversion-failure.private.log').read_text()
allowed=('invalid source group row','missing source timestamp','verified native schema v40 required','target database does not match verified rehearsal','post-insert full group reconciliation failed','invalid private conversion input','unexpected trailing conversion input','PGDATABASE must match isolated target','group parent is absent or quarantined','zero group or tenant identity','reviewed source group name changed','invalid group name resolution','group hierarchy contains a cycle','target groups differ','conversion provenance differs','conversion audit identity differs','missing conversion provenance','invalid duplicate group exclusion')
out['group_failure']={'categories':[v for v in allowed if v in log],'sqlstate':re.findall(r'SQLSTATE ([0-9A-Z]+)',log),'error_words': [w for w in ('permission denied','relation','does not exist','column','constraint','too long','connection','timeout','cannot assign','unsupported Scan','cannot scan','invalid input syntax') if w in log]}
out['target_group_count']=subprocess.check_output(['docker','exec','timescaledb','psql','-X','-q','-At','-U','postgres','-d','tp_upgrade_rehearsal_20261111165830','-c','SELECT count(*) FROM device_groups']).decode().strip()
print(json.dumps(out,ensure_ascii=False))
