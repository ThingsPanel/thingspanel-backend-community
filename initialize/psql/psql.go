package psql

import (
	"fmt"
	"log"
	"time"

	"gorm.io/gorm/logger"

	"github.com/beego/beego/v2/core/logs"
	"github.com/spf13/viper"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var Mydb *gorm.DB

var Err error

// 重写gorm日志的Writer
type Writer struct {
}

func (w Writer) Printf(format string, args ...interface{}) {
	// log.Infof(format, args...)
	logs.Notice(format, args...)
}

// 设置psql
func init() {
	log.Println("连接数据库...")

	psqladdr := viper.GetString("db.psql.psqladdr")
	psqlport := viper.GetInt("db.psql.psqlport")
	psqluser := viper.GetString("db.psql.psqluser")
	psqlpass := viper.GetString("db.psql.psqlpass")
	psqldb := viper.GetString("db.psql.psqldb")
	psqlMaxConns := viper.GetInt("db.psql.psqlMaxConns")
	psqlMaxOpen := viper.GetInt("db.psql.psqlMaxOpen")
	sqlloglevel := viper.GetInt("db.psql.sqlloglevel")
	dataSource := fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=disable TimeZone=Asia/Shanghai",
		psqladdr,
		psqlport,
		psqldb,
		psqluser,
		psqlpass,
	)

	slow_threshold := viper.GetInt("db.psql.slow_threshold")

	//设置gorm日志规则
	newLogger := logger.New(
		// log.New(os.Stdout, "\r\n", log.LstdFlags), // io writer 单独设置grom日志输出
		Writer{}, // beego日志info输出
		logger.Config{
			SlowThreshold:             time.Duration(slow_threshold) * time.Millisecond, // Slow SQL threshold
			LogLevel:                  logger.LogLevel(sqlloglevel),                     // Log level
			IgnoreRecordNotFoundError: true,                                             // Ignore ErrRecordNotFound error for logger
			Colorful:                  true,                                             // Disable color
		},
	)

	Mydb, Err = gorm.Open(postgres.Open(dataSource), &gorm.Config{
		Logger: newLogger,
	})

	if Err != nil {
		log.Fatalf("连接数据库失败: %v", Err)
	}

	SqlDB, err2 := Mydb.DB()
	if err2 != nil {
		log.Fatalf("连接数据库失败: %v", err2)
	}

	SqlDB.SetMaxIdleConns(psqlMaxConns)
	SqlDB.SetMaxOpenConns(psqlMaxOpen)
	SqlDB.SetConnMaxLifetime(time.Hour)

	log.Println("连接数据库完成...")
	log.Println("初始化数据库...")

	init_db(psqluser, psqlpass, psqldb, psqladdr, psqlport)

	log.Println("初始化数据库完成")
}
