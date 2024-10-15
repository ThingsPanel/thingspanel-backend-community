package publish

import (
	"context"
	"encoding/json"
	"fmt"
	"project/internal/dal"
	"project/internal/model"
	config "project/mqtt"
	"time"

	pkgerrors "github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// 网关设备publish
// @description PublishMessage
// @params topic string
// @params qos byte
// @params retained bool
// @params payload interface{}
// @return error
func publishMessage(topic string, qos byte, retained bool, payload interface{}) error {
	token := mqttClient.Publish(topic, qos, retained, payload)
	if token.Wait() && token.Error() != nil {
		return pkgerrors.Wrap(token.Error(), "[PublishMessage][send]failed")
	}
	return nil
}

// 网关设备命令发送
// @description GatewayPublishCommandMessage
// @params deviceInfo model.Device
// @params messageId sting
// @params command model.GatewayCommandPulish
// @params fn config.GatewayResponseFunc
// @return error
func GatewayPublishCommandMessage(ctx context.Context, deviceInfo model.Device, messageId string, command model.GatewayCommandPulish, fn ...config.GatewayResponseFunc) error {

	topic := fmt.Sprintf(config.MqttConfig.Commands.GatewayPublishTopic, deviceInfo.DeviceNumber, messageId)
	//topic := fmt.Sprintf("string3333", deviceInfo.DeviceNumber, messageId)
	logrus.Debug("topic:", topic)
	qos := byte(config.MqttConfig.Commands.QoS)
	payload, err := json.Marshal(command)
	if err != nil {
		return pkgerrors.Wrap(err, "[GatewayPublishCommandMessage][Marshal]failed")
	}
	topic, err = getGatewayPublishTopic(ctx, topic, deviceInfo)
	if err != nil {
		return pkgerrors.WithMessage(err, "[GatewayPublishResponseEventMessage][getGatewayPublishTopic]failed")
	}
	err = publishMessage(topic, qos, false, payload)
	if err != nil {
		return pkgerrors.WithMessage(err, "[GatewayPublishResponseEventMessage][publishMessage]failed")
	}
	if len(fn) > 0 {
		config.GatewayResponseFuncMap[messageId] = make(chan model.GatewayResponse)
		go func() {
			select {
			case data := <-config.GatewayResponseFuncMap[messageId]:
				fmt.Println("接收到数据:", data)
				fn[0](data)
				close(config.GatewayResponseFuncMap[messageId])
				delete(config.GatewayResponseFuncMap, messageId)
			case <-time.After(3 * time.Minute): // 设置超时时间为 3 分钟
				fmt.Println("超时，关闭通道")
				close(config.GatewayResponseFuncMap[messageId])
				delete(config.GatewayResponseFuncMap, messageId)
				return
			}
		}()
	}
	return nil
}

// 网关设备遥测命令下发
// @description GatewayPublishTelemetryMessage
// @params deviceInfo model.Device
// @params messageId sting
// @params command model.GatewayPublish
// @return error
func GatewayPublishTelemetryMessage(ctx context.Context, deviceInfo model.Device, messageId string, command model.GatewayPublish) error {

	topic := fmt.Sprintf(config.MqttConfig.Telemetry.GatewayPublishTopic, deviceInfo.DeviceNumber, messageId)
	qos := byte(config.MqttConfig.Telemetry.QoS)
	payload, err := json.Marshal(command)
	if err != nil {
		return pkgerrors.Wrap(err, "[GatewayPublishTelemetryMessage][Marshal]failed")
	}
	topic, err = getGatewayPublishTopic(ctx, topic, deviceInfo)
	if err != nil {
		return pkgerrors.WithMessage(err, "[GatewayPublishResponseEventMessage][getGatewayPublishTopic]failed")
	}
	return publishMessage(topic, qos, false, payload)
}

// 设置网关设置属性
// @description GatewayPublishSetAttributesMessage
// @params deviceInfo model.Device
// @params messageId sting
// @params command model.GatewayPublish
// @return error
func GatewayPublishSetAttributesMessage(ctx context.Context, deviceInfo model.Device, messageId string, command model.GatewayPublish, fn ...config.GatewayResponseFunc) error {

	topic := fmt.Sprintf(config.MqttConfig.Attributes.GatewayPublishTopic, deviceInfo.DeviceNumber, messageId)
	qos := byte(config.MqttConfig.Attributes.QoS)
	payload, err := json.Marshal(command)
	if err != nil {
		return pkgerrors.Wrap(err, "[GatewayPublishSetAttributesMessage][Marshal]failed")
	}
	topic, err = getGatewayPublishTopic(ctx, topic, deviceInfo)
	if err != nil {
		return pkgerrors.WithMessage(err, "[GatewayPublishSetAttributesMessage][getGatewayPublishTopic]failed")
	}
	err = publishMessage(topic, qos, false, payload)
	if err != nil {
		return pkgerrors.WithMessage(err, "[GatewayPublishSetAttributesMessage][publishMessage]failed")
	}
	if len(fn) > 0 {
		config.GatewayResponseFuncMap[messageId] = make(chan model.GatewayResponse)
		go func() {
			select {
			case data := <-config.GatewayResponseFuncMap[messageId]:
				fmt.Println("接收到数据:", data)
				fn[0](data)
				close(config.GatewayResponseFuncMap[messageId])
				delete(config.GatewayResponseFuncMap, messageId)
			case <-time.After(3 * time.Minute): // 设置超时时间为 3 分钟
				fmt.Println("超时，关闭通道")
				close(config.GatewayResponseFuncMap[messageId])
				delete(config.GatewayResponseFuncMap, messageId)
				return
			}

		}()
	}
	return nil
}

// 发送获取设备属性
// @description GatewayPublishGetAttributesMessage
// @params deviceInfo model.Device
// @params messageId sting
// @params command model.GatewayPublish
// @return error
func GatewayPublishGetAttributesMessage(ctx context.Context, deviceInfo model.Device, messageId string, command model.GatewayAttributeGet) error {

	topic := fmt.Sprintf(config.MqttConfig.Attributes.GatewayPublishGetTopic, deviceInfo.DeviceNumber)
	qos := byte(config.MqttConfig.Attributes.QoS)
	payload, err := json.Marshal(command)
	if err != nil {
		return pkgerrors.Wrap(err, "[GatewayPublishGetAttributesMessage][Marshal]failed")
	}
	topic, err = getGatewayPublishTopic(ctx, topic, deviceInfo)
	if err != nil {
		return pkgerrors.WithMessage(err, "[GatewayPublishResponseEventMessage][getGatewayPublishTopic]failed")
	}
	return publishMessage(topic, qos, false, payload)
}

// 平台收到属性响应
// @description GatewayPublishResponseAttributesMessage
// @params deviceInfo model.Device
// @params messageId sting
// @params command model.GatewayPublish
// @return error
func GatewayPublishResponseAttributesMessage(ctx context.Context, deviceInfo model.Device, messageId string, command model.GatewayResponse) error {

	topic := fmt.Sprintf(config.MqttConfig.Attributes.GatewayPublishResponseTopic, deviceInfo.DeviceNumber, messageId)
	qos := byte(config.MqttConfig.Attributes.QoS)
	payload, err := json.Marshal(command)
	if err != nil {
		return pkgerrors.Wrap(err, "[GatewayPublishResponseAttributesMessage][Marshal]failed")
	}
	topic, err = getGatewayPublishTopic(ctx, topic, deviceInfo)
	if err != nil {
		return pkgerrors.WithMessage(err, "[GatewayPublishResponseEventMessage][getGatewayPublishTopic]failed")
	}
	return publishMessage(topic, qos, false, payload)
}

// 平台收到属性响应
// @description GatewayPublishResponseEventMessage
// @params deviceInfo model.Device
// @params messageId sting
// @params command model.GatewayPublish
// @return error
func GatewayPublishResponseEventMessage(ctx context.Context, deviceInfo model.Device, messageId string, command model.GatewayResponse) error {

	topic := fmt.Sprintf(config.MqttConfig.Events.GatewayPublishTopic, deviceInfo.DeviceNumber, messageId)
	qos := byte(config.MqttConfig.Events.QoS)
	payload, err := json.Marshal(command)
	if err != nil {
		return pkgerrors.Wrap(err, "[GatewayPublishResponseEventMessage][Marshal]failed")
	}
	topic, err = getGatewayPublishTopic(ctx, topic, deviceInfo)
	if err != nil {
		return pkgerrors.WithMessage(err, "[GatewayPublishResponseEventMessage][getGatewayPublishTopic]failed")
	}
	return publishMessage(topic, qos, false, payload)
}

// 获取网关设备 是否有协议插座
// @description GatewayPublishResponseEventMessage
// @params deviceInfo model.Device
// @params messageId sting
// @params command model.GatewayPublish
// @return error
func getGatewayPublishTopic(ctx context.Context, topic string, deviceInfo model.Device) (string, error) {

	if deviceInfo.DeviceConfigID == nil {
		return topic, nil
	}
	// 查询协议插件信息
	protocolPluginInfo, err := dal.GetProtocolPluginByDeviceConfigID(*deviceInfo.DeviceConfigID)
	if err != nil {
		return topic, pkgerrors.Wrap(err, "[getGatewayPublishTopic]failed:")
	}
	if protocolPluginInfo != nil && protocolPluginInfo.SubTopicPrefix != nil {
		// 增加主题前缀
		topic = fmt.Sprintf("%s%s", *protocolPluginInfo.SubTopicPrefix, topic)
	}
	return topic, nil
}
