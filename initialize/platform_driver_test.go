package initialize

import (
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

func TestMysqlPlatformStopsAfterProvingConnectivityPostgresUntouched(t *testing.T) {
	host, port, user, pass, name := os.Getenv("TP_MYSQL_TEST_HOST"), os.Getenv("TP_MYSQL_TEST_PORT"), os.Getenv("TP_MYSQL_TEST_USER"), os.Getenv("TP_MYSQL_TEST_PASSWORD"), os.Getenv("TP_MYSQL_TEST_DB")
	if host == "" || port == "" || user == "" || name == "" {
		t.Skip("TP_MYSQL_TEST_{HOST,PORT,USER,PASSWORD,DB} required")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
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
	// A restricted application account passes the guard and reaches the explicit schema stop.
	admin, err := MysqlConnect(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"CREATE USER IF NOT EXISTS 'tp_app'@'%' IDENTIFIED BY 'app-pass-1'",
		"GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, ALTER, INDEX, DROP, REFERENCES, TRIGGER ON `" + name + "`.* TO 'tp_app'@'%'",
	} {
		if err := admin.Exec(statement).Error; err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	t.Cleanup(func() {
		_ = admin.Exec("DROP USER IF EXISTS 'tp_app'@'%'").Error
		if p, e := admin.DB(); e == nil {
			_ = p.Close()
		}
	})
	setViper(t, map[string]interface{}{"db.mysql.username": "tp_app", "db.mysql.password": "app-pass-1"})
	db3, err := PgInit()
	if db3 != nil || !errors.Is(err, ErrPlatformMySQLSchemaMissing) {
		t.Fatalf("PgInit with a restricted account: db=%v err=%v", db3 != nil, err)
	}
	setViper(t, map[string]interface{}{"db.mysql.username": user, "db.mysql.password": pass})
	// A wrong password must fail at connect, not later.
	setViper(t, map[string]interface{}{"db.mysql.password": pass + "-wrong"})
	if _, err := PgInit(); err == nil || errors.Is(err, ErrPlatformMySQLSchemaMissing) {
		t.Fatalf("a wrong password must fail to connect, got %v", err)
	}
}
