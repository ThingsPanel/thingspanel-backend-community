package pluginmgr

// inMemoryRepository is a ConnectorRepository backed by in-memory maps.
// It is used in E2E and integration tests to exercise the full service flow
// without a real database. It is NOT used in production.
//
// Thread safety: all methods are guarded by a mutex so tests that run
// concurrent goroutines (e.g. heartbeat) do not race.

import (
	"context"
	"fmt"
	"sync"
)

type inMemoryRepository struct {
	mu sync.Mutex

	connectors map[string]*DeviceConnector
	runtimes   map[string]*ConnectorRuntime
	instances  map[string]*ConnectorInstance
	statuses   map[string]*ConnectorStatus
	events     []ConnectorEvent
	bindings   map[string]*ConnectorBinding
}

func newInMemoryRepository() *inMemoryRepository {
	return &inMemoryRepository{
		connectors: make(map[string]*DeviceConnector),
		runtimes:   make(map[string]*ConnectorRuntime),
		instances:  make(map[string]*ConnectorInstance),
		statuses:   make(map[string]*ConnectorStatus),
		bindings:   make(map[string]*ConnectorBinding),
	}
}

// ─── DeviceConnector ─────────────────────────────────────────────────────────

func (r *inMemoryRepository) CreateDeviceConnector(_ context.Context, dc *DeviceConnector) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.connectors[dc.ID] = dc
	return nil
}

func (r *inMemoryRepository) GetDeviceConnector(_ context.Context, id string) (*DeviceConnector, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	dc, ok := r.connectors[id]
	if !ok {
		return nil, fmt.Errorf("device connector %q not found", id)
	}
	cp := *dc
	return &cp, nil
}

func (r *inMemoryRepository) GetDeviceConnectorByKey(_ context.Context, key string) (*DeviceConnector, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, dc := range r.connectors {
		if dc.ConnectorKey == key {
			cp := *dc
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("device connector with key %q not found", key)
}

func (r *inMemoryRepository) ListDeviceConnectors(_ context.Context, req ListDeviceConnectorsRequest) ([]DeviceConnector, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var all []DeviceConnector
	for _, dc := range r.connectors {
		cp := *dc
		all = append(all, cp)
	}
	total := len(all)
	// Simple pagination.
	page := req.Page
	if page < 1 {
		page = 1
	}
	size := req.PageSize
	if size < 1 {
		size = 20
	}
	start := (page - 1) * size
	if start >= total {
		return nil, total, nil
	}
	end := start + size
	if end > total {
		end = total
	}
	return all[start:end], total, nil
}

func (r *inMemoryRepository) UpdateDeviceConnector(_ context.Context, dc *DeviceConnector) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.connectors[dc.ID]; !ok {
		return fmt.Errorf("device connector %q not found for update", dc.ID)
	}
	r.connectors[dc.ID] = dc
	return nil
}

func (r *inMemoryRepository) DeleteDeviceConnector(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.connectors, id)
	return nil
}

func (r *inMemoryRepository) SetLegacyIDs(_ context.Context, dcID, spID, ppID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	dc, ok := r.connectors[dcID]
	if !ok {
		return fmt.Errorf("device connector %q not found for SetLegacyIDs", dcID)
	}
	dc.LegacyServicePluginID = spID
	dc.LegacyProtocolPluginID = ppID
	return nil
}

// ─── ConnectorRuntime ─────────────────────────────────────────────────────────

func (r *inMemoryRepository) CreateConnectorRuntime(_ context.Context, rt *ConnectorRuntime) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runtimes[rt.ID] = rt
	return nil
}

func (r *inMemoryRepository) GetConnectorRuntime(_ context.Context, id string) (*ConnectorRuntime, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rt, ok := r.runtimes[id]
	if !ok {
		return nil, fmt.Errorf("connector runtime %q not found", id)
	}
	cp := *rt
	return &cp, nil
}

func (r *inMemoryRepository) UpdateConnectorRuntime(_ context.Context, rt *ConnectorRuntime) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runtimes[rt.ID] = rt
	return nil
}

func (r *inMemoryRepository) DeleteConnectorRuntime(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.runtimes, id)
	return nil
}

// ─── ConnectorInstance ────────────────────────────────────────────────────────

func (r *inMemoryRepository) CreateConnectorInstance(_ context.Context, inst *ConnectorInstance) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.instances[inst.ID] = inst
	return nil
}

func (r *inMemoryRepository) GetConnectorInstance(_ context.Context, id string) (*ConnectorInstance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	inst, ok := r.instances[id]
	if !ok {
		return nil, fmt.Errorf("connector instance %q not found", id)
	}
	cp := *inst
	return &cp, nil
}

func (r *inMemoryRepository) UpdateConnectorInstanceHTTPAddress(_ context.Context, id, addr string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	inst, ok := r.instances[id]
	if !ok {
		return fmt.Errorf("connector instance %q not found for http address update", id)
	}
	inst.HTTPAddress = addr
	return nil
}

