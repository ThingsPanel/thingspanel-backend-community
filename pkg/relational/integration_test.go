package relational

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
)

// These tests need real servers: eval "$(scripts/dev-databases.sh env)" after `scripts/dev-databases.sh up`.
// They are skipped, with the reason, when the variables are not set.

func integrationTargets(t *testing.T) map[Dialect]Target {
	t.Helper()
	out := map[Dialect]Target{}
	if dsn := os.Getenv("TEST_PG_SCHEMA_DSN"); dsn != "" {
		out[Postgres] = Target{Name: "it_pg", Dialect: Postgres, DSN: dsn}
	}
	if host := os.Getenv("TP_MYSQL_TEST_HOST"); host != "" {
		port, _ := strconv.Atoi(os.Getenv("TP_MYSQL_TEST_PORT"))
		out[MySQL] = Target{Name: "it_my", Dialect: MySQL, Host: host, Port: port, Database: os.Getenv("TP_MYSQL_TEST_DB"),
			Username: os.Getenv("TP_MYSQL_TEST_USER"), Password: os.Getenv("TP_MYSQL_TEST_PASSWORD"), TimeZone: "UTC"}
	}
	if len(out) == 0 {
		t.Skip("TEST_PG_SCHEMA_DSN / TP_MYSQL_TEST_* not set; start the databases with scripts/dev-databases.sh up")
	}
	return out
}

func TestIntegrationClassifyOnRealServers(t *testing.T) {
	for d, target := range integrationTargets(t) {
		d, target := d, target
		t.Run(string(d), func(t *testing.T) {
			ctx := context.Background()
			db, err := Open(ctx, target)
			if err != nil {
				t.Fatal(err)
			}
			defer CloseDB(db)
			if got, _ := DialectOf(db); got != d {
				t.Fatalf("DialectOf = %q", got)
			}

			table := "relational_it_" + strconv.FormatInt(time.Now().UnixNano(), 36)
			must := func(sql string, args ...any) {
				t.Helper()
				if err := db.Exec(sql, args...).Error; err != nil {
					t.Fatalf("%s: %v", sql, err)
				}
			}
			must("CREATE TABLE " + table + " (id INT PRIMARY KEY, n INT NOT NULL, UNIQUE (n))")
			defer db.Exec("DROP TABLE " + table)
			must("INSERT INTO "+table+" (id, n) VALUES (?, ?), (?, ?)", 1, 10, 2, 20)

			// unique
			err = db.Exec("INSERT INTO "+table+" (id, n) VALUES (?, ?)", 3, 10).Error
			if !Is(err, KindUnique) || !IsUnique(err, "") {
				t.Errorf("duplicate: %v classified %v", err, Classify(err))
			}
			// not found
			var n int
			err = db.Raw("SELECT n FROM "+table+" WHERE id = ?", 99).Take(&n).Error
			if !Is(err, KindNotFound) {
				t.Errorf("missing row: %v classified %v", err, Classify(err))
			}
			// RowsAffected counts matched rows, not changed rows (clientFoundRows on MySQL)
			res := db.Exec("UPDATE "+table+" SET n = n WHERE id = ?", 1)
			if res.Error != nil || res.RowsAffected != 1 {
				t.Errorf("idempotent UPDATE affected %d rows (err %v); want 1", res.RowsAffected, res.Error)
			}
			// timeout
			tctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
			sleep := "SELECT pg_sleep(5)"
			if d == MySQL {
				sleep = "SELECT SLEEP(5)"
			}
			err = db.WithContext(tctx).Exec(sleep).Error
			cancel()
			if !Is(err, KindTimeout) {
				t.Errorf("sleep under a 300ms deadline: %v classified %v", err, Classify(err))
			}
			// deadlock: two transactions lock rows 1 and 2 in opposite order
			var wg sync.WaitGroup
			errs := make([]error, 2)
			ready := make(chan struct{}, 2)
			barrier := make(chan struct{})
			for i := 0; i < 2; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					first, second := 1+i, 2-i
					errs[i] = db.Transaction(func(tx *gorm.DB) error {
						if err := tx.Exec("UPDATE "+table+" SET n = n WHERE id = ?", first).Error; err != nil {
							return err
						}
						ready <- struct{}{}
						<-barrier
						return tx.Exec("UPDATE "+table+" SET n = n WHERE id = ?", second).Error
					})
				}(i)
			}
			<-ready
			<-ready
			close(barrier)
			wg.Wait()
			deadlocks := 0
			for _, e := range errs {
				if e != nil && !Is(e, KindDeadlock) {
					t.Errorf("unexpected error %v classified %v", e, Classify(e))
				}
				if Is(e, KindDeadlock) {
					deadlocks++
				}
			}
			if deadlocks != 1 {
				t.Errorf("want exactly one deadlock victim, got %d (%v)", deadlocks, fmt.Sprint(errs))
			}
		})
	}
}
