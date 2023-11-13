package conf

import (
	"fmt"
	"log"
	"strings"

	"github.com/spf13/viper"
)

func Init() {
	log.Println("系统配置文件初始化...")
	viper.SetEnvPrefix("GOTP")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.SetConfigName("./conf/app")
	viper.SetConfigType("yml")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig()
	if err != nil {
		panic(fmt.Errorf("failed to read configuration file: %s", err))
	}
	log.Println("系统配置文件初始化完成")
}
