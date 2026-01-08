package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"project/pkg/constant"
	"project/pkg/errcode"
	"project/pkg/global"
	"project/pkg/utils"

	model "project/internal/model"
	service "project/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"project/internal/middleware"
)

type TelemetryDataApi struct{}

// GetCurrentData 设备当前值查询
// @Router   /api/v1/telemetry/datas/current/{id} [get]
func (*TelemetryDataApi) HandleCurrentData(c *gin.Context) {
	deviceId := c.Param("id")
	date, err := service.GroupApp.TelemetryData.GetCurrentTelemetrData(deviceId)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", date)
}

// 根据设备ID和key查询遥测当前值
// @Router /api/v1/telemetry/datas/current/keys [get]
func (*TelemetryDataApi) HandleCurrentDataKeys(c *gin.Context) {
	var req model.GetTelemetryCurrentDataKeysReq
	if !BindAndValidate(c, &req) {
		return
	}

	date, err := service.GroupApp.TelemetryData.GetCurrentTelemetrDataKeys(&req)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", date)
}

// ServeHistoryData 设备历史数值查询
// @Router   /api/v1/telemetry/datas/history [get]
func (*TelemetryDataApi) ServeHistoryData(c *gin.Context) {
	var req model.GetTelemetryHistoryDataReq
	if !BindAndValidate(c, &req) {
		return
	}
	date, err := service.GroupApp.TelemetryData.GetTelemetrHistoryData(&req)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", date)
}

// DeleteData 删除数据
// @Router   /api/v1/telemetry/datas [delete]
func (*TelemetryDataApi) DeleteData(c *gin.Context) {
	var req model.DeleteTelemetryDataReq
	if !BindAndValidate(c, &req) {
		return
	}
	err := service.GroupApp.TelemetryData.DeleteTelemetrData(&req)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", nil)
}

// GetCurrentData 根据设备ID获取最新的一条遥测数据
// @Router   /api/v1/telemetry/datas/current/detail/{id} [get]
func (*TelemetryDataApi) ServeCurrentDetailData(c *gin.Context) {
	deviceId := c.Param("id")
	date, err := service.GroupApp.TelemetryData.GetCurrentTelemetrDetailData(deviceId)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", date)
}

// ServeHistoryData 设备历史数值查询（分页）
// @Router   /api/v1/telemetry/datas/history/pagination [get]
func (*TelemetryDataApi) ServeHistoryDataByPage(c *gin.Context) {
	var req model.GetTelemetryHistoryDataByPageReq
	if !BindAndValidate(c, &req) {
		return
	}

	// 时间区间限制一个月内
	// if req.EndTime.Sub(req.StartTime) > time.Hour*24*30 {
	// 	ErrorHandler(c, http.StatusBadRequest, fmt.Errorf("time range should be within 30 days"))
	// 	return
	// }

	date, err := service.GroupApp.TelemetryData.GetTelemetrHistoryDataByPageV2(&req)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", date)
}

// ServeHistoryData 设备历史数值查询（分页）
// @Router   /api/v1/telemetry/datas/history/page [get]
func (*TelemetryDataApi) ServeHistoryDataByPageV2(c *gin.Context) {
	var req model.GetTelemetryHistoryDataByPageReq
	if !BindAndValidate(c, &req) {
		return
	}

	// 时间区间限制一个月内
	// if req.EndTime.Sub(req.StartTime) > time.Hour*24*30 {
	// 	ErrorHandler(c, http.StatusBadRequest, fmt.Errorf("time range should be within 30 days"))
	// 	return
	// }

	date, err := service.GroupApp.TelemetryData.GetTelemetrHistoryDataByPageV2(&req)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", date)
}

// ServeSetLogsDataListByPage 遥测数据下发记录查询（分页）
// @Router   /api/v1/telemetry/datas/set/logs [get]
func (*TelemetryDataApi) ServeSetLogsDataListByPage(c *gin.Context) {
	var req model.GetTelemetrySetLogsListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}

	date, err := service.GroupApp.TelemetryData.GetTelemetrSetLogsDataListByPage(&req)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", date)
}

