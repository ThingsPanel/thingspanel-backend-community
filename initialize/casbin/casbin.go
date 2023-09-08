package casbin

import (
	"fmt"
	"log"
	"os"
	"strconv"

	beego "github.com/beego/beego/v2/server/web"
	"github.com/casbin/casbin/v2"
	gormadapter "github.com/casbin/gorm-adapter/v3"
)

var CasbinEnforcer *casbin.Enforcer

func init() {
	log.Println("casbin启动...")

	psqluser, err := beego.AppConfig.String("psqluser")
	if err != nil {
		log.Fatalf("Failed to get psqluser: %v", err)
	}

	psqlpass, err := beego.AppConfig.String("psqlpass")
	if err != nil {
		log.Fatalf("Failed to get psqlpass: %v", err)
	}

	psqladdr := os.Getenv("TP_PG_IP")
	if psqladdr == "" {
		psqladdr, err = beego.AppConfig.String("psqladdr")
		if err != nil {
			log.Fatalf("Failed to get psqladdr: %v", err)
		}
	}

	psqlports := os.Getenv("TP_PG_PORT")
	var psqlport int
	if psqlports == "" {
		psqlport, err = beego.AppConfig.Int("psqlport")
		if err != nil {
			log.Fatalf("Failed to get psqlport: %v", err)
		}
	} else {
		psqlport, err = strconv.Atoi(psqlports)
		if err != nil {
			log.Fatalf("Failed to convert psqlports to int: %v", err)
		}
	}

	psqldb, err := beego.AppConfig.String("psqldb")
	if err != nil {
		log.Fatalf("Failed to get psqldb: %v", err)
	}

	dataSource := fmt.Sprintf("host=%s port=%d user=%s dbname=%s password=%s sslmode=disable TimeZone=Asia/Shanghai",
		psqladdr,
		psqlport,
		psqluser,
		psqldb,
		psqlpass,
	)

	a, err := gormadapter.NewAdapter("postgres", dataSource, false) // 设置为 false 以禁止自动创建表
	if err != nil {
		log.Fatalf("Failed to create new adapter: %v", err)
	}

	e, err := casbin.NewEnforcer("initialize/casbin/model.conf", a)
	if err != nil {
		log.Fatalf("Failed to create new enforcer: %v", err)
	}

	CasbinEnforcer = e
	err = CasbinEnforcer.LoadPolicy()
	if err != nil {
		log.Fatalf("Failed to load policy: %v", err)
	}

	log.Println("casbin启动完成")
}
