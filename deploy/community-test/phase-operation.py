import sys
sys.argv=['probe','--gin-pid','2825348','--encore-image-id','sha256:d284d82d112a0f12c1038aa4dfb7fd2427b38a4c0a49bb6c51078242733e9154']
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


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--gin-pid", type=int, required=True)
    parser.add_argument("--encore-image-id", required=True)
    args = parser.parse_args()
    phase = "environment_guard"
    try:
        origin, token, deployment, container = checked_environment(args.gin_pid, args.encore_image_id)
        phase = "fixture_read"
        pins = read_fixture()
        phase = "contract_probe"
        cases = probe(origin, token, deployment, pins)
        result = {"changes": False, "command_admission": False, "secrets_exported": False,
                  "encore_id": container, "image_id": args.encore_image_id, "fixture": pins,
                  "cases": cases, "pass": all(item["pass"] for item in cases)}
        print(json.dumps(result))
        return 0 if result["pass"] else 1
    except Exception as error:
        # Exception messages from transport/processes may contain URL/config data.
        print(json.dumps({"changes": False, "pass": False, "phase": phase, "error_class": type(error).__name__,
                          "reason": str(error) if type(error) is RuntimeError else "operation failed", "secrets_exported": False}))
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
