package dal

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"project/internal/model"
	"project/internal/query"
	componentsql "project/sql"
)

func TestCurrentTelemetryReadsBehaveTheSameOnMySQLAsOnPostgres(t *testing.T) {
	pgDSN := os.Getenv("TEST_DBDIALECT_PG_DSN")
	host, port := os.Getenv("TP_MYSQL_TEST_HOST"), os.Getenv("TP_MYSQL_TEST_PORT")
	if pgDSN == "" || host == "" {
		t.Skip("TEST_DBDIALECT_PG_DSN and TP_MYSQL_TEST_* required")
	}
	pg, err := gorm.Open(postgres.Open(pgDSN), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	_ = pg.Exec("DROP TABLE IF EXISTS telemetry_current_datas").Error
	if err := pg.Exec(`CREATE TABLE telemetry_current_datas (device_id varchar(36) NOT NULL, "key" varchar(255) NOT NULL, ts timestamptz(6) NOT NULL,
		bool_v bool, number_v float8, string_v text, tenant_id varchar(36), CONSTRAINT telemetry_current_datas_unique UNIQUE (device_id, "key"))`).Error; err != nil {
		t.Fatal(err)
	}
	cfg := mysqldriver.NewConfig()
	cfg.User, cfg.Passwd, cfg.Net, cfg.Addr = os.Getenv("TP_MYSQL_TEST_USER"), os.Getenv("TP_MYSQL_TEST_PASSWORD"), "tcp", host+":"+port
	cfg.DBName, cfg.ParseTime, cfg.Loc, cfg.ClientFoundRows = os.Getenv("TP_MYSQL_TEST_DB"), true, time.UTC, true
	my, err := gorm.Open(mysql.Open(cfg.FormatDSN()), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"telemetry_history_deletions", "telemetry_history_intents", "telemetry_current_datas"} {
		_ = my.Exec("DROP TABLE IF EXISTS " + table).Error
	}
	for _, name := range componentsql.MySQLHistoryIntentMigrationNames() {
		statements, _ := componentsql.MySQLMigrationStatements(name)
		for _, s := range statements {
			if err := my.Exec(s).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Cleanup(func() {
		_ = pg.Exec("DROP TABLE IF EXISTS telemetry_current_datas").Error
		for _, table := range []string{"telemetry_history_deletions", "telemetry_history_intents", "telemetry_current_datas"} {
			_ = my.Exec("DROP TABLE IF EXISTS " + table).Error
		}
	})
	base := time.Date(2026, 11, 3, 1, 2, 3, 456000000, time.UTC)
	seed := func(db *gorm.DB) {
		for i, row := range []struct {
			device, key, tenant string
			offset              int
			number              *float64
			flag                *bool
			text                *string
		}{
			{"dev-1", "temp", "ten-a", 5, fp(21.5), nil, nil}, {"dev-1", "switch", "ten-a", 9, nil, bp(true), nil},
			{"dev-1", "label", "ten-a", 1, nil, nil, sp("boiler")}, {"dev-2", "temp", "ten-a", 20, fp(18), nil, nil},
			{"dev-3", "temp", "ten-a", 3, fp(1), nil, nil}, {"dev-4", "temp", "ten-a", 30, fp(2), nil, nil},
			{"dev-5", "temp", "ten-b", 99, fp(3), nil, nil},
		} {
			ts := base.Add(time.Duration(row.offset) * time.Minute)
			if err := db.Exec("INSERT INTO telemetry_current_datas (device_id, "+quote(db, "key")+", ts, bool_v, number_v, string_v, tenant_id) VALUES (?,?,?,?,?,?,?)",
				row.device, row.key, ts, row.flag, row.number, row.text, row.tenant).Error; err != nil {
				t.Fatalf("seed %d: %v", i, err)
			}
		}
	}
	seed(pg)
	seed(my)
	run := func(db *gorm.DB) []string {
		query.SetDefault(db)
		var out []string
		rows := func(label string, data []*model.TelemetryCurrentData, err error) {
			var lines []string
			for _, d := range data {
				lines = append(lines, fmt.Sprintf("%s/%s@%d number=%s bool=%s text=%s", d.DeviceID, d.Key, d.T.UnixMilli(), pv(d.NumberV), pbool(d.BoolV), pv(d.StringV)))
			}
			out = append(out, fmt.Sprintf("%s: err=%v %v", label, err != nil, lines))
		}
		a, err := GetCurrentTelemetryDataEvolution("dev-1")
		rows("all keys of dev-1, newest first", a, err)
		b, err := GetCurrentTelemetryDataEvolutionByKeys("dev-1", []string{"temp", "label", "missing"})
		rows("selected keys", b, err)
		c, err := GetCurrentTelemetryDataEvolution("nobody")
		rows("unknown device", c, err)
		for _, key := range []string{"temp", "switch", "label", "missing"} {
			v, err := GetCurrentTelemetryDataOneKeys("dev-1", key)
			out = append(out, fmt.Sprintf("one key %s: %v (%T) err=%v", key, v, v, err != nil))
		}
		recent, err := GetTenantTelemetryData("ten-a")
		var lines []string
		for _, r := range recent {
			lines = append(lines, fmt.Sprintf("%s@%d", r.DeviceID, r.Timestamp.UnixMilli()))
		}
		out = append(out, fmt.Sprintf("three most recent devices of ten-a: err=%v %v", err != nil, lines))
		empty, err := GetTenantTelemetryData("nobody")
		out = append(out, fmt.Sprintf("tenant without data: err=%v count=%d", err != nil, len(empty)))
		out = append(out, fmt.Sprintf("delete one key: err=%v", DeleteCurrentTelemetryData("dev-1", "label") != nil))
		after, _ := GetCurrentTelemetryDataEvolution("dev-1")
		var keys []string
		for _, d := range after {
			keys = append(keys, d.Key)
		}
		sort.Strings(keys)
		out = append(out, fmt.Sprintf("keys after the delete: %v", keys))
		return out
	}
	a, b := run(pg), run(my)
	if !reflect.DeepEqual(a, b) {
		for i := range a {
			if i >= len(b) || a[i] != b[i] {
				bb := ""
				if i < len(b) {
					bb = b[i]
				}
				t.Errorf("step %d differs\n  pg: %s\n  my: %s", i, a[i], bb)
			}
		}
	}
	if len(a) != 11 {
		t.Fatalf("unexpected scenario length %d: %v", len(a), a)
	}
}

func quote(db *gorm.DB, column string) string {
	if db.Dialector.Name() == "mysql" {
		return "`" + column + "`"
	}
	return `"` + column + `"`
}
func fp(v float64) *float64 { return &v }
func bp(v bool) *bool       { return &v }
func sp(v string) *string   { return &v }
func pv[T any](v *T) string {
	if v == nil {
		return "nil"
	}
	return fmt.Sprint(*v)
}
func pbool(v *bool) string { return pv(v) }
