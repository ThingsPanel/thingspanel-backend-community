package psql

import (
	"fmt"
	"time"

	adapter "github.com/beego/beego/v2/adapter"
	beego "github.com/beego/beego/v2/server/web"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var Mydb *gorm.DB

var Err error

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
	Mydb, Err = gorm.Open(postgres.Open(dataSource), &gorm.Config{})
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
