package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// HeartbeatMonitor 心跳监控服务
type HeartbeatMonitor struct {
	redis           *redis.Client
	statusPublisher StatusPublisher // ✨ 依赖本地定义的接口（避免循环依赖）
	logger          *logrus.Logger
	ctx             context.Context
	cancel          context.CancelFunc
}

// NewHeartbeatMonitor 创建心跳监控服务实例
func NewHeartbeatMonitor(redis *redis.Client, publisher StatusPublisher, logger *logrus.Logger) *HeartbeatMonitor {
	ctx, cancel := context.WithCancel(context.Background())
	return &HeartbeatMonitor{
		redis:           redis,
		statusPublisher: publisher,
		logger:          logger,
		ctx:             ctx,
		cancel:          cancel,
	}
}

// Start 启动心跳监控服务
func (m *HeartbeatMonitor) Start() error {
	// 配置 Redis 过期通知
	if err := m.configureRedis(); err != nil {
		return fmt.Errorf("failed to configure redis: %w", err)
	}

	// 获取 Redis 数据库编号
	dbNum := viper.GetInt("db.redis.db1")
	if dbNum == 0 {
		dbNum = 10 // 默认使用 db10
	}

	// 订阅过期事件
	pattern := fmt.Sprintf("__keyevent@%d__:expired", dbNum)
	pubsub := m.redis.PSubscribe(m.ctx, pattern)

	m.logger.WithField("pattern", pattern).Info("HeartbeatMonitor started, subscribing to Redis expiry events")

	// 启动监听协程
	go func() {
		ch := pubsub.Channel()
		for {
			select {
			case <-m.ctx.Done():
				m.logger.Info("HeartbeatMonitor stopped")
				pubsub.Close()
				return
			case msg := <-ch:
				if msg != nil {
					m.handleExpiredKey(msg)
				}
			}
		}
	}()

	return nil
}

// Stop 停止心跳监控服务
func (m *HeartbeatMonitor) Stop() error {
	m.cancel()
	return nil
}

// configureRedis 配置 Redis 启用过期事件通知
func (m *HeartbeatMonitor) configureRedis() error {
	// 设置 Redis 配置: notify-keyspace-events Ex
	// E - keyevent 事件, x - 过期事件
	err := m.redis.ConfigSet(m.ctx, "notify-keyspace-events", "Ex").Err()
	if err != nil {
		m.logger.WithError(err).Warn("Failed to set Redis notify-keyspace-events, may already be configured")
		// 不返回错误,可能已经配置过
	}
	return nil
}

// handleExpiredKey 处理过期的 Redis key
func (m *HeartbeatMonitor) handleExpiredKey(msg *redis.Message) {
	// 解析 key: device:{deviceId}:{type}
	if !strings.HasPrefix(msg.Payload, "device:") {
		return
	}

	parts := strings.Split(msg.Payload, ":")
	if len(parts) != 3 {
		return
	}

	keyType := parts[2]
	if keyType != "heartbeat" && keyType != "timeout" {
		return
	}

	deviceID := parts[1]

	m.logger.WithFields(logrus.Fields{
		"device_id": deviceID,
		"key_type":  keyType,
		"key":       msg.Payload,
	}).Info("Device heartbeat/timeout expired, marking as offline")

	// 确定离线来源
	source := "heartbeat_expired"
	if keyType == "timeout" {
		source = "timeout_expired"
	}

	// ✨ 通过 StatusPublisher 接口发送离线状态到 Flow Bus → StatusFlow
	// 协议无关设计：无论 MQTT/Kafka 都通过统一的接口处理
	if m.statusPublisher != nil {
		if err := m.statusPublisher.PublishStatusOffline(deviceID, source); err != nil {
			m.logger.WithError(err).WithFields(logrus.Fields{
				"device_id": deviceID,
				"source":    source,
			}).Error("Failed to publish device offline event")
		}
	} else {
		m.logger.Warn("StatusPublisher not available, cannot send offline event")
	}
}
