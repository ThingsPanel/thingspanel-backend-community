package pluginmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// DeviceConnectorService implements the DeviceConnector control plane.
// It owns DeviceConnector / ConnectorRuntime / ConnectorInstance / ConnectorBinding
// resources and coordinates with LegacyBridge and Runner.
//
// IMPORTANT: This is NOT a separate Encore service. It runs inside the existing
// pluginmgr.Service to avoid a package rename during MVP. See ADR.
// The existing InstallProtocolPlugin behavior is unchanged.
type DeviceConnectorService struct {
	repo   ConnectorRepository
	bridge LegacyBridge
	runner Runner
}

func newStandaloneService() *Service {
	return &Service{}
}

func CreateDeviceConnectorForDeploy(ctx context.Context, req *CreateDeviceConnectorRequest) (*DeviceConnector, error) {
	return newStandaloneService().CreateDeviceConnector(ctx, req)
}

func ListDeviceConnectorsForDeploy(ctx context.Context, req *ListDeviceConnectorsRequest) (*DeviceConnectorListData, error) {
	return newStandaloneService().ListDeviceConnectors(ctx, req)
}

func CreateConnectorRuntimeForDeploy(ctx context.Context, req *CreateConnectorRuntimeRequest) (*ConnectorRuntime, error) {
	return newStandaloneService().CreateConnectorRuntime(ctx, req)
}

func CreateConnectorInstanceForDeploy(ctx context.Context, req *CreateConnectorInstanceRequest) (*ConnectorInstance, error) {
	return newStandaloneService().CreateConnectorInstance(ctx, req)
}

func UpdateDeviceConnectorForDeploy(ctx context.Context, id string, req *UpdateDeviceConnectorRequest) (*DeviceConnector, error) {
	return newStandaloneService().UpdateDeviceConnector(ctx, id, req)
}

// NewDeviceConnectorService creates a service with the given dependencies.
// Use NewDeviceConnectorService(newNoopRepository(), newNoopBridge(), NewFakeRunner())
// in tests.
func NewDeviceConnectorService(repo ConnectorRepository, bridge LegacyBridge, runner Runner) *DeviceConnectorService {
	return &DeviceConnectorService{
		repo:   repo,
		bridge: bridge,
		runner: runner,
	}
}

func normalizeAdditionalInfo(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) {
		return nil
	}
	return raw
}

// ─── DeviceConnector CRUD ────────────────────────────────────────────────────

// CreateDeviceConnector registers a new DeviceConnector and writes bridge
// projection rows to legacy backend tables.
//
//encore:api auth method=POST path=/v1/device-connectors
func (s *Service) CreateDeviceConnector(ctx context.Context, req *CreateDeviceConnectorRequest) (*DeviceConnector, error) {
	if err := validateCreateDeviceConnectorRequest(req); err != nil {
		return nil, err
	}

	svc := s.connectorService()
	return svc.create(ctx, req)
}

func (s *DeviceConnectorService) create(ctx context.Context, req *CreateDeviceConnectorRequest) (*DeviceConnector, error) {
	httpCB := true
	if req.HTTPCallbacksEnabled != nil {
		httpCB = *req.HTTPCallbacksEnabled
	}

	now := time.Now().UTC()
	dc := &DeviceConnector{
		ID:                   newID(),
		ConnectorKey:         strings.TrimSpace(req.ConnectorKey),
		DisplayName:          strings.TrimSpace(req.DisplayName),
		MQTTTopicPrefix:      req.MQTTTopicPrefix,
		DeviceTypeSupport:    req.DeviceTypeSupport,
		HTTPCallbacksEnabled: httpCB,
		Description:          req.Description,
		AdditionalInfo:       normalizeAdditionalInfo(req.AdditionalInfo),
		CreatedAt:            now,
		UpdatedAt:            now,
	}

	if err := s.repo.CreateDeviceConnector(ctx, dc); err != nil {
		return nil, fmt.Errorf("create device_connectors row: %w", err)
	}

	// Bridge projection: write shadow rows to legacy backend tables.
	// Failures are recorded as events but do not roll back the canonical row.
	spID, ppID, err := s.bridge.ProjectConnector(ctx, *dc)
	if err != nil {
		_ = s.repo.AppendConnectorEvent(ctx, &ConnectorEvent{
			ID:                  newID(),
			ConnectorInstanceID: "", // no instance yet
			Reason:              ReasonBridgeProjectionFailed,
			Message:             err.Error(),
			OccurredAt:          time.Now().UTC(),
		})
		// Non-fatal: canonical row exists; bridge can be retried.
	}

	if spID != "" || ppID != "" {
		_ = s.repo.SetLegacyIDs(ctx, dc.ID, spID, ppID)
		dc.LegacyServicePluginID = spID
		dc.LegacyProtocolPluginID = ppID
	}

	return dc, nil
}

