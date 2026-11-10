# Standalone Go notification plugin example

This is a self-contained Go module for learning the plugin HTTP v1 shape. It
contains a snapshot of the current portable SDK source under `sdk/`, a tiny
provider, and the SDK handler contract tests. It has no `require` or `replace`
directive and imports no Encore/Core packages. The SDK is not published yet;
this local copy is included deliberately instead of pretending a remote module
can be fetched. Compare `sdk/` with `../../sdk/` when updating the SDK snapshot.

The example listens on loopback only and never sends an email or calls any
provider. Its `/v1/send` implementation always returns `rejected` with
`example_no_external_send`, making it safe to exercise the HTTP contract.

From this directory:

```sh
GOMAXPROCS=2 GOFLAGS=-p=2 go test ./...
GOMAXPROCS=2 GOFLAGS=-p=2 go vet ./...
NOTIFICATION_PLUGIN_TOKEN=local-fixture-token go run ./cmd/plugin
```

In another local shell, the authenticated manifest is available at
`http://127.0.0.1:5080/v1/manifest`. No token or recipient is compiled into the
example. Keep `NOTIFICATION_PLUGIN_TOKEN` private even in local development.