// 获取模拟设备发送遥测数据的回显数据
// /api/v1/telemetry/datas/simulation [get]
func (*TelemetryDataApi) ServeEchoData(c *gin.Context) {
	var req model.ServeEchoDataReq
	if !BindAndValidate(c, &req) {
		return
	}

	// 获取Host (直接客户端IP)
	clientIP := c.Request.Host

	date, err := service.GroupApp.TelemetryData.ServeEchoData(&req, clientIP)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", date)
}

// 模拟设备发送遥测数据
// /api/v1/telemetry/datas/simulation [post]
func (*TelemetryDataApi) SimulationTelemetryData(c *gin.Context) {
	var req model.SimulationTelemetryDataReq
	if !BindAndValidate(c, &req) {
		return
	}
	_, err := service.GroupApp.TelemetryData.TelemetryPub(req.Command)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// validateToken 验证WebSocket中的token
func validateToken(token string) (*utils.UserClaims, error) {
	// 验证 Redis 中的 token
	if global.REDIS.Get(context.Background(), token).Val() != "1" {
		return nil, errors.New("token is expired")
	}

	// 刷新 token 过期时间
	timeout := viper.GetInt("session.timeout")
	if timeout == 0 {
		timeout = 60 // 默认60分钟
	}
	global.REDIS.Set(context.Background(), token, "1", time.Duration(timeout)*time.Minute)

	// 验证 JWT
	key := viper.GetString("jwt.key")
	j := utils.NewJWT([]byte(key))
	claims, err := j.ParseToken(token)
	if err != nil {
		return nil, errors.New("invalid token")
	}

	return claims, nil
}

// validateAPIKey 验证WebSocket中的API Key
func validateAPIKey(apiKey string) (*utils.UserClaims, error) {
	// 创建API Key验证器
	validator := middleware.NewAPIKeyValidator(global.DB, global.REDIS)

	// 验证API Key
	info, err := validator.ValidateAPIKey(apiKey)
	if err != nil {
		return nil, err
	}

	// 构造UserClaims
	claims := &utils.UserClaims{
		TenantID:  info.TenantID,
		Authority: "TENANT_ADMIN",
		ID:        info.CreatedID,
	}

	return claims, nil
}

// validateAuth 验证WebSocket中的认证信息（支持token和API Key双重认证）
func validateAuth(msgMap map[string]interface{}) (*utils.UserClaims, error) {
	// 规范化 key 为小写便于兼容不同客户端字段命名
	norm := make(map[string]interface{}, len(msgMap))
	for k, v := range msgMap {
		norm[strings.ToLower(k)] = v
	}

	// helpers
	getStr := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := norm[k]; ok {
				if s, isStr := v.(string); isStr && s != "" {
					return s
				}
				return fmt.Sprintf("%v", v)
			}
		}
		return ""
	}

	var tokenErr, apiKeyErr error
	tokenProvided := getStr("token", "authorization") != ""
	if tokenProvided {
		token := getStr("token")
		if token == "" {
			// try authorization header style
			token = getStr("authorization")
			token = strings.TrimPrefix(token, "Bearer ")
		}
		if token != "" {
			if claims, err := validateToken(token); err == nil {
				return claims, nil
			} else {
				tokenErr = err
				logrus.Warnf("Token validation failed: %v", err)
			}
		}
	}

	// 尝试 API Key（兼容多种命名）
	apiKeyCandidates := []string{"x-api-key", "x_api_key", "xapikey", "apikey"}
	apiKeyProvided := false
	for _, k := range apiKeyCandidates {
		if v := getStr(k); v != "" {
			apiKeyProvided = true
			if claims, err := validateAPIKey(v); err == nil {
				return claims, nil
			} else {
				apiKeyErr = err
				logrus.Warnf("API Key validation failed for key %s: %v", k, err)
			}
		}
	}

	// 决策：优先返回有意义的错误信息，而不是模糊的“未提供”
	switch {
	case tokenErr != nil && !apiKeyProvided:
		return nil, tokenErr
	case apiKeyErr != nil && !tokenProvided:
		return nil, apiKeyErr
	case tokenErr != nil && apiKeyErr != nil:
		return nil, fmt.Errorf("token validation failed: %v; api key validation failed: %v", tokenErr, apiKeyErr)
	case !tokenProvided && !apiKeyProvided:
		return nil, errors.New("authentication failed: token or x-api-key is required")
	default:
		// fallback generic
		return nil, errors.New("authentication failed")
	}
}

