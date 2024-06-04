package initialize

import (
	"fmt"
	"log"

	global "project/global"

	"github.com/casbin/casbin/v2"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/spf13/viper"
)

func CasbinInit() {
	log.Println("casbin启动...")
	// Initialize a Gorm adapter and use it in a Casbin enforcer:
	// The adapter will use the MySQL database named "casbin".
	// If it doesn't exist, the adapter will create it automatically.
	// You can also use an already existing gorm instance with gormadapter.NewAdapterByDB(gormInstance)

	a, _ := gormadapter.NewAdapterByDB(global.DB)
	e, err := casbin.NewEnforcer("./configs/casbin.conf", a)
	// Or you can use an existing DB "abc" like this:
	// The adapter will use the table named "casbin_rule".
	// If it doesn't exist, the adapter will create it automatically.
	// a := gormadapter.NewAdapter("mysql", "mysql_username:mysql_password@tcp(127.0.0.1:3306)/abc", true)
	if err != nil {
		fmt.Println(err.Error())
	}
	e.LoadPolicy()
	global.CasbinEnforcer = e
	log.Println("casbin启动完成")
	global.OtaAddress = viper.GetString("ota.download_address")
}
