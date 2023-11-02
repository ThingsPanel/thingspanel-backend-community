package casbin

import (
	_ "ThingsPanel-Go/initialize/psql"
	"fmt"
	"log"

	"github.com/casbin/casbin/v2"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/spf13/viper"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var CasbinEnforcer *casbin.Enforcer

func init() {
	log.Println("casbin启动...")
	psqladdr := viper.GetString("db.psql.psqladdr")
	psqlport := viper.GetInt("db.psql.psqlport")
	psqluser := viper.GetString("db.psql.psqluser")
	psqlpass := viper.GetString("db.psql.psqlpass")
	psqldb := viper.GetString("db.psql.psqldb")

	// 构建数据源字符串
	dataSource := fmt.Sprintf("host=%s port=%d user=%s dbname=%s password=%s sslmode=disable TimeZone=Asia/Shanghai",
		psqladdr,
		psqlport,
		psqluser,
		psqldb,
		psqlpass,
	)

	// 创建一个GORM DB实例
	dsn := dataSource
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	checkErr(err, "Failed to open gorm db")

	// 使用GORM DB实例和表名创建Casbin适配器，而不检查或创建表\
	a, err := gormadapter.NewAdapterByDBUseTableName(db, "", "casbin_rule")
	checkErr(err, "Failed to create new adapter")

	// 创建和配置Casbin执行器
	e, err := casbin.NewEnforcer("initialize/casbin/model.conf", a)
	checkErr(err, "Failed to create new enforcer")

	CasbinEnforcer = e
	err = CasbinEnforcer.LoadPolicy()
	checkErr(err, "Failed to load policy")

	log.Println("casbin启动完成")
}

func checkErr(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %v", msg, err)
	}
}