// keysOfMap returns slice of keys in the map (for debug logging)
func keysOfMap(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// ServeCurrentDataByWS 通过WebSocket处理设备实时遥测数据
// @Router   /api/v1/telemetry/datas/current/ws [get]
func (*TelemetryDataApi) ServeCurrentDataByWS(c *gin.Context) {
	// 升级HTTP连接为WebSocket连接
	conn, err := Wsupgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		c.Error(errcode.WithData(errcode.CodeSystemError, "WebSocket upgrade failed"))
		return
	}
	defer conn.Close()

	clientIP := conn.RemoteAddr().String()
	logrus.Info("收到新的WebSocket连接:", clientIP)

	// 读取客户端发送的第一条消息
	msgType, msg, err := conn.ReadMessage()
	if err != nil {
		logrus.Error("读取初始消息失败:", err)
		conn.WriteMessage(websocket.TextMessage, []byte("Failed to read message"))
		return
	}

	// 解析JSON格式消息
	var msgMap map[string]interface{}
	if err := json.Unmarshal(msg, &msgMap); err != nil {
		logrus.Error("JSON格式无效:", err)
		conn.WriteMessage(msgType, []byte("Invalid message format"))
		return
	}

	// 验证必要的字段
	deviceIDInterface, ok := msgMap["device_id"]
	if !ok {
		conn.WriteMessage(msgType, []byte("device_id is required"))
		return
	}

	deviceID, ok := deviceIDInterface.(string)
	if !ok || deviceID == "" {
		conn.WriteMessage(msgType, []byte("device_id must be a non-empty string"))
		return
	}

	// 验证认证信息（token或API Key）
	claims, err := validateAuth(msgMap)
	if err != nil {
		logrus.Error("认证失败:", err)
		conn.WriteMessage(msgType, []byte(err.Error()))
		return
	}

	logrus.Infof("WebSocket连接已建立 - 设备ID: %s, 用户ID: %s, 租户ID: %s", deviceID, claims.ID, claims.TenantID)
	// 生成唯一连接ID并注册到 WebSocket 管理器（先创建客户端写队列，避免后续写阻塞）
	connID := fmt.Sprintf("%s-%d", conn.RemoteAddr().String(), time.Now().UnixNano())
	var mu sync.Mutex
	wsClient := &global.WSClient{
		DeviceID: deviceID,
		TenantID: claims.TenantID,
		UserID:   claims.ID,
		Conn:     conn,
		ConnID:   connID,
		MsgType:  msgType,
		Mu:       &mu,
		Keys:     nil, // 订阅所有字段
		Send:     make(chan []byte, 64),
	}

	if err := global.TPWSManager.SubscribeDevice(deviceID, connID, wsClient); err != nil {
		logrus.Error("订阅设备失败:", err)
		conn.WriteMessage(msgType, []byte("Failed to subscribe to device"))
		return
	}
	defer func() {
		// 取消订阅并关闭写通道
		global.TPWSManager.UnsubscribeDevice(deviceID, connID)
		close(wsClient.Send)
	}()

	// 启动写入 goroutine（负责将缓冲消息写入 WebSocket，避免在主读循环或推送路径中直接写 Conn 导致阻塞）
	go func(c *global.WSClient) {
		defer func() {
			if r := recover(); r != nil {
				logrus.WithField("conn_id", c.ConnID).Warnf("writer goroutine recovered: %v", r)
			}
		}()
		for b := range c.Send {
			c.Mu.Lock()
			c.Conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			err := c.Conn.WriteMessage(c.MsgType, b)
			c.Mu.Unlock()
			if err != nil {
				logrus.WithError(err).WithField("conn_id", c.ConnID).Error("writer goroutine write failed")
				_ = global.TPWSManager.UnsubscribeDevice(c.DeviceID, c.ConnID)
				return
			}
		}
	}(wsClient)

	// 获取当前遥测数据
	data, err := service.GroupApp.TelemetryData.GetCurrentTelemetrDataForWs(deviceID)
	if err != nil {
		logrus.Error("获取遥测数据失败:", err)
		select {
		case wsClient.Send <- []byte("Failed to get telemetry data"):
		default:
		}
		return
	}

	// 如果有数据，通过写队列发送给客户端（避免阻塞）
	if data != nil {
		dataByte, err := json.Marshal(data)
		if err != nil {
			logrus.Error("序列化数据失败:", err)
			select {
			case wsClient.Send <- []byte("Failed to process telemetry data"):
			default:
			}
			return
		}
		select {
		case wsClient.Send <- dataByte:
		default:
			logrus.WithField("conn_id", connID).Warn("initial telemetry send buffer full, dropping initial data")
		}
	}

	// 处理心跳消息和超时检测
	lastPingTime := time.Now()
	heartbeatTimeout := 15 * time.Second // 前端15秒不发ping就断开

	// 心跳检测定时器
	heartbeatTicker := time.NewTicker(5 * time.Second)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-heartbeatTicker.C:
			// 检查心跳超时
			if time.Since(lastPingTime) > heartbeatTimeout {
				logrus.Warnf("WebSocket心跳超时，断开连接 - 设备ID: %s, 最后ping时间: %v",
					deviceID, lastPingTime.Format("2006-01-02 15:04:05"))

				// 发送关闭消息
				closeMsg := []byte("connection closed due to heartbeat timeout")
				deadline := time.Now().Add(time.Second)
				conn.WriteControl(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseGoingAway, string(closeMsg)),
					deadline)

				return
			}

		default:
			// 设置读取超时（稍大于心跳超时，避免频繁超时）
			conn.SetReadDeadline(time.Now().Add(heartbeatTimeout + 5*time.Second))
			_, msg, err := conn.ReadMessage()
			if err != nil {
				// 判断是否是超时错误
				if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
					// 这是正常的超时，继续心跳检测循环
					continue
				}

				// 记录真正的错误日志
				logrus.Error("WebSocket读取错误:", err)

				// 尝试发送错误消息给客户端
				closeMsg := []byte("connection closed due to error")
				deadline := time.Now().Add(time.Second)
				conn.WriteControl(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseInternalServerErr, string(closeMsg)),
					deadline)

				return
			}

			// 处理心跳消息
			if string(msg) == "ping" {
				// 更新最后ping时间
				lastPingTime = time.Now()

				// 续期Redis订阅
				if err := global.TPWSManager.RefreshSubscription(deviceID); err != nil {
					logrus.WithError(err).WithField("device_id", deviceID).Error("续期订阅失败")
					// 续期失败不中断连接，继续运行
				}

				// 回复pong（通过写队列）
				select {
				case wsClient.Send <- []byte("pong"):
				default:
					// 如果发送队列已满，尝试发送控制帧作为降级方式
					deadline := time.Now().Add(2 * time.Second)
					_ = conn.WriteControl(websocket.PongMessage, []byte{}, deadline)
				}

				logrus.Debugf("WebSocket心跳响应 - 设备ID: %s, ping时间: %v",
					deviceID, lastPingTime.Format("2006-01-02 15:04:05.000"))
			} else {
				// 收到非ping消息，记录但不处理（保持向后兼容）
				logrus.Debugf("收到非ping消息 - 设备ID: %s, 消息: %s", deviceID, string(msg))
			}
		}
	}
}

