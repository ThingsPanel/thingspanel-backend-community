package storage

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"context"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newHistoryIntentDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TP_TELEMETRY_BROKER_TEST_DSN")
	if dsn == "" {
		t.Skip("TP_TELEMETRY_BROKER_TEST_DSN required")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open isolated PostgreSQL: %v", err)
	}
	schema := "history_intent_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatalf("create private schema: %v", err)
	}
	db, err := gorm.Open(postgres.Open(telemetryBrokerDSNWithSchema(dsn, schema)), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	adminPool, _ := admin.DB()
	t.Cleanup(func() {
		_ = pool.Close()
		_ = admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`).Error
		_ = adminPool.Close()
	})
	migration, err := os.ReadFile("../../sql/history_intent/001_intents.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(migration)).Error; err != nil {
		t.Fatalf("apply migration: %v", err)
	}
	return db
}

func intentPoints(n int) []TelemetryData {
	points := make([]TelemetryData, n)
	for i := range points {
		v := float64(i)
		points[i] = TelemetryData{DeviceID: "dev-1", Key: fmt.Sprintf("k%d", i%5), TS: int64(1000 + i), NumberV: &v, TenantID: "tenant-a"}
	}
	return points
}

// dedupSink is an idempotent backend keyed by the intent identity.
type dedupSink struct {
	mu      sync.Mutex
	stored  map[string]HistoryIntent
	appends int
	failNow error
	batches [][]int64
}

func newDedupSink() *dedupSink { return &dedupSink{stored: map[string]HistoryIntent{}} }

func (s *dedupSink) Append(_ context.Context, intents []HistoryIntent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNow != nil {
		return s.failNow
	}
	s.appends++
	ids := make([]int64, 0, len(intents))
	for _, in := range intents {
		s.stored[fmt.Sprintf("%s|%s|%d|%d", in.DeviceID, in.Key, in.TS, in.Generation)] = in
		ids = append(ids, in.ID)
	}
	s.batches = append(s.batches, ids)
	return nil
}

func countState(t *testing.T, db *gorm.DB, state string) int {
	t.Helper()
	var n int
	if err := db.Raw(`SELECT count(*) FROM telemetry_history_intents WHERE state = ?`, state).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestHistoryIntentEnqueueIsIdempotentAndTransactional(t *testing.T) {
	db := newHistoryIntentDB(t)
	points := intentPoints(20)
	tx := db.Begin()
	if n, err := EnqueueHistoryIntents(tx, 0, points); err != nil || n != 20 {
		t.Fatalf("first enqueue n=%d err=%v", n, err)
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	if got := countState(t, db, "pending"); got != 0 {
		t.Fatalf("rolled-back transaction left %d intents", got)
	}
	for round, want := range []int{20, 0} {
		tx := db.Begin()
		n, err := EnqueueHistoryIntents(tx, 0, points)
		if err != nil || n != want {
			t.Fatalf("round %d: n=%d err=%v want %d", round, n, err, want)
		}
		if err := tx.Commit().Error; err != nil {
			t.Fatal(err)
		}
	}
	// A new generation (for example after a delete) is a distinct identity.
	tx = db.Begin()
	if n, err := EnqueueHistoryIntents(tx, 1, points[:3]); err != nil || n != 3 {
		t.Fatalf("generation 1 n=%d err=%v", n, err)
	}
	_ = tx.Commit()
	if got := countState(t, db, "pending"); got != 23 {
		t.Fatalf("pending=%d want 23", got)
	}
	if _, err := EnqueueHistoryIntents(db.Begin(), 0, []TelemetryData{{DeviceID: "d", Key: "k", TS: 1}}); err == nil {
		t.Fatal("an intent without a tenant must be rejected")
	}
}

func TestHistoryIntentReplayAppliesOnceAndStops(t *testing.T) {
	db := newHistoryIntentDB(t)
	tx := db.Begin()
	if _, err := EnqueueHistoryIntents(tx, 0, intentPoints(25)); err != nil {
		t.Fatal(err)
	}
	_ = tx.Commit()
	sink := newDedupSink()
	total := 0
	for i := 0; i < 5; i++ {
		n, err := ReplayHistoryIntents(context.Background(), db, sink, "w1", 10, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		total += n
	}
	if total != 25 || len(sink.stored) != 25 || countState(t, db, "applied") != 25 || countState(t, db, "pending") != 0 {
		t.Fatalf("total=%d stored=%d applied=%d pending=%d", total, len(sink.stored), countState(t, db, "applied"), countState(t, db, "pending"))
	}
	if n, err := ReplayHistoryIntents(context.Background(), db, sink, "w1", 10, time.Minute); err != nil || n != 0 {
		t.Fatalf("nothing left to replay, got n=%d err=%v", n, err)
	}
}

func TestHistoryIntentSinkFailureKeepsPendingAndLeaseExpiryRetries(t *testing.T) {
	db := newHistoryIntentDB(t)
	tx := db.Begin()
	if _, err := EnqueueHistoryIntents(tx, 0, intentPoints(5)); err != nil {
		t.Fatal(err)
	}
	_ = tx.Commit()
	sink := newDedupSink()
	sink.failNow = errors.New("backend outcome unknown")
	if _, err := ReplayHistoryIntents(context.Background(), db, sink, "w1", 10, time.Minute); err == nil {
		t.Fatal("a sink error must be returned")
	}
	if countState(t, db, "pending") != 5 || countState(t, db, "applied") != 0 {
		t.Fatal("failed append must leave every intent pending")
	}
	sink.failNow = nil
	if n, _ := ReplayHistoryIntents(context.Background(), db, sink, "w2", 10, time.Minute); n != 0 {
		t.Fatalf("an unexpired lease must not be stolen, got %d", n)
	}
	if err := db.Exec(`UPDATE telemetry_history_intents SET claimed_until = NOW() - interval '1 second'`).Error; err != nil {
		t.Fatal(err)
	}
	if n, err := ReplayHistoryIntents(context.Background(), db, sink, "w2", 10, time.Minute); err != nil || n != 5 {
		t.Fatalf("after lease expiry n=%d err=%v", n, err)
	}
	var attempts int
	_ = db.Raw(`SELECT max(attempts) FROM telemetry_history_intents`).Scan(&attempts).Error
	if attempts != 2 {
		t.Fatalf("attempts=%d want 2", attempts)
	}
}

func TestHistoryIntentLostAckReplayDoesNotDuplicate(t *testing.T) {
	db := newHistoryIntentDB(t)
	tx := db.Begin()
	if _, err := EnqueueHistoryIntents(tx, 0, intentPoints(8)); err != nil {
		t.Fatal(err)
	}
	_ = tx.Commit()
	sink := newDedupSink()
	if n, err := ReplayHistoryIntents(context.Background(), db, sink, "w1", 100, time.Minute); err != nil || n != 8 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	// Simulate a crash between the backend write and the acknowledgement.
	if err := db.Exec(`UPDATE telemetry_history_intents SET state='pending', applied_at=NULL`).Error; err != nil {
		t.Fatal(err)
	}
	if n, err := ReplayHistoryIntents(context.Background(), db, sink, "w2", 100, time.Minute); err != nil || n != 8 {
		t.Fatalf("replay n=%d err=%v", n, err)
	}
	if len(sink.stored) != 8 || sink.appends != 2 {
		t.Fatalf("stored=%d appends=%d: replay must be idempotent at the sink", len(sink.stored), sink.appends)
	}
}

func TestHistoryIntentConcurrentReplayersClaimDisjointBatches(t *testing.T) {
	db := newHistoryIntentDB(t)
	tx := db.Begin()
	if _, err := EnqueueHistoryIntents(tx, 0, intentPoints(200)); err != nil {
		t.Fatal(err)
	}
	_ = tx.Commit()
	sink := newDedupSink()
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				if _, err := ReplayHistoryIntents(context.Background(), db, sink, fmt.Sprintf("w%d", w), 7, time.Minute); err != nil {
					errs <- err
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	seen := map[int64]int{}
	for _, b := range sink.batches {
		for _, id := range b {
			seen[id]++
		}
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("intent %d was claimed %d times", id, n)
		}
	}
	if len(seen) != 200 || countState(t, db, "applied") != 200 {
		t.Fatalf("claimed=%d applied=%d want 200", len(seen), countState(t, db, "applied"))
	}
}
