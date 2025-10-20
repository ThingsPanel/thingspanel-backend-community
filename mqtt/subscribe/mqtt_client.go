package subscribe

import (
	"path"
	"time"

	"project/initialize"
	config "project/mqtt"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

var SubscribeMqttClient mqtt.Client
var TelemetryMessagesChan chan map[string]interface{}

func GenTopic(topic string) string {
	topic = path.Join("$share/mygroup", topic)
	return topic
}

func SubscribeInit() error {

	//实例限流客户端
	initialize.NewAutomateLimiter()
	// 创建mqtt客户端
	subscribeMqttClient()
	// 创建消息队列（已废弃，遥测数据现在通过 Flow 层处理）
	// telemetryMessagesChan()

	//消息订阅
	err := subscribe()
	return err
}

func subscribe() error {

	// 订阅OTA命令消息（暂未迁移到Adapter）
	var err error
	err = SubscribeOtaUpprogress()
	if err != nil {
		logrus.Error(err)
		return err
	}
	return nil
}

func subscribeMqttClient() {
	// 初始化配置
	opts := mqtt.NewClientOptions()
	opts.AddBroker(config.MqttConfig.Broker)
	opts.SetUsername(config.MqttConfig.User)
	opts.SetPassword(config.MqttConfig.Pass)
	id := "thingspanel-go-sub-" + uuid.New()[0:8]
	opts.SetClientID(id)
	logrus.Info("clientid: ", id)

	// 干净会话
	opts.SetCleanSession(true)
	// 恢复客户端订阅，需要broker支持
	opts.SetResumeSubs(true)
	// 自动重连
	opts.SetAutoReconnect(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetMaxReconnectInterval(200 * time.Second)
	// 消息顺序
	opts.SetOrderMatters(false)
	opts.SetOnConnectHandler(func(_ mqtt.Client) {
		logrus.Println("mqtt connect success")
	})
	// 断线重连
	opts.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		logrus.Println("mqtt connect  lost: ", err)
		SubscribeMqttClient.Disconnect(250)
		for {
			if token := SubscribeMqttClient.Connect(); token.Wait() && token.Error() != nil {
				logrus.Error("MQTT Broker 1 连接失败:", token.Error())
				time.Sleep(5 * time.Second)
				continue
			}
			subscribe()
			break
		}
	})

	SubscribeMqttClient = mqtt.NewClient(opts)
	// 等待连接成功，失败重新连接
	for {
		if token := SubscribeMqttClient.Connect(); token.Wait() && token.Error() != nil {
			logrus.Error("MQTT Broker 1 连接失败:", token.Error())
			time.Sleep(5 * time.Second)
			continue
		}
		break
	}

}

func SubscribeOtaUpprogress() error {
	// 订阅ota升级消息
	otaUpgradeHandler := func(_ mqtt.Client, d mqtt.Message) {
		// 处理消息
		logrus.Debug("ota upgrade message:", string(d.Payload()))
		OtaUpgrade(d.Payload(), d.Topic())
	}
	topic := config.MqttConfig.OTA.SubscribeTopic
	qos := byte(config.MqttConfig.OTA.QoS)
	if token := SubscribeMqttClient.Subscribe(topic, qos, otaUpgradeHandler); token.Wait() && token.Error() != nil {
		logrus.Error(token.Error())
		return token.Error()
	}
	return nil
}