// ServeDeviceStatusByWS 通过WebSocket获取设备在线状态
// @Summary      获取设备在线状态
// @Description  通过WebSocket连接获取实时设备在线状态
// @Tags         设备
// @Accept       json
// @Produce      json
// @Router       /api/v1/device/online/status/ws [get]
func (*TelemetryDataApi) ServeDeviceStatusByWS(c *gin.Context) {
	// 升级WebSocket连接
	conn, err := Wsupgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		c.Error(errcode.WithData(errcode.CodeSystemError, "WebSocket upgrade failed"))
		return
	}
	defer conn.Close()

	clientIP := conn.RemoteAddr().String()
	logrus.Info("收到新的WebSocket连接:", clientIP)

	// 读取初始消息
	msgType, msg, err := conn.ReadMessage()
	if err != nil {
		logrus.Error("读取初始消息失败:", err)
		conn.WriteMessage(websocket.TextMessage, []byte("Failed to read message"))
		return
	}

	// 解析JSON消息
	var msgMap map[string]interface{}
	if err := json.Unmarshal(msg, &msgMap); err != nil {
		logrus.Error("JSON格式无效:", err)
		conn.WriteMessage(msgType, []byte("Invalid message format"))
		return
	}

	// 验证必要字段
	deviceIDInterface, ok := msgMap["device_id"]
	if !ok {
		conn.WriteMessage(msgType, []byte("device_id is required"))
		return
	}

	deviceID, ok := deviceIDInterface.(string)
	if !ok || deviceID == "" {
		conn.WriteMessage(msgType, []byte("device_id must be a non-empty string"))
		return
	}

	// 验证认证信息（token或API Key）
	claims, err := validateAuth(msgMap)
	if err != nil {
		logrus.Error("认证失败:", err)
		conn.WriteMessage(msgType, []byte(err.Error()))
		return
	}

	logrus.Infof("WebSocket连接已建立 - 设备ID: %s, 用户ID: %s, 租户ID: %s", deviceID, claims.ID, claims.TenantID)

	// 查询设备当前状态并立即推送 (保持原有格式: is_online 为整数 0/1)
	currentStatusMap, err := service.GroupApp.Device.GetDeviceOnlineStatus(deviceID)
	if err != nil {
		logrus.WithError(err).Error("查询设备当前状态失败")
		conn.WriteMessage(msgType, []byte("Failed to query device status"))
		return
	}

	// 提取在线状态 (整数格式)
	isOnline := 0
	if status, ok := currentStatusMap["is_online"]; ok {
		isOnline = status
	}

	// 发送当前状态 (简化格式,与原有接口保持一致)
	initialMsg := map[string]interface{}{
		"is_online": isOnline,
	}
	// 创建本地 WSClient 写队列并启动写goroutine，避免直接写 conn 导致阻塞
	var mu sync.Mutex
	localClient := &global.WSClient{
		DeviceID: deviceID,
		TenantID: claims.TenantID,
		UserID:   claims.ID,
		Conn:     conn,
		ConnID:   fmt.Sprintf("%s-%d", conn.RemoteAddr().String(), time.Now().UnixNano()),
		MsgType:  msgType,
		Mu:       &mu,
		Keys:     nil,
		Send:     make(chan []byte, 64),
	}
	// 启动写入 goroutine
	go func(c *global.WSClient) {
		defer func() {
			if r := recover(); r != nil {
				logrus.WithField("conn_id", c.ConnID).Warnf("writer goroutine recovered: %v", r)
			}
		}()
		for b := range c.Send {
			c.Mu.Lock()
			c.Conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			err := c.Conn.WriteMessage(c.MsgType, b)
			c.Mu.Unlock()
			if err != nil {
				logrus.WithError(err).WithField("conn_id", c.ConnID).Error("writer goroutine write failed")
				return
			}
		}
	}(localClient)

	if data, err := json.Marshal(initialMsg); err == nil {
		select {
		case localClient.Send <- data:
		default:
			logrus.WithField("conn_id", localClient.ConnID).Warn("status initial send buffer full, dropping initial data")
		}
	}

	// 订阅 Redis Pub/Sub 通道
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	channel := fmt.Sprintf("device:%s:status", deviceID)
	pubsub := global.REDIS.Subscribe(ctx, channel)
	defer pubsub.Close()

	logrus.Infof("WebSocket已订阅Redis通道: %s", channel)

	// 使用 sync.Mutex 保护 WebSocket 写操作（使用 localClient.Mu）

	// Goroutine 1: Redis消息转发到WebSocket
	go func() {
		ch := pubsub.Channel()
		for {
			select {
			case <-ctx.Done():
				logrus.Debug("Redis订阅上下文取消")
				return
			case redisMsg, ok := <-ch:
				if !ok {
					logrus.Warn("Redis通道已关闭")
					return
				}

				// 转发到WebSocket（通过本地写队列，避免阻塞Redis转发goroutine）
				payload := []byte(redisMsg.Payload)
				select {
				case localClient.Send <- payload:
				default:
					// 如果写队列已满，记录并丢弃，避免阻塞Redis消息处理
					logrus.WithField("device_id", deviceID).Warn("status send buffer full, dropping update")
				}
				logrus.WithField("device_id", deviceID).Debug("状态更新已排入发送队列")
			}
		}
	}()

	// Goroutine 2 (主): WebSocket心跳处理
	for {
		_, wsMsg, err := conn.ReadMessage()
		if err != nil {
			logrus.WithError(err).Info("WebSocket连接关闭")

			// 发送关闭消息
			closeMsg := []byte("connection closed")
			deadline := time.Now().Add(time.Second)
			conn.WriteControl(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, string(closeMsg)),
				deadline)

			cancel() // 通知Redis订阅goroutine退出
			return
		}

		// 处理心跳消息
		if string(wsMsg) == "ping" {
			// 回复pong（通过本地写队列）
			select {
			case localClient.Send <- []byte("pong"):
			default:
				deadline := time.Now().Add(2 * time.Second)
				_ = conn.WriteControl(websocket.PongMessage, []byte{}, deadline)
			}
		}
	}
}

