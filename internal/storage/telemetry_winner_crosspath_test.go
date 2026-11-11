package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type telemetryOutcomePool struct {
	*sql.DB
	begins, commits, rollbacks atomic.Int32
	commitLoss, rollbackLoss   bool
}

func (p *telemetryOutcomePool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	p.begins.Add(1)
	tx, e := p.DB.BeginTx(ctx, opts)
	if e != nil {
		return nil, e
	}
	return &telemetryOutcomeTx{Tx: tx, p: p}, nil
}

type telemetryOutcomeTx struct {
	*sql.Tx
	p *telemetryOutcomePool
}

func (t *telemetryOutcomeTx) Commit() error {
	t.p.commits.Add(1)
	if e := t.Tx.Commit(); e != nil {
		return e
	}
	if t.p.commitLoss {
		return errors.New("injected lost commit response")
	}
	return nil
}
func (t *telemetryOutcomeTx) Rollback() error {
	t.p.rollbacks.Add(1)
	if e := t.Tx.Rollback(); e != nil {
		return e
	}
	if t.p.rollbackLoss {
		return errors.New("injected lost rollback response")
	}
	return nil
}

func TestTelemetryCrossPathHistoryWinnerAndTrustedEffect(t *testing.T) {
	for _, first := range []string{"legacy", "trusted"} {
		t.Run(first, func(t *testing.T) {
			f := newTelemetryBrokerFixture(t)
			tenant, device, cell := "tenant-cross", "device-cross", "cell-cross"
			f.insertDevice(t, tenant, device, "")
			f.insertBinding(t, tenant, device, cell, 1, "active")
			ts := time.Now().Add(-time.Second).UnixMilli()
			w := telemetryBrokerWriter(f)
			legacyValue, trustedValue := 1.0, 2.0
			legacy := TelemetryData{DeviceID: device, TenantID: tenant, Key: "temp", TS: ts, NumberV: &legacyValue}
			item := telemetryTrustedItem(tenant, device, device, uuid.NewString(), cell, 1, ts, "temp", trustedValue)
			doLegacy := func() {
				n, fail, unknown := w.batchInsert([]TelemetryData{legacy}, nil)
				if n != 1 || fail != 0 || unknown != 0 {
					t.Fatalf("legacy %d/%d/%d", n, fail, unknown)
				}
			}
			doTrusted := func() {
				_, _, _, err := w.commitTrustedTelemetry([]*telemetryBatchItem{item})
				if err != nil {
					t.Fatal(err)
				}
			}
			if first == "legacy" {
				doLegacy()
				doTrusted()
			} else {
				doTrusted()
				doLegacy()
			}
			want := legacyValue
			if first == "trusted" {
				want = trustedValue
			}
			var h TelemetryData
			var c TelemetryCurrentData
			if e := f.db.Where("device_id=? AND key=? AND ts=?", device, "temp", ts).Take(&h).Error; e != nil {
				t.Fatal(e)
			}
			if e := f.db.Where("device_id=? AND key=?", device, "temp").Take(&c).Error; e != nil {
				t.Fatal(e)
			}
			if h.NumberV == nil || c.NumberV == nil || *h.NumberV != want || *c.NumberV != want {
				t.Fatalf("history/current mismatch %v %v want %v", h.NumberV, c.NumberV, want)
			}
			var effect string
			if e := f.db.Raw(`SELECT payload->'trigger_values'->>'temp' FROM broker_ingress_effects WHERE receipt_id=?`, item.route.ReceiptID).Scan(&effect).Error; e != nil || effect != "2" {
				t.Fatalf("trusted input effect=%q err=%v", effect, e)
			}
		})
	}
}

