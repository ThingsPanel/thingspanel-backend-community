package storage

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTelemetryCurrentWriteAcknowledgementPostgres(t *testing.T) {
	for _, path := range []string{"legacy", "trusted"} {
		for _, boundary := range []string{"missing_insert", "stale_update", "equal_timestamp_mismatch", "altered_insert"} {
			t.Run(path+"/"+boundary, func(t *testing.T) {
				f := newTelemetryBrokerFixture(t)
				tenant, device, cell := "tenant-current", "device-current", "cell-current"
				f.insertDevice(t, tenant, device, "")
				f.insertBinding(t, tenant, device, cell, 1, "active")
				ts := time.Now().Add(-time.Second).UnixMilli()
				event, body := "INSERT", "RETURN NULL;"
				historyBefore, currentBefore := 0, 0
				if boundary == "stale_update" || boundary == "equal_timestamp_mismatch" {
					event = "UPDATE"
					seedTS, historyValue, currentValue := ts-1000, 1.0, 1.0
					if boundary == "equal_timestamp_mismatch" {
						seedTS = ts
						currentValue = 9.0
					}
					if err := f.db.Create(&TelemetryData{DeviceID: device, TenantID: tenant, Key: "temp", TS: seedTS, NumberV: &historyValue}).Error; err != nil {
						t.Fatal(err)
					}
					if err := f.db.Create(&TelemetryCurrentData{DeviceID: device, TenantID: tenant, Key: "temp", TS: time.UnixMilli(seedTS), NumberV: &currentValue}).Error; err != nil {
						t.Fatal(err)
					}
					historyBefore, currentBefore = 1, 1
				}
				if boundary == "altered_insert" {
					body = "NEW.number_v := 999; RETURN NEW;"
				}
				if err := f.db.Exec("CREATE FUNCTION corrupt_current_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN " + body + " END $$; CREATE TRIGGER corrupt_current_write BEFORE " + event + " ON telemetry_current_datas FOR EACH ROW EXECUTE FUNCTION corrupt_current_write()").Error; err != nil {
					t.Fatal(err)
				}
				w := telemetryBrokerWriter(f)
				item := telemetryTrustedItem(tenant, device, device, uuid.NewString(), cell, 1, ts, "temp", 2.0)
				var callbacks atomic.Int32
				item.afterCommit = func() { callbacks.Add(1) }
				if path == "legacy" {
					item.route = nil
					w.doFlushLegacy([]*telemetryBatchItem{item})
				} else {
					w.doFlushTrusted([]*telemetryBatchItem{item})
				}
				metrics := w.metrics.GetMetrics()
				if metrics.TelemetryWritten != 0 || metrics.TelemetryFailed != 1 || metrics.TelemetryUnknown != 0 || callbacks.Load() != 0 {
					t.Fatalf("current write %s reported confirmation: metrics=%+v afterCommit=%d", boundary, metrics, callbacks.Load())
				}
				assertTelemetryCount(t, f.db, "telemetry_datas", `tenant_id=? AND device_id=?`, tenant, device, historyBefore)
				assertTelemetryCount(t, f.db, "telemetry_current_datas", `tenant_id=? AND device_id=?`, tenant, device, currentBefore)
				assertTelemetryCount(t, f.db, "broker_ingress_receipts", `tenant_id=?`, tenant, 0)
				assertTelemetryCount(t, f.db, "broker_ingress_effects", `tenant_id=?`, tenant, 0)
				assertTelemetryCount(t, f.db, "broker_telemetry_activity", `tenant_id=?`, tenant, 0)
				assertTelemetryCount(t, f.db, "broker_state_projection_queue", `tenant_id=?`, tenant, 0)
				if err := f.db.Exec("DROP TRIGGER corrupt_current_write ON telemetry_current_datas; DROP FUNCTION corrupt_current_write()").Error; err != nil {
					t.Fatal(err)
				}
				// Confirmed rollback retains the original trusted receipt identity.
				// Once the fault is removed, the same input can be admitted exactly once.
				if path == "legacy" {
					w.doFlushLegacy([]*telemetryBatchItem{item})
				} else {
					w.doFlushTrusted([]*telemetryBatchItem{item})
				}
				var current TelemetryCurrentData
				if err := f.db.Where("device_id=? AND key=?", device, "temp").Take(&current).Error; err != nil {
					t.Fatal(err)
				}
				want := 2.0
				if boundary == "equal_timestamp_mismatch" {
					want = 1.0
				}
				if current.TS.UnixMilli() != ts || current.NumberV == nil || *current.NumberV != want {
					t.Fatalf("recovery did not retain accepted history winner: %+v", current)
				}
				if path == "trusted" {
					if callbacks.Load() != 1 {
						t.Fatalf("original trusted retry did not acknowledge exactly once: %d", callbacks.Load())
					}
					assertTelemetryCount(t, f.db, "broker_ingress_receipts", `tenant_id=?`, tenant, 1)
					assertTelemetryCount(t, f.db, "broker_ingress_effects", `tenant_id=?`, tenant, 1)
				}
			})
		}
	}
}

func TestTelemetryNewerCurrentIsValidDespiteZeroRowUpsertPostgres(t *testing.T) {
	for _, path := range []string{"legacy", "trusted"} {
		t.Run(path, func(t *testing.T) {
			f := newTelemetryBrokerFixture(t)
			tenant, device, cell := "tenant-newer-current", "device-newer-current", "cell-newer-current"
			f.insertDevice(t, tenant, device, "")
			f.insertBinding(t, tenant, device, cell, 1, "active")
			ts := time.Now().Add(-time.Second).UnixMilli()
			newer := 9.0
			if err := f.db.Create(&TelemetryData{DeviceID: device, TenantID: tenant, Key: "temp", TS: ts, NumberV: &newer}).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.db.Create(&TelemetryCurrentData{DeviceID: device, TenantID: tenant, Key: "temp", TS: time.UnixMilli(ts), NumberV: &newer}).Error; err != nil {
				t.Fatal(err)
			}
			item := telemetryTrustedItem(tenant, device, device, uuid.NewString(), cell, 1, ts-1000, "temp", 1.0)
			var callbacks atomic.Int32
			item.afterCommit = func() { callbacks.Add(1) }
			w := telemetryBrokerWriter(f)
			if path == "legacy" {
				item.route = nil
				w.doFlushLegacy([]*telemetryBatchItem{item})
			} else {
				w.doFlushTrusted([]*telemetryBatchItem{item})
			}
			metrics := w.metrics.GetMetrics()
			if metrics.TelemetryWritten != 1 || metrics.TelemetryFailed != 0 || metrics.TelemetryUnknown != 0 {
				t.Fatalf("legitimate newer current rejected: %+v", metrics)
			}
			var current TelemetryCurrentData
			if err := f.db.Where("device_id=? AND key=?", device, "temp").Take(&current).Error; err != nil {
				t.Fatal(err)
			}
			if current.TS.UnixMilli() != ts || current.NumberV == nil || *current.NumberV != newer {
				t.Fatalf("newer current changed: %+v", current)
			}
			assertTelemetryCount(t, f.db, "telemetry_datas", `tenant_id=? AND device_id=?`, tenant, device, 2)
			if path == "trusted" && callbacks.Load() != 1 {
				t.Fatal("valid trusted history was not acknowledged")
			}
		})
	}
}
