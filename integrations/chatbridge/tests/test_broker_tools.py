from __future__ import annotations

import json
import os
import sys
import tempfile
import unittest
import subprocess
from datetime import datetime, timedelta, timezone
from pathlib import Path
from types import SimpleNamespace
from typing import Any

CHATBRIDGE_SRC = os.environ.get("CHATBRIDGE_SRC", "/Users/junhong/Downloads/code/chatbridge/src")
if Path(CHATBRIDGE_SRC).is_dir() and CHATBRIDGE_SRC not in sys.path:
    sys.path.insert(0, CHATBRIDGE_SRC)

try:
    from chatbridge.runtime import RuntimeAdapterRegistry, RuntimeDispatcher
    from chatbridge.services.installed_skill_registry import EffectiveSkillInstallation
    from chatbridge.skillspec import InstallationStatus, RuntimeKind, SkillInstallation, SkillPackageManifest, SkillScope
    from chatbridge.services.package_storage import PackageStorageService
    from chatbridge.store import SkillVersionRecord
except ImportError as exc:  # The community adapter is optional outside ChatBridge.
    raise unittest.SkipTest("ChatBridge source is not available; set CHATBRIDGE_SRC") from exc

sys.path.insert(0, str(Path(__file__).resolve().parents[1].parent.parent))
from integrations.chatbridge.broker_tools import (  # noqa: E402
    CommunityBrokerToolAdapter,
    GrantBindings,
    MAX_GRANTS,
)
from chatbridge.contracts.types import RuntimeToolCall  # noqa: E402


TENANT = "tenant-a"
ACTOR = "user-a"
THREAD = "thread-a"
DEVICE = "device-a"
TOKEN = "tpai_" + "A" * 43
MANIFEST = {
    "community_broker_contract": "1",
    "tools": [
        {"name": "community_devices_describe", "operation": "devices.describe"},
        {"name": "community_telemetry_query", "operation": "telemetry.query"},
        {"name": "community_commands_preview", "operation": "commands.preview"},
        {"name": "community_commands_get_result", "operation": "commands.getResult"},
    ],
}


class AllowPolicy:
    def authorize_call(self, **_kwargs: Any):
        from chatbridge.contracts.types import AuthorizationResult
        return AuthorizationResult(True)


class CommunityBrokerAdapterTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.client = Path(self.temp.name) / "tp-broker-tools-client"
        self._write_client()
        self.bindings = GrantBindings()
        self.context = self._trusted_context()
        self.grant_id = self.bindings.bind_issued(self._issued(), self._actor_context())
        self.adapter = CommunityBrokerToolAdapter(self.bindings, executable=str(self.client), origin="https://broker-api.example.test")
        self.installation = self._installation()
        registry = RuntimeAdapterRegistry()
        registry.register(self.adapter)
        self.dispatcher = RuntimeDispatcher(registry, policy_evaluator=AllowPolicy())

    def tearDown(self) -> None:
        self.temp.cleanup()

    def _write_client(self, *, mismatch: str = "") -> None:
        script = f'''#!/usr/bin/env python3
import json, sys
request=json.load(sys.stdin)
token=request["token"]
assert token not in sys.argv
device=request["input"]["device_id"]
outer={{"operation":request["operation"],"device_id":device,"tenant_id":"{TENANT}","subject_id":"{ACTOR}","data":{{"status":"ok","operation_echo":request["operation"]}}}}
if {mismatch!r} == "tenant": outer["tenant_id"] = "tenant-b"
if {mismatch!r} == "subject": outer["subject_id"] = "user-b"
if {mismatch!r} == "device": outer["device_id"] = "device-b"
if {mismatch!r} == "operation": outer["operation"] = "commands.preview"
if {mismatch!r} == "echo": outer["data"] = {{"credential": token}}
if {mismatch!r} == "unknown": outer["url"] = "https://attacker.invalid"
if {mismatch!r} == "duplicate":
    print('{{"ok":true,"ok":true}}')
    raise SystemExit(0)
print(json.dumps({{"ok":True,"data":outer}},separators=(",",":")))
'''
        self.client.write_text(script, encoding="utf-8")
        self.client.chmod(0o755)

    def _issued(self, **overrides: Any) -> dict[str, Any]:
        item = {
            "delegation_id": "delegation-1",
            "tenant_id": TENANT,
            "subject_id": ACTOR,
            "token": TOKEN,
            "expires_at": (datetime.now(timezone.utc) + timedelta(seconds=90)).isoformat(),
            "device_ids": [DEVICE],
            "scopes": ["devices.describe", "telemetry.query", "commands.preview", "commands.getResult"],
            "max_calls": 20,
        }
        item.update(overrides)
        return item

    @staticmethod
    def _actor_context() -> dict[str, str]:
        return {"tenant_id": TENANT, "actor_id": ACTOR, "thread_id": THREAD}

    def _trusted_context(self, **overrides: Any) -> dict[str, Any]:
        context = {
            "binding": SimpleNamespace(tenant_id=TENANT, system_user_id=ACTOR),
            "tenant_id": TENANT,
            "actor_id": ACTOR,
            "thread_id": THREAD,
            "community_broker_grant_id": "delegation-1",
        }
        context.update(overrides)
        return context

    def _installation(self, *, runtime: str = "http_tool", manifest: Any = None, scope: SkillScope = SkillScope.TENANT, scope_key: str = TENANT, status: InstallationStatus = InstallationStatus.ENABLED):
        return EffectiveSkillInstallation(
            installation=SkillInstallation("install-1", "thingspanel.community.broker-tools", "1.0.0", scope, scope_key, status),
            version_record=SkillVersionRecord("thingspanel.community.broker-tools", "1.0.0", runtime, manifest if manifest is not None else dict(MANIFEST)),
        )

    def _dispatch(self, name: str, arguments: dict[str, Any], *, context: dict[str, Any] | None = None, installation: Any = None):
        return self.dispatcher.dispatch(
            call=RuntimeToolCall(name, arguments),
            installations=[installation or self.installation],
            request_context=context or self.context,
            actor_context=self._actor_context(),
        )

    def test_dispatcher_executes_fixed_operation_and_never_exposes_token(self):
        surface = self.dispatcher.build_surface([self.installation], actor_context=self._actor_context())
        self.assertEqual({tool.name for tool in surface.tools}, set(MANIFEST["tools"][i]["name"] for i in range(4)))
        self.assertNotIn("token", json.dumps([tool.parameters for tool in surface.tools]))
        result = self._dispatch("community_commands_preview", {"device_id": DEVICE, "identify": "relay.set", "params": {"enabled": True}})
        self.assertTrue(result.ok, result.error)
        self.assertEqual(result.payload["data"], {"status": "ok", "operation_echo": "commands.preview"})
        visible = json.dumps({"payload": result.payload, "audit": result.audit_payload, "error": result.error})
        self.assertNotIn(TOKEN, visible)
        self.assertEqual(result.audit_payload["operation"], "commands.preview")

    def test_all_four_tools_map_only_to_fixed_backend_operations(self):
        vectors = [
            ("community_devices_describe", {"device_id": DEVICE}, "devices.describe"),
            ("community_telemetry_query", {"device_id": DEVICE, "start": "2026-10-20T00:00:00Z", "end": "2026-10-20T01:00:00Z", "keys": ["temp"], "limit": 10}, "telemetry.query"),
            ("community_commands_preview", {"device_id": DEVICE, "identify": "switch.set", "params": {}}, "commands.preview"),
            ("community_commands_get_result", {"device_id": DEVICE, "message_id": "01c9bdb8-4480-7a91-b12e-0147057ab4a1"}, "commands.getResult"),
        ]
        for name, args, expected in vectors:
            with self.subTest(name=name):
                result = self._dispatch(name, args)
                self.assertTrue(result.ok, result.error)
                self.assertEqual(result.payload["data"]["operation_echo"], expected)

    def test_authorization_rejects_cross_actor_thread_device_and_unknown_fields(self):
        vectors = [
            (self._trusted_context(actor_id="user-b", binding=SimpleNamespace(tenant_id=TENANT, system_user_id="user-b")), {"device_id": DEVICE}, "not_authorized"),
            (self._trusted_context(thread_id="thread-b"), {"device_id": DEVICE}, "not_authorized"),
            (self.context, {"device_id": "device-b"}, "not_authorized"),
            (self.context, {"device_id": DEVICE, "tenant_id": TENANT}, "invalid_argument"),
            (self.context, {"device_id": DEVICE, "url": "https://attacker.invalid"}, "invalid_argument"),
            (self.context, {"device_id": DEVICE, "community_broker_grant_id": "attacker"}, "invalid_argument"),
        ]
        for context, args, expected_error in vectors:
            with self.subTest(args=args, context=context.get("thread_id")):
                result = self._dispatch("community_devices_describe", args, context=context)
                self.assertFalse(result.ok)
                self.assertEqual(result.error, expected_error)

    def test_installation_must_be_active_scoped_exact_package_and_fixed_manifest(self):
        invalid = [
            self._installation(runtime="script"),
            self._installation(scope=SkillScope.PLATFORM, scope_key=""),
            self._installation(scope_key="tenant-b"),
            self._installation(status=InstallationStatus.DISABLED),
            self._installation(manifest={**MANIFEST, "url": "https://attacker.invalid"}),
            self._installation(manifest={"community_broker_contract": "1", "tools": [{"name": "community_devices_describe", "operation": "devices.describe", "headers": {"Authorization": "x"}}]}),
            self._installation(manifest={"community_broker_contract": "1", "tools": [{"name": "http", "operation": "arbitrary"}]}),
        ]
        for install in invalid:
            with self.subTest(install=install):
                self.assertEqual(self.adapter.describe(install, self._actor_context()).tools, [])
                self.assertFalse(self._dispatch("community_devices_describe", {"device_id": DEVICE}, installation=install).ok)

    def test_bad_model_arguments_never_reach_transport(self):
        calls = []
        def runner(*args: Any, **kwargs: Any):
            calls.append((args, kwargs))
            raise AssertionError("transport should not run")
        self.adapter._runner = runner
        invalid_vectors = [
            ("community_devices_describe", {"device_id": DEVICE, "extra": 1}),
            ("community_telemetry_query", {"device_id": DEVICE, "start": "2026-10-20T00:00:00", "end": "2026-10-20T01:00:00Z", "keys": ["x"], "limit": 2}),
            ("community_telemetry_query", {"device_id": DEVICE, "start": "2026-10-20T00:00:00Z", "end": "2026-10-21T00:00:01Z", "keys": ["x"], "limit": 2}),
            ("community_telemetry_query", {"device_id": DEVICE, "start": "2026-10-20T00:00:00Z", "end": "2026-10-20T01:00:00Z", "keys": ["x", "x"], "limit": 2}),
            ("community_telemetry_query", {"device_id": DEVICE, "start": "2026-10-20T00:00:00Z", "end": "2026-10-20T01:00:00Z", "keys": ["x"], "limit": True}),
            ("community_commands_preview", {"device_id": DEVICE, "identify": "go", "params": {"x": float("nan")}}),
            ("community_commands_preview", {"device_id": DEVICE, "identify": "go", "params": {"x": "v" * (64 << 10)}}),
            ("community_commands_preview", {"device_id": DEVICE, "identify": "go", "params": {1: "non-string key"}}),
            ("community_commands_get_result", {"device_id": DEVICE, "message_id": "01C9BDB8-4480-7A91-B12E-0147057AB4A1"}),
        ]
        for name, args in invalid_vectors:
            with self.subTest(name=name, args=args):
                self.assertFalse(self._dispatch(name, args).ok)
        deep: dict[str, Any] = {}
        cursor = deep
        for _ in range(34):
            cursor["x"] = {}
            cursor = cursor["x"]
        self.assertFalse(self._dispatch("community_commands_preview", {"device_id": DEVICE, "identify": "go", "params": deep}).ok)
        cyclic: dict[str, Any] = {}
        cyclic["self"] = cyclic
        self.assertFalse(self._dispatch("community_commands_preview", {"device_id": DEVICE, "identify": "go", "params": cyclic}).ok)
        self.assertEqual(calls, [])

    def test_transport_metadata_mismatch_and_token_reflection_fail_closed(self):
        for mismatch in ("tenant", "subject", "device", "operation", "echo", "duplicate", "unknown"):
            with self.subTest(mismatch=mismatch):
                self._write_client(mismatch=mismatch)
                result = self._dispatch("community_devices_describe", {"device_id": DEVICE})
                self.assertFalse(result.ok)
                self.assertNotIn(TOKEN, json.dumps({"error": result.error, "payload": result.payload, "audit": result.audit_payload}))

    def test_subprocess_uses_stdin_not_argv_or_parent_environment(self):
        seen = []
        real_run = subprocess.run
        def recording_run(argv: list[str], **kwargs: Any):
            seen.append((argv, kwargs))
            return real_run(argv, **kwargs)
        self.adapter._runner = recording_run
        result = self._dispatch("community_devices_describe", {"device_id": DEVICE})
        self.assertTrue(result.ok, result.error)
        argv, kwargs = seen[0]
        self.assertNotIn(TOKEN, " ".join(argv))
        self.assertNotIn(TOKEN, str(kwargs.get("env")))
        self.assertEqual(kwargs["env"], {"PATH": os.defpath, "LANG": "C.UTF-8"})
        self.assertFalse(kwargs["shell"])
        self.assertEqual(kwargs["timeout"], 6)
        self.assertIn(TOKEN.encode(), kwargs["input"])

    def test_timeout_is_fixed_and_does_not_escape_process_error(self):
        def timeout_runner(*_args: Any, **_kwargs: Any):
            raise subprocess.TimeoutExpired("trusted-client", 6, stderr=TOKEN.encode())
        self.adapter._runner = timeout_runner
        result = self._dispatch("community_devices_describe", {"device_id": DEVICE})
        self.assertFalse(result.ok)
        self.assertEqual(result.error, "temporarily_unavailable")
        self.assertNotIn(TOKEN, json.dumps({"error": result.error, "payload": result.payload, "audit": result.audit_payload}))

    def test_user_scoped_installation_matches_only_current_actor(self):
        user_install = self._installation(scope=SkillScope.USER, scope_key=ACTOR)
        self.assertTrue(self._dispatch("community_devices_describe", {"device_id": DEVICE}, installation=user_install).ok)
        wrong_actor = self._trusted_context(actor_id="user-b", binding=SimpleNamespace(tenant_id=TENANT, system_user_id="user-b"))
        self.assertFalse(self._dispatch("community_devices_describe", {"device_id": DEVICE}, context=wrong_actor, installation=user_install).ok)

    def test_grant_binding_requires_trusted_actor_and_supports_revoke_expiry_and_capacity(self):
        bindings = GrantBindings(max_items=1)
        context = self._actor_context()
        handle = bindings.bind_issued(self._issued(), context)
        with self.assertRaises(ValueError):
            bindings.bind_issued(self._issued(delegation_id="delegation-2"), context)
        with self.assertRaises(ValueError):
            bindings.resolve(self._trusted_context(actor_id="user-b"))
        bindings.revoke(handle)
        with self.assertRaises(ValueError):
            bindings.resolve(self.context)
        with self.assertRaises(ValueError):
            bindings.bind_issued(self._issued(subject_id="user-b"), context)
        self.assertEqual(MAX_GRANTS, 128)

    def test_expiry_and_replay_grants_fail_closed_for_proposals(self):
        now = [datetime.now(timezone.utc).timestamp()]
        bindings = GrantBindings(clock=lambda: now[0])
        ctx = self._actor_context()
        issued = self._issued(scopes=["devices.describe"], expires_at=datetime.fromtimestamp(now[0] + 30, timezone.utc).isoformat())
        handle = bindings.bind_issued(issued, ctx)
        runtime_context = self._trusted_context(community_broker_grant_id=handle)
        self.assertIsNotNone(bindings.resolve(runtime_context))
        now[0] += 31
        with self.assertRaises(ValueError):
            bindings.resolve(runtime_context)
        replay = self._issued(
            delegation_id="replay-grant",
            scopes=["commands.preview"],
            replay=True,
        )
        with self.assertRaises(ValueError):
            bindings.bind_issued(replay, ctx)

    def test_issuer_binding_and_runtime_output_do_not_expose_parent_or_opaque_token(self):
        with self.assertRaises(ValueError):
            self.bindings.bind_issued(self._issued(tenant_id="tenant-b"), self._actor_context())
        result = self._dispatch("community_devices_describe", {"device_id": DEVICE})
        schemas = [
            {"name": item.name, "description": item.description, "parameters": item.parameters}
            for item in self.adapter.describe(self.installation, self._actor_context()).tools
        ]
        dump = json.dumps({"payload": result.payload, "audit": result.audit_payload, "schemas": schemas})
        self.assertNotIn(TOKEN, dump)
        self.assertNotIn("parent_jwt", dump)

    def test_nonzero_client_exit_preserves_only_well_formed_fixed_error(self):
        self.client.write_text(
            "#!/usr/bin/env python3\nimport json\nprint(json.dumps({'ok':False,'error':'not_authorized'}))\nraise SystemExit(1)\n",
            encoding="utf-8",
        )
        self.client.chmod(0o755)
        result = self._dispatch("community_devices_describe", {"device_id": DEVICE})
        self.assertFalse(result.ok)
        self.assertEqual(result.error, "not_authorized")

    def test_package_storage_keeps_runtime_manifest_separate_from_standard_package_manifest(self):
        class Repo:
            def __init__(self):
                self.items = {}
            def put(self, value):
                self.items[value.package_id] = value
                return value

        store = SimpleNamespace(skill_packages=Repo(), skill_versions=Repo())
        package_manifest = SkillPackageManifest.from_dict({
            "package_id": "thingspanel.community.broker-tools",
            "version": "1.0.0",
            "runtime": RuntimeKind.HTTP_TOOL.value,
            "entrypoint": "community_tools.py",
            "scopes": ["tenant", "user"],
            "display_name": "ThingsPanel community broker tools",
        })
        published = PackageStorageService(store).publish_package(
            package_manifest=package_manifest,
            runtime_manifest=MANIFEST,
        )
        version_record = store.skill_versions.items["thingspanel.community.broker-tools"]
        self.assertEqual(version_record.runtime, "http_tool")
        self.assertEqual(version_record.manifest_json, MANIFEST)
        self.assertEqual(version_record.metadata["package_manifest"], package_manifest.to_dict())
        self.assertEqual(published["version"]["manifest_json"], MANIFEST)


if __name__ == "__main__":
    unittest.main()
