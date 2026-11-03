package mysqlschema

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"encore.app/pkg/dbx"
	"encore.app/pkg/sqldialect"
)

// Each guard must refuse the same change on both databases (PostgreSQL SQLSTATE 23514, MySQL error 3819) and allow
// the legitimate one. Needs the same two databases as TestSchemaMatchesPostgres, with the MySQL schema applied.
func TestGuardsRefuseTheSameChangesOnBothDatabases(t *testing.T) {
	pgDSN, myDSN := os.Getenv("TP_DM_PG_DSN"), os.Getenv("TP_DM_MYSQL_DSN")
	if pgDSN == "" || myDSN == "" {
		t.Skip("TP_DM_PG_DSN and TP_DM_MYSQL_DSN required")
	}
	config, err := pgx.ParseConfig(pgDSN)
	if err != nil {
		t.Fatal(err)
	}
	pgPool := stdlib.OpenDB(*config)
	myPool, err := sqldialect.OpenMySQL(myDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pgPool.Close(); myPool.Close() })
	ctx := context.Background()
	conn, err := myPool.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	_, _ = conn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 0")
	rows, _ := conn.QueryContext(ctx, "SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE()")
	var names []string
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		names = append(names, n)
	}
	rows.Close()
	for _, n := range names {
		_, _ = conn.ExecContext(ctx, "DROP TABLE IF EXISTS `"+n+"`")
	}
	_, _ = conn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 1")
	for i, statement := range Statements() {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			t.Fatalf("statement %d: %v\n%.200s", i+1, err, statement)
		}
	}

	const tenant = "11111111-1111-4111-8111-111111111111"
	const other = "22222222-2222-4222-8222-222222222222"
	const model = "33333333-3333-4333-8333-333333333333"
	const snapshot = "44444444-4444-4444-8444-444444444444"
	const actor = "55555555-5555-4555-8555-555555555555"
	run := func(db dbx.DB) []string {
		var out []string
		note := func(f string, a ...any) { out = append(out, fmt.Sprintf(f, a...)) }
		verdict := func(err error) string {
			switch {
			case err == nil:
				return "allowed"
			case strings.Contains(err.Error(), "23514") || strings.Contains(err.Error(), "3819"):
				return "refused (check violation)"
			case strings.Contains(err.Error(), "P0001") || strings.Contains(err.Error(), "1644"):
				return "refused (raised exception)"
			}
			return "other error: " + err.Error()
		}
		_, err := db.Exec(ctx, `INSERT INTO thing_models (id, tenant_id, name) VALUES ($1,$2,$3)`, model, tenant, "boiler")
		note("create thing model: %s", verdict(err))
		_, err = db.Exec(ctx, `UPDATE thing_models SET name=$2 WHERE id=$1`, model, "boiler v2")
		note("rename: %s", verdict(err))
		_, err = db.Exec(ctx, `UPDATE thing_models SET tenant_id=$2 WHERE id=$1`, model, other)
		note("move to another tenant: %s", verdict(err))
		_, err = db.Exec(ctx, `UPDATE thing_models SET tenant_id=$2 WHERE id=$1`, model, tenant)
		note("set the same tenant: %s", verdict(err))
		_, err = db.Exec(ctx, `INSERT INTO thing_model_snapshots (id, thing_model_id, tenant_id, version, content, published_by) VALUES ($1,$2,$3,1,$4,$5)`,
			snapshot, model, tenant, `{"a":1}`, actor)
		note("publish snapshot: %s", verdict(err))
		_, err = db.Exec(ctx, `UPDATE thing_model_snapshots SET content=$2 WHERE id=$1`, snapshot, `{"a":2}`)
		note("edit snapshot: %s", verdict(err))
		_, err = db.Exec(ctx, `DELETE FROM thing_model_snapshots WHERE id=$1`, snapshot)
		note("delete snapshot: %s", verdict(err))
		var before, after any
		_ = db.QueryRow(ctx, `SELECT updated_at FROM thing_models WHERE id=$1`, model).Scan(&before)
		_, _ = db.Exec(ctx, `UPDATE thing_models SET name=$2 WHERE id=$1`, model, "boiler v2")
		_ = db.QueryRow(ctx, `SELECT updated_at FROM thing_models WHERE id=$1`, model).Scan(&after)
		note("updated_at readable: %v", before != nil && after != nil)
		return out
	}
	// TRUNCATE skips the row triggers (the snapshot guard refuses DELETE), so the oracle can be reset.
	truncate := func() { _, _ = pgPool.Exec("TRUNCATE thing_model_snapshots, thing_models CASCADE") }
	truncate()
	t.Cleanup(truncate)
	want := run(dbx.SQL(pgPool, "postgres"))
	got := run(dbx.SQL(myPool, "mysql"))
	for i := range want {
		if want[i] != got[i] {
			t.Errorf("step %d\n pg: %s\n my: %s", i+1, want[i], got[i])
		}
	}
	t.Logf("%d guard steps compared", len(want))
}
