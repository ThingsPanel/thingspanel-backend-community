"""Thin ChatBridge adapter for the fixed ThingsPanel community broker tools.

This module intentionally does not mint credentials or perform an issuer
exchange. A trusted server must obtain an issuer response over verified TLS,
bind it to its current actor/thread, then place only the opaque binding handle
in ChatBridge's server-created request_context.
"""
from __future__ import annotations

import ipaddress
import json
import os
import re
import subprocess
import threading
import time
import uuid
from collections import OrderedDict
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable
from urllib.parse import urlsplit

from chatbridge.contracts.runtime import RuntimeAdapter
from chatbridge.contracts.types import AuthorizationResult, RuntimeResult, RuntimeToolCall, ToolSchema, ToolSurface


PACKAGE_ID = "thingspanel.community.broker-tools"
PACKAGE_VERSION = "1.0.0"
CONTRACT_MARKER = "1"
RUNTIME_KIND = "http_tool"
MAX_GRANTS = 128
MAX_GRANT_TTL_SECONDS = 120
MAX_INPUT_BYTES = 128 << 10
MAX_PARAMS_BYTES = 64 << 10
MAX_OUTPUT_BYTES = (64 << 10) + 2048
MAX_DEPTH = 32
TOOL_TIMEOUT_SECONDS = 6
_DEVICE_ID_LIMIT = 36
_SAFE_ID = re.compile(r"^[^\x00-\x20\x7f]{1,36}$")
_RFC3339 = re.compile(r"^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?(?:Z|[+-]\d\d:\d\d)$")

_TOOLS: dict[str, tuple[str, dict[str, Any]]] = {
    "community_devices_describe": (
        "devices.describe",
        {"type": "object", "additionalProperties": False, "required": ["device_id"], "properties": {"device_id": {"type": "string", "maxLength": 36}}},
    ),
    "community_telemetry_query": (
        "telemetry.query",
        {"type": "object", "additionalProperties": False, "required": ["device_id", "start", "end", "keys", "limit"], "properties": {
            "device_id": {"type": "string", "maxLength": 36},
            "start": {"type": "string", "format": "date-time"},
            "end": {"type": "string", "format": "date-time"},
            "keys": {"type": "array", "minItems": 1, "maxItems": 8, "uniqueItems": True, "items": {"type": "string", "minLength": 1, "maxLength": 128}},
            "limit": {"type": "integer", "minimum": 1, "maximum": 200},
        }},
    ),
    "community_commands_preview": (
        "commands.preview",
        {"type": "object", "additionalProperties": False, "required": ["device_id", "identify"], "properties": {
            "device_id": {"type": "string", "maxLength": 36},
            "identify": {"type": "string", "minLength": 1, "maxLength": 255},
            "params": {"type": "object"},
        }},
    ),
    "community_commands_get_result": (
        "commands.getResult",
        {"type": "object", "additionalProperties": False, "required": ["device_id", "message_id"], "properties": {
            "device_id": {"type": "string", "maxLength": 36},
            "message_id": {"type": "string", "format": "uuid"},
        }},
    ),
}
_OP_TO_TOOL = {operation: name for name, (operation, _schema) in _TOOLS.items()}
_FIXED_ERRORS = {
    "invalid_request", "invalid_argument", "not_authorized", "budget_exceeded",
    "unsupported_capability", "not_found", "stale_state", "temporarily_unavailable",
}


class AdapterError(ValueError):
    """Safe local validation error; its text is never exposed to the model."""


@dataclass(frozen=True, slots=True)
class BoundGrant:
    token: str = field(repr=False)
    delegation_id: str
    tenant_id: str
    subject_id: str
    thread_id: str
    device_ids: frozenset[str]
    scopes: frozenset[str]
    expires_at: datetime
    replay: bool = False


