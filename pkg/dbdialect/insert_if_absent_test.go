package dbdialect

import (
	"os"
	"sync"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type probe struct {
	ID    string `gorm:"column:id;primaryKey"`
	Other string `gorm:"column:other;uniqueIndex"`
}

func (probe) TableName() string { return "dbdialect_probe" }

func open(t *testing.T) []*gorm.DB {
	t.Helper()
	pgDSN := os.Getenv("TEST_DBDIALECT_PG_DSN")
	host, port := os.Getenv("TP_MYSQL_TEST_HOST"), os.Getenv("TP_MYSQL_TEST_PORT")
	if pgDSN == "" || host == "" {
		t.Skip("TEST_DBDIALECT_PG_DSN and TP_MYSQL_TEST_* required")
	}
	pg, err := gorm.Open(postgres.Open(pgDSN), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	cfg := mysqldriver.NewConfig()
	cfg.User, cfg.Passwd, cfg.Net, cfg.Addr = os.Getenv("TP_MYSQL_TEST_USER"), os.Getenv("TP_MYSQL_TEST_PASSWORD"), "tcp", host+":"+port
	cfg.DBName, cfg.ParseTime, cfg.Loc = os.Getenv("TP_MYSQL_TEST_DB"), true, time.UTC
	cfg.ClientFoundRows = true // the setting the account queries require; this is what breaks affected-row checks
	my, err := gorm.Open(mysql.Open(cfg.FormatDSN()), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, db := range []*gorm.DB{pg, my} {
		_ = db.Exec("DROP TABLE IF EXISTS dbdialect_probe").Error
		if err := db.Exec("CREATE TABLE dbdialect_probe (id VARCHAR(20) PRIMARY KEY, other VARCHAR(20) UNIQUE)").Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, db := range []*gorm.DB{pg, my} {
			_ = db.Exec("DROP TABLE IF EXISTS dbdialect_probe").Error
			if p, e := db.DB(); e == nil {
				_ = p.Close()
			}
		}
	})
	return []*gorm.DB{pg, my}
}

func TestInsertIfAbsentReportsExistingRowsTheSameOnBothDatabases(t *testing.T) {
	for _, db := range open(t) {
		name := db.Dialector.Name()
		t.Run(name, func(t *testing.T) {
			inserted, err := InsertIfAbsent(db, &probe{ID: "a", Other: "x"}, "id")
			if err != nil || !inserted {
				t.Fatalf("first insert: inserted=%v err=%v", inserted, err)
			}
			inserted, err = InsertIfAbsent(db, &probe{ID: "a", Other: "y"}, "id")
			if err != nil || inserted {
				t.Fatalf("duplicate primary key must be reported as not inserted: inserted=%v err=%v", inserted, err)
			}
			if name == "mysql" {
				// A different unique key is also "absent-not": MySQL cannot tell which key collided; PostgreSQL's
				// target (id) lets this one through as an error. Callers name the key they care about.
				if _, err := InsertIfAbsent(db, &probe{ID: "b", Other: "x"}); err != nil {
					t.Fatalf("MySQL reports any unique collision as not inserted: %v", err)
				}
			}
			var count int64
			db.Model(&probe{}).Count(&count)
			if count != 1 {
				t.Fatalf("expected one row, got %d", count)
			}
			// Inside a transaction a duplicate must not poison later statements.
			tx := db.Begin()
			if inserted, err := InsertIfAbsent(tx, &probe{ID: "a"}, "id"); err != nil || inserted {
				t.Fatalf("duplicate in a transaction: inserted=%v err=%v", inserted, err)
			}
			if inserted, err := InsertIfAbsent(tx, &probe{ID: "c", Other: "z"}, "id"); err != nil || !inserted {
				t.Fatalf("insert after a duplicate in the same transaction: inserted=%v err=%v", inserted, err)
			}
			if err := tx.Commit().Error; err != nil {
				t.Fatal(err)
			}
			// Concurrent callers: exactly one wins.
			var wg sync.WaitGroup
			wins := make(chan bool, 8)
			for i := 0; i < cap(wins); i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					ok, err := InsertIfAbsent(db, &probe{ID: "race", Other: "r"}, "id")
					if err != nil {
						t.Error(err)
					}
					wins <- ok
				}()
			}
			wg.Wait()
			close(wins)
			won := 0
			for ok := range wins {
				if ok {
					won++
				}
			}
			if won != 1 {
				t.Fatalf("exactly one concurrent insert must win, %d did", won)
			}
		})
	}
}
