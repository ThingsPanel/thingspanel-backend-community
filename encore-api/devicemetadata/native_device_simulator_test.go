package devicemetadata

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"encore.app/l1-access/authn"
	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/et"
	"github.com/google/uuid"
)

type countingSender struct {
	calls int
	fail  map[int]error
	data  []string
}

func (c *countingSender) Send(_ context.Context, _, _ string, data string) error {
	c.calls++
	c.data = append(c.data, data)
	return c.fail[c.calls]
}

func TestDecodeUpdateSimulatorProfile(t *testing.T) {
	good := `{"kind":"telemetry","payload":{"temp":1},"topicPurpose":"telemetry","intervalSeconds":5,"messageLimit":10}`
	if _, err := decodeUpdateSimulatorProfile([]byte(good)); err != nil {
		t.Fatalf("valid profile rejected: %v", err)
	}
	for name, body := range map[string]string{
		"unknown field": strings.Replace(good, `"kind"`, `"server":"x","kind"`, 1),
		"bad kind":      strings.Replace(good, `"kind":"telemetry"`, `"kind":"cmd"`, 1),
		"no payload":    strings.Replace(good, `"payload":{"temp":1},`, "", 1),
		"interval 0":    strings.Replace(good, `"intervalSeconds":5`, `"intervalSeconds":0`, 1),
		"limit 1001":    strings.Replace(good, `"messageLimit":10`, `"messageLimit":1001`, 1),
		"no interval":   strings.Replace(good, `"intervalSeconds":5,`, "", 1),
	} {
		if _, err := decodeUpdateSimulatorProfile([]byte(body)); err == nil {
			t.Errorf("%s must be rejected", name)
		}
	}
}