class GrantBindings:
    """Small process-local binding cache; the owner remains the authority."""

    def __init__(self, *, max_items: int = MAX_GRANTS, clock: Callable[[], float] = time.time) -> None:
        if type(max_items) is not int or max_items < 1 or max_items > MAX_GRANTS:
            raise ValueError("invalid grant cache bound")
        self._max_items = max_items
        self._clock = clock
        self._items: OrderedDict[str, BoundGrant] = OrderedDict()
        self._lock = threading.RLock()

    def bind_issued(self, reply: dict[str, Any], trusted_actor_context: dict[str, Any]) -> str:
        """Bind an issuer DTO that a trusted server obtained over verified TLS.

        Never call this with model arguments or installation manifest data.
        """
        required = {"delegation_id", "tenant_id", "subject_id", "token", "expires_at", "device_ids", "scopes"}
        if not isinstance(reply, dict) or not required.issubset(reply) or set(reply) - (required | {"max_calls", "replay"}):
            raise AdapterError("invalid issuer reply")
        tenant = _bounded_id(trusted_actor_context.get("tenant_id"), _DEVICE_ID_LIMIT)
        subject = _bounded_id(trusted_actor_context.get("actor_id"), _DEVICE_ID_LIMIT)
        thread = _bounded_id(trusted_actor_context.get("thread_id"), 256)
        if tenant != reply.get("tenant_id") or subject != reply.get("subject_id"):
            raise AdapterError("issuer subject mismatch")
        delegation_id = _bounded_id(reply.get("delegation_id"), 64)
        token = reply.get("token")
        if not isinstance(token, str) or not re.fullmatch(r"tpai_[A-Za-z0-9_-]{43}", token):
            raise AdapterError("invalid issuer token")
        devices = _unique_strings(reply.get("device_ids"), 1, 16, 36)
        scopes = _unique_strings(reply.get("scopes"), 1, 4, 64)
        if not set(scopes).issubset(set(_OP_TO_TOOL)):
            raise AdapterError("invalid issuer scopes")
        replay = reply.get("replay", False)
        if type(replay) is not bool:
            raise AdapterError("invalid issuer replay mode")
        if replay and not set(scopes).issubset({"devices.describe", "telemetry.query", "commands.getResult"}):
            raise AdapterError("replay grant includes proposal capability")
        if "max_calls" in reply and (type(reply["max_calls"]) is not int or not 1 <= reply["max_calls"] <= 20):
            raise AdapterError("invalid issuer budget")
        expiry = _parse_expiry(reply.get("expires_at"))
        remaining = expiry.timestamp() - self._clock()
        if remaining <= 0 or remaining > MAX_GRANT_TTL_SECONDS:
            raise AdapterError("issuer grant expiry outside local bound")
        grant = BoundGrant(token, delegation_id, tenant, subject, thread, frozenset(devices), frozenset(scopes), expiry, replay)
        with self._lock:
            self._purge_expired_locked()
            if delegation_id not in self._items and len(self._items) >= self._max_items:
                raise AdapterError("grant cache full")
            self._items[delegation_id] = grant
            self._items.move_to_end(delegation_id)
        return delegation_id

    def resolve(self, request_context: dict[str, Any]) -> BoundGrant:
        if not isinstance(request_context, dict):
            raise AdapterError("missing server request context")
        binding = request_context.get("binding")
        tenant = request_context.get("tenant_id")
        actor = request_context.get("actor_id")
        thread = request_context.get("thread_id")
        handle = request_context.get("community_broker_grant_id")
        if not isinstance(handle, str) or not handle:
            raise AdapterError("missing server grant binding")
        if binding is None:
            raise AdapterError("missing authenticated binding")
        bound_tenant = getattr(binding, "tenant_id", None)
        bound_actor = getattr(binding, "system_user_id", None)
        with self._lock:
            self._purge_expired_locked()
            grant = self._items.get(handle)
            if grant is None:
                raise AdapterError("grant unavailable")
            if (tenant, actor, thread, bound_tenant, bound_actor) != (
                grant.tenant_id, grant.subject_id, grant.thread_id, grant.tenant_id, grant.subject_id
            ):
                raise AdapterError("grant context mismatch")
            self._items.move_to_end(handle)
            return grant

    def revoke(self, handle: str) -> None:
        with self._lock:
            self._items.pop(handle, None)

    def unbind_thread(self, tenant_id: str, actor_id: str, thread_id: str) -> None:
        with self._lock:
            for handle, grant in list(self._items.items()):
                if (grant.tenant_id, grant.subject_id, grant.thread_id) == (tenant_id, actor_id, thread_id):
                    self._items.pop(handle, None)

    def _purge_expired_locked(self) -> None:
        now = self._clock()
        for handle, grant in list(self._items.items()):
            if grant.expires_at.timestamp() <= now:
                self._items.pop(handle, None)