func TestTrustedTelemetryUnknownDoesNotFallback(t *testing.T) {
	for _, kind := range []string{"commit", "rollback"} {
		t.Run(kind, func(t *testing.T) {
			f := newTelemetryBrokerFixture(t)
			tenant, device, cell := "tenant-unknown", "device-unknown", "cell-unknown"
			f.insertDevice(t, tenant, device, "")
			f.insertBinding(t, tenant, device, cell, 1, "active")
			if kind == "rollback" {
				if e := f.db.Exec(`ALTER TABLE telemetry_datas ADD CONSTRAINT reject_unknown CHECK ("key" <> 'bad')`).Error; e != nil {
					t.Fatal(e)
				}
			}
			pool := &telemetryOutcomePool{DB: f.dbSQL, commitLoss: kind == "commit", rollbackLoss: kind == "rollback"}
			db, e := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true})
			if e != nil {
				t.Fatal(e)
			}
			w := newTelemetryWriter(db, telemetryTestLogger{}, Config{TelemetryBatchSize: 32}, newMetricsCollector())
			ts := time.Now().Add(-time.Second).UnixMilli()
			key := "a"
			if kind == "rollback" {
				key = "bad"
			}
			var callbacks atomic.Int32
			a := telemetryTrustedItem(tenant, device, device, uuid.NewString(), cell, 1, ts, key, 1.0)
			b := telemetryTrustedItem(tenant, device, device, uuid.NewString(), cell, 1, ts+1, "b", 2.0)
			a.afterCommit = func() { callbacks.Add(1) }
			b.afterCommit = func() { callbacks.Add(1) }
			w.doFlushTrusted([]*telemetryBatchItem{a, b})
			m := w.metrics.GetMetrics()
			if pool.begins.Load() != 1 || m.TelemetryUnknown != 2 || m.TelemetryFailed != 0 || m.TelemetryWritten != 0 || callbacks.Load() != 0 {
				t.Fatalf("%s begins=%d metrics=%+v callbacks=%d", kind, pool.begins.Load(), m, callbacks.Load())
			}
			want := 0
			if kind == "commit" {
				want = 2
			}
			assertTelemetryCount(t, f.db, "broker_ingress_receipts", `tenant_id=? AND stream='telemetry_sample'`, tenant, want)
		})
	}
}

func TestTrustedTelemetryPartialFallbackAndMetrics(t *testing.T) {
	f := newTelemetryBrokerFixture(t)
	tenant, device, cell := "tenant-partial", "device-partial", "cell-partial"
	f.insertDevice(t, tenant, device, "")
	f.insertBinding(t, tenant, device, cell, 1, "active")
	if e := f.db.Exec(`ALTER TABLE telemetry_datas ADD CONSTRAINT reject_partial CHECK ("key" <> 'bad')`).Error; e != nil {
		t.Fatal(e)
	}
	w := telemetryBrokerWriter(f)
	ts := time.Now().Add(-time.Second).UnixMilli()
	var calls atomic.Int32
	good := telemetryTrustedItem(tenant, device, device, uuid.NewString(), cell, 1, ts, "ok", 1.0)
	bad := telemetryTrustedItem(tenant, device, device, uuid.NewString(), cell, 1, ts, "bad", 2.0)
	good.afterCommit = func() { calls.Add(1) }
	bad.afterCommit = func() { calls.Add(1) }
	w.doFlushTrusted([]*telemetryBatchItem{good, bad})
	m := w.metrics.GetMetrics()
	if m.TelemetryWritten != 1 || m.TelemetryFailed != 1 || m.TelemetryUnknown != 0 || calls.Load() != 1 {
		t.Fatal(fmt.Sprintf("metrics=%+v callbacks=%d", m, calls.Load()))
	}
	assertTelemetryCount(t, f.db, "broker_ingress_receipts", `tenant_id=? AND stream='telemetry_sample'`, tenant, 1)
}

