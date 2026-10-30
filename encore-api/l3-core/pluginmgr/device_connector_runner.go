package pluginmgr

import (
	"context"
	"fmt"
	"time"
)

// Runner manages the lifecycle of a ConnectorInstance in a runtime environment.
// Implementations must be replaceable:
//   - FakeRunner: deterministic, no external dependencies, used in unit tests.
//   - K8SRunner: maps ConnectorRuntime to K8S Deployment/Service/ConfigMap/Secret (T007).
//
// Constraints (from Developer B handoff):
//   - Runner must NOT know about protocol_type.
//   - Runner must NOT call ThingsPanel backend directly.
//   - Runner must NOT encode adapter-specific behavior.
//   - Runner must NOT hide failed pods behind a generic error.
type Runner interface {
	// Reconcile ensures the instance is running according to the given runtime spec.
	// On success, returns a ConnectorStatus with the resolved httpAddress set.
	// The caller is responsible for persisting status and projecting httpAddress.
	Reconcile(ctx context.Context, instance ConnectorInstance, runtime ConnectorRuntime) (ConnectorStatus, error)

	// Delete stops and removes the instance from the runtime environment.
	Delete(ctx context.Context, instanceID string) error

	// Logs returns a stream of log lines for the given instance.
	// Implementations may return (nil, nil) if logs are not available.
	Logs(ctx context.Context, instanceID string, opts LogOptions) ([]string, error)
}

// LogOptions controls log retrieval.
type LogOptions struct {
	TailLines int  // 0 means return all available lines
	Follow    bool // ignored in FakeRunner
}

// ─── FakeRunner ──────────────────────────────────────────────────────────────

// FakeRunner is a deterministic in-process runner for testing.
// It simulates lifecycle transitions without any K8S or Docker dependency.
// Status transitions: Pending → Starting → Running (after fakeStartupDelay).
// All reconciled instances are tracked in memory.
type FakeRunner struct {
	// StartupDelay is how long Reconcile waits before marking an instance Running.
	// Default: 0 (instant).
	StartupDelay time.Duration

	// FailImage causes Reconcile to return an error for instances whose image matches.
	FailImage string

	// fakeHTTPAddress is the address reported for successfully started instances.
	// Default: "127.0.0.1:9001"
	FakeHTTPAddress string

	instances map[string]fakeInstanceState
}

type fakeInstanceState struct {
	phase     string
	startedAt time.Time
}

// NewFakeRunner creates a FakeRunner with sensible defaults.
func NewFakeRunner() *FakeRunner {
	return &FakeRunner{
		FakeHTTPAddress: "127.0.0.1:9001",
		instances:       make(map[string]fakeInstanceState),
	}
}

// Reconcile simulates starting a connector.
// If FailImage is set and matches runtime.Image, returns an error.
// Otherwise records the instance as Running and returns a status with HTTPAddress.
func (f *FakeRunner) Reconcile(ctx context.Context, instance ConnectorInstance, runtime ConnectorRuntime) (ConnectorStatus, error) {
	if f.instances == nil {
		f.instances = make(map[string]fakeInstanceState)
	}

	if f.FailImage != "" && runtime.Image == f.FailImage {
		f.instances[instance.ID] = fakeInstanceState{phase: PhaseError}
		return ConnectorStatus{
			ConnectorInstanceID: instance.ID,
			Phase:               PhaseError,
			ProcessHealthy:      false,
			Message:             fmt.Sprintf("ImagePullFailed: image %q not found", runtime.Image),
			UpdatedAt:           time.Now().UTC(),
		}, fmt.Errorf("ImagePullFailed: image %q not found", runtime.Image)
	}

	if f.StartupDelay > 0 {
		select {
		case <-time.After(f.StartupDelay):
		case <-ctx.Done():
			return ConnectorStatus{}, ctx.Err()
		}
	}

	now := time.Now().UTC()
	f.instances[instance.ID] = fakeInstanceState{phase: PhaseRunning, startedAt: now}

	addr := f.FakeHTTPAddress
	if addr == "" {
		addr = "127.0.0.1:9001"
	}

	return ConnectorStatus{
		ConnectorInstanceID: instance.ID,
		Phase:               PhaseRunning,
		ProcessHealthy:      true,
		HTTPAddress:         addr,
		Message:             "started by FakeRunner",
		UpdatedAt:           now,
	}, nil
}

// Delete marks the instance as stopped in the fake state store.
func (f *FakeRunner) Delete(_ context.Context, instanceID string) error {
	if f.instances == nil {
		return nil
	}
	f.instances[instanceID] = fakeInstanceState{phase: PhaseStopped}
	return nil
}

// Logs returns a static log line for testing.
func (f *FakeRunner) Logs(_ context.Context, instanceID string, _ LogOptions) ([]string, error) {
	state, ok := f.instances[instanceID]
	if !ok {
		return nil, fmt.Errorf("instance %q not found in FakeRunner", instanceID)
	}
	return []string{
		fmt.Sprintf("[FakeRunner] instance=%s phase=%s", instanceID, state.phase),
	}, nil
}

// FakeHTTPAddressFor returns the HTTP address that FakeRunner resolves for a given instance.
// Used by tests and bridge code to retrieve the address after Reconcile.
func (f *FakeRunner) FakeHTTPAddressFor(_ string) string {
	if f.FakeHTTPAddress != "" {
		return f.FakeHTTPAddress
	}
	return "127.0.0.1:9001"
}
