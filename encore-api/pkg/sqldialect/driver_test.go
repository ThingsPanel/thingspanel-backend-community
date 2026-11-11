package sqldialect

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestOpenMySQLRefusesDSNsWithoutTheRequiredOptions(t *testing.T) {
	for _, dsn := range []string{"u:p@tcp(h)/db", "u:p@tcp(h)/db?parseTime=true", "u:p@tcp(h)/db?clientFoundRows=true", "not a dsn"} {
		if db, err := OpenMySQL(dsn); err == nil {
			db.Close()
			t.Errorf("must refuse %q", dsn)
		} else if strings.Contains(err.Error(), "u:p") {
			t.Errorf("the error leaks the credentials: %v", err)
		}
	}
}

func TestTranslatingDriverRunsPostgresStyleStatementsOnMySQL(t *testing.T) {
	dsn := os.Getenv("TP_DIALECT_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TP_DIALECT_MYSQL_DSN required")
	}
	db, err := OpenMySQL(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	_, _ = db.Exec("DROP TABLE IF EXISTS dialect_driver_probe")
	if _, err := db.Exec("CREATE TABLE dialect_driver_probe (id VARCHAR(20) PRIMARY KEY, note VARCHAR(40), doc JSON, blob_v LONGBLOB, n INT)"); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP TABLE IF EXISTS dialect_driver_probe")

	// $n reuse and reorder, ON CONFLICT DO UPDATE, parameter casts.
	for i := 0; i < 2; i++ {
		if _, err := db.ExecContext(ctx, `INSERT INTO dialect_driver_probe (id, note, n) VALUES ($1, $2, $3) ON CONFLICT (id) DO UPDATE SET note = EXCLUDED.note, n = EXCLUDED.n`, "a", "run"+string(rune('0'+i)), i+10); err != nil {
			t.Fatalf("upsert %d: %v", i, err)
		}
	}
	var note string
	var n int
	if err := db.QueryRowContext(ctx, `SELECT note, n FROM dialect_driver_probe WHERE id = $1 AND ($2::text IS NULL OR note = $2) AND n = $3::int`, "a", "run1", 11).Scan(&note, &n); err != nil || note != "run1" || n != 11 {
		t.Fatalf("reused placeholders: note=%q n=%d err=%v", note, n, err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dialect_driver_probe WHERE ($1::text IS NULL OR note = $1)`, nil).Scan(&count); err != nil || count != 1 {
		t.Fatalf("a NULL filter must match everything: count=%d err=%v", count, err)
	}

	// Valid UTF-8 byte slices reach a JSON column; binary data survives untouched.
	raw := []byte(`{"b": 1, "a": "中文"}`)
	binary := []byte{0xff, 0x00, 0xfe, 0x80, 0x01}
	if _, err := db.ExecContext(ctx, `INSERT INTO dialect_driver_probe (id, doc, blob_v) VALUES ($1, $2, $3)`, "j", raw, binary); err != nil {
		t.Fatalf("json bytes: %v", err)
	}
	var doc string
	var back []byte
	if err := db.QueryRowContext(ctx, `SELECT CAST(doc AS CHAR), blob_v FROM dialect_driver_probe WHERE id = $1`, "j").Scan(&doc, &back); err != nil || !strings.Contains(doc, "中文") || !bytes.Equal(back, binary) {
		t.Fatalf("round trip: doc=%q blob=%v err=%v", doc, back, err)
	}
	var name string
	if err := db.QueryRowContext(ctx, `SELECT doc->>'a' FROM dialect_driver_probe WHERE id = $1`, "j").Scan(&name); err != nil || name != "中文" {
		t.Fatalf("JSON extraction: %q %v", name, err)
	}

	// Transactions and prepared statements with reused placeholders.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.PrepareContext(ctx, `UPDATE dialect_driver_probe SET note = $2, n = n + 1 WHERE id = $1 AND note <> $2`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		res, err := stmt.ExecContext(ctx, "a", "prepared")
		if err != nil {
			t.Fatalf("prepared exec %d: %v", i, err)
		}
		affected, _ := res.RowsAffected()
		if (i == 0 && affected != 1) || (i == 1 && affected != 0) {
			t.Fatalf("prepared exec %d affected %d", i, affected)
		}
	}
	_ = stmt.Close()
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT note FROM dialect_driver_probe WHERE id = $1`, "a").Scan(&note); err != nil || note != "run1" {
		t.Fatalf("a rolled-back update must leave the row alone: %q %v", note, err)
	}

	// UPDATE reports matched rows (clientFoundRows), as PostgreSQL does.
	res, err := db.ExecContext(ctx, `UPDATE dialect_driver_probe SET note = $1 WHERE id = $2`, "run1", "a")
	if err != nil {
		t.Fatal(err)
	}
	if affected, _ := res.RowsAffected(); affected != 1 {
		t.Fatalf("an UPDATE that changes nothing must still report the matched row, got %d", affected)
	}

	// Statements the translator refuses never reach the server.
	before := Unsupported.Load()
	for _, bad := range []string{`DELETE FROM dialect_driver_probe WHERE id = $1 RETURNING id`, `SELECT pg_advisory_xact_lock(1)`, `SELECT id FROM dialect_driver_probe WHERE id = ANY($1)`} {
		if _, err := db.ExecContext(ctx, bad, "a"); !errors.Is(err, ErrUntranslatable) {
			t.Errorf("%q must be refused, got %v", bad, err)
		}
	}
	if Unsupported.Load()-before != 3 {
		t.Fatal("refused statements must be counted")
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dialect_driver_probe`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("a refused DELETE must not have run: count=%d err=%v", count, err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SET @x := $1`, 1); err != nil {
		t.Fatalf("a pinned connection translates too: %v", err)
	}
}
