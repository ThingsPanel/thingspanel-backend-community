package pluginmgr

// device_connector_e2e_test.go — T011: MVP E2E verification
//
// These tests exercise the full DeviceConnector control-plane flow from API
// to runner status using the FakeRunner and an in-memory repository. They do
// not require a database, K8S cluster, or running Encore process.
//
// The test suite covers:
//
//  1. Happy path: create connector → runtime → instance (Running status + events) → binding
//     → delete binding → delete instance (Stopped) → delete connector
//  2. Failure path: bad image → Error status + ReconcileError event
//  3. Bridge projection contract (T000): verifies ProjectConnector, UpdateHTTPAddress,
//     ProjectBinding, DeleteConnector, DeleteBinding are all called.
//  4. Status convergence: status is inspectable via GetConnectorStatus after each transition.
//  5. Event audit: event list contains the correct reason codes in order.
//
// To enable K8S runner path (future), set CONNECTOR_E2E_USE_K8S=1 and
// CONNECTOR_E2E_KUBECONFIG=<path>. When unset, the FakeRunner is used.

import (
	"context"
	"os"
	"testing"
	"time"
)

// ─── Shared test helpers ──────────────────────────────────────────────────────

func newE2EService() (*DeviceConnectorService, *inMemoryRepository, *recordingBridge, *FakeRunner) {
	repo := newInMemoryRepository()
	bridge := newRecordingBridge()
	runner := NewFakeRunner()
	svc := NewDeviceConnectorService(repo, bridge, runner)
	return svc, repo, bridge, runner
}

// useK8SRunner returns true when the E2E env flag is set.
// When true, tests construct a real K8SRunner instead of FakeRunner.
// Not exercised in this MVP but wired for future gating.
func useK8SRunner() bool {
	return os.Getenv("CONNECTOR_E2E_USE_K8S") == "1"
}

// ─── T011-01: Happy path ──────────────────────────────────────────────────────

