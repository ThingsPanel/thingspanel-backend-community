package services

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"ThingsPanel-Go/initialize/redis"
	ws_mqtt "ThingsPanel-Go/modules/dataService/ws_mqtt"
	"ThingsPanel-Go/utils"

	"github.com/beego/beego/v2/core/logs"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/gorilla/websocket"
	"github.com/spf13/viper"
)

type TpWsCurrentData struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

// 验证token是否存在
func AuthenticateAndFetchTenantID(token string, deviceID string) (string, error) {
	if redis.GetStr(token) != "1" {
		return "", fmt.Errorf("token is not exist")
	}

	// 解析token
	userMsg, err := utils.ParseCliamsToken(token)
	if err != nil {
		return "", err
	}

	// 获取用户权限
	var userService UserService
	_, tenantID, err := userService.GetUserAuthorityById(userMsg.ID)
	if err != nil {
		return "", err
	}

	// 验证设备是否存在
	var deviceService DeviceService
	if !deviceService.IsDeviceExistByTenantIdAndDeviceId(tenantID, deviceID) {
		return "", fmt.Errorf("device is not exist")
	}

	return tenantID, nil
}

func (*TpWsCurrentData) CurrentData(w http.ResponseWriter, r *http.Request) {
	var upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logs.Error(err)
		return
	}
	defer ws.Close()

	clientIp := ws.RemoteAddr().String()
	log.Printf("Received: %s", clientIp)

	msgType, msg, err := ws.ReadMessage()
	if err != nil {
		logs.Error(err)
		return
	}

	var msgMap map[string]string
	if err := json.Unmarshal(msg, &msgMap); err != nil {
		logs.Error("断开连接", err)
		return
	}
	
	deviceID, ok2 := msgMap["device_id"]
	if !ok2 {
		errMsg := "device_id is missing"
		ws.WriteMessage(msgType, []byte(errMsg))
		return
	}
	// 共享数据接口判断
	shareId, IsExist := msgMap["shareId"]
	if IsExist {
		var sharedVisualizationService SharedVisualizationService
		isShared := sharedVisualizationService.IsDeviceIDShared(shareId, deviceID)
		if !isShared {
			// 异常退出并断开连接
			logs.Error("断开连接", err)
			// 回复错误信息
			ws.WriteMessage(msgType, []byte("device is not shared"))
			return
		}
	} else {
		token, ok := msgMap["token"]
		if !ok {
			errMsg := "token or device_id is missing"
			ws.WriteMessage(msgType, []byte(errMsg))
			return
		}
		// 验证token是否存在
		tenantID, err := AuthenticateAndFetchTenantID(token, deviceID)
		if err != nil {
			logs.Error("断开连接", err)
			ws.WriteMessage(msgType, []byte(err.Error()))
			return
		}
		// 验证设备是否存在
		var DeviceService DeviceService
		if !DeviceService.IsDeviceExistByTenantIdAndDeviceId(tenantID, msgMap["device_id"]) {
			// 异常退出并断开连接
			logs.Error("断开连接", err)
			// 回复错误信息
			ws.WriteMessage(msgType, []byte("device is not exist"))
			return
		}
	}
// 发送设备当前数据
	var TSKVService TSKVService
	var dataByte []byte
	data, err := TSKVService.GetCurrentData(msgMap["device_id"], nil)
	if err != nil {
		ws.WriteMessage(msgType, []byte("Error:"+err.Error()))
	} else {
		// 判断是否有数据
		if len(data) != 0 {
			// data转[]byte
			dataByte, err = json.Marshal(data)
			if err != nil {
				logs.Error(err)
				ws.WriteMessage(msgType, []byte(err.Error()))
			} else {
				ws.WriteMessage(msgType, dataByte)
			}
		}
	}

	// 订阅mqtt主题
	topic := viper.GetString("mqtt.topicToSubscribe") + "/" + deviceID
	var wsMqtt ws_mqtt.WsMqtt
	err = wsMqtt.NewMqttClient()
	if err != nil {
		logs.Error(err)
		ws.WriteMessage(msgType, []byte(err.Error()))
		return
	}
	wsMqtt.WsMqttClient.Subscribe(topic, 0, func(client mqtt.Client, message mqtt.Message) {
		type mqttPayload struct {
			Token  string `json:"token"`
			Values []byte `json:"values"`
		}

		var payload mqttPayload
		if err := json.Unmarshal(message.Payload(), &payload); err != nil {
			logs.Error(err)
		} else {
			// 给payload.Values加上时间戳
			// 先转map
			var valuesMap map[string]interface{}
			if err := json.Unmarshal(payload.Values, &valuesMap); err != nil {
				logs.Error(err)
				ws.WriteMessage(msgType, []byte(err.Error()))
				return
			}
			// 加时间，mqtt消息里没有系统时间，systime 2023-09-25 16:32:26
			valuesMap["systime"] = time.Now().Format("2006-01-02 15:04:05")
			// 转json
			payload.Values, err = json.Marshal(valuesMap)
			if err != nil {
				logs.Error(err)
				ws.WriteMessage(msgType, []byte(err.Error()))
				return
			}
			if err = ws.WriteMessage(msgType, payload.Values); err != nil {
				logs.Error(err)
				ws.WriteMessage(msgType, []byte(err.Error()))
				return
			}
		}

	})

	defer wsMqtt.WsMqttClient.Disconnect(250)
	// 循环读取消息
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			logs.Error(err)
			return
		}
		log.Printf("Received: %s", msg)
	}
}
