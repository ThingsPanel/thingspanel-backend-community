package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type commitReplyLossPool struct {
	*sql.DB
	commits   atomic.Int32
	begins    atomic.Int32
	loseReply bool
}

func (p *commitReplyLossPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	p.begins.Add(1)
	tx, err := p.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &commitReplyLossTx{Tx: tx, pool: p}, nil
}

type commitReplyLossTx struct {
	*sql.Tx
	pool *commitReplyLossPool
}

func (tx *commitReplyLossTx) Commit() error {
	tx.pool.commits.Add(1)
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	if tx.pool.loseReply {
		return errors.New("injected lost commit response")
	}
	return nil
}

func TestLegacyCommitUnknownDoesNotFallback(t *testing.T) {
	f := newTelemetryBrokerFixture(t)
	pool := &commitReplyLossPool{DB: f.dbSQL, loseReply: true}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	w := newTelemetryWriter(db, telemetryTestLogger{}, Config{TelemetryBatchSize: 32}, newMetricsCollector())
	v := 7.0
	points := []TelemetryData{{DeviceID: "commit-unknown", Key: "x", TS: 1000, NumberV: &v}, {DeviceID: "commit-unknown", Key: "y", TS: 1000, NumberV: &v}}
	written, failed, unknown := w.batchInsert(points, nil)
	if written != 0 || failed != 0 || unknown != len(points) {
		t.Fatalf("written=%d failed=%d unknown=%d", written, failed, unknown)
	}
	if got := pool.commits.Load(); got != 1 {
		t.Fatalf("commit attempts=%d; unexpected fallback", got)
	}
	if got := pool.begins.Load(); got != 1 {
		t.Fatalf("transaction begins=%d; unexpected fallback", got)
	}
	var count int64
	if err := f.db.Model(&TelemetryData{}).Where("device_id = ?", "commit-unknown").Count(&count).Error; err != nil || count != int64(len(points)) {
		t.Fatalf("committed history count=%d err=%v", count, err)
	}
}

