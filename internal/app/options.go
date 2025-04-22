package app

import (
	"project/initialize"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// WithConfig 直接使用已经初始化好的Viper实例
func WithConfig(config *viper.Viper) Option {
	return func(app *Application) error {
		app.Config = config
		// 设置全局viper实例（为了兼容现有代码）
		for _, key := range config.AllKeys() {
			viper.Set(key, config.Get(key))
		}
		return nil
	}
}

// WithEnvironment 根据环境名称加载配置
func WithEnvironment(env string) Option {
	return func(app *Application) error {
		config, err := LoadEnvironmentConfig(env)
		if err != nil {
			return err
		}
		return WithConfig(config)(app)
	}
}

// WithProductionConfig 使用生产环境配置
func WithProductionConfig() Option {
	return WithEnvironment("prod")
}

// WithDevelopmentConfig 使用开发环境配置
func WithDevelopmentConfig() Option {
	return WithEnvironment("dev")
}

// WithTestConfig 使用测试环境配置
func WithTestConfig() Option {
	return WithEnvironment("test")
}

// WithRsaDecrypt 初始化RSA解密
func WithRsaDecrypt(keyPath string) Option {
	return func(app *Application) error {
		return initialize.RsaDecryptInit(keyPath)
	}
}

// WithLogger 配置日志系统
func WithLogger() Option {
	return func(app *Application) error {
		if err := initialize.LogInIt(); err != nil {
			return err
		}
		app.Logger = logrus.StandardLogger()
		return nil
	}
}

// WithDatabase 初始化数据库连接
func WithDatabase() Option {
	return func(app *Application) error {
		db, err := initialize.PgInit()
		if err != nil {
			return err
		}
		app.DB = db
		return nil
	}
}

// WithRedis 初始化Redis连接
func WithRedis() Option {
	return func(app *Application) error {
		client, err := initialize.RedisInit()
		if err != nil {
			return err
		}
		app.RedisClient = client
		return nil
	}
}
