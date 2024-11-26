package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	ws_subscribe "project/mqtt/ws_subscribe"
	"project/pkg/constant"
	"project/pkg/utils"
	"strconv"
	"sync"

	model "project/internal/model"
	service "project/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type TelemetryDataApi struct{}

// GetCurrentData 设备当前值查询
// @Tags     遥测数据
// @Summary  设备当前值查询
// @Description 设备当前值查询，获取设备每个key的最新一条数据
// @accept    application/json
// @Produce   application/json
// @Param     id  path      string     true  "设备ID"
// @Success  200  {object}  ApiResponse  "删除用户成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/telemetry/datas/current/{id} [get]
func (a *TelemetryDataApi) GetCurrentData(c *gin.Context) {
	deviceId := c.Param("id")
	date, err := service.GroupApp.TelemetryData.GetCurrentTelemetrData(deviceId)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Get current data successfully", date)
}

// 根据设备ID和key查询遥测当前值
// @Router /api/v1/telemetry/datas/current/keys [get]
func (a *TelemetryDataApi) GetCurrentDataKeys(c *gin.Context) {
	var req model.GetTelemetryCurrentDataKeysReq
	if !BindAndValidate(c, &req) {
		return
	}

	date, err := service.GroupApp.TelemetryData.GetCurrentTelemetrDataKeys(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Get current data successfully", date)
}

// ServeHistoryData 设备历史数值查询
// @Tags     遥测数据
// @Summary  设备历史数值查询
// @Description 设备历史数值查询
// @accept    application/json
// @Produce   application/json
// @Param   data query model.GetTelemetryHistoryDataReq true "见下方JSON"
// @Success  200  {object}  ApiResponse  "删除用户成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/telemetry/datas/history [get]
func (a *TelemetryDataApi) ServeHistoryData(c *gin.Context) {
	var req model.GetTelemetryHistoryDataReq
	if !BindAndValidate(c, &req) {
		return
	}
	date, err := service.GroupApp.TelemetryData.GetTelemetrHistoryData(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Get history data successfully", date)
}

// DeleteData 删除数据
// @Tags     遥测数据
// @Summary  删除数据
// @Description 删除数据
// @accept    application/json
// @Produce   application/json
// @Param   data query model.DeleteTelemetryDataReq true "见下方JSON"
// @Success  200  {object}  ApiResponse  "删除成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/telemetry/datas/ [delete]
func (a *TelemetryDataApi) DeleteData(c *gin.Context) {
	var req model.DeleteTelemetryDataReq
	if !BindAndValidate(c, &req) {
		return
	}
	err := service.GroupApp.TelemetryData.DeleteTelemetrData(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Delete data successfully", nil)
}

// GetCurrentData 根据设备ID获取最新的一条遥测数据
// @Tags     遥测数据
// @Summary  根据设备ID获取最新的一条遥测数据
// @Description 根据设备ID获取最新的一条遥测数据
// @accept    application/json
// @Produce   application/json
// @Param     id  path      string     true  "设备ID"
// @Success  200  {object}  ApiResponse  "删除用户成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/telemetry/datas/current/detail/{id} [get]
func (a *TelemetryDataApi) ServeCurrentDetailData(c *gin.Context) {
	deviceId := c.Param("id")
	date, err := service.GroupApp.TelemetryData.GetCurrentTelemetrDetailData(deviceId)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get current detail data successfully", date)
}

// ServeHistoryData 设备历史数值查询（分页）
// @Router   /api/v1/telemetry/datas/history/pagination [get]
func (a *TelemetryDataApi) ServeHistoryDataByPage(c *gin.Context) {
	var req model.GetTelemetryHistoryDataByPageReq
	if !BindAndValidate(c, &req) {
		return
	}

	// 时间区间限制一个月内
	// if req.EndTime.Sub(req.StartTime) > time.Hour*24*30 {
	// 	ErrorHandler(c, http.StatusBadRequest, fmt.Errorf("time range should be within 30 days"))
	// 	return
	// }

	date, err := service.GroupApp.TelemetryData.GetTelemetrHistoryDataByPage(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Get history data successfully", date)
}

// ServeSetLogsDataListByPage 遥测数据下发记录查询（分页）
// @Tags     遥测数据
// @Summary  遥测数据下发记录查询（分页）
// @Description 遥测数据下发记录查询（分页）
// @accept    application/json
// @Produce   application/json
// @Param   data query model.GetTelemetrySetLogsListByPageReq true "见下方JSON"
// @Success  200  {object}  ApiResponse  "查询成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/telemetry/datas/set/logs [get]
func (a *TelemetryDataApi) ServeSetLogsDataListByPage(c *gin.Context) {
	var req model.GetTelemetrySetLogsListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}

	date, err := service.GroupApp.TelemetryData.GetTelemetrSetLogsDataListByPage(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Get history data successfully", date)
}

// 获取模拟设备发送遥测数据的回显数据
// /api/v1/telemetry/datas/simulation
func (a *TelemetryDataApi) ServeEchoData(c *gin.Context) {
	var req model.ServeEchoDataReq
	if !BindAndValidate(c, &req) {
		return
	}

	date, err := service.GroupApp.TelemetryData.ServeEchoData(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Get echo data successfully", date)
}

// 模拟设备发送遥测数据
// /api/v1/telemetry/datas/simulation
func (a *TelemetryDataApi) SimulationTelemetryData(c *gin.Context) {
	var req model.SimulationTelemetryDataReq
	if !BindAndValidate(c, &req) {
		return
	}
	_, err := service.GroupApp.TelemetryData.TelemetryPub(req.Command)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Simulation telemetry data successfully", nil)
}

// ServeHistoryData 设备遥测数据（WS）
// @Tags     遥测数据
// @Summary  设备遥测数据（WS）
// @Description 设备遥测数据（WS）
// @accept    application/json
// @Produce   application/json
// @Param   data query model.GetTelemetryHistoryDataByPageReq true "见下方JSON"
// @Security ApiKeyAuth
// @Router   /api/v1/telemetry/datas/current/ws [get]
func (t *TelemetryDataApi) ServeCurrentDataByWS(c *gin.Context) {
	conn, err := Wsupgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		c.String(http.StatusInternalServerError, "WebSocket升级失败: %v", err)
		return
	}
	defer conn.Close()
	clientIp := conn.RemoteAddr().String()
	logrus.Info("Received:", clientIp)

	// 读取首次请求
	msgType, msg, err := conn.ReadMessage()
	if err != nil {
		logrus.Error(err)
		return
	}

	// 校验msg是否为json格式
	var msgMap map[string]string
	if err := json.Unmarshal(msg, &msgMap); err != nil {
		logrus.Error("断开连接", err)
		return
	}

	// 获取device_id
	deviceID, ok := msgMap["device_id"]
	if !ok {
		errMsg := "device_id is missing"
		conn.WriteMessage(msgType, []byte(errMsg))
		return
	}

	// 获取token
	token, ok := msgMap["token"]
	if !ok {
		errMsg := "token or device_id is missing"
		conn.WriteMessage(msgType, []byte(errMsg))
		return
	}

	logrus.Info(fmt.Printf("device_id: %s, token: %s", deviceID, token))
	// TODO：验证token

	// 查询设备遥测当前数据并返回给客户端
	var dataByte []byte
	data, err := service.GroupApp.TelemetryData.GetCurrentTelemetrDataForWs(deviceID)
	if err != nil {
		c.String(http.StatusInternalServerError, "get telemetry current data: %v", err)
		return
	} else {
		// 判断是否有数据
		if data != nil {
			// data转[]byte
			dataByte, err = json.Marshal(data)
			if err != nil {
				logrus.Error(err)
				conn.WriteMessage(msgType, []byte(err.Error()))
			} else {
				conn.WriteMessage(msgType, dataByte)
			}
		}
	}
	var mu sync.Mutex
	logrus.Info("User SubscribeDeviceTelemetry")
	var mqttClient ws_subscribe.WsMqttClient
	err = mqttClient.SubscribeDeviceTelemetry(deviceID, conn, msgType, &mu)
	if err != nil {
		logrus.Error(err)
		conn.WriteMessage(msgType, []byte(err.Error()))
	}
	defer mqttClient.Close()
	// 循环读取消息
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			logrus.Error(err)
			return
		}
		logrus.Info(fmt.Printf("Received: %s", msg))
		if string(msg) == "ping" {
			mu.Lock()
			conn.WriteMessage(msgType, []byte("pong"))
			mu.Unlock()
		}
	}
}

// @Router   /api/v1/device/online/status/ws
func (t *TelemetryDataApi) ServeDeviceStatusByWS(c *gin.Context) {
	conn, err := Wsupgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		c.String(http.StatusInternalServerError, "WebSocket升级失败: %v", err)
		return
	}
	defer conn.Close()
	clientIp := conn.RemoteAddr().String()
	logrus.Info("Received:", clientIp)

	// 读取首次请求
	msgType, msg, err := conn.ReadMessage()
	if err != nil {
		logrus.Error(err)
		return
	}

	// 校验msg是否为json格式
	var msgMap map[string]string
	if err := json.Unmarshal(msg, &msgMap); err != nil {
		logrus.Error("断开连接", err)
		return
	}

	// 获取device_id
	deviceID, ok := msgMap["device_id"]
	if !ok {
		errMsg := "device_id is missing"
		conn.WriteMessage(msgType, []byte(errMsg))
		return
	}

	// 获取token
	token, ok := msgMap["token"]
	if !ok {
		errMsg := "token or device_id is missing"
		conn.WriteMessage(msgType, []byte(errMsg))
		return
	}

	logrus.Info(fmt.Printf("device_id: %s, token: %s", deviceID, token))
	// TODO：验证token

	var mu sync.Mutex
	logrus.Info("User SubscribeOnlineOffline")
	var mqttClient ws_subscribe.WsMqttClient
	err = mqttClient.SubscribeOnlineOffline(deviceID, conn, msgType, &mu)
	if err != nil {
		logrus.Error(err)
		conn.WriteMessage(msgType, []byte(err.Error()))
	}
	defer mqttClient.Close()
	// 循环读取消息
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			logrus.Error(err)
			return
		}
		logrus.Info(fmt.Printf("Received: %s", msg))
		if string(msg) == "ping" {
			mu.Lock()
			conn.WriteMessage(msgType, []byte("pong"))
			mu.Unlock()
		}
	}
}

