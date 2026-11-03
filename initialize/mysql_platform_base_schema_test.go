package initialize

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	componentsql "project/sql"
)

// platformBaseOrder is the order the MySQL migrations are applied in.
func platformBaseOrder() []string {
	var names []string
	names = append(names, componentsql.MySQLMigrationNames()...)
	names = append(names, componentsql.MySQLAuthMigrationNames()...)
	names = append(names, componentsql.MySQLDeviceStatusMigrationNames()...)
	names = append(names, componentsql.MySQLServiceAccessMigrationNames()...)
	names = append(names, componentsql.MySQLHistoryIntentMigrationNames()...)
	names = append(names, componentsql.MySQLPlatformConfigMigrationNames()...)
	names = append(names, componentsql.MySQLAutomationMigrationNames()...)
	names = append(names, componentsql.MySQLPlatformBaseMigrationNames()...)
	return names
}

type columnInfo struct {
	class    string
	nullable bool
	length   int
}

func pgClass(dataType string) string {
	switch dataType {
	case "character varying", "character", "text":
		return "string"
	case "integer":
		return "int"
	case "smallint":
		return "smallint"
	case "bigint":
		return "bigint"
	case "boolean":
		return "bool"
	case "numeric":
		return "decimal"
	case "timestamp with time zone", "timestamp without time zone":
		return "datetime"
	case "time without time zone", "time with time zone":
		return "time"
	case "json", "jsonb":
		return "json"
	case "uuid":
		return "string"
	case "double precision":
		return "double"
	case "bytea":
		return "blob"
	}
	return dataType
}

func myClass(dataType, columnType string) string {
	switch dataType {
	case "varchar", "char", "longtext", "text", "mediumtext":
		return "string"
	case "int":
		return "int"
	case "smallint":
		return "smallint"
	case "bigint":
		return "bigint"
	case "tinyint":
		if columnType == "tinyint(1)" {
			return "bool"
		}
	case "decimal":
		return "decimal"
	case "datetime", "timestamp":
		return "datetime"
	case "time":
		return "time"
	case "json":
		return "json"
	case "double":
		return "double"
	case "longblob", "blob":
		return "blob"
	}
	return dataType
}

func readColumns(t *testing.T, db *sql.DB, query string, myside bool) map[string]map[string]columnInfo {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]map[string]columnInfo{}
	for rows.Next() {
		var table, column, dataType, columnType, nullable string
		var length sql.NullInt64
		if err := rows.Scan(&table, &column, &dataType, &columnType, &nullable, &length); err != nil {
			t.Fatal(err)
		}
		if out[table] == nil {
			out[table] = map[string]columnInfo{}
		}
		info := columnInfo{nullable: nullable == "YES"}
		if myside {
			info.class = myClass(dataType, columnType)
		} else {
			info.class = pgClass(dataType)
		}
		if length.Valid && dataType != "text" && dataType != "longtext" {
			info.length = int(length.Int64)
		}
		out[table][column] = info
	}
	return out
}

// keySets returns "table|kind|col,col" for primary keys, unique indexes and plain indexes.
func keySets(t *testing.T, db *sql.DB, query string) map[string]bool {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var table, kind, cols string
		if err := rows.Scan(&table, &kind, &cols); err != nil {
			t.Fatal(err)
		}
		out[table+"|"+kind+"|"+cols] = true
	}
	return out
}

