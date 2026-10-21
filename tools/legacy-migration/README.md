# Legacy ThingModel Migration Dry Run

This directory contains the dry-run migration scaffolding for moving legacy ThingsPanel
device template metadata into the Encore `devicemetadata` schema.

Files:

- `migrate_legacy_to_thingmodel.go`: dry-run entrypoint and report writer
- `transform.go`: legacy field to new item transforms, identifier de-duplication, chart config mapping
- `validators.go`: post-migration sampling report helpers

The current implementation is intentionally dry-run friendly:

- reads legacy data through a `LegacyStore` abstraction
- computes renames and chart-config projection in memory
- writes a JSON report for operator review before any real run is attempted