// 根据key查询遥测当前值
// @Router /api/v1/telemetry/datas/current/keys/ws [get]
func (a *TelemetryDataApi) ServeCurrentDataByKey(c *gin.Context) {
	conn, err := Wsupgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		c.String(http.StatusInternalServerError, "WebSocket升级失败: %v", err)
		return
	}
	defer conn.Close()
	clientIp := conn.RemoteAddr().String()
	logrus.Info(fmt.Printf("Received: %s", clientIp))

	// 读取首次请求
	msgType, msg, err := conn.ReadMessage()
	if err != nil {
		logrus.Error(err)
		return
	}

	// 校验msg是否为json格式
	var msgMap map[string]interface{}
	if err := json.Unmarshal(msg, &msgMap); err != nil {
		logrus.Error("断开连接", err)
		return
	}

	// 获取device_id
	deviceID, ok := msgMap["device_id"]
	if !ok {
		errMsg := "device_id is missing"
		conn.WriteMessage(msgType, []byte(errMsg))
		return
	}

	// 获取keys
	keys, ok := msgMap["keys"]
	if !ok {
		errMsg := "keys is missing"
		conn.WriteMessage(msgType, []byte(errMsg))
		return
	}

	// 获取token
	token, ok := msgMap["token"]
	if !ok {
		errMsg := "token or device_id is missing"
		conn.WriteMessage(msgType, []byte(errMsg))
		return
	}

	logrus.Info(fmt.Printf("device_id: %s, token: %s", deviceID, token))
	// TODO：验证token

	// 查询设备遥测当前数据并返回给客户端
	var dataByte []byte
	// deviceID,keys转string和[]string
	d, ok := deviceID.(string)
	if !ok {
		errMsg := "data type error"
		conn.WriteMessage(msgType, []byte(errMsg))
		return
	}
	var stringKeys []string
	for _, key := range keys.([]interface{}) { // 这里假设我们知道 keys 是 []interface{}
		strKey, ok := key.(string)
		if !ok {
			errMsg := "data type error"
			conn.WriteMessage(msgType, []byte(errMsg))
			return
		}
		stringKeys = append(stringKeys, strKey)
	}
	if len(stringKeys) == 0 {
		errMsg := "keys is empty"
		conn.WriteMessage(msgType, []byte(errMsg))
		return
	}
	data, err := service.GroupApp.TelemetryData.GetCurrentTelemetrDataKeysForWs(d, stringKeys)
	if err != nil {
		c.String(http.StatusInternalServerError, "get telemetry current data: %v", err)
		return
	} else {
		// 判断是否有数据
		if data != nil {
			// data转[]byte
			dataByte, err = json.Marshal(data)
			if err != nil {
				logrus.Error(err)
				conn.WriteMessage(msgType, []byte(err.Error()))
			} else {
				conn.WriteMessage(msgType, dataByte)
			}
		}
	}
	var mu sync.Mutex
	logrus.Info("User SubscribeDeviceTelemetry")
	var mqttClient ws_subscribe.WsMqttClient
	err = mqttClient.SubscribeDeviceTelemetryByKeys(deviceID.(string), conn, msgType, &mu, stringKeys)
	if err != nil {
		logrus.Error(err)
		conn.WriteMessage(msgType, []byte(err.Error()))
	}
	defer mqttClient.Close()
	// 循环读取消息
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			logrus.Error(err)
			return
		}
		logrus.Debug(fmt.Printf("Received: %s", msg))
		if string(msg) == "ping" {
			mu.Lock()
			conn.WriteMessage(msgType, []byte("pong"))
			mu.Unlock()
		}

	}
}

