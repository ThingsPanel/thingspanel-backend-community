package tp_mqtt

import (
	uuid "ThingsPanel-Go/utils"
	"fmt"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/spf13/viper"
)

// tp客户端
var Tp_mqtt_client mqtt.Client
var Tp_mqtt_config Config

type Config struct {
	Mqtt *MqttClient `yaml:"mqtt"`
}
type MqttClient struct {
	Broker           string `yaml:"broker"`
	ClientId         string `yaml:"clientId"`
	User             string `yaml:"user"`
	Pass             string `yaml:"pass"`
	TopicToPublish   string `yaml:"topicToPublish"`
	TopicToSubscribe string `yaml:"topicToSubscribe"`
}

func InitTpClient() {
	fmt.Println("send->tp mqtt主程序开始。。。")
	InitConfigByViper()
	opts := mqtt.NewClientOptions()
	opts = setTpOpts(opts)
	Tp_mqtt_client = mqtt.NewClient(opts)
	mqtt_token := Tp_mqtt_client.Connect()
	if mqtt_token.Wait() && mqtt_token.Error() != nil {
		fmt.Println("Connect error:", mqtt_token.Error())
	}
	fmt.Println("send->tp 主程序结束。。。")
}

// 读取配置文件
func InitConfigByViper() {
	viper.SetConfigType("yaml")
	viper.SetConfigFile("./modules/dataService/config.yml")
	err := viper.ReadInConfig()
	if err != nil {
		fmt.Println(err.Error())
	}
	err = viper.Unmarshal(&Tp_mqtt_config)
	if err != nil {
		fmt.Println(err.Error())
	}
}

// 网关客户端选项
func setTpOpts(opts *mqtt.ClientOptions) *mqtt.ClientOptions {
	running := false
	var connectLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
		fmt.Printf("Connect lost: %v;", err)
	}
	clientId := uuid.GetUuid()
	opts.SetClientID(clientId)
	opts.SetUsername(Tp_mqtt_config.Mqtt.User)
	opts.SetPassword(Tp_mqtt_config.Mqtt.Pass)
	opts.AddBroker(Tp_mqtt_config.Mqtt.Broker)
	opts.SetAutoReconnect(true)
	opts.OnConnectionLost = connectLostHandler
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		if !running {
			fmt.Println("send->tp connect success...", Tp_mqtt_config.Mqtt.Broker)
		}
		running = true
	})
	opts.SetDefaultPublishHandler(func(c mqtt.Client, m mqtt.Message) {
		msgProc(c, m)
	})
	return opts
}

// 向tp发送消息
func Send(message string) {
	token := Tp_mqtt_client.Publish(Tp_mqtt_config.Mqtt.TopicToSubscribe, 1, false, message)
	if token.Error() != nil {
		fmt.Println(token.Error())
	} else {
		fmt.Println("发送成功：", message)
	}
}

// 发布信息回调
func msgProc(c mqtt.Client, m mqtt.Message) {
	fmt.Println("收到网关信息。。。")
}