func TestTrustedWaitsForLegacyHistoryWinner(t *testing.T) {
	f := newTelemetryBrokerFixture(t)
	tenant, device, cell := "tenant-wait", "device-wait", "cell-wait"
	f.insertDevice(t, tenant, device, "")
	f.insertBinding(t, tenant, device, cell, 1, "active")
	conn, err := f.dbSQL.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var pid int
	if err = conn.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	tx, err := conn.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Now().Add(-time.Second).UnixMilli()
	if _, err = tx.ExecContext(context.Background(), `INSERT INTO telemetry_datas(device_id,"key",ts,number_v,tenant_id) VALUES($1,'temp',$2,1,$3)`, device, ts, tenant); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	item := telemetryTrustedItem(tenant, device, device, uuid.NewString(), cell, 1, ts, "temp", 2.0)
	go func() {
		_, _, _, e := telemetryBrokerWriter(f).commitTrustedTelemetry([]*telemetryBatchItem{item})
		done <- e
	}()
	finished := false
	defer func() {
		_ = tx.Rollback()
		if !finished {
			select {
			case <-done:
			case <-time.After(7 * time.Second):
				t.Error("trusted worker did not stop")
			}
		}
	}()
	deadline := time.Now().Add(3 * time.Second)
	waited := false
	for time.Now().Before(deadline) {
		var n int64
		err = f.admin.Raw(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND ? = ANY(pg_blocking_pids(pid))`, pid).Scan(&n).Error
		if err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			waited = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waited {
		t.Fatal("trusted transaction did not wait on held legacy history key")
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		finished = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("trusted transaction timed out")
	}
	var row TelemetryCurrentData
	if err = f.db.Where("device_id=? AND key=?", device, "temp").Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.NumberV == nil || *row.NumberV != 1 {
		t.Fatalf("current does not follow committed legacy history: %+v", row)
	}
}

func TestTrustedDistinctReceiptsPreservePayloadAndSameReceiptConflict(t *testing.T) {
	f := newTelemetryBrokerFixture(t)
	tenant, device, cell := "tenant-receipts", "device-receipts", "cell-receipts"
	f.insertDevice(t, tenant, device, "")
	f.insertBinding(t, tenant, device, cell, 1, "active")
	ts := time.Now().Add(-time.Second).UnixMilli()
	w := telemetryBrokerWriter(f)
	a := telemetryTrustedItem(tenant, device, device, uuid.NewString(), cell, 1, ts, "temp", 1.0)
	b := telemetryTrustedItem(tenant, device, device, uuid.NewString(), cell, 1, ts, "temp", 2.0)
	for _, item := range []*telemetryBatchItem{a, b} {
		if _, _, _, err := w.commitTrustedTelemetry([]*telemetryBatchItem{item}); err != nil {
			t.Fatal(err)
		}
	}
	conflict := telemetryTrustedItem(tenant, device, device, a.route.ReceiptID, cell, 1, ts, "temp", 9.0)
	if _, _, _, err := w.commitTrustedTelemetry([]*telemetryBatchItem{conflict}); err == nil {
		t.Fatal("same receipt with different payload accepted")
	}
	var values []string
	if err := f.db.Raw(`SELECT payload->'trigger_values'->>'temp' FROM broker_ingress_effects WHERE receipt_id IN (?,?) ORDER BY payload->'trigger_values'->>'temp'`, a.route.ReceiptID, b.route.ReceiptID).Scan(&values).Error; err != nil || len(values) != 2 || values[0] != "1" || values[1] != "2" {
		t.Fatalf("effects=%v err=%v", values, err)
	}
	var current TelemetryCurrentData
	if err := f.db.Where("device_id=? AND key=?", device, "temp").Take(&current).Error; err != nil || current.NumberV == nil || *current.NumberV != 1 {
		t.Fatalf("current=%+v err=%v", current, err)
	}
}

func TestTelemetryDeduplicateUsesStructuredKeys(t *testing.T) {
	w := &telemetryWriter{}
	batch := []*telemetryBatchItem{{deviceID: "a|b", tenantID: "tenant", timestamp: 1, points: []TelemetryDataPoint{{Key: "c", Value: 1.0}}}, {deviceID: "a", tenantID: "tenant", timestamp: 1, points: []TelemetryDataPoint{{Key: "b|c", Value: 2.0}}}}
	history, current, duplicates := w.deduplicateAndConvert(batch)
	if len(history) != 2 || len(current) != 2 || duplicates != 0 {
		t.Fatalf("history=%d current=%d duplicates=%d", len(history), len(current), duplicates)
	}
}

func TestTrustedMixedFencedAndUnknownMetrics(t *testing.T) {
	for _, kind := range []string{"commit", "rollback"} {
		t.Run(kind, func(t *testing.T) {
			f := newTelemetryBrokerFixture(t)
			tenant, device, cell := "tenant-mixed-u", "device-mixed-u", "cell-mixed-u"
			f.insertDevice(t, tenant, device, "")
			f.insertBinding(t, tenant, device, cell, 1, "active")
			if kind == "rollback" {
				if err := f.db.Exec(`ALTER TABLE telemetry_datas ADD CONSTRAINT reject_mixed_unknown CHECK ("key" <> 'bad')`).Error; err != nil {
					t.Fatal(err)
				}
			}
			pool := &telemetryOutcomePool{DB: f.dbSQL, commitLoss: kind == "commit", rollbackLoss: kind == "rollback"}
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			w := newTelemetryWriter(db, telemetryTestLogger{}, Config{TelemetryBatchSize: 32}, newMetricsCollector())
			ts := time.Now().Add(-time.Second).UnixMilli()
			key := "good"
			if kind == "rollback" {
				key = "bad"
			}
			fenced := telemetryTrustedItem(tenant, device, device, uuid.NewString(), cell, 2, ts, "old", 1.0)
			valid := telemetryTrustedItem(tenant, device, device, uuid.NewString(), cell, 1, ts, key, 2.0)
			var callbacks atomic.Int32
			fenced.afterCommit = func() { callbacks.Add(1) }
			valid.afterCommit = func() { callbacks.Add(1) }
			w.doFlushTrusted([]*telemetryBatchItem{fenced, valid})
			m := w.metrics.GetMetrics()
			if m.TelemetryFailed != 1 || m.TelemetryUnknown != 1 || m.TelemetryWritten != 0 || pool.begins.Load() != 1 || callbacks.Load() != 0 {
				t.Fatalf("kind=%s metrics=%+v begins=%d callbacks=%d", kind, m, pool.begins.Load(), callbacks.Load())
			}
		})
	}
}

func TestTrustedTransportBindingKeysDoNotCollide(t *testing.T) {
	f := newTelemetryBrokerFixture(t)
	f.insertDevice(t, "a/b", "c", "")
	f.insertBinding(t, "a/b", "c", "cell-one", 1, "active")
	f.insertDevice(t, "a", "b/c", "")
	f.insertBinding(t, "a", "b/c", "cell-two", 2, "active")
	ts := time.Now().Add(-time.Second).UnixMilli()
	w := telemetryBrokerWriter(f)
	a := telemetryTrustedItem("a/b", "c", "c", uuid.NewString(), "cell-one", 1, ts, "k", 1.0)
	b := telemetryTrustedItem("a", "b/c", "b/c", uuid.NewString(), "cell-two", 2, ts, "k", 2.0)
	accepted, written, failed, err := w.commitTrustedTelemetry([]*telemetryBatchItem{a, b})
	if err != nil || len(accepted) != 2 || written != 2 || failed != 0 {
		t.Fatalf("accepted=%d written=%d failed=%d err=%v", len(accepted), written, failed, err)
	}
	assertTelemetryCount(t, f.db, "broker_ingress_receipts", `id IN (?,?)`, a.route.ReceiptID, b.route.ReceiptID, 2)
}

func TestTrustedAllFencedCommitUnknownCountsOnlyFailed(t *testing.T) {
	f := newTelemetryBrokerFixture(t)
	tenant, device, cell := "tenant-fenced-u", "device-fenced-u", "cell-fenced-u"
	f.insertDevice(t, tenant, device, "")
	f.insertBinding(t, tenant, device, cell, 1, "active")
	pool := &telemetryOutcomePool{DB: f.dbSQL, commitLoss: true}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	w := newTelemetryWriter(db, telemetryTestLogger{}, Config{TelemetryBatchSize: 32}, newMetricsCollector())
	ts := time.Now().Add(-time.Second).UnixMilli()
	a := telemetryTrustedItem(tenant, device, device, uuid.NewString(), cell, 2, ts, "a", 1.0)
	b := telemetryTrustedItem(tenant, device, device, uuid.NewString(), cell, 3, ts, "b", 2.0)
	w.doFlushTrusted([]*telemetryBatchItem{a, b})
	m := w.metrics.GetMetrics()
	if m.TelemetryFailed != 2 || m.TelemetryUnknown != 0 || m.TelemetryWritten != 0 || pool.begins.Load() != 1 {
		t.Fatalf("metrics=%+v begins=%d", m, pool.begins.Load())
	}
}
