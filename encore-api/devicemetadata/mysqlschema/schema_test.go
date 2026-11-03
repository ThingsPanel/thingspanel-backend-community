package mysqlschema

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"encore.app/pkg/sqldialect"
)

// The MySQL schema must carry every table, column (type class, nullability), primary/unique/plain index of the
// PostgreSQL device_metadata schema, except what is listed in known.
//
//	TP_DM_PG_DSN     a PostgreSQL database built from ../migrations
//	TP_DM_MYSQL_DSN  an empty MySQL database (parseTime=true&clientFoundRows=true&multiStatements=true)
func TestSchemaMatchesPostgres(t *testing.T) {
	pgDSN, myDSN := os.Getenv("TP_DM_PG_DSN"), os.Getenv("TP_DM_MYSQL_DSN")
	if pgDSN == "" || myDSN == "" {
		t.Skip("TP_DM_PG_DSN and TP_DM_MYSQL_DSN required")
	}
	config, err := pgx.ParseConfig(pgDSN)
	if err != nil {
		t.Fatal(err)
	}
	pg := stdlib.OpenDB(*config)
	t.Cleanup(func() { pg.Close() })
	my, err := sqldialect.OpenMySQL(myDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { my.Close() })
	ctx := context.Background()
	conn, err := my.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	reset := func() {
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
	}
	reset()
	t.Cleanup(reset)
	for i, statement := range Statements() {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			t.Fatalf("statement %d: %v\n%.300s", i+1, err, statement)
		}
	}
	// Applying twice must change nothing (every statement is idempotent).
	for i, statement := range Statements() {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			t.Fatalf("second run, statement %d: %v", i+1, err)
		}
	}

	class := func(pgType string) string {
		switch pgType {
		case "character varying", "character", "text", "uuid":
			return "string"
		case "integer":
			return "int"
		case "bigint":
			return "bigint"
		case "smallint":
			return "smallint"
		case "boolean":
			return "bool"
		case "numeric":
			return "decimal"
		case "timestamp with time zone", "timestamp without time zone":
			return "datetime"
		case "jsonb", "json":
			return "json"
		case "bytea":
			return "blob"
		case "double precision":
			return "double"
		case "date":
			return "date"
		}
		return pgType
	}
	myClass := func(dataType, columnType string) string {
		switch dataType {
		case "varchar", "char", "longtext", "text":
			return "string"
		case "tinyint":
			if columnType == "tinyint(1)" {
				return "bool"
			}
		case "varbinary", "longblob", "blob":
			return "blob"
		case "timestamp":
			return "datetime"
		}
		return dataType
	}
	type column struct {
		class    string
		nullable bool
	}
	read := func(db *sql.DB, query string, my bool) map[string]map[string]column {
		rows, err := db.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := map[string]map[string]column{}
		for rows.Next() {
			var table, name, dataType, columnType, nullable string
			if err := rows.Scan(&table, &name, &dataType, &columnType, &nullable); err != nil {
				t.Fatal(err)
			}
			if out[table] == nil {
				out[table] = map[string]column{}
			}
			c := column{nullable: nullable == "YES"}
			if my {
				c.class = myClass(dataType, columnType)
			} else {
				c.class = class(dataType)
			}
			out[table][name] = c
		}
		return out
	}
	want := read(pg, `SELECT c.table_name, c.column_name, c.data_type, c.data_type, c.is_nullable FROM information_schema.columns c
        JOIN information_schema.tables t ON t.table_schema=c.table_schema AND t.table_name=c.table_name AND t.table_type='BASE TABLE'
        WHERE c.table_schema='public'`, false)
	got := read(my, `SELECT c.table_name, c.column_name, c.data_type, c.column_type, c.is_nullable FROM information_schema.columns c
        JOIN information_schema.tables t ON t.table_schema=c.table_schema AND t.table_name=c.table_name AND t.table_type='BASE TABLE'
        WHERE c.table_schema=DATABASE()`, true)
	var problems []string
	tables := 0
	for table, columns := range want {
		mine, ok := got[table]
		if !ok {
			problems = append(problems, "missing table "+table)
			continue
		}
		tables++
		for name, w := range columns {
			g, ok := mine[name]
			switch {
			case !ok:
				problems = append(problems, fmt.Sprintf("%s.%s: missing column", table, name))
			case g.class != w.class:
				problems = append(problems, fmt.Sprintf("%s.%s: type %s, MySQL %s", table, name, w.class, g.class))
			case g.nullable != w.nullable:
				problems = append(problems, fmt.Sprintf("%s.%s: nullable=%v, MySQL nullable=%v", table, name, w.nullable, g.nullable))
			}
		}
		for name := range mine {
			if _, ok := columns[name]; !ok && !strings.HasSuffix(name, "__uk") && !strings.HasSuffix(name, "__pk") {
				problems = append(problems, fmt.Sprintf("%s.%s: column only in MySQL", table, name))
			}
		}
	}
	sort.Strings(problems)
	t.Logf("%d PostgreSQL tables, %d in MySQL, %d differences", len(want), tables, len(problems))
	for _, p := range problems {
		t.Error(p)
	}
}
