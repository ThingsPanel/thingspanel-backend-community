package pluginmgr

import (
	"context"
	"fmt"
)

// LegacyBridge writes shadow rows to the legacy backend tables so existing
// ThingsPanel backend code (downlink routing, form fetch, heartbeat, disconnect
// callbacks) continues to work without modification during the MVP window.
//
// Rules (from backend-integration-contract.md):
//
//  1. device_connectors row is canonical. Legacy rows are derived.
//  2. connector_key == service_plugins.service_identifier == protocol_plugins.protocol_type.
//  3. mqtt_topic_prefix projected to both sub_topic_prefix fields.
//  4. httpAddress projected to both http_address fields when instance starts.
//  5. Projection failures MUST emit a ConnectorEvent and MUST NOT silently fail.
//
// This file must only reference legacy table concepts in comments and field
// names; public-facing types use DeviceConnector vocabulary throughout.
type LegacyBridge interface {
	// ProjectConnector writes shadow service_plugins and protocol_plugins rows
	// for the given DeviceConnector. Returns legacy IDs on success.
	ProjectConnector(ctx context.Context, dc DeviceConnector) (servicePluginID, protocolPluginID string, err error)

	// UpdateHTTPAddress writes the resolved httpAddress back to the legacy
	// service_plugins.service_config.http_address and protocol_plugins.http_address fields.
	UpdateHTTPAddress(ctx context.Context, connectorKey, httpAddress string) error

	// UpdateConnectorProjection refreshes mutable connector metadata in the
	// legacy projection without changing the resolved runtime httpAddress.
	UpdateConnectorProjection(ctx context.Context, dc DeviceConnector) error

	// DeleteConnector removes shadow rows created by ProjectConnector.
	// Must be idempotent: if rows do not exist, return nil.
	DeleteConnector(ctx context.Context, connectorKey string) error

	// ProjectBinding writes a shadow service_access row for a ConnectorBinding.
	// Returns the service_access row ID on success.
	ProjectBinding(ctx context.Context, b ConnectorBinding, connector DeviceConnector) (serviceAccessID string, err error)

	// DeleteBinding removes the shadow service_access row for the given binding.
	DeleteBinding(ctx context.Context, bindingID string) error
}

// noopBridge is a no-operation bridge used when the real backend client is not
// yet wired (e.g. in unit tests with FakeRunner, or when running without the
// ThingsPanel backend). It logs calls but performs no writes.
//
// Replace with a real implementation that calls the legacy DAL or ThingsPanel
// backend HTTP API when the MVP backend integration is ready.
type noopBridge struct {
	eventSink func(ctx context.Context, e ConnectorEvent)
}

func newNoopBridge() LegacyBridge {
	return &noopBridge{
		eventSink: func(_ context.Context, _ ConnectorEvent) {},
	}
}

func (b *noopBridge) ProjectConnector(ctx context.Context, dc DeviceConnector) (string, string, error) {
	b.eventSink(ctx, ConnectorEvent{
		ConnectorInstanceID: "",
		Reason:              ReasonBridgeProjectionFailed,
		Message:             fmt.Sprintf("noopBridge: ProjectConnector called for %q — no-op, replace with real bridge", dc.ConnectorKey),
	})
	// Return empty IDs — caller must tolerate empty strings in noop mode.
	return "", "", nil
}

func (b *noopBridge) UpdateHTTPAddress(_ context.Context, connectorKey, httpAddress string) error {
	_ = connectorKey
	_ = httpAddress
	// No-op: in real implementation, this updates service_plugins.service_config.http_address
	// and protocol_plugins.http_address for the row with service_identifier == connectorKey.
	return nil
}

func (b *noopBridge) UpdateConnectorProjection(_ context.Context, dc DeviceConnector) error {
	_ = dc
	return nil
}

func (b *noopBridge) DeleteConnector(_ context.Context, _ string) error {
	return nil
}

func (b *noopBridge) ProjectBinding(_ context.Context, _ ConnectorBinding, _ DeviceConnector) (string, error) {
	// No-op: in real implementation, creates a service_access row and updates
	// devices.service_access_id to point to it.
	return "", nil
}

func (b *noopBridge) DeleteBinding(_ context.Context, _ string) error {
	return nil
}

// ─── Bridge projection spec (reference) ─────────────────────────────────────
//
// ProjectConnector MUST:
//   - INSERT service_plugins row with:
//       service_identifier = dc.ConnectorKey
//       service_type       = 1 (if dc.DeviceTypeSupport is "direct" or "gateway") or 2
//       service_config     = JSON: {http_address:"", sub_topic_prefix:dc.MQTTTopicPrefix}
//       name               = dc.DisplayName
//   - INSERT protocol_plugins row with:
//       protocol_type     = dc.ConnectorKey
//       sub_topic_prefix  = dc.MQTTTopicPrefix
//       http_address      = "" (filled by UpdateHTTPAddress when instance starts)
//       device_type       = 1 for "direct", 2 for "gateway" / "both"
//   - INSERT sys_dict row with:
//       dict_code  = "DRIECT_ATTACHED_PROTOCOL" (device_type=1) or "GATEWAY_PROTOCOL"
//       dict_value = dc.ConnectorKey
//   - INSERT sys_dict_language row with:
//       dict_id        = above sys_dict row id
//       language_code  = "zh-CN" (default)
//       translation    = dc.DisplayName
//   - Return the service_plugins.id and protocol_plugins.id.
//
// All inserts must be in a single DB transaction. On failure, emit a
// ConnectorEvent with reason="BridgeProjectionFailed" before returning error.
//
// ProjectBinding MUST:
//   - INSERT service_access row with:
//       service_plugin_id      = legacy_service_plugin_id from DeviceConnector
//       voucher                = b.Voucher
//       tenant_id              = (derived from ConnectorBinding or auth context)
//       service_access_config  = {}
//   - UPDATE devices SET service_access_id = new_service_access_id WHERE id = b.DeviceID
//   - Return new_service_access_id.
