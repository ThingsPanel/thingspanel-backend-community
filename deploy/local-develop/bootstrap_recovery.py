"""Explicit recovery of this fixture's rolled-back FIRST SQL transaction only."""
import json

RELATIONS = {('casbin_rule','r'),('local_fixture_owner','r'),('sys_version','r'),
             ('casbin_rule_pkey','i'),('local_fixture_owner_pkey','i'),('sys_version_pkey','i'),('casbin_rule_id_seq','S')}
COLUMNS = {('casbin_rule','id','bigint',True),('local_fixture_owner','runtime_id','text',True),
           ('sys_version','version_number','integer',True),('sys_version','version','character varying(255)',True)} | {
           ('casbin_rule',field,'character varying(100)',False) for field in ['ptype','v0','v1','v2','v3','v4','v5']}


def validate_container(info, root, project, image):
    labels=info['Config']['Labels']
    if labels.get('com.docker.compose.project')!=project or labels.get('com.docker.compose.service')!='postgres' or labels.get('com.docker.compose.project.working_dir')!=str(root) or info['Image']!=image:
        raise ValueError('Postgres project/service/image ownership mismatch')
    expected={('/var/lib/postgresql/data',str(root/'postgres')),('/run/secrets/postgres-password',str(root/'private/postgres-password'))}
    actual={(m['Destination'],m['Source']) for m in info['Mounts'] if m['Type']=='bind'}
    if actual!=expected or len(info['Mounts'])!=2:
        raise ValueError('Postgres mounts are not exclusively this fixture data/secret')
    if info['NetworkSettings']['Ports'].get('5432/tcp')!=[{'HostIp':'127.0.0.1','HostPort':'15555'}]:
        raise ValueError('Postgres loopback binding mismatch')


def validate(snapshot):
    if snapshot['database'] != 'ThingsPanel' or not snapshot['owner_matches'] or snapshot['casbin_rows'] != 0:
        raise ValueError('Recovery owner/database/control-row mismatch')
    if snapshot['versions'] != [[0,'0.0.0']] or snapshot['native_databases']:
        raise ValueError('Recovery requires exactly initial version zero and absent native databases')
    if set(map(tuple,snapshot['relations'])) != RELATIONS or set(map(tuple,snapshot['columns'])) != COLUMNS:
        raise ValueError('Recovery requires exact original control-table shape and no other relations')
    if snapshot['other_objects'] or snapshot['extra_schemas'] or snapshot['user_triggers'] or snapshot['other_constraints']:
        raise ValueError('Recovery refuses additional database objects: ' + json.dumps({k:snapshot[k] for k in ['other_objects','extra_schemas','user_triggers','other_constraints']}))
    if set(snapshot['extensions']) != {'plpgsql','timescaledb'} or not snapshot['sequence_owned']:
        raise ValueError('Recovery extension/sequence ownership mismatch')
    expected_pks={('casbin_rule','casbin_rule_pkey',(1,)),('sys_version','sys_version_pkey',(1,)),('local_fixture_owner','local_fixture_owner_pkey',(1,))}
    if {(a,b,tuple(c)) for a,b,c in snapshot['primary_keys']} != expected_pks or not snapshot['owners_valid']:
        raise ValueError('Recovery control key/owner mismatch')
    if snapshot['defaults'] != [["casbin_rule","id","nextval('casbin_rule_id_seq'::regclass)"]]:
        raise ValueError('Recovery control default expression mismatch')
    if not snapshot['listen_addresses']:
        raise ValueError('Temporary socket-only PostgreSQL is not a recovery target')


