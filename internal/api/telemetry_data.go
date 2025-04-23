package api

import (
	"encoding/json"
	"strconv"
	"sync"
	"time"

	ws_subscribe "project/mqtt/ws_subscribe"
	"project/pkg/constant"
	"project/pkg/errcode"
	"project/pkg/utils"

	model "project/internal/model"
	service "project/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
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

	// 获取X-real-ip (直接客户端IP)
	clientIP := c.Request.Header.Get("X-real-ip")

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
	var msgMap map[string]string
	if err := json.Unmarshal(msg, &msgMap); err != nil {
		logrus.Error("JSON格式无效:", err)
		conn.WriteMessage(msgType, []byte("Invalid message format"))
		return
	}

	// 验证必要的字段
	deviceID, ok := msgMap["device_id"]
	if !ok || deviceID == "" {
		conn.WriteMessage(msgType, []byte("device_id is required"))
		return
	}

	token, ok := msgMap["token"]
	if !ok || token == "" {
		conn.WriteMessage(msgType, []byte("token is required"))
		return
	}

	logrus.Infof("WebSocket连接已建立 - 设备ID: %s", deviceID)

	// 获取当前遥测数据
	data, err := service.GroupApp.TelemetryData.GetCurrentTelemetrDataForWs(deviceID)
	if err != nil {
		logrus.Error("获取遥测数据失败:", err)
		conn.WriteMessage(msgType, []byte("Failed to get telemetry data"))
		return
	}

	// 如果有数据，发送给客户端
	if data != nil {
		dataByte, err := json.Marshal(data)
		if err != nil {
			logrus.Error("序列化数据失败:", err)
			conn.WriteMessage(msgType, []byte("Failed to process telemetry data"))
			return
		}
		if err := conn.WriteMessage(msgType, dataByte); err != nil {
			logrus.Error("发送数据失败:", err)
			return
		}
	}

	// 订阅实时更新
	var mu sync.Mutex
	var mqttClient ws_subscribe.WsMqttClient
	if err := mqttClient.SubscribeDeviceTelemetry(deviceID, conn, msgType, &mu); err != nil {
		logrus.Error("订阅遥测数据失败:", err)
		conn.WriteMessage(msgType, []byte("Failed to subscribe to telemetry updates"))
		return
	}
	defer mqttClient.Close()

	// 处理心跳消息
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			// 记录错误日志
			logrus.Error("WebSocket读取错误:", err)

			// 尝试发送错误消息给客户端
			closeMsg := []byte("connection closed due to error")
			// 使用 WriteControl 发送关闭消息，设置1秒超时
			deadline := time.Now().Add(time.Second)
			conn.WriteControl(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseInternalServerErr, string(closeMsg)),
				deadline)

			// 现在可以安全退出了
			return
		}

		// 处理心跳消息
		if string(msg) == "ping" {
			mu.Lock()
			if err := conn.WriteMessage(msgType, []byte("pong")); err != nil {
				logrus.Error("发送pong消息失败:", err)

				// 尝试发送错误消息
				deadline := time.Now().Add(time.Second)
				conn.WriteControl(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "failed to send pong"),
					deadline)

				mu.Unlock()
				return
			}
			mu.Unlock()
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
	var msgMap map[string]string
	if err := json.Unmarshal(msg, &msgMap); err != nil {
		logrus.Error("JSON格式无效:", err)
		conn.WriteMessage(msgType, []byte("Invalid message format"))
		return
	}

	// 验证必要字段
	deviceID, ok := msgMap["device_id"]
	if !ok || deviceID == "" {
		conn.WriteMessage(msgType, []byte("device_id is required"))
		return
	}

	token, ok := msgMap["token"]
	if !ok || token == "" {
		conn.WriteMessage(msgType, []byte("token is required"))
		return
	}

	logrus.Infof("WebSocket连接已建立 - 设备ID: %s", deviceID)
	// TODO: 验证token

	// 订阅设备在线状态
	var mu sync.Mutex
	logrus.Info("User SubscribeOnlineOffline")
	var mqttClient ws_subscribe.WsMqttClient
	if err := mqttClient.SubscribeOnlineOffline(deviceID, conn, msgType, &mu); err != nil {
		logrus.Error("订阅设备状态失败:", err)
		conn.WriteMessage(msgType, []byte("Failed to subscribe to device status"))
		return
	}
	defer mqttClient.Close()

	// 处理心跳
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			// 记录错误日志
			logrus.Error("WebSocket读取错误:", err)

			// 尝试发送错误消息给客户端
			closeMsg := []byte("connection closed due to error")
			// 使用 WriteControl 发送关闭消息，设置1秒超时
			deadline := time.Now().Add(time.Second)
			conn.WriteControl(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseInternalServerErr, string(closeMsg)),
				deadline)

			// 现在可以安全退出了
			return
		}

		// 处理心跳消息
		if string(msg) == "ping" {
			mu.Lock()
			if err := conn.WriteMessage(msgType, []byte("pong")); err != nil {
				logrus.Error("发送pong消息失败:", err)

				// 尝试发送错误消息
				deadline := time.Now().Add(time.Second)
				conn.WriteControl(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "failed to send pong"),
					deadline)

				mu.Unlock()
				return
			}
			mu.Unlock()
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
	deviceID, ok := msgMap["device_id"].(string)
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

	// 验证token
	token, ok := msgMap["token"].(string)
	if !ok || token == "" {
		conn.WriteMessage(msgType, []byte("token is required"))
		return
	}
	// TODO: 验证token

	logrus.Infof("WebSocket连接已建立 - 设备ID: %s, Keys: %v", deviceID, stringKeys)

	// 获取遥测数据
	data, err := service.GroupApp.TelemetryData.GetCurrentTelemetrDataKeysForWs(deviceID, stringKeys)
	if err != nil {
		logrus.Error("获取遥测数据失败:", err)
		conn.WriteMessage(msgType, []byte("Failed to get telemetry data"))
		return
	}

	// 发送数据给客户端
	if data != nil {
		dataByte, err := json.Marshal(data)
		if err != nil {
			logrus.Error("序列化数据失败:", err)
			conn.WriteMessage(msgType, []byte("Failed to process telemetry data"))
			return
		}
		if err := conn.WriteMessage(msgType, dataByte); err != nil {
			logrus.Error("发送数据失败:", err)
			return
		}
	}

	// 订阅遥测更新
	var mu sync.Mutex
	var mqttClient ws_subscribe.WsMqttClient
	if err := mqttClient.SubscribeDeviceTelemetryByKeys(deviceID, conn, msgType, &mu, stringKeys); err != nil {
		logrus.Error("订阅遥测数据失败:", err)
		conn.WriteMessage(msgType, []byte("Failed to subscribe to telemetry updates"))
		return
	}
	defer mqttClient.Close()

	// 处理心跳
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			// 记录错误日志
			logrus.Error("WebSocket读取错误:", err)

			// 尝试发送错误消息给客户端
			closeMsg := []byte("connection closed due to error")
			// 使用 WriteControl 发送关闭消息，设置1秒超时
			deadline := time.Now().Add(time.Second)
			conn.WriteControl(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseInternalServerErr, string(closeMsg)),
				deadline)

			// 现在可以安全退出了
			return
		}

		// 处理心跳消息
		if string(msg) == "ping" {
			mu.Lock()
			if err := conn.WriteMessage(msgType, []byte("pong")); err != nil {
				logrus.Error("发送pong消息失败:", err)

				// 尝试发送错误消息
				deadline := time.Now().Add(time.Second)
				conn.WriteControl(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "failed to send pong"),
					deadline)

				mu.Unlock()
				return
			}
			mu.Unlock()
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
