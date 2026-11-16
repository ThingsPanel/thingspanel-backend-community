package brokerstate

import (
	"context"
	"testing"
	"time"
)

func TestInspectBatchTenantAndReception(t *testing.T) {
	s := activityStore(t)
	configureActivity(t, s, "da", "ta", 2, 0)
	at := time.Now().UTC().Truncate(time.Microsecond)
	setActivity(t, s, "ta", "da", "da", "c", 1, at)
	rows, err := s.InspectBatch(context.Background(), "ta", []string{"da"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Connectivity != "online" || rows[0].LastTelemetryAt == nil || !rows[0].LastTelemetryAt.Equal(at) {
		t.Fatalf("rows=%+v", rows)
	}
	single, err := s.InspectCurrent(context.Background(), "ta", "da")
	if err != nil || single.Status != rows[0].Connectivity {
		t.Fatalf("batch owner parity: %v %+v %+v", err, single, rows)
	}
	if err := s.db.Exec(`UPDATE broker_telemetry_activity SET observed_at=? WHERE tenant_id='ta' AND device_id='da'`, at.Add(-10*time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	rows, err = s.InspectBatch(context.Background(), "ta", []string{"da"})
	if err != nil || rows[0].Connectivity != "offline" {
		t.Fatalf("heartbeat batch expired: %+v %v", rows, err)
	}
	if err := s.db.Exec(`UPDATE devices SET is_enabled='disabled' WHERE tenant_id='ta' AND id='da'`).Error; err != nil {
		t.Fatal(err)
	}
	rows, err = s.InspectBatch(context.Background(), "ta", []string{"da"})
	if err != nil || rows[0].Connectivity != "unknown" {
		t.Fatalf("disabled is not offline: %+v %v", rows, err)
	}

	if _, err = s.InspectBatch(context.Background(), "other", []string{"da"}); err != ErrDeviceUnavailable {
		t.Fatalf("foreign tenant: %v", err)
	}
	if _, err = s.InspectBatch(context.Background(), "ta", []string{"da", "da"}); err == nil {
		t.Fatal("duplicate accepted")
	}
}
