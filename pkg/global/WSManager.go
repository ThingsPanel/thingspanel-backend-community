package global

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

var TPWSManager *WSManager

// InitWSManager 初始化 WebSocket 管理器
func InitWSManager() {
	TPWSManager = NewWSManager()
	go TPWSManager.ListenForEvents()
}

// WSManager WebSocket 管理器
type WSManager struct {
	redisClient *redis.Client
	// 设备订阅: map[deviceID][connID]*WSClient
	deviceClients map[string]map[string]*WSClient
	mutex         sync.RWMutex
}

// WSClient WebSocket 客户端
type WSClient struct {
	DeviceID string
	TenantID string
	UserID   string
	Conn     *websocket.Conn
	ConnID   string
	MsgType  int // websocket.TextMessage or websocket.BinaryMessage
	Mu       *sync.Mutex
	Keys     []string // 订阅的字段（为空表示订阅全部）
	// Send 用于写入数据的缓冲管道，避免在多个goroutine中直接写Conn导致阻塞。
	// 关闭 Send 的所有权只属于 WSManager（见 closeSend），调用方不要自己 close，
	// 否则会出现重复 close 的 panic。
	Send chan []byte
	// closeOnce 保证 Send 只被关闭一次：连接断开时 handler 的 defer 和写 goroutine
	// 的错误分支都会走到取消订阅，二者可能并发。
	closeOnce sync.Once
}

// closeSend 幂等地关闭写队列，结束对应的写入 goroutine。
func (c *WSClient) closeSend() {
	if c == nil || c.Send == nil {
		return
	}
	c.closeOnce.Do(func() {
		close(c.Send)
	})
}

