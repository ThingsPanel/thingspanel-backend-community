package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Service 接口定义了所有服务组件必须实现的方法
type Service interface {
	// Name 返回服务名称
	Name() string

	// Start 启动服务，如果出错则返回错误
	Start() error

	// Stop 停止服务并清理资源
	Stop() error
}

// ServiceManager 管理多个服务的启动和停止
type ServiceManager struct {
	services []Service
	wg       sync.WaitGroup
	mu       sync.Mutex
	started  bool
}

// NewServiceManager 创建一个新的服务管理器
func NewServiceManager() *ServiceManager {
	return &ServiceManager{
		services: make([]Service, 0),
	}
}

// RegisterService 注册一个服务到管理器
func (m *ServiceManager) RegisterService(service Service) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.services = append(m.services, service)
	logrus.Infof("服务 %s 已注册", service.Name())
}

// StartAll 启动所有注册的服务
func (m *ServiceManager) StartAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return fmt.Errorf("服务已经启动")
	}

	for _, service := range m.services {
		logrus.Infof("正在启动服务: %s", service.Name())
		if err := service.Start(); err != nil {
			return fmt.Errorf("启动服务 %s 失败: %v", service.Name(), err)
		}
		m.wg.Add(1)
	}

	m.started = true
	return nil
}

// StopAll 停止所有服务，按照注册的相反顺序
func (m *ServiceManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return
	}

	// 创建一个带超时的上下文，确保停止操作不会永远阻塞
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 反向遍历服务列表，确保按照依赖顺序停止
	for i := len(m.services) - 1; i >= 0; i-- {
		service := m.services[i]
		logrus.Infof("正在停止服务: %s", service.Name())

		// 创建一个通道来接收停止完成的信号
		done := make(chan error, 1)

		go func(s Service) {
			done <- s.Stop()
			m.wg.Done()
		}(service)

		// 等待服务停止或超时
		select {
		case err := <-done:
			if err != nil {
				logrus.Errorf("停止服务 %s 时出错: %v", service.Name(), err)
			} else {
				logrus.Infof("服务 %s 已成功停止", service.Name())
			}
		case <-ctx.Done():
			logrus.Warnf("停止服务 %s 超时", service.Name())
		}
	}

	m.started = false
}

// Wait 等待所有服务完成
func (m *ServiceManager) Wait() {
	m.wg.Wait()
}