// ServeCurrentDataByKey 根据key查询遥测当前值
// @Router /api/v1/telemetry/datas/current/keys/ws [get]
func (*TelemetryDataApi) ServeCurrentDataByKey(c *gin.Context) {
	// 升级WebSocket连接
	conn, err := Wsupgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		c.Error(errcode.WithData(errcode.CodeSystemError, "WebSocket upgrade failed"))
		return
	}
	defer conn.Close()

	clientIP := conn.RemoteAddr().String()
	logrus.Infof("收到新的WebSocket连接: %s", clientIP)

	// 读取初始消息
	msgType, msg, err := conn.ReadMessage()
	if err != nil {
		logrus.Error("读取初始消息失败:", err)
		conn.WriteMessage(websocket.TextMessage, []byte("Failed to read message"))
		return
	}

	// 解析JSON消息
	var msgMap map[string]interface{}
	if err := json.Unmarshal(msg, &msgMap); err != nil {
		logrus.Error("JSON格式无效:", err)
		conn.WriteMessage(msgType, []byte("Invalid message format"))
		return
	}

	// 验证并提取设备ID
	deviceIDInterface, ok := msgMap["device_id"]
	if !ok {
		conn.WriteMessage(msgType, []byte("device_id is required"))
		return
	}

	deviceID, ok := deviceIDInterface.(string)
	if !ok || deviceID == "" {
		conn.WriteMessage(msgType, []byte("device_id is required and must be string"))
		return
	}

	// 验证并提取keys
	keysInterface, ok := msgMap["keys"].([]interface{})
	if !ok {
		conn.WriteMessage(msgType, []byte("keys must be array"))
		return
	}

	// 转换keys为字符串数组
	var stringKeys []string
	for _, key := range keysInterface {
		strKey, ok := key.(string)
		if !ok || strKey == "" {
			conn.WriteMessage(msgType, []byte("keys must be non-empty strings"))
			return
		}
		stringKeys = append(stringKeys, strKey)
	}

	if len(stringKeys) == 0 {
		conn.WriteMessage(msgType, []byte("keys array cannot be empty"))
		return
	}

	// 验证认证信息（token或API Key）
	claims, err := validateAuth(msgMap)
	if err != nil {
		logrus.Error("认证失败:", err)
		conn.WriteMessage(msgType, []byte(err.Error()))
		return
	}

	logrus.Infof("WebSocket连接已建立 - 设备ID: %s, Keys: %v, 用户ID: %s, 租户ID: %s", deviceID, stringKeys, claims.ID, claims.TenantID)

	// 获取遥测数据
	data, err := service.GroupApp.TelemetryData.GetCurrentTelemetrDataKeysForWs(deviceID, stringKeys)
	if err != nil {
		logrus.Error("获取遥测数据失败:", err)
		conn.WriteMessage(msgType, []byte("Failed to get telemetry data"))
		return
	}

	// 生成唯一连接ID并注册到 WebSocket 管理器（先创建写队列）
	connID := fmt.Sprintf("%s-%d", conn.RemoteAddr().String(), time.Now().UnixNano())
	var mu sync.Mutex
	wsClient := &global.WSClient{
		DeviceID: deviceID,
		TenantID: claims.TenantID,
		UserID:   claims.ID,
		Conn:     conn,
		ConnID:   connID,
		MsgType:  msgType,
		Mu:       &mu,
		Keys:     stringKeys, // 订阅指定字段
		Send:     make(chan []byte, 64),
	}

	if err := global.TPWSManager.SubscribeDevice(deviceID, connID, wsClient); err != nil {
		logrus.Error("订阅设备失败:", err)
		conn.WriteMessage(msgType, []byte("Failed to subscribe to device"))
		return
	}
	defer func() {
		global.TPWSManager.UnsubscribeDevice(deviceID, connID)
		close(wsClient.Send)
	}()

	// 启动写入 goroutine
	go func(c *global.WSClient) {
		defer func() {
			if r := recover(); r != nil {
				logrus.WithField("conn_id", c.ConnID).Warnf("writer goroutine recovered: %v", r)
			}
		}()
		for b := range c.Send {
			c.Mu.Lock()
			c.Conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			err := c.Conn.WriteMessage(c.MsgType, b)
			c.Mu.Unlock()
			if err != nil {
				logrus.WithError(err).WithField("conn_id", c.ConnID).Error("writer goroutine write failed")
				_ = global.TPWSManager.UnsubscribeDevice(c.DeviceID, c.ConnID)
				return
			}
		}
	}(wsClient)

	// 发送数据给客户端（通过写队列）
	if data != nil {
		dataByte, err := json.Marshal(data)
		if err != nil {
			logrus.Error("序列化数据失败:", err)
			select {
			case wsClient.Send <- []byte("Failed to process telemetry data"):
			default:
			}
			return
		}
		select {
		case wsClient.Send <- dataByte:
		default:
			logrus.WithField("conn_id", connID).Warn("initial telemetry send buffer full, dropping initial data")
		}
	}

	// 处理心跳消息和超时检测
	lastPingTime := time.Now()
	heartbeatTimeout := 15 * time.Second // 前端15秒不发ping就断开

	// 心跳检测定时器
	heartbeatTicker := time.NewTicker(5 * time.Second)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-heartbeatTicker.C:
			// 检查心跳超时
			if time.Since(lastPingTime) > heartbeatTimeout {
				logrus.Warnf("WebSocket心跳超时，断开连接 - 设备ID: %s, 最后ping时间: %v",
					deviceID, lastPingTime.Format("2006-01-02 15:04:05"))

				// 发送关闭消息
				closeMsg := []byte("connection closed due to heartbeat timeout")
				deadline := time.Now().Add(time.Second)
				conn.WriteControl(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseGoingAway, string(closeMsg)),
					deadline)

				return
			}

		default:
			// 设置读取超时（稍大于心跳超时，避免频繁超时）
			conn.SetReadDeadline(time.Now().Add(heartbeatTimeout + 5*time.Second))
			_, msg, err := conn.ReadMessage()
			if err != nil {
				// 判断是否是超时错误
				if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
					// 这是正常的超时，继续心跳检测循环
					continue
				}

				// 记录真正的错误日志
				logrus.Error("WebSocket读取错误:", err)

				// 尝试发送错误消息给客户端
				closeMsg := []byte("connection closed due to error")
				deadline := time.Now().Add(time.Second)
				conn.WriteControl(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseInternalServerErr, string(closeMsg)),
					deadline)

				return
			}

			// 处理心跳消息
			if string(msg) == "ping" {
				// 更新最后ping时间
				lastPingTime = time.Now()

				// 续期Redis订阅
				if err := global.TPWSManager.RefreshSubscription(deviceID); err != nil {
					logrus.WithError(err).WithField("device_id", deviceID).Error("续期订阅失败")
					// 续期失败不中断连接，继续运行
				}

				// 回复pong（通过写队列）
				select {
				case wsClient.Send <- []byte("pong"):
				default:
					deadline := time.Now().Add(2 * time.Second)
					_ = conn.WriteControl(websocket.PongMessage, []byte{}, deadline)
				}

				logrus.Debugf("WebSocket心跳响应 - 设备ID: %s, ping时间: %v",
					deviceID, lastPingTime.Format("2006-01-02 15:04:05.000"))
			} else {
				// 收到非ping消息，记录但不处理（保持向后兼容）
				logrus.Debugf("收到非ping消息 - 设备ID: %s, 消息: %s", deviceID, string(msg))
			}
		}
	}
}