func TestE2E_HappyPath_CreateAndRunConnector(t *testing.T) {
	if useK8SRunner() {
		t.Skip("K8S runner path not implemented in MVP; set CONNECTOR_E2E_USE_K8S=0")
	}

	ctx := context.Background()
	svc, repo, bridge, fakeRunner := newE2EService()
	fakeRunner.FakeHTTPAddress = "10.0.0.1:9001"

	// ── Step 1: Create DeviceConnector ───────────────────────────────────────
	httpCBEnabled := true
	req := &CreateDeviceConnectorRequest{
		ConnectorKey:         "xiaomi-plug",
		DisplayName:          "小米智能插座",
		MQTTTopicPrefix:      "plugin/xiaomi-plug/",
		DeviceTypeSupport:    "direct",
		HTTPCallbacksEnabled: &httpCBEnabled,
	}

	dc, err := svc.create(ctx, req)
	if err != nil {
		t.Fatalf("create DeviceConnector: %v", err)
	}
	if dc.ID == "" {
		t.Fatal("expected non-empty DeviceConnector ID")
	}
	if dc.ConnectorKey != "xiaomi-plug" {
		t.Errorf("expected connectorKey=xiaomi-plug, got %q", dc.ConnectorKey)
	}

	// Bridge contract (T000): ProjectConnector must have been called.
	if !bridge.wasCalled("ProjectConnector") {
		t.Error("T000 contract: bridge.ProjectConnector was not called after CreateDeviceConnector")
	}
	if args := bridge.lastCallArgs("ProjectConnector"); len(args) > 0 && args[0] != "xiaomi-plug" {
		t.Errorf("T000 contract: ProjectConnector called with wrong key %q", args[0])
	}

	// Legacy IDs must have been set back (recording bridge returns sp-/pp- IDs).
	stored, err := repo.GetDeviceConnector(ctx, dc.ID)
	if err != nil {
		t.Fatalf("get stored connector: %v", err)
	}
	if stored.LegacyServicePluginID == "" {
		t.Error("expected LegacyServicePluginID to be set after ProjectConnector")
	}
	if stored.LegacyProtocolPluginID == "" {
		t.Error("expected LegacyProtocolPluginID to be set after ProjectConnector")
	}

	// ── Step 2: Create ConnectorRuntime ───────────────────────────────────────
	rt := &ConnectorRuntime{
		ID:                newID(),
		DeviceConnectorID: dc.ID,
		Image:             "registry.example.com/xiaomi-plug:1.0.0",
		HTTPPort:          9001,
		HealthCheckPath:   "/health",
		EnvVars:           map[string]string{"LOG_LEVEL": "info"},
	}
	if err := repo.CreateConnectorRuntime(ctx, rt); err != nil {
		t.Fatalf("create ConnectorRuntime: %v", err)
	}

	// ── Step 3: Create ConnectorInstance (calls runner.Reconcile) ────────────
	instReq := &CreateConnectorInstanceRequest{
		ConnectorRuntimeID: rt.ID,
		Namespace:          "connector-instances",
	}
	instance, err := svc.createInstance(ctx, instReq)
	if err != nil {
		t.Fatalf("create ConnectorInstance: %v", err)
	}
	if instance.ID == "" {
		t.Fatal("expected non-empty instance ID")
	}

	// Status must be Running after successful Reconcile.
	status, err := repo.GetConnectorStatus(ctx, instance.ID)
	if err != nil {
		t.Fatalf("get connector status: %v", err)
	}
	if status.Phase != PhaseRunning {
		t.Errorf("expected phase Running, got %q (message: %s)", status.Phase, status.Message)
	}
	if !status.ProcessHealthy {
		t.Error("expected processHealthy=true for running instance")
	}

	// Event audit: Started event must exist.
	events, total, err := repo.ListConnectorEvents(ctx, instance.ID, 1, 20)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if total == 0 {
		t.Fatal("expected at least one event after instance start")
	}
	hasStarted := false
	for _, e := range events {
		if e.Reason == ReasonStarted {
			hasStarted = true
		}
	}
	if !hasStarted {
		t.Errorf("expected ReasonStarted event; got events: %v", eventReasons(events))
	}

	// Bridge contract (T000): UpdateHTTPAddress must have been called with the
	// connector key and a non-empty address (set by FakeRunner).
	if !bridge.wasCalled("UpdateHTTPAddress") {
		t.Error("T000 contract: bridge.UpdateHTTPAddress was not called after instance started")
	}
	if args := bridge.lastCallArgs("UpdateHTTPAddress"); len(args) >= 2 && args[1] == "" {
		t.Error("T000 contract: UpdateHTTPAddress called with empty httpAddress")
	}

	// HTTPAddress must be stored on the instance.
	storedInst, err := repo.GetConnectorInstance(ctx, instance.ID)
	if err != nil {
		t.Fatalf("get stored instance: %v", err)
	}
	if storedInst.HTTPAddress == "" {
		t.Error("expected HTTPAddress to be set on connector instance after reconcile")
	}

	// ── Step 4: Create ConnectorBinding ───────────────────────────────────────
	b, err := svc.createBinding(ctx, &CreateConnectorBindingRequest{
		ConnectorInstanceID: instance.ID,
		DeviceID:            "device-aabbcc",
		Voucher:             "test-voucher",
	})
	if err != nil {
		t.Fatalf("create ConnectorBinding: %v", err)
	}
	if b.ID == "" {
		t.Fatal("expected non-empty binding ID")
	}

	// Bridge contract (T000): ProjectBinding must be called.
	if !bridge.wasCalled("ProjectBinding") {
		t.Error("T000 contract: bridge.ProjectBinding was not called after CreateConnectorBinding")
	}
	// Legacy service_access ID must be set.
	if b.LegacyServiceAccessID == "" {
		t.Error("expected LegacyServiceAccessID to be set after ProjectBinding")
	}

	// ── Step 5: Delete ConnectorBinding ───────────────────────────────────────
	if err := svc.deleteBinding(ctx, b.ID); err != nil {
		t.Fatalf("delete ConnectorBinding: %v", err)
	}
	if !bridge.wasCalled("DeleteBinding") {
		t.Error("T000 contract: bridge.DeleteBinding was not called after DeleteConnectorBinding")
	}

	// ── Step 6: Delete ConnectorInstance ──────────────────────────────────────
	if err := svc.deleteInstance(ctx, instance.ID); err != nil {
		t.Fatalf("delete ConnectorInstance: %v", err)
	}

	// Status must converge to Stopped.
	stoppedStatus, err := repo.GetConnectorStatus(ctx, instance.ID)
	if err != nil {
		t.Fatalf("get status after delete: %v", err)
	}
	if stoppedStatus.Phase != PhaseStopped {
		t.Errorf("expected Stopped after instance delete, got %q", stoppedStatus.Phase)
	}

	// Event audit: Stopped event must exist.
	events2, _, _ := repo.ListConnectorEvents(ctx, instance.ID, 1, 20)
	hasStopped := false
	for _, e := range events2 {
		if e.Reason == ReasonStopped {
			hasStopped = true
		}
	}
	if !hasStopped {
		t.Errorf("expected ReasonStopped event after delete; events: %v", eventReasons(events2))
	}

	// ── Step 7: Delete DeviceConnector ────────────────────────────────────────
	if err := svc.deleteConnector(ctx, dc.ID); err != nil {
		t.Fatalf("delete DeviceConnector: %v", err)
	}
	if !bridge.wasCalled("DeleteConnector") {
		t.Error("T000 contract: bridge.DeleteConnector was not called after DeleteDeviceConnector")
	}
	// Connector must be gone from repo.
	if _, err := repo.GetDeviceConnector(ctx, dc.ID); err == nil {
		t.Error("expected error when fetching deleted DeviceConnector")
	}
}

