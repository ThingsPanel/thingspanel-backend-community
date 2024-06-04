package simulationpublish

import (
	"fmt"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
)

// 发布一次消息
func PublishMessage(host string, port string, topic string, payload string, username string, password string, clientId string) error {
	// 初始化配置
	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("%s:%s", host, port))
	opts.SetUsername(username)
	opts.SetPassword(password)
	opts.SetClientID(clientId)
	// 干净会话
	opts.SetCleanSession(true)
	// 消息顺序
	opts.SetOrderMatters(false)
	// 恢复客户端订阅，需要broker支持
	opts.SetResumeSubs(false)
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		logrus.Println("simulation mqtt connect success")
	})
	c := mqtt.NewClient(opts)
	if token := c.Connect(); token.Wait() && token.Error() != nil {
		logrus.Error("simulation MQTT Broker 连接失败:", token.Error())
		return token.Error()
	}
	defer c.Disconnect(250)
	logrus.Debug("username:", username)
	logrus.Debug("password:", password)
	logrus.Debug("clientId:", clientId)
	logrus.Debug("host:", host)
	logrus.Debug("port:", port)
	logrus.Debug("Topic:", topic)
	logrus.Debug("Payload:", payload)
	token := c.Publish(topic, 0, false, []byte(payload))
	if token.Wait() && token.Error() != nil {
		logrus.Error("simulation MQTT Broker 发布失败:", token.Error())
		return token.Error()
	}
	return nil
}
