package dal

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"project/internal/model"
	"project/internal/query"
	componentsql "project/sql"
)

func TestDeviceIDReservationsBehaveTheSameOnMySQLAsOnPostgres(t *testing.T) {
	pgDSN := os.Getenv("TEST_DBDIALECT_PG_DSN")
	host, port := os.Getenv("TP_MYSQL_TEST_HOST"), os.Getenv("TP_MYSQL_TEST_PORT")
	if pgDSN == "" || host == "" {
		t.Skip("TEST_DBDIALECT_PG_DSN and TP_MYSQL_TEST_* required")
	}
	pg, err := gorm.Open(postgres.Open(pgDSN), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := pg.Exec(`CREATE TABLE IF NOT EXISTS device_id_reservations (id text PRIMARY KEY, tenant_id text NOT NULL,
		state varchar(16) NOT NULL CHECK (state IN ('active','tombstoned')), created_at timestamptz NOT NULL DEFAULT now(), deleted_at timestamptz,
		CHECK ((state='active' AND deleted_at IS NULL) OR (state='tombstoned' AND deleted_at IS NOT NULL)))`).Error; err != nil {
		t.Fatal(err)
	}
	cfg := mysqldriver.NewConfig()
	cfg.User, cfg.Passwd, cfg.Net, cfg.Addr = os.Getenv("TP_MYSQL_TEST_USER"), os.Getenv("TP_MYSQL_TEST_PASSWORD"), "tcp", host+":"+port
	cfg.DBName, cfg.ParseTime, cfg.Loc, cfg.ClientFoundRows = os.Getenv("TP_MYSQL_TEST_DB"), true, time.UTC, true
	my, err := gorm.Open(mysql.Open(cfg.FormatDSN()), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"device_id_reservations", "devices", "users"} {
		_ = my.Exec("DROP TABLE IF EXISTS " + table).Error
	}
	for _, name := range componentsql.MySQLMigrationNames() {
		statements, err := componentsql.MySQLMigrationStatements(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range statements {
			if err := my.Exec(s).Error; err != nil {
				t.Fatalf("apply %s: %v", name, err)
			}
		}
	}
	t.Cleanup(func() {
		_ = pg.Exec("DROP TABLE IF EXISTS device_id_reservations").Error
		for _, table := range []string{"device_id_reservations", "devices", "users"} {
			_ = my.Exec("DROP TABLE IF EXISTS " + table).Error
		}
	})
	suffix := uuid.NewString()[:8]
	run := func(db *gorm.DB) []string {
		var out []string
		note := func(name string, err error) {
			switch {
			case err == nil:
				out = append(out, name+": ok")
			case errors.Is(err, ErrDeviceIDReserved):
				out = append(out, name+": reserved")
			case errors.Is(err, ErrDeviceIdentityState):
				out = append(out, name+": state")
			default:
				out = append(out, name+": other "+err.Error())
			}
		}
		tx := query.Use(db)
		d := func(id, tenant string) *model.Device { return &model.Device{ID: id + "-" + suffix, TenantID: tenant} }
		note("reserve two", reserveDeviceIDs(tx, []*model.Device{d("b", "t1"), d("a", "t1")}))
		note("reserve one of them again", reserveDeviceIDs(tx, []*model.Device{d("a", "t1")}))
		note("same id under another tenant", reserveDeviceIDs(tx, []*model.Device{d("a", "t2")}))
		note("a batch whose second id is taken", reserveDeviceIDs(tx, []*model.Device{d("c", "t1"), d("b", "t1")}))
		note("blank id", reserveDeviceIDs(tx, []*model.Device{{ID: " ", TenantID: "t1"}}))
		note("blank tenant", reserveDeviceIDs(tx, []*model.Device{{ID: "x-" + suffix, TenantID: ""}}))
		note("tombstone", tombstoneDeviceID(tx, "a-"+suffix, "t1"))
		note("tombstone again", tombstoneDeviceID(tx, "a-"+suffix, "t1"))
		note("tombstone for the wrong tenant", tombstoneDeviceID(tx, "b-"+suffix, "t2"))
		note("tombstone an unknown id", tombstoneDeviceID(tx, "nope-"+suffix, "t1"))
		note("reserve a tombstoned id", reserveDeviceIDs(tx, []*model.Device{d("a", "t1")}))
		rows, err := db.Raw("SELECT id, tenant_id, state, deleted_at IS NOT NULL FROM device_id_reservations WHERE id LIKE ? ORDER BY id", "%-"+suffix).Rows()
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var id, tenant, state string
			var deleted bool
			if err := rows.Scan(&id, &tenant, &state, &deleted); err != nil {
				t.Fatal(err)
			}
			out = append(out, fmt.Sprintf("row %s %s %s deleted=%v", id, tenant, state, deleted))
		}
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
	want := []string{"reserve two: ok", "reserve one of them again: reserved", "same id under another tenant: reserved", "a batch whose second id is taken: reserved",
		"blank id: state", "blank tenant: state", "tombstone: ok", "tombstone again: state", "tombstone for the wrong tenant: state", "tombstone an unknown id: state", "reserve a tombstoned id: reserved"}
	if len(a) != len(want)+2 || !reflect.DeepEqual(a[:len(want)], want) {
		t.Fatalf("the oracle did not behave as expected: %v", a)
	}
}
