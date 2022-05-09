package psql

import (
	"fmt"
	"time"

	"gorm.io/gorm/logger"

	adapter "github.com/beego/beego/v2/adapter"
	"github.com/beego/beego/v2/core/logs"
	beego "github.com/beego/beego/v2/server/web"
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
	logs.Info(format, args...)
}

// 设置psql
func init() {
	psqluser, _ := beego.AppConfig.String("psqluser")
	psqlpass, _ := beego.AppConfig.String("psqlpass")
	psqladdr, _ := beego.AppConfig.String("psqladdr")
	psqlport, _ := beego.AppConfig.Int("psqlport")
	psqldb, _ := beego.AppConfig.String("psqldb")
	psqlMaxConns, _ := beego.AppConfig.Int("psqlMaxConns")
	psqlMaxOpen, _ := beego.AppConfig.Int("psqlMaxOpen")
	dataSource := fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=disable TimeZone=Asia/Shanghai",
		psqladdr,
		psqlport,
		psqldb,
		psqluser,
		psqlpass,
	)
	//设置gorm日志规则
	newLogger := logger.New(
		Writer{},
		logger.Config{
			SlowThreshold:             200 * time.Millisecond, // Slow SQL threshold
			LogLevel:                  logger.Info,            // Log level
			IgnoreRecordNotFoundError: true,                   // Ignore ErrRecordNotFound error for logger
			Colorful:                  false,                  // Disable color
		},
	)
	Mydb, Err = gorm.Open(postgres.Open(dataSource), &gorm.Config{
		// Logger: logger.Default.LogMode(logger.Info),
		Logger: newLogger,
	})
	if Err != nil {
		adapter.Error("psql database error:", Err)
	}
	SqlDB, err2 := Mydb.DB()
	if err2 != nil {
		adapter.Error("psql database error:", err2)
	}
	SqlDB.SetMaxIdleConns(psqlMaxConns)
	SqlDB.SetMaxOpenConns(psqlMaxOpen)
	SqlDB.SetConnMaxLifetime(time.Hour)
}
