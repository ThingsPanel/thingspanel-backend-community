package bl

import (
	"ThingsPanel-Go/gateway/tp_mqtt"
	uuid "ThingsPanel-Go/utils"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/spf13/viper"
)

var Bl110_config Config

// 客户端
var mqtt_client mqtt.Client

type Config struct {
	Mqtt *MqttClient `yaml:"mqtt"`
}
type MqttClient struct {
	Broker           string `yaml:"broker"`
	ClientId         string `yaml:"clientId"`
	User             string `yaml:"user"`
	Pass             string `yaml:"pass"`
	TopicToSubscribe string `yaml:"topicToSubscribe"`
	TopicToPublish   string `yaml:"sopicToPublish"`
}

func InitBl110Client() {
	fmt.Println("bl110-mqtt主程序开始。。。")
	InitConfigByViper()
	opts := mqtt.NewClientOptions()
	opts = setOpts(opts)
	mqtt_client = mqtt.NewClient(opts)
	mqtt_token := mqtt_client.Connect()
	if mqtt_token.Wait() && mqtt_token.Error() != nil {
		fmt.Println("Connect error:", mqtt_token.Error())
	}
	// 初始订阅
	fmt.Println(Bl110_config.Mqtt.TopicToSubscribe)
	subscribeList := strings.Split(Bl110_config.Mqtt.TopicToSubscribe, "||")
	filters := make(map[string]byte)
	for _, subscribe := range subscribeList {
		filters[subscribe] = 0
	}
	initSubcrube(filters)
	initTpSubcrube()
	fmt.Println("bl110-mqtt主程序结束。。。")
}

// 读取配置文件
func InitConfigByViper() {
	viper.SetConfigType("yaml")
	viper.SetConfigFile("./gateway/bl/bl_config.yml")
	err := viper.ReadInConfig()
	if err != nil {
		fmt.Println(err.Error())
	}
	err = viper.Unmarshal(&Bl110_config)
	if err != nil {
		fmt.Println(err.Error())
	}
}

func setOpts(opts *mqtt.ClientOptions) *mqtt.ClientOptions {
	tp_running := false
	var connectLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
		fmt.Printf("Connect lost: %v;", err)
	}
	mqttHost := os.Getenv("TP_MQTT_HOST")
	if mqttHost == "" {
		mqttHost = Bl110_config.Mqtt.Broker
	}
	clientId := uuid.GetUuid()
	opts.SetClientID(clientId)
	opts.SetUsername(Bl110_config.Mqtt.User)
	opts.SetPassword(Bl110_config.Mqtt.Pass)
	opts.AddBroker(mqttHost)
	opts.SetAutoReconnect(true)
	opts.OnConnectionLost = connectLostHandler
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		if !tp_running {
			fmt.Println("bl110-mqtt connect success...", mqttHost)
		}
		tp_running = true
	})
	opts.SetDefaultPublishHandler(func(c mqtt.Client, m mqtt.Message) {
		msgProc(c, m)
	})
	return opts
}

// 发布信息回调
func msgProc(c mqtt.Client, m mqtt.Message) {
	fmt.Println("收到网关信息。。。")
}

// 系统初始化订阅
func initSubcrube(filters map[string]byte) {
	sub_token := mqtt_client.SubscribeMultiple(filters, func(c mqtt.Client, m mqtt.Message) {
		subscribeMsgProc(c, m)
	})
	if sub_token.Wait() && sub_token.Error() != nil {
		fmt.Println("Subscribe error:", sub_token.Error())
	} else {
		fmt.Println("订阅：", filters)
	}
}

// 初始化Tp订阅
func initTpSubcrube() {
	sub_token := tp_mqtt.Tp_mqtt_client.Subscribe(tp_mqtt.Tp_mqtt_config.Mqtt.TopicToPublish, 0, func(c mqtt.Client, m mqtt.Message) {
		subscribeTpMsgProc(c, m)
	})
	if sub_token.Wait() && sub_token.Error() != nil {
		fmt.Println("Subscribe error:", sub_token.Error())
	} else {
		fmt.Println("订阅：", tp_mqtt.Tp_mqtt_config.Mqtt.TopicToSubscribe)
	}
}

// 接收处理订阅信息
// 此处判断token是否为BL110的
func subscribeTpMsgProc(c mqtt.Client, m mqtt.Message) {
	fmt.Println("收到Tp控制信息。。。")
	fmt.Println(m.Topic(), m.MessageID(), string(m.Payload()))
	SendMessage(m.Payload())
}

// 接收处理订阅信息
func subscribeMsgProc(c mqtt.Client, m mqtt.Message) {
	fmt.Println("收到bl信息。。。")
	fmt.Println(m.Topic(), m.MessageID(), string(m.Payload()))
	type messagePayload struct {
		SensorDatas []map[string]interface{} `json:"sensorDatas"`
		Time        string                   `json:"time"`
	}
	payload := &messagePayload{}
	if err := json.Unmarshal(m.Payload(), payload); err != nil {
		fmt.Println("Msg Consumer: Cannot unmarshal msg payload to JSON:", err)
		return
	}
	// 解析bl110报文
	for _, sensorData := range payload.SensorDatas {
		if _, ok := sensorData["flag"]; ok {
			var payloadInterface = make(map[string]interface{})
			//存在
			switch sensorData["flag"].(type) {
			case string:
				token := m.Topic() + "/" + sensorData["flag"].(string)
				fmt.Println(token)
				payloadInterface["token"] = token
			default:
				continue
			}
			delete(sensorData, "flag")
			payloadInterface["values"] = sensorData
			newPayload, toErr := json.Marshal(payloadInterface)
			if toErr != nil {
				fmt.Println("JSON 编码失败：", toErr)
			}
			tp_mqtt.Send(string(newPayload))
		}
	}
}
