package initialize

import (
	"fmt"
	"log"

	global "project/pkg/global"

	"github.com/casbin/casbin/v2"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/spf13/viper"
)

func CasbinInit() error {
	log.Println("casbin启动...")

	a, err := gormadapter.NewAdapterByDB(global.DB)
	if err != nil {
		return fmt.Errorf("failed to initialize GORM adapter: %v", err)
	}

	e, err := casbin.NewEnforcer("./configs/casbin.conf", a)
	if err != nil {
		return fmt.Errorf("failed to create enforcer: %v", err)
	}

	if err := e.LoadPolicy(); err != nil {
		return fmt.Errorf("failed to load policy: %v", err)
	}

	global.CasbinEnforcer = e
	log.Println("casbin启动完成")

	global.OtaAddress = viper.GetString("ota.download_address")
	return nil
}
