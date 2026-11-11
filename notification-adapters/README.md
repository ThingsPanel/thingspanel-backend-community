# Portable notification adapters

This directory is a standalone Go module (`encore.app/notification-adapters`) for the fixed plugin HTTP v1 protocol. It does not import Encore or modify the shared application module. Run commands from this directory with the project-pinned Go toolchain.

```sh
GOMAXPROCS=2 GOFLAGS=-p=2 go test ./...
GOMAXPROCS=2 GOFLAGS=-p=2 go vet ./...
GOMAXPROCS=2 GOFLAGS=-p=2 go test -race ./...
```

The `sdk` package serves `/v1/manifest`, `/v1/validate`, `/v1/send`, and `/healthz`. It enforces bounded strict JSON, the frozen snake_case envelope, constant-time bearer-token comparison, accepted/rejected/unknown union rules, and fixed public errors. `AllowedUIOrigins` is opt-in and matches complete origins only; it supports credential-free GET/POST preflight for the four plugin routes and allows only `Authorization` and `Content-Type`. It is empty by default. Manifest JSON is emitted with one trailing newline so a caller can hash the exact response bytes for registration preflight.

The `mock` package is an in-process provider fixture. It does not use an external service. Each `cmd` program starts a local plugin HTTP server only when an operator supplies `NOTIFICATION_PLUGIN_TOKEN`; none sends a message during startup. The DingTalk, Alibaba SMS, SMTP, and legacy webhook executables default to loopback ports 5003 through 5006. The mock is in-process and has no executable. `NOTIFICATION_PLUGIN_UI_ORIGINS` is an optional comma-separated exact-origin allowlist.

## DingTalk group robot

`dingtalk` uses the unified `im` channel and supports text for its configured group only (`recipient.kind=chat_id`, `recipient.address=default`). The `default` target names the group fixed by this account's robot URL. It rejects group-specific URLs, direct-message targets, and non-text content. Configuration contains a full webhook URL and signing secret; both are marked secret. The webhook URL is validated against `https://oapi.dingtalk.com/robot/send` and exactly one `access_token` query value before any request is built. The request uses the documented `timestamp + newline + secret` HMAC-SHA256 signature and posts to a fixed API path. Only HTTP 200 with explicit `errcode: 0` returns `accepted`; any nonzero business code is a definitive generic rejection, while non-200, malformed, oversized, timeout, or connection results are `unknown`. Provider error text and the secret-bearing URL are never copied into errors.

