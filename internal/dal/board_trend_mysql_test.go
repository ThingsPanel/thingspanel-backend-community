package dal

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational"
	"project/internal/model"
	global "project/pkg/global"
	componentsql "project/sql"
)

func trendDialects(t *testing.T) (pg, my *gorm.DB) {
	t.Helper()
	pgDSN := os.Getenv("TEST_PG_SCHEMA_DSN")
	host, port := os.Getenv("TP_MYSQL_TEST_HOST"), os.Getenv("TP_MYSQL_TEST_PORT")
	if pgDSN == "" || host == "" || port == "" {
		t.Skip("TEST_PG_SCHEMA_DSN and TP_MYSQL_TEST_* required")
	}
	pg, err := gorm.Open(postgres.Open(pgDSN), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	pgPool, _ := pg.DB()
	pgPool.SetMaxOpenConns(1)
	if err := pg.Exec("SET session_replication_role = replica").Error; err != nil { // skip the identity trigger and FK checks on the oracle
		t.Fatal(err)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	my, err = relational.Open(context.Background(), relational.Target{
		Name: "board-trend-test", Dialect: relational.MySQL, Flavor: relational.Flavor(os.Getenv("TP_MYSQL_TEST_FLAVOR")),
		Host: host, Port: portNumber, Database: os.Getenv("TP_MYSQL_TEST_DB"), Username: os.Getenv("TP_MYSQL_TEST_USER"),
		Password: os.Getenv("TP_MYSQL_TEST_PASSWORD"), TimeZone: "UTC", Pool: relational.PoolOptions{MaxOpenConns: 5, MaxIdleConns: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"device_status_history", "devices", "users"} {
		_ = my.Exec("DROP TABLE IF EXISTS " + table).Error
	}
	for _, names := range [][]string{{"001_core_users_devices.sql"}, componentsql.MySQLDeviceStatusMigrationNames()} {
		for _, name := range names {
			statements, err := componentsql.MySQLFamilyMigrationStatements(os.Getenv("TP_MYSQL_TEST_FLAVOR"), name)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range statements {
				if err := my.Exec(s).Error; err != nil {
					t.Fatalf("apply %s: %v", name, err)
				}
			}
		}
	}
	previous := global.DB
	t.Cleanup(func() {
		global.DB = previous
		_ = my.Exec("DROP TABLE IF EXISTS device_status_history").Error
		_ = my.Exec("DROP TABLE IF EXISTS devices").Error
		_ = my.Exec("DROP TABLE IF EXISTS users").Error
		_ = relational.CloseDB(my)
		_ = pgPool.Close()
	})
	return pg, my
}

func TestDeviceTrendMatchesPostgresOnMySQL(t *testing.T) {
	pg, my := trendDialects(t)
	suffix := uuid.NewString()[:8]
	tenant := "tr-" + suffix
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	at := func(h, m int) time.Time { return base.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute) }
	seed := func(db *gorm.DB) {
		for i, created := range []time.Time{at(-2000, 0), at(-100, 0), at(-100, 0), at(30, 0), at(500, 0)} { // the last is created after every window
			id := fmt.Sprintf("dev-%d-%s", i, suffix)
			if err := db.Exec("INSERT INTO devices (id, voucher, tenant_id, device_number, created_at, is_online) VALUES (?, ?, ?, ?, ?, 0)", id, "v-"+id, tenant, "n-"+id, created).Error; err != nil {
				t.Fatalf("seed device: %v", err)
			}
		}
		events := []struct {
			device, hour, minute, status int
		}{
			{0, -50, 0, 1}, {0, 2, 10, 0}, {0, 2, 20, 0}, {0, 3, 5, 1}, {0, 3, 50, 1}, {0, 9, 0, 0}, // repeated states change nothing
			{1, -3, 0, 0}, {1, 1, 45, 1}, {1, 4, 0, 1}, {1, 6, 30, 0},
			{2, 5, 0, 1}, {2, 5, 0, 0}, {2, 5, 0, 1}, // same instant: id breaks the tie
			{3, 31, 0, 1},
		}
		for _, e := range events {
			id := fmt.Sprintf("dev-%d-%s", e.device, suffix)
			if err := db.Exec("INSERT INTO device_status_history (tenant_id, device_id, status, change_time) VALUES (?, ?, ?, ?)", tenant, id, e.status, at(e.hour, e.minute)).Error; err != nil {
				t.Fatalf("seed event: %v", err)
			}
		}
	}
	seed(pg)
	seed(my)
	t.Cleanup(func() {
		_ = pg.Exec("DELETE FROM device_status_history WHERE tenant_id = ?", tenant).Error
		_ = pg.Exec("DELETE FROM devices WHERE tenant_id = ?", tenant).Error
	})
	ptr := func(t time.Time) *int64 { v := t.Unix(); return &v }
	cases := []struct {
		name       string
		start, end *int64
		minimum    int
	}{
		{"aligned window", ptr(at(0, 0)), ptr(at(12, 0)), 13},
		{"unaligned window", ptr(at(1, 25)), ptr(at(7, 40)), 7},
		{"window that starts before any event", ptr(at(-60, 0)), ptr(at(40, 0)), 101},
		{"single hour", ptr(at(5, 10)), ptr(at(5, 50)), 1},
		{"sixty days (beyond the default recursion limit)", ptr(at(-10, 0)), ptr(at(24*60-10, 0)), 1441},
		{"start after end yields nothing", ptr(at(10, 0)), ptr(at(2, 0)), 0},
		{"empty tenant", ptr(at(0, 0)), ptr(at(5, 0)), 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tenantID := tenant
			if c.name == "empty tenant" {
				tenantID = "nobody-" + suffix
			}
			global.DB = pg
			want, err := GetDeviceTrend(tenantID, c.start, c.end)
			if err != nil {
				t.Fatalf("PostgreSQL: %v", err)
			}
			global.DB = my
			got, err := GetDeviceTrend(tenantID, c.start, c.end)
			if err != nil {
				t.Fatalf("MySQL: %v", err)
			}
			if len(want) < c.minimum && c.minimum > 0 {
				t.Fatalf("the oracle returned %d points, expected at least %d", len(want), c.minimum)
			}
			norm := func(in []model.DeviceTrendPoint) []string {
				var out []string
				for _, p := range in {
					out = append(out, fmt.Sprintf("%d|%d|%d|%d", p.Timestamp.Unix(), p.DeviceTotal, p.DeviceOnline, p.DeviceOffline))
				}
				return out
			}
			if !reflect.DeepEqual(norm(want), norm(got)) {
				t.Fatalf("trend differs\n pg: %s\n my: %s", strings.Join(head(norm(want), 12), " "), strings.Join(head(norm(got), 12), " "))
			}
		})
	}
}

func head(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
