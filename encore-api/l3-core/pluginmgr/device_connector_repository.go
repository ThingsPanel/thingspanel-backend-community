package pluginmgr

import "context"

// ConnectorRepository is the storage interface for DeviceConnector resources.
// Implementations must not expose legacy table structure.
// All bridge/projection writes are the responsibility of LegacyBridge, not this interface.
type ConnectorRepository interface {
	// ── DeviceConnector ──────────────────────────────────────────────────────

	CreateDeviceConnector(ctx context.Context, dc *DeviceConnector) error
	GetDeviceConnector(ctx context.Context, id string) (*DeviceConnector, error)
	GetDeviceConnectorByKey(ctx context.Context, connectorKey string) (*DeviceConnector, error)
	UpdateDeviceConnector(ctx context.Context, dc *DeviceConnector) error
	DeleteDeviceConnector(ctx context.Context, id string) error
	ListDeviceConnectors(ctx context.Context, filter ListDeviceConnectorsRequest) ([]DeviceConnector, int, error)

	// SetLegacyIDs records the bridge projection back-references.
	SetLegacyIDs(ctx context.Context, id string, servicePluginID string, protocolPluginID string) error

	// ── ConnectorRuntime ─────────────────────────────────────────────────────

	CreateConnectorRuntime(ctx context.Context, r *ConnectorRuntime) error
	GetConnectorRuntime(ctx context.Context, id string) (*ConnectorRuntime, error)

	// ── ConnectorInstance ────────────────────────────────────────────────────

	CreateConnectorInstance(ctx context.Context, i *ConnectorInstance) error
	GetConnectorInstance(ctx context.Context, id string) (*ConnectorInstance, error)
	UpdateConnectorInstanceHTTPAddress(ctx context.Context, id string, httpAddress string) error
	DeleteConnectorInstance(ctx context.Context, id string) error

	// ── ConnectorStatus ──────────────────────────────────────────────────────

	UpsertConnectorStatus(ctx context.Context, s *ConnectorStatus) error
	GetConnectorStatus(ctx context.Context, instanceID string) (*ConnectorStatus, error)

	// ── ConnectorEvent ───────────────────────────────────────────────────────

	AppendConnectorEvent(ctx context.Context, e *ConnectorEvent) error
	ListConnectorEvents(ctx context.Context, instanceID string, page, pageSize int) ([]ConnectorEvent, int, error)

	// ── ConnectorBinding ─────────────────────────────────────────────────────

	CreateConnectorBinding(ctx context.Context, b *ConnectorBinding) error
	GetConnectorBinding(ctx context.Context, id string) (*ConnectorBinding, error)
	DeleteConnectorBinding(ctx context.Context, id string) error
	SetBindingLegacyServiceAccessID(ctx context.Context, bindingID string, serviceAccessID string) error
}

// noopRepository is a placeholder implementation used before the real DB
// repository is wired. It returns ErrNotImplemented for all operations.
// Replace with the real DB-backed implementation in T005.
type noopRepository struct{}

func newNoopRepository() ConnectorRepository { return &noopRepository{} }

func (*noopRepository) CreateDeviceConnector(_ context.Context, _ *DeviceConnector) error {
	return errNotImplemented("CreateDeviceConnector")
}
func (*noopRepository) GetDeviceConnector(_ context.Context, _ string) (*DeviceConnector, error) {
	return nil, errNotImplemented("GetDeviceConnector")
}
func (*noopRepository) GetDeviceConnectorByKey(_ context.Context, _ string) (*DeviceConnector, error) {
	return nil, errNotImplemented("GetDeviceConnectorByKey")
}
func (*noopRepository) UpdateDeviceConnector(_ context.Context, _ *DeviceConnector) error {
	return errNotImplemented("UpdateDeviceConnector")
}
func (*noopRepository) DeleteDeviceConnector(_ context.Context, _ string) error {
	return errNotImplemented("DeleteDeviceConnector")
}
func (*noopRepository) ListDeviceConnectors(_ context.Context, _ ListDeviceConnectorsRequest) ([]DeviceConnector, int, error) {
	return nil, 0, errNotImplemented("ListDeviceConnectors")
}
func (*noopRepository) SetLegacyIDs(_ context.Context, _, _, _ string) error {
	return errNotImplemented("SetLegacyIDs")
}
func (*noopRepository) CreateConnectorRuntime(_ context.Context, _ *ConnectorRuntime) error {
	return errNotImplemented("CreateConnectorRuntime")
}
func (*noopRepository) GetConnectorRuntime(_ context.Context, _ string) (*ConnectorRuntime, error) {
	return nil, errNotImplemented("GetConnectorRuntime")
}
func (*noopRepository) CreateConnectorInstance(_ context.Context, _ *ConnectorInstance) error {
	return errNotImplemented("CreateConnectorInstance")
}
func (*noopRepository) GetConnectorInstance(_ context.Context, _ string) (*ConnectorInstance, error) {
	return nil, errNotImplemented("GetConnectorInstance")
}
func (*noopRepository) UpdateConnectorInstanceHTTPAddress(_ context.Context, _, _ string) error {
	return errNotImplemented("UpdateConnectorInstanceHTTPAddress")
}
func (*noopRepository) DeleteConnectorInstance(_ context.Context, _ string) error {
	return errNotImplemented("DeleteConnectorInstance")
}
func (*noopRepository) UpsertConnectorStatus(_ context.Context, _ *ConnectorStatus) error {
	return errNotImplemented("UpsertConnectorStatus")
}
func (*noopRepository) GetConnectorStatus(_ context.Context, _ string) (*ConnectorStatus, error) {
	return nil, errNotImplemented("GetConnectorStatus")
}
func (*noopRepository) AppendConnectorEvent(_ context.Context, _ *ConnectorEvent) error {
	return errNotImplemented("AppendConnectorEvent")
}
func (*noopRepository) ListConnectorEvents(_ context.Context, _ string, _, _ int) ([]ConnectorEvent, int, error) {
	return nil, 0, errNotImplemented("ListConnectorEvents")
}
func (*noopRepository) CreateConnectorBinding(_ context.Context, _ *ConnectorBinding) error {
	return errNotImplemented("CreateConnectorBinding")
}
func (*noopRepository) GetConnectorBinding(_ context.Context, _ string) (*ConnectorBinding, error) {
	return nil, errNotImplemented("GetConnectorBinding")
}
func (*noopRepository) DeleteConnectorBinding(_ context.Context, _ string) error {
	return errNotImplemented("DeleteConnectorBinding")
}
func (*noopRepository) SetBindingLegacyServiceAccessID(_ context.Context, _, _ string) error {
	return errNotImplemented("SetBindingLegacyServiceAccessID")
}
