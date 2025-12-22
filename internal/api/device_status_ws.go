package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"project/internal/service"
	"project/pkg/errcode"
	"project/pkg/global"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

// ServeDeviceOnlineStatusWS 通过 WebSocket 提供设备在线状态订阅（新实现）
// @Router       /api/v1/device/online/status/ws [get]
func (*TelemetryDataApi) ServeDeviceOnlineStatusWS(c *gin.Context) {
	// 升级 WebSocket 连接
	conn, err := Wsupgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		c.Error(errcode.WithData(errcode.CodeSystemError, "WebSocket upgrade failed"))
		return
	}
	defer conn.Close()

	clientIP := conn.RemoteAddr().String()
	logrus.Infof("收到新的 WebSocket 连接: %s", clientIP)

	// 读取客户端首条消息（用于鉴权 + 指定 device_ids）
	msgType, msg, err := conn.ReadMessage()
	if err != nil {
		logrus.Error("读取初始消息失败:", err)
		conn.WriteMessage(websocket.TextMessage, []byte("Failed to read initial message"))
		return
	}

	var initMap map[string]interface{}
	if err := json.Unmarshal(msg, &initMap); err != nil {
		logrus.Error("初始消息 JSON 格式无效:", err)
		conn.WriteMessage(msgType, []byte("Invalid initial message format"))
		return
	}

	// 提取 device_ids（支持数组或逗号分隔字符串或单个 device_id）
	var deviceIDs []string
	if v, ok := initMap["device_ids"]; ok {
		switch t := v.(type) {
		case []interface{}:
			for _, it := range t {
				if s, ok := it.(string); ok && s != "" {
					deviceIDs = append(deviceIDs, strings.TrimSpace(s))
				}
			}
		case string:
			if strings.Contains(t, ",") {
				for _, s := range strings.Split(t, ",") {
					if s = strings.TrimSpace(s); s != "" {
						deviceIDs = append(deviceIDs, s)
					}
				}
			} else if t != "" {
				deviceIDs = append(deviceIDs, strings.TrimSpace(t))
			}
		}
	}
	if len(deviceIDs) == 0 {
		if v, ok := initMap["device_id"]; ok {
			if s, ok := v.(string); ok && s != "" {
				deviceIDs = append(deviceIDs, strings.TrimSpace(s))
			}
		}
	}

	// 规范化并提取鉴权字段（支持多种命名与 Header）
	getStr := func(m map[string]interface{}, keys ...string) string {
		for _, k := range keys {
			if v, ok := m[k]; ok {
				if s, isStr := v.(string); isStr && s != "" {
					return s
				}
				// 其它类型也尝试格式化为字符串
				return fmt.Sprintf("%v", v)
			}
		}
		return ""
	}

	// 优先从初始消息中读取 token 或 authorization
	if token := getStr(initMap, "token", "Token", "authorization", "Authorization"); token != "" {
		token = strings.TrimPrefix(token, "Bearer ")
		initMap["token"] = token
	}

	// 优先从初始消息中读取 api key 多种命名
	if apiKey := getStr(initMap, "x-api-key", "x_api_key", "xapikey", "X-Api-Key", "X_Api_Key", "apikey"); apiKey != "" {
		initMap["x-api-key"] = apiKey
	}

	// 若仍未提取到，再从 HTTP Header 中读取（兼容客户端只在 header 传 token 的场景）
	if _, ok := initMap["token"]; !ok || initMap["token"] == "" {
		if auth := c.GetHeader("Authorization"); auth != "" {
			initMap["token"] = strings.TrimPrefix(auth, "Bearer ")
		}
	}
	if _, ok := initMap["x-api-key"]; !ok || initMap["x-api-key"] == "" {
		if x := c.GetHeader("X-Api-Key"); x != "" {
			initMap["x-api-key"] = x
		}
	}

	// 调试日志：列出初始消息中携带的键（不打印敏感值）
	keys := make([]string, 0, len(initMap))
	for k := range initMap {
		keys = append(keys, k)
	}
	logrus.Debugf("WS initial message keys: %v", keys)

	// 鉴权（复用现有 validateAuth）
	claims, err := validateAuth(initMap)
	if err != nil {
		logrus.WithError(err).Error("鉴权失败")
		conn.WriteMessage(msgType, []byte(err.Error()))
		return
	}

	if len(deviceIDs) == 0 {
		conn.WriteMessage(msgType, []byte("device_ids is required"))
		return
	}

	logrus.Infof("WebSocket 鉴权通过 - 用户ID: %s, 租户ID: %s, 订阅设备数: %d", claims.ID, claims.TenantID, len(deviceIDs))

	// 组装并发送初始状态（数组形式）
	var initialList []map[string]interface{}
	for _, did := range deviceIDs {
		st, err := service.GroupApp.Device.GetDeviceOnlineStatus(did)
		if err != nil {
			logrus.WithError(err).WithField("device_id", did).Warn("查询当前设备状态失败，跳过")
			continue
		}
		isOnline := 0
		// GetDeviceOnlineStatus 返回 map[string]int
		if v, ok := st["is_online"]; ok {
			isOnline = v
		} else if v, ok := st["device_status"]; ok {
			isOnline = v
		}
		tm := time.Now().UnixMilli()
		// service 层目前不返回 timestamp，使用当前时间作为兜底
		initialList = append(initialList, map[string]interface{}{
			"device_id": did,
			"is_online": isOnline,
			"timestamp": tm,
		})
	}
	if b, err := json.Marshal(initialList); err == nil {
		if err := conn.WriteMessage(msgType, b); err != nil {
			logrus.WithError(err).Error("发送初始状态失败")
			return
		}
	}

	// 订阅对应 Redis 通道并转发（支持多通道）
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	channels := make([]string, 0, len(deviceIDs))
	for _, did := range deviceIDs {
		channels = append(channels, fmt.Sprintf("device:%s:status", did))
	}

	pubsub := global.REDIS.Subscribe(ctx, channels...)
	defer pubsub.Close()

	var mu sync.Mutex

	// 转发 goroutine
	go func() {
		ch := pubsub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					logrus.Warn("Redis 通道关闭")
					return
				}

				// 尝试将 payload 解析为 JSON 并注入 device_id（从 channel 中解析）
				var out []byte
				payload := []byte(msg.Payload)
				deviceID := ""
				// Redis channel 预期格式: device:{device_id}:status
				parts := strings.Split(msg.Channel, ":")
				if len(parts) >= 3 {
					deviceID = parts[1]
				}

				var payloadMap map[string]interface{}
				if err := json.Unmarshal(payload, &payloadMap); err == nil {
					// 合并 device_id（覆盖或添加）
					payloadMap["device_id"] = deviceID
					if b, err := json.Marshal(payloadMap); err == nil {
						out = b
					} else {
						// 若 Marshal 失败则兜底为原始 payload 包装
						out, _ = json.Marshal(map[string]interface{}{"device_id": deviceID, "payload": string(payload)})
					}
				} else {
					// 非 JSON payload，包装为结构化对象
					out, _ = json.Marshal(map[string]interface{}{"device_id": deviceID, "payload": string(payload)})
				}

				mu.Lock()
				err := conn.WriteMessage(websocket.TextMessage, out)
				mu.Unlock()
				if err != nil {
					logrus.WithError(err).Error("写入 WebSocket 失败，结束连接")
					cancel()
					return
				}
			}
		}
	}()

	// 心跳 + 等待关闭
	for {
		_, wsMsg, err := conn.ReadMessage()
		if err != nil {
			logrus.WithError(err).Info("WebSocket 连接关闭")
			_ = conn.WriteControl(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "connection closed"),
				time.Now().Add(time.Second))
			cancel()
			return
		}
		if string(wsMsg) == "ping" {
			mu.Lock()
			_ = conn.WriteMessage(msgType, []byte("pong"))
			mu.Unlock()
		}
	}
}
