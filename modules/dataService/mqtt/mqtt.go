package mqtt

import (
	"ThingsPanel-Go/utils"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/beego/beego/v2/core/logs"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/spf13/viper"
)

var running bool
var _client mqtt.Client

func Listen(broker, username, password, clientid string, msgProc func(c mqtt.Client, m mqtt.Message), msgProcOther func(c mqtt.Client, m mqtt.Message), gatewayMsgProc func(c mqtt.Client, m mqtt.Message)) (err error) {
	running = false
	if _client == nil {
		// 掉线重连
		var connectLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
			fmt.Printf("Mqtt Connect lost: %v", err)
			i := 0
			for {

				time.Sleep(5 * time.Second)
				if !_client.IsConnectionOpen() {
					i++
					fmt.Println("MQTT掉线重连...", i)
				} else {
					subscribe(msgProcOther, gatewayMsgProc)
					break
				}
			}
		}
		opts := mqtt.NewClientOptions()
		fmt.Println(broker + username + password + clientid)
		opts.SetUsername(username)
		opts.SetPassword(password)
		opts.SetClientID(clientid)
		opts.AddBroker(broker)
		opts.SetAutoReconnect(true)
		opts.SetOrderMatters(false)
		opts.OnConnectionLost = connectLostHandler
		opts.SetOnConnectHandler(func(c mqtt.Client) {
			if !running {
				fmt.Println("MQTT CONNECT SUCCESS -- ", broker)
			}
			running = true
		})
		opts.SetDefaultPublishHandler(func(c mqtt.Client, m mqtt.Message) {
			msgProc(c, m)
		})
		_client = mqtt.NewClient(opts)
		reconnec_number := 0
		for { // 失败重连
			if token := _client.Connect(); token.Wait() && token.Error() != nil {
				reconnec_number++
				fmt.Println("MQTT连接失败...重试", reconnec_number)
			} else {
				break
			}
			time.Sleep(5 * time.Second)
		}
		subscribe(msgProcOther, gatewayMsgProc)

	}
	return
}

// mqtt订阅
func subscribe(msgProcOther func(c mqtt.Client, m mqtt.Message), gatewayMsgProc func(c mqtt.Client, m mqtt.Message)) {
	// 订阅默认，直连设备
	if token := _client.Subscribe(viper.GetString("mqtt.topicToSubscribe"), byte(viper.GetUint("mqtt.qos")), nil); token.Wait() &&
		token.Error() != nil {
		fmt.Println(token.Error())
		os.Exit(1)
	}
	//订阅网关
	if token := _client.Subscribe(viper.GetString("mqtt.gateway_topic"), byte(viper.GetUint("mqtt.qos")), func(c mqtt.Client, m mqtt.Message) {
		gatewayMsgProc(c, m)
	}); token.Wait() &&
		token.Error() != nil {
		fmt.Println(token.Error())
		os.Exit(1)
	}
	// if token := _client.Subscribe(viper.GetString("mqtt.topicToStatus"), byte(viper.GetUint("mqtt.qos")), func(c mqtt.Client, m mqtt.Message) {
	if token := _client.Subscribe(viper.GetString("mqtt.topicToStatus"), byte(1), func(c mqtt.Client, m mqtt.Message) {

		msgProcOther(c, m)
	}); token.Wait() &&
		token.Error() != nil {
		fmt.Println(token.Error())
		os.Exit(1)
	}
}

//发送消息给直连设备
func Send(payload []byte, token string) (err error) {
	if _client == nil {
		return errors.New("_client is error")
	}
	logs.Info("-------------------")
	logs.Info(viper.GetString("mqtt.topicToPublish") + "/" + token)
	logs.Info(utils.ReplaceUserInput(string(payload)))
	logs.Info("-------------------")
	t := _client.Publish(viper.GetString("mqtt.topicToPublish")+"/"+token, byte(viper.GetUint("mqtt.publishQos")), false, string(payload))
	if t.Error() != nil {
		fmt.Println(t.Error())
	}
	return t.Error()
}
func SendGateWay(payload []byte, token string, protocol string) (err error) {
	var clientErr = errors.New("_client is error")
	if _client == nil {
		return clientErr
	}
	logs.Info("-------------------")
	logs.Info(viper.GetString("mqtt.gateway_topic") + "/" + token)
	logs.Info(utils.ReplaceUserInput(string(payload)))
	logs.Info("-------------------")
	t := _client.Publish(viper.GetString("mqtt.gateway_topic")+"/"+token, 1, false, string(payload))
	if t.Error() != nil {
		fmt.Println(t.Error())
	}
	return t.Error()
}

func SendPlugin(payload []byte, topic string) (err error) {
	var clientErr = errors.New("_client is error")
	if _client == nil {
		return clientErr
	}
	logs.Info("-------------------")
	logs.Info(topic)
	logs.Info(utils.ReplaceUserInput(string(payload)))
	logs.Info("-------------------")
	t := _client.Publish(topic, 1, false, string(payload))
	if t.Error() != nil {
		fmt.Println(t.Error())
	}
	return t.Error()
}

func Close() {
	if _client != nil {
		_client.Disconnect(3000)
	}
}
