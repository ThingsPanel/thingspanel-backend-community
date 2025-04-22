package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// 配置文件路径优先级：
// 1. 环境变量 GOTP_CONFIG_PATH 指定的路径
// 2. 当前目录下的 configs/conf.yml
// 3. 用户主目录下的 .thingspanel/conf.yml
// 4. /etc/thingspanel/conf.yml (Linux/Mac)
// 5. 内置默认配置

// LoadConfig 根据优先级加载配置文件
func LoadConfig() (*viper.Viper, error) {
	v := viper.New()
	v.SetConfigType("yml")

	// 1. 检查环境变量
	if configPath := os.Getenv("GOTP_CONFIG_PATH"); configPath != "" {
		v.SetConfigFile(configPath)
		if err := v.ReadInConfig(); err == nil {
			return v, nil
		}
	}

	// 2. 检查当前目录
	v.SetConfigFile("./configs/conf.yml")
	if err := v.ReadInConfig(); err == nil {
		return v, nil
	}

	// 3. 检查用户主目录
	home, err := os.UserHomeDir()
	if err == nil {
		userConfigPath := filepath.Join(home, ".thingspanel", "conf.yml")
		v.SetConfigFile(userConfigPath)
		if err := v.ReadInConfig(); err == nil {
			return v, nil
		}
	}

	// 4. 检查系统目录 (仅适用于Linux/Mac)
	if os.Getenv("GOOS") != "windows" {
		v.SetConfigFile("/etc/thingspanel/conf.yml")
		if err := v.ReadInConfig(); err == nil {
			return v, nil
		}
	}

	// 5. 使用内置默认配置
	return nil, fmt.Errorf("没有找到任何可用的配置文件")
}

// 加载特定环境的配置
func LoadEnvironmentConfig(env string) (*viper.Viper, error) {
	v := viper.New()
	v.SetConfigType("yml")

	var configFile string
	switch env {
	case "dev":
		configFile = "./configs/conf-localdev.yml"
	case "test":
		configFile = "./configs/conf-test.yml"
	case "prod":
		configFile = "./configs/conf.yml"
	default:
		return nil, fmt.Errorf("未知的环境类型: %s", env)
	}

	v.SetConfigFile(configFile)
	if err := v.ReadInConfig(); err != nil {
		return nil, err
	}

	return v, nil
}