// GetDeviceConnector returns a DeviceConnector by ID.
//
//encore:api auth method=GET path=/v1/device-connectors/:id
func (s *Service) GetDeviceConnector(ctx context.Context, id string) (*DeviceConnector, error) {
	return s.connectorService().get(ctx, id)
}

func (s *DeviceConnectorService) get(ctx context.Context, id string) (*DeviceConnector, error) {
	dc, err := s.repo.GetDeviceConnector(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get device connector %q: %w", id, err)
	}
	return dc, nil
}

// ListDeviceConnectors returns a paginated list of DeviceConnectors.
//
//encore:api auth method=GET path=/v1/device-connectors
func (s *Service) ListDeviceConnectors(ctx context.Context, req *ListDeviceConnectorsRequest) (*DeviceConnectorListData, error) {
	if req == nil {
		req = &ListDeviceConnectorsRequest{}
	}
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}

	list, total, err := s.connectorService().repo.ListDeviceConnectors(ctx, *req)
	if err != nil {
		return nil, fmt.Errorf("list device connectors: %w", err)
	}

	return &DeviceConnectorListData{Total: total, List: list}, nil
}

// UpdateDeviceConnector updates displayName, mqttTopicPrefix, and description.
// connector_key is immutable.
//
//encore:api auth method=PUT path=/v1/device-connectors/:id
func (s *Service) UpdateDeviceConnector(ctx context.Context, id string, req *UpdateDeviceConnectorRequest) (*DeviceConnector, error) {
	return s.connectorService().updateConnector(ctx, id, req)
}

func (s *DeviceConnectorService) updateConnector(ctx context.Context, id string, req *UpdateDeviceConnectorRequest) (*DeviceConnector, error) {
	dc, err := s.repo.GetDeviceConnector(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get device connector for update: %w", err)
	}

	if req.DisplayName != "" {
		dc.DisplayName = req.DisplayName
	}
	if req.MQTTTopicPrefix != "" {
		dc.MQTTTopicPrefix = req.MQTTTopicPrefix
	}
	if req.Description != "" {
		dc.Description = req.Description
	}
	if req.AdditionalInfo != nil {
		dc.AdditionalInfo = normalizeAdditionalInfo(req.AdditionalInfo)
	}
	dc.UpdatedAt = time.Now().UTC()

	if err := s.repo.UpdateDeviceConnector(ctx, dc); err != nil {
		return nil, fmt.Errorf("update device connector: %w", err)
	}

	if bridgeErr := s.bridge.UpdateConnectorProjection(ctx, *dc); bridgeErr != nil {
		_ = s.repo.AppendConnectorEvent(ctx, &ConnectorEvent{
			ID:         newID(),
			Reason:     ReasonBridgeProjectionFailed,
			Message:    "UpdateDeviceConnector bridge re-projection: " + bridgeErr.Error(),
			OccurredAt: time.Now().UTC(),
		})
	}

	return dc, nil
}

// DeleteDeviceConnector deletes a DeviceConnector and its legacy bridge rows.
// Returns an error if any ConnectorInstance is Running.
//
//encore:api auth method=DELETE path=/v1/device-connectors/:id
func (s *Service) DeleteDeviceConnector(ctx context.Context, id string) error {
	svc := s.connectorService()

	dc, err := svc.repo.GetDeviceConnector(ctx, id)
	if err != nil {
		return fmt.Errorf("get device connector for delete: %w", err)
	}

	if err := svc.repo.DeleteDeviceConnector(ctx, id); err != nil {
		return fmt.Errorf("delete device connector: %w", err)
	}

	if bridgeErr := svc.bridge.DeleteConnector(ctx, dc.ConnectorKey); bridgeErr != nil {
		_ = svc.repo.AppendConnectorEvent(ctx, &ConnectorEvent{
			ID:         newID(),
			Reason:     ReasonBridgeProjectionFailed,
			Message:    "DeleteDeviceConnector bridge cleanup: " + bridgeErr.Error(),
			OccurredAt: time.Now().UTC(),
		})
	}

	return nil
}

// ─── ConnectorRuntime ────────────────────────────────────────────────────────