class CommunityBrokerToolAdapter(RuntimeAdapter):
    runtime_kind = RUNTIME_KIND

    def __init__(
        self,
        bindings: GrantBindings,
        *,
        executable: str,
        origin: str,
        ca_file: str = "",
        development_http: bool = False,
        runner: Callable[..., subprocess.CompletedProcess[bytes]] = subprocess.run,
    ) -> None:
        self._bindings = bindings
        self._executable = str(Path(executable))
        if not Path(self._executable).is_absolute() or not Path(self._executable).is_file():
            raise ValueError("trusted broker tools client must be an existing absolute executable")
        self._origin = _validate_origin(origin, development_http)
        if ca_file and (not Path(ca_file).is_absolute() or not Path(ca_file).is_file()):
            raise ValueError("trusted CA file must be an existing absolute file")
        self._ca_file = ca_file
        self._development_http = bool(development_http)
        self._runner = runner

    def describe(self, installation: Any, actor_context: dict[str, Any]) -> ToolSurface:
        if not self._valid_installation(installation, actor_context):
            return ToolSurface()
        manifest = installation.version_record.manifest_json
        allowed = _manifest_tools(manifest)
        if allowed is None:
            return ToolSurface()
        tools = [
            ToolSchema(name=name, description=_description(operation), parameters=json.loads(json.dumps(_TOOLS[name][1])))
            for name, operation in ((name, _TOOLS[name][0]) for name in _TOOLS)
            if name in allowed and allowed[name] == operation
        ]
        return ToolSurface(tools=tools)

    def authorize(self, installation: Any, request_context: dict[str, Any], call: RuntimeToolCall) -> AuthorizationResult:
        surface_names = {tool.name for tool in self.describe(installation, _actor_context_from_request(request_context)).tools}
        operation = _tool_operation(call.tool_name)
        if call.tool_name not in surface_names or operation is None:
            return AuthorizationResult(False, "not_authorized")
        try:
            device_id, backend_input = _validate_and_translate(call.tool_name, call.arguments)
            _strict_json_bytes({"operation": operation, "input": backend_input}, MAX_INPUT_BYTES)
        except Exception:
            return AuthorizationResult(False, "invalid_argument")
        try:
            grant = self._bindings.resolve(request_context)
        except Exception:
            return AuthorizationResult(False, "not_authorized")
        if device_id not in grant.device_ids or operation not in grant.scopes or (grant.replay and operation == "commands.preview"):
            return AuthorizationResult(False, "not_authorized")
        try:
            _strict_json_bytes({"token": grant.token, "operation": operation, "input": backend_input}, MAX_INPUT_BYTES)
        except Exception:
            return AuthorizationResult(False, "invalid_argument")
        return AuthorizationResult(True, metadata={"operation": operation})

    def execute(self, installation: Any, request_context: dict[str, Any], call: RuntimeToolCall) -> RuntimeResult:
        if not self._valid_installation(installation, _actor_context_from_request(request_context)):
            return _failure("not_authorized")
        allowed = _manifest_tools(installation.version_record.manifest_json)
        operation = _tool_operation(call.tool_name)
        if allowed is None or allowed.get(call.tool_name) != operation or operation is None:
            return _failure("not_authorized")
        try:
            device_id, backend_input = _validate_and_translate(call.tool_name, call.arguments)
        except Exception:
            return _failure("invalid_argument")
        try:
            grant = self._bindings.resolve(request_context)
        except Exception:
            return _failure("not_authorized")
        if device_id not in grant.device_ids or operation not in grant.scopes or (grant.replay and operation == "commands.preview"):
            return _failure("not_authorized")
        try:
            request = _strict_json_bytes({"token": grant.token, "operation": operation, "input": backend_input}, MAX_INPUT_BYTES)
        except Exception:
            return _failure("invalid_argument")
        try:
            argv = [self._executable, "--origin", self._origin]
            if self._ca_file:
                argv.extend(["--ca-file", self._ca_file])
            if self._development_http:
                argv.append("--development-http")
            completed = self._runner(
                argv,
                input=request,
                timeout=TOOL_TIMEOUT_SECONDS,
                capture_output=True,
                check=False,
                shell=False,
                env={"PATH": os.defpath, "LANG": "C.UTF-8"},
            )
            stdout = completed.stdout
            if not isinstance(stdout, bytes) or len(stdout) > MAX_OUTPUT_BYTES:
                return _failure("temporarily_unavailable")
            envelope = _strict_json_loads(stdout, MAX_OUTPUT_BYTES)
            if not isinstance(envelope, dict) or type(envelope.get("ok")) is not bool:
                return _failure("temporarily_unavailable")
            if envelope["ok"] is False:
                if completed.returncode == 0 or set(envelope) != {"ok", "error"} or envelope.get("error") not in _FIXED_ERRORS:
                    return _failure("temporarily_unavailable")
                return _failure(envelope["error"])
            if completed.returncode != 0:
                return _failure("temporarily_unavailable")
            if set(envelope) != {"ok", "data"} or not isinstance(envelope.get("data"), dict):
                return _failure("temporarily_unavailable")
            outer = envelope["data"]
            if set(outer) != {"operation", "device_id", "tenant_id", "subject_id", "data"}:
                return _failure("temporarily_unavailable")
            if (outer["operation"], outer["device_id"], outer["tenant_id"], outer["subject_id"]) != (
                operation, device_id, grant.tenant_id, grant.subject_id
            ):
                return _failure("not_authorized")
            if not isinstance(outer["data"], dict):
                return _failure("temporarily_unavailable")
            model_data = _strict_json_bytes(outer["data"], MAX_OUTPUT_BYTES)
            if grant.token.encode("utf-8") in model_data or len(model_data) > (64 << 10):
                return _failure("temporarily_unavailable")
            return RuntimeResult(ok=True, payload={"ok": True, "data": outer["data"]}, audit_payload={"operation": operation})
        except subprocess.TimeoutExpired:
            return _failure("temporarily_unavailable")
        except Exception:
            return _failure("temporarily_unavailable")

    def validate_output(self, result: RuntimeResult) -> RuntimeResult:
        if not isinstance(result, RuntimeResult) or type(result.ok) is not bool or not isinstance(result.payload, dict):
            return _failure("temporarily_unavailable")
        try:
            if len(_strict_json_bytes(result.payload, 64 << 10)) > 64 << 10:
                return _failure("temporarily_unavailable")
        except Exception:
            return _failure("temporarily_unavailable")
        if not isinstance(result.audit_payload, dict):
            result.audit_payload = {}
        return result

    @staticmethod
    def _valid_installation(installation: Any, actor_context: dict[str, Any]) -> bool:
        try:
            item = installation.installation
            version = installation.version_record
            if item.package_id != PACKAGE_ID or item.version != PACKAGE_VERSION or not item.is_active:
                return False
            if version.package_id != PACKAGE_ID or version.version != PACKAGE_VERSION or version.runtime != RUNTIME_KIND:
                return False
            scope = getattr(item.scope, "value", item.scope)
            if scope == "tenant":
                return bool(item.scope_key) and item.scope_key == actor_context.get("tenant_id")
            if scope == "user":
                return bool(item.scope_key) and item.scope_key == actor_context.get("actor_id")
            return False
        except Exception:
            return False


