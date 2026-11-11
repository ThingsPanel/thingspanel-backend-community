package initialize

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/spf13/viper"
	"gorm.io/gorm"

	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational"
	global "project/pkg/global"
)

// DbConfig is retained for the standalone PostgreSQL-backed maintenance tools
// that still call LoadDbConfig/PgConnect. Runtime platform startup uses
// RelationalInit and resolves relational.driver exactly once.
type DbConfig struct {
	Host          string
	Port          int
	DbName        string
	Username      string
	Password      string
	TimeZone      string
	LogLevel      int
	SlowThreshold int
	IdleConns     int
	OpenConns     int
}

// LoadDbConfig reads the new PostgreSQL platform resource keys. It deliberately
// does not fall back to the retired db.psql.* keys.
func LoadDbConfig() (*DbConfig, error) {
	dialect, err := relational.ResolveDialect(func(key string) string { return viper.GetString(key) })
	if err != nil {
		return nil, err
	}
	if dialect != relational.Postgres {
		return nil, fmt.Errorf("PostgreSQL-only database configuration requested while relational.driver=%s", dialect)
	}
	config := &DbConfig{
		Host:          viper.GetString("relational.postgres.platform.host"),
		Port:          viper.GetInt("relational.postgres.platform.port"),
		DbName:        viper.GetString("relational.postgres.platform.database"),
		Username:      viper.GetString("relational.postgres.platform.username"),
		Password:      viper.GetString("relational.postgres.platform.password"),
		TimeZone:      viper.GetString("relational.postgres.platform.time_zone"),
		LogLevel:      viper.GetInt("relational.postgres.platform.log_level"),
		SlowThreshold: viper.GetInt("relational.postgres.platform.slow_threshold_ms"),
		IdleConns:     viper.GetInt("relational.postgres.platform.idle_conns"),
		OpenConns:     viper.GetInt("relational.postgres.platform.open_conns"),
	}
	if config.Host == "" {
		config.Host = "localhost"
	}
	if config.Port == 0 {
		config.Port = 5432
	}
	if config.TimeZone == "" {
		config.TimeZone = "Asia/Shanghai"
	}
	if config.LogLevel == 0 {
		config.LogLevel = 1
	}
	if config.SlowThreshold == 0 {
		config.SlowThreshold = 200
	}
	if config.IdleConns == 0 {
		config.IdleConns = 10
	}
	if config.OpenConns == 0 {
		config.OpenConns = 50
	}
	if config.DbName == "" || config.Username == "" || config.Password == "" {
		return nil, fmt.Errorf("relational.postgres.platform database configuration is incomplete")
	}
	return config, nil
}

// PgConnect is used by standalone PostgreSQL maintenance commands and tests.
// The main platform process must use RelationalInit so the selected driver is
// resolved from relational.driver.
func PgConnect(config *DbConfig) (*gorm.DB, error) {
	if config == nil {
		return nil, fmt.Errorf("PostgreSQL configuration is nil")
	}
	dialect, err := relational.ResolveDialect(func(key string) string { return viper.GetString(key) })
	if err != nil {
		return nil, err
	}
	if dialect != relational.Postgres {
		return nil, fmt.Errorf("PostgreSQL-only connection requested while relational.driver=%s", dialect)
	}
	target := relational.Target{
		Name: "platform", Dialect: relational.Postgres,
		Host: config.Host, Port: config.Port, Database: config.DbName,
		Username: config.Username, Password: config.Password, TimeZone: config.TimeZone,
		Pool: relational.PoolOptions{
			MaxOpenConns: config.OpenConns, MaxIdleConns: config.IdleConns,
			LogLevel: config.LogLevel, SlowThreshold: time.Duration(config.SlowThreshold) * time.Millisecond,
		},
	}
	db, err := relational.Open(context.Background(), target)
	if err != nil {
		return nil, fmt.Errorf("connect PostgreSQL platform database: %w", err)
	}
	log.Println("连接数据库完成...")
	return db, nil
}

// PgInit remains as a source-level alias for maintenance code. The application
// entrypoint is wired to RelationalInit and no legacy config keys are read.
func PgInit() (*gorm.DB, error) { return RelationalInit() }

// CheckVersion applies the historical PostgreSQL migrations in place. These
// files and their execution order remain unchanged by T1-A1.
func CheckVersion(db *gorm.DB) error {
	return applyLegacySchemaMigrations(db, global.VERSION_NUMBER, global.VERSION, func(number int) ([]byte, error) {
		fileName := fmt.Sprintf("sql/%d.sql", number)
		contents, err := os.ReadFile(fileName)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", fileName, err)
		}
		return contents, nil
	})
}

func ExecuteSQLFile(db *gorm.DB, fileName string) error {
	contents, err := os.ReadFile(fileName)
	if err != nil {
		return err
	}
	return db.Exec(string(contents)).Error
}