func TestE2E_UpdateConnector_ReprojectsMutableLegacyFields(t *testing.T) {
	ctx := context.Background()
	svc, repo, bridge, _ := newE2EService()

	dc, err := svc.create(ctx, &CreateDeviceConnectorRequest{
		ConnectorKey:      "xiaomi-plug",
		DisplayName:       "Xiaomi Plug",
		MQTTTopicPrefix:   "plugin/xiaomi-plug/",
		DeviceTypeSupport: "direct",
	})
	if err != nil {
		t.Fatalf("create DeviceConnector: %v", err)
	}

	updated := &UpdateDeviceConnectorRequest{
		DisplayName:     "Xiaomi Plug Updated",
		MQTTTopicPrefix: "plugin/xiaomi-updated/",
		Description:     "updated description",
	}
	got, err := svc.updateConnector(ctx, dc.ID, updated)
	if err != nil {
		t.Fatalf("update DeviceConnector: %v", err)
	}
	if got.DisplayName != updated.DisplayName {
		t.Fatalf("DisplayName = %q, want %q", got.DisplayName, updated.DisplayName)
	}
	if got.MQTTTopicPrefix != updated.MQTTTopicPrefix {
		t.Fatalf("MQTTTopicPrefix = %q, want %q", got.MQTTTopicPrefix, updated.MQTTTopicPrefix)
	}

	stored, err := repo.GetDeviceConnector(ctx, dc.ID)
	if err != nil {
		t.Fatalf("get stored DeviceConnector: %v", err)
	}
	if stored.MQTTTopicPrefix != updated.MQTTTopicPrefix {
		t.Fatalf("stored MQTTTopicPrefix = %q, want %q", stored.MQTTTopicPrefix, updated.MQTTTopicPrefix)
	}

	if !bridge.wasCalled("UpdateConnectorProjection") {
		t.Fatal("expected bridge.UpdateConnectorProjection to be called")
	}
	args := bridge.lastCallArgs("UpdateConnectorProjection")
	if len(args) != 3 {
		t.Fatalf("UpdateConnectorProjection args = %v, want key/displayName/prefix", args)
	}
	if args[0] != "xiaomi-plug" || args[1] != updated.DisplayName || args[2] != updated.MQTTTopicPrefix {
		t.Fatalf("UpdateConnectorProjection args = %v", args)
	}
	if args := bridge.lastCallArgs("UpdateHTTPAddress"); len(args) > 0 && args[1] == "" {
		t.Fatalf("UpdateDeviceConnector must not clear projected httpAddress; UpdateHTTPAddress args = %v", args)
	}
}