def inspect(root, runtime, manifest):
    if (root/'migration-ledger.json').exists() or (root/'bootstrap.complete').exists():
        raise ValueError('Recovery refuses any previously recorded migration or completed bootstrap')
    nonce = manifest.get('runtime_id','')
    if len(nonce)!=64 or any(c not in '0123456789abcdef' for c in nonce):
        raise ValueError('Invalid fixture nonce')
    # Extension membership plus internal/automatic children are the only allowed
    # non-control objects. Public namespace dependencies catch routines/types,
    # operators/collations/etc; explicit checks also cover table-attached objects.
    query = r"""
WITH RECURSIVE ext_owned(classid,objid,objsubid) AS (
 SELECT d.classid,d.objid,d.objsubid FROM pg_depend d JOIN pg_extension e ON d.refclassid='pg_extension'::regclass AND d.refobjid=e.oid WHERE d.deptype='e' AND e.extname IN ('plpgsql','timescaledb')
 UNION
 SELECT d.classid,d.objid,d.objsubid FROM pg_depend d JOIN ext_owned e ON d.refclassid=e.classid AND d.refobjid=e.objid WHERE d.deptype IN ('i','a')
), controls AS (SELECT oid,reltype FROM pg_class WHERE relnamespace='public'::regnamespace AND relname IN ('casbin_rule','local_fixture_owner','sys_version'))
SELECT json_build_object(
 'database',current_database(),'listen_addresses',current_setting('listen_addresses'),
 'owner_matches',(SELECT count(*)=1 AND bool_and(runtime_id='NONCE') FROM local_fixture_owner),
 'versions',(SELECT json_agg(json_build_array(version_number,version) ORDER BY version_number) FROM sys_version),
 'casbin_rows',(SELECT count(*) FROM casbin_rule),
 'extensions',(SELECT json_agg(extname ORDER BY extname) FROM pg_extension),
 'native_databases',(SELECT coalesce(json_agg(datname),'[]') FROM pg_database WHERE datname IN ('device_metadata','automation','notification','file_storage','connector_runtime')),
 'relations',(SELECT json_agg(json_build_array(relname,relkind) ORDER BY relname) FROM pg_class c WHERE relnamespace='public'::regnamespace AND NOT EXISTS(SELECT 1 FROM ext_owned e WHERE e.classid='pg_class'::regclass AND e.objid=c.oid)),
 'columns',(SELECT json_agg(json_build_array(c.relname,a.attname,format_type(a.atttypid,a.atttypmod),a.attnotnull) ORDER BY c.relname,a.attnum) FROM pg_class c JOIN pg_attribute a ON a.attrelid=c.oid WHERE c.oid IN (SELECT oid FROM controls) AND a.attnum>0 AND NOT a.attisdropped),
 'owners_valid',(SELECT bool_and(c.relowner=(SELECT oid FROM pg_roles WHERE rolname='postgres')) FROM pg_class c WHERE c.oid IN (SELECT oid FROM controls)),
 'primary_keys',(SELECT json_agg(json_build_array(c.relname,k.conname,k.conkey) ORDER BY c.relname) FROM pg_constraint k JOIN pg_class c ON c.oid=k.conrelid WHERE k.conrelid IN (SELECT oid FROM controls) AND k.contype='p'),
 'defaults',(SELECT coalesce(json_agg(json_build_array(c.relname,a.attname,pg_get_expr(d.adbin,d.adrelid)) ORDER BY c.relname,a.attnum),'[]') FROM pg_attrdef d JOIN pg_class c ON c.oid=d.adrelid JOIN pg_attribute a ON a.attrelid=c.oid AND a.attnum=d.adnum WHERE c.oid IN (SELECT oid FROM controls)),
 'sequence_owned',EXISTS(SELECT 1 FROM pg_depend d WHERE d.classid='pg_class'::regclass AND d.objid=to_regclass('public.casbin_rule_id_seq') AND d.refobjid=to_regclass('public.casbin_rule') AND d.deptype='a' AND d.refobjsubid=(SELECT attnum FROM pg_attribute WHERE attrelid=to_regclass('public.casbin_rule') AND attname='id')),
 'other_objects',(SELECT coalesce(json_agg(DISTINCT pg_describe_object(d.classid,d.objid,d.objsubid)),'[]') FROM pg_depend d WHERE d.refclassid='pg_namespace'::regclass AND d.refobjid='public'::regnamespace AND NOT EXISTS(SELECT 1 FROM ext_owned e WHERE e.classid=d.classid AND e.objid=d.objid) AND NOT (d.classid='pg_class'::regclass AND d.objid IN (SELECT oid FROM pg_class WHERE relnamespace='public'::regnamespace)) AND NOT (d.classid='pg_extension'::regclass AND d.objid IN (SELECT oid FROM pg_extension WHERE extname IN ('plpgsql','timescaledb'))) AND NOT (d.classid='pg_type'::regclass AND d.objid IN (SELECT oid FROM pg_type WHERE typrelid IN (SELECT oid FROM controls) OR typelem IN (SELECT reltype FROM controls)))),
 'extra_schemas',(SELECT coalesce(json_agg(nspname),'[]') FROM pg_namespace n WHERE nspname NOT LIKE 'pg_%' AND nspname NOT IN ('public','information_schema') AND NOT EXISTS(SELECT 1 FROM ext_owned e WHERE e.classid='pg_namespace'::regclass AND e.objid=n.oid)),
 'user_triggers',(SELECT count(*) FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid WHERE c.relnamespace='public'::regnamespace AND NOT t.tgisinternal AND NOT EXISTS(SELECT 1 FROM ext_owned e WHERE e.classid='pg_trigger'::regclass AND e.objid=t.oid)),
 'other_constraints',(SELECT count(*) FROM pg_constraint WHERE conrelid IN (SELECT oid FROM controls) AND contype<>'p')
);
""".replace('NONCE',nonce)
    snapshot=json.loads(runtime.sql(root,'ThingsPanel',query))
    validate(snapshot)
    return snapshot
