package dbx

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"encore.app/pkg/sqldialect"
)

// The SQL wrapper is what MySQL uses; running it on PostgreSQL as well shows both behave the same through dbx.
func databases(t *testing.T) map[string]DB {
	t.Helper()
	pgDSN, myDSN := os.Getenv("TP_AUTO_SCHEMA_PG_DSN"), os.Getenv("TP_AUTO_SCHEMA_MYSQL_DSN")
	if pgDSN == "" || myDSN == "" {
		t.Skip("TP_AUTO_SCHEMA_PG_DSN and TP_AUTO_SCHEMA_MYSQL_DSN required")
	}
	config, err := pgx.ParseConfig(pgDSN)
	if err != nil {
		t.Fatal(err)
	}
	pg := stdlib.OpenDB(*config)
	my, err := sqldialect.OpenMySQL(myDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pg.Close(); my.Close() })
	table := "dbx_t_" + strconv.Itoa(os.Getpid())
	mustExec := func(db *sql.DB, q string) {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	mustExec(pg, `CREATE TABLE `+table+` (id VARCHAR(36) PRIMARY KEY, name VARCHAR(50) NOT NULL, doc JSONB NULL, CONSTRAINT `+table+`_name_key UNIQUE (name))`)
	mustExec(my, "CREATE TABLE `"+table+"` (id VARCHAR(36) PRIMARY KEY, name VARCHAR(50) NOT NULL, doc JSON NULL, CONSTRAINT `"+table+"_name_key` UNIQUE (name))")
	t.Cleanup(func() { _, _ = pg.Exec(`DROP TABLE ` + table); _, _ = my.Exec("DROP TABLE `" + table + "`") })
	t.Setenv("DBX_TABLE", table)
	return map[string]DB{"postgres": SQL(pg, "postgres"), "mysql": SQL(my, "mysql")}
}

func TestSQLWrapperBehavesTheSameOnPostgresAndMySQL(t *testing.T) {
	dbs := databases(t)
	table := os.Getenv("DBX_TABLE")
	ctx := context.Background()
	run := func(db DB) []string {
		var out []string
		note := func(f string, a ...any) { out = append(out, fmt.Sprintf(f, a...)) }
		res, err := db.Exec(ctx, `INSERT INTO `+table+` (id, name, doc) VALUES ($1,$2,$3)`, "a", "alpha", `{"k":1}`)
		note("insert: %v rows=%d", err == nil, res.RowsAffected())
		_, err = db.Exec(ctx, `INSERT INTO `+table+` (id, name) VALUES ($1,$2)`, "b", "alpha")
		note("duplicate name: unique=%v named=%v other=%v", IsUniqueViolation(err, ""), IsUniqueViolation(err, table+"_name_key"), IsUniqueViolation(err, "no_such_key"))
		var name string
		err = db.QueryRow(ctx, `SELECT name FROM `+table+` WHERE id=$1`, "missing").Scan(&name)
		note("missing row: noRows=%v", IsNoRows(err))
		err = db.QueryRow(ctx, `SELECT name FROM `+table+` WHERE id=$1`, "a").Scan(&name)
		note("found: %s %v", name, err)
		var doc []byte
		_ = db.QueryRow(ctx, `SELECT doc FROM `+table+` WHERE id=$1`, "a").Scan(&doc)
		note("json: %s", doc)
		res, _ = db.Exec(ctx, `UPDATE `+table+` SET name=$2 WHERE id=$1`, "a", "alpha")
		note("update to the same value reports matched rows: %d", res.RowsAffected())
		tx, err := db.Begin(ctx)
		note("begin: %v", err == nil)
		_, _ = tx.Exec(ctx, `INSERT INTO `+table+` (id, name) VALUES ($1,$2)`, "c", "gamma")
		note("rollback: %v", tx.Rollback() == nil)
		var n int
		_ = db.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n)
		note("rows after rollback: %d", n)
		tx, _ = db.Begin(ctx)
		_, _ = tx.Exec(ctx, `INSERT INTO `+table+` (id, name) VALUES ($1,$2)`, "d", "delta")
		note("commit: %v", tx.Commit() == nil)
		rows, err := db.Query(ctx, `SELECT id FROM `+table+` ORDER BY id`)
		var ids []string
		for err == nil && rows.Next() {
			var id string
			_ = rows.Scan(&id)
			ids = append(ids, id)
		}
		if rows != nil {
			note("rows: %v err=%v", ids, rows.Err())
			rows.Close()
		}
		_, err = db.Exec(ctx, `INSERT INTO `+table+` (id) VALUES ($1)`, "e")
		note("not-null violation is an error: %v unique=%v", err != nil, IsUniqueViolation(err, ""))
		return out
	}
	want, got := run(dbs["postgres"]), run(dbs["mysql"])
	if len(want) != len(got) {
		t.Fatalf("%d vs %d steps", len(want), len(got))
	}
	for i := range want {
		if want[i] != got[i] {
			t.Errorf("step %d\n pg: %s\n my: %s", i+1, want[i], got[i])
		}
	}
	t.Logf("%d steps compared", len(want))
}
