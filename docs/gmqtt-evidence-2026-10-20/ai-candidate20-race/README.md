# Community AI integration candidate: 20-package evidence

This is a bounded engineering checkpoint, not G0–G5 completion or production approval.

## Frozen input and actual execution

The candidate started from `1fb74bc9` on `codex/gmqtt-community-20261019`, with an explicitly frozen new AI source snapshot. `source-manifest.json` records 218 owned Go/SQL/Python/C/header files. The single full command and timing are in `summary.json`: `go test -json -race -p 1 -count=1 -timeout 12m` over 20 listed packages. Private QA PostgreSQL was silently exported to all four documented test DSN variables, native MQTT opt-in was enabled, and ChatBridge runtime opt-in plus the explicit existing source path were enabled. Docker SIGKILL, legacy GMQTT and long-load opt-ins were absent. No DSN was written to the command or summary.

Nineteen packages passed; the service package failed only its new escaped-credential echo subtest at grant issuance. Earlier subtests had retained enough grants to reach the real eight-active-grants-per-user cap. The test now revokes its own echo-test delegation on cleanup. No production limit was relaxed. The complete original JSON/log is preserved, including this failure. Do not describe the original invocation as all-green.

The corrected service integration was first run separately against explicit real PG/Redis (`service-fixture-cleanup-rerun.json`), with both escaped and unescaped echo subtests passing. The entire service package then passed on the candidate with real PG/Redis, no skipped tests (`service-package-after-fixture-fix.json`). `go build -p 1 ./...` passed (`go-build-after-fixture-fix.log`). `source-manifest-after-fixture-fix.json` differs from the original only in that test cleanup file; the other 217 recorded sources are unchanged.

The root's earlier exploratory service command omitted the opt-in DB variable and skipped the integration. Its package-level PASS was insufficient evidence; it was not retained as validation of the escaped case. The explicit-PG runs above replace that assertion.

## Exercised boundaries

The full run actually passed the new Redis delegation tests, HTTP describe/telemetry/preview/result tests, revocation and parent logout, current user/device/tenant/authority/policy checks, safe result projection, real OperationLogs isolation and command-plan pre-logger body limits. Preview adds only a plan; it does not authorize, enqueue or publish a device command. Encore success fixtures are HTTP contract stubs.

The API's `TestBrokerAIToolHTTPDelegationRevocationAndOperationLogIsolation/ChatBridge_runtime_adapter_dispatcher_and_real_CLI` actually ran and passed. It builds this repository's Go CLI, starts a private TLS Gin server with a temporary CA, and imports the existing ChatBridge dispatcher/registry/installation contracts. Python adapter → Go process → TLS HTTP → current-session and private PG/Redis result lookup is exercised for `commands.getResult`. This does not start a ChatBridge server, run a model, install a live gateway issuer or demonstrate physical execution.

`python-adapter.log` contains 14 passing tests using the actual ChatBridge contracts and temporary executable fixtures. Those tests separately cover all fixed tool mappings, binding and installation scope, fixed errors, credential secrecy, malformed results and timeouts. They do not independently provide API or model evidence.

Exactly three tests skipped in the full run: private SIGKILL child helper, Linux-image SIGKILL parent without the image opt-in in this Mac invocation, and legacy GMQTT health without its opt-in. The separately documented Linux SIGKILL evidence remains distinct. No new AI integration test skipped in this run.

## Limits

No real model call, per-tenant model-token budget certification, live Encore/gateway deployment, full adversarial AC-A03 campaign, blind/operator study, paired benefit measurement, 72-hour stability or production cutover is claimed. Native MQTT regression here uses the local test plugin; it does not rebuild or recertify the previously pinned production image. Existing unrelated notification/version edits remain outside the managed candidate.
