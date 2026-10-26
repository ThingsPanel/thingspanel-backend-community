# Optional ChatBridge community broker tools adapter

This directory contains a thin, manually registered adapter for the ThingsPanel
community broker tool API. It imports ChatBridge's public `RuntimeAdapter`,
`RuntimeResult`, `ToolSchema`, `RuntimeToolCall`, `RuntimeDispatcher`, and
installation contracts at runtime; it does not vendor or modify ChatBridge.
The `http_tool` registry kind exists in ChatBridge's installation model, but
this adapter is not registered by a builder and is not installed or enabled by
this repository.

The adapter exposes only four fixed tools: device description, bounded
telemetry query, command preview, and command result lookup. It has no command
submit, authorize, generic HTTP, URL, header, tenant override, or MQTT publish
operation. Command preview produces a proposal only. JSON tool schemas describe
the surface but do not enforce it; `authorize` and `execute` independently
validate arguments, installation, actor/thread grant binding, and the fixed
operation/device scope.

## Trusted server integration

The host server must obtain an issuer response with verified TLS using the
existing human session, compare the returned `tenant_id` and `subject_id` to
the authenticated server actor, then call `GrantBindings.bind_issued()` with
that response and server-derived tenant, actor, and thread IDs. The returned
opaque binding handle belongs only in ChatBridge's server-created
`request_context` under `community_broker_grant_id`. The framework path
provides that context from `ChannelBinding`, `actor_context`, and the current
thread. Never copy the handle or token from model arguments or an installation
manifest. This repository does not implement the live gateway/login issuer
exchange, browser session handoff, revocation callback, or token refresh.

Bindings are process-local, capped at 128, and expire within 120 seconds.
Revoke/unbind them on local session end; the broker API remains the authority
and checks its own Redis grant and parent session on every call. The opaque
token is sent to the fixed Go client over stdin only, never argv, environment,
model output, schema, or audit payload. The client executable and API origin
are trusted constructor configuration. Production origins must use HTTPS and
verified TLS. `development_http=True` permits only loopback IP literals for
isolated local tests.

The Python subprocess timeout is six seconds around the Go client's five-second
RPC bound. Only `PATH` and a fixed locale are inherited. The adapter accepts a
success object only if its operation, device, tenant, and subject exactly match
the trusted call and grant, then exposes only its inner `data`. Errors are
normalized to fixed categories and raw stderr/exception text is never returned.
ChatBridge's current runtime contract passes `RuntimeToolCall.arguments` as an
already-parsed dictionary and its kernel drops the planner's raw argument text.
This adapter therefore cannot detect duplicate keys in the original model JSON;
the host's argument parser must reject those before constructing the runtime
call. This adapter rejects unknown fields and non-JSON values in the received
mapping, and the Go client rejects duplicate keys in the newly serialized
backend request and response.

## Manual registration

In a ChatBridge host that has explicitly completed the trusted server binding
flow, construct the adapter with a fixed executable and origin and register it
with the existing `RuntimeAdapterRegistry`:

```python
from chatbridge.runtime import RuntimeAdapterRegistry
from integrations.chatbridge import CommunityBrokerToolAdapter, GrantBindings

registry = RuntimeAdapterRegistry()
registry.register(CommunityBrokerToolAdapter(
    GrantBindings(),
    executable="/usr/local/bin/tp-broker-tools-client",
    origin="https://thingspanel.example.test",
))  # explicit host opt-in; not a generic HTTP executor
```

Do not enable a package-wide generic `http_tool` handler. Use the sample `runtime-manifest.json` as the
version record's `manifest_json`; the adapter rebuilds its own fixed schemas
and rejects added fields or unknown tool declarations. Effective installation
must be `enabled` or `pinned`, exact package/version, runtime `http_tool`, and
tenant- or user-scoped to the current actor. Platform-scoped installation is
rejected.

The ChatBridge skill package manifest remains the standard package manifest
(`package_id`, `version`, `runtime`, and `entrypoint`, plus normal scope and
compatibility metadata). The JSON file in this directory is only the separate
runtime manifest stored in `SkillVersionRecord.manifest_json`; package
ingestion stores the full package manifest independently in version metadata.

## Tests

The tests use ChatBridge's actual dispatcher, registry, and skill-installation
dataclasses, plus a temporary executable fixture that reads one stdin request
and writes a fixed response. They do not start ChatBridge, contact the broker,
invoke an LLM, or claim a live delegation flow. Example command:

```sh
CHATBRIDGE_SRC=/path/to/chatbridge/src python3 -m unittest discover \
  -s integrations/chatbridge/tests -v
```

Verification on 2026-10-20: all 14 adapter tests passed against the local
ChatBridge source tree using that command. Coverage includes actual dispatcher
authorization/execution, all four fixed operations, tenant/user/thread/device
scope, active installation and runtime-manifest shape, error-exit propagation,
bounded grant expiry/revoke, process stdin/argv/environment boundaries, timeout,
credential reflection, and malformed or mismatched five-field responses. The
test uses a temporary executable fixture; it does not call the Gin API, Redis,
PostgreSQL, a live issuer, ChatBridge service, or an LLM. It is not evidence of
gateway integration or deployment.

The Go client caps the complete upstream HTTP response at 64 KiB. Its stdout
adds only the small `{ok,data}` envelope, and the adapter separately rejects
model-visible inner data above 64 KiB. The success DTO is exactly
`operation/device_id/tenant_id/subject_id/data`; the adapter compares the first
four fields with trusted context before returning `data`.
