package services

import (
	"encoding/json"
	"log"
	"net/http"

	ws_mqtt "ThingsPanel-Go/modules/dataService/ws_mqtt"

	"github.com/beego/beego/v2/core/logs"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/gorilla/websocket"
	"github.com/spf13/viper"
)

type TpWsEventData struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

func (*TpWsEventData) EventData(w http.ResponseWriter, r *http.Request) {
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

	token, ok := msgMap["token"]
	deviceID, ok2 := msgMap["device_id"]
	if !ok || !ok2 {
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

	topic := viper.GetString("mqtt.topicToEvent") + "/" + msgMap["device_id"]
	// 创建mqtt订阅
	var WsMqtt ws_mqtt.WsMqtt
	err = WsMqtt.NewMqttClient()
	if err != nil {
		logs.Error(err)
		ws.WriteMessage(msgType, []byte(err.Error()))
		return
	}
	WsMqtt.WsMqttClient.Subscribe(topic, 0, func(client mqtt.Client, message mqtt.Message) {
		type mqttPayload struct {
			Token  string `json:"token"`
			Values []byte `json:"values"`
		}
		// 解析Payload
		var payload mqttPayload
		if err := json.Unmarshal(message.Payload(), &payload); err != nil {
			logs.Error(err)
		} else {
			// 回复消息
			if err = ws.WriteMessage(msgType, payload.Values); err != nil {
				logs.Error(err)
				ws.WriteMessage(msgType, []byte(err.Error()))
				return
			}
		}

	})
	//取消订阅
	defer WsMqtt.WsMqttClient.Disconnect(250)
	// 等待客户端断开连接
	for {
		// 读取新的消息
		_, msg, err := ws.ReadMessage()
		if err != nil {
			logs.Error(err)
			return
		}
		log.Printf("Received: %s", msg)
	}
}
