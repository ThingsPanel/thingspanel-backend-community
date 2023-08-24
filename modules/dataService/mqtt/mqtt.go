package mqtt

import (
	sendmqtt "ThingsPanel-Go/modules/dataService/mqtt/sendMqtt"
	"ThingsPanel-Go/services"
	"ThingsPanel-Go/utils"
	"fmt"
	"os"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"github.com/beego/beego/v2/server/web"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/panjf2000/ants"
	"github.com/spf13/viper"
)

var MqttClient mqtt.Client

func ListenNew(broker, username, password string) (err error) {
	sendmqtt.InitTopic()
	var connectLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
		fmt.Printf("Mqtt Connect lost: %v", err)
		i := 0
		for {
			time.Sleep(5 * time.Second)
			if !MqttClient.IsConnected() {
				fmt.Println("Mqtt reconnecting...")
				if token := MqttClient.Connect(); token.Wait() && token.Error() != nil {
					fmt.Println(token.Error())
					i++
				} else {
					fmt.Println("Mqtt reconnect success")
					break
				}
			} else {
				break
			}
		}
	}
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
	opts.OnConnectionLost = connectLostHandler
	var s services.TSKVService
	var device services.DeviceService
	var otaDevice services.TpOtaDeviceService

	// opts.SetDefaultPublishHandler(func(c mqtt.Client, m mqtt.Message) {
	// 	s.MsgProc(m.Payload(), m.Topic())
	// })
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		fmt.Println("Mqtt客户端已连接")
	})
	opts.SetCleanSession(false)
	MqttClient = mqtt.NewClient(opts)
	for {
		if token := MqttClient.Connect(); token.Wait() && token.Error() != nil {
			fmt.Println(token.Error())
			time.Sleep(5 * time.Second)
		} else {
			break
		}
	}
	p1, _ := ants.NewPool(500) //设备属性，网关属性
	pOther, _ := ants.NewPool(50)
	var qos = byte(viper.GetUint("mqtt.qos"))
	// 启动批量写入
	// 通道缓冲区大小
	channelBufferSize, err := web.AppConfig.Int("channel_buffer_size")
	if err != nil {
		logs.Error("channelBufferSize:", err)
	}
	messages := make(chan map[string]interface{}, channelBufferSize)
	// 写入协程数
	writeWorkers, _ := web.AppConfig.Int("write_workers")
	for i := 0; i < writeWorkers; i++ {
		go s.BatchWrite(messages)
	}
	// 订阅设备属性
	deviceAttributesMessageHandler := func(c mqtt.Client, d mqtt.Message) {
		_ = p1.Submit(func() {
			s.MsgProc(messages, d.Payload(), d.Topic())
		})
	}
	fmt.Println("订阅设备属性:", sendmqtt.Topic_DeviceAttributes)
	if token := MqttClient.Subscribe(sendmqtt.Topic_DeviceAttributes, qos, deviceAttributesMessageHandler); token.Wait() && token.Error() != nil {
		fmt.Println(token.Error())
		os.Exit(1)
	}
	// 订阅设备状态
	deviceStatusMessageHandler := func(c mqtt.Client, d mqtt.Message) {
		_ = pOther.Submit(func() {
			s.MsgProcOther(d.Payload(), d.Topic())
		})
	}
	if token := MqttClient.Subscribe(sendmqtt.Topic_DeviceStatus, 1, deviceStatusMessageHandler); token.Wait() && token.Error() != nil {
		fmt.Println(token.Error())
		os.Exit(1)
	}
	// 订阅设备事件
	deviceEventMessageHandler := func(c mqtt.Client, d mqtt.Message) {
		_ = pOther.Submit(func() {
			device.SubscribeDeviceEvent(d.Payload(), d.Topic())
		})
	}
	if token := MqttClient.Subscribe(sendmqtt.Topic_DeviceEvent, qos, deviceEventMessageHandler); token.Wait() && token.Error() != nil {
		fmt.Println(token.Error())
		os.Exit(1)
	}
	// 订阅网关属性
	gatewayAttributesMessageHandler := func(c mqtt.Client, d mqtt.Message) {
		_ = p1.Submit(func() {
			s.GatewayMsgProc(d.Payload(), d.Topic())
		})
	}
	if token := MqttClient.Subscribe(sendmqtt.Topic_GatewayAttributes, qos, gatewayAttributesMessageHandler); token.Wait() && token.Error() != nil {
		fmt.Println(token.Error())
		os.Exit(1)
	}
	// 订阅网关事件
	gatewayEventMessageHandler := func(c mqtt.Client, d mqtt.Message) {
		_ = pOther.Submit(func() {
			device.SubscribeGatwayEvent(d.Payload(), d.Topic())
		})
	}
	if token := MqttClient.Subscribe(sendmqtt.Topic_GatewayEvent, qos, gatewayEventMessageHandler); token.Wait() && token.Error() != nil {
		fmt.Println(token.Error())
		os.Exit(1)
	}
	// 订阅ota升级进度
	otaDeviceProgressMessageHandler := func(c mqtt.Client, d mqtt.Message) {
		_ = pOther.Submit(func() {
			otaDevice.OtaProgressMsgProc(d.Payload(), d.Topic())
		})
	}
	if token := MqttClient.Subscribe(sendmqtt.Topic_OtaDeviceProgress, qos, otaDeviceProgressMessageHandler); token.Wait() && token.Error() != nil {
		fmt.Println(token.Error())
		os.Exit(1)
	}
	// 订阅ota升级通知
	otaDeviceInformMessageHandler := func(c mqtt.Client, d mqtt.Message) {
		_ = pOther.Submit(func() {
			otaDevice.OtaToinformMsgProcOther(d.Payload(), d.Topic())
		})
	}
	if token := MqttClient.Subscribe(sendmqtt.Topic_OtaDeviceInform, qos, otaDeviceInformMessageHandler); token.Wait() && token.Error() != nil {
		fmt.Println(token.Error())
		os.Exit(1)
	}

	return err
}
