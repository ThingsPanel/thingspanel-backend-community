package tests

import (
	"encoding/json"
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

func TestGatewayTelemetryControl(t *testing.T) {
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

	// 订阅控制主题
	require.NoError(t, dev.SubscribeAll())

	// 创建API客户端
	apiClient := platform.NewAPIClient(&cfg.API, logger)

	// 创建数据库客户端
	dbClient, err := platform.NewDBClient(&cfg.Database, logger)
	require.NoError(t, err)
	defer dbClient.Close()

	// 获取主题构建器
	topics := utils.NewGatewayMQTTTopics(cfg.Device.DeviceNumber)

	logger.Info("Starting gateway telemetry control test",
		zap.String("gateway_number", cfg.Device.DeviceNumber))

	// 测试1: 给顶层网关下发控制指令
	t.Run("Control_Gateway_Self", func(t *testing.T) {
		dev.ClearReceivedMessages(topics.TelemetryControl())

		controlData := map[string]interface{}{
			"switch":      true,
			"temperature": 25.5,
		}

		startTime := time.Now()

		logger.Info("Sending control to gateway itself",
			zap.String("device_id", cfg.Device.DeviceID),
			zap.Any("control_data", controlData))

		// 下发控制指令
		err = apiClient.PublishTelemetryControl(cfg.Device.DeviceID, controlData)
		require.NoError(t, err)

		// 等待设备接收
		timeout := time.Duration(cfg.Test.WaitMQTTResponseSeconds) * time.Second
		messages := dev.GetReceivedMessages(topics.TelemetryControl(), timeout)
		assert.NotEmpty(t, messages, "Gateway did not receive control message")

		if len(messages) > 0 {
			// 验证接收到的消息
			var receivedData map[string]interface{}
			err := json.Unmarshal(messages[0].Payload, &receivedData)
			require.NoError(t, err)

			logger.Info("Gateway received control message",
				zap.Any("received_data", receivedData))

			// 验证是否包含 gateway_data
			if gatewayData, ok := receivedData["gateway_data"].(map[string]interface{}); ok {
				for key, expectedValue := range controlData {
					actualValue, exists := gatewayData[key]
					assert.True(t, exists, "Control data key not found in gateway_data: %s", key)
					assert.Equal(t, expectedValue, actualValue, "Control data value mismatch for key: %s", key)
				}
			} else {
				// 如果不是嵌套格式，直接验证
				for key, expectedValue := range controlData {
					actualValue, exists := receivedData[key]
					assert.True(t, exists, "Control data key not found: %s", key)
					assert.Equal(t, expectedValue, actualValue, "Control data value mismatch for key: %s", key)
				}
			}
		}

		// 验证下发日志
		time.Sleep(1 * time.Second)
		logs, err := dbClient.QueryTelemetrySetLogs(cfg.Device.DeviceID, startTime)
		require.NoError(t, err)
		assert.NotEmpty(t, logs, "No telemetry set log found for gateway")

		if len(logs) > 0 {
			log := logs[0]
			// 验证状态为成功
			assert.Equal(t, "1", log.Status, "Gateway control status should be success(1)")

			// 验证日志中的数据字段
			assert.NotEmpty(t, log.Data, "Gateway control log data should not be empty")

			logger.Info("Gateway control log verified",
				zap.String("status", log.Status),
				zap.String("data", log.Data))
		}
	})

	// 测试2: 给子设备下发控制指令
	if len(cfg.Gateway.SubDevices) > 0 {
		t.Run("Control_SubDevice", func(t *testing.T) {
			subDev := cfg.Gateway.SubDevices[0]
			dev.ClearReceivedMessages(topics.TelemetryControl())

			controlData := map[string]interface{}{
				"switch": false,
				"power":  float64(100), // JSON 反序列化时数字会变成 float64
			}

			startTime := time.Now()

			logger.Info("Sending control to sub-device",
				zap.String("sub_device_id", subDev.DeviceID),
				zap.String("sub_device_number", subDev.SubDeviceNumber),
				zap.Any("control_data", controlData))

			// 下发控制指令到子设备
			err = apiClient.PublishTelemetryControl(subDev.DeviceID, controlData)
			require.NoError(t, err)

			// 等待网关接收（平台会封装为网关报文）
			timeout := time.Duration(cfg.Test.WaitMQTTResponseSeconds) * time.Second
			messages := dev.GetReceivedMessages(topics.TelemetryControl(), timeout)
			assert.NotEmpty(t, messages, "Gateway did not receive control message for sub-device")

			if len(messages) > 0 {
				var receivedData map[string]interface{}
				err := json.Unmarshal(messages[0].Payload, &receivedData)
				require.NoError(t, err)

				logger.Info("Gateway received control message for sub-device",
					zap.Any("received_data", receivedData))

				// 验证 sub_device_data 中是否包含目标子设备的控制数据
				if subDeviceData, ok := receivedData["sub_device_data"].(map[string]interface{}); ok {
					subDevControl, exists := subDeviceData[subDev.SubDeviceNumber]
					assert.True(t, exists, "Sub-device control data not found for: %s", subDev.SubDeviceNumber)

					if exists {
						if subDevCtrlMap, ok := subDevControl.(map[string]interface{}); ok {
							for key, expectedValue := range controlData {
								actualValue, keyExists := subDevCtrlMap[key]
								assert.True(t, keyExists, "Control key not found in sub-device data: %s", key)
								assert.Equal(t, expectedValue, actualValue, "Sub-device control value mismatch for key: %s", key)
							}
						}
					}
				}
			}

			// 验证子设备的下发日志
			time.Sleep(1 * time.Second)
			logs, err := dbClient.QueryTelemetrySetLogs(subDev.DeviceID, startTime)
			require.NoError(t, err)
			assert.NotEmpty(t, logs, "No telemetry set log found for sub-device")

			if len(logs) > 0 {
				log := logs[0]
				// 验证状态为成功
				assert.Equal(t, "1", log.Status, "Sub-device control status should be success(1)")

				// 验证日志数据包含子设备信息
				assert.NotEmpty(t, log.Data, "Sub-device control log data should not be empty")
				assert.Contains(t, log.Data, subDev.SubDeviceNumber,
					"Log data should contain sub-device number: %s", subDev.SubDeviceNumber)

				logger.Info("Sub-device control log verified",
					zap.String("sub_device_number", subDev.SubDeviceNumber),
					zap.String("status", log.Status),
					zap.String("data", log.Data))
			}
		})
	}

	// 测试3: 给子网关下发控制指令
	if len(cfg.Gateway.SubGateways) > 0 {
		t.Run("Control_SubGateway", func(t *testing.T) {
			subGw := cfg.Gateway.SubGateways[0]
			dev.ClearReceivedMessages(topics.TelemetryControl())

			controlData := map[string]interface{}{
				"mode":   "auto",
				"status": float64(1), // JSON 反序列化时数字会变成 float64
			}

			startTime := time.Now()

			logger.Info("Sending control to sub-gateway",
				zap.String("sub_gateway_id", subGw.DeviceID),
				zap.String("sub_gateway_number", subGw.SubGatewayNumber),
				zap.Any("control_data", controlData))

			// 下发控制指令到子网关
			err = apiClient.PublishTelemetryControl(subGw.DeviceID, controlData)
			require.NoError(t, err)

			// 等待网关接收
			timeout := time.Duration(cfg.Test.WaitMQTTResponseSeconds) * time.Second
			messages := dev.GetReceivedMessages(topics.TelemetryControl(), timeout)
			assert.NotEmpty(t, messages, "Gateway did not receive control message for sub-gateway")

			if len(messages) > 0 {
				var receivedData map[string]interface{}
				err := json.Unmarshal(messages[0].Payload, &receivedData)
				require.NoError(t, err)

				logger.Info("Gateway received control message for sub-gateway",
					zap.Any("received_data", receivedData))

				// 验证 sub_gateway_data 中是否包含目标子网关的控制数据
				if subGatewayData, ok := receivedData["sub_gateway_data"].(map[string]interface{}); ok {
					subGwControl, exists := subGatewayData[subGw.SubGatewayNumber]
					assert.True(t, exists, "Sub-gateway control data not found for: %s", subGw.SubGatewayNumber)

					if exists && subGwControl != nil {
						logger.Info("Sub-gateway control data found",
							zap.String("sub_gateway_number", subGw.SubGatewayNumber),
							zap.Any("control_data", subGwControl))
					}
				}
			}

			// 验证子网关的下发日志
			time.Sleep(1 * time.Second)
			logs, err := dbClient.QueryTelemetrySetLogs(subGw.DeviceID, startTime)
			require.NoError(t, err)
			assert.NotEmpty(t, logs, "No telemetry set log found for sub-gateway")

			if len(logs) > 0 {
				log := logs[0]
				// 验证状态为成功
				assert.Equal(t, "1", log.Status, "Sub-gateway control status should be success(1)")

				// 验证日志数据包含子网关信息
				assert.NotEmpty(t, log.Data, "Sub-gateway control log data should not be empty")
				assert.Contains(t, log.Data, subGw.SubGatewayNumber,
					"Log data should contain sub-gateway number: %s", subGw.SubGatewayNumber)

				logger.Info("Sub-gateway control log verified",
					zap.String("sub_gateway_number", subGw.SubGatewayNumber),
					zap.String("status", log.Status),
					zap.String("data", log.Data))
			}
		})
	}

	// 测试4: 给子网关下的子设备下发控制指令
	if len(cfg.Gateway.SubGateways) > 0 && len(cfg.Gateway.SubGateways[0].SubDevices) > 0 {
		t.Run("Control_SubGateway_SubDevice", func(t *testing.T) {
			subGw := cfg.Gateway.SubGateways[0]
			subGwSubDev := subGw.SubDevices[0]
			dev.ClearReceivedMessages(topics.TelemetryControl())

			controlData := map[string]interface{}{
				"alarm":  true,
				"volume": float64(80), // JSON 反序列化时数字会变成 float64
			}

			startTime := time.Now()

			logger.Info("Sending control to sub-gateway's sub-device",
				zap.String("device_id", subGwSubDev.DeviceID),
				zap.String("sub_device_number", subGwSubDev.SubDeviceNumber),
				zap.String("parent_sub_gateway", subGw.SubGatewayNumber),
				zap.Any("control_data", controlData))

			// 下发控制指令到子网关的子设备
			err = apiClient.PublishTelemetryControl(subGwSubDev.DeviceID, controlData)
			require.NoError(t, err)

			// 等待网关接收
			timeout := time.Duration(cfg.Test.WaitMQTTResponseSeconds) * time.Second
			messages := dev.GetReceivedMessages(topics.TelemetryControl(), timeout)
			assert.NotEmpty(t, messages, "Gateway did not receive control message for sub-gateway's sub-device")

			if len(messages) > 0 {
				var receivedData map[string]interface{}
				err := json.Unmarshal(messages[0].Payload, &receivedData)
				require.NoError(t, err)

				logger.Info("Gateway received control message for sub-gateway's sub-device",
					zap.Any("received_data", receivedData))
			}

			// 验证下发日志
			time.Sleep(1 * time.Second)
			logs, err := dbClient.QueryTelemetrySetLogs(subGwSubDev.DeviceID, startTime)
			require.NoError(t, err)
			assert.NotEmpty(t, logs, "No telemetry set log found for sub-gateway's sub-device")

			if len(logs) > 0 {
				log := logs[0]
				// 验证状态为成功
				assert.Equal(t, "1", log.Status, "Sub-gateway's sub-device control status should be success(1)")

				// 验证日志数据包含完整的嵌套路径
				assert.NotEmpty(t, log.Data, "Sub-gateway's sub-device control log data should not be empty")
				// 应该包含子网关编号和子设备编号
				assert.Contains(t, log.Data, subGw.SubGatewayNumber,
					"Log data should contain sub-gateway number: %s", subGw.SubGatewayNumber)
				assert.Contains(t, log.Data, subGwSubDev.SubDeviceNumber,
					"Log data should contain sub-device number: %s", subGwSubDev.SubDeviceNumber)

				logger.Info("Sub-gateway's sub-device control log verified",
					zap.String("sub_device_number", subGwSubDev.SubDeviceNumber),
					zap.String("parent_sub_gateway", subGw.SubGatewayNumber),
					zap.String("status", log.Status),
					zap.String("data", log.Data))
			}
		})
	}

	logger.Info("Gateway telemetry control test completed successfully")
}