Reference: [DingTalk custom robot access](https://open.dingtalk.com/document/orgapp/custom-robot-access). This plugin has no final delivery receipt capability. A provider `errcode: 0` means accepted by the robot API, not delivered to every group member.

## Alibaba Cloud domestic SMS

`aliyunsms` targets domestic Dysmsapi `2017-05-25` at the fixed `https://dysmsapi.aliyuncs.com/` endpoint. It sends one Mainland China SMS target with an approved template ID from the instance `template_allowlist`; it does not convert free text to a template. `access_key_id`, `access_key_secret`, and optional `security_token` are all marked secret. The configured region is constrained to a China region ID and is retained for the receipt identity/queue configuration; the send endpoint does not come from tenant configuration.

The official Alibaba Go SDK was reviewed but is not used: it owns its HTTP client, follows redirects, and reads response bodies without a configurable bound. This implementation follows Alibaba's V3 `ACS3-HMAC-SHA256` request signing specification with the fixed API host, a 12-second total client timeout, redirects and environment proxies disabled, no transport retry, and a 64 KiB response bound. It never sends an `Idempotency-Key` header to Alibaba. Only HTTP 200 plus `Code=OK` and a non-empty `BizId` returns `accepted`; `BizId` is the receipt correlation ID, not an idempotency key. `isv.BUSINESS_LIMIT_CONTROL` is a definitive rejection eligible for core-managed backoff; other explicit provider codes are generic non-retryable rejections. Transport errors, malformed/oversized responses, and an `OK` response without `BizId` return `unknown`, with automatic retry disabled.

Reference: [SendSms API](https://api.aliyun.com/document/Dysmsapi/2017-05-25/SendSms), [V3 request signing](https://www.alibabacloud.com/help/en/sdk/product-overview/v3-request-structure-and-signature), and [domestic SMS API error codes](https://help.aliyun.com/zh/sms/developer-reference/api-error-codes). The signer test includes an independently calculated fixed request vector and the RFC 4231 HMAC-SHA256 primitive vector.

`NormalizeReports` maps a strict domestic `SmsReport` array into normalized receipt events. The module also contains an authenticated MNS REST queue adapter, encrypted send-correlation ledger, operator route resolver, worker, and fixed HTTPS core receipt poster. The worker is disabled by default and ACKs an MNS message only after the core returns HTTP 200. However, Alibaba's documented queue example wraps reports in an `arg` field without specifying a complete literal body or the field's type/encoding; the current parser deliberately accepts only a strict report array. There is no verified queue fixture or live core receipt-path evidence, so the manifest and accepted send response remain `delivery_receipts=false` / `receipt_expected=false`. The worker code is not an end-to-end receipt capability claim.

## SMTP email

`smtp` preserves the legacy email adapter's `host`, `port`, `from_email`, `from_password`, `ssl`, subject, and plain-text body inputs. It sends exactly one recipient per plugin request, uses base64 MIME body encoding, and never retries internally. `ssl=true` means implicit TLS; `ssl=false` or omitted requires STARTTLS and refuses cleartext authentication. The legacy sender did not pass its `ssl` field to Gomail, so this is a deliberate transport-hardening difference that needs an operator compatibility check before cutover. A server acceptance returns `accepted` with no receipt. Explicit SMTP rejections before DATA are `rejected`; once the server grants DATA, any write or final-response failure is `unknown` and non-retryable.

The executable requires `NOTIFICATION_SMTP_ALLOWED_SERVERS` as a comma-separated exact `host:port` allowlist. DNS is resolved at startup and pinned. Resolved private service IPs require an explicit operator `NOTIFICATION_SMTP_ALLOWED_CIDRS` range; loopback, link-local, metadata, and multicast destinations are always refused. The allowlist is server configuration, never tenant-controlled request data.

## Legacy signed webhook

`legacywebhook` preserves the old normalization path: decode the payload into `map[string]interface{}`, encode with Go's JSON encoder with HTML escaping disabled, then trim the encoder newline. It sends the resulting bytes as `application/json` with `X-Signature-256: sha256=<lowercase HMAC-SHA256>`. This matches the old body/signature format for the same JSON input, including float64 conversion for JSON numbers and sorted map keys.

The executable requires `NOTIFICATION_LEGACY_WEBHOOK_ALLOWED_ORIGINS`, a comma-separated list of exact HTTPS origins. Each origin is DNS-resolved once at startup; public unicast addresses are pinned, while private, loopback, link-local, metadata, multicast, and reserved addresses are refused. Proxy use and redirects are disabled. Only 2xx is `accepted`; every non-2xx (including 3xx, 4xx/429, 5xx) and transport error is `unknown` and non-retryable because the legacy receiver may have performed side effects before returning an error. Payload URL and signing secret are both secret fields. No endpoint value supplied by the recipient can affect routing.

## Legacy coverage boundary

The [UniPush adapter](unipush/README.md) supports tenant-scoped App notifications.
The Community notification core expands members into active device bindings; the
plugin revalidates each binding immediately before submitting it to UniCloud.
The plugin preserves the 2.0.0 manifest and the latest preview-length fix from
Kernel commit `077f890`. Explicit single-CID requests are diagnostic-only.
Password-reset email is unchanged. See [source convergence](../docs/KERNEL-SOURCE-CONVERGENCE.md)
for source ownership and deployment boundaries.

## Validation boundary

Provider tests use local HTTP and SMTP fixtures. The Community worker integration
test additionally runs a real PostgreSQL store and a separately built UniPush
process; the directory and cloud remain fixtures. These checks do not send real
DingTalk, Aliyun, SMTP, webhook or phone notifications. Alibaba trusted receipt
delivery remains unverified and is not advertised by the manifest.
