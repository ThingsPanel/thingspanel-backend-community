package initialize

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational"
)

// MysqlConnect is a test-only connector retained for existing schema fixtures.
// Production startup uses RelationalInit and never exposes this forced dialect.
func MysqlConnect(config *DbConfig) (*gorm.DB, error) {
	return relational.Open(context.Background(), relational.Target{
		Name: "test", Dialect: relational.MySQL,
		Host: config.Host, Port: config.Port, Database: config.DbName,
		Username: config.Username, Password: config.Password, TimeZone: config.TimeZone,
		Pool: relational.PoolOptions{MaxOpenConns: config.OpenConns, MaxIdleConns: config.IdleConns, LogLevel: config.LogLevel},
	})
}

func connectMySQLTestDatabase(t *testing.T, config *DbConfig) *gorm.DB {
	t.Helper()
	db, err := MysqlConnect(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = relational.CloseDB(db) })
	return db
}
