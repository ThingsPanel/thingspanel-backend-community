package initialize

import (
	"fmt"
	"log"
	"os"
	"time"

	global "project/pkg/global"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// 数据库配置
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

func PgInit() (*gorm.DB, error) {
	// 初始化配置
	config, err := LoadDbConfig()
	if err != nil {
		logrus.Errorf("加载数据库配置失败: %v", err)
		return nil, err
	}

	// 初始化数据库（添加重试逻辑）
	var db *gorm.DB
	maxRetries := 10
	retryInterval := 6 * time.Second

	for retryCount := 0; retryCount < maxRetries; retryCount++ {
		db, err = PgConnect(config)
		if err == nil {
			break
		}

		logrus.Warnf("连接数据库失败 (尝试 %d/%d): %v", retryCount+1, maxRetries, err)

		if retryCount < maxRetries-1 {
			logrus.Infof("将在 %v 后重试连接...", retryInterval)
			time.Sleep(retryInterval)
		}
	}

	if err != nil {
		logrus.Error("连接数据库失败，已达到最大重试次数:", err)
		return nil, err
	}

	global.DB = db

	// casbin 初始化
	CasbinInit()

	// 检查版本
	err = CheckVersion(db)
	if err != nil {
		return nil, fmt.Errorf("检查数据库版本失败: %w", err)
	}
	if err = ApplyDeviceIdentitySchema(db); err != nil {
		return nil, fmt.Errorf("initialize permanent device identity schema: %w", err)
	}
	if err = ApplyServiceAccessSchema(db); err != nil {
		return nil, fmt.Errorf("initialize service access receipt schema: %w", err)
	}
	if err = ApplyHistoryIntentSchema(db); err != nil {
		return nil, fmt.Errorf("initialize telemetry history intent schema: %w", err)
	}
	if err = ApplyMarketInstallSchema(db); err != nil {
		return nil, fmt.Errorf("initialize market installation schema: %w", err)
	}
	if os.Getenv("TP_PLATFORMCONFIG_OWNER") == "encore" {
		if err = ApplyPlatformConfigSchema(db); err != nil {
			return nil, fmt.Errorf("initialize existing platform dictionary schema: %w", err)
		}
	}
	if err = ApplyNativeBindingSchema(db); err != nil {
		return nil, fmt.Errorf("初始化 native binding schema 失败: %w", err)
	}

	// The automation command guard decides which of two competing commands is
	// the current one. It must exist before any automation rule can fire, so it
	// is applied with the other component schemas rather than lazily.
	if err = ApplyCommandGuardSchema(db); err != nil {
		return nil, fmt.Errorf("初始化自动化命令受理守卫 schema 失败: %w", err)
	}

	if err = ApplyAppPushSchema(db); err != nil {
		return nil, fmt.Errorf("initialize app push schema: %w", err)
	}
	if err = ApplyAuthSchema(db); err != nil {
		return nil, err
	}
	return db, nil
}

// LoadDbConfig 从配置文件加载数据库配置
func LoadDbConfig() (*DbConfig, error) {
	config := &DbConfig{
		Host:          viper.GetString("db.psql.host"),
		Port:          viper.GetInt("db.psql.port"),
		DbName:        viper.GetString("db.psql.dbname"),
		Username:      viper.GetString("db.psql.username"),
		Password:      viper.GetString("db.psql.password"),
		TimeZone:      viper.GetString("db.psql.time_zone"),
		LogLevel:      viper.GetInt("db.psql.log_level"),
		SlowThreshold: viper.GetInt("db.psql.slow_threshold"),
		IdleConns:     viper.GetInt("db.psql.idle_conns"),
		OpenConns:     viper.GetInt("db.psql.open_conns"),
	}

	// 设置默认值
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

	// 检查必要的配置
	if config.DbName == "" || config.Username == "" || config.Password == "" {
		return nil, fmt.Errorf("database configuration is incomplete")
	}

	return config, nil
}

// Writer 重写gorm日志的Writer
// type Writer struct{}

// func (w Writer) Printf(format string, args ...interface{}) {
// 	log.Println(args...)
// }

// PgInit 初始化数据库连接
func PgConnect(config *DbConfig) (*gorm.DB, error) {
	dataSource := fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=disable TimeZone=%s",
		config.Host, config.Port, config.DbName, config.Username, config.Password, config.TimeZone)

	// 根据配置获取 SQL 日志 Writer（支持文件和控制台输出）
	sqlLogWriter := GetSQLLogWriter()

	newLogger := logger.New(
		//Writer{},
		log.New(sqlLogWriter, "\r\n", log.LstdFlags), // 使用配置的日志输出（文件或控制台）
		logger.Config{
			SlowThreshold:             time.Duration(config.SlowThreshold) * time.Millisecond,
			LogLevel:                  logger.LogLevel(config.LogLevel),
			IgnoreRecordNotFoundError: true,
			Colorful:                  true, // 控制台输出时显示颜色，文件输出时会被忽略
		})

	var err error
	db, err := gorm.Open(postgres.Open(dataSource), &gorm.Config{
		Logger:                 newLogger,
		SkipDefaultTransaction: true,
		NamingStrategy: schema.NamingStrategy{
			SingularTable: false, // use singular table name, table for `User` would be `user` with this option enabled
		},
	})
	if err != nil {
		return nil, fmt.Errorf("连接数据库失败: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("获取原生数据库连接失败: %v", err)
	}

	sqlDB.SetMaxIdleConns(config.IdleConns)
	sqlDB.SetMaxOpenConns(config.OpenConns)
	sqlDB.SetConnMaxLifetime(time.Hour)

	log.Println("连接数据库完成...")
	// Component migrations run once, after the historical base schema exists.
	// Connecting to an empty database must not attempt an auth ALTER TABLE.
	return db, nil
}

/*
注意 sql中不要有sys_version表
1. 检查版本表是否存在: 检查数据库版本，如果没有sys_version表，创建sys_version表，插入版本序号0，版本号0.0.0
2. 程序版本低于数据版本: 提示升级
3. 数据版本低于程序版本: 执行sql文件，更新版本号
*/
// 检查版本，在表sys_version中的version字段
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
	// 读取 SQL 脚本文件
	sqlFile, err := os.ReadFile(fileName)
	if err != nil {
		return err
	}
	// 执行 SQL 脚本
	t := db.Exec(string(sqlFile))
	if t.Error != nil {
		return t.Error
	}

	return nil
}