def _actor_context_from_request(request_context: Any) -> dict[str, Any]:
    if not isinstance(request_context, dict):
        return {}
    return {"tenant_id": request_context.get("tenant_id"), "actor_id": request_context.get("actor_id")}


def _manifest_tools(manifest: Any) -> dict[str, str] | None:
    if not isinstance(manifest, dict) or set(manifest) != {"community_broker_contract", "tools"}:
        return None
    if manifest.get("community_broker_contract") != CONTRACT_MARKER or not isinstance(manifest.get("tools"), list):
        return None
    if len(manifest["tools"]) > len(_TOOLS):
        return None
    allowed: dict[str, str] = {}
    for declaration in manifest["tools"]:
        if not isinstance(declaration, dict) or set(declaration) != {"name", "operation"}:
            return None
        name, operation = declaration.get("name"), declaration.get("operation")
        if name not in _TOOLS or _TOOLS[name][0] != operation or name in allowed:
            return None
        allowed[name] = operation
    return allowed


def _validate_and_translate(tool_name: str, arguments: Any) -> tuple[str, dict[str, Any]]:
    if not isinstance(arguments, dict) or _depth(arguments) > MAX_DEPTH:
        raise AdapterError("invalid arguments")
    operation = _tool_operation(tool_name)
    if operation is None:
        raise AdapterError("unknown tool")
    expected = {
        "devices.describe": {"device_id"},
        "telemetry.query": {"device_id", "start", "end", "keys", "limit"},
        "commands.preview": {"device_id", "identify", "params"},
        "commands.getResult": {"device_id", "message_id"},
    }[operation]
    required = {
        "devices.describe": {"device_id"},
        "telemetry.query": {"device_id", "start", "end", "keys", "limit"},
        "commands.preview": {"device_id", "identify"},
        "commands.getResult": {"device_id", "message_id"},
    }[operation]
    if set(arguments) - expected or not required.issubset(arguments):
        raise AdapterError("unknown or missing arguments")
    device_id = _bounded_id(arguments.get("device_id"), _DEVICE_ID_LIMIT)
    if not _SAFE_ID.fullmatch(device_id):
        raise AdapterError("invalid device")
    if operation == "devices.describe":
        return device_id, {"device_id": device_id}
    if operation == "telemetry.query":
        start_text, end_text = arguments.get("start"), arguments.get("end")
        start, end = _parse_rfc3339(start_text), _parse_rfc3339(end_text)
        duration = (end - start).total_seconds()
        if duration < 0 or duration > 24 * 60 * 60:
            raise AdapterError("invalid window")
        keys = _unique_strings(arguments.get("keys"), 1, 8, 128)
        limit = arguments.get("limit")
        if type(limit) is not int or not 1 <= limit <= 200:
            raise AdapterError("invalid limit")
        return device_id, {"device_id": device_id, "window": {"start": start_text, "end": end_text, "keys": keys, "limit": limit}}
    if operation == "commands.preview":
        identify = _bounded_id(arguments.get("identify"), 255)
        if "params" in arguments and not isinstance(arguments["params"], dict):
            raise AdapterError("params must be an object")
        params = arguments.get("params", {})
        encoded_params = _strict_json_bytes(params, MAX_PARAMS_BYTES)
        params_json = encoded_params.decode("utf-8")
        command = {"device_id": device_id, "identify": identify, "value": params_json}
        return device_id, {"device_id": device_id, "command": command}
    message_id = arguments.get("message_id")
    if not isinstance(message_id, str):
        raise AdapterError("invalid message id")
    try:
        parsed = uuid.UUID(message_id)
    except (ValueError, AttributeError):
        raise AdapterError("invalid message id") from None
    if str(parsed) != message_id:
        raise AdapterError("non-canonical message id")
    return device_id, {"device_id": device_id, "message_id": message_id}