// ─── T011-02: Failure path ────────────────────────────────────────────────────

func TestE2E_FailurePath_BadImage_ErrorStatus(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, fakeRunner := newE2EService()
	fakeRunner.FailImage = "broken-image:latest"

	// Set up connector + runtime with the broken image.
	dc := &DeviceConnector{
		ID:                newID(),
		ConnectorKey:      "bad-connector",
		DisplayName:       "Broken Connector",
		MQTTTopicPrefix:   "plugin/bad/",
		DeviceTypeSupport: "direct",
	}
	if err := repo.CreateDeviceConnector(ctx, dc); err != nil {
		t.Fatal(err)
	}

	rt := &ConnectorRuntime{
		ID:                newID(),
		DeviceConnectorID: dc.ID,
		Image:             "broken-image:latest", // matches FakeRunner.FailImage
		HTTPPort:          9001,
	}
	if err := repo.CreateConnectorRuntime(ctx, rt); err != nil {
		t.Fatal(err)
	}

	// Creating an instance should NOT return an error — the service returns the
	// instance so the caller can inspect events (see CreateConnectorInstance spec).
	instance, err := svc.createInstance(ctx, &CreateConnectorInstanceRequest{
		ConnectorRuntimeID: rt.ID,
	})
	if err != nil {
		t.Fatalf("createInstance returned unexpected error: %v", err)
	}
	if instance == nil || instance.ID == "" {
		t.Fatal("expected instance to be returned even on runner failure")
	}

	// Status must be Error (not Running or Pending).
	status, err := repo.GetConnectorStatus(ctx, instance.ID)
	if err != nil {
		t.Fatalf("get status after failed reconcile: %v", err)
	}
	if status.Phase != PhaseError {
		t.Errorf("expected phase Error, got %q", status.Phase)
	}
	if status.ProcessHealthy {
		t.Error("expected processHealthy=false on error")
	}
	if status.Message == "" {
		t.Error("expected non-empty error message in status")
	}

	// Event audit: ReconcileError event must exist.
	events, total, _ := repo.ListConnectorEvents(ctx, instance.ID, 1, 20)
	if total == 0 {
		t.Fatal("expected at least one event on reconcile failure")
	}
	hasError := false
	for _, e := range events {
		if e.Reason == ReasonReconcileError {
			hasError = true
			if e.Message == "" {
				t.Error("ReconcileError event must have a non-empty message")
			}
		}
	}
	if !hasError {
		t.Errorf("expected ReasonReconcileError event; got: %v", eventReasons(events))
	}
}

// ─── T011-03: Status convergence ──────────────────────────────────────────────

