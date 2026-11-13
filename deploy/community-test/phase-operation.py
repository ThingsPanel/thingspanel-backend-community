"""Read-only rollout probe: model validation POSTs never admit device commands.

Run on the server only after the new Encore image is active and before Gin
enforces the new contract. Secret values are read in memory and never emitted.
"""
import argparse
import hmac
import json
import os
import pathlib
import subprocess
import urllib.error
import urllib.parse
import urllib.request

DEVICE = "c13ad287-76b1-38cd-b16e-946b37e8c88e"
CONFIG = "9a151988-61bb-307c-ec2e-ff06b8e6393b"
MODEL = "ce4e7118-1b14-42d4-ba72-861f96a59276"
SNAPSHOT = "6d332a51-07f8-4a6e-8863-a4a432ded976"
ENCORE = "tp-community-main-47-encore-api-1"
EXECUTABLE = "/home/thingspanel/thingspanel-backend-community/thingspanel-backend-community"
OTHER_QA_TENANT = "ab4fa6e9"


def checked_environment(pid, expected_image):
    if os.readlink("/proc/{}/exe".format(pid)) != EXECUTABLE:
        raise RuntimeError("Gin executable changed")
    env = dict(part.split("=", 1) for part in pathlib.Path("/proc/{}/environ".format(pid)).read_text().split("\0") if "=" in part)
    result = subprocess.run(["docker", "inspect", ENCORE], stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=10)
    if result.returncode:
        raise RuntimeError("Encore inventory unavailable")
    container = json.loads(result.stdout)[0]
    if container["Image"] != expected_image or not container["State"]["Running"]:
        raise RuntimeError("Encore image or running state changed")
    other = dict(item.split("=", 1) for item in container["Config"]["Env"] if "=" in item)
    token = env.get("GOTP_AUTOMATION_TOKEN", "").strip()
    deployment = env.get("GOTP_AUTOMATION_DEPLOYMENT_ID", "").strip()
    if len(token) < 32 or not hmac.compare_digest(token, other.get("TP_AUTOMATION_SERVICE_TOKEN", "").strip()):
        raise RuntimeError("Owner identity mismatch")
    if not deployment or not hmac.compare_digest(deployment, other.get("TP_SOURCE_DEPLOYMENT_ID", "").strip()):
        raise RuntimeError("Owner deployment mismatch")
    origin = env.get("TP_THINGMODEL_NATIVE_URL", "").rstrip("/")
    if origin != "http://127.0.0.1:14000":
        raise RuntimeError("Model owner origin differs from approved direct route")
    return origin, token, deployment, container["Id"]


