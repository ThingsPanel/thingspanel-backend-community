# Alibaba SMS receipt worker decision

## Decision

Use the SMS `SmsReport` queue through Alibaba Cloud Simple Message Queue (formerly MNS) as the production receipt source. Keep `QuerySendDetails` as an isolated, read-only adapter experiment until Alibaba resolves its conflicting API lifecycle notices. Do not advertise `DeliveryReceipts` as available until the queue path, wire format, and durable sink are connected and verified.

The report normalizer now uses the documented domestic SmsReport fields: phone_number, send_time, report_time, success, err_code, err_msg, sms_size, biz_id, and out_id. biz_id is the value returned by SendSms and is the provider correlation identifier.

## Verified Alibaba behavior

- Domestic SMS status reports can be received through a queue consumer or HTTP batch push. The queue is generated when SMS status-report delivery is enabled; the documented name resembles `Alicom-Queue-i{instanceId}-SmsReport`.
- The official SMS MNS example reads a map from the queue message body and accesses its `arg` field. The documentation does not include a complete literal example of that queue payload, so the parser must not assume that the HTTP batch-push JSON array is also the queue body. Capture a sanitized queue sample or verify the exact `arg` shape before declaring the parser production-ready.
- Alibaba's official Go MNS SDK is `github.com/aliyun/aliyun-mns-go-sdk/v2`. Its queue interface provides receive and delete operations. MNS `ReceiveMessage` returns both a stable `MessageId` and a temporary `ReceiptHandle`; `DeleteMessage` requires the latest `ReceiptHandle`, which expires when visibility changes. These values must have separate fields in the worker message type.
- MNS delivery is at least once: process and durably accept each normalized receipt before deleting its queue message. A sink error, timeout, or uncertain result leaves the message available for redelivery. The implementation keeps stable MessageID separate from the current AckHandle; only the latter is used for delete or visibility changes.
- mns_queue.go implements context-aware REST receive/delete/visibility calls with the documented HMAC-SHA1 signature, fixed HTTPS regional hostname, redirects disabled, no proxy, bounded bodies, and bounded timeouts. TLS-mock tests verify request construction, current-handle use, and that redirects are not followed. These are local protocol tests, not a live Alibaba queue verification.

## `QuerySendDetails` lifecycle conflict

The current SMS help page documents `PhoneNumber`, `SendDate` (`yyyyMMdd`), optional single `BizId`, `PageSize` (1–50), and `CurrentPage`. The detail response contains `PhoneNum`, `SendDate`, `ReceiveDate`, `SendStatus`, and `ErrCode`; status 1 means waiting, 2 means failed, and 3 means successful. `ErrCode=DELIVERED` is the documented successful carrier receipt. The official Go SDK emits this RPC as HTTPS `POST` with `x-acs-action: QuerySendDetails` and API version `2017-05-25`.

However, Alibaba's API change log marks `QuerySendDetails` as “OpenAPI discontinued” on 2026-07-01 and again on 2026-07-02, while the API page still shows the operation and a 2026-07-16 response change. This contradiction blocks production use until Alibaba confirms the operation remains callable for the target SMS account and region. `query_report.go` is therefore a bounded read-only experiment: it signs only `QuerySendDetails`, has no send action, uses bounded pages and body size, and maps only final statuses to SDK receipts. Its HTTPS mock tests prove local request construction, not Alibaba service availability.

## Worker message and acknowledgment boundary

ledger.go stores accepted-send correlation in a process-exclusive AES-GCM encrypted snapshot. The parent directory is set to mode 0700; ledger, lock, and temporary files use 0600; commits fsync the encrypted temporary file, atomically rename it, and fsync the directory. Its key is the canonicalized hash of the access-key identity, region, and SmsReport; each record adds biz_id and phone as the lookup key and retains tenant, instance, delivery, config version, and send time as encrypted data. If Alibaba returns acceptance but the ledger cannot commit the mapping, Provider.Send returns non-retryable unknown. Reuse of the same correlation tuple for a different delivery scope is rejected without overwrite.

ReportWorker.RunOnce routes only an exact queue-scope + biz_id + phone mapping. It resolves an operator-configured fixed HTTPS core URL and receipt token for that exact tenant/instance, posts one standard receipt with the mapped delivery ID, and deletes the provider message only after HTTP 200 from the core receipt endpoint. Missing mappings, parser failures, route failures, and core failures delay visibility and never delete. Partial batch success is safe to replay because the core receipt inbox is idempotent.

The documented MNS example unwraps a JSON arg value, but Alibaba does not provide a literal sanitized queue body that establishes its exact type or encoding. decodeSmsReports intentionally accepts only a strict domestic report array and rejects unknown wrappers, duplicate keys, nulls, and extra fields. Until an operator supplies a sanitized queue fixture and that wrapper is verified, the worker leaves these messages visible for diagnosis and DeliveryReceipts remains false. No credentials are logged, and receipt errors do not include provider bodies or recipient values.

## Local command wiring and remaining gates