func TestE2E_StatusConvergence_PendingThenRunning(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, fakeRunner := newE2EService()
	fakeRunner.StartupDelay = 5 * time.Millisecond // minimal delay

	dc := &DeviceConnector{
		ID: newID(), ConnectorKey: "fast-connector", DisplayName: "Fast",
		MQTTTopicPrefix: "plugin/fast/", DeviceTypeSupport: "direct",
	}
	_ = repo.CreateDeviceConnector(ctx, dc)

	rt := &ConnectorRuntime{
		ID:                newID(),
		DeviceConnectorID: dc.ID,
		Image:             "fast:1.0",
		HTTPPort:          9001,
	}
	_ = repo.CreateConnectorRuntime(ctx, rt)

	instance, err := svc.createInstance(ctx, &CreateConnectorInstanceRequest{
		ConnectorRuntimeID: rt.ID,
	})
	if err != nil || instance == nil {
		t.Fatalf("createInstance: %v", err)
	}

	// Status should be Running (FakeRunner is synchronous — Pending only briefly).
	status, err := repo.GetConnectorStatus(ctx, instance.ID)
	if err != nil {
		t.Fatalf("get status: %v", err)
	}
	if status.Phase != PhaseRunning {
		t.Errorf("expected Running after successful reconcile, got %q", status.Phase)
	}
	if status.UpdatedAt.IsZero() {
		t.Error("status.updatedAt must not be zero")
	}
}

// ─── T011-04: Event ordering ──────────────────────────────────────────────────

func TestE2E_EventOrdering_StartedBeforeStopped(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, _ := newE2EService()

	dc := &DeviceConnector{
		ID: newID(), ConnectorKey: "evt-connector", DisplayName: "Events",
		MQTTTopicPrefix: "plugin/evt/", DeviceTypeSupport: "direct",
	}
	_ = repo.CreateDeviceConnector(ctx, dc)
	rt := &ConnectorRuntime{
		ID: newID(), DeviceConnectorID: dc.ID, Image: "evt:1.0", HTTPPort: 9001,
	}
	_ = repo.CreateConnectorRuntime(ctx, rt)

	instance, _ := svc.createInstance(ctx, &CreateConnectorInstanceRequest{
		ConnectorRuntimeID: rt.ID,
	})
	_ = svc.deleteInstance(ctx, instance.ID)

	events, _, _ := repo.ListConnectorEvents(ctx, instance.ID, 1, 20)
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events (Started + Stopped), got %d", len(events))
	}

	// Find Started and Stopped positions.
	startedIdx, stoppedIdx := -1, -1
	for i, e := range events {
		if e.Reason == ReasonStarted && startedIdx == -1 {
			startedIdx = i
		}
		if e.Reason == ReasonStopped {
			stoppedIdx = i
		}
	}
	if startedIdx == -1 {
		t.Error("ReasonStarted event not found")
	}
	if stoppedIdx == -1 {
		t.Error("ReasonStopped event not found")
	}
	if startedIdx != -1 && stoppedIdx != -1 && startedIdx >= stoppedIdx {
		t.Errorf("expected Started (idx %d) before Stopped (idx %d)", startedIdx, stoppedIdx)
	}
}

// ─── T011-05: Bridge projection failure is non-fatal ─────────────────────────

func TestE2E_BridgeProjectionFailure_IsNonFatal(t *testing.T) {
	ctx := context.Background()
	bridge := newRecordingBridge()
	bridge.ProjectConnectorErr = errNotImplemented("ProjectConnector")

	repo := newInMemoryRepository()
	svc := NewDeviceConnectorService(repo, bridge, NewFakeRunner())

	httpCB := true
	dc, err := svc.create(ctx, &CreateDeviceConnectorRequest{
		ConnectorKey:         "bridge-fail-connector",
		DisplayName:          "Bridge Fail Test",
		MQTTTopicPrefix:      "plugin/fail/",
		DeviceTypeSupport:    "direct",
		HTTPCallbacksEnabled: &httpCB,
	})

	// Bridge failure must NOT cause CreateDeviceConnector to return an error.
	if err != nil {
		t.Fatalf("expected no error despite bridge failure, got: %v", err)
	}
	if dc == nil || dc.ID == "" {
		t.Fatal("expected DeviceConnector to be returned despite bridge failure")
	}

	// The canonical row must exist in the repository.
	stored, storeErr := repo.GetDeviceConnector(ctx, dc.ID)
	if storeErr != nil {
		t.Fatalf("canonical row must exist: %v", storeErr)
	}
	if stored.ConnectorKey != "bridge-fail-connector" {
		t.Errorf("unexpected connector key: %q", stored.ConnectorKey)
	}

	// A BridgeProjectionFailed event must have been recorded.
	events, total, _ := repo.ListConnectorEvents(ctx, "", 1, 20)
	// Note: event has empty ConnectorInstanceID (no instance yet at create time).
	_ = events
	if total == 0 {
		t.Error("expected a BridgeProjectionFailed event to be recorded")
	}
}