// CreateConnectorRuntime creates a new runtime specification for a DeviceConnector.
//
//encore:api auth method=POST path=/v1/connector-runtimes
func (s *Service) CreateConnectorRuntime(ctx context.Context, req *CreateConnectorRuntimeRequest) (*ConnectorRuntime, error) {
	if req == nil || strings.TrimSpace(req.DeviceConnectorID) == "" {
		return nil, fmt.Errorf("deviceConnectorId is required")
	}
	if strings.TrimSpace(req.Image) == "" {
		return nil, fmt.Errorf("image is required")
	}

	// Verify parent exists.
	if _, err := s.connectorService().repo.GetDeviceConnector(ctx, req.DeviceConnectorID); err != nil {
		return nil, fmt.Errorf("device connector %q not found: %w", req.DeviceConnectorID, err)
	}

	hp := req.HTTPPort
	hcp := strings.TrimSpace(req.HealthCheckPath)
	if hcp == "" {
		hcp = "/health"
	}

	now := time.Now().UTC()
	r := &ConnectorRuntime{
		ID:                newID(),
		DeviceConnectorID: req.DeviceConnectorID,
		Image:             strings.TrimSpace(req.Image),
		EnvVars:           req.EnvVars,
		HTTPPort:          hp,
		HealthCheckPath:   hcp,
		ResourceLimits:    req.ResourceLimits,
		Description:       req.Description,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	if err := s.connectorService().repo.CreateConnectorRuntime(ctx, r); err != nil {
		return nil, fmt.Errorf("create connector runtime: %w", err)
	}

	return r, nil
}

// GetConnectorRuntime returns a ConnectorRuntime by ID.
//
//encore:api auth method=GET path=/v1/connector-runtimes/:id
func (s *Service) GetConnectorRuntime(ctx context.Context, id string) (*ConnectorRuntime, error) {
	r, err := s.connectorService().repo.GetConnectorRuntime(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get connector runtime %q: %w", id, err)
	}
	return r, nil
}

// ─── ConnectorInstance ───────────────────────────────────────────────────────

// CreateConnectorInstance launches a new ConnectorInstance for a given runtime.
// Calls the Runner to reconcile, then projects the resolved httpAddress to legacy tables.
//
//encore:api auth method=POST path=/v1/connector-instances
func (s *Service) CreateConnectorInstance(ctx context.Context, req *CreateConnectorInstanceRequest) (*ConnectorInstance, error) {
	if req == nil || strings.TrimSpace(req.ConnectorRuntimeID) == "" {
		return nil, fmt.Errorf("connectorRuntimeId is required")
	}

	svc := s.connectorService()

	rt, err := svc.repo.GetConnectorRuntime(ctx, req.ConnectorRuntimeID)
	if err != nil {
		return nil, fmt.Errorf("get connector runtime: %w", err)
	}

	dc, err := svc.repo.GetDeviceConnector(ctx, rt.DeviceConnectorID)
	if err != nil {
		return nil, fmt.Errorf("get device connector: %w", err)
	}

	ns := strings.TrimSpace(req.Namespace)
	if ns == "" {
		ns = "connector-instances"
	}

	now := time.Now().UTC()
	instance := &ConnectorInstance{
		ID:                 newID(),
		ConnectorRuntimeID: rt.ID,
		DeviceConnectorID:  dc.ID,
		Namespace:          ns,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	if err := svc.repo.CreateConnectorInstance(ctx, instance); err != nil {
		return nil, fmt.Errorf("create connector instance row: %w", err)
	}

	// Record initial Pending status.
	_ = svc.repo.UpsertConnectorStatus(ctx, &ConnectorStatus{
		ConnectorInstanceID: instance.ID,
		Phase:               PhasePending,
		ProcessHealthy:      false,
		UpdatedAt:           time.Now().UTC(),
	})

	// Call runner to start the instance.
	status, runErr := svc.runner.Reconcile(ctx, *instance, *rt)
	if runErr != nil {
		// Record Error status and event.
		_ = svc.repo.UpsertConnectorStatus(ctx, &ConnectorStatus{
			ConnectorInstanceID: instance.ID,
			Phase:               PhaseError,
			ProcessHealthy:      false,
			Message:             runErr.Error(),
			UpdatedAt:           time.Now().UTC(),
		})
		_ = svc.repo.AppendConnectorEvent(ctx, &ConnectorEvent{
			ID:                  newID(),
			ConnectorInstanceID: instance.ID,
			Reason:              ReasonReconcileError,
			Message:             runErr.Error(),
			OccurredAt:          time.Now().UTC(),
		})
		return instance, nil // Return instance so caller can inspect events.
	}

	// Record Running status.
	_ = svc.repo.UpsertConnectorStatus(ctx, &status)
	_ = svc.repo.AppendConnectorEvent(ctx, &ConnectorEvent{
		ID:                  newID(),
		ConnectorInstanceID: instance.ID,
		Reason:              ReasonStarted,
		Message:             status.Message,
		OccurredAt:          time.Now().UTC(),
	})

	// Resolve HTTP address and project to legacy tables.
	if status.HTTPAddress != "" {
		instance.HTTPAddress = status.HTTPAddress
	} else if fakeR, ok := svc.runner.(*FakeRunner); ok {
		instance.HTTPAddress = fakeR.FakeHTTPAddressFor(instance.ID)
	}
	if instance.HTTPAddress != "" {
		_ = svc.repo.UpdateConnectorInstanceHTTPAddress(ctx, instance.ID, instance.HTTPAddress)
		if bridgeErr := svc.bridge.UpdateHTTPAddress(ctx, dc.ConnectorKey, instance.HTTPAddress); bridgeErr != nil {
			_ = svc.repo.AppendConnectorEvent(ctx, &ConnectorEvent{
				ID:                  newID(),
				ConnectorInstanceID: instance.ID,
				Reason:              ReasonBridgeProjectionFailed,
				Message:             "UpdateHTTPAddress: " + bridgeErr.Error(),
				OccurredAt:          time.Now().UTC(),
			})
		}
	}

	return instance, nil
}

// GetConnectorInstance returns a ConnectorInstance by ID.
//
//encore:api auth method=GET path=/v1/connector-instances/:id
func (s *Service) GetConnectorInstance(ctx context.Context, id string) (*ConnectorInstance, error) {
	i, err := s.connectorService().repo.GetConnectorInstance(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get connector instance %q: %w", id, err)
	}
	return i, nil
}

// DeleteConnectorInstance stops and removes a ConnectorInstance.
//
//encore:api auth method=DELETE path=/v1/connector-instances/:id
func (s *Service) DeleteConnectorInstance(ctx context.Context, id string) error {
	svc := s.connectorService()

	if runErr := svc.runner.Delete(ctx, id); runErr != nil {
		_ = svc.repo.AppendConnectorEvent(ctx, &ConnectorEvent{
			ID:                  newID(),
			ConnectorInstanceID: id,
			Reason:              ReasonReconcileError,
			Message:             "Delete runner: " + runErr.Error(),
			OccurredAt:          time.Now().UTC(),
		})
	}

	_ = svc.repo.UpsertConnectorStatus(ctx, &ConnectorStatus{
		ConnectorInstanceID: id,
		Phase:               PhaseStopped,
		ProcessHealthy:      false,
		Message:             "instance deleted",
		UpdatedAt:           time.Now().UTC(),
	})
	_ = svc.repo.AppendConnectorEvent(ctx, &ConnectorEvent{
		ID:                  newID(),
		ConnectorInstanceID: id,
		Reason:              ReasonStopped,
		OccurredAt:          time.Now().UTC(),
	})

	return svc.repo.DeleteConnectorInstance(ctx, id)
}

// ─── ConnectorStatus / Events ────────────────────────────────────────────────

// GetConnectorInstanceStatus returns the observed status of a ConnectorInstance.
//
//encore:api auth method=GET path=/v1/connector-instances/:id/status
func (s *Service) GetConnectorInstanceStatus(ctx context.Context, id string) (*ConnectorStatus, error) {
	status, err := s.connectorService().repo.GetConnectorStatus(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get connector status for %q: %w", id, err)
	}
	return status, nil
}

// ListConnectorInstanceEvents returns lifecycle and diagnostic events.
//
//encore:api auth method=GET path=/v1/connector-instances/:id/events
func (s *Service) ListConnectorInstanceEvents(ctx context.Context, id string, req *ListConnectorEventsRequest) (*ConnectorEventListData, error) {
	if req == nil {
		req = &ListConnectorEventsRequest{}
	}
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}

	events, total, err := s.connectorService().repo.ListConnectorEvents(ctx, id, req.Page, req.PageSize)
	if err != nil {
		return nil, fmt.Errorf("list connector events for %q: %w", id, err)
	}

	return &ConnectorEventListData{Total: total, List: events}, nil
}

// ─── ConnectorBinding ────────────────────────────────────────────────────────

// CreateConnectorBinding binds a device to a ConnectorInstance.
//
//encore:api auth method=POST path=/v1/connector-bindings
func (s *Service) CreateConnectorBinding(ctx context.Context, req *CreateConnectorBindingRequest) (*ConnectorBinding, error) {
	if req == nil || strings.TrimSpace(req.ConnectorInstanceID) == "" {
		return nil, fmt.Errorf("connectorInstanceId is required")
	}
	if strings.TrimSpace(req.DeviceID) == "" {
		return nil, fmt.Errorf("deviceId is required")
	}

	svc := s.connectorService()

	instance, err := svc.repo.GetConnectorInstance(ctx, req.ConnectorInstanceID)
	if err != nil {
		return nil, fmt.Errorf("get connector instance: %w", err)
	}

	dc, err := svc.repo.GetDeviceConnector(ctx, instance.DeviceConnectorID)
	if err != nil {
		return nil, fmt.Errorf("get device connector: %w", err)
	}

	b := &ConnectorBinding{
		ID:                  newID(),
		ConnectorInstanceID: req.ConnectorInstanceID,
		DeviceConnectorID:   dc.ID,
		DeviceID:            req.DeviceID,
		Voucher:             req.Voucher,
		CreatedAt:           time.Now().UTC(),
	}

	if err := svc.repo.CreateConnectorBinding(ctx, b); err != nil {
		return nil, fmt.Errorf("create connector binding: %w", err)
	}

	// Bridge: create shadow service_access row.
	saID, bridgeErr := svc.bridge.ProjectBinding(ctx, *b, *dc)
	if bridgeErr != nil {
		_ = svc.repo.AppendConnectorEvent(ctx, &ConnectorEvent{
			ID:                  newID(),
			ConnectorInstanceID: b.ConnectorInstanceID,
			Reason:              ReasonBridgeProjectionFailed,
			Message:             "ProjectBinding: " + bridgeErr.Error(),
			OccurredAt:          time.Now().UTC(),
		})
	}
	if saID != "" {
		_ = svc.repo.SetBindingLegacyServiceAccessID(ctx, b.ID, saID)
		b.LegacyServiceAccessID = saID
	}

	return b, nil
}

// DeleteConnectorBinding removes a binding and its legacy shadow row.
//
//encore:api auth method=DELETE path=/v1/connector-bindings/:id
func (s *Service) DeleteConnectorBinding(ctx context.Context, id string) error {
	svc := s.connectorService()

	if bridgeErr := svc.bridge.DeleteBinding(ctx, id); bridgeErr != nil {
		_ = svc.repo.AppendConnectorEvent(ctx, &ConnectorEvent{
			ID:         newID(),
			Reason:     ReasonBridgeProjectionFailed,
			Message:    "DeleteBinding: " + bridgeErr.Error(),
			OccurredAt: time.Now().UTC(),
		})
	}

	return svc.repo.DeleteConnectorBinding(ctx, id)
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// connectorService returns a DeviceConnectorService bound to this Service's
// dependencies. In MVP these are the noop implementations; swap with real ones
// by constructing and storing a *DeviceConnectorService on Service at init time.
func (s *Service) connectorService() *DeviceConnectorService {
	return defaultConnectorService()
}

var (
	defaultConnectorServiceOnce sync.Once
	defaultConnectorServiceInst *DeviceConnectorService
)

func defaultConnectorService() *DeviceConnectorService {
	defaultConnectorServiceOnce.Do(func() {
		db, err := openDeviceConnectorDB()
		if err != nil {
			defaultConnectorServiceInst = NewDeviceConnectorService(
				newNoopRepository(),
				newNoopBridge(),
				NewFakeRunner(),
			)
			return
		}

		runner := Runner(NewFakeRunner())
		if strings.EqualFold(strings.TrimSpace(os.Getenv("CONNECTOR_RUNNER")), "k8s") {
			runner = NewK8SRunner(os.Getenv("CONNECTOR_E2E_KUBECONFIG"))
		}

		defaultConnectorServiceInst = NewDeviceConnectorService(
			newPostgresConnectorRepository(db),
			newPostgresLegacyBridge(db),
			runner,
		)
	})
	return defaultConnectorServiceInst
}

func validateCreateDeviceConnectorRequest(req *CreateDeviceConnectorRequest) error {
	if req == nil {
		return fmt.Errorf("request is required")
	}
	if strings.TrimSpace(req.ConnectorKey) == "" {
		return fmt.Errorf("connectorKey is required")
	}
	if strings.TrimSpace(req.DisplayName) == "" {
		return fmt.Errorf("displayName is required")
	}
	if strings.TrimSpace(req.MQTTTopicPrefix) == "" {
		return fmt.Errorf("mqttTopicPrefix is required")
	}
	switch req.DeviceTypeSupport {
	case "direct", "gateway", "both":
		// valid
	default:
		return fmt.Errorf("deviceTypeSupport must be one of: direct, gateway, both")
	}
	return nil
}

func newID() string {
	return uuid.New().String()
}

func errNotImplemented(op string) error {
	return fmt.Errorf("%s: not implemented — wire a real repository implementation", op)
}
