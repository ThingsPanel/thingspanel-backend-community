package casbin

import (
	"fmt"
	"log"
	"os"
	"strconv"

	beego "github.com/beego/beego/v2/server/web"
	"github.com/casbin/casbin/v2"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var CasbinEnforcer *casbin.Enforcer

func init() {
	log.Println("casbin启动...")

	// 从配置文件中获取PostgreSQL数据库的配置信息
	psqluser, err := beego.AppConfig.String("psqluser")
	checkErr(err, "Failed to get psqluser")

	psqlpass, err := beego.AppConfig.String("psqlpass")
	checkErr(err, "Failed to get psqlpass")

	psqladdr := os.Getenv("TP_PG_IP")
	if psqladdr == "" {
		psqladdr, err = beego.AppConfig.String("psqladdr")
		checkErr(err, "Failed to get psqladdr")
	}

	psqlports := os.Getenv("TP_PG_PORT")
	var psqlport int
	if psqlports == "" {
		psqlport, err = beego.AppConfig.Int("psqlport")
		checkErr(err, "Failed to get psqlport")
	} else {
		psqlport, err = strconv.Atoi(psqlports)
		checkErr(err, "Failed to convert psqlports to int")
	}

	psqldb, err := beego.AppConfig.String("psqldb")
	checkErr(err, "Failed to get psqldb")

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
	a, err := gormadapter.NewAdapterByDBUseTableName(db, "postgres", "casbin_rule")
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