func TestLegacyFallbackAndOutOfOrder(t *testing.T) {
	f := newTelemetryBrokerFixture(t)
	if err := f.db.Exec(`ALTER TABLE telemetry_datas ADD CONSTRAINT reject_bad CHECK ("key" <> 'bad')`).Error; err != nil {
		t.Fatal(err)
	}
	w := telemetryBrokerWriter(f)
	n1, n2, n3 := 1.0, 2.0, 3.0
	history := []TelemetryData{{DeviceID: "d", Key: "a", TS: 2000, NumberV: &n2}, {DeviceID: "d", Key: "b", TS: 1000, NumberV: &n3}, {DeviceID: "d", Key: "a", TS: 1000, NumberV: &n1}, {DeviceID: "d", Key: "bad", TS: 1000, NumberV: &n1}}
	current := []TelemetryCurrentData{{DeviceID: "d", Key: "a", TS: time.UnixMilli(2000), NumberV: &n2}, {DeviceID: "d", Key: "b", TS: time.UnixMilli(1000), NumberV: &n3}, {DeviceID: "d", Key: "bad", TS: time.UnixMilli(1000), NumberV: &n1}}
	written, failed, unknown := w.batchInsert(history, current)
	if written != 3 || failed != 1 || unknown != 0 {
		t.Fatalf("written=%d failed=%d unknown=%d", written, failed, unknown)
	}
	var a, b TelemetryCurrentData
	if err := f.db.Where("device_id = ? AND key = ?", "d", "a").Take(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Where("device_id = ? AND key = ?", "d", "b").Take(&b).Error; err != nil {
		t.Fatal(err)
	}
	if a.TS.UnixMilli() != 2000 || *a.NumberV != 2 || *b.NumberV != 3 {
		t.Fatalf("wrong current a=%+v b=%+v", a, b)
	}
	// A successful later batch containing an older point must not regress current.
	w.batchInsert(history[2:3], []TelemetryCurrentData{{DeviceID: "d", Key: "a", TS: time.UnixMilli(1000), NumberV: &n1}})
	if err := f.db.Where("device_id = ? AND key = ?", "d", "a").Take(&a).Error; err != nil {
		t.Fatal(err)
	}
	if a.TS.UnixMilli() != 2000 || *a.NumberV != 2 {
		t.Fatalf("latest regressed: %+v", a)
	}
}

func TestLegacyBatchChunkBoundary(t *testing.T) {
	f := newTelemetryBrokerFixture(t)
	pool := &commitReplyLossPool{DB: f.dbSQL}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	w := newTelemetryWriter(db, telemetryTestLogger{}, Config{TelemetryBatchSize: 32}, newMetricsCollector())
	points := make([]TelemetryData, 1001)
	for i := range points {
		v := float64(i)
		points[i] = TelemetryData{DeviceID: "chunk-device", Key: fmt.Sprintf("key-%04d", i), TS: 1000, NumberV: &v}
	}
	written, failed, unknown := w.batchInsert(points, nil)
	if written != len(points) || failed != 0 || unknown != 0 {
		t.Fatalf("written=%d failed=%d unknown=%d", written, failed, unknown)
	}
	if got := pool.begins.Load(); got != 1 {
		t.Fatalf("transaction begins=%d; unexpected fallback", got)
	}
	var count int64
	if err := f.db.Model(&TelemetryData{}).Where("device_id = ?", "chunk-device").Count(&count).Error; err != nil || count != int64(len(points)) {
		t.Fatalf("history count=%d err=%v", count, err)
	}
	if err := f.db.Model(&TelemetryCurrentData{}).Where("device_id = ?", "chunk-device").Count(&count).Error; err != nil || count != int64(len(points)) {
		t.Fatalf("current count=%d err=%v", count, err)
	}
}

func TestLegacyConcurrentSameTimestampAndReverseOrder(t *testing.T) {
	f := newTelemetryBrokerFixture(t)
	w := telemetryBrokerWriter(f)
	start := make(chan struct{})
	results := make(chan [3]int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v := float64(i + 1)
			keys := []string{"a", "b"}
			if i == 1 {
				keys[0], keys[1] = keys[1], keys[0]
			}
			points := []TelemetryData{{DeviceID: "race-device", Key: keys[0], TS: 1000, NumberV: &v}, {DeviceID: "race-device", Key: keys[1], TS: 1000, NumberV: &v}}
			<-start
			written, failed, unknown := w.batchInsert(points, nil)
			results <- [3]int{written, failed, unknown}
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	total := 0
	for result := range results {
		if result[1] != 0 || result[2] != 0 {
			t.Fatalf("failed or unknown result: %v", result)
		}
		total += result[0]
	}
	if total != 4 {
		t.Fatalf("processed history points=%d", total)
	}
	for _, key := range []string{"a", "b"} {
		var history TelemetryData
		var current TelemetryCurrentData
		if err := f.db.Where("device_id=? AND key=? AND ts=?", "race-device", key, 1000).Take(&history).Error; err != nil {
			t.Fatal(err)
		}
		if err := f.db.Where("device_id=? AND key=?", "race-device", key).Take(&current).Error; err != nil {
			t.Fatal(err)
		}
		if history.NumberV == nil || current.NumberV == nil || *history.NumberV != *current.NumberV {
			t.Fatalf("key=%s history=%v current=%v", key, history.NumberV, current.NumberV)
		}
	}
}

func TestLegacyConcurrentConflictWaitsForHistoryWinner(t *testing.T) {
	f := newTelemetryBrokerFixture(t)
	first := 11.0
	tx := f.db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	var holderPID int
	if err := tx.Raw("SELECT pg_backend_pid()").Scan(&holderPID).Error; err != nil {
		_ = tx.Rollback().Error
		t.Fatal(err)
	}
	if err := tx.Create(&TelemetryData{DeviceID: "held-device", Key: "x", TS: 1000, NumberV: &first}).Error; err != nil {
		_ = tx.Rollback().Error
		t.Fatal(err)
	}
	result := make(chan [3]int, 1)
	consumed := false
	defer func() {
		_ = tx.Rollback().Error
		if !consumed {
			select {
			case <-result:
			case <-time.After(10 * time.Second):
				t.Error("contending transaction did not exit after releasing lock")
			}
		}
	}()
	go func() {
		second := 22.0
		written, failed, unknown := telemetryBrokerWriter(f).batchInsert([]TelemetryData{{DeviceID: "held-device", Key: "x", TS: 1000, NumberV: &second}}, nil)
		result <- [3]int{written, failed, unknown}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int64
		err := f.admin.Raw(`SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock' AND ? = ANY(pg_blocking_pids(pid)) AND query LIKE '%telemetry_datas%'`, holderPID).Scan(&waiting).Error
		if err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("second transaction did not wait for the uncommitted history key")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-result:
		consumed = true
		if got != [3]int{1, 0, 0} {
			t.Fatalf("result=%v", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("contending transaction did not finish")
	}
	var current TelemetryCurrentData
	if err := f.db.Where("device_id=? AND key=?", "held-device", "x").Take(&current).Error; err != nil {
		t.Fatal(err)
	}
	if current.NumberV == nil || *current.NumberV != first {
		t.Fatalf("current=%v, expected first committed history value %v", current.NumberV, first)
	}
}

func TestLegacyFallbackSameKeyAndReplay(t *testing.T) {
	f := newTelemetryBrokerFixture(t)
	w := telemetryBrokerWriter(f)
	first, second, equal := 1.0, 2.0, 9.0
	history := []TelemetryData{{DeviceID: "d", Key: "x", TS: 1000, NumberV: &first}, {DeviceID: "d", Key: "x", TS: 2000, NumberV: &second}}
	current := []TelemetryCurrentData{{DeviceID: "d", Key: "x", TS: time.UnixMilli(2000), NumberV: &second}}
	if written, failed, unknown := w.fallbackInsert(history, current); written != 2 || failed != 0 || unknown != 0 {
		t.Fatalf("fallback written=%d failed=%d unknown=%d", written, failed, unknown)
	}
	if written, failed, unknown := w.batchInsert(history, current); written != 2 || failed != 0 || unknown != 0 {
		t.Fatalf("replay written=%d failed=%d unknown=%d", written, failed, unknown)
	}
	// Same timestamp: history and current must retain the first accepted value.
	replay := TelemetryData{DeviceID: "d", Key: "x", TS: 2000, NumberV: &equal}
	current[0].NumberV = &equal
	if written, failed, unknown := w.batchInsert([]TelemetryData{replay}, current); written != 1 || failed != 0 || unknown != 0 {
		t.Fatalf("equal replay written=%d failed=%d unknown=%d", written, failed, unknown)
	}
	var got TelemetryCurrentData
	if err := f.db.Where("device_id=? AND key=?", "d", "x").Take(&got).Error; err != nil {
		t.Fatal(err)
	}
	if *got.NumberV != second {
		t.Fatalf("equal timestamp current=%v", *got.NumberV)
	}
	var old TelemetryData
	if err := f.db.Where("device_id=? AND key=? AND ts=?", "d", "x", 2000).Take(&old).Error; err != nil {
		t.Fatal(err)
	}
	if *old.NumberV != second {
		t.Fatalf("history changed=%v", *old.NumberV)
	}
}
