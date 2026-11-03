package initialize

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func setViper(t *testing.T, values map[string]interface{}) {
	t.Helper()
	for key, value := range values {
		previous := viper.Get(key)
		viper.Set(key, value)
		key := key
		t.Cleanup(func() { viper.Set(key, previous) })
	}
}

func TestPlatformDriverSelectionIsFailClosed(t *testing.T) {
	for value, want := range map[string]string{"": PlatformDriverPostgres, "postgres": PlatformDriverPostgres, "mysql": PlatformDriverMySQL} {
		setViper(t, map[string]interface{}{"db.driver": value})
		if got, err := PlatformDriver(); err != nil || got != want {
			t.Fatalf("db.driver=%q: got %q err=%v", value, got, err)
		}
	}
	for _, bad := range []string{"mysq1", "MySQL", "sqlite", "postgresql", " mysql"} {
		setViper(t, map[string]interface{}{"db.driver": bad})
		if got, err := PlatformDriver(); err == nil || got != "" {
			t.Fatalf("db.driver=%q must be rejected, got %q", bad, got)
		}
	}
}

func TestMysqlConfigRequiresDatabaseAndUser(t *testing.T) {
	setViper(t, map[string]interface{}{"db.mysql.dbname": "", "db.mysql.username": ""})
	if _, err := LoadMysqlConfig(); err == nil {
		t.Fatal("a missing database name and user must be rejected")
	}
	setViper(t, map[string]interface{}{"db.mysql.dbname": "tp", "db.mysql.username": "u", "db.mysql.time_zone": "Not/AZone"})
	config, err := LoadMysqlConfig()
	if err != nil || config.Port != 3306 {
		t.Fatalf("defaults: %+v err=%v", config, err)
	}
	if _, err := MysqlConnect(config); err == nil || !strings.Contains(err.Error(), "time_zone") {
		t.Fatalf("an invalid time zone must be rejected before connecting: %v", err)
	}
}

func TestMysqlPlatformStartsWithARestrictedAccountAndRefusesAPrivilegedOne(t *testing.T) {
	host, port, user, pass, name := os.Getenv("TP_MYSQL_TEST_HOST"), os.Getenv("TP_MYSQL_TEST_PORT"), os.Getenv("TP_MYSQL_TEST_USER"), os.Getenv("TP_MYSQL_TEST_PASSWORD"), os.Getenv("TP_MYSQL_TEST_DB")
	if host == "" || port == "" || user == "" || name == "" {
		t.Skip("TP_MYSQL_TEST_{HOST,PORT,USER,PASSWORD,DB} required")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	// Start-up loads ./configs/casbin.conf relative to the repository root.
	previous, _ := os.Getwd()
	if err := os.Chdir(".."); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	setViper(t, map[string]interface{}{
		"db.driver": "mysql", "db.mysql.host": host, "db.mysql.port": portNumber, "db.mysql.username": user,
		"db.mysql.password": pass, "db.mysql.dbname": name, "db.mysql.time_zone": "UTC",
	})
	config, err := LoadMysqlConfig()
	if err != nil {
		t.Fatal(err)
	}
	db, err := MysqlConnect(config)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	var version, charset string
	if err := db.Raw("SELECT VERSION()").Scan(&version).Error; err != nil || !strings.HasPrefix(version, "8.") {
		t.Fatalf("version=%q err=%v", version, err)
	}
	if err := db.Raw("SELECT @@character_set_connection").Scan(&charset).Error; err != nil || charset != "utf8mb4" {
		t.Fatalf("charset=%q err=%v", charset, err)
	}
	if pool, e := db.DB(); e == nil {
		_ = pool.Close()
	}
	// The privileged test account can create temporary tables, so start-up must refuse it.
	db2, err := PgInit()
	if db2 != nil || !errors.Is(err, ErrMySQLTemporaryTablePrivilege) {
		t.Fatalf("PgInit with a temporary-table privilege: db=%v err=%v", db2 != nil, err)
	}
	// A restricted application account passes the guard, gets the schema applied, and starts.
	admin, err := MysqlConnect(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"CREATE USER IF NOT EXISTS 'tp_app'@'%' IDENTIFIED BY 'app-pass-1'",
		"GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, ALTER, INDEX, DROP, REFERENCES, TRIGGER, CREATE VIEW, SHOW VIEW ON `" + name + "`.* TO 'tp_app'@'%'",
	} {
		if err := admin.Exec(statement).Error; err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	t.Cleanup(func() {
		if pool, e := admin.DB(); e == nil {
			if c, e := pool.Conn(context.Background()); e == nil {
				dropAllMySQLObjects(c)
				_ = c.Close()
			}
		}
		_ = admin.Exec("DROP USER IF EXISTS 'tp_app'@'%'").Error
		if p, e := admin.DB(); e == nil {
			_ = p.Close()
		}
	})
	setViper(t, map[string]interface{}{"db.mysql.username": "tp_app", "db.mysql.password": "app-pass-1"})
	db3, err := PgInit()
	if err != nil || db3 == nil {
		t.Fatalf("PgInit with a restricted account must apply the schema and return the handle: db=%v err=%v", db3 != nil, err)
	}
	var seeded int64
	if err := db3.Raw("SELECT COUNT(*) FROM sys_ui_elements").Scan(&seeded).Error; err != nil || seeded == 0 {
		t.Fatalf("the platform seed rows are missing after start-up: %d %v", seeded, err)
	}
	var modules int64
	if err := db3.Raw("SELECT COUNT(DISTINCT module_name) FROM native_component_migration_ledger").Scan(&modules).Error; err != nil || modules < 8 {
		t.Fatalf("every owner must have recorded its migrations: %d modules, %v", modules, err)
	}
	if pool, e := db3.DB(); e == nil {
		_ = pool.Close()
	}
	// A second start-up finds everything applied and changes nothing.
	again, err := PgInit()
	if err != nil || again == nil {
		t.Fatalf("second start-up: %v", err)
	}
	if pool, e := again.DB(); e == nil {
		_ = pool.Close()
	}
	setViper(t, map[string]interface{}{"db.mysql.username": user, "db.mysql.password": pass})
	// A wrong password must fail at connect, not later.
	setViper(t, map[string]interface{}{"db.mysql.password": pass + "-wrong"})
	if _, err := PgInit(); err == nil {
		t.Fatalf("a wrong password must fail to connect, got %v", err)
	}
}
