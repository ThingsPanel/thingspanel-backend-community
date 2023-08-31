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

func (*TpWsEventData) EventData(w http.ResponseWriter, r *http.Request, tenantId string) {
	var upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	// 获取头信息示例
	key := r.Header.Get("Authorization")
	log.Printf("Received: %s", key)
	// key = "Bearer "+ token,获取token
	// 升级初始 GET 请求为 websocket
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logs.Error(err)
		return
	}

	// 关闭连接
	defer ws.Close()
	// 获取ip信息
	headers := ws.RemoteAddr().String()
	//[]byte转string
	ms := string(headers)
	log.Printf("Received: %s", ms)

	// 读取新的消息
	msgType, msg, err := ws.ReadMessage()
	if err != nil {
		logs.Error(err)
		return
	}
	log.Printf("Received: %s", msg)
	// 创建map
	var msgMap map[string]interface{}
	// 解析json
	if err := json.Unmarshal([]byte(msg), &msgMap); err != nil {
		// 异常退出并断开连接
		logs.Error("断开连接", err)
		// 回复错误信息
		ws.WriteMessage(msgType, []byte(err.Error()))
		return
	} else {
		if _, ok := msgMap["device_id"]; !ok {
			// 异常退出并断开连接
			logs.Error("断开连接", err)
			// 回复错误信息
			ws.WriteMessage(msgType, []byte("device_id is missing"))
			return
		}
		// 验证设备是否存在
		var DeviceService DeviceService
		if !DeviceService.IsDeviceExistByTenantIdAndDeviceId(tenantId, msgMap["device_id"].(string)) {
			// 异常退出并断开连接
			logs.Error("断开连接", err)
			// 回复错误信息
			ws.WriteMessage(msgType, []byte("device is not exist"))
			return
		}

	}
	topic := viper.GetString("mqtt.topicToEvent") + "/" + msgMap["device_id"].(string)
	// 创建mqtt订阅
	ws_mqtt.WsMqttClient.Subscribe(topic, 0, func(client mqtt.Client, message mqtt.Message) {
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
				return
			}
		}

	})
	//取消订阅
	defer ws_mqtt.WsMqttClient.Unsubscribe(topic)
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
