package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	router "project/router"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// HTTPService 实现HTTP服务
type HTTPService struct {
	server *http.Server
	config *HTTPConfig
}

// HTTPConfig 保存HTTP服务配置
type HTTPConfig struct {
	Host            string
	Port            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
}

// NewHTTPService 创建新的HTTP服务
func NewHTTPService() *HTTPService {
	return &HTTPService{
		config: &HTTPConfig{
			Host:            "localhost",
			Port:            "9999",
			ReadTimeout:     60 * time.Second,
			WriteTimeout:    60 * time.Second,
			ShutdownTimeout: 5 * time.Second,
		},
	}
}

// Name 返回服务名称
func (s *HTTPService) Name() string {
	return "HTTP服务"
}

// SetConfig 设置HTTP服务配置
func (s *HTTPService) SetConfig(host, port string, readTimeout, writeTimeout, shutdownTimeout time.Duration) {
	s.config.Host = host
	s.config.Port = port
	s.config.ReadTimeout = readTimeout
	s.config.WriteTimeout = writeTimeout
	s.config.ShutdownTimeout = shutdownTimeout
}

// Start 启动HTTP服务
func (s *HTTPService) Start() error {
	// 从配置中加载主机和端口
	host := viper.GetString("service.http.host")
	if host == "" {
		host = s.config.Host
		logrus.Debugf("使用默认主机: %s", host)
	}

	port := viper.GetString("service.http.port")
	if port == "" {
		port = s.config.Port
		logrus.Debugf("使用默认端口: %s", port)
	}

	// 初始化路由
	handler := router.RouterInit()

	// 创建服务器
	s.server = &http.Server{
		Addr:         net.JoinHostPort(host, port),
		Handler:      handler,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
	}

	// 异步启动服务器
	go func() {
		logrus.Infof("HTTP服务正在监听 %s:%s", host, port)

		// 打印启动成功信息
		successInfo()

		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logrus.Errorf("HTTP服务器错误: %v", err)
		}
	}()

	return nil
}

// Stop 停止HTTP服务
func (s *HTTPService) Stop() error {
	if s.server == nil {
		return nil
	}

	logrus.Info("正在停止HTTP服务...")
	ctx, cancel := context.WithTimeout(context.Background(), s.config.ShutdownTimeout)
	defer cancel()

	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("HTTP服务优雅关闭失败: %v", err)
	}

	logrus.Info("HTTP服务已停止")
	return nil
}

// WithHTTPService 将HTTP服务添加到应用
func WithHTTPService() Option {
	return func(app *Application) error {
		service := NewHTTPService()
		app.RegisterService(service)
		return nil
	}
}

// 打印启动成功信息
func successInfo() {
	// 获取当前时间
	startTime := time.Now().Format("2006-01-02 15:04:05")

	// 打印启动成功消息
	fmt.Println("----------------------------------------")
	fmt.Println("        TingsPanel 启动成功!")
	fmt.Println("----------------------------------------")
	fmt.Printf("启动时间: %s\n", startTime)
	fmt.Println("版本: v1.1.8社区版")
	fmt.Println("----------------------------------------")
	fmt.Println("欢迎使用 TingsPanel！")
	fmt.Println("如需帮助，请访问: http://docs.thingspanel.cn")
	fmt.Println("----------------------------------------")
}
