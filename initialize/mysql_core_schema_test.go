package initialize

import (
	"encoding/json"
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

	componentsql "project/sql"

	"project/internal/model"
)

func openCoreDialects(t *testing.T) (pg, my *gorm.DB) {
	t.Helper()
	pgDSN := os.Getenv("TEST_PG_SCHEMA_DSN")
	host, port, user, pass, name := os.Getenv("TP_MYSQL_TEST_HOST"), os.Getenv("TP_MYSQL_TEST_PORT"), os.Getenv("TP_MYSQL_TEST_USER"), os.Getenv("TP_MYSQL_TEST_PASSWORD"), os.Getenv("TP_MYSQL_TEST_DB")
	if pgDSN == "" || host == "" || port == "" {
		t.Skip("TEST_PG_SCHEMA_DSN (a database built by sql/1.sql..32.sql) and TP_MYSQL_TEST_* required")
	}
	pg, err := gorm.Open(postgres.Open(pgDSN), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	// The oracle database carries the device-identity trigger that requires a tenant reservation
	// (a PostgreSQL-only component guard with no MySQL equivalent yet). Bypass it for this
	// session on a single connection so the base-table behaviour is what is compared.
	pgPool, err := pg.DB()
	if err != nil {
		t.Fatal(err)
	}
	pgPool.SetMaxOpenConns(1)
	if err := pg.Exec("SET session_replication_role = replica").Error; err != nil {
		t.Fatalf("cannot bypass component triggers on the oracle: %v", err)
	}
	portNumber, _ := strconv.Atoi(port)
	my, err = MysqlConnect(&DbConfig{Host: host, Port: portNumber, Username: user, Password: pass, DbName: name, TimeZone: "UTC", LogLevel: 1, IdleConns: 2, OpenConns: 5})
	if err != nil {
		t.Fatal(err)
	}
	statements, err := componentsql.MySQLMigrationStatements("001_core_users_devices.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range statements {
		if err := my.Exec(statement).Error; err != nil {
			t.Fatalf("apply MySQL schema: %v", err)
		}
	}
	t.Cleanup(func() {
		_ = my.Exec("DROP TABLE IF EXISTS devices, users").Error
		if p, e := my.DB(); e == nil {
			_ = p.Close()
		}
		if p, e := pg.DB(); e == nil {
			_ = p.Close()
		}
	})
	return pg, my
}

func str(v string) *string { return &v }

func sameJSON(t *testing.T, a, b *string) bool {
	t.Helper()
	if a == nil || b == nil {
		return a == b
	}
	var x, y interface{}
	if err := json.Unmarshal([]byte(*a), &x); err != nil {
		t.Fatalf("invalid json %q", *a)
	}
	if err := json.Unmarshal([]byte(*b), &y); err != nil {
		t.Fatalf("invalid json %q", *b)
	}
	return reflect.DeepEqual(x, y)
}

func sameInstant(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

func TestMysqlCoreSchemaMatchesPostgresBehaviour(t *testing.T) {
	pg, my := openCoreDialects(t)
	tenant := "tenant-" + uuid.NewString()[:8]
	moment := time.Date(2026, 11, 3, 1, 2, 3, 123456000, time.UTC)
	later := moment.Add(90 * time.Minute)
	suffix := uuid.NewString()[:8]
	for _, db := range []*gorm.DB{pg, my} {
		db := db
		t.Cleanup(func() {
			_ = db.Exec("DELETE FROM devices WHERE tenant_id = ?", tenant).Error
			_ = db.Exec("DELETE FROM users WHERE tenant_id = ?", tenant).Error
		})
	}
	type result struct {
		users   []model.User
		devices []model.Device
		count   int64
		errs    []bool
	}
	run := func(db *gorm.DB) result {
		var r result
		fail := func(err error) { r.errs = append(r.errs, err != nil) }
		users := []model.User{
			{ID: "u1-" + suffix, Name: str("Alice"), PhoneNumber: "100", Email: "A-" + suffix + "@x.com", Status: str("N"), Authority: str("TENANT_ADMIN"), Password: "p", TenantID: &tenant,
				AdditionalInfo: str(`{"theme": "dark", "n": [1, 2]}`), CreatedAt: &moment, UpdatedAt: &later},
			{ID: "u2-" + suffix, PhoneNumber: "101", Email: "a-" + suffix + "@x.com", Password: "p", TenantID: &tenant, CreatedAt: &moment, UpdatedAt: &moment}, // differs only by case
		}
		for i := range users {
			fail(db.Create(&users[i]).Error)
		}
		dup := model.User{ID: "u3-" + suffix, PhoneNumber: "1", Email: "A-" + suffix + "@x.com", Password: "p", TenantID: &tenant}
		fail(db.Create(&dup).Error) // identical email: rejected

		longA := strings.Repeat("v", 2000) + "A"
		longB := strings.Repeat("v", 2000) + "B"
		devices := []model.Device{
			{ID: "d1-" + suffix, Name: str("Boiler Room"), Voucher: longA, TenantID: tenant, IsEnabled: "enabled", ActivateFlag: "active", DeviceNumber: "DN-" + suffix, CreatedAt: &moment,
				AdditionalInfo: str(`{"k":  "v"}`), IsOnline: 1, ActivateAt: &later},
			{ID: "d2-" + suffix, Name: str("boiler spare"), Voucher: longB, TenantID: tenant, IsEnabled: "enabled", ActivateFlag: "active", DeviceNumber: "dn-" + suffix, CreatedAt: &later}, // number differs by case
			{ID: "d3-" + suffix, Voucher: "short-" + suffix, TenantID: tenant, DeviceNumber: "other-" + suffix},                                                                              // all defaults
		}
		for i := range devices {
			fail(db.Create(&devices[i]).Error)
		}
		fail(db.Create(&model.Device{ID: "d4-" + suffix, Voucher: longA, TenantID: tenant, DeviceNumber: "unique-" + suffix}).Error)      // duplicate voucher
		fail(db.Create(&model.Device{ID: "d5-" + suffix, Voucher: "v5-" + suffix, TenantID: tenant, DeviceNumber: "DN-" + suffix}).Error) // duplicate number

		fail(db.Model(&model.Device{}).Where("id = ?", "d2-"+suffix).Updates(map[string]interface{}{"is_online": 1, "label": "hot", "update_at": later}).Error)
		fail(db.Where("id = ?", "d3-"+suffix).Delete(&model.Device{}).Error)

		tx := db.Begin()
		fail(tx.Create(&model.Device{ID: "d6-" + suffix, Voucher: "v6-" + suffix, TenantID: tenant, DeviceNumber: "rolled-" + suffix}).Error)
		fail(tx.Rollback().Error)

		fail(db.Where("tenant_id = ?", tenant).Order("id").Find(&r.users).Error)
		fail(db.Where("tenant_id = ? AND activate_flag = ?", tenant, "active").Order("created_at DESC, id").Limit(10).Find(&r.devices).Error)
		fail(db.Model(&model.Device{}).Where("tenant_id = ?", tenant).Count(&r.count).Error)
		return r
	}
	a, b := run(pg), run(my)

	if !reflect.DeepEqual(a.errs, b.errs) {
		t.Fatalf("success/failure pattern differs:\npg=%v\nmy=%v", a.errs, b.errs)
	}
	rejected := 0
	for _, failed := range a.errs {
		if failed {
			rejected++
		}
	}
	if rejected != 3 {
		t.Fatalf("expected exactly three rejections (email, voucher, device number), got %d: %v", rejected, a.errs)
	}
	if a.count != b.count || a.count != 2 {
		t.Fatalf("device count pg=%d my=%d, want 2", a.count, b.count)
	}
	if len(a.users) != 2 || len(b.users) != 2 {
		t.Fatalf("users pg=%d my=%d, want 2 (emails that differ by case are distinct in both)", len(a.users), len(b.users))
	}
	for i := range a.users {
		pu, mu := a.users[i], b.users[i]
		if pu.ID != mu.ID || pu.Email != mu.Email || !reflect.DeepEqual(pu.Name, mu.Name) || !reflect.DeepEqual(pu.Status, mu.Status) ||
			!sameInstant(pu.CreatedAt, mu.CreatedAt) || !sameInstant(pu.UpdatedAt, mu.UpdatedAt) || !sameJSON(t, pu.AdditionalInfo, mu.AdditionalInfo) {
			t.Fatalf("user %d differs:\npg=%+v\nmy=%+v", i, pu, mu)
		}
		// gorm inserts an explicit NULL for a nil pointer, so the column default is not applied on
		// either database; what matters is that both agree.
		if !reflect.DeepEqual(pu.PasswordFailCount, mu.PasswordFailCount) {
			t.Fatalf("password_fail_count differs: pg=%v my=%v", pu.PasswordFailCount, mu.PasswordFailCount)
		}
	}
	if len(a.devices) != 2 || len(b.devices) != 2 {
		t.Fatalf("devices pg=%d my=%d, want 2", len(a.devices), len(b.devices))
	}
	for i := range a.devices {
		pd, md := a.devices[i], b.devices[i]
		if pd.ID != md.ID || pd.Voucher != md.Voucher || pd.DeviceNumber != md.DeviceNumber || pd.IsOnline != md.IsOnline || pd.IsEnabled != md.IsEnabled ||
			!reflect.DeepEqual(pd.Name, md.Name) || !reflect.DeepEqual(pd.Label, md.Label) ||
			!sameInstant(pd.CreatedAt, md.CreatedAt) || !sameInstant(pd.UpdateAt, md.UpdateAt) || !sameInstant(pd.ActivateAt, md.ActivateAt) ||
			!sameJSON(t, pd.AdditionalInfo, md.AdditionalInfo) || !sameJSON(t, pd.ProtocolConfig, md.ProtocolConfig) {
			t.Fatalf("device %d differs:\npg=%+v\nmy=%+v", i, pd, md)
		}
	}
	if a.devices[0].CreatedAt == nil || a.devices[0].CreatedAt.Nanosecond() != 123456000 && a.devices[0].CreatedAt.Nanosecond() != 0 {
		t.Fatalf("test data lost its microsecond precision: %v", a.devices[0].CreatedAt)
	}
	if a.devices[1].ProtocolConfig == nil || *a.devices[1].ProtocolConfig == "" {
		t.Fatal("the JSON default must be materialised on both databases")
	}
}

// Every MySQL table defaults to utf8mb4_bin, so LIKE is case-sensitive exactly as in PostgreSQL. Code that wants
// a case-insensitive match (ILIKE sites) says so with LOWER(col) LIKE LOWER(?).
func TestMysqlCoreSchemaLikeIsCaseSensitiveLikePostgres(t *testing.T) {
	pg, my := openCoreDialects(t)
	tenant := "tenant-" + uuid.NewString()[:8]
	suffix := uuid.NewString()[:8]
	for _, db := range []*gorm.DB{pg, my} {
		db := db
		if err := db.Create(&model.Device{ID: "l1-" + suffix, Name: str("Boiler"), Voucher: "lv-" + suffix, TenantID: tenant, DeviceNumber: "ln-" + suffix}).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Exec("DELETE FROM devices WHERE tenant_id = ?", tenant).Error })
	}
	count := func(db *gorm.DB, where string, arg string) int64 {
		var n int64
		if err := db.Model(&model.Device{}).Where("tenant_id = ?", tenant).Where(where, arg).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	if pgN, myN := count(pg, "name LIKE ?", "boiler%"), count(my, "name LIKE ?", "boiler%"); pgN != 0 || myN != 0 {
		t.Fatalf("plain LIKE must be case-sensitive on both: pg=%d my=%d", pgN, myN)
	}
	if pgN, myN := count(pg, "LOWER(name) LIKE LOWER(?)", "BOILER%"), count(my, "LOWER(name) LIKE LOWER(?)", "BOILER%"); pgN != 1 || myN != 1 {
		t.Fatalf("LOWER(col) LIKE LOWER(?) must behave the same: pg=%d my=%d", pgN, myN)
	}
	_ = fmt.Sprint
}
