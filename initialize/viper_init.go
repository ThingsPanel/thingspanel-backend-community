package initialize

import (
	"fmt"
	"log"
	"strings"

	"github.com/spf13/viper"
)

func ViperInit(path string) {
	viper.SetEnvPrefix("GOTP")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if path != "" {
		viper.SetConfigFile(path)
	} else {
		viper.SetConfigName("./configs/conf")
	}

	viper.SetConfigType("yml")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig()
	if err != nil {
		panic(fmt.Errorf("failed to read configuration file: %s", err))
	}
	log.Println("viper加载conf.yml配置文件完成...")
}
