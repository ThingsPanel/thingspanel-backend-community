package initialize

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMySQLSeedRowsMatchWhatThePostgresMigrationsInsert(t *testing.T) {
	pg, my := openPlatformDialects(t)
	tables := map[string]string{
		"sys_function": "id", "sys_dict": "id", "sys_dict_language": "id", "sys_ui_elements": "id", "data_policy": "id",
		"logo": "id", "notification_services_config": "id", "service_plugins": "id", "users": "id", "boards": "id",
	}
	var names []string
	for name := range tables {
		names = append(names, name)
	}
	sort.Strings(names)
	total := 0
	for _, table := range names {
		pgRows := readTable(t, pg, table, true)
		myRows := readTable(t, my, table, false)
		total += len(pgRows)
		if len(pgRows) == 0 {
			t.Errorf("%s: the PostgreSQL oracle has no seed rows; the oracle is not built from sql/1..32", table)
		}
		for key, want := range pgRows {
			got, ok := myRows[key]
			if !ok {
				t.Errorf("%s %s: missing in MySQL", table, key)
				continue
			}
			for column, value := range want {
				if !reflect.DeepEqual(value, got[column]) {
					t.Errorf("%s %s.%s: PostgreSQL %v (%T), MySQL %v (%T)", table, key, column, value, value, got[column], got[column])
				}
			}
		}
		if len(myRows) != len(pgRows) {
			t.Errorf("%s: %d rows in PostgreSQL, %d in MySQL", table, len(pgRows), len(myRows))
		}
	}
	var rules int
	if err := my.QueryRow("SELECT COUNT(*) FROM casbin_rule").Scan(&rules); err != nil || rules != 3 {
		t.Errorf("casbin_rule: %d rows, err %v; want the 3 rows of sql/25.sql", rules, err)
	}
	t.Logf("%d seed rows in %d tables compared", total, len(names))
}

// readTable returns key -> column -> value; a missing MySQL-only column is ignored by the caller.
func readTable(t *testing.T, db *sql.DB, table string, postgres bool) map[string]map[string]any {
	t.Helper()
	rows, err := db.Query("SELECT * FROM " + table)
	if err != nil {
		t.Fatalf("%s: %v", table, err)
	}
	defer rows.Close()
	columns, _ := rows.Columns()
	types, _ := rows.ColumnTypes()
	out := map[string]map[string]any{}
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatal(err)
		}
		row := map[string]any{}
		for i, column := range columns {
			row[column] = normalise(values[i], types[i].DatabaseTypeName())
		}
		out[fmt.Sprint(row["id"])] = row
	}
	return out
}

func normalise(v any, dbType string) any {
	switch x := v.(type) {
	case nil:
		return nil
	case []byte:
		v = string(x)
	}
	switch x := v.(type) {
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano)
	case bool:
		if x {
			return int64(1)
		}
		return int64(0)
	case int:
		return int64(x)
	case int8:
		return int64(x)
	case int16:
		return int64(x)
	case int32:
		return int64(x)
	case string:
		upper := strings.ToUpper(dbType)
		switch upper {
		case "INT", "BIGINT", "SMALLINT", "TINYINT", "MEDIUMINT", "INT2", "INT4", "INT8", "UNSIGNED INT", "UNSIGNED BIGINT":
			if n, err := strconv.ParseInt(x, 10, 64); err == nil {
				return n
			}
		case "DOUBLE", "FLOAT", "FLOAT8", "FLOAT4", "DECIMAL", "NUMERIC":
			if f, err := strconv.ParseFloat(x, 64); err == nil {
				return f
			}
		}
		if upper == "JSON" || upper == "JSONB" {
			var doc any
			if json.Unmarshal([]byte(x), &doc) == nil {
				return doc
			}
		}
		return x
	}
	return v
}
