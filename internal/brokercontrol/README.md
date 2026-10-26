# Broker owner service grants

Service credentials are explicit broker-cell and broker-instance allowlist entries. A service grant may subscribe to authenticated ingress or state streams without device targets when it has no publish permissions.

Any service `Policy.Publish` entry requires `AllowedDeviceIDs`. The owner resolves every ID in one tenant-scoped database query and grants only active, enabled devices whose tenant is either listed in `AllowedTenantIDs` or, when that list is empty, equals the service principal's `TenantID`. Device IDs and tenant IDs are literal values; wildcards are rejected. A service grant is limited to 256 targets, duplicate device numbers are rejected, and a grant larger than 64 KiB is denied with `grant_too_large`.

Publish filters must use a finite canonical reply or control family. The owner replaces the device-number level with the resolved target numbers and retains `+` only for message IDs. Custom plugin wire topics are not supported for service publishing. These checks scope each grant to its configured device IDs; they do not establish that a service proxy's broader application behavior enforces tenant isolation outside this broker grant.
