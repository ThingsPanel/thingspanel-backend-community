# Native ThingModel command host

This host implements the first persistent vertical slice of the direct-cutover
release: create, complete draft replacement, publish, list, detail, version list
and immutable version detail under `/api/v2/thing-model-*`. It delegates to
`app`, which reuses the existing pure rules and snapshot builder in `core`.
It does not call the Community model API or the legacy connector.

## Supported API boundary

The native API includes these model and resource operations:

- `POST /api/v2/thing-model-commands/create`
- `POST /api/v2/thing-model-commands/update`
- `POST /api/v2/thing-model-commands/publish`
- `POST /api/v2/thing-model-commands/update-resources`
- `GET /api/v2/thing-models` (supports `page`, `pageSize`, and `query`)
- `GET /api/v2/thing-models/:modelID`
- `GET /api/v2/thing-models/:modelID/resources`
- `GET /api/v2/thing-models/:modelID/versions`
- `GET /api/v2/thing-models/:modelID/versions/:publishedRevision`

Create and update accept semantic `ModelDraft` data, including typed fields,
ENUM values, and EVENT/COMMAND parameter definitions. Update is a complete
model replacement guarded by `expectedDraftRevision`; `fieldOperations` is not
implemented. List filters `kind` and `lifecycle` are not implemented and are
rejected. `fieldID` and `metaItems` remain unsupported by semantic replacement.
Catalog profile fields (`author`, `tags`, `cover`, `version`, `brand`,
`modelNumber`) and Web/App chart configuration live in a separately revisioned
resources document. Resource saves use full replacement, explicit null to
clear a chart config, `expectedRevision`, and an idempotency key. Create and
semantic update can include resources in the same transaction; omitted
resources preserve the existing document. Publish can bind
`expectedResourcesRevision` to the publication. Version detail returns the
immutable resources captured with that semantic snapshot; edits to the current
resources document do not alter older versions. Existing snapshots without
resource payloads read as empty revision zero.

`app` accepts a trusted Principal separately from command data. Writes require
an 8–128 byte Idempotency-Key, scoped by tenant, subject and operation. A changed
payload conflicts; matching requests replay the original committed result.
Model and resource changes, snapshots, audit, outbox and receipts commit
together. Updates compare their respective expected revisions. Publishing returns `PENDING_CONSUMER` with
consumer state `PENDING`; this host never claims that a device has applied it.

The single existing `device_metadata` resource remains declared in
`devicemetadata/db.go`; this host references it with `sqldb.Named`. Migration
Migrations 0015 and 0016 add a versioned canonical aggregate and isolated
resource and snapshot storage to the existing model database. The aggregate is
the only native draft source. Native writes do not maintain a second field
copy in the old item table. Old rows remain invisible until an explicit,
validated upgrade conversion; same-name target conflicts block the conversion
rather than overwrite existing data. Native semantic snapshots retain exact
checksum input bytes alongside JSONB storage. Resource snapshot content and
its checksum are stored with each native publication.

## Verification

From `encore-api`:

```sh
go test -race ./thingmodel/core ./thingmodel/app
encore test ./thingmodel/... -count=1
encore gen client --lang=openapi --services thingmodel --output /tmp/native-model.openapi.json
```

An isolated Docker host was built with `--services thingmodel,authn`. Its runtime
configuration explicitly hosts only those services and uses `hosted_gateways: []`.
The default gateway knows every service in the app and requires discovery for
excluded services; it must not be enabled for this narrow monolith. The runtime
was verified to serve the native operations while old model/device APIs
return 404, using the original resource's isolated Encore integration-test DB.
Use `scripts/thingmodel-native-smoke.py` from the repository root for that local,
fixture-only HTTP test; never point the fixture at a customer database.

Encore CLI 1.58.6 loops in `WorkspaceRoot` when `encore build docker` encounters
a Git worktree `.git` file. Build from a normal clone or a source-hash-verified
normal Git snapshot; do not rewrite the user's `.git` file. The macOS Encore
`-race` integration runtime also crashed before tests; normal Encore integration
tests and ordinary Go race tests for core/app passed. A Linux CI race run remains
pending.

## Incomplete release work

This is not the full release. Field-operation commands, field-ID metadata,
presentation/custom-control resources, template binding, archive,
derive, migration/import/export, consumer application, installer upgrades and
the final page switch remain pending. Unsupported metadata is rejected rather
than silently discarded. The first implementation uses complete semantic-model
replacement, not the optional `fieldOperations` command variant.

The host uses the existing auth handler. The DEV candidate now checks each
legacy-jwt request against the configured Community `/api/v1/user/detail`
session endpoint after local signature validation. `TP_COMMUNITY_SESSION_BASE_URL`
must be the trusted internal Community origin (for example `http://backend:9999`).
Missing configuration, revoked sessions, redirects, identity changes, and upstream
errors fail closed. This adapter has no positive cache and does not place user
tables or Redis details in the model core. Cloud tests and real logout/kick-out
HTTP regression remain release gates until their evidence is recorded.
Fixture `TP_AUTH_MODE=dev` is only for isolated loopback testing. Do not ship the
private test runtime configuration or integration-test DB credentials.
