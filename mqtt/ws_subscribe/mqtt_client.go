package ws_publish

import (
	"encoding/json"
	"strconv"
	"sync"
	"time"

	config "project/mqtt"
	"project/mqtt/subscribe"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/go-basic/uuid"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

type WsMqttClient struct {
	Client mqtt.Client
}

func (w *WsMqttClient) CreateMqttClient() error {
	// 初始化配置
	opts := mqtt.NewClientOptions()
	opts.AddBroker(config.MqttConfig.Broker)
	opts.SetUsername(config.MqttConfig.User)
	opts.SetPassword(config.MqttConfig.Pass)
	opts.SetClientID("ws_mqtt_" + uuid.New()[0:8])
	// 干净会话
	opts.SetCleanSession(true)
	// 恢复客户端订阅，需要broker支持
	opts.SetResumeSubs(false)
	// 自动重连
	opts.SetAutoReconnect(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetMaxReconnectInterval(20 * time.Second)
	// 消息顺序
	opts.SetOrderMatters(false)
	opts.SetOnConnectHandler(func(_ mqtt.Client) {
		logrus.Println("ws mqtt connect success")
	})

	w.Client = mqtt.NewClient(opts)
	if token := w.Client.Connect(); token.Wait() && token.Error() != nil {
		logrus.Error("Ws MQTT Broker 连接失败:", token.Error())
		return token.Error()
	}
	return nil
}

// 前端订阅单设备遥测消息
func (w *WsMqttClient) SubscribeDeviceTelemetry(deviceId string, conn *websocket.Conn, msgType int, mu *sync.Mutex) error {
	err := w.CreateMqttClient()
	if err != nil {
		return err
	}
	// 订阅单设备遥测消息
	deviceTelemetryHandler := func(_ mqtt.Client, d mqtt.Message) {
		// 处理消息
		var valuesMap map[string]interface{}
		if err := json.Unmarshal(d.Payload(), &valuesMap); err != nil {
			logrus.Error(err)
			mu.Lock()
			conn.WriteMessage(msgType, []byte(err.Error()))
			mu.Unlock()
			return
		}
		// 加时间，mqtt消息里没有系统时间
		valuesMap["systime"] = time.Now().UTC()
		// 转json
		data, err := json.Marshal(valuesMap)
		if err != nil {
			logrus.Error(err)
			mu.Lock()
			conn.WriteMessage(msgType, []byte(err.Error()))
			mu.Unlock()
			return
		}
		mu.Lock()
		err = conn.WriteMessage(msgType, data)
		mu.Unlock()
		if err != nil {
			logrus.Error(err)
			conn.WriteMessage(msgType, []byte(err.Error()))
			return
		}
	}
	telemetryTopic := config.MqttConfig.Telemetry.SubscribeTopic + "/" + deviceId
	telemetryQos := byte(0)
	if token := w.Client.Subscribe(telemetryTopic, telemetryQos, deviceTelemetryHandler); token.Wait() && token.Error() != nil {
		logrus.Error(token.Error())
		return token.Error()
	}
	return nil
}

// 前端订阅单设备遥测消息
func (w *WsMqttClient) SubscribeDeviceTelemetryByKeys(deviceId string, conn *websocket.Conn, msgType int, mu *sync.Mutex, keys []string) error {
	err := w.CreateMqttClient()
	if err != nil {
		return err
	}
	// 订阅单设备遥测消息
	deviceTelemetryHandler := func(_ mqtt.Client, d mqtt.Message) {
		// 处理消息
		var valuesMap map[string]interface{}
		rspMap := make(map[string]interface{})
		if err := json.Unmarshal(d.Payload(), &valuesMap); err != nil {
			logrus.Error(err)
			mu.Lock()
			conn.WriteMessage(msgType, []byte(err.Error()))
			mu.Unlock()
			return
		}
		// 遍历keys
		for _, key := range keys {
			if value, ok := valuesMap[key]; ok {
				rspMap[key] = value
			}
		}
		// 加时间，mqtt消息里没有系统时间
		rspMap["systime"] = time.Now().UTC()
		// 转json
		data, err := json.Marshal(rspMap)
		if err != nil {
			logrus.Error(err)
			mu.Lock()
			conn.WriteMessage(msgType, []byte(err.Error()))
			mu.Unlock()
			return
		}
		mu.Lock()
		err = conn.WriteMessage(msgType, data)
		mu.Unlock()
		if err != nil {
			logrus.Error(err)
			conn.WriteMessage(msgType, []byte(err.Error()))
			return
		}
	}
	telemetryTopic := config.MqttConfig.Telemetry.SubscribeTopic + "/" + deviceId
	telemetryQos := byte(0)
	if token := w.Client.Subscribe(telemetryTopic, telemetryQos, deviceTelemetryHandler); token.Wait() && token.Error() != nil {
		logrus.Error(token.Error())
		return token.Error()
	}
	return nil
}

// 订阅在线离线消息
func (w *WsMqttClient) SubscribeOnlineOffline(deviceId string, conn *websocket.Conn, msgType int, mu *sync.Mutex) error {
	err := w.CreateMqttClient()
	if err != nil {
		return err
	}
	// 订阅在线离线消息
	onlineOfflineHandler := func(_ mqtt.Client, d mqtt.Message) {
		// 处理消息

		payloadInt, err := strconv.Atoi(string(d.Payload()))
		if err != nil {
			logrus.Error(err.Error())
			return
		}
		// 放到map里
		payloadMap := make(map[string]interface{})
		payloadMap["is_online"] = payloadInt
		// 转json
		data, err := json.Marshal(payloadMap)
		if err != nil {
			logrus.Error(err)
			mu.Lock()
			conn.WriteMessage(msgType, []byte(err.Error()))
			mu.Unlock()
			return
		}
		mu.Lock()
		err = conn.WriteMessage(msgType, data)
		mu.Unlock()
		if err != nil {
			logrus.Error(err)
			conn.WriteMessage(msgType, []byte(err.Error()))
			return
		}
	}
	onlineOfflineTopic := "devices/status/" + deviceId
	onlineOfflineTopic = subscribe.GenTopic(onlineOfflineTopic)
	onlineOfflineQos := byte(0)
	if token := w.Client.Subscribe(onlineOfflineTopic, onlineOfflineQos, onlineOfflineHandler); token.Wait() && token.Error() != nil {
		logrus.Error(token.Error())
		return token.Error()
	}
	return nil
}

// 关闭连接
func (w *WsMqttClient) Close() {
	w.Client.Disconnect(250)
}
