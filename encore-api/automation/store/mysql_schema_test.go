package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type column struct {
	typeClass string
	nullable  bool
}

type indexInfo struct {
	unique  bool
	columns string
}

type shape struct {
	columns map[string]column    // table.column
	indexes map[string]indexInfo // table.index
	fks     map[string]bool      // table.column->reftable.refcolumn:rule
}

func typeClass(t string) string {
	t = strings.ToLower(t)
	switch {
	case t == "text" || t == "character varying" || t == "varchar" || t == "longtext" || t == "mediumtext":
		return "text"
	case t == "integer" || t == "int":
		return "int"
	case t == "bigint":
		return "bigint"
	case t == "boolean" || t == "tinyint":
		return "bool"
	case t == "timestamp with time zone" || t == "datetime":
		return "time"
	case t == "jsonb" || t == "json":
		return "json"
	case t == "uuid" || t == "char":
		return "uuid"
	}
	return t
}

func pgShape(t *testing.T, db *sql.DB, tables []string) shape {
	t.Helper()
	s := shape{columns: map[string]column{}, indexes: map[string]indexInfo{}, fks: map[string]bool{}}
	rows, err := db.Query(`SELECT table_name, column_name, data_type, is_nullable FROM information_schema.columns WHERE table_schema = current_schema()`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var table, name, typ, nullable string
		if err := rows.Scan(&table, &name, &typ, &nullable); err != nil {
			t.Fatal(err)
		}
		s.columns[table+"."+name] = column{typeClass(typ), nullable == "YES"}
	}
	rows.Close()
	rows, err = db.Query(`SELECT t.relname, i.relname, ix.indisunique, ix.indisprimary,
		(SELECT string_agg(a.attname, ',' ORDER BY k.ord) FROM unnest(ix.indkey) WITH ORDINALITY AS k(attnum, ord) JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum)
		FROM pg_index ix JOIN pg_class t ON t.oid = ix.indrelid JOIN pg_class i ON i.oid = ix.indexrelid JOIN pg_namespace n ON n.oid = t.relnamespace WHERE n.nspname = current_schema()`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var table, name, cols string
		var unique, primary bool
		if err := rows.Scan(&table, &name, &unique, &primary, &cols); err != nil {
			t.Fatal(err)
		}
		if primary {
			name = "PRIMARY"
		}
		s.indexes[table+"."+name] = indexInfo{unique, cols}
	}
	rows.Close()
	rows, err = db.Query(`SELECT cl.relname, att.attname, rcl.relname, ratt.attname, c.confdeltype
		FROM pg_constraint c JOIN pg_class cl ON cl.oid = c.conrelid JOIN pg_namespace n ON n.oid = cl.relnamespace
		JOIN pg_class rcl ON rcl.oid = c.confrelid
		JOIN pg_attribute att ON att.attrelid = c.conrelid AND att.attnum = c.conkey[1]
		JOIN pg_attribute ratt ON ratt.attrelid = c.confrelid AND ratt.attnum = c.confkey[1]
		WHERE c.contype = 'f' AND n.nspname = current_schema()`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var table, col, ref, refcol, rule string
		if err := rows.Scan(&table, &col, &ref, &refcol, &rule); err != nil {
			t.Fatal(err)
		}
		cascade := map[string]string{"c": "CASCADE", "a": "NO ACTION", "r": "RESTRICT"}[rule]
		s.fks[fmt.Sprintf("%s.%s->%s.%s:%s", table, col, ref, refcol, cascade)] = true
	}
	rows.Close()
	return s
}

