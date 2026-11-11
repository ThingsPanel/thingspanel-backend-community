package storage

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"gorm.io/gorm"
)

type recordingDeleter struct {
	deleted []string
	failNow error
}

func (r *recordingDeleter) DeleteDevice(_ context.Context, deviceID string) error {
	if r.failNow != nil {
		return r.failNow
	}
	r.deleted = append(r.deleted, deviceID)
	return nil
}

func seedIntents(t *testing.T, db *gorm.DB, devices ...string) {
	t.Helper()
	var points []TelemetryData
	for i, d := range devices {
		v := float64(i)
		points = append(points, TelemetryData{DeviceID: d, Key: "temp", TS: int64(1000 + i), NumberV: &v, TenantID: "tenant-a"})
	}
	tx := db.Begin()
	if _, err := EnqueueHistoryIntents(tx, 0, points); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
}

func count(t *testing.T, db *gorm.DB, query string, args ...interface{}) int {
	t.Helper()
	var n int
	if err := db.Raw(query, args...).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestHistoryDeletionCancelsUnclaimedIntentsInTheSameTransaction(t *testing.T) {
	db := newHistoryIntentDB(t)
	seedIntents(t, db, "dev-a", "dev-b")
	tx := db.Begin()
	if err := EnqueueHistoryDeviceDeletion(tx, "dev-a"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	if count(t, db, `SELECT count(*) FROM telemetry_history_intents`) != 2 || count(t, db, `SELECT count(*) FROM telemetry_history_deletions`) != 0 {
		t.Fatal("a rolled-back device deletion must leave intents and deletions untouched")
	}
	tx = db.Begin()
	if err := EnqueueHistoryDeviceDeletion(tx, "dev-a"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	if count(t, db, `SELECT count(*) FROM telemetry_history_intents WHERE device_id='dev-a'`) != 0 ||
		count(t, db, `SELECT count(*) FROM telemetry_history_intents WHERE device_id='dev-b'`) != 1 ||
		count(t, db, `SELECT count(*) FROM telemetry_history_deletions WHERE state='pending'`) != 1 {
		t.Fatal("only the deleted device's queued intents may disappear")
	}
	if err := EnqueueHistoryDeviceDeletion(db, ""); err == nil {
		t.Fatal("an empty device id must be rejected")
	}
}

func TestHistoryDeletionWaitsForLiveClaimsRetriesAndIsIdempotent(t *testing.T) {
	db := newHistoryIntentDB(t)
	seedIntents(t, db, "dev-a")
	ctx := context.Background()
	// A replayer is mid-write for dev-a: its claim is live.
	failing := newDedupSink()
	failing.failNow = errors.New("backend slow")
	if _, err := ReplayHistoryIntents(ctx, db, failing, "w1", 10, time.Minute); err == nil {
		t.Fatal("expected the sink failure")
	}
	if err := EnqueueHistoryDeviceDeletion(db, "dev-a"); err != nil {
		t.Fatal(err)
	}
	deleter := &recordingDeleter{}
	if n, err := ReplayHistoryDeletions(ctx, db, deleter, "d1", 10, time.Minute); err != nil || n != 0 || len(deleter.deleted) != 0 {
		t.Fatalf("a deletion must wait for a live claim: n=%d err=%v deleted=%v", n, err, deleter.deleted)
	}
	if err := db.Exec(`UPDATE telemetry_history_intents SET claimed_until = NOW() - interval '1 second'`).Error; err != nil {
		t.Fatal(err)
	}
	deleter.failNow = errors.New("backend down")
	if _, err := ReplayHistoryDeletions(ctx, db, deleter, "d1", 10, time.Minute); err == nil {
		t.Fatal("a failing backend must surface")
	}
	if count(t, db, `SELECT count(*) FROM telemetry_history_deletions WHERE state='pending'`) != 1 {
		t.Fatal("a failed deletion must stay pending")
	}
	if count(t, db, `SELECT count(*) FROM telemetry_history_intents WHERE device_id='dev-a'`) != 0 {
		t.Fatal("once the deletion runs, the device's leftover intents must be superseded")
	}
	if err := db.Exec(`UPDATE telemetry_history_deletions SET claimed_until = NOW() - interval '1 second'`).Error; err != nil {
		t.Fatal(err)
	}
	deleter.failNow = nil
	if n, err := ReplayHistoryDeletions(ctx, db, deleter, "d2", 10, time.Minute); err != nil || n != 1 || fmt.Sprint(deleter.deleted) != "[dev-a]" {
		t.Fatalf("n=%d err=%v deleted=%v", n, err, deleter.deleted)
	}
	if n, _ := ReplayHistoryDeletions(ctx, db, deleter, "d2", 10, time.Minute); n != 0 || len(deleter.deleted) != 1 {
		t.Fatal("an applied deletion must not run again")
	}
}