func (r *inMemoryRepository) DeleteConnectorInstance(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.instances, id)
	return nil
}

// ─── ConnectorStatus ─────────────────────────────────────────────────────────

func (r *inMemoryRepository) UpsertConnectorStatus(_ context.Context, s *ConnectorStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statuses[s.ConnectorInstanceID] = s
	return nil
}

func (r *inMemoryRepository) GetConnectorStatus(_ context.Context, instanceID string) (*ConnectorStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.statuses[instanceID]
	if !ok {
		return nil, fmt.Errorf("connector status for instance %q not found", instanceID)
	}
	cp := *s
	return &cp, nil
}

// ─── ConnectorEvent ───────────────────────────────────────────────────────────

func (r *inMemoryRepository) AppendConnectorEvent(_ context.Context, e *ConnectorEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, *e)
	return nil
}

func (r *inMemoryRepository) ListConnectorEvents(_ context.Context, instanceID string, page, pageSize int) ([]ConnectorEvent, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var filtered []ConnectorEvent
	for _, e := range r.events {
		if e.ConnectorInstanceID == instanceID {
			filtered = append(filtered, e)
		}
	}
	total := len(filtered)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	start := (page - 1) * pageSize
	if start >= total {
		return nil, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return filtered[start:end], total, nil
}

// ─── ConnectorBinding ─────────────────────────────────────────────────────────

func (r *inMemoryRepository) CreateConnectorBinding(_ context.Context, b *ConnectorBinding) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bindings[b.ID] = b
	return nil
}

func (r *inMemoryRepository) GetConnectorBinding(_ context.Context, id string) (*ConnectorBinding, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.bindings[id]
	if !ok {
		return nil, fmt.Errorf("connector binding %q not found", id)
	}
	cp := *b
	return &cp, nil
}

func (r *inMemoryRepository) DeleteConnectorBinding(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.bindings, id)
	return nil
}

func (r *inMemoryRepository) SetBindingLegacyServiceAccessID(_ context.Context, bindingID, saID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.bindings[bindingID]
	if !ok {
		return fmt.Errorf("connector binding %q not found for SetBindingLegacyServiceAccessID", bindingID)
	}
	b.LegacyServiceAccessID = saID
	return nil
}

// ─── recordingBridge ─────────────────────────────────────────────────────────
// recordingBridge wraps noopBridge and records which methods were called.
// Used in E2E tests to verify the bridge projection contract from T000.

type recordingBridge struct {
	mu sync.Mutex
	// calls records method name → arg summary
	calls []bridgeCall

	// Controlled returns for each method (nil = return noop success)
	ProjectConnectorErr          error
	UpdateHTTPAddressErr         error
	UpdateConnectorProjectionErr error
}

type bridgeCall struct {
	method string
	args   []string
}

func newRecordingBridge() *recordingBridge {
	return &recordingBridge{}
}

func (b *recordingBridge) record(method string, args ...string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, bridgeCall{method: method, args: args})
}

func (b *recordingBridge) wasCalled(method string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, c := range b.calls {
		if c.method == method {
			return true
		}
	}
	return false
}

func (b *recordingBridge) lastCallArgs(method string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := len(b.calls) - 1; i >= 0; i-- {
		if b.calls[i].method == method {
			return b.calls[i].args
		}
	}
	return nil
}

func (b *recordingBridge) ProjectConnector(_ context.Context, dc DeviceConnector) (string, string, error) {
	b.record("ProjectConnector", dc.ConnectorKey)
	if b.ProjectConnectorErr != nil {
		return "", "", b.ProjectConnectorErr
	}
	// Noop success: return synthetic legacy IDs for testing.
	return "sp-" + dc.ID[:8], "pp-" + dc.ID[:8], nil
}

func (b *recordingBridge) UpdateHTTPAddress(_ context.Context, connectorKey, httpAddress string) error {
	b.record("UpdateHTTPAddress", connectorKey, httpAddress)
	return b.UpdateHTTPAddressErr
}

func (b *recordingBridge) UpdateConnectorProjection(_ context.Context, dc DeviceConnector) error {
	b.record("UpdateConnectorProjection", dc.ConnectorKey, dc.DisplayName, dc.MQTTTopicPrefix)
	return b.UpdateConnectorProjectionErr
}

func (b *recordingBridge) DeleteConnector(_ context.Context, connectorKey string) error {
	b.record("DeleteConnector", connectorKey)
	return nil
}

func (b *recordingBridge) ProjectBinding(_ context.Context, bnd ConnectorBinding, dc DeviceConnector) (string, error) {
	b.record("ProjectBinding", bnd.DeviceID, dc.ConnectorKey)
	return "sa-" + bnd.ID[:8], nil
}

func (b *recordingBridge) DeleteBinding(_ context.Context, bindingID string) error {
	b.record("DeleteBinding", bindingID)
	return nil
}