func mysqlShape(t *testing.T, db *sql.DB) shape {
	t.Helper()
	s := shape{columns: map[string]column{}, indexes: map[string]indexInfo{}, fks: map[string]bool{}}
	rows, err := db.Query(`SELECT table_name, column_name, data_type, is_nullable FROM information_schema.columns WHERE table_schema = DATABASE()`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var table, name, typ, nullable string
		if err := rows.Scan(&table, &name, &typ, &nullable); err != nil {
			t.Fatal(err)
		}
		s.columns[table+"."+name] = column{typeClass(typ), nullable == "YES"}
	}
	rows.Close()
	rows, err = db.Query(`SELECT table_name, index_name, non_unique, GROUP_CONCAT(column_name ORDER BY seq_in_index SEPARATOR ',') FROM information_schema.statistics WHERE table_schema = DATABASE() GROUP BY table_name, index_name, non_unique`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var table, name, cols string
		var nonUnique int
		if err := rows.Scan(&table, &name, &nonUnique, &cols); err != nil {
			t.Fatal(err)
		}
		s.indexes[table+"."+name] = indexInfo{nonUnique == 0, cols}
	}
	rows.Close()
	rows, err = db.Query(`SELECT k.table_name, k.column_name, k.referenced_table_name, k.referenced_column_name, r.delete_rule
		FROM information_schema.key_column_usage k JOIN information_schema.referential_constraints r ON r.constraint_schema = k.constraint_schema AND r.constraint_name = k.constraint_name AND r.table_name = k.table_name
		WHERE k.table_schema = DATABASE() AND k.referenced_table_name IS NOT NULL`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var table, col, ref, refcol, rule string
		if err := rows.Scan(&table, &col, &ref, &refcol, &rule); err != nil {
			t.Fatal(err)
		}
		s.fks[fmt.Sprintf("%s.%s->%s.%s:%s", table, col, ref, refcol, rule)] = true
	}
	rows.Close()
	return s
}

func statementsOf(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, part := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n-- @@\n") {
		var kept []string
		for _, line := range strings.Split(part, "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "--") {
				kept = append(kept, line)
			}
		}
		if statement := strings.TrimSpace(strings.Join(kept, "\n")); statement != "" {
			out = append(out, statement)
		}
	}
	return out
}