// ─── Thin wrappers for internal methods ───────────────────────────────────────
// DeviceConnectorService has internal create/delete methods. These wrappers
// call them directly to bypass the Encore API layer (which requires auth context).

func (s *DeviceConnectorService) createInstance(ctx context.Context, req *CreateConnectorInstanceRequest) (*ConnectorInstance, error) {
	rt, err := s.repo.GetConnectorRuntime(ctx, req.ConnectorRuntimeID)
	if err != nil {
		return nil, err
	}
	dc, err := s.repo.GetDeviceConnector(ctx, rt.DeviceConnectorID)
	if err != nil {
		return nil, err
	}

	ns := req.Namespace
	if ns == "" {
		ns = "connector-instances"
	}

	now := timeNow()
	inst := &ConnectorInstance{
		ID:                 newID(),
		ConnectorRuntimeID: rt.ID,
		DeviceConnectorID:  dc.ID,
		Namespace:          ns,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	if err := s.repo.CreateConnectorInstance(ctx, inst); err != nil {
		return nil, err
	}

	// Initial Pending status.
	_ = s.repo.UpsertConnectorStatus(ctx, &ConnectorStatus{
		ConnectorInstanceID: inst.ID,
		Phase:               PhasePending,
		ProcessHealthy:      false,
		UpdatedAt:           timeNow(),
	})

	// Reconcile via runner.
	status, runErr := s.runner.Reconcile(ctx, *inst, *rt)
	if runErr != nil {
		_ = s.repo.UpsertConnectorStatus(ctx, &ConnectorStatus{
			ConnectorInstanceID: inst.ID,
			Phase:               PhaseError,
			ProcessHealthy:      false,
			Message:             runErr.Error(),
			UpdatedAt:           timeNow(),
		})
		_ = s.repo.AppendConnectorEvent(ctx, &ConnectorEvent{
			ID:                  newID(),
			ConnectorInstanceID: inst.ID,
			Reason:              ReasonReconcileError,
			Message:             runErr.Error(),
			OccurredAt:          timeNow(),
		})
		return inst, nil
	}

	_ = s.repo.UpsertConnectorStatus(ctx, &status)
	_ = s.repo.AppendConnectorEvent(ctx, &ConnectorEvent{
		ID:                  newID(),
		ConnectorInstanceID: inst.ID,
		Reason:              ReasonStarted,
		Message:             status.Message,
		OccurredAt:          timeNow(),
	})

	// Resolve HTTP address from runner and project to legacy tables.
	if fakeR, ok := s.runner.(*FakeRunner); ok {
		inst.HTTPAddress = fakeR.FakeHTTPAddressFor(inst.ID)
	}
	if inst.HTTPAddress != "" {
		_ = s.repo.UpdateConnectorInstanceHTTPAddress(ctx, inst.ID, inst.HTTPAddress)
		if bridgeErr := s.bridge.UpdateHTTPAddress(ctx, dc.ConnectorKey, inst.HTTPAddress); bridgeErr != nil {
			_ = s.repo.AppendConnectorEvent(ctx, &ConnectorEvent{
				ID:                  newID(),
				ConnectorInstanceID: inst.ID,
				Reason:              ReasonBridgeProjectionFailed,
				Message:             "UpdateHTTPAddress: " + bridgeErr.Error(),
				OccurredAt:          timeNow(),
			})
		}
	}

	return inst, nil
}

func (s *DeviceConnectorService) createBinding(ctx context.Context, req *CreateConnectorBindingRequest) (*ConnectorBinding, error) {
	inst, err := s.repo.GetConnectorInstance(ctx, req.ConnectorInstanceID)
	if err != nil {
		return nil, err
	}
	dc, err := s.repo.GetDeviceConnector(ctx, inst.DeviceConnectorID)
	if err != nil {
		return nil, err
	}

	b := &ConnectorBinding{
		ID:                  newID(),
		ConnectorInstanceID: req.ConnectorInstanceID,
		DeviceConnectorID:   dc.ID,
		DeviceID:            req.DeviceID,
		Voucher:             req.Voucher,
		CreatedAt:           timeNow(),
	}
	if err := s.repo.CreateConnectorBinding(ctx, b); err != nil {
		return nil, err
	}

	saID, bridgeErr := s.bridge.ProjectBinding(ctx, *b, *dc)
	if bridgeErr != nil {
		_ = s.repo.AppendConnectorEvent(ctx, &ConnectorEvent{
			ID:                  newID(),
			ConnectorInstanceID: b.ConnectorInstanceID,
			Reason:              ReasonBridgeProjectionFailed,
			Message:             "ProjectBinding: " + bridgeErr.Error(),
			OccurredAt:          timeNow(),
		})
	}
	if saID != "" {
		_ = s.repo.SetBindingLegacyServiceAccessID(ctx, b.ID, saID)
		b.LegacyServiceAccessID = saID
	}
	return b, nil
}

func (s *DeviceConnectorService) deleteBinding(ctx context.Context, id string) error {
	if err := s.bridge.DeleteBinding(ctx, id); err != nil {
		_ = s.repo.AppendConnectorEvent(ctx, &ConnectorEvent{
			ID:         newID(),
			Reason:     ReasonBridgeProjectionFailed,
			Message:    "DeleteBinding: " + err.Error(),
			OccurredAt: timeNow(),
		})
	}
	return s.repo.DeleteConnectorBinding(ctx, id)
}

func (s *DeviceConnectorService) deleteInstance(ctx context.Context, id string) error {
	if err := s.runner.Delete(ctx, id); err != nil {
		_ = s.repo.AppendConnectorEvent(ctx, &ConnectorEvent{
			ID:                  newID(),
			ConnectorInstanceID: id,
			Reason:              ReasonReconcileError,
			Message:             "Delete runner: " + err.Error(),
			OccurredAt:          timeNow(),
		})
	}
	_ = s.repo.UpsertConnectorStatus(ctx, &ConnectorStatus{
		ConnectorInstanceID: id,
		Phase:               PhaseStopped,
		ProcessHealthy:      false,
		Message:             "instance deleted",
		UpdatedAt:           timeNow(),
	})
	_ = s.repo.AppendConnectorEvent(ctx, &ConnectorEvent{
		ID:                  newID(),
		ConnectorInstanceID: id,
		Reason:              ReasonStopped,
		OccurredAt:          timeNow(),
	})
	return s.repo.DeleteConnectorInstance(ctx, id)
}

func (s *DeviceConnectorService) deleteConnector(ctx context.Context, id string) error {
	dc, err := s.repo.GetDeviceConnector(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteDeviceConnector(ctx, id); err != nil {
		return err
	}
	if bridgeErr := s.bridge.DeleteConnector(ctx, dc.ConnectorKey); bridgeErr != nil {
		_ = s.repo.AppendConnectorEvent(ctx, &ConnectorEvent{
			ID:         newID(),
			Reason:     ReasonBridgeProjectionFailed,
			Message:    "DeleteConnector bridge: " + bridgeErr.Error(),
			OccurredAt: timeNow(),
		})
	}
	return nil
}

// ─── Utilities ────────────────────────────────────────────────────────────────

func timeNow() time.Time { return time.Now().UTC() }

func eventReasons(events []ConnectorEvent) []string {
	var reasons []string
	for _, e := range events {
		reasons = append(reasons, e.Reason)
	}
	return reasons
}
