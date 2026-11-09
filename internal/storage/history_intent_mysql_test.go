package storage

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	componentsql "project/sql"
)

func openMySQLIngest(t *testing.T) *gorm.DB {
	t.Helper()
	host, port := os.Getenv("TP_MYSQL_TEST_HOST"), os.Getenv("TP_MYSQL_TEST_PORT")
	if host == "" || port == "" {
		t.Skip("TP_MYSQL_TEST_* required")
	}
	cfg := mysqldriver.NewConfig()
	cfg.User, cfg.Passwd, cfg.Net, cfg.Addr = os.Getenv("TP_MYSQL_TEST_USER"), os.Getenv("TP_MYSQL_TEST_PASSWORD"), "tcp", host+":"+port
	cfg.DBName, cfg.ParseTime, cfg.Loc, cfg.ClientFoundRows = os.Getenv("TP_MYSQL_TEST_DB"), true, time.UTC, true
	db, err := gorm.Open(mysql.Open(cfg.FormatDSN()), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	drop := func() {
		for _, table := range []string{"telemetry_history_deletions", "telemetry_history_intents", "telemetry_current_datas"} {
			_ = db.Exec("DROP TABLE IF EXISTS " + table).Error
		}
	}
	drop()
	for _, name := range componentsql.MySQLHistoryIntentMigrationNames() {
		statements, err := componentsql.MySQLMigrationStatements(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range statements {
			if err := db.Exec(s).Error; err != nil {
				t.Fatalf("apply %s: %v", name, err)
			}
		}
	}
	t.Cleanup(func() {
		drop()
		if p, e := db.DB(); e == nil {
			_ = p.Close()
		}
	})
	return db
}

func snapshotMySQLCurrent(t *testing.T, db *gorm.DB) []string {
	t.Helper()
	var rows []TelemetryCurrentData
	if err := db.Order("device_id, `key`").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, p := range rows {
		out = append(out, fmt.Sprintf("%s|%s|%d|%s|%s|%s|%s", p.DeviceID, p.Key, p.TS.UnixMilli(), ptr(p.BoolV), ptr(p.NumberV), ptr(p.StringV), p.TenantID))
	}
	sort.Strings(out)
	return out
}

// PostgreSQL's transactional path is the oracle; the subject is MySQL holding the intents and the
// snapshot while TDengine receives the history: the MySQL + TDengine profile with PostgreSQL absent.
func TestMySQLIntentPathMatchesPostgresLegacyAndFeedsTDengine(t *testing.T) {
	if os.Getenv("TP_TELEMETRY_BROKER_TEST_DSN") == "" || os.Getenv("TP_TDENGINE_TEST_URL") == "" {
		t.Skip("TP_TELEMETRY_BROKER_TEST_DSN and TP_TDENGINE_TEST_URL required")
	}
	for seed := int64(1); seed <= 4; seed++ {
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			legacy := newTelemetryBrokerFixture(t)
			my := openMySQLIngest(t)
			td := newTDengineSink(t)
			legacyStore, intentStore := TelemetryHistoryStore(postgresHistoryStore{}), NewIntentHistoryStore(0)
			for i, batch := range randomBatches(seed) {
				a := commit(t, legacy.db, legacyStore, batch)
				b := commit(t, my, intentStore, batch)
				if a != b {
					t.Fatalf("batch %d: PostgreSQL stored %d, MySQL stored %d", i, a, b)
				}
				if i%3 == 0 {
					if _, err := ReplayHistoryIntents(context.Background(), my, td, "w", 10, time.Minute); err != nil {
						t.Fatal(err)
					}
				}
				_, want := snapshotTelemetry(t, legacy.db)
				got := snapshotMySQLCurrent(t, my)
				if fmt.Sprint(want) != fmt.Sprint(got) {
					t.Fatalf("batch %d: snapshots differ\npg=%v\nmy=%v", i, want, got)
				}
			}
			for {
				n, err := ReplayHistoryIntents(context.Background(), my, td, "w", 50, time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				if n == 0 {
					break
				}
			}
			// The oracle's history table against what reached TDengine.
			var oracle []TelemetryData
			if err := legacy.db.Order("device_id, key, ts").Find(&oracle).Error; err != nil {
				t.Fatal(err)
			}
			rows := tdQuery(t, td, "SELECT device_id, k, CAST(ts AS BIGINT), number_v, bool_v, string_v FROM "+td.Database+".telemetry ORDER BY device_id, k, ts")
			if len(rows) != len(oracle) || len(oracle) == 0 {
				t.Fatalf("history rows: oracle=%d tdengine=%d", len(oracle), len(rows))
			}
			sort.Slice(oracle, func(i, j int) bool {
				a, b := oracle[i], oracle[j]
				if a.DeviceID != b.DeviceID {
					return a.DeviceID < b.DeviceID
				}
				if a.Key != b.Key {
					return a.Key < b.Key
				}
				return a.TS < b.TS
			})
			for i, o := range oracle {
				r := rows[i]
				if r[0] != o.DeviceID || r[1] != o.Key || int64(r[2].(float64)) != o.TS {
					t.Fatalf("row %d identity differs: oracle=%v|%v|%v td=%v", i, o.DeviceID, o.Key, o.TS, r)
				}
				if (o.NumberV == nil) != (r[3] == nil) || (o.NumberV != nil && *o.NumberV != r[3].(float64)) ||
					(o.BoolV == nil) != (r[4] == nil) || (o.BoolV != nil && *o.BoolV != r[4].(bool)) ||
					(o.StringV == nil) != (r[5] == nil) || (o.StringV != nil && *o.StringV != r[5].(string)) {
					t.Fatalf("row %d values differ: oracle=%s|%s|%s td=%v", i, ptr(o.NumberV), ptr(o.BoolV), ptr(o.StringV), r[3:])
				}
			}
		})
	}
}

func TestMySQLIntentsAreIdempotentAndValidated(t *testing.T) {
	my := openMySQLIngest(t)
	points := intentPoints(30)
	tx := my.Begin()
	n, err := EnqueueHistoryIntents(tx, 0, points)
	if err != nil || n != 30 {
		t.Fatalf("first enqueue n=%d err=%v", n, err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	tx = my.Begin()
	if n, err = EnqueueHistoryIntents(tx, 0, points); err != nil || n != 0 {
		t.Fatalf("re-enqueue must report no new intent even on a clientFoundRows connection, n=%d err=%v", n, err)
	}
	if n, err = EnqueueHistoryIntents(tx, 1, points[:4]); err != nil || n != 4 {
		t.Fatalf("a new generation is a distinct identity, n=%d err=%v", n, err)
	}
	_ = tx.Rollback()
	var pending int
	my.Raw("SELECT COUNT(*) FROM telemetry_history_intents").Scan(&pending)
	if pending != 30 {
		t.Fatalf("a rolled-back enqueue must leave nothing behind, have %d", pending)
	}
	long := TelemetryData{DeviceID: strings.Repeat("d", 65), Key: "k", TS: 1, TenantID: "t"}
	if _, err := EnqueueHistoryIntents(my.Begin(), 0, []TelemetryData{long}); err == nil {
		t.Fatal("an over-long device id must be refused, not truncated by INSERT IGNORE")
	}
	if _, err := EnqueueHistoryIntents(my.Begin(), 0, []TelemetryData{{DeviceID: "d", Key: "k", TS: 1}}); err == nil {
		t.Fatal("a missing tenant must be refused")
	}
	// Replay semantics on MySQL: lease, failure, retry, deletion supersedes queued intents.
	failing := newDedupSink()
	failing.failNow = fmt.Errorf("backend down")
	if _, err := ReplayHistoryIntents(context.Background(), my, failing, "w1", 100, time.Minute); err == nil {
		t.Fatal("a sink failure must surface")
	}
	if n, _ := ReplayHistoryIntents(context.Background(), my, newDedupSink(), "w2", 100, time.Minute); n != 0 {
		t.Fatalf("a live lease must not be stolen, got %d", n)
	}
	if err := my.Exec("UPDATE telemetry_history_intents SET claimed_until = NOW(6) - INTERVAL 1 SECOND").Error; err != nil {
		t.Fatal(err)
	}
	ok := newDedupSink()
	if n, err := ReplayHistoryIntents(context.Background(), my, ok, "w2", 100, time.Minute); err != nil || n != 30 || len(ok.stored) != 30 {
		t.Fatalf("after lease expiry n=%d stored=%d err=%v", n, len(ok.stored), err)
	}
	seedMy := func(device string) {
		v := 1.0
		tx := my.Begin()
		if _, err := EnqueueHistoryIntents(tx, 5, []TelemetryData{{DeviceID: device, Key: "k", TS: 9, NumberV: &v, TenantID: "t"}}); err != nil {
			t.Fatal(err)
		}
		_ = tx.Commit()
	}
	seedMy("gone")
	if err := EnqueueHistoryDeviceDeletion(my, "gone"); err != nil {
		t.Fatal(err)
	}
	var queued int
	my.Raw("SELECT COUNT(*) FROM telemetry_history_intents WHERE device_id='gone' AND state='pending'").Scan(&queued)
	if queued != 0 {
		t.Fatalf("a device deletion must cancel queued intents, %d remain", queued)
	}
	deleter := &recordingDeleter{}
	if n, err := ReplayHistoryDeletions(context.Background(), my, deleter, "d1", 10, time.Minute); err != nil || n != 1 || fmt.Sprint(deleter.deleted) != "[gone]" {
		t.Fatalf("deletion replay n=%d err=%v deleted=%v", n, err, deleter.deleted)
	}
	_ = url.QueryEscape
}

func TestHistoryDeletionClaimSkipsLockedRowsOnBothDialects(t *testing.T) {
	for _, scenario := range []struct {
		name string
		open func(*testing.T) *gorm.DB
	}{
		{name: "postgres", open: newHistoryIntentDB},
		{name: "mysql", open: openMySQLIngest},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			db := scenario.open(t)
			if err := EnqueueHistoryDeviceDeletion(db, "locked-device"); err != nil {
				t.Fatal(err)
			}
			lock := db.Begin()
			if lock.Error != nil {
				t.Fatal(lock.Error)
			}
			var id int64
			if err := lock.Raw("SELECT id FROM telemetry_history_deletions WHERE device_id = ? FOR UPDATE", "locked-device").Scan(&id).Error; err != nil {
				_ = lock.Rollback().Error
				t.Fatal(err)
			}
			result := make(chan struct {
				n   int
				err error
			}, 1)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			go func() {
				n, err := ReplayHistoryDeletions(ctx, db, &recordingDeleter{}, "worker-1", 10, time.Minute)
				result <- struct {
					n   int
					err error
				}{n: n, err: err}
			}()
			select {
			case got := <-result:
				if got.err != nil || got.n != 0 {
					_ = lock.Rollback().Error
					t.Fatalf("claim should skip the locked row, got n=%d err=%v", got.n, got.err)
				}
			case <-time.After(3 * time.Second):
				_ = lock.Rollback().Error
				t.Fatal("claim waited on a locked deletion instead of skipping it")
			}
			if err := lock.Rollback().Error; err != nil {
				t.Fatal(err)
			}
			deleter := &recordingDeleter{}
			if n, err := ReplayHistoryDeletions(context.Background(), db, deleter, "worker-2", 10, time.Minute); err != nil || n != 1 || fmt.Sprint(deleter.deleted) != "[locked-device]" {
				t.Fatalf("unlocked deletion should complete, got n=%d err=%v deleted=%v", n, err, deleter.deleted)
			}
		})
	}
}