def _tool_operation(name: str) -> str | None:
    pair = _TOOLS.get(name)
    return pair[0] if pair else None


def _bounded_id(value: Any, limit: int) -> str:
    if not isinstance(value, str) or not value or len(value.encode("utf-8")) > limit or value.strip() != value or any(ord(c) < 0x20 or ord(c) == 0x7f for c in value):
        raise AdapterError("invalid identifier")
    return value


def _unique_strings(value: Any, minimum: int, maximum: int, item_limit: int) -> list[str]:
    if not isinstance(value, (list, tuple)) or not minimum <= len(value) <= maximum:
        raise AdapterError("invalid list")
    result = []
    seen = set()
    for item in value:
        checked = _bounded_id(item, item_limit)
        if checked in seen:
            raise AdapterError("duplicate list value")
        seen.add(checked)
        result.append(checked)
    return result


def _parse_rfc3339(value: Any) -> datetime:
    if not isinstance(value, str) or not _RFC3339.fullmatch(value):
        raise AdapterError("invalid timestamp")
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        raise AdapterError("invalid timestamp") from None
    if parsed.tzinfo is None:
        raise AdapterError("timezone required")
    return parsed.astimezone(timezone.utc)


def _parse_expiry(value: Any) -> datetime:
    if isinstance(value, datetime):
        parsed = value
    elif isinstance(value, str):
        try:
            parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
        except ValueError:
            raise AdapterError("invalid expiry") from None
    else:
        raise AdapterError("invalid expiry")
    if parsed.tzinfo is None:
        raise AdapterError("invalid expiry")
    return parsed.astimezone(timezone.utc)


