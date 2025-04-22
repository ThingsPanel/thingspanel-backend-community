package main

import (
	"fmt"
	"os"

	"project/internal/app"

	"github.com/sirupsen/logrus"
)

// @title           ThingsPanel API
// @version         1.0
// @description     ThingsPanel API.
// @schemes         http
// @host      localhost:9999
// @BasePath
// @securityDefinitions.apikey  ApiKeyAuth
// @in                          header
// @name                        x-token
func main() {
	// 使用Application结构体初始化
	application, err := app.NewApplication(
		// 基础配置
		app.WithProductionConfig(),
		app.WithRsaDecrypt("./configs/rsa_key/private_key.pem"),
		app.WithLogger(),
		app.WithDatabase(),
		app.WithRedis(),

		// 服务
		app.WithHTTPService(),
		app.WithGRPCService(),
		app.WithMQTTService(),
		app.WithCronService(),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "应用初始化失败: %v\n", err)
		os.Exit(1)
	}

	// 启动所有服务
	if err := application.Start(); err != nil {
		logrus.Fatalf("启动服务失败: %v", err)
	}

	// 等待服务运行并处理退出
	application.Wait()

	// 应用关闭时自动调用 Shutdown 方法清理资源
	defer application.Shutdown()
}
