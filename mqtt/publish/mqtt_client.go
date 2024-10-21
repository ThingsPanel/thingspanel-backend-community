package publish

import (
	"fmt"
	"path"
	"time"

	"project/initialize"
	"project/internal/model"
	config "project/mqtt"
	"project/pkg/common"
	"project/pkg/utils"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

var mqttClient mqtt.Client

func PublishInit() {
	// 创建mqtt客户端
	CreateMqttClient()

}

type MqttPublish interface {
}

// 创建mqtt客户端
func CreateMqttClient() {
	// 初始化配置
	opts := mqtt.NewClientOptions()
	opts.AddBroker(config.MqttConfig.Broker)
	opts.SetUsername(config.MqttConfig.User)
	opts.SetPassword(config.MqttConfig.Pass)
	opts.SetClientID("thingspanel-go-pub-" + uuid.New()[0:8])
	// 干净会话
	opts.SetCleanSession(true)
	// 恢复客户端订阅，需要broker支持
	opts.SetResumeSubs(true)
	// 自动重连
	opts.SetAutoReconnect(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetMaxReconnectInterval(20 * time.Second)
	// 消息顺序
	opts.SetOrderMatters(false)
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		logrus.Println("mqtt connect success")
	})
	// 断线重连
	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		logrus.Println("mqtt connect  lost: ", err)
		mqttClient.Disconnect(250)
		// 等待连接成功，失败重新连接
		for {
			if token := mqttClient.Connect(); token.Wait() && token.Error() == nil {
				fmt.Println("Reconnected to MQTT broker")
				break
			} else {
				fmt.Printf("Reconnect failed: %v\n", token.Error())
			}
			time.Sleep(5 * time.Second)
		}
	})

	mqttClient = mqtt.NewClient(opts)
	for {
		if token := mqttClient.Connect(); token.Wait() && token.Error() != nil {
			logrus.Error("MQTT Broker 1 连接失败:", token.Error())
			time.Sleep(5 * time.Second)
			continue
		}
		break
	}
}

// 下发telemetry消息
func PublishTelemetryMessage(topic string, device *model.Device, param *model.PutMessage) error {
	// TODO脚本处理
	if device.DeviceConfigID != nil && *device.DeviceConfigID != "" {
		script, err := initialize.GetScriptByDeviceAndScriptType(device, "B")
		if err != nil {
			logrus.Error(err.Error())
			return err
		}
		if script != nil && script.Content != nil && *script.Content != "" {
			msg, err := utils.ScriptDeal(*script.Content, []byte(param.Value), topic)
			if err != nil {
				logrus.Error(err.Error())
				return err
			}
			param.Value = msg
		}
	}
	qos := byte(config.MqttConfig.Telemetry.QoS)

	logrus.Info("topic:", topic, "value:", param.Value)
	// 发布消息
	token := mqttClient.Publish(topic, qos, false, []byte(param.Value))
	if token.Wait() && token.Error() != nil {
		logrus.Error(token.Error())
	}
	return token.Error()
}

// 发送ota版本包消息给直连设备
func PublishOtaAdress(deviceNumber string, payload []byte) error {
	topic := config.MqttConfig.OTA.PublishTopic + deviceNumber
	qos := byte(config.MqttConfig.OTA.QoS)
	// 发布消息
	token := mqttClient.Publish(topic, qos, false, payload)
	if token.Wait() && token.Error() != nil {
		logrus.Error(token.Error())
	}
	return token.Error()
}

// Send
// @AUTH：zxq
// @DATE：2024-03-08 14:30
// @DESCRIPTION：下发属性
func PublishAttributeMessage(topic string, payload []byte) error {
	qos := byte(config.MqttConfig.Attributes.QoS)
	// 发布消息
	token := mqttClient.Publish(topic, qos, false, payload)
	if token.Wait() && token.Error() != nil {
		logrus.Error(token.Error())
		return token.Error()
	}
	return nil
}

// 接收设备属性响应
func PublishAttributeResponseMessage(deviceNumber string, messageId string, err error) error {
	qos := byte(config.MqttConfig.Attributes.QoS)
	topic := fmt.Sprintf("%s%s/%s", config.MqttConfig.Attributes.PublishResponseTopic, deviceNumber, messageId)

	payload := common.GetResponsePayload("", err)
	// 发布消息
	token := mqttClient.Publish(topic, qos, false, payload)
	if token.Wait() && token.Error() != nil {
		logrus.Error(token.Error())
	}
	return token.Error()
}

// 接收设备事件响应
func PublishEventResponseMessage(deviceNumber string, messageId string, method string, err error) error {
	qos := byte(config.MqttConfig.Events.QoS)
	topic := fmt.Sprintf("%s%s/%s", config.MqttConfig.Events.PublishTopic, deviceNumber, messageId)

	payload := common.GetResponsePayload(method, err)
	// 发布消息
	token := mqttClient.Publish(topic, qos, false, payload)
	if token.Wait() && token.Error() != nil {
		logrus.Error(token.Error())
	}
	return token.Error()
}

// 发布获取属性请求
func PublishGetAttributeMessage(deviceNumber string, payload []byte) error {
	topic := fmt.Sprintf("%s%s", config.MqttConfig.Attributes.PublishGetTopic, deviceNumber)
	qos := byte(config.MqttConfig.Attributes.QoS)
	// 发布消息
	token := mqttClient.Publish(topic, qos, false, payload)
	if token.Wait() && token.Error() != nil {
		logrus.Error(token.Error())
	}
	return token.Error()
}

// 发送事件响应
func PublishEventMessage(payload []byte) error {
	topic := config.MqttConfig.Events.PublishTopic
	qos := byte(config.MqttConfig.Events.QoS)
	// 发布消息
	token := mqttClient.Publish(topic, qos, false, payload)
	if token.Wait() && token.Error() != nil {
		logrus.Error(token.Error())
	}
	return token.Error()
}

// 下发command消息
func PublishCommandMessage(topic string, payload []byte) error {
	qos := byte(config.MqttConfig.Commands.QoS)
	// 发布消息
	token := mqttClient.Publish(topic, qos, false, payload)
	if token.Wait() && token.Error() != nil {
		logrus.Error(token.Error())
	}
	logrus.Debug("下发主题:", topic)
	logrus.Debug("下发命令:", string(payload))
	return token.Error()
}

// 转发telemetry消息
func ForwardTelemetryMessage(deviceId string, payload []byte) error {
	telemetryTopic := config.MqttConfig.Telemetry.SubscribeTopic + "/" + deviceId
	qos := byte(config.MqttConfig.Telemetry.QoS)
	// 发布消息
	token := mqttClient.Publish(telemetryTopic, qos, false, payload)
	if token.Wait() && token.Error() != nil {
		logrus.Error(token.Error())
	}
	return token.Error()
}

// 发送在线离线消息
func PublishOnlineMessage(deviceID string, payload []byte) error {
	topic := fmt.Sprintf("devices/status/%s", deviceID)
	topic = path.Join("$share/mygroup", topic)
	qos := byte(0)
	// 发布消息
	token := mqttClient.Publish(topic, qos, false, payload)
	if token.Wait() && token.Error() != nil {
		logrus.Error(token.Error())
	}
	return token.Error()
}
