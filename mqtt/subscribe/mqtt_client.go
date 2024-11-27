package subscribe

import (
	"os"
	"path"
	"time"

	"project/initialize"
	config "project/mqtt"

	"project/mqtt/publish"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/go-basic/uuid"
	"github.com/panjf2000/ants"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

var SubscribeMqttClient mqtt.Client
var TelemetryMessagesChan chan map[string]interface{}

func GenTopic(topic string) string {
	topic = path.Join("$share/mygroup", topic)
	return topic
}

func SubscribeInit() {

	//实例限流客户端
	initialize.NewAutomateLimiter()
	// 创建mqtt客户端
	subscribeMqttClient()
	// 创建消息队列
	telemetryMessagesChan()

	//消息订阅
	subscribe()
}

func subscribe() {
	// 订阅attribute消息
	SubscribeAttribute()
	// 订阅设置设备属性回应
	SubscribeSetAttribute()
	// 订阅event消息
	SubscribeEvent()
	//订阅telemetry消息
	err := SubscribeTelemetry()
	if err != nil {
		logrus.Error(err)
		os.Exit(1)
	}

	// 订阅在线离线消息
	//SubscribeDeviceStatus()

	//网关订阅主题
	GatewaySubscribeTopic()

	// 订阅设备命令消息
	SubscribeCommand()
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

// 创建消息队列
func telemetryMessagesChan() {
	TelemetryMessagesChan = make(chan map[string]interface{}, config.MqttConfig.ChannelBufferSize)
	writeWorkers := config.MqttConfig.WriteWorkers
	for i := 0; i < writeWorkers; i++ {
		go MessagesChanHandler(TelemetryMessagesChan)
	}
}

// 订阅telemetry消息
func SubscribeTelemetry() error {
	//如果配置了别的数据库，遥测数据不写入原来的库了
	dbType := viper.GetString("grpc.tptodb_type")
	if dbType == "TSDB" || dbType == "KINGBASE" || dbType == "POLARDB" {
		logrus.Infof("dbType:%v do not need subcribe topic: %v", dbType, config.MqttConfig.Telemetry.SubscribeTopic)
		return nil
	}

	p, err := ants.NewPool(config.MqttConfig.Telemetry.PoolSize)
	if err != nil {
		return err
	}
	deviceTelemetryMessageHandler := func(_ mqtt.Client, d mqtt.Message) {
		err = p.Submit(func() {
			// 处理消息
			TelemetryMessages(d.Payload(), d.Topic())
		})
		if err != nil {
			logrus.Error(err)
		}
	}

	topic := config.MqttConfig.Telemetry.SubscribeTopic
	topic = GenTopic(topic)
	logrus.Info("subscribe topic:", topic)

	qos := byte(config.MqttConfig.Telemetry.QoS)

	if token := SubscribeMqttClient.Subscribe(topic, qos, deviceTelemetryMessageHandler); token.Wait() && token.Error() != nil {
		logrus.Error(token.Error())
		os.Exit(1)
	}
	return nil
}

// 订阅attribute消息，暂不需要线程池，不需要消息队列
func SubscribeAttribute() {
	// 订阅attribute消息
	deviceAttributeHandler := func(_ mqtt.Client, d mqtt.Message) {
		// 处理消息
		logrus.Debug("attribute message:", string(d.Payload()))
		deviceNumber, messageId, err := DeviceAttributeReport(d.Payload(), d.Topic())
		logrus.Debug("响应设备属性上报", deviceNumber, err)
		if err != nil {
			logrus.Error(err)
		}
		if deviceNumber != "" && messageId != "" {
			// 响应设备属性上报
			publish.PublishAttributeResponseMessage(deviceNumber, messageId, err)
		}
	}
	topic := config.MqttConfig.Attributes.SubscribeTopic
	topic = GenTopic(topic)
	logrus.Info("subscribe topic:", topic)
	qos := byte(config.MqttConfig.Attributes.QoS)
	if token := SubscribeMqttClient.Subscribe(topic, qos, deviceAttributeHandler); token.Wait() && token.Error() != nil {
		logrus.Error(token.Error())
		os.Exit(1)
	}
}

func SubscribeSetAttribute() {
	// 订阅attribute消息
	deviceAttributeHandler := func(_ mqtt.Client, d mqtt.Message) {
		// 处理消息
		logrus.Debug("attribute message:", string(d.Payload()))
		DeviceSetAttributeResponse(d.Payload(), d.Topic())
	}
	topic := config.MqttConfig.Attributes.SubscribeResponseTopic
	topic = GenTopic(topic)
	logrus.Info("subscribe topic:", topic)
	qos := byte(config.MqttConfig.Attributes.QoS)
	if token := SubscribeMqttClient.Subscribe(topic, qos, deviceAttributeHandler); token.Wait() && token.Error() != nil {
		logrus.Error(token.Error())
		os.Exit(1)
	}
}

// 订阅command消息，暂不需要线程池，不需要消息队列
func SubscribeCommand() {
	// 订阅command消息
	deviceCommandHandler := func(_ mqtt.Client, d mqtt.Message) {
		// 处理消息
		messageID, err := DeviceCommand(d.Payload(), d.Topic())
		logrus.Debug("设备命令响应上报", messageID, err)
		if err != nil || messageID == "" {
			logrus.Debug("设备命令响应上报失败", messageID, err)
			logrus.Error(err)
		}
	}
	topic := config.MqttConfig.Commands.SubscribeTopic
	topic = GenTopic(topic)
	logrus.Info("subscribe topic:", topic)
	qos := byte(config.MqttConfig.Commands.QoS)
	if token := SubscribeMqttClient.Subscribe(topic, qos, deviceCommandHandler); token.Wait() && token.Error() != nil {
		logrus.Error(token.Error())
		os.Exit(1)
	}
}

// 订阅event消息，暂不需要线程池，不需要消息队列
func SubscribeEvent() {
	// 订阅event消息
	deviceEventHandler := func(_ mqtt.Client, d mqtt.Message) {
		// 处理消息
		logrus.Debug("event message:", string(d.Payload()))
		deviceNumber, messageId, method, err := DeviceEvent(d.Payload(), d.Topic())
		logrus.Debug("响应设备属性上报", deviceNumber, err)
		if err != nil {
			logrus.Error(err)
		}
		if deviceNumber != "" && messageId != "" {
			// 响应设备属性上报
			publish.PublishEventResponseMessage(deviceNumber, messageId, method, err)
		}
	}
	topic := config.MqttConfig.Events.SubscribeTopic
	qos := byte(config.MqttConfig.Events.QoS)
	if token := SubscribeMqttClient.Subscribe(topic, qos, deviceEventHandler); token.Wait() && token.Error() != nil {
		logrus.Error(token.Error())
		os.Exit(1)
	}
}

// 订阅设备上线离线消息
func SubscribeDeviceStatus() {
	// 订阅设备上线离线消息
	deviceOnlineHandler := func(_ mqtt.Client, d mqtt.Message) {
		logrus.Debug("接收来自broker的设备在线离线通知")
		// 处理消息
		DeviceOnline(d.Payload(), d.Topic())

	}
	onlineTopic := "devices/status/+"
	onlineTopic = GenTopic(onlineTopic)
	logrus.Info("subscribe topic:", onlineTopic)

	onlineQos := byte(1)
	if token := SubscribeMqttClient.Subscribe(onlineTopic, onlineQos, deviceOnlineHandler); token.Wait() && token.Error() != nil {
		logrus.Error(token.Error())
		os.Exit(1)
	}
}

func SubscribeOtaUpprogress() {
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
		os.Exit(1)
	}
}