// Applies the 15 PostgreSQL migrations to a real PostgreSQL and the generated MySQL migrations to a real
// MySQL, then compares what exists: tables, columns (type class and nullability), key and index definitions
// and foreign keys. The MySQL side may have extra generated columns (partial-unique emulation) and the
// counter table that replaces the sequence; nothing else may differ.
func TestAutomationMySQLSchemaMatchesPostgres(t *testing.T) {
	pgDSN, myDSN := os.Getenv("TP_AUTO_SCHEMA_PG_DSN"), os.Getenv("TP_AUTO_SCHEMA_MYSQL_DSN")
	if pgDSN == "" || myDSN == "" {
		t.Skip("TP_AUTO_SCHEMA_PG_DSN and TP_AUTO_SCHEMA_MYSQL_DSN required")
	}
	config, err := pgx.ParseConfig(pgDSN)
	if err != nil {
		t.Fatal(err)
	}
	admin := stdlib.OpenDB(*config)
	schema := "auto_schema_" + strconv.FormatInt(int64(os.Getpid()), 10)
	_, _ = admin.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE`)
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); admin.Close() })
	config.RuntimeParams["search_path"] = schema
	pgDB := stdlib.OpenDB(*config)
	defer pgDB.Close()
	files, _ := filepath.Glob(filepath.Join("..", "migrations", "*.up.sql"))
	sort.Slice(files, func(i, j int) bool {
		a, _ := strconv.Atoi(strings.SplitN(filepath.Base(files[i]), "_", 2)[0])
		b, _ := strconv.Atoi(strings.SplitN(filepath.Base(files[j]), "_", 2)[0])
		return a < b
	})
	if len(files) != 15 {
		t.Fatalf("expected 15 automation migrations, found %d", len(files))
	}
	for _, f := range files {
		raw, _ := os.ReadFile(f)
		if _, err := pgDB.Exec(string(raw)); err != nil {
			t.Fatalf("PostgreSQL migration %s: %v", f, err)
		}
	}

	myDB, err := sql.Open("mysql", myDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer myDB.Close()
	pg := pgShape(t, pgDB, nil)
	var tables []string
	seen := map[string]bool{}
	for key := range pg.columns {
		table := strings.SplitN(key, ".", 2)[0]
		if !seen[table] {
			seen[table] = true
			tables = append(tables, table)
		}
	}
	conn, err := myDB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = conn.ExecContext(context.Background(), "SET FOREIGN_KEY_CHECKS = 0")
	for _, table := range append(tables, "auto_seq_counter") {
		_, _ = conn.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table)
	}
	_, _ = conn.ExecContext(context.Background(), "SET FOREIGN_KEY_CHECKS = 1")
	for _, name := range []string{"010_automation.sql", "011_automation_partial_unique.sql"} {
		for i, statement := range statementsOf(t, filepath.Join("..", "..", "..", "sql", "mysql", name)) {
			if _, err := conn.ExecContext(context.Background(), statement); err != nil {
				t.Fatalf("MySQL %s statement %d: %v\n%s", name, i+1, err, statement)
			}
		}
	}
	t.Cleanup(func() {
		c, _ := myDB.Conn(context.Background())
		if c != nil {
			_, _ = c.ExecContext(context.Background(), "SET FOREIGN_KEY_CHECKS = 0")
			for _, table := range append(tables, "auto_seq_counter") {
				_, _ = c.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table)
			}
			c.Close()
		}
	})
	my := mysqlShape(t, myDB)

	allowedExtraColumns := map[string]bool{"alarm_episodes.active_marker": true, "auto_rules.live_resource_key": true}
	for key, want := range pg.columns {
		got, ok := my.columns[key]
		if !ok {
			t.Errorf("column %s is missing on MySQL", key)
			continue
		}
		if got.typeClass != want.typeClass || got.nullable != want.nullable {
			t.Errorf("column %s: PostgreSQL %+v, MySQL %+v", key, want, got)
		}
	}
	for key := range my.columns {
		table := strings.SplitN(key, ".", 2)[0]
		if _, ok := pg.columns[key]; !ok && !allowedExtraColumns[key] && table != "auto_seq_counter" {
			t.Errorf("column %s exists only on MySQL", key)
		}
	}
	stripGenerated := func(cols string) string {
		var out []string
		for _, c := range strings.Split(cols, ",") {
			if c != "active_marker" {
				out = append(out, c)
			}
		}
		return strings.Join(out, ",")
	}
	for key, want := range pg.indexes {
		table := strings.SplitN(key, ".", 2)[0]
		name := strings.SplitN(key, ".", 2)[1]
		// PostgreSQL names a table's key <table>_pkey and implicit unique constraints <table>_<cols>_key; MySQL
		// names them PRIMARY and after the first column, so those are compared by column list instead.
		if name == "PRIMARY" || strings.HasSuffix(name, "_pkey") || (strings.HasSuffix(name, "_key") && name != "auto_rules_resource_key") {
			found := false
			for k2, got := range my.indexes {
				if strings.HasPrefix(k2, table+".") && got.unique == want.unique && stripGenerated(got.columns) == want.columns {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("key %s (%s unique=%v) has no MySQL equivalent", key, want.columns, want.unique)
			}
			continue
		}
		got, ok := my.indexes[key]
		if !ok {
			t.Errorf("index %s is missing on MySQL", key)
			continue
		}
		wantCols := want.columns
		if name == "auto_rules_resource_key" {
			wantCols = "tenant_id,live_resource_key" // emulation of the partial unique index
		}
		if got.unique != want.unique || stripGenerated(got.columns) != stripGenerated(wantCols) && got.columns != wantCols {
			t.Errorf("index %s: PostgreSQL %+v, MySQL %+v", key, want, got)
		}
	}
	for fk := range pg.fks {
		if !my.fks[fk] {
			t.Errorf("foreign key %s is missing on MySQL", fk)
		}
	}
	if len(pg.columns) < 150 || len(pg.indexes) < 40 {
		t.Fatalf("the comparison looked at too little: %d columns, %d indexes", len(pg.columns), len(pg.indexes))
	}
	t.Logf("compared %d tables, %d columns, %d indexes, %d foreign keys", len(tables), len(pg.columns), len(pg.indexes), len(pg.fks))
}

// The two PostgreSQL partial unique indexes are emulated with generated columns; this proves the database
// still enforces the same invariants rather than leaving them to application code.
func TestAutomationMySQLPartialUniqueInvariants(t *testing.T) {
	myDSN := os.Getenv("TP_AUTO_SCHEMA_MYSQL_DSN")
	if myDSN == "" || os.Getenv("TP_AUTO_SCHEMA_PG_DSN") == "" {
		t.Skip("run together with TestAutomationMySQLSchemaMatchesPostgres (needs both DSNs)")
	}
	db, err := sql.Open("mysql", myDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("SELECT 1 FROM alarm_episodes LIMIT 1"); err != nil {
		t.Skip("the automation schema is not applied (run the schema comparison first): ", err)
	}
	episode := func(id, state, dimension string) error {
		_, err := db.Exec(`INSERT INTO alarm_episodes (id, tenant_id, policy_id, subject_id, dimension, state, revision, epoch, opened_at, last_seq, policy_key)
			VALUES (?, 't1', 'p1', 's1', ?, ?, 1, 1, NOW(6), 1, 'policy-a')`, id, dimension, state)
		return err
	}
	defer func() { _, _ = db.Exec("DELETE FROM alarm_episodes WHERE tenant_id = 't1'") }()
	for _, c := range []struct {
		name  string
		id    string
		state string
		dim   string
		want  bool // accepted
	}{
		{"first active episode", "e1", "active", "temp", true},
		{"a second active episode for the same key", "e2", "active", "temp", false},
		{"an active episode for another dimension", "e3", "active", "humidity", true},
		{"a cleared episode for the same key", "e4", "cleared", "temp", true},
		{"another cleared episode for the same key", "e5", "cleared", "temp", true},
	} {
		if err := episode(c.id, c.state, c.dim); (err == nil) != c.want {
			t.Errorf("%s: accepted=%v err=%v", c.name, err == nil, err)
		}
	}
	if _, err := db.Exec("UPDATE alarm_episodes SET state = 'cleared' WHERE id = 'e1'"); err != nil {
		t.Fatal(err)
	}
	if err := episode("e6", "active", "temp"); err != nil {
		t.Errorf("once the active episode is cleared a new one must be allowed: %v", err)
	}

	rule := func(id, resource string, deleted bool) error {
		var key any
		if resource != "" {
			key = resource
		}
		deletedAt := "NULL"
		if deleted {
			deletedAt = "NOW(6)"
		}
		_, err := db.Exec(`INSERT INTO auto_rules (id, tenant_id, name, kind, definition, definition_hash, resource_key, deleted_at) VALUES (?, 't1', 'n', 'k', '{}', 'h', ?, `+deletedAt+`)`, id, key)
		return err
	}
	defer func() { _, _ = db.Exec("DELETE FROM auto_rules WHERE tenant_id = 't1'") }()
	for _, c := range []struct {
		name     string
		id, key  string
		deleted  bool
		accepted bool
	}{
		{"first live rule with a resource key", "r1", "res-a", false, true},
		{"a second live rule with the same key", "r2", "res-a", false, false},
		{"a soft-deleted rule with the same key", "r3", "res-a", true, true},
		{"rules without a resource key", "r4", "", false, true},
		{"more rules without a resource key", "r5", "", false, true},
		{"the same key under another tenant is a different tenant's concern", "r6", "res-b", false, true},
	} {
		if err := rule(c.id, c.key, c.deleted); (err == nil) != c.accepted {
			t.Errorf("%s: accepted=%v err=%v", c.name, err == nil, err)
		}
	}
	if _, err := db.Exec("UPDATE auto_rules SET deleted_at = NOW(6) WHERE id = 'r1'"); err != nil {
		t.Fatal(err)
	}
	if err := rule("r7", "res-a", false); err != nil {
		t.Errorf("after the live rule is soft-deleted the key must be reusable: %v", err)
	}
}
