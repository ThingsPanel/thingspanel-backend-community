# T09 compatibility candidate report

Status: candidate for independent review; not a T09 completion claim.

The adapter module is `encore.app/notification-adapters` and contains no third-party dependencies. The repository remote is `https://github.com/zjhong/iot-api-encore-kernel.git`. All changes in this candidate are confined to `notification-adapters/`. The frozen plugin-v1 contract file remains unchanged at SHA-256 `9a08abb1dd7932f66d0fe3d27d9a33b0464f9ce71984523955c2d51b272dd853`.

## Changes

- Alibaba SMS manifest now advertises `delivery_receipts=false`; successful submissions set `receipt_expected=false`. `aliyunsms/report.go` remains a normalization/interface helper only. There is no authenticated MNS/SMQ consumer or notification-core receipt sink in this module, so trusted delivery receipts are blocked.
- `smtp` and `cmd/smtp` implement a single-recipient text email adapter. Exact operator `host:port` targets are allowlisted, DNS answers are pinned, private networks require operator CIDRs, and loopback/link-local/metadata/multicast addresses are rejected. `ssl=true` uses implicit TLS; omitted/false requires STARTTLS. This is a documented transport-hardening difference because the old Gomail calls did not pass the persisted `ssl` field. There is no internal retry. Explicit SMTP refusal before DATA is `rejected`; after the server grants DATA, any body write or final server response failure is `unknown` and non-retryable.
- `legacywebhook` and `cmd/legacywebhook` preserve the old map decode → JSON encode with HTML escaping disabled → trim-newline representation and lowercase HMAC-SHA256 `X-Signature-256` header. It accepts only exact operator-approved HTTPS origins and pins public DNS answers. Proxies and redirects are disabled. Only 2xx maps to accepted; all non-2xx (including 429) and transport errors are non-retryable `unknown`, because the legacy receiver may have performed side effects before returning an error.
- APP compatibility remains blocked: the old member path invokes a void push-and-log method and records one group-level success, so it cannot provide a verifiable result for each push target. Existing password-reset email wiring and the old service are not modified. The Encore `plugin_client` is also outside this candidate.

## Local evidence

Toolchain: `/Users/junhong/Downloads/code/notification-workspaces/tools/encore-1.58.6/encore-go/bin/go`, observed as `go version go1.27.0-encore darwin/arm64`. Commands ran from `notification-adapters/` with `GOMAXPROCS=2 GOFLAGS=-p=2`:

- `go test ./...` — exit 0; local HTTP/TLS recorders and loopback SMTP fixture only.
- `go vet ./...` — exit 0.
- `go test -race ./...` — exit 0.
- `git diff --check` — exit 0.
- Frozen plugin contract SHA verified as listed above.

Fault coverage includes SMTP implicit TLS and STARTTLS, explicit recipient rejection before DATA, connection loss after DATA with one connection and `unknown`; webhook byte/signature fixture, 3xx/4xx/429/408/5xx, redirect suppression, unapproved origin, invalid/oversized JSON, and connection loss with one invocation. Alibaba accepted-response tests assert receipt capability and expectation both remain false.

No real provider credentials, recipients, external test sends, production database, Encore CLI, or external ports were used. Real SMTP and webhook endpoint compatibility still needs an explicitly authorized account/target/attempt budget before live validation. APP, trusted Alibaba queue receipts, production integration, and the remaining T09 deliverables remain unverified or blocked.
