package app

import (
	tptodb "project/third_party/grpc/tptodb_client"

	"github.com/sirupsen/logrus"
)

// GRPCService 实现gRPC客户端服务
type GRPCService struct {
	initialized bool
}

// NewGRPCService 创建gRPC服务实例
func NewGRPCService() *GRPCService {
	return &GRPCService{
		initialized: false,
	}
}

// Name 返回服务名称
func (s *GRPCService) Name() string {
	return "gRPC客户端服务"
}

// Start 启动gRPC服务
func (s *GRPCService) Start() error {
	logrus.Info("正在初始化gRPC客户端...")

	// 初始化gRPC客户端
	tptodb.GrpcTptodbInit()

	s.initialized = true
	logrus.Info("gRPC客户端初始化完成")
	return nil
}

// Stop 停止gRPC服务
func (s *GRPCService) Stop() error {
	if !s.initialized {
		return nil
	}

	logrus.Info("正在停止gRPC客户端...")
	// 如果有关闭方法，可以在这里调用

	logrus.Info("gRPC客户端已停止")
	return nil
}

// WithGRPCService 将gRPC服务添加到应用
func WithGRPCService() Option {
	return func(app *Application) error {
		service := NewGRPCService()
		app.RegisterService(service)
		return nil
	}
}
