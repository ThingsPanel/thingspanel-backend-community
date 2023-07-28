package mqtt

import (
	"ThingsPanel-Go/services"
	"ThingsPanel-Go/utils"
	"fmt"
	"os"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/panjf2000/ants"
)

var MqttClient mqtt.Client

const (
	Qos0 = byte(0)
	Qos1 = byte(1)
	Qos2 = byte(2)
)

const (
	Topic_DeviceAttributes  = "device/attributes"
	Topic_DeviceStatus      = "device/status"
	Topic_OtaDeviceProgress = "ota/device/progress"
	Topic_OtaDeviceInform   = "ota/device/inform"
	Topic_DeviceCommand     = "device/command"
	Topic_DeviceEvent       = "device/event"
	Topic_GatewayAttributes = "gateway/attributes"
	Topic_GatewayEvent      = "gateway/event"
)

// 所有订阅的Topic
var TopicList = map[string]byte{
	Topic_DeviceAttributes:  Qos0,
	Topic_DeviceStatus:      Qos0,
	Topic_OtaDeviceProgress: Qos0,
	Topic_DeviceCommand:     Qos0,
	Topic_DeviceEvent:       Qos0,
	Topic_OtaDeviceInform:   Qos0,
	Topic_GatewayAttributes: Qos0,
	Topic_GatewayEvent:      Qos0,
}

func ListenNew(broker, username, password string) (err error) {
	opts := mqtt.NewClientOptions()
	opts.SetUsername(username)
	opts.SetPassword(password)
	opts.SetClientID(utils.GetUuid())
	opts.AddBroker(broker)
	// 自动重连
	opts.SetAutoReconnect(true)
	// 重连间隔时间
	opts.SetConnectRetryInterval(time.Duration(5) * time.Second)
	opts.SetOrderMatters(false)

	var s services.TSKVService
	var device services.DeviceService
	var otaDevice services.TpOtaDeviceService

	opts.SetDefaultPublishHandler(func(c mqtt.Client, m mqtt.Message) {
		s.MsgProc(m.Payload(), m.Topic())
	})

	MqttClient = mqtt.NewClient(opts)

	if token := MqttClient.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}
	p1, _ := ants.NewPool(500)
	pOther, _ := ants.NewPool(500)
	var messageHandler mqtt.MessageHandler = func(c mqtt.Client, d mqtt.Message) {
		switch d.Topic() {
		case Topic_DeviceAttributes: // device/attributes // topicToSubscribe
			_ = p1.Submit(func() {
				s.MsgProc(d.Payload(), d.Topic())
			})
		case Topic_DeviceStatus: // "device/status" // topicToStatus
			_ = pOther.Submit(func() {
				s.MsgProcOther(d.Payload(), d.Topic())
			})
		case Topic_DeviceEvent: // device/event // topicToEvent
			_ = p1.Submit(func() {
				device.SubscribeDeviceEvent(d.Payload(), d.Topic())
			})
		case Topic_OtaDeviceInform: // ota/device/inform // topicToInform
			_ = p1.Submit(func() {
				otaDevice.OtaToinformMsgProcOther(d.Payload(), d.Topic())
			})
		case Topic_OtaDeviceProgress: // ota/device/progress // topicToProgress
			_ = p1.Submit(func() {
				otaDevice.OtaProgressMsgProc(d.Payload(), d.Topic())
			})
		case Topic_GatewayAttributes: // gateway/attributes // gateway_topic
			_ = p1.Submit(func() {
				s.GatewayMsgProc(d.Payload(), d.Topic())
			})
		case Topic_GatewayEvent: // gateway/event // gateway_topic
			_ = p1.Submit(func() {
				device.SubscribeGatwayEvent(d.Payload(), d.Topic())
			})

		default:
			fmt.Println("undefine topic")
		}
	}
	// 批量订阅
	if token := MqttClient.SubscribeMultiple(TopicList, messageHandler); token.Wait() && token.Error() != nil {
		fmt.Println(token.Error())
		os.Exit(1)
	}

	return err
}
