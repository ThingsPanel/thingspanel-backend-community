package relational_test

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"
	"time"

	"gorm.io/gorm"

	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational"
	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational/reltest"
)

func TestSplitStatements(t *testing.T) {
	got := relational.SplitStatements("-- header\nCREATE TABLE a (id INT);\n-- @@\n\n-- only a comment\n-- @@\nCREATE TRIGGER t BEFORE INSERT ON a FOR EACH ROW BEGIN\n  SET NEW.id = 1;\nEND\n-- @@\n")
	if len(got) != 2 || got[0] != "-- header\nCREATE TABLE a (id INT);" || got[1][:14] != "CREATE TRIGGER" {
		t.Fatalf("got %q", got)
	}
}

func TestLoadMigrationsRejectsBadNamesAndDuplicates(t *testing.T) {
	ok := fstest.MapFS{
		"m/0002_b.up.sql": {Data: []byte("SELECT 2")},
		"m/0001_a.up.sql": {Data: []byte("SELECT 1")},
		"m/PG-MAP.txt":    {Data: []byte("ignored")},
	}
	got, err := relational.LoadMigrations(ok, "m")
	if err != nil || len(got) != 2 || got[0].Version != 1 || got[1].Version != 2 || len(got[0].SHA256) != 64 {
		t.Fatalf("got %+v err %v", got, err)
	}
	for name, fsys := range map[string]fstest.MapFS{
		"misnamed":  {"m/create.sql": {Data: []byte("SELECT 1")}},
		"duplicate": {"m/0001_a.up.sql": {Data: []byte("SELECT 1")}, "m/01_b.up.sql": {Data: []byte("SELECT 1")}},
		"empty":     {"m/0001_a.up.sql": {Data: []byte("-- nothing\n")}},
	} {
		if _, err := relational.LoadMigrations(fsys, "m"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func fsOf(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for name, body := range files {
		m["m/"+name] = &fstest.MapFile{Data: []byte(body)}
	}
	return m
}

func TestMigrateRunsOnceAndIsIdempotent(t *testing.T) {
	db := reltest.NewEnv(t, relational.MySQL).DB[relational.MySQL]
	ctx := context.Background()
	files := fsOf(map[string]string{
		"0001_a.up.sql": "CREATE TABLE a (id INT PRIMARY KEY)\n-- @@\nINSERT INTO a VALUES (1)",
		"0002_b.up.sql": "CREATE TABLE b (id INT PRIMARY KEY)",
	})
	if err := relational.Verify(ctx, db, "svc", files, "m"); !errors.Is(err, relational.ErrBehind) {
		t.Fatalf("Verify before Migrate = %v, want ErrBehind (no ledger table yet)", err)
	}
	for i := 0; i < 2; i++ { // the second run must not re-execute (INSERT would fail on the duplicate key)
		if err := relational.Migrate(ctx, db, "svc", files, "m"); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}
	var n int64
	db.Raw("SELECT COUNT(*) FROM tp_schema_migrations WHERE resource='svc'").Scan(&n)
	if n != 2 {
		t.Fatalf("ledger rows = %d", n)
	}
	if err := relational.Verify(ctx, db, "svc", files, "m"); err != nil {
		t.Fatalf("Verify after Migrate: %v", err)
	}
}

func TestMigrateRefusesEditedRegisteredFile(t *testing.T) {
	db := reltest.NewEnv(t, relational.MySQL).DB[relational.MySQL]
	ctx := context.Background()
	if err := relational.Migrate(ctx, db, "svc", fsOf(map[string]string{"0001_a.up.sql": "CREATE TABLE a (id INT)"}), "m"); err != nil {
		t.Fatal(err)
	}
	edited := fsOf(map[string]string{"0001_a.up.sql": "CREATE TABLE a (id INT, extra INT)"})
	if err := relational.Migrate(ctx, db, "svc", edited, "m"); !errors.Is(err, relational.ErrChecksumMismatch) {
		t.Fatalf("Migrate = %v, want ErrChecksumMismatch", err)
	}
	if err := relational.Verify(ctx, db, "svc", edited, "m"); !errors.Is(err, relational.ErrChecksumMismatch) {
		t.Fatalf("Verify = %v, want ErrChecksumMismatch", err)
	}
	var cols int64
	db.Raw("SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name='a'").Scan(&cols)
	if cols != 1 {
		t.Fatalf("the edited file was executed anyway (%d columns)", cols)
	}
}

func TestVerifyFailsWhenBehind(t *testing.T) {
	db := reltest.NewEnv(t, relational.MySQL).DB[relational.MySQL]
	ctx := context.Background()
	v1 := fsOf(map[string]string{"0001_a.up.sql": "CREATE TABLE a (id INT)"})
	if err := relational.Migrate(ctx, db, "svc", v1, "m"); err != nil {
		t.Fatal(err)
	}
	v2 := fsOf(map[string]string{"0001_a.up.sql": "CREATE TABLE a (id INT)", "0002_b.up.sql": "CREATE TABLE b (id INT)"})
	if err := relational.Verify(ctx, db, "svc", v2, "m"); !errors.Is(err, relational.ErrBehind) {
		t.Fatalf("Verify = %v, want ErrBehind", err)
	}
	var n int64
	db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name='b'").Scan(&n)
	if n != 0 {
		t.Fatal("Verify must be read-only but table b exists")
	}
	// another resource in the same database has its own ledger rows
	if err := relational.Verify(ctx, db, "other", v1, "m"); !errors.Is(err, relational.ErrBehind) {
		t.Fatalf("Verify of an unmigrated resource = %v, want ErrBehind", err)
	}
}

func TestMigrateOutOfOrderFileIsRefused(t *testing.T) {
	db := reltest.NewEnv(t, relational.MySQL).DB[relational.MySQL]
	ctx := context.Background()
	if err := relational.Migrate(ctx, db, "svc", fsOf(map[string]string{"0002_b.up.sql": "CREATE TABLE b (id INT)"}), "m"); err != nil {
		t.Fatal(err)
	}
	late := fsOf(map[string]string{"0001_a.up.sql": "CREATE TABLE a (id INT)", "0002_b.up.sql": "CREATE TABLE b (id INT)"})
	if err := relational.Migrate(ctx, db, "svc", late, "m"); !errors.Is(err, relational.ErrBehind) {
		t.Fatalf("Migrate = %v, want ErrBehind", err)
	}
}

func TestMigrateOnPostgresIsRefused(t *testing.T) {
	db := reltest.NewEnv(t, relational.Postgres).DB[relational.Postgres]
	err := relational.Migrate(context.Background(), db, "svc", fsOf(map[string]string{"0001_a.up.sql": "SELECT 1"}), "m")
	if !errors.Is(err, relational.ErrUnsupportedDialect) {
		t.Fatalf("got %v", err)
	}
}

// Two executors on the same database: the one that holds the lock runs, the other gets ErrBusy at once.
func TestTwoConcurrentExecutorsOnlyOneRuns(t *testing.T) {
	db := reltest.NewEnv(t, relational.MySQL).DB[relational.MySQL]
	ctx := context.Background()
	slow := fsOf(map[string]string{"0001_slow.up.sql": "SELECT SLEEP(3)\n-- @@\nCREATE TABLE slow_ran (id INT)"})
	first := make(chan error, 1)
	go func() { first <- relational.Migrate(ctx, db, "svc", slow, "m") }()

	// wait until the first executor holds the lock
	var holder int64
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		db.Raw("SELECT COALESCE(IS_USED_LOCK(?),0)", relational.SchemaLockName(currentDatabase(t, db))).Scan(&holder)
		if holder != 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if holder == 0 {
		t.Fatal("the first executor never took the lock")
	}
	err := relational.Migrate(ctx, db, "svc", slow, "m")
	if !errors.Is(err, relational.ErrBusy) {
		t.Fatalf("second executor = %v, want ErrBusy", err)
	}
	if err := <-first; err != nil {
		t.Fatalf("first executor: %v", err)
	}
	var ran int64
	db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name='slow_ran'").Scan(&ran)
	var rows int64
	db.Raw("SELECT COUNT(*) FROM tp_schema_migrations WHERE resource='svc'").Scan(&rows)
	if ran != 1 || rows != 1 {
		t.Fatalf("slow_ran=%d ledger rows=%d, want 1 and 1", ran, rows)
	}
	// and once the lock is free the second executor sees nothing to do
	if err := relational.Migrate(ctx, db, "svc", slow, "m"); err != nil {
		t.Fatalf("after release: %v", err)
	}
}

func currentDatabase(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var name string
	if err := db.Raw("SELECT DATABASE()").Scan(&name).Error; err != nil || name == "" {
		t.Fatalf("SELECT DATABASE(): %q %v", name, err)
	}
	return name
}