The Aliyun command leaves the report worker disabled unless ALIYUN_SMS_REPORT_WORKER_ENABLED is exactly true. With the switch off, start the plugin with NOTIFICATION_PLUGIN_TOKEN supplied by the operator and run `go run ./cmd/aliyunsms` from notification-adapters. The plugin listener defaults to loopback 127.0.0.1:5004. Operator readiness is served separately at 127.0.0.1:5005/internal/ready; that listener rejects non-loopback addresses and reports only ready plus report_worker_configured.

To start the plugin with queue polling configured, run it from `notification-adapters` with operator-injected values (the placeholders below are not literal secrets):

```sh
NOTIFICATION_PLUGIN_TOKEN='<plugin token>' \
ALIYUN_SMS_REPORT_WORKER_ENABLED=true \
ALIYUN_SMS_LEDGER_PATH='/var/lib/thingspanel/aliyunsms/send-ledger.bin' \
ALIYUN_SMS_LEDGER_KEY_FILE='/etc/thingspanel/aliyunsms/ledger.key' \
ALIYUN_SMS_ACCOUNT_ID='<account id>' \
ALIYUN_SMS_REGION='<region>' \
ALIYUN_SMS_MNS_QUEUE_NAME='<SmsReport queue name>' \
ALIYUN_SMS_MNS_ENDPOINT='https://<account id>.mns.<region>.aliyuncs.com' \
ALIYUN_SMS_MNS_ACCESS_KEY_ID='<MNS access key id>' \
ALIYUN_SMS_MNS_ACCESS_KEY_SECRET='<MNS access key secret>' \
ALIYUN_SMS_CORE_ALLOWED_ORIGINS='https://<notification core host>' \
ALIYUN_SMS_RECEIPT_ROUTES_FILE='/etc/thingspanel/aliyunsms/receipt-routes.json' \
go run ./cmd/aliyunsms
```

The route file contains one entry per exact tenant and instance, with a fixed allowlisted `core_origin` and a path to that instance's private receipt-token file. The runtime does not print configuration values. Its `/internal/ready` response is only available through the private loopback listener; it reports whether the process is ready and whether the worker is configured.

To configure the worker, provide ALIYUN_SMS_LEDGER_PATH and ALIYUN_SMS_LEDGER_KEY_FILE. The key file contains exactly 32 random bytes, is a regular non-symlink file with mode 0600, and is supplied outside the repository. Set ALIYUN_SMS_ACCOUNT_ID, ALIYUN_SMS_REGION, ALIYUN_SMS_MNS_QUEUE_NAME, ALIYUN_SMS_MNS_ENDPOINT to the exact HTTPS account/region endpoint, and the MNS access key ID and secret through the operator's secret injection. ALIYUN_SMS_CORE_ALLOWED_ORIGINS is a comma-separated exact HTTPS origin allowlist. ALIYUN_SMS_RECEIPT_ROUTES_FILE names a private mode-0600 JSON file whose entries map tenant_id and instance_id to one allowlisted core_origin and a receipt_token_file; each token file must also be a regular non-symlink mode-0600 file. Optional polling controls are ALIYUN_SMS_MNS_WAIT_SECONDS (1–30) and ALIYUN_SMS_REPORT_POLL_INTERVAL_SECONDS (1–60).

Any missing field, invalid origin, insecure file, duplicate route scope, or endpoint outside the fixed allowlist stops startup before serving HTTP. The provider and worker use the same exclusive ledger handle. Shutdown cancels the worker, waits for it, drains both HTTP servers, and only then closes the ledger. If a worker or HTTP request cannot drain, shutdown returns failure and the command exits without releasing the ledger while an in-flight user may still be writing.

This command wiring does not enable the receipt capability. The queue body's documented arg wrapper has no literal sanitized fixture; the parser rejects it and leaves the message available. There has been no live Alibaba MNS poll or SMS submission, no real core receipt-token request, and no verification that a tenant's queue/account credentials match the provider send scope. QuerySendDetails remains an isolated read-only experiment because official lifecycle notices conflict. Keep delivery_receipts and receipt_expected false until the wrapper and real account path are separately verified.

## Sources

- [Alibaba SMS `SmsReport` fields and MNS consumer example](https://help.aliyun.com/zh/sms/developer-reference/smsreport)
- [SMS receipt configuration and queue mode](https://help.aliyun.com/zh/sms/developer-reference/configure-delivery-receipts-1/)
- [Domestic SMS `SmsReport` HTTP batch-push payload](https://help.aliyun.com/zh/sms/developer-reference/smsreport-http)
- [MNS `ReceiveMessage` response and `ReceiptHandle`](https://help.aliyun.com/en/mns/developer-reference/receivemessage)
- [MNS `DeleteMessage` must use the latest `ReceiptHandle`](https://help.aliyun.com/zh/mns/developer-reference/deletemessage)
- [Official Alibaba MNS Go SDK](https://github.com/aliyun/aliyun-mns-go-sdk)
- [MNS request signing and canonical resource rules](https://help.aliyun.com/zh/mns/developer-reference/request-protocol-description)
- [SMS `QuerySendDetails` API reference](https://api.aliyun.com/document/Dysmsapi/2017-05-25/QuerySendDetails)
- [SMS API change log showing the QuerySendDetails lifecycle notices](https://help.aliyun.com/zh/sms/developer-reference/api-dysmsapi-2017-05-25-changeset)
