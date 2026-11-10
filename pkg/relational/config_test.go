package relational

import (
	"strings"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
)

func TestEnvName(t *testing.T) {
	if got := EnvName("relational.mysql.device_metadata.dsn"); got != "TP_RELATIONAL_MYSQL_DEVICE_METADATA_DSN" {
		t.Fatal(got)
	}
	if got := EnvName("relational.driver"); got != "TP_RELATIONAL_DRIVER" {
		t.Fatal(got)
	}
}

func TestResolveTargetDefaultsToPostgres(t *testing.T) {
	src := MapSource(map[string]string{
		"relational.postgres.host": "db", "relational.postgres.username": "u", "relational.postgres.platform.database": "tp",
	})
	got, err := ResolveTarget(src, "platform")
	if err != nil {
		t.Fatal(err)
	}
	if got.Dialect != Postgres || got.Host != "db" || got.Port != 5432 || got.Database != "tp" || got.TimeZone != "Asia/Shanghai" {
		t.Fatalf("%+v", got)
	}
}

func TestResolveTargetResourceKeyWinsOverShared(t *testing.T) {
	src := MapSource(map[string]string{
		"relational.driver":                  "mysql",
		"relational.mysql.username":          "shared",
		"relational.mysql.auto.username":     "own",
		"relational.mysql.auto.database":     "auto",
		"relational.mysql.auto.open_conns":   "7",
		"relational.mysql.conn_max_lifetime": "30m",
		"relational.mysql.slow_threshold_ms": "250",
	})
	got, err := ResolveTarget(src, "auto")
	if err != nil {
		t.Fatal(err)
	}
	if got.Dialect != MySQL || got.Username != "own" || got.Port != 3306 || got.Pool.MaxOpenConns != 7 ||
		got.Flavor != FlavorMySQL || got.Pool.ConnMaxLifetime != 30*time.Minute || got.Pool.SlowThreshold != 250*time.Millisecond {
		t.Fatalf("%+v", got)
	}
}

func TestResolveTargetMariaDBFlavor(t *testing.T) {
	src := MapSource(map[string]string{
		"relational.driver": "mysql", "relational.mysql.flavor": "mariadb",
		"relational.mysql.platform.database": "tp", "relational.mysql.platform.username": "app",
	})
	got, err := ResolveTarget(src, "platform")
	if err != nil {
		t.Fatal(err)
	}
	if got.Dialect != MySQL || got.Flavor != FlavorMariaDB || got.Port != 3306 {
		t.Fatalf("%+v", got)
	}
}

func TestResolveTargetErrors(t *testing.T) {
	cases := map[string]map[string]string{
		"unknown driver": {"relational.driver": "oracle"},
		"no dsn, no db":  {"relational.driver": "mysql"},
		"bad number":     {"relational.driver": "mysql", "relational.mysql.dsn": "x", "relational.mysql.port": "abc"},
		"bad duration":   {"relational.driver": "mysql", "relational.mysql.database": "d", "relational.mysql.username": "u", "relational.mysql.conn_max_lifetime": "soon"},
		"bad flavor":     {"relational.driver": "mysql", "relational.mysql.flavor": "mysql8", "relational.mysql.database": "d", "relational.mysql.username": "u"},
	}
	for name, m := range cases {
		if _, err := ResolveTarget(MapSource(m), "r"); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestMySQLDSNIsStructuredAndForcesRowCounting(t *testing.T) {
	tg := Target{Name: "platform", Dialect: MySQL, Host: "h", Port: 3307, Database: "d", Username: "u@x", Password: "p:/@w", TimeZone: "UTC"}
	dsn, err := tg.dsn()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.User != "u@x" || cfg.Passwd != "p:/@w" || cfg.Addr != "h:3307" || cfg.DBName != "d" || !cfg.ParseTime || !cfg.ClientFoundRows {
		t.Fatalf("%+v", cfg)
	}
	if cfg.Params["charset"] != "utf8mb4" {
		t.Fatalf("params %v", cfg.Params)
	}
}

func TestMySQLExplicitDSNGetsRowCountingForced(t *testing.T) {
	tg := Target{Name: "r", Dialect: MySQL, DSN: "u:p@tcp(h:1)/d"}
	dsn, err := tg.dsn()
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := mysqldriver.ParseDSN(dsn)
	if !cfg.ParseTime || !cfg.ClientFoundRows {
		t.Fatalf("%+v", cfg)
	}
}

func TestInvalidMySQLDSNErrorDoesNotEchoTheDSN(t *testing.T) {
	_, err := Target{Name: "r", Dialect: MySQL, DSN: "user:secret@@@(((bad"}.dsn()
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("%v", err)
	}
}

func TestPostgresDSNEscapesCredentials(t *testing.T) {
	dsn, err := Target{Name: "r", Dialect: Postgres, Host: "h", Port: 5432, Database: "d", Username: "u s", Password: "p@:/?#", TimeZone: "UTC"}.dsn()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(dsn, "postgres://u%20s:p%40%3A%2F%3F%23@h:5432/d?") || !strings.Contains(dsn, "TimeZone=UTC") {
		t.Fatal(dsn)
	}
}

func TestDefaultPool(t *testing.T) {
	p := DefaultPool(PoolOptions{MaxOpenConns: 3, MaxIdleConns: 9})
	if p.MaxOpenConns != 3 || p.MaxIdleConns != 3 || p.ConnMaxLifetime != time.Hour || p.LogLevel != 2 {
		t.Fatalf("%+v", p)
	}
}

func TestValidate(t *testing.T) {
	if (Target{Dialect: MySQL, DSN: "x"}).Validate() == nil {
		t.Error("nameless target accepted")
	}
	if (Target{Name: "r", DSN: "x"}).Validate() == nil {
		t.Error("dialect-less target accepted")
	}
}
