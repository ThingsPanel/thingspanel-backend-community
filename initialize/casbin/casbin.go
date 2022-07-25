package casbin

import (
	"fmt"
	"os"
	"strconv"

	"github.com/beego/beego/v2/core/logs"
	beego "github.com/beego/beego/v2/server/web"
	"github.com/casbin/casbin/v2"
	gormadapter "github.com/casbin/gorm-adapter/v3"
)

var CasbinEnforcer *casbin.Enforcer

func init() {
	logs.Info("casbin初始化开始。。。")
	// Initialize a Gorm adapter and use it in a Casbin enforcer:
	// The adapter will use the MySQL database named "casbin".
	// If it doesn't exist, the adapter will create it automatically.
	// You can also use an already existing gorm instance with gormadapter.NewAdapterByDB(gormInstance)
	psqluser, _ := beego.AppConfig.String("psqluser")
	psqlpass, _ := beego.AppConfig.String("psqlpass")
	psqladdr := os.Getenv("TP_PG_IP")
	if psqladdr == "" {
		psqladdr, _ = beego.AppConfig.String("psqladdr")
	}
	psqlports := os.Getenv("TP_PG_PORT")
	var psqlport int
	if psqlports == "" {
		psqlport, _ = beego.AppConfig.Int("psqlport")
	} else {
		psqlport, _ = strconv.Atoi(psqlports)
	}
	psqldb, _ := beego.AppConfig.String("psqldb")
	dataSource := fmt.Sprintf("host=%s port=%d user=%s dbname=%s password=%s sslmode=disable TimeZone=Asia/Shanghai",
		psqladdr,
		psqlport,
		psqluser,
		psqldb,
		psqlpass,
	)
	a, _ := gormadapter.NewAdapter("postgres", dataSource, true)
	e, err := casbin.NewEnforcer("initialize/casbin/model.conf", a)
	// Or you can use an existing DB "abc" like this:
	// The adapter will use the table named "casbin_rule".
	// If it doesn't exist, the adapter will create it automatically.
	// a := gormadapter.NewAdapter("mysql", "mysql_username:mysql_password@tcp(127.0.0.1:3306)/abc", true)
	if err != nil {
		fmt.Println(err.Error())
	}
	CasbinEnforcer = e
	CasbinEnforcer.LoadPolicy()
}
