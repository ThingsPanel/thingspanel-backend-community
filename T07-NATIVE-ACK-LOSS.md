# T07 native source ACK-loss evidence

This local integration test exercised the legacy SourceBridge against the running native Core at `127.0.0.1:19404`, with the approved fixture PostgreSQL schema and the local SMTP fixture. It did not contact a real mail provider.

The test sent one frozen source intent through a TLS reverse proxy. The Core durably accepted it and returned HTTP 202; the proxy truncated that first response to simulate a lost ACK. The source row moved to `retry_wait` at attempt 1. The test recreated the bridge and retried the same request key and body. Core returned 202 again, the row moved to `handed_off` at attempt 2, and the notification and delivery IDs stayed stable. The SMTP fixture send counter increased by exactly one for the test, so the retry did not create another send.

The focused test command exited 0:

```sh
NOTIFICATION_TEST_DSN='postgres://notification_test:fixture-only-password@127.0.0.1:25543/notification_test' NOTIFICATION_NATIVE_E2E=1 GOMAXPROCS=2 /Users/junhong/Downloads/code/notification-workspaces/tools/encore-1.58.6/encore-go/bin/go test -p 2 ./internal/service -run '^TestNativeEncoreSourceBridgeEndToEnd$' -count=1 -v
```

The test output recorded projection HTTP 200, first and retry source HTTP 202, dispatch `accepted`, two relay attempts, and one fixture send. IDs were observed stable by assertions; credentials, request body, recipient, and hashes were not written to the report. The first attempt before the successful run received HTTP 401 because the fixed fixture source credential had expired. The fixture owner extended that single unrevoked source row by two hours and checked that it was active and unexpired; no token or hash was output. This is local fixture evidence, not product or production acceptance.
