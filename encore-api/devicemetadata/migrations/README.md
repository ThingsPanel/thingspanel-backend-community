This service owns the `device_metadata` PostgreSQL database.

Tables:

- `thing_models`: reusable thing model root records
- `thing_model_items`: property/event/command item definitions
- `thing_model_snapshots`: immutable published versions
- `device_templates`: device model templates bound to a snapshot
- `products`: product view bound to a device template
- `devices`: runtime device instances
- `meta_item_keys`: built-in and tenant metadata dictionary keys
- `thing_model_audit_log`: append-only audit records

Seeds:

- built-in `meta_item_keys` for charting, widgets, units, ranges, icons, and storage policy
