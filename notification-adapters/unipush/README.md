# UniPush App notification plugin 2.0.0

This is a standalone plugin HTTP v1 server, registered as `thingspanel.unipush`.
The community backend owns user/device/session bindings; Core owns policy expansion,
queues and per-device attempts; this plugin owns UniPush transport.

## Deployment

1. Deploy `app/uniCloud-aliyun/cloudfunctions/push-message-v2`. Copy
   `config.example.json` to **git-ignored** `config.private.json`, set the real App ID
   and a random `cloudToken` of at least 32 characters. Upload this function only.
   Its package sets URL path `/push-message-v2`; the old function stays unchanged.
2. Copy `deployments/unipush/.env.example` to `.env`, fill all values, then run
   `docker compose up -d --build` there. The listener is localhost-only on port 5009.
   Expose it to Core through your private network/TLS gateway and add that origin to
   Core's plugin allowlist. Never expose the community directory reader publicly
   without its dedicated credential and configured deployment/tenant allowlists.
3. `NOTIFICATION_UNIPUSH_DIRECTORY_TOKEN` must match the community member-reader
   token. The deployment ID must match its allowlist. `APP_PUSH_APP_ID` on the
   community backend defaults to `__UNI__71E31D5`; all three components must agree.
   Deploy one plugin process per App/deployment. Tenant config stays empty and
   cannot override endpoints, credentials or App identity.
4. Register version `2.0.0`, grant it to the tenant, create and enable an `im`
   instance as SYS_ADMIN. In the tenant notification policy, select that instance
   and **Use device alarm notification for all active phones**, then select users.
   Publish the policy. Select it on an alarm rule, or set it as the tenant default.

`go run ./cmd/unipush` is an alternative to Docker. HTTPS is required by default.
`NOTIFICATION_UNIPUSH_ALLOW_HTTP=1` explicitly permits HTTP for isolated local
services; the actual cloud URL should still use HTTPS. No redirects or environment
proxies, 12-second timeout, bounded responses, no internal send retries.

## Recipient and content contract

- Member source: `{kind:"member", userId:"<community user>", contactField:"appDevice"}`.
- Core expands each active phone to `{kind:"user_id", address:"<opaque binding UUID>"}`.
  This is a **binding ID**, never the community user ID or a stored CID.
- Immediately before sending, the plugin resolves that binding with the dedicated
  reader credential and tenant scope. Logout, session expiry, disabled user,
  CID rotation and account switching invalidate old queued bindings.
- `chat_id` with a single CID remains an explicit administrator diagnostic mode;
  it is not used in member policies and does not provide account lifecycle checks.
- Alarm content: `templateId: device_alarm`, `locale: zh-CN`, mapping
  `{title:"subject",text:"text",alarmId:"alarmId",alarmKind:"alarmKind"}`.
  `alarmKind` is `history` or `info`; omitted kind denotes history. Text is also
  supported for service tests. The payload includes tenant/user/message/alarm IDs.
- One device is one Core delivery/attempt. Repeated targets in the same intent
  are deduplicated. A user with no active phones is skipped; if everyone has none,
  the event is recorded as blocked (`app_no_active_devices`). Directory outages
  fail closed. Failure of one submitted device does not undo another delivery.
- The transport shortens displayed titles to 19 UTF-16 units and content to 49,
  adding an ellipsis for truncated previews. This satisfies UniPush's strict
  title/content limits without splitting supplementary Unicode characters.
  Original Core records and routing/alarm/idempotency identifiers stay unchanged.
- The cloud function checks the plugin credential and uses a stable hashed
  `request_id`. Only explicit provider `errCode:0` is accepted. Provider acceptance
  is not proof of phone display; receipt capability remains false.

## App lifecycle and release

App login/foreground refresh registers CID plus a stable installation ID, platform
and App ID. A new account or session replaces that installation's previous binding;
other phones remain separate. Explicit unbind is session-scoped. The backend also
revokes the session's notification marker on logout and single-session kickout.
If concurrent account login is desired, enable the existing `shared_account` system
setting; the plugin does not override that login policy. Expired sessions stop
receiving until login/registration resumes. Redis failure suppresses sends.

The App suppresses received/clicked messages for a different user, tenant or server.
Already accepted vendor messages cannot be recalled after logout. For confidential
alerts, keep lock-screen text appropriately brief.

The manifest enables UniPush offline support, but a release/custom base must be
rebuilt after native manifest changes. Configure the real package signing identity,
Android vendor channels and iOS APNs credentials in DCloud. Standard HBuilder bases
and an online Android test do not establish killed-App delivery on every vendor.
See [UniPush setup](https://uniapp.dcloud.net.cn/unipush-v2.html) and
[provider API](https://doc.dcloud.net.cn/uniCloud/uni-cloud-push/api.html).

## Validation

`go test ./...` from notification-adapters. Tests cover provider outcome parsing,
credential and tenant scope, revoked binding rejection before any cloud call,
alarm payloads and no unsafe retry. Core tests cover member fanout, deduplication,
empty-device policies and refusal by unrelated IM plugins. Community PostgreSQL +
Redis tests cover concurrent registration, multi-phone login, switch/logout/expiry,
disabled users and tenant boundaries. App scripts cover cloud authorization,
account/server isolation and session cleanup.
