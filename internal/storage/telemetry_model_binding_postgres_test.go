package storage

import (
	"context"
	"github.com/google/uuid"
	"project/internal/uplinkbinding"
	"testing"
	"time"
)

func TestPostgresTelemetryModelFencePreventsHistoryCurrentAndEffect(t *testing.T) {
	f := newTelemetryBrokerFixture(t)
	ctx := context.Background()
	for _, sql := range []string{
		`ALTER TABLE devices ADD COLUMN device_config_id text,ADD COLUMN sub_device_addr text`,
		`CREATE TABLE device_configs(id text PRIMARY KEY,tenant_id text,device_template_id text,native_thing_model_id text,native_thing_model_snapshot_id text,native_thing_model_revision integer,native_binding_revision integer)`,
		`INSERT INTO device_configs VALUES('cfg','tenant-fence',NULL,'m','s',1,1)`,
	} {
		if err := f.db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	f.insertDevice(t, "tenant-fence", "logical", "")
	f.insertBinding(t, "tenant-fence", "logical", "cell-1", 1, "active")
	// Capture unbound state, then bind before flushing queued data.
	unbound, err := uplinkbinding.Read(ctx, f.db, "tenant-fence", "logical")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.db.Exec(`UPDATE devices SET device_config_id='cfg' WHERE id='logical'`).Error; err != nil {
		t.Fatal(err)
	}
	w := telemetryBrokerWriter(f)
	ts := time.Now().UnixMilli()
	stale := telemetryTrustedItem("tenant-fence", "logical", "logical", uuid.NewString(), "cell-1", 1, ts, "temperature", 22.0)
	stale.bindings = []uplinkbinding.Snapshot{unbound}
	if _, _, _, err = w.commitTrustedTelemetry([]*telemetryBatchItem{stale}); err == nil {
		t.Fatal("unbound queued message bypassed new binding")
	}
	for _, table := range []string{"telemetry_datas", "telemetry_current_datas", "broker_ingress_effects"} {
		assertTelemetryCount(t, f.db, table, "1=1", 0)
	}
	current, err := uplinkbinding.Read(ctx, f.db, "tenant-fence", "logical")
	if err != nil {
		t.Fatal(err)
	}
	good := telemetryTrustedItem("tenant-fence", "logical", "logical", uuid.NewString(), "cell-1", 1, ts+1, "temperature", 23.0)
	good.bindings = []uplinkbinding.Snapshot{current}
	if _, _, _, err = w.commitTrustedTelemetry([]*telemetryBatchItem{good}); err != nil {
		t.Fatal(err)
	}
	if err = f.db.Exec(`UPDATE device_configs SET native_binding_revision=2,native_thing_model_snapshot_id='new' WHERE id='cfg'`).Error; err != nil {
		t.Fatal(err)
	}
	changed := telemetryTrustedItem("tenant-fence", "logical", "logical", uuid.NewString(), "cell-1", 1, ts+2, "temperature", 99.0)
	changed.bindings = []uplinkbinding.Snapshot{current}
	if _, _, _, err = w.commitTrustedTelemetry([]*telemetryBatchItem{changed}); err == nil {
		t.Fatal("native rebind bypassed fence")
	}
	for _, table := range []string{"telemetry_datas", "telemetry_current_datas", "broker_ingress_effects"} {
		assertTelemetryCount(t, f.db, table, "1=1", 1)
	}
	var value float64
	f.db.Table("telemetry_current_datas").Select("number_v").Scan(&value)
	if value != 23 {
		t.Fatalf("stale write overwrote current=%v", value)
	}
	called := false
	legacy := &telemetryBatchItem{deviceID: "logical", tenantID: "tenant-fence", timestamp: ts + 3, points: []TelemetryDataPoint{{Key: "temperature", Value: 100.0}}, bindings: []uplinkbinding.Snapshot{current}, afterCommit: func() { called = true }}
	w.doFlushFencedLegacy([]*telemetryBatchItem{legacy})
	if called {
		t.Fatal("legacy automation callback escaped stale fence")
	}
	assertTelemetryCount(t, f.db, "telemetry_datas", "1=1", 1)
	fresh, err := uplinkbinding.Read(ctx, f.db, "tenant-fence", "logical")
	if err != nil {
		t.Fatal(err)
	}
	legacy.bindings = []uplinkbinding.Snapshot{fresh}
	w.doFlushFencedLegacy([]*telemetryBatchItem{legacy})
	if !called {
		t.Fatal("legacy callback missing after valid commit")
	}
	assertTelemetryCount(t, f.db, "telemetry_datas", "1=1", 2)
	// Independent messages share a batch but not atomic gateway semantics. A
	// stale first item must not silently discard a different legal device.
	f.insertDevice(t, "tenant-fence", "independent", "")
	f.insertBinding(t, "tenant-fence", "independent", "cell-1", 1, "active")
	other, err := uplinkbinding.Read(ctx, f.db, "tenant-fence", "independent")
	if err != nil {
		t.Fatal(err)
	}
	valid := telemetryTrustedItem("tenant-fence", "independent", "independent", uuid.NewString(), "cell-1", 1, ts+4, "temperature", 88.0)
	valid.bindings = []uplinkbinding.Snapshot{other}
	staleCalled, validCalled := false, false
	changed.afterCommit = func() { staleCalled = true }
	valid.afterCommit = func() { validCalled = true }
	w.doFlushTrusted([]*telemetryBatchItem{changed, valid})
	if staleCalled || !validCalled {
		t.Fatalf("mixed batch outcomes stale=%v valid=%v", staleCalled, validCalled)
	}
	assertTelemetryCount(t, f.db, "telemetry_datas", "device_id=?", "independent", 1)
	assertTelemetryCount(t, f.db, "broker_ingress_effects", "device_id=?", "independent", 1)
}
