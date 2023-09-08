package mqtt

import (
	sendmqtt "ThingsPanel-Go/modules/dataService/mqtt/sendMqtt"
	"ThingsPanel-Go/services"
	"ThingsPanel-Go/utils"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"github.com/beego/beego/v2/server/web"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/panjf2000/ants"
	"github.com/spf13/viper"
)

var MqttClient mqtt.Client

func ListenNew(broker, username, password string) error {
	sendmqtt.InitTopic()
	var s services.TSKVService
	p1, err := ants.NewPool(500)
	if err != nil {
		return err
	}
	//defer p1.Release()

	pOther, err := ants.NewPool(50)
	if err != nil {
		return err
	}
	//defer pOther.Release()

	qos := byte(viper.GetUint("mqtt.qos"))

	channelBufferSize, err := web.AppConfig.Int("channel_buffer_size")
	if err != nil {
		return err
	}

	messages := make(chan map[string]interface{}, channelBufferSize)

	writeWorkers, _ := web.AppConfig.Int("write_workers")
	for i := 0; i < writeWorkers; i++ {
		go s.BatchWrite(messages)
	}

	var connectLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
		log.Println("Mqtt Connect lost:", err)
		for !MqttClient.IsConnected() {
			log.Println("Mqtt reconnecting...")
			if token := MqttClient.Connect(); token.Wait() && token.Error() != nil {
				log.Println(token.Error())
				time.Sleep(5 * time.Second)
			} else {
				log.Println("Mqtt reconnect success")
			}
		}

		if viper.GetString("mqtt_server") == "gmqtt" {
			sub(p1, pOther, qos, messages)
		}
	}

	opts := mqtt.NewClientOptions()
	opts.SetUsername(username)
	opts.SetPassword(password)
	opts.SetClientID(utils.GetUuid())
	opts.AddBroker(broker)
	opts.SetResumeSubs(true)
	opts.SetCleanSession(false)
	opts.SetAutoReconnect(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetMaxReconnectInterval(20 * time.Second)
	opts.SetOrderMatters(false)
	opts.OnConnectionLost = connectLostHandler

	opts.SetOnConnectHandler(func(c mqtt.Client) {
		log.Println("Mqtt client connected")
	})

	MqttClient = mqtt.NewClient(opts)
	for {
		if token := MqttClient.Connect(); token.Wait() && token.Error() != nil {
			log.Println(token.Error())
			time.Sleep(5 * time.Second)
		} else {
			break
		}
	}

	sub(p1, pOther, qos, messages)

	return nil
}
func sub(p1 *ants.Pool, pOther *ants.Pool, qos byte, messages chan map[string]interface{}) {
	var s services.TSKVService
	var device services.DeviceService
	var otaDevice services.TpOtaDeviceService
	// 订阅设备属性
	deviceAttributesMessageHandler := func(c mqtt.Client, d mqtt.Message) {
		_ = p1.Submit(func() {
			s.MsgProc(messages, d.Payload(), d.Topic())
		})
	}
	fmt.Println("订阅设备属性:", sendmqtt.Topic_DeviceAttributes)
	if token := MqttClient.Subscribe(sendmqtt.Topic_DeviceAttributes, qos, deviceAttributesMessageHandler); token.Wait() && token.Error() != nil {
		logs.Error(token.Error())
		os.Exit(1)
	}
	// 订阅设备状态
	deviceStatusMessageHandler := func(c mqtt.Client, d mqtt.Message) {
		_ = pOther.Submit(func() {
			s.MsgProcOther(d.Payload(), d.Topic())
		})
	}
	if token := MqttClient.Subscribe(sendmqtt.Topic_DeviceStatus, 1, deviceStatusMessageHandler); token.Wait() && token.Error() != nil {
		logs.Error(token.Error())
		os.Exit(1)
	}
	// 订阅设备事件
	deviceEventMessageHandler := func(c mqtt.Client, d mqtt.Message) {
		_ = pOther.Submit(func() {
			device.SubscribeDeviceEvent(d.Payload(), d.Topic())
		})
	}
	if token := MqttClient.Subscribe(sendmqtt.Topic_DeviceEvent, qos, deviceEventMessageHandler); token.Wait() && token.Error() != nil {
		logs.Error(token.Error())
		os.Exit(1)
	}
	// 订阅网关属性
	gatewayAttributesMessageHandler := func(c mqtt.Client, d mqtt.Message) {
		_ = p1.Submit(func() {
			s.GatewayMsgProc(d.Payload(), d.Topic(), messages)
		})
	}
	if token := MqttClient.Subscribe(sendmqtt.Topic_GatewayAttributes, qos, gatewayAttributesMessageHandler); token.Wait() && token.Error() != nil {
		logs.Error(token.Error())
		os.Exit(1)
	}
	// 订阅网关事件
	gatewayEventMessageHandler := func(c mqtt.Client, d mqtt.Message) {
		_ = pOther.Submit(func() {
			device.SubscribeGatwayEvent(d.Payload(), d.Topic())
		})
	}
	if token := MqttClient.Subscribe(sendmqtt.Topic_GatewayEvent, qos, gatewayEventMessageHandler); token.Wait() && token.Error() != nil {
		logs.Error(token.Error())
		os.Exit(1)
	}
	// 订阅ota升级进度
	otaDeviceProgressMessageHandler := func(c mqtt.Client, d mqtt.Message) {
		_ = pOther.Submit(func() {
			otaDevice.OtaProgressMsgProc(d.Payload(), d.Topic())
		})
	}
	if token := MqttClient.Subscribe(sendmqtt.Topic_OtaDeviceProgress, qos, otaDeviceProgressMessageHandler); token.Wait() && token.Error() != nil {
		logs.Error(token.Error())
		os.Exit(1)
	}
	// 订阅ota升级通知
	otaDeviceInformMessageHandler := func(c mqtt.Client, d mqtt.Message) {
		_ = pOther.Submit(func() {
			otaDevice.OtaToinformMsgProcOther(d.Payload(), d.Topic())
		})
	}
	if token := MqttClient.Subscribe(sendmqtt.Topic_OtaDeviceInform, qos, otaDeviceInformMessageHandler); token.Wait() && token.Error() != nil {
		logs.Error(token.Error())
		os.Exit(1)
	}
}
