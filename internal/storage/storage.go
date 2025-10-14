package storage

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// Storage 存储层接口
type Storage interface {
	Start(ctx context.Context, inputChan <-chan *Message) error
	Stop(timeout time.Duration) error
	GetMetrics() Metrics
}

// storage 存储层实现
type storage struct {
	db      *gorm.DB
	logger  Logger
	config  Config
	metrics *metricsCollector

	telemetryWriter *telemetryWriter
	directWriter    *directWriter

	inputChan <-chan *Message
	stopCh    chan struct{}
	doneCh    chan struct{}
}

// New 创建存储层实例
func New(db *gorm.DB, logger Logger, config Config) Storage {
	metrics := newMetricsCollector()

	return &storage{
		db:              db,
		logger:          logger,
		config:          config,
		metrics:         metrics,
		telemetryWriter: newTelemetryWriter(db, logger, config, metrics),
		directWriter:    newDirectWriter(db, logger, metrics),
		stopCh:          make(chan struct{}),
		doneCh:          make(chan struct{}),
	}
}

// Start 启动存储服务
func (s *storage) Start(ctx context.Context, inputChan <-chan *Message) error {
	if inputChan == nil {
		return fmt.Errorf("input channel is nil")
	}

	s.inputChan = inputChan

	s.telemetryWriter.start(ctx)

	go s.run(ctx)

	s.logger.Info("storage service started")
	return nil
}

// Stop 停止存储服务
func (s *storage) Stop(timeout time.Duration) error {
	close(s.stopCh)

	select {
	case <-s.doneCh:
		s.logger.Info("storage main loop stopped")
	case <-time.After(timeout):
		s.logger.Warn("storage main loop stop timeout")
	}

	s.telemetryWriter.stop(timeout)

	s.logger.Info("storage service stopped")
	return nil
}

// GetMetrics 获取监控指标
func (s *storage) GetMetrics() Metrics {
	return s.metrics.GetMetrics()
}

// run 主循环
func (s *storage) run(ctx context.Context) {
	defer close(s.doneCh)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("storage context cancelled")
			return
		case <-s.stopCh:
			s.logger.Info("storage stopped")
			return
		case msg, ok := <-s.inputChan:
			if !ok {
				s.logger.Info("input channel closed")
				return
			}
			s.handleMessage(msg)
		}
	}
}

// handleMessage 处理消息
func (s *storage) handleMessage(msg *Message) {
	switch msg.DataType {
	case DataTypeTelemetry:
		s.metrics.incTelemetryReceived()
		if err := s.telemetryWriter.write(msg); err != nil {
			s.logger.Errorf("handle telemetry message failed: %v", err)
		}

	case DataTypeAttribute:
		if err := s.directWriter.writeAttribute(msg); err != nil {
			s.logger.Errorf("handle attribute message failed: %v", err)
		}

	case DataTypeEvent:
		if err := s.directWriter.writeEvent(msg); err != nil {
			s.logger.Errorf("handle event message failed: %v", err)
		}

	default:
		s.logger.Warnf("unknown data type: %s", msg.DataType)
	}
}
