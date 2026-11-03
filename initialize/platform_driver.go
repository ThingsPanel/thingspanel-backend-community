package initialize

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/spf13/viper"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

const (
	PlatformDriverPostgres = "postgres"
	PlatformDriverMySQL    = "mysql"
)

// ErrPlatformMySQLSchemaMissing is returned once a MySQL platform database is reachable:
// the platform's base schema and queries are still PostgreSQL-only, so start-up must stop
// rather than run against a database it cannot use.
var ErrPlatformMySQLSchemaMissing = errors.New("platform MySQL is selected and reachable, but the platform schema and queries do not support MySQL yet")

// PlatformDriver returns the configured platform database driver (db.driver). Empty means
// PostgreSQL; any other unknown value is an error so a typo never selects a database.
func PlatformDriver() (string, error) {
	switch driver := viper.GetString("db.driver"); driver {
	case "", PlatformDriverPostgres:
		return PlatformDriverPostgres, nil
	case PlatformDriverMySQL:
		return PlatformDriverMySQL, nil
	default:
		return "", fmt.Errorf("unsupported db.driver %q", driver)
	}
}

// LoadMysqlConfig reads db.mysql.* with the same shape as db.psql.*.
func LoadMysqlConfig() (*DbConfig, error) {
	config := &DbConfig{
		Host:      viper.GetString("db.mysql.host"),
		Port:      viper.GetInt("db.mysql.port"),
		DbName:    viper.GetString("db.mysql.dbname"),
		Username:  viper.GetString("db.mysql.username"),
		Password:  viper.GetString("db.mysql.password"),
		TimeZone:  viper.GetString("db.mysql.time_zone"),
		LogLevel:  viper.GetInt("db.mysql.log_level"),
		IdleConns: viper.GetInt("db.mysql.idle_conns"),
		OpenConns: viper.GetInt("db.mysql.open_conns"),
	}
	if config.Host == "" {
		config.Host = "localhost"
	}
	if config.Port == 0 {
		config.Port = 3306
	}
	if config.TimeZone == "" {
		config.TimeZone = "Asia/Shanghai"
	}
	if config.LogLevel == 0 {
		config.LogLevel = 1
	}
	if config.IdleConns == 0 {
		config.IdleConns = 10
	}
	if config.OpenConns == 0 {
		config.OpenConns = 50
	}
	if config.DbName == "" || config.Username == "" {
		return nil, errors.New("db.mysql.dbname and db.mysql.username are required")
	}
	return config, nil
}

// MysqlConnect opens a MySQL platform connection. The DSN is built from a structured config,
// never by concatenating user input, and the connection is verified before it is returned.
func MysqlConnect(config *DbConfig) (*gorm.DB, error) {
	location, err := time.LoadLocation(config.TimeZone)
	if err != nil {
		return nil, fmt.Errorf("invalid db.mysql.time_zone: %w", err)
	}
	cfg := mysqldriver.NewConfig()
	cfg.User, cfg.Passwd = config.Username, config.Password
	cfg.Net, cfg.Addr = "tcp", net.JoinHostPort(config.Host, strconv.Itoa(config.Port))
	cfg.DBName = config.DbName
	cfg.ParseTime, cfg.Loc = true, location
	cfg.Params = map[string]string{"charset": "utf8mb4"}
	cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = 10*time.Second, 30*time.Second, 30*time.Second
	db, err := gorm.Open(gormmysql.Open(cfg.FormatDSN()), &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.LogLevel(config.LogLevel)),
		SkipDefaultTransaction: true,
		NamingStrategy:         schema.NamingStrategy{SingularTable: false},
	})
	if err != nil {
		return nil, fmt.Errorf("connect MySQL platform database: %w", err)
	}
	pool, err := db.DB()
	if err != nil {
		return nil, err
	}
	pool.SetMaxIdleConns(config.IdleConns)
	pool.SetMaxOpenConns(config.OpenConns)
	pool.SetConnMaxLifetime(time.Hour)
	if err := pool.Ping(); err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("ping MySQL platform database: %w", err)
	}
	return db, nil
}

// mysqlPlatformInit proves the MySQL database is usable as a connection and then stops with
// ErrPlatformMySQLSchemaMissing. It never sets global.DB.
func mysqlPlatformInit() (*gorm.DB, error) {
	config, err := LoadMysqlConfig()
	if err != nil {
		return nil, err
	}
	db, err := MysqlConnect(config)
	if err != nil {
		return nil, err
	}
	defer func() {
		if pool, e := db.DB(); e == nil {
			_ = pool.Close()
		}
	}()
	if err := RequireNoTemporaryTablePrivilege(db); err != nil {
		return nil, err
	}
	return nil, ErrPlatformMySQLSchemaMissing
}
