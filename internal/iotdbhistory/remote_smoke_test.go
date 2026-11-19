package iotdbhistory

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"project/internal/model"
	"project/internal/storage"
)

func TestIoTDBRemoteSmoke(t *testing.T) {
	baseURL := os.Getenv("TP_IOTDB_TEST_URL")
	if baseURL == "" {
		t.Skip("TP_IOTDB_TEST_URL required")
	}
	user := os.Getenv("TP_IOTDB_TEST_USER")
	if user == "" {
		user = "root"
	}
	database := "iotdb_smoke_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	sink := &storage.IoTDBHistorySink{
		BaseURL: strings.TrimRight(baseURL, "/"), User: user,
		Password: os.Getenv("TP_IOTDB_TEST_PASSWORD"), Database: database,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanCancel()
		if err := sink.DropDatabase(cleanCtx); err != nil {
			t.Errorf("drop isolated IoTDB smoke database: %v", err)
		}
	})
	if err := sink.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}

	number := 12.5
	boolean := true
	text := "sensor-ready"
	base := time.Now().Add(-time.Hour).UnixMilli()
	device := uuid.NewString()
	if err := sink.Append(ctx, []storage.HistoryIntent{
		{ID: 1, TenantID: "smoke-tenant", DeviceID: device, Key: "temperature", TS: base, NumberV: &number},
		{ID: 2, TenantID: "smoke-tenant", DeviceID: device, Key: "temperature", TS: base + 1000, BoolV: &boolean},
		{ID: 3, TenantID: "smoke-tenant", DeviceID: device, Key: "status", TS: base + 2000, StringV: &text},
	}); err != nil {
		t.Fatal(err)
	}

	backend := &Backend{Sink: sink}
	rows, err := backend.GetHistoryTelemetrData(device, "temperature", base, base+2000)
	if err != nil || len(rows) != 2 {
		t.Fatalf("range read rows=%v err=%v", rows, err)
	}
	if rows[0].T != base || rows[0].NumberV == nil || *rows[0].NumberV != number || rows[1].T != base+1000 || rows[1].BoolV == nil || !*rows[1].BoolV {
		t.Fatalf("range read values = %#v", rows)
	}
	page, pageSize := 1, 1
	total, pageRows, err := backend.GetHistoryTelemetrDataByPage(&model.GetTelemetryHistoryDataByPageReq{
		DeviceID: device, Key: "temperature", StartTime: base, EndTime: base + 2000, Page: &page, PageSize: &pageSize,
	})
	if err != nil || total != 2 || len(pageRows) != 1 || pageRows[0].T != base+1000 {
		t.Fatalf("page total=%d rows=%v err=%v", total, pageRows, err)
	}
	if count, err := backend.GetTelemetryDataCountByTenantId("smoke-tenant"); err != nil || count != 3 {
		t.Fatalf("tenant count=%d err=%v", count, err)
	}
	if err := backend.DeleteTelemetrData(device, "temperature"); err != nil {
		t.Fatal(err)
	}
	if rows, err := backend.GetHistoryTelemetrData(device, "temperature", base, base+2000); err != nil || len(rows) != 0 {
		t.Fatalf("deleted temperature rows=%v err=%v", rows, err)
	}
	if err := backend.DeleteTelemetrDataByTime(base + 2000); err != nil {
		t.Fatal(err)
	}
	if count, err := backend.GetTelemetryDataCountByTenantId("smoke-tenant"); err != nil || count != 0 {
		t.Fatalf("post-retention tenant count=%d err=%v", count, err)
	}
}
