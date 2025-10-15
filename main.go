package main

import (
	"flag"
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
	// 解析命令行参数
	configPath := flag.String("config", "", "配置文件路径...")
	flag.Parse()

	// 根据是否指定配置文件选择配置加载方式
	var configOption app.Option
	if *configPath != "" {
		configOption = app.WithConfigFile(*configPath)
	} else {
		configOption = app.WithProductionConfig()
	}

	// 使用Application结构体初始化
	application, err := app.NewApplication(
		// 基础配置
		configOption,
		app.WithRsaDecrypt("./configs/rsa_key/private_key.pem"),
		app.WithLogger(),
		app.WithDatabase(),
		app.WithRedis(),

		// 服务
		app.WithStorageService(),   // 添加 Storage 服务
		app.WithFlowService(),      // 添加 Flow 服务
		app.WithHeartbeatMonitor(), // 添加心跳监控服务
		app.WithGRPCService(),
		app.WithHTTPService(),
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
