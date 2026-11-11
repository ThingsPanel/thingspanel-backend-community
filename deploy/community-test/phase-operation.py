import json, subprocess

query="""BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='30s';
SELECT json_build_object(
 'device_shapes',(SELECT json_agg(t) FROM (
  SELECT coalesce(d.protocol,'') protocol,coalesce(d.access_way,'') access_way,
    coalesce(c.protocol_type,'') template_protocol,coalesce(c.voucher_type,'') voucher_type,
    count(*) devices,count(*) FILTER(WHERE d.is_enabled IN ('enable','enabled')) enabled,
    count(*) FILTER(WHERE coalesce(d.voucher,'')<>'') with_credential,
    count(*) FILTER(WHERE coalesce(d.service_access_id,'')<>'') service_bound
  FROM devices d LEFT JOIN device_configs c ON c.id=d.device_config_id
  GROUP BY 1,2,3,4 ORDER BY count(*) DESC)t),
 'template_shapes',(SELECT json_agg(t) FROM (
  SELECT coalesce(protocol_type,'') protocol,device_type,count(*) templates,
   count(*) FILTER(WHERE coalesce(device_template_id,'')<>'') old_model_bound,
   count(*) FILTER(WHERE coalesce(native_thing_model_snapshot_id,'')<>'') native_snapshot_bound
  FROM device_configs GROUP BY 1,2 ORDER BY count(*) DESC)t),
 'service_access_count',(SELECT count(*) FROM service_access),
 'model_counts',(SELECT json_build_object(
   'telemetry',(SELECT count(*) FROM device_model_telemetry),
   'attributes',(SELECT count(*) FROM device_model_attributes),
   'events',(SELECT count(*) FROM device_model_events),
   'commands',(SELECT count(*) FROM device_model_commands),
   'custom_commands',(SELECT count(*) FROM device_model_custom_commands)))
);ROLLBACK;"""
p=subprocess.run(['docker','exec','-i','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1','-U','postgres','-d','ThingsPanel'],input=query.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=60)
if p.returncode:raise RuntimeError('Read-only upgrade coverage inventory failed')
print(json.dumps(json.loads(p.stdout)))
