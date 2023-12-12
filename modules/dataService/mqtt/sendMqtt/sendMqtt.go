package sendmqtt

import (
	"ThingsPanel-Go/initialize/redis"
	"ThingsPanel-Go/utils"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/beego/beego/logs"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/spf13/viper"
)

var _client mqtt.Client

var (
	Qos = byte(viper.GetUint("mqtt.qos"))
)

var (
	Topic_DeviceAttributes  = "device/attributes"   // 订阅、发布
	Topic_DeviceStatus      = "device/status"       // 订阅
	Topic_OtaDeviceProgress = "ota/device/progress" // 订阅
	Topic_DeviceEvent       = "device/event"        // 订阅
	Topic_GatewayAttributes = "gateway/attributes"  // 订阅、发布
	Topic_GatewayEvent      = "gateway/event"       // 订阅

	Topic_DeviceCommand   = "device/command"    // 发布
	Topic_GatewayCommand  = "gateway/command"   // 发布
	Topic_OtaDeviceInform = "ota/device/inform" // 发布
)

func InitTopic() {
	// mqtt服务如果是vernemq,需要在订阅前增加共享订阅前缀，否则不需要
	fmt.Println("mqtt_server:", viper.GetString("mqtt_server"))
	if viper.GetString("mqtt_server") == "vernemq" {
		fmt.Println("mqtt_server is vernemq")
		Topic_DeviceAttributes = "$share/group/" + Topic_DeviceAttributes + "/+"
		Topic_GatewayAttributes = "$share/group/" + Topic_GatewayAttributes + "/+"
		Topic_OtaDeviceProgress = "$share/group/" + Topic_OtaDeviceProgress + "/+"
		Topic_DeviceEvent = "$share/group/" + Topic_DeviceEvent + "/+"
		Topic_GatewayEvent = "$share/group/" + Topic_GatewayEvent + "/+"
		Topic_DeviceStatus = "$share/group/" + Topic_DeviceStatus
	}
	go connect()
}

func connect() {
	mqttHost := os.Getenv("TP_MQTT_HOST")
	if mqttHost == "" {
		mqttHost = viper.GetString("mqtt.broker")
	}

	user := viper.GetString("mqtt.user")
	pass := viper.GetString("mqtt.pass")

	clientID := utils.GetUuid()
	options := mqtt.NewClientOptions()
	options.AddBroker(mqttHost)
	options.SetClientID(clientID)
	options.SetPassword(pass)
	options.SetUsername(user)

	client := mqtt.NewClient(options)
	for {
		if token := client.Connect(); token.Wait() && token.Error() != nil {
			log.Println(token.Error())
			time.Sleep(5 * time.Second)
		} else {
			break
		}
	}
	_client = client
}

// 发送消息给直连设备
func Send(payload []byte, token string) (err error) {
	//connect()
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

// 发送ota版本包消息给直连设备
func SendOtaAdress(payload []byte, token string) (err error) {
	//connect()
	if _client == nil {
		return errors.New("_client is error")
	}
	logs.Info("-------------------")
	logs.Info(viper.GetString("mqtt.topicToInform") + "/" + token)
	logs.Info(utils.ReplaceUserInput(string(payload)))
	logs.Info("-------------------")
	t := _client.Publish(viper.GetString("mqtt.topicToInform")+"/"+token, byte(viper.GetUint("mqtt.publishQos")), false, string(payload))
	if t.Error() != nil {
		fmt.Println(t.Error())
	}
	return t.Error()
}
func SendGateWay(payload []byte, token string, protocol string) (err error) {
	//connect()
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
	//connect()
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

func SendMQTT(payload []byte, topic string, qos byte) (err error) {
	//connect()
	var clientErr = errors.New("_client is error")
	if _client == nil {
		return clientErr
	}
	// 判断主题是否包含device/attributes/或device/command/
	if strings.Contains(topic, "device/attributes/") || strings.Contains(topic, "device/command/") {
		//获取最后一个/位置的字符串
		splitList := strings.Split(topic, "/")
		token := splitList[len(strings.Split(topic, "/"))-1]
		userTopic, err := redis.GetUserTopicByToken(token)
		if err == nil {
			if splitList[1] == "attributes" {
				if userTopic.UserPub.Attribute != "" {
					if strings.Contains(userTopic.UserPub.Attribute, "{username}") {
						topic = strings.Replace(userTopic.UserPub.Attribute, "{username}", token, 1)
					}
				}
			} else if splitList[1] == "command" {
				if userTopic.UserSub.Commands != "" {
					if strings.Contains(userTopic.UserSub.Commands, "{username}") {
						topic = strings.Replace(userTopic.UserSub.Commands, "{username}", token, 1)
					}
				}
			}
		}
	}
	t := _client.Publish(topic, qos, false, string(payload))
	if t.Error() != nil {
		return t.Error()
	}
	return nil
}
