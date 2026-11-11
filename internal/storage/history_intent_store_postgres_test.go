package storage

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"testing"
	"time"

	"gorm.io/gorm"
)

func snapshotTelemetry(t *testing.T, db *gorm.DB) (history []string, current []string) {
	t.Helper()
	var h []TelemetryData
	if err := db.Order("device_id, key, ts").Find(&h).Error; err != nil {
		t.Fatal(err)
	}
	for _, p := range h {
		history = append(history, fmt.Sprintf("%s|%s|%d|%s|%s|%s|%s", p.DeviceID, p.Key, p.TS, ptr(p.BoolV), ptr(p.NumberV), ptr(p.StringV), p.TenantID))
	}
	var c []TelemetryCurrentData
	if err := db.Order("device_id, key").Find(&c).Error; err != nil {
		t.Fatal(err)
	}
	for _, p := range c {
		current = append(current, fmt.Sprintf("%s|%s|%d|%s|%s|%s|%s", p.DeviceID, p.Key, p.TS.UnixMilli(), ptr(p.BoolV), ptr(p.NumberV), ptr(p.StringV), p.TenantID))
	}
	sort.Strings(history)
	sort.Strings(current)
	return
}

func ptr[T any](v *T) string {
	if v == nil {
		return "nil"
	}
	return fmt.Sprint(*v)
}

func randomBatches(seed int64) [][]TelemetryData {
	r := rand.New(rand.NewSource(seed))
	var batches [][]TelemetryData
	for b := 0; b < 12; b++ {
		var batch []TelemetryData
		for i := 0; i < 1+r.Intn(40); i++ {
			v := float64(r.Intn(100))
			p := TelemetryData{DeviceID: fmt.Sprintf("dev-%d", r.Intn(3)), Key: fmt.Sprintf("k%d", r.Intn(4)), TS: int64(1000 + r.Intn(60)), TenantID: "tenant-a"}
			switch r.Intn(3) {
			case 0:
				p.NumberV = &v
			case 1:
				bv := r.Intn(2) == 0
				p.BoolV = &bv
			default:
				sv := fmt.Sprintf("s%d", r.Intn(50))
				p.StringV = &sv
			}
			batch = append(batch, p)
		}
		batches = append(batches, batch)
	}
	return batches
}

// With TEST_BROKER_TIMESCALE=1 the history table becomes a TimescaleDB hypertable, the
// layout of the production Timescale profile (sql/1.sql).
func maybeHypertable(t *testing.T, db *gorm.DB) {
	t.Helper()
	if os.Getenv("TEST_BROKER_TIMESCALE") != "1" {
		return
	}
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS timescaledb WITH SCHEMA public`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`SELECT public.create_hypertable('telemetry_datas','ts',chunk_time_interval=>86400000::bigint,if_not_exists=>true)`).Error; err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.Raw(`SELECT count(*) FROM timescaledb_information.hypertables WHERE hypertable_name='telemetry_datas' AND hypertable_schema=current_schema()`).Scan(&n).Error; err != nil || n != 1 {
		t.Fatalf("telemetry_datas is not a hypertable: n=%d err=%v", n, err)
	}
}

func commit(t *testing.T, db *gorm.DB, store TelemetryHistoryStore, batch []TelemetryData) int {
	t.Helper()
	tx := db.Begin()
	n, err := store.AppendInTx(tx, batch)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("append: %v", err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	return n
}

// The intent path must leave exactly the same history and snapshot as the legacy
// transactional path for the same sequence of batches, including duplicates,
// reordered timestamps and conflicting values for one identity.
func TestIntentHistoryStoreMatchesLegacyPath(t *testing.T) {
	if os.Getenv("TP_TELEMETRY_BROKER_TEST_DSN") == "" {
		t.Skip("TP_TELEMETRY_BROKER_TEST_DSN required")
	}
	for seed := int64(1); seed <= 6; seed++ {
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			legacy := newTelemetryBrokerFixture(t)
			intent := newTelemetryBrokerFixture(t)
			maybeHypertable(t, legacy.db)
			maybeHypertable(t, intent.db)
			migration, err := os.ReadFile("../../sql/history_intent/001_intents.sql")
			if err != nil {
				t.Fatal(err)
			}
			if err := intent.db.Exec(string(migration)).Error; err != nil {
				t.Fatal(err)
			}
			legacyStore, intentStore := TelemetryHistoryStore(postgresHistoryStore{}), NewIntentHistoryStore(0)
			sink := NewPostgresHistorySink(intent.db)
			for i, batch := range randomBatches(seed) {
				a := commit(t, legacy.db, legacyStore, batch)
				b := commit(t, intent.db, intentStore, batch)
				if a != b {
					t.Fatalf("batch %d: legacy stored %d, intent stored %d", i, a, b)
				}
				// Replay at arbitrary points, not only at the end.
				if i%3 == 0 {
					if _, err := ReplayHistoryIntents(context.Background(), intent.db, sink, "w", 10, time.Minute); err != nil {
						t.Fatal(err)
					}
				}
				_, curA := snapshotTelemetry(t, legacy.db)
				_, curB := snapshotTelemetry(t, intent.db)
				if fmt.Sprint(curA) != fmt.Sprint(curB) {
					t.Fatalf("batch %d: snapshots differ\nlegacy=%v\nintent=%v", i, curA, curB)
				}
			}
			for {
				n, err := ReplayHistoryIntents(context.Background(), intent.db, sink, "w", 50, time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				if n == 0 {
					break
				}
			}
			histA, curA := snapshotTelemetry(t, legacy.db)
			histB, curB := snapshotTelemetry(t, intent.db)
			if fmt.Sprint(histA) != fmt.Sprint(histB) {
				t.Fatalf("history differs\nlegacy=%v\nintent=%v", histA, histB)
			}
			if fmt.Sprint(curA) != fmt.Sprint(curB) {
				t.Fatalf("final snapshots differ")
			}
			if len(histA) == 0 {
				t.Fatal("test produced no history")
			}
		})
	}
}

func TestIntentHistoryStoreRollsBackIntentsWithSnapshot(t *testing.T) {
	f := newTelemetryBrokerFixture(t)
	migration, _ := os.ReadFile("../../sql/history_intent/001_intents.sql")
	if err := f.db.Exec(string(migration)).Error; err != nil {
		t.Fatal(err)
	}
	v := 1.0
	tx := f.db.Begin()
	if n, err := NewIntentHistoryStore(0).AppendInTx(tx, []TelemetryData{{DeviceID: "d", Key: "k", TS: 5, NumberV: &v, TenantID: "t"}}); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	var intents, current int64
	f.db.Raw(`SELECT count(*) FROM telemetry_history_intents`).Scan(&intents)
	f.db.Raw(`SELECT count(*) FROM telemetry_current_datas`).Scan(&current)
	if intents != 0 || current != 0 {
		t.Fatalf("rollback left intents=%d snapshot=%d", intents, current)
	}
}
