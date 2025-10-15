package device

// DEPRECATED: 此文件已废弃,请使用 internal/service/heartbeat_monitor.go
//
// 新的架构:
// - HeartbeatMonitor: 监听 Redis 过期事件
// - 通过 MQTTAdapter 发送离线消息到 Flow 层
// - StatusFlow 处理状态更新
//
// 迁移说明:
// 1. 启用 Flow 层: flow.enable = true
// 2. 添加 WithHeartbeatMonitor() 到 main.go
// 3. 删除对 DeviceListener 的调用

import (
	"context"
	"errors"
	"fmt"
	"project/mqtt/subscribe"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// DeviceListener Redis过期事件监听器
// DEPRECATED: 使用 service.HeartbeatMonitor 替代
type DeviceListener struct {
	redis     *redis.Client
	ctx       context.Context
	cancel    context.CancelFunc
	waitGroup sync.WaitGroup
}

// NewDeviceListener 创建新的设备状态监听器
func NewDeviceListener(redis *redis.Client) *DeviceListener {
	ctx, cancel := context.WithCancel(context.Background())
	return &DeviceListener{
		redis:  redis,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start 启动监听器
func (l *DeviceListener) Start() error {
	// 检查Redis配置
	if err := l.checkRedisConfig(); err != nil {
		return err
	}

	l.waitGroup.Add(1)
	go l.run()
	return nil
}

// checkRedisConfig 检查并设置Redis配置
func (l *DeviceListener) checkRedisConfig() error {
	config, err := l.redis.ConfigGet(l.ctx, "notify-keyspace-events").Result()
	if err != nil {
		return fmt.Errorf("获取Redis配置失败: %v", err)
	}

	configValue := config["notify-keyspace-events"]
	if !strings.Contains(configValue, "Ex") {
		err = l.redis.ConfigSet(l.ctx, "notify-keyspace-events", "Ex").Err()
		if err != nil {
			return fmt.Errorf("设置Redis配置失败: %v", err)
		}
		logrus.Info("已更新Redis过期通知配置")
	}
	return nil
}

func (l *DeviceListener) run() {
	defer l.waitGroup.Done()
	defer logrus.Info("设备监听器已退出")

	dbNum := viper.GetInt("db.redis.db1")
	if dbNum == 0 {
		dbNum = 10 // 默认使用第11个DB
	}
	// 只订阅 db的过期事件
	pubsub := l.redis.PSubscribe(l.ctx, fmt.Sprintf("__keyevent@%d__:expired", dbNum))
	defer pubsub.Close()

	// 验证订阅是否成功
	if err := pubsub.Ping(l.ctx); err != nil {
		logrus.WithError(err).Error("订阅Redis过期事件失败")
		return
	}

	logrus.Infof("设备监听器启动成功，监听db%d过期事件", dbNum)

	ch := pubsub.Channel(redis.WithChannelSize(100))
	for {
		select {
		case <-l.ctx.Done():
			logrus.Info("监听器上下文已取消")
			return
		case msg, ok := <-ch:
			if !ok {
				logrus.Warn("通道已关闭")
				return
			}
			// 只处理设备相关的key
			if strings.HasPrefix(msg.Payload, "device:") &&
				(strings.HasSuffix(msg.Payload, ":heartbeat") ||
					strings.HasSuffix(msg.Payload, ":timeout")) {
				l.handleExpiredKey(msg)
			}
		}
	}
}

func (l *DeviceListener) handleExpiredKey(msg *redis.Message) {
	if msg == nil {
		return
	}

	// 解析过期的key，格式为 device:{deviceId}:{type}
	keyParts := strings.Split(msg.Payload, ":")
	if len(keyParts) != 3 {
		logrus.WithField("payload", msg.Payload).
			Warn("无效的key格式")
		return
	}

	deviceID := keyParts[1]
	logrus.WithFields(logrus.Fields{
		"deviceID": deviceID,
		"type":     keyParts[2],
	}).Debug("处理设备状态更新")

	select {
	case <-l.ctx.Done():
		return
	default:
		subscribe.DeviceOnline([]byte("0"), "devices/status/"+deviceID)
	}
}

// Stop 停止监听器
func (l *DeviceListener) Stop() error {
	logrus.Info("正在停止设备监听器...")

	l.cancel()

	done := make(chan struct{})
	go func() {
		l.waitGroup.Wait()
		close(done)
	}()

	select {
	case <-done:
		logrus.Info("设备监听器已正常停止")
		return nil
	case <-time.After(3 * time.Second):
		return errors.New("设备监听器停止超时")
	}
}