def read_fixture():
    query = """BEGIN READ ONLY;
SELECT row_to_json(x) FROM (
 SELECT d.id AS device_id,d.tenant_id,c.id AS config_id,
 c.native_thing_model_id AS model_id,c.native_thing_model_snapshot_id AS snapshot_id,
 c.native_thing_model_revision AS revision,c.native_binding_revision AS binding_revision
 FROM devices d JOIN device_configs c ON c.id=d.device_config_id AND c.tenant_id=d.tenant_id
 WHERE d.id='c13ad287-76b1-38cd-b16e-946b37e8c88e'
 AND c.id='9a151988-61bb-307c-ec2e-ff06b8e6393b'
 AND d.is_enabled='enabled' AND d.activate_flag='active'
) x; COMMIT;"""
    result = subprocess.run(["docker", "exec", "-i", "timescaledb", "psql", "-X", "-q", "-At", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", "ThingsPanel"], input=query.encode(), stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=10)
    if result.returncode:
        raise RuntimeError("QA binding read failed")
    rows = [line for line in result.stdout.decode().splitlines() if line.strip()]
    if len(rows) != 1:
        raise RuntimeError("QA fixture is absent or not unique")
    pins = json.loads(rows[0])
    if pins["device_id"] != DEVICE or pins["config_id"] != CONFIG or pins["model_id"] != MODEL or pins["snapshot_id"] != SNAPSHOT or pins["revision"] != 3 or pins["binding_revision"] < 1:
        raise RuntimeError("QA fixture binding changed; review before probing")
    if not pins["tenant_id"] or pins["tenant_id"] == OTHER_QA_TENANT:
        raise RuntimeError("QA tenant precondition changed")
    return pins


def http_post(origin, headers, payload):
    request = urllib.request.Request(origin + "/internal/thingmodel/validate-command", data=json.dumps(payload).encode(), headers=headers, method="POST")
    # Never forward an owner credential across a redirect.
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, req, fp, code, msg, headers, newurl):
            return None
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    try:
        with opener.open(request, timeout=5) as response:
            raw = response.read(4097)
            if len(raw) > 4096:
                raise RuntimeError("Unexpected oversized owner response")
            return response.status, json.loads(raw)
    except urllib.error.HTTPError as error:
        return error.code, None


def probe(origin, token, deployment, pins, transport=http_post):
    headers = {"Content-Type": "application/json", "Authorization": "Bearer " + token,
               "X-Automation-Tenant": pins["tenant_id"], "X-Automation-Deployment": deployment}
    payload = {key: value for key, value in pins.items() if key != "tenant_id"}
    payload.update(identifier="set_switch", params={"enabled": True, "mode": "manual"})
    cases = [
        ("valid_pinned_command", {}, {}, 200),
        ("missing_required_parameters", {}, {"params": {}}, 400),
        ("wrong_boolean_type", {}, {"params": {"enabled": "true", "mode": "manual"}}, 400),
        ("unknown_command", {}, {"identifier": "qa_undefined_command"}, 400),
        ("other_qa_tenant", {"X-Automation-Tenant": OTHER_QA_TENANT}, {}, 404),
        ("anonymous", {"Authorization": ""}, {}, 401),
    ]
    output = []
    for name, header_change, body_change, expected in cases:
        actual_headers = dict(headers); actual_headers.update(header_change)
        actual_body = dict(payload); actual_body.update(body_change)
        status, result = transport(origin, actual_headers, actual_body)
        passed = status == expected
        if expected == 200:
            passed = passed and result == {"valid": True, "device_id": DEVICE, "snapshot_id": SNAPSHOT, "binding_revision": pins["binding_revision"]}
        output.append({"case": name, "http_status": status, "expected": expected, "pass": passed})
    return output



import hashlib,re,fcntl
STAGE=pathlib.Path('/opt/tp-community-actions/q04-q09-encore-c3fd-ci43366233046')
IMAGE='sha256:65331c77f28c13a98ecca2385db0f4508da8e62f4e88090fae7fe9cfa1f86a63'
CANDIDATE='c3fd6ec0e89c38a305c1adbd92e7de7015fbc424'
def uplink(origin,token,deployment,pins):
    base={'Content-Type':'application/json','Authorization':'Bearer '+token,'X-Automation-Tenant':pins['tenant_id'],'X-Automation-Deployment':deployment}
    payload={k:pins[k] for k in ('model_id','snapshot_id','revision')}
    class NR(urllib.request.HTTPRedirectHandler):
        def redirect_request(self,*args,**kwargs):return None
    opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),NR())
    cases=[('valid_published_snapshot',{}, {},200),('other_tenant',{'X-Automation-Tenant':OTHER_QA_TENANT},{},404),('anonymous',{'Authorization':''},{},401),('wrong_service_identity',{'Authorization':'Bearer qa-invalid'},{},401),('wrong_deployment',{'X-Automation-Deployment':'qa-invalid'},{},401),('missing_tenant',{'X-Automation-Tenant':''},{},400),('snapshot_mismatch',{}, {'snapshot_id':'00000000-0000-4000-8000-000000000000'},409),('zero_revision',{}, {'revision':0},400),('unknown_draft_selector',{}, {'draft':True},400)]
    rows=[]
    for name,h,b,want in cases:
        headers=dict(base);headers.update(h);body=dict(payload);body.update(b)
        req=urllib.request.Request(origin+'/internal/thingmodel/uplink-contract',data=json.dumps(body).encode(),headers=headers,method='POST')
        try:
            with opener.open(req,timeout=5) as r:status=r.status;raw=r.read(1048577);cache=r.headers.get('Cache-Control')
        except urllib.error.HTTPError as e:status=e.code;raw=e.read(1048577);cache=None
        ok=status==want and len(raw)<=1048576
        detail={}
        if status==200:
            value=json.loads(raw);text=raw.decode();match=re.search(r'"model"\s*:',text)
            assert match
            start=match.end()
            while text[start].isspace():start+=1
            model,end=json.JSONDecoder().raw_decode(text,start)
            digest=hashlib.sha256(text[start:end].encode()).hexdigest()
            ok=ok and set(value)=={'schema_version','tenant_id','model_id','snapshot_id','revision','contract_sha256','model'} and value['schema_version']==1 and value['tenant_id']==pins['tenant_id'] and all(value[k]==payload[k] for k in payload) and value['contract_sha256']==digest and cache=='no-store'
            detail={'contract_sha256':digest,'pins_match':all(value[k]==payload[k] for k in payload),'cache_control':cache}
        rows.append({'case':name,'http_status':status,'expected':want,'pass':bool(ok),**detail})
    return rows

def execute():
    with pathlib.Path('/opt/tp-community-actions/deployment.lock').open() as lock:
        fcntl.flock(lock,fcntl.LOCK_SH|fcntl.LOCK_NB)
        state=json.loads((STAGE/'state.private.json').read_text());applied=json.loads((STAGE/'applied.private.json').read_text())
        assert state['tooling_commit']==CANDIDATE and state['image_id']==IMAGE and applied['image_id']==IMAGE
        pid=state['spec']['gin']['pid'];origin,token,deployment,cid=checked_environment(pid,IMAGE)
        assert cid==applied['container_id']
        pins=read_fixture();command=probe(origin,token,deployment,pins);up=uplink(origin,token,deployment,pins)
        _,_,_,again=checked_environment(pid,IMAGE);assert again==cid
        result={'schema_version':1,'candidate':CANDIDATE,'image_id':IMAGE,'container_id':cid,'gin_pid':pid,'read_only':True,'command_admission':False,'fixture_writes':0,'command_checks':command,'uplink_checks':up,'pass':all(x['pass'] for x in command+up)}
        if result['pass']:
            fd=os.open(str(STAGE/'owner-acceptance.json'),os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
            with os.fdopen(fd,'w') as f:json.dump(result,f);f.flush();os.fsync(f.fileno())
        output=dict(result)
        if result['pass']:output['receipt_sha256']=hashlib.sha256((STAGE/'owner-acceptance.json').read_bytes()).hexdigest()
        print(json.dumps(output))
        return 0 if result['pass'] else 1
if __name__=='__main__':raise SystemExit(execute())