// ServeStatisticData 遥测统计数据查询
// @Tags     遥测数据
// @Summary  遥测统计数据查询
// @Description 遥测统计数据查询
// @accept    application/json
// @Produce   application/json
// @Param   data query model.GetTelemetryStatisticReq true "见下方JSON"
// @Success  200  {object}  ApiResponse  "成功"
// @Failure  400  {object}  ApiResponse  "无效的请求数据"
// @Failure  422  {object}  ApiResponse  "数据验证失败"
// @Failure  500  {object}  ApiResponse  "服务器内部错误"
// @Security ApiKeyAuth
// @Router   /api/v1/telemetry/datas/statistic [get]
func (a *TelemetryDataApi) ServeStatisticData(c *gin.Context) {
	var req model.GetTelemetryStatisticReq
	if !BindAndValidate(c, &req) {
		return
	}

	date, err := service.GroupApp.TelemetryData.GetTelemetrServeStatisticData(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Get data successfully", date)
}

// /api/v1/telemetry/datas/pub
func (a *TelemetryDataApi) TelemetryPutMessage(c *gin.Context) {
	var req model.PutMessage
	if !BindAndValidate(c, &req) {
		return
	}

	userClaims := c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.TelemetryData.TelemetryPutMessage(c, userClaims.ID, &req, strconv.Itoa(constant.Manual))
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessOK(c)
}

// /api/v1/telemetry/datas/msg/count
func (a *TelemetryDataApi) ServeMsgCountByTenant(c *gin.Context) {
	userClaims := c.MustGet("claims").(*utils.UserClaims)
	if userClaims.TenantID == "" {
		ErrorHandler(c, http.StatusInternalServerError, fmt.Errorf("no tenantid"))
		return
	}
	cnt, err := service.GroupApp.TelemetryData.ServeMsgCountByTenantId(userClaims.TenantID)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Get msg count successfully", map[string]interface{}{"msg": cnt})
}
