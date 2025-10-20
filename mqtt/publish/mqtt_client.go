package publish

import (
	"fmt"
	"path"
	"time"

	config "project/mqtt"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

var mqttClient mqtt.Client

func PublishInit() {
	// 创建mqtt客户端
	CreateMqttClient()
}

type MqttPublish interface{}

// 创建mqtt客户端
func CreateMqttClient() {
	// 初始化配置
	opts := mqtt.NewClientOptions()
	opts.AddBroker(config.MqttConfig.Broker)
	opts.SetUsername(config.MqttConfig.User)
	opts.SetPassword(config.MqttConfig.Pass)
	opts.SetClientID("thingspanel-go-pub-" + uuid.New()[0:8])
	// 干净会话
	opts.SetCleanSession(true)
	// 恢复客户端订阅，需要broker支持
	opts.SetResumeSubs(true)
	// 自动重连
	opts.SetAutoReconnect(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetMaxReconnectInterval(20 * time.Second)
	// 消息顺序
	opts.SetOrderMatters(false)
	opts.SetOnConnectHandler(func(_ mqtt.Client) {
		logrus.Println("mqtt connect success")
	})
	// 断线重连
	opts.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		logrus.Println("mqtt connect  lost: ", err)
		mqttClient.Disconnect(250)
		// 等待连接成功，失败重新连接
		for {
			token := mqttClient.Connect()
			if token.Wait() && token.Error() == nil {
				fmt.Println("Reconnected to MQTT broker")
				break
			}
			fmt.Printf("Reconnect failed: %v\n", token.Error())
			time.Sleep(5 * time.Second)
		}
	})

	mqttClient = mqtt.NewClient(opts)
	for {
		if token := mqttClient.Connect(); token.Wait() && token.Error() != nil {
			logrus.Error("MQTT Broker 1 连接失败:", token.Error())
			time.Sleep(5 * time.Second)
			continue
		}
		break
	}
}

// PublishOtaAdress 发送ota版本包消息给直连设备
// 保留此函数用于 OTA 功能（企业版兼容性）
func PublishOtaAdress(deviceNumber string, payload []byte) error {
	topic := config.MqttConfig.OTA.PublishTopic + deviceNumber
	qos := byte(config.MqttConfig.OTA.QoS)
	// 发布消息
	token := mqttClient.Publish(topic, qos, false, payload)
	if token.Wait() && token.Error() != nil {
		logrus.Error(token.Error())
	}
	return token.Error()
}

// PublishOnlineMessage 发送在线离线消息
// 保留此函数用于模拟设备功能
func PublishOnlineMessage(deviceID string, payload []byte) error {
	topic := fmt.Sprintf("devices/status/%s", deviceID)
	topic = path.Join("$share/mygroup", topic)
	qos := byte(0)
	// 发布消息
	token := mqttClient.Publish(topic, qos, false, payload)
	if token.Wait() && token.Error() != nil {
		logrus.Error(token.Error())
	}
	return token.Error()
}

// GetMQTTClient 获取全局 MQTT 客户端（供其他模块使用）
// 保留此函数以防有其他地方使用
func GetMQTTClient() mqtt.Client {
	return mqttClient
}