// WSEvent WebSocket 事件
type WSEvent struct {
	DeviceID  string                 `json:"device_id"`
	TenantID  string                 `json:"tenant_id"`
	Timestamp int64                  `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}

// NewWSManager 创建 WebSocket 管理器
func NewWSManager() *WSManager {
	return &WSManager{
		redisClient:   REDIS,
		deviceClients: make(map[string]map[string]*WSClient),
	}
}

// SubscribeDevice 订阅设备
func (m *WSManager) SubscribeDevice(deviceID, connID string, client *WSClient) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 注册到内存 map
	if _, ok := m.deviceClients[deviceID]; !ok {
		m.deviceClients[deviceID] = make(map[string]*WSClient)
	}
	m.deviceClients[deviceID][connID] = client

	// 更新 Redis 订阅表
	ctx := context.Background()
	if err := m.redisClient.Incr(ctx, "ws:sub:"+deviceID).Err(); err != nil {
		logrus.WithError(err).Error("Failed to increment Redis subscription counter")
		return err
	}

	// 设置过期时间（5 分钟）
	if err := m.redisClient.Expire(ctx, "ws:sub:"+deviceID, 5*time.Minute).Err(); err != nil {
		logrus.WithError(err).Error("Failed to set Redis expiration")
	}

	logrus.WithFields(logrus.Fields{
		"device_id": deviceID,
		"conn_id":   connID,
		"keys":      client.Keys,
	}).Info("WebSocket client subscribed to device")

	return nil
}

// UnsubscribeDevice 取消订阅设备
func (m *WSManager) UnsubscribeDevice(deviceID, connID string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 从内存 map 移除
	var removedClient *WSClient
	if clients, ok := m.deviceClients[deviceID]; ok {
		if c, ok2 := clients[connID]; ok2 {
			removedClient = c
		}
		delete(clients, connID)
		if len(clients) == 0 {
			delete(m.deviceClients, deviceID)
		}
	}

	// 同一条连接会被取消订阅两次（handler 的 defer + 写 goroutine 的错误分支）。
	// 只有真正摘掉了客户端的那一次才能去减 Redis 计数，否则会多减一次：
	// 当同一设备还有别的订阅者时，计数会被提前减到 0 并删除 ws:sub:<id>，
	// 而 checkAndPublishToWS 以该 key 是否存在作为"有没有订阅者"的判据，
	// RefreshSubscription 用的又是 Expire（对不存在的 key 不会重建），
	// 于是剩下那个连接还活着、心跳也正常，却再也收不到任何遥测推送。
	if removedClient == nil {
		logrus.WithFields(logrus.Fields{
			"device_id": deviceID,
			"conn_id":   connID,
		}).Debug("WebSocket client already unsubscribed, skip counter decrement")
		return nil
	}

	// 更新 Redis 订阅表
	ctx := context.Background()
	count, err := m.redisClient.Decr(ctx, "ws:sub:"+deviceID).Result()
	if err != nil {
		logrus.WithError(err).Error("Failed to decrement Redis subscription counter")
		return err
	}

	// 如果订阅数为 0，删除 key
	if count <= 0 {
		m.redisClient.Del(ctx, "ws:sub:"+deviceID)
	}

	logrus.WithFields(logrus.Fields{
		"device_id": deviceID,
		"conn_id":   connID,
	}).Info("WebSocket client unsubscribed from device")

	// 关闭写队列（如果存在），以结束对应的写入 goroutine。
	// 仍持有写锁，与 PushToDevice 的读锁互斥，保证不会出现"向已关闭 channel 发送"。
	removedClient.closeSend()

	return nil
}

// RefreshSubscription 续期订阅（心跳）
func (m *WSManager) RefreshSubscription(deviceID string) error {
	ctx := context.Background()
	if err := m.redisClient.Expire(ctx, "ws:sub:"+deviceID, 5*time.Minute).Err(); err != nil {
		logrus.WithError(err).WithField("device_id", deviceID).Error("Failed to refresh subscription")
		return err
	}
	return nil
}

// PushToDevice 推送消息到设备订阅者（本实例）
func (m *WSManager) PushToDevice(deviceID string, data map[string]interface{}) {
	// 整个发送过程持有读锁：UnsubscribeDevice 需要写锁才能关闭 Send，
	// 这样就不会在本函数发送的同时把 channel 关掉（"send on closed channel" 会 panic，
	// 而本函数运行在 ListenForEvents 这个没有 recover 的 goroutine 上，会直接带崩进程）。
	// 下面的发送都是非阻塞的（select + default），不会长时间占锁。
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	clients, ok := m.deviceClients[deviceID]
	if !ok || len(clients) == 0 {
		return // 本实例无订阅者
	}

	// 添加系统时间
	data["systime"] = time.Now().UTC()

	for connID, client := range clients {
		// 过滤字段（如果指定了 keys）
		filteredData := data
		if len(client.Keys) > 0 {
			filteredData = filterDataByKeys(data, client.Keys)
		}

		// 序列化
		jsonData, err := json.Marshal(filteredData)
		if err != nil {
			logrus.WithError(err).WithField("conn_id", connID).Error("Failed to marshal WebSocket data")
			continue
		}

		// 推送到 WebSocket：优先通过 client.Send 非阻塞发送到写入 goroutine，
		// 避免在此处直接写 Conn 导致阻塞整个管理器或读处理循环。
		select {
		case client.Send <- jsonData:
			// queued successfully
		default:
			// send queue is full，记录并丢弃消息，避免阻塞
			logrus.WithFields(logrus.Fields{
				"device_id": deviceID,
				"conn_id":   connID,
			}).Warn("WebSocket send buffer full, dropping message")
		}
	}
}

// ListenForEvents 监听 Redis Pub/Sub，使用指数退避重连
func (m *WSManager) ListenForEvents() {
	const (
		initialBackoff = 1 * time.Second
		maxBackoff     = 30 * time.Second
	)
	backoff := initialBackoff
	ctx := context.Background()

	logrus.Info("WebSocketManager started listening for Redis Pub/Sub events")

	for {
		pubsub := m.redisClient.PSubscribe(ctx, "ws:device:*")
		for {
			msg, err := pubsub.ReceiveMessage(ctx)
			if err != nil {
				logrus.WithError(err).Warnf("WS: Redis pubsub error, reconnecting in %v", backoff)
				time.Sleep(backoff)
				backoff = min(backoff*2, maxBackoff)
				break
			}
			backoff = initialBackoff

			var event WSEvent
			if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
				logrus.WithError(err).Error("Failed to unmarshal WebSocket event")
				continue
			}

			m.PushToDevice(event.DeviceID, event.Data)
		}
		pubsub.Close()
	}
}

// GetStats 获取统计信息
func (m *WSManager) GetStats() map[string]interface{} {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	totalClients := 0
	for _, clients := range m.deviceClients {
		totalClients += len(clients)
	}

	return map[string]interface{}{
		"device_subscriptions": len(m.deviceClients),
		"total_clients":        totalClients,
	}
}

// filterDataByKeys 过滤数据字段
func filterDataByKeys(data map[string]interface{}, keys []string) map[string]interface{} {
	filtered := make(map[string]interface{})

	// 保留 systime
	if systime, ok := data["systime"]; ok {
		filtered["systime"] = systime
	}

	// 只保留指定的 keys
	for _, key := range keys {
		if value, ok := data[key]; ok {
			filtered[key] = value
		}
	}

	return filtered
}
