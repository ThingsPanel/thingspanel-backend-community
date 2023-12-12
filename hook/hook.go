package hook

import (
	"ThingsPanel-Go/models"
	"fmt"

	"github.com/spf13/viper"
)

type Hook interface {
	LoginAdditionalInfoVerifyHook(*models.Users) error
}

var PluginSlice []Hook

func Init() {
	type PluginConfig struct {
		Name    string
		Enabled bool
	}
	// 从配置文件中获取插件列表
	var pluginConfig []PluginConfig
	err := viper.UnmarshalKey("plugins", &pluginConfig)
	if err != nil {
		fmt.Printf("UnmarshalKey failed: %v\n", err)
		return
	}
	for _, p := range pluginConfig {
		if p.Enabled {
			plugin, err := CreateHook(p.Name)
			if err != nil {
				fmt.Printf("Create plugin %s failed: %v\n", p.Name, err)
				continue
			}
			PluginSlice = append(PluginSlice, plugin)
		}
	}
}
