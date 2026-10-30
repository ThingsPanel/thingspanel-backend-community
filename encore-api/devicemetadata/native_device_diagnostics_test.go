package devicemetadata

import (
	"context"
	"errors"
	"testing"
	"time"

	"encore.app/l1-access/authn"
	"encore.dev/beta/auth"
	"encore.dev/et"
	"github.com/google/uuid"
)

func TestDecodeNativeDiagnosticsQuery(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	q, err := decodeNativeDiagnosticsQuery(map[string][]string{}, now)
	if err != nil || !q.to.Equal(now) || !q.from.Equal(now.Add(-24*time.Hour)) || q.limit != 20 {
		t.Fatalf("defaults wrong: %+v %v", q, err)
	}
	ok := map[string][]string{"from": {"2026-10-01T00:00:00Z"}, "to": {"2026-10-02T00:00:00Z"}, "recentFailureLimit": {"100"}}
	if q, err = decodeNativeDiagnosticsQuery(ok, now); err != nil || q.limit != 100 {
		t.Fatalf("valid window rejected: %v", err)
	}
	for name, values := range map[string]map[string][]string{
		"from only":     {"from": {"2026-10-01T00:00:00Z"}},
		"reversed":      {"from": {"2026-10-02T00:00:00Z"}, "to": {"2026-10-01T00:00:00Z"}},
		"too long":      {"from": {"2026-08-01T00:00:00Z"}, "to": {"2026-10-01T00:00:00Z"}},
		"bad time":      {"from": {"yesterday"}, "to": {"2026-10-01T00:00:00Z"}},
		"limit zero":    {"recentFailureLimit": {"0"}},
		"limit too big": {"recentFailureLimit": {"101"}},
		"limit text":    {"recentFailureLimit": {"many"}},
	} {
		if _, err := decodeNativeDiagnosticsQuery(values, now); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestNativeDiagnosticsAreTruthfulAboutWhatIsKnown(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID, subjectID := testTenantID(), "diagnostics-test"
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	deviceID := uuid.NewString()
	if err := insertNativeReadDevice(ctx, tx, tenantID, deviceID, "diag-"+deviceID, "diagnostic device", `{}`, true); err != nil {
		t.Fatalf("insert device fixture: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, statement := range []string{
			`DELETE FROM device_command_executions WHERE tenant_id=$1 AND device_id=$2::uuid`,
			`DELETE FROM device_access_bindings WHERE tenant_id=$1 AND device_id=$2::uuid`,
			`DELETE FROM devices WHERE tenant_id=$1 AND id=$2::uuid`,
		} {
			if _, err := db.Exec(ctx, statement, tenantID, deviceID); err != nil {
				t.Errorf("cleanup %q: %v", statement, err)
			}
		}
	})
	for i, state := range []string{"failed", "timed_out", "acknowledged"} {
		if _, err := db.Exec(ctx, `INSERT INTO device_command_executions(tenant_id,id,device_id,identifier,snapshot_id,parameters,state,reason,created_by,accepted_at)
			VALUES($1,$2,$3::uuid,'reboot',$4,'{}'::jsonb,$5,$6,$7,NOW()-($8 * INTERVAL '1 minute'))`,
			tenantID, uuid.NewString(), deviceID, uuid.NewString(), state, "synthetic "+state, subjectID, 3-i); err != nil {
			t.Fatalf("insert command fixture: %v", err)
		}
	}
	et.OverrideAuthInfo(auth.UID(subjectID), &authn.Data{UserID: subjectID, TenantID: tenantID, Roles: []string{"TENANT_ADMIN"}})
	defer et.OverrideAuthInfo("", nil)
	identity, err := currentNativeDeviceReadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	query := nativeDiagnosticsQuery{from: now.Add(-time.Hour), to: now.Add(time.Minute), limit: 1}
	offline := func(context.Context) (string, error) { return "offline", nil }
	result, err := readNativeDiagnostics(ctx, identity, query, deviceID, "diag-1", now, offline)
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]NativeDiagnosticCheck{}
	for _, check := range result.Data.Checks {
		checks[check.Stage] = check
	}
	for stage, want := range map[string]string{"metadata": "pass", "credential": "unknown", "configuration": "fail", "connection": "fail", "ingestion": "unknown", "model": "unknown", "command": "pass"} {
		if checks[stage].State != want {
			t.Errorf("stage %s state=%q code=%q, want %s", stage, checks[stage].State, checks[stage].Code, want)
		}
	}
	for name, counter := range map[string]NativeDiagnosticCounter{"uplink": result.Data.Stats.Uplink, "downlink": result.Data.Stats.Downlink, "storage": result.Data.Stats.Storage} {
		if counter.Complete || counter.Success != nil || counter.Total != nil || counter.SuccessRate != nil {
			t.Errorf("%s counter must be incomplete with null values, got %+v", name, counter)
		}
	}
	if len(result.Data.RecentFailures) != 1 || !result.Data.RecentFailuresTruncated {
		t.Fatalf("limit=1 with two failures must return one and mark truncation: %+v", result.Data)
	}
	if result.Data.RecentFailures[0].Code != "COMMAND_TIMED_OUT" || result.Data.RecentFailures[0].Direction != "downlink" {
		t.Fatalf("newest failure first expected, got %+v", result.Data.RecentFailures[0])
	}

	// An unreachable runtime is "unknown", never a fabricated online/offline answer.
	unreachable := func(context.Context) (string, error) { return "", errors.New("upstream down") }
	result, err = readNativeDiagnostics(ctx, identity, query, deviceID, "diag-2", now, unreachable)
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range result.Data.Checks {
		if check.Stage == "connection" && (check.State != "unknown" || check.Code != "RUNTIME_UNAVAILABLE") {
			t.Fatalf("unreachable runtime reported as %+v", check)
		}
	}
	other := nativeDeviceReadIdentity{tenantID: "00000000-0000-0000-0000-00000000dead"}
	if _, err := readNativeDiagnostics(ctx, other, query, deviceID, "diag-3", now, offline); err == nil {
		t.Fatal("another tenant read this device's diagnostics")
	}
}
