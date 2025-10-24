package tests

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"iot-platform-autotest/internal/config"
	"iot-platform-autotest/internal/device"
	"iot-platform-autotest/internal/platform"
	"iot-platform-autotest/internal/utils"
)

func TestGatewayAttributeSet(t *testing.T) {
	// 初始化日志
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	defer logger.Sync()

	// 加载配置
	cfg, err := config.Load("../../config-gateway-community.yaml")
	require.NoError(t, err)

	// 验证是网关设备
	require.Equal(t, "gateway", cfg.DeviceType, "Test requires gateway device type")

	// 创建网关设备
	dev, err := device.NewDevice(cfg, logger)
	require.NoError(t, err)
	require.NoError(t, dev.Connect())
	defer dev.Disconnect()

	// 订阅主题
	require.NoError(t, dev.SubscribeAll())

	// 创建API客户端
	apiClient := platform.NewAPIClient(&cfg.API, logger)

	// 创建数据库客户端
	dbClient, err := platform.NewDBClient(&cfg.Database, logger)
	require.NoError(t, err)
	defer dbClient.Close()

	// 获取主题构建器
	topics := utils.NewGatewayMQTTTopics(cfg.Device.DeviceNumber)

	logger.Info("Starting gateway attribute set test",
		zap.String("gateway_number", cfg.Device.DeviceNumber))

	// 测试1: 给顶层网关下发属性设置
	t.Run("AttributeSet_Gateway_Self", func(t *testing.T) {
		dev.ClearReceivedMessages(topics.AttributeSet())

		attributeData := map[string]interface{}{
			"device_name": "Gateway-001",
			"location":    "Building A",
			"firmware":    "v2.1.0",
		}

		logger.Info("Sending attribute set to gateway itself",
			zap.String("device_id", cfg.Device.DeviceID),
			zap.Any("attribute_data", attributeData))

		// 下发属性设置指令
		err = apiClient.PublishAttributeSet(cfg.Device.DeviceID, attributeData)
		require.NoError(t, err)

		// 等待设备接收
		timeout := time.Duration(cfg.Test.WaitMQTTResponseSeconds) * time.Second
		messages := dev.GetReceivedMessages(topics.AttributeSet(), timeout)
		assert.NotEmpty(t, messages, "Gateway did not receive attribute set message")

		var receivedMessageID string
		if len(messages) > 0 {
			// 验证接收到的消息
			var receivedData map[string]interface{}
			err := json.Unmarshal(messages[0].Payload, &receivedData)
			require.NoError(t, err)

			logger.Info("Gateway received attribute set message",
				zap.Any("received_data", receivedData))

			// 从 topic 中提取 message_id
			// Topic 格式: gateway/attributes/set/device_number_666666/6ef2ba79
			topicParts := strings.Split(messages[0].Topic, "/")
			if len(topicParts) > 0 {
				receivedMessageID = topicParts[len(topicParts)-1]
				logger.Info("Extracted message_id from topic",
					zap.String("message_id", receivedMessageID),
					zap.String("topic", messages[0].Topic))
			}

			// 验证是否包含 gateway_data
			if gatewayData, ok := receivedData["gateway_data"].(map[string]interface{}); ok {
				for key, expectedValue := range attributeData {
					actualValue, exists := gatewayData[key]
					assert.True(t, exists, "Attribute key not found in gateway_data: %s", key)
					assert.Equal(t, expectedValue, actualValue, "Attribute value mismatch for key: %s", key)
				}
			}

			// 发送响应
			if receivedMessageID != "" {
				err = dev.PublishAttributeSetResponse(receivedMessageID, true)
				require.NoError(t, err)
				logger.Info("Attribute set response sent", zap.String("message_id", receivedMessageID))
			}
		}

		// 验证属性设置日志
		if receivedMessageID != "" {
			time.Sleep(2 * time.Second)
			log, err := dbClient.QueryAttributeSetLogs(cfg.Device.DeviceID, receivedMessageID)
			require.NoError(t, err)
			assert.NotNil(t, log, "No attribute set log found for gateway")

			if log != nil {
				// 状态: 1=下发成功, 3=响应成功
				logger.Info("Gateway attribute set log verified",
					zap.String("status", log.Status),
					zap.String("data", log.Data),
					zap.Any("rsp_data", log.RspData))
			}
		}
	})

	// 测试2: 给子设备下发属性设置
	if len(cfg.Gateway.SubDevices) > 0 {
		t.Run("AttributeSet_SubDevice", func(t *testing.T) {
			subDev := cfg.Gateway.SubDevices[0]
			dev.ClearReceivedMessages(topics.AttributeSet())

			attributeData := map[string]interface{}{
				"sensor_type": "temperature",
				"unit":        "celsius",
				"range":       float64(100),
			}

			logger.Info("Sending attribute set to sub-device",
				zap.String("sub_device_id", subDev.DeviceID),
				zap.String("sub_device_number", subDev.SubDeviceNumber),
				zap.Any("attribute_data", attributeData))

			// 下发属性设置指令到子设备
			err = apiClient.PublishAttributeSet(subDev.DeviceID, attributeData)
			require.NoError(t, err)

			// 等待网关接收
			timeout := time.Duration(cfg.Test.WaitMQTTResponseSeconds) * time.Second
			messages := dev.GetReceivedMessages(topics.AttributeSet(), timeout)
			assert.NotEmpty(t, messages, "Gateway did not receive attribute set message for sub-device")

			var receivedMessageID string
			if len(messages) > 0 {
				var receivedData map[string]interface{}
				err := json.Unmarshal(messages[0].Payload, &receivedData)
				require.NoError(t, err)

				logger.Info("Gateway received attribute set message for sub-device",
					zap.Any("received_data", receivedData))

				// 从 topic 提取 message_id
				topicParts := strings.Split(messages[0].Topic, "/")
				if len(topicParts) > 0 {
					receivedMessageID = topicParts[len(topicParts)-1]
				}

				// 验证 sub_device_data
				if subDeviceData, ok := receivedData["sub_device_data"].(map[string]interface{}); ok {
					subDevAttr, exists := subDeviceData[subDev.SubDeviceNumber]
					assert.True(t, exists, "Sub-device attribute data not found for: %s", subDev.SubDeviceNumber)

					if exists {
						if subDevAttrMap, ok := subDevAttr.(map[string]interface{}); ok {
							for key, expectedValue := range attributeData {
								actualValue, keyExists := subDevAttrMap[key]
								assert.True(t, keyExists, "Attribute key not found in sub-device data: %s", key)
								assert.Equal(t, expectedValue, actualValue, "Sub-device attribute value mismatch for key: %s", key)
							}
						}
					}
				}

				// 发送响应
				if receivedMessageID != "" {
					err = dev.PublishAttributeSetResponse(receivedMessageID, true)
					require.NoError(t, err)
				}
			}

			// 验证日志
			if receivedMessageID != "" {
				time.Sleep(2 * time.Second)
				log, err := dbClient.QueryAttributeSetLogs(subDev.DeviceID, receivedMessageID)
				require.NoError(t, err)
				assert.NotNil(t, log, "No attribute set log found for sub-device")

				if log != nil {
					assert.NotEmpty(t, log.Data, "Sub-device attribute set log data should not be empty")
					assert.Contains(t, log.Data, subDev.SubDeviceNumber,
						"Log data should contain sub-device number: %s", subDev.SubDeviceNumber)

					logger.Info("Sub-device attribute set log verified",
						zap.String("sub_device_number", subDev.SubDeviceNumber),
						zap.String("status", log.Status),
						zap.String("data", log.Data))
				}
			}
		})
	}

	// 测试3: 给子网关下发属性设置
	if len(cfg.Gateway.SubGateways) > 0 {
		t.Run("AttributeSet_SubGateway", func(t *testing.T) {
			subGw := cfg.Gateway.SubGateways[0]
			dev.ClearReceivedMessages(topics.AttributeSet())

			attributeData := map[string]interface{}{
				"gateway_type": "edge",
				"protocol":     "mqtt",
				"max_devices":  float64(50),
			}

			logger.Info("Sending attribute set to sub-gateway",
				zap.String("sub_gateway_id", subGw.DeviceID),
				zap.String("sub_gateway_number", subGw.SubGatewayNumber),
				zap.Any("attribute_data", attributeData))

			// 下发属性设置指令到子网关
			err = apiClient.PublishAttributeSet(subGw.DeviceID, attributeData)
			require.NoError(t, err)

			// 等待网关接收
			timeout := time.Duration(cfg.Test.WaitMQTTResponseSeconds) * time.Second
			messages := dev.GetReceivedMessages(topics.AttributeSet(), timeout)
			assert.NotEmpty(t, messages, "Gateway did not receive attribute set message for sub-gateway")

			var receivedMessageID string
			if len(messages) > 0 {
				var receivedData map[string]interface{}
				err := json.Unmarshal(messages[0].Payload, &receivedData)
				require.NoError(t, err)

				logger.Info("Gateway received attribute set message for sub-gateway",
					zap.Any("received_data", receivedData))

				// 从 topic 提取 message_id
				topicParts := strings.Split(messages[0].Topic, "/")
				if len(topicParts) > 0 {
					receivedMessageID = topicParts[len(topicParts)-1]
				}

				// 验证 sub_gateway_data
				if subGatewayData, ok := receivedData["sub_gateway_data"].(map[string]interface{}); ok {
					_, exists := subGatewayData[subGw.SubGatewayNumber]
					assert.True(t, exists, "Sub-gateway attribute data not found for: %s", subGw.SubGatewayNumber)
				}

				// 发送响应
				if receivedMessageID != "" {
					err = dev.PublishAttributeSetResponse(receivedMessageID, true)
					require.NoError(t, err)
				}
			}

			// 验证日志
			if receivedMessageID != "" {
				time.Sleep(2 * time.Second)
				log, err := dbClient.QueryAttributeSetLogs(subGw.DeviceID, receivedMessageID)
				require.NoError(t, err)
				assert.NotNil(t, log, "No attribute set log found for sub-gateway")

				if log != nil {
					assert.NotEmpty(t, log.Data, "Sub-gateway attribute set log data should not be empty")
					assert.Contains(t, log.Data, subGw.SubGatewayNumber,
						"Log data should contain sub-gateway number: %s", subGw.SubGatewayNumber)

					logger.Info("Sub-gateway attribute set log verified",
						zap.String("sub_gateway_number", subGw.SubGatewayNumber),
						zap.String("status", log.Status),
						zap.String("data", log.Data))
				}
			}
		})
	}

	// 测试4: 给子网关下的子设备下发属性设置
	if len(cfg.Gateway.SubGateways) > 0 && len(cfg.Gateway.SubGateways[0].SubDevices) > 0 {
		t.Run("AttributeSet_SubGateway_SubDevice", func(t *testing.T) {
			subGw := cfg.Gateway.SubGateways[0]
			subGwSubDev := subGw.SubDevices[0]
			dev.ClearReceivedMessages(topics.AttributeSet())

			attributeData := map[string]interface{}{
				"sensor_model": "TH-100",
				"calibrated":   true,
				"interval":     float64(60),
			}

			logger.Info("Sending attribute set to sub-gateway's sub-device",
				zap.String("device_id", subGwSubDev.DeviceID),
				zap.String("sub_device_number", subGwSubDev.SubDeviceNumber),
				zap.String("parent_sub_gateway", subGw.SubGatewayNumber),
				zap.Any("attribute_data", attributeData))

			// 下发属性设置指令到子网关的子设备
			err = apiClient.PublishAttributeSet(subGwSubDev.DeviceID, attributeData)
			require.NoError(t, err)

			// 等待网关接收
			timeout := time.Duration(cfg.Test.WaitMQTTResponseSeconds) * time.Second
			messages := dev.GetReceivedMessages(topics.AttributeSet(), timeout)
			assert.NotEmpty(t, messages, "Gateway did not receive attribute set message for sub-gateway's sub-device")

			var receivedMessageID string
			if len(messages) > 0 {
				var receivedData map[string]interface{}
				err := json.Unmarshal(messages[0].Payload, &receivedData)
				require.NoError(t, err)

				logger.Info("Gateway received attribute set message for sub-gateway's sub-device",
					zap.Any("received_data", receivedData))

				// 从 topic 提取 message_id
				topicParts := strings.Split(messages[0].Topic, "/")
				if len(topicParts) > 0 {
					receivedMessageID = topicParts[len(topicParts)-1]
				}

				// 发送响应
				if receivedMessageID != "" {
					err = dev.PublishAttributeSetResponse(receivedMessageID, true)
					require.NoError(t, err)
				}
			}

			// 验证日志
			if receivedMessageID != "" {
				time.Sleep(2 * time.Second)
				log, err := dbClient.QueryAttributeSetLogs(subGwSubDev.DeviceID, receivedMessageID)
				require.NoError(t, err)
				assert.NotNil(t, log, "No attribute set log found for sub-gateway's sub-device")

				if log != nil {
					assert.NotEmpty(t, log.Data, "Sub-gateway's sub-device attribute set log data should not be empty")
					assert.Contains(t, log.Data, subGw.SubGatewayNumber,
						"Log data should contain sub-gateway number: %s", subGw.SubGatewayNumber)
					assert.Contains(t, log.Data, subGwSubDev.SubDeviceNumber,
						"Log data should contain sub-device number: %s", subGwSubDev.SubDeviceNumber)

					logger.Info("Sub-gateway's sub-device attribute set log verified",
						zap.String("sub_device_number", subGwSubDev.SubDeviceNumber),
						zap.String("parent_sub_gateway", subGw.SubGatewayNumber),
						zap.String("status", log.Status),
						zap.String("data", log.Data))
				}
			}
		})
	}

	logger.Info("Gateway attribute set test completed successfully")
}