// ServeStatisticData 遥测统计数据查询
// @Router   /api/v1/telemetry/datas/statistic [get]
func (*TelemetryDataApi) ServeStatisticData(c *gin.Context) {
	var req model.GetTelemetryStatisticReq
	if !BindAndValidate(c, &req) {
		return
	}

	date, err := service.GroupApp.TelemetryData.GetTelemetrServeStatisticData(&req)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", date)
}

// /api/v1/telemetry/datas/pub
func (*TelemetryDataApi) TelemetryPutMessage(c *gin.Context) {
	var req model.PutMessage
	if !BindAndValidate(c, &req) {
		return
	}

	userClaims := c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.TelemetryData.TelemetryPutMessage(c, userClaims.ID, &req, strconv.Itoa(constant.Manual))
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// /api/v1/telemetry/datas/msg/count
func (*TelemetryDataApi) ServeMsgCountByTenant(c *gin.Context) {
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	if userClaims.TenantID == "" {
		c.Error(errcode.New(201001))
		return
	}
	cnt, err := service.GroupApp.TelemetryData.ServeMsgCountByTenantId(userClaims.TenantID)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", map[string]interface{}{"msg": cnt})
}

// 请求参数
// 设备ID: device_ids
// 遥测key: keys
// 时间类型: 时间单位 hour、day、week、month、year
// 数据数量: limit
// 聚合方式: 聚合方式: avg、sum、max、min、count、diff
// 批量查询多个设备的遥测统计数据
// @Router   /api/v1/telemetry/datas/statistic/batch [get]
func (*TelemetryDataApi) ServeStatisticDataByDeviceId(c *gin.Context) {
	var req model.GetTelemetryStatisticByDeviceIdReq
	if !BindAndValidate(c, &req) {
		return
	}

	data, err := service.GroupApp.TelemetryData.GetTelemetryStatisticDataByDeviceIds(&req)
	if err != nil {
		c.Error(err)
		return
	}

	c.Set("data", data)
}
