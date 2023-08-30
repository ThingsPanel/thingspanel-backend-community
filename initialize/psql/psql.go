package psql

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"gorm.io/gorm/logger"

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
	logs.Notice(format, args...)
}

// 设置psql
func init() {
	log.Println("连接数据库...")
	psqladdr := os.Getenv("TP_PG_IP")
	var err error
	if psqladdr == "" {
		psqladdr, err = beego.AppConfig.String("psqladdr")
		if err != nil {
			log.Fatalf("无法获取psqladdr: %v", err)
		}
	}

	psqlportStr := os.Getenv("TP_PG_PORT")
	psqlport, err := strconv.Atoi(psqlportStr)
	if err != nil || psqlport == 0 {
		psqlport, err = beego.AppConfig.Int("psqlport")
		if err != nil {
			log.Fatalf("无法获取psqlport: %v", err)
		}
	}

	psqluser, err := beego.AppConfig.String("psqluser")
	if err != nil {
		log.Fatalf("无法获取psqluser: %v", err)
	}

	psqlpass, err := beego.AppConfig.String("psqlpass")
	if err != nil {
		log.Fatalf("无法获取psqlpass: %v", err)
	}

	psqldb, err := beego.AppConfig.String("psqldb")
	if err != nil {
		log.Fatalf("无法获取psqldb: %v", err)
	}

	psqlMaxConns, err := beego.AppConfig.Int("psqlMaxConns")
	if err != nil {
		log.Fatalf("无法获取psqlMaxConns: %v", err)
	}

	psqlMaxOpen, err := beego.AppConfig.Int("psqlMaxOpen")
	if err != nil {
		log.Fatalf("无法获取psqlMaxOpen: %v", err)
	}

	sqlloglevel, err := beego.AppConfig.Int("sqlloglevel")
	if err != nil {
		log.Fatalf("无法获取sqlloglevel: %v", err)
	}

	dataSource := fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=disable TimeZone=Asia/Shanghai",
		psqladdr,
		psqlport,
		psqldb,
		psqluser,
		psqlpass,
	)
	slow_threshold, err := beego.AppConfig.Int("slow_threshold")
	if err != nil {
		log.Fatalf("无法获取slow_threshold: %v", err)
	}
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