def _depth(value: Any, level: int = 0) -> int:
    if level > MAX_DEPTH:
        return level
    if isinstance(value, dict):
        return max([level, *(_depth(k, level + 1) for k in value.keys()), *(_depth(v, level + 1) for v in value.values())])
    if isinstance(value, (list, tuple)):
        return max([level, *(_depth(item, level + 1) for item in value)])
    return level


def _strict_json_bytes(value: Any, max_bytes: int) -> bytes:
    if _depth(value) > MAX_DEPTH:
        raise AdapterError("JSON nesting too deep")
    if not _is_json_value(value):
        raise AdapterError("invalid JSON value")
    try:
        encoded = json.dumps(value, ensure_ascii=False, separators=(",", ":"), allow_nan=False).encode("utf-8")
    except (TypeError, ValueError, UnicodeError):
        raise AdapterError("invalid JSON value") from None
    if len(encoded) > max_bytes:
        raise AdapterError("JSON size limit exceeded")
    return encoded


def _is_json_value(value: Any, seen: set[int] | None = None) -> bool:
    if value is None or type(value) in (bool, str, int, float):
        return True
    if seen is None:
        seen = set()
    if isinstance(value, list):
        identity = id(value)
        if identity in seen:
            return False
        seen.add(identity)
        valid = all(_is_json_value(item, seen) for item in value)
        seen.remove(identity)
        return valid
    if isinstance(value, dict):
        identity = id(value)
        if identity in seen:
            return False
        seen.add(identity)
        valid = all(type(key) is str and _is_json_value(item, seen) for key, item in value.items())
        seen.remove(identity)
        return valid
    return False


def _strict_json_loads(payload: bytes, max_bytes: int) -> Any:
    if not isinstance(payload, bytes) or len(payload) == 0 or len(payload) > max_bytes:
        raise AdapterError("invalid tool response size")

    def pairs(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
        result: dict[str, Any] = {}
        for key, value in pairs:
            if key in result:
                raise AdapterError("duplicate JSON field")
            result[key] = value
        return result

    try:
        value = json.loads(payload.decode("utf-8"), object_pairs_hook=pairs, parse_constant=lambda _value: (_ for _ in ()).throw(AdapterError("non-finite JSON number")))
    except (UnicodeError, json.JSONDecodeError):
        raise AdapterError("invalid JSON response") from None
    if _depth(value) > MAX_DEPTH:
        raise AdapterError("JSON nesting too deep")
    return value


def _validate_origin(origin: str, development_http: bool) -> str:
    if not isinstance(origin, str) or len(origin) > 512:
        raise ValueError("invalid trusted broker tool origin")
    parsed = urlsplit(origin)
    if parsed.username or parsed.password or parsed.path not in ("", "/") or parsed.query or parsed.fragment:
        raise ValueError("invalid trusted broker tool origin")
    if parsed.scheme == "https" and parsed.hostname:
        return origin.rstrip("/")
    if parsed.scheme == "http" and development_http and parsed.hostname:
        try:
            address = ipaddress.ip_address(parsed.hostname)
        except ValueError:
            raise ValueError("development HTTP requires loopback IP literal") from None
        if address.is_loopback:
            return origin.rstrip("/")
    raise ValueError("broker tool origin must use HTTPS")


def _description(operation: str) -> str:
    return {
        "devices.describe": "Read bounded metadata for one explicitly delegated device.",
        "telemetry.query": "Read a bounded telemetry window for one explicitly delegated device.",
        "commands.preview": "Preview a command only; this operation never submits it.",
        "commands.getResult": "Read the result of a command for one explicitly delegated device.",
    }[operation]


def _failure(category: str) -> RuntimeResult:
    if category not in _FIXED_ERRORS:
        category = "temporarily_unavailable"
    return RuntimeResult(ok=False, error=category, audit_payload={})