// openPlatformDialects connects to the PostgreSQL oracle and an empty MySQL database and applies every MySQL
// migration to it. Both are closed, and MySQL emptied again, when the test ends.
func openPlatformDialects(t *testing.T) (pg, my *sql.DB) {
	t.Helper()
	pgDSN := os.Getenv("TEST_PG_SCHEMA_DSN")
	host, port, user, pass, name := os.Getenv("TP_MYSQL_TEST_HOST"), os.Getenv("TP_MYSQL_TEST_PORT"), os.Getenv("TP_MYSQL_TEST_USER"), os.Getenv("TP_MYSQL_TEST_PASSWORD"), os.Getenv("TP_MYSQL_TEST_DB")
	if pgDSN == "" || host == "" || port == "" {
		t.Skip("TEST_PG_SCHEMA_DSN (a database built by sql/1.sql..32.sql) and TP_MYSQL_TEST_* required")
	}
	pgGorm, err := gorm.Open(postgres.Open(pgDSN), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	pg, _ = pgGorm.DB()
	t.Cleanup(func() { pg.Close() })
	portNumber, _ := strconv.Atoi(port)
	myGorm, err := MysqlConnect(&DbConfig{Host: host, Port: portNumber, Username: user, Password: pass, DbName: name, TimeZone: "UTC", LogLevel: 1, IdleConns: 2, OpenConns: 5})
	if err != nil {
		t.Fatal(err)
	}
	my, _ = myGorm.DB()
	t.Cleanup(func() { my.Close() })
	ctx := context.Background()
	conn, err := my.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	// The platform schema is large and other tests in this package use the same database: start and end empty.
	dropAll := func() { dropAllMySQLObjects(conn) }
	dropAll()
	t.Cleanup(dropAll)
	for _, file := range platformBaseOrder() {
		statements, err := componentsql.MySQLMigrationStatements(file)
		if err != nil {
			t.Fatal(err)
		}
		for i, statement := range statements {
			if _, err := conn.ExecContext(ctx, statement); err != nil {
				t.Fatalf("MySQL %s statement %d: %v\n%.300s", file, i+1, err, statement)
			}
		}
	}
	return pg, my
}

func TestMySQLPlatformSchemaCoversEveryPostgresPlatformTable(t *testing.T) {
	pg, my := openPlatformDialects(t)

	// Platform tables that have no MySQL counterpart by design.
	ignored := map[string]string{
		"casbin_rule":     "created by the casbin adapter at start-up",
		"telemetry_datas": "history lives in the history backend, not in the relational database",
	}
	pgCols := readColumns(t, pg, `SELECT table_name, column_name, data_type, data_type, is_nullable, character_maximum_length
        FROM information_schema.columns c WHERE table_schema='public'
        AND EXISTS (SELECT 1 FROM information_schema.tables t WHERE t.table_schema=c.table_schema AND t.table_name=c.table_name AND t.table_type='BASE TABLE')`, false)
	myCols := readColumns(t, my, `SELECT table_name, column_name, data_type, column_type, is_nullable, character_maximum_length
        FROM information_schema.columns c WHERE table_schema=DATABASE()
        AND EXISTS (SELECT 1 FROM information_schema.tables t WHERE t.table_schema=c.table_schema AND t.table_name=c.table_name AND t.table_type='BASE TABLE')`, true)

	// Documented, deliberate differences from the PostgreSQL schema.
	known := map[string]string{
		"devices.voucher_digest":               "MySQL cannot index the 2048-character voucher; a stored SHA-256 carries the unique key",
		"users.auth_version":                   "added by the MySQL auth migration",
		"service_access.service_access_config": "stored as text so the key order of the configuration survives (MySQL JSON reorders keys)",
	}
	var problems []string
	var tables []string
	for table := range pgCols {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	covered := 0
	for _, table := range tables {
		if _, skip := ignored[table]; skip {
			continue
		}
		mine, ok := myCols[table]
		if !ok {
			problems = append(problems, "missing table "+table)
			continue
		}
		covered++
		for column, want := range pgCols[table] {
			got, ok := mine[column]
			switch {
			case !ok:
				problems = append(problems, fmt.Sprintf("%s.%s: missing column", table, column))
			case got.class != want.class:
				problems = append(problems, fmt.Sprintf("%s.%s: type %s, MySQL %s", table, column, want.class, got.class))
			case got.nullable != want.nullable:
				problems = append(problems, fmt.Sprintf("%s.%s: nullable=%v, MySQL nullable=%v", table, column, want.nullable, got.nullable))
			case want.class == "string" && want.length > 0 && got.length != 0 && got.length != want.length && got.length != 191 && got.length != 36:
				problems = append(problems, fmt.Sprintf("%s.%s: length %d, MySQL %d", table, column, want.length, got.length))
			}
		}
		for column := range mine {
			if _, ok := pgCols[table][column]; !ok && !strings.HasSuffix(column, "__uk") {
				problems = append(problems, fmt.Sprintf("%s.%s: column only in MySQL", table, column))
			}
		}
	}

	pgKeys := keySets(t, pg, `
        SELECT c.relname, CASE WHEN x.indisprimary THEN 'primary' WHEN x.indisunique THEN 'unique' ELSE 'index' END,
               array_to_string(ARRAY(SELECT a.attname FROM unnest(x.indkey) WITH ORDINALITY k(attnum, ord)
                                      JOIN pg_attribute a ON a.attrelid=c.oid AND a.attnum=k.attnum ORDER BY k.ord), ',')
          FROM pg_index x JOIN pg_class c ON c.oid=x.indrelid JOIN pg_namespace n ON n.oid=c.relnamespace
         WHERE n.nspname='public' AND x.indpred IS NULL AND x.indexprs IS NULL`)
	myKeys := keySets(t, my, `
        SELECT table_name, CASE WHEN index_name='PRIMARY' THEN 'primary' WHEN MIN(non_unique)=0 THEN 'unique' ELSE 'index' END,
               GROUP_CONCAT(column_name ORDER BY seq_in_index SEPARATOR ',')
          FROM information_schema.statistics WHERE table_schema=DATABASE() AND column_name IS NOT NULL
         GROUP BY table_name, index_name`)
	// An over-long unique key is emulated by a unique key on a stored hash column (name ends in __uk).
	hashed := map[string][]string{}
	hashRows, err := my.Query(`SELECT table_name, generation_expression FROM information_schema.columns
        WHERE table_schema=DATABASE() AND column_name LIKE '%\_\_uk'`)
	if err != nil {
		t.Fatal(err)
	}
	for hashRows.Next() {
		var table, expression string
		if err := hashRows.Scan(&table, &expression); err != nil {
			t.Fatal(err)
		}
		hashed[table] = append(hashed[table], expression)
	}
	hashRows.Close()
	emulated := func(key string) bool {
		parts := strings.SplitN(key, "|", 3)
		if parts[1] != "unique" {
			return false
		}
		for _, expression := range hashed[parts[0]] {
			all := true
			for _, c := range strings.Split(parts[2], ",") {
				all = all && strings.Contains(expression, "`"+c+"`")
			}
			if all {
				return true
			}
		}
		return false
	}
	var keyProblems []string
	for key := range pgKeys {
		table := strings.SplitN(key, "|", 2)[0]
		if key == "devices|unique|voucher" {
			continue // carried by devices.voucher_digest
		}
		if _, skip := ignored[table]; skip {
			continue
		}
		if !myKeys[key] && !emulated(key) {
			keyProblems = append(keyProblems, "missing in MySQL: "+key)
		}
	}
	problems = filterKnown(problems, known)
	sort.Strings(problems)
	sort.Strings(keyProblems)
	t.Logf("%d PostgreSQL platform tables, %d present in MySQL, %d column differences, %d key differences", len(tables)-len(ignored), covered, len(problems), len(keyProblems))
	for _, p := range append(problems, keyProblems...) {
		t.Error(p)
	}
}

func filterKnown(problems []string, known map[string]string) []string {
	var out []string
	for _, p := range problems {
		skip := false
		for column := range known {
			if strings.HasPrefix(p, column+":") {
				skip = true
			}
		}
		if !skip {
			out = append(out, p)
		}
	}
	return out
}

// dropAllMySQLObjects empties the test database (tables, views) so the next test starts from nothing.
func dropAllMySQLObjects(conn *sql.Conn) {
	ctx := context.Background()
	_, _ = conn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 0")
	rows, _ := conn.QueryContext(ctx, "SELECT table_name, table_type FROM information_schema.tables WHERE table_schema = DATABASE()")
	var existing [][2]string
	for rows.Next() {
		var n, kind string
		_ = rows.Scan(&n, &kind)
		existing = append(existing, [2]string{n, kind})
	}
	rows.Close()
	for _, n := range existing {
		if n[1] == "VIEW" {
			_, _ = conn.ExecContext(ctx, "DROP VIEW IF EXISTS `"+n[0]+"`")
		} else {
			_, _ = conn.ExecContext(ctx, "DROP TABLE IF EXISTS `"+n[0]+"`")
		}
	}
	_, _ = conn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 1")
}
