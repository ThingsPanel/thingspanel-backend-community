package initialize

import (
	"testing"

	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational"
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

func TestPlatformRelationalTargetIsSelectedOnlyByNewDriverKey(t *testing.T) {
	values := map[string]string{
		"relational.driver":                  "mysql",
		"relational.mysql.platform.database": "tp_test",
		"relational.mysql.platform.username": "app",
		"relational.mysql.platform.password": "secret",
		"db.driver":                          "postgres", // retired keys do not override the selected driver
	}
	target, err := relational.ResolveTarget(relational.MapSource(values), "platform")
	if err != nil || target.Dialect != relational.MySQL || target.Database != "tp_test" {
		t.Fatalf("target=%+v err=%v", target, err)
	}
	values["relational.driver"] = "mysqlish"
	if _, err := relational.ResolveTarget(relational.MapSource(values), "platform"); err == nil {
		t.Fatal("unknown relational.driver must fail closed")
	}
}

func TestLoadDbConfigUsesOnlyRelationalPostgresKeys(t *testing.T) {
	setViper(t, map[string]interface{}{
		"db.psql.dbname":                        "legacy",
		"db.psql.username":                      "legacy-user",
		"relational.postgres.platform.database": "platform_db",
		"relational.postgres.platform.username": "platform-user",
		"relational.postgres.platform.password": "test-password",
	})
	got, err := LoadDbConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.DbName != "platform_db" || got.Username != "platform-user" || got.Port != 5432 {
		t.Fatalf("config=%+v", got)
	}
	setViper(t, map[string]interface{}{"relational.postgres.platform.database": "", "relational.postgres.platform.username": ""})
	if _, err := LoadDbConfig(); err == nil {
		t.Fatal("retired db.psql.* values must not act as compatibility fallback")
	}
}

func TestPostgresOnlyHelpersRejectMySQLDeployment(t *testing.T) {
	setViper(t, map[string]interface{}{
		"relational.driver":                     "mysql",
		"relational.postgres.platform.database": "postgres_still_configured",
		"relational.postgres.platform.username": "postgres_user",
		"relational.postgres.platform.password": "test-password",
	})
	if _, err := LoadDbConfig(); err == nil {
		t.Fatal("PostgreSQL-only utility must not ignore relational.driver=mysql")
	}
	if _, err := PgConnect(&DbConfig{DbName: "configured", Username: "u", Password: "p"}); err == nil {
		t.Fatal("PostgreSQL-only connection helper must not bypass the deployment driver")
	}
}