func TestDeviceSimulatorProfileSessionAndRun(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID, subjectID := testTenantID(), "simulator-test"
	deviceID := uuid.NewString()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertNativeReadDevice(ctx, tx, tenantID, deviceID, "SIM-"+deviceID[:8], "sim device", "{}", true); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM metadata_idempotency WHERE tenant_id=$1`,
			`DELETE FROM metadata_audit_events WHERE tenant_id=$1`,
			`DELETE FROM device_simulation_runs WHERE tenant_id=$1`,
			`DELETE FROM device_simulation_sessions WHERE tenant_id=$1`,
			`DELETE FROM device_simulator_profiles WHERE tenant_id=$1`,
			`DELETE FROM device_access_bindings WHERE tenant_id=$1`,
			`DELETE FROM devices WHERE tenant_id=$1`,
		} {
			if _, err := db.Exec(ctx, q, tenantID); err != nil {
				t.Errorf("cleanup %q: %v", q, err)
			}
		}
	})
	et.OverrideAuthInfo(auth.UID(subjectID), &authn.Data{UserID: subjectID, TenantID: tenantID, Roles: []string{"TENANT_ADMIN"}})
	defer et.OverrideAuthInfo("", nil)
	identity, err := currentNativeDeviceWriteIdentity(false)
	if err != nil {
		t.Fatal(err)
	}
	sender := &countingSender{fail: map[int]error{}}
	previous := activeSimulationSender
	activeSimulationSender = sender
	defer func() { activeSimulationSender = previous }()
	code := func(err error) errs.ErrCode {
		var e *errs.Error
		if errors.As(err, &e) {
			return e.Code
		}
		return errs.Unknown
	}
	key := func(label string) string { return "sim-" + label + "-" + strings.ReplaceAll(uuid.NewString(), "-", "") }
	profileBody := func(limit int) []byte {
		return []byte(`{"kind":"telemetry","payload":{"temp":21},"topicPurpose":"telemetry","intervalSeconds":5,"messageLimit":` + jsonInt(limit) + `}`)
	}

	// No profile yet; the first save needs the version-0 ETag.
	if _, err := readSimulatorProfile(ctx, identity.nativeDeviceReadIdentity, deviceID, "r1"); code(err) != errs.NotFound {
		t.Fatalf("missing profile: %v", err)
	}
	if _, err := updateSimulatorProfile(ctx, identity, deviceID, key("a"), `"sim-profile-3"`, profileBody(5), "r2"); code(err) != errs.FailedPrecondition {
		t.Fatalf("wrong ETag must be refused: %v", err)
	}
	saved, err := updateSimulatorProfile(ctx, identity, deviceID, key("b"), `"sim-profile-0"`, profileBody(5), "r3")
	if err != nil || saved.ETag != `"sim-profile-1"` {
		t.Fatalf("first save: %+v %v", saved, err)
	}
	if again, err := updateSimulatorProfile(ctx, identity, deviceID, key("c"), `"sim-profile-0"`, profileBody(5), "r4"); code(err) != errs.FailedPrecondition {
		t.Fatalf("stale ETag must be refused: %+v %v", again, err)
	}

	// Sessions bind to the saved version.
	if _, err := createSimulationSession(ctx, identity, deviceID, key("d"), []byte(`{"profileVersion":9}`), "r5"); code(err) != errs.FailedPrecondition {
		t.Fatalf("wrong profile version: %v", err)
	}
	created, err := createSimulationSession(ctx, identity, deviceID, key("e"), []byte(`{"profileVersion":1}`), "r6")
	if err != nil || created.Status != 201 {
		t.Fatalf("create session: %+v %v", created, err)
	}
	var session struct{ Data SimulationSession }
	if err := json.Unmarshal(created.Body, &session); err != nil || session.Data.State != "ready" {
		t.Fatalf("session body: %s", created.Body)
	}

	// Runs: capped, bounded by the profile, partial failures are recorded.
	runBody := func(count int) []byte {
		return []byte(`{"sessionId":"` + session.Data.ID + `","messageCount":` + jsonInt(count) + `}`)
	}
	if _, err := runDeviceSimulation(ctx, identity, deviceID, key("f"), runBody(6), "r7"); code(err) != errs.InvalidArgument {
		t.Fatalf("count above profile limit: %v", err)
	}
	if _, err := runDeviceSimulation(ctx, identity, deviceID, key("g"), runBody(21), "r8"); code(err) != errs.InvalidArgument {
		t.Fatalf("count above sync cap: %v", err)
	}
	sender.fail[2] = errors.New("broker unreachable")
	ran, err := runDeviceSimulation(ctx, identity, deviceID, key("h"), runBody(3), "r9")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var run struct{ Data SimulationRun }
	if err := json.Unmarshal(ran.Body, &run); err != nil || run.Data.State != "failed" || run.Data.Sent != 2 || run.Data.Failed != 1 || string(run.Data.Error) == "null" {
		t.Fatalf("run body: %s", ran.Body)
	}
	if sender.calls != 3 || sender.data[0] != `{"temp":21}` {
		t.Fatalf("sender calls=%d data=%v", sender.calls, sender.data)
	}
	// The same key replays the stored response without sending again.
	sameKey := key("i")
	first, err := runDeviceSimulation(ctx, identity, deviceID, sameKey, runBody(1), "r10")
	if err != nil {
		t.Fatal(err)
	}
	callsAfterFirst := sender.calls
	second, err := runDeviceSimulation(ctx, identity, deviceID, sameKey, runBody(1), "r11")
	if err != nil || string(second.Body) != string(first.Body) || sender.calls != callsAfterFirst {
		t.Fatalf("replay re-sent or changed body: err=%v calls=%d/%d", err, sender.calls, callsAfterFirst)
	}
	var runID struct{ Data SimulationRun }
	_ = json.Unmarshal(first.Body, &runID)
	read, err := readSimulationRun(ctx, identity.nativeDeviceReadIdentity, deviceID, runID.Data.ID, "r12")
	if err != nil || !strings.Contains(string(read.Body), runID.Data.ID) {
		t.Fatalf("read run: %v", err)
	}

	// A new profile version invalidates the open session; closing is idempotent.
	if _, err := updateSimulatorProfile(ctx, identity, deviceID, key("j"), `"sim-profile-1"`, profileBody(5), "r13"); err != nil {
		t.Fatal(err)
	}
	if _, err := runDeviceSimulation(ctx, identity, deviceID, key("k"), runBody(1), "r14"); code(err) != errs.FailedPrecondition {
		t.Fatalf("session on an old profile version: %v", err)
	}
	for i := 0; i < 2; i++ {
		if closed, err := closeSimulationSession(ctx, identity, deviceID, session.Data.ID, "r15"); err != nil || !strings.Contains(string(closed.Body), `"closed"`) {
			t.Fatalf("close #%d: %v", i, err)
		}
	}
	if _, err := runDeviceSimulation(ctx, identity, deviceID, key("l"), runBody(1), "r16"); code(err) != errs.FailedPrecondition {
		t.Fatalf("closed session: %v", err)
	}

	// Another tenant and a role without the permission see nothing.
	other := identity
	other.tenantID = uuid.NewString()
	if _, err := readSimulatorProfile(ctx, other.nativeDeviceReadIdentity, deviceID, "r17"); code(err) != errs.NotFound {
		t.Fatalf("cross tenant: %v", err)
	}
	et.OverrideAuthInfo(auth.UID(subjectID), &authn.Data{UserID: subjectID, TenantID: tenantID, Roles: []string{"tenant_viewer"}})
	if _, err := readSimulatorProfile(ctx, identity.nativeDeviceReadIdentity, deviceID, "r18"); code(err) != errs.PermissionDenied {
		t.Fatalf("role without device:simulate: %v", err)
	}
}

func jsonInt(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestNativeSimulatorConfigRequiresDiagnoseAndNeverLeaksSecrets(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID, subjectID := testTenantID(), "sim-config-test"
	deviceID := uuid.NewString()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertNativeReadDevice(ctx, tx, tenantID, deviceID, "CFG-"+deviceID[:8], "config device", "{}", true); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM device_access_bindings WHERE tenant_id=$1 AND device_id=$2::uuid`, tenantID, deviceID)
		_, _ = db.Exec(ctx, `DELETE FROM devices WHERE tenant_id=$1 AND id=$2::uuid`, tenantID, deviceID)
	})
	et.OverrideAuthInfo(auth.UID(subjectID), &authn.Data{UserID: subjectID, TenantID: tenantID, Roles: []string{"TENANT_ADMIN"}})
	defer et.OverrideAuthInfo("", nil)
	identity, err := currentNativeDeviceReadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readNativeSimulatorConfig(ctx, identity, false, deviceID, "cfg-denied"); errs.Code(err) != errs.PermissionDenied {
		t.Fatalf("without device:diagnose: %v", err)
	}
	if _, err := readNativeSimulatorConfig(ctx, identity, true, deviceID, "cfg-unconfigured"); errs.Code(err) != errs.NotFound {
		t.Fatalf("device without access config: %v", err)
	}
	if _, err := db.Exec(ctx, `UPDATE device_access_bindings SET direct_access='{"mode":"direct","protocol":"mqtt","password":"must-not-leak"}'::jsonb,
		configuration_generation=configuration_generation+1 WHERE tenant_id=$1 AND device_id=$2::uuid`, tenantID, deviceID); err != nil {
		t.Fatal(err)
	}
	t.Setenv(connectionInfoEndpointEnv, "mqtts://iot.example.test:8883")
	config, err := readNativeSimulatorConfig(ctx, identity, true, deviceID, "cfg-ok")
	if err != nil {
		t.Fatal(err)
	}
	if config.Data.DeviceID != deviceID || config.Data.Connection.Protocol != "mqtt" || config.Data.ModelBinding != nil || len(config.Data.Topics) != 0 {
		t.Fatalf("unexpected config: %+v", config.Data)
	}
	encoded, _ := json.Marshal(config)
	if strings.Contains(string(encoded), "must-not-leak") || !strings.Contains(string(encoded), `"topics":[]`) {
		t.Fatalf("config leaked a secret or omitted topics: %s", encoded)
	}
}
