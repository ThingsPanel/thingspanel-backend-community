package tests

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"iot-platform-autotest/internal/config"
	"iot-platform-autotest/internal/device"
	"iot-platform-autotest/internal/platform"
	"iot-platform-autotest/internal/protocol"
)

func TestGatewayEventPublish(t *testing.T) {
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

	// 创建数据库客户端
	dbClient, err := platform.NewDBClient(&cfg.Database, logger)
	require.NoError(t, err)
	defer dbClient.Close()

	logger.Info("Starting gateway event publish test",
		zap.String("gateway_number", cfg.Device.DeviceNumber))

	// 构建嵌套事件数据
	// 包含: 网关自身事件 + 子设备事件 + 子网关事件
	gatewayEvent := map[string]interface{}{
		"method": "GatewayRestart",
		"params": map[string]interface{}{
			"reason":   "software_update",
			"duration": float64(30),
		},
	}

	subDeviceEvents := make(map[string]interface{})
	// 如果配置中有子设备，添加其事件
	if len(cfg.Gateway.SubDevices) > 0 {
		subDev := cfg.Gateway.SubDevices[0]
		subDeviceEvents[subDev.SubDeviceNumber] = map[string]interface{}{
			"method": "AlarmTriggered",
			"params": map[string]interface{}{
				"alarm_type": "temperature_high",
				"level":      "critical",
				"value":      float64(85),
			},
		}
	}

	subGatewayEvents := make(map[string]interface{})
	// 如果配置中有子网关，添加其事件
	if len(cfg.Gateway.SubGateways) > 0 {
		subGw := cfg.Gateway.SubGateways[0]
		subGwEvent := map[string]interface{}{
			"gateway_data": map[string]interface{}{
				"method": "ConnectionLost",
				"params": map[string]interface{}{
					"reason":   "network_timeout",
					"duration": float64(15),
				},
			},
		}

		// 如果子网关有子设备，添加子设备事件
		if len(subGw.SubDevices) > 0 {
			subGwSubDevEvents := make(map[string]interface{})
			for _, subDev := range subGw.SubDevices {
				subGwSubDevEvents[subDev.SubDeviceNumber] = map[string]interface{}{
					"method": "BatteryLow",
					"params": map[string]interface{}{
						"battery_level": float64(10),
						"threshold":     float64(20),
					},
				}
			}
			subGwEvent["sub_device_data"] = subGwSubDevEvents
		}

		subGatewayEvents[subGw.SubGatewayNumber] = subGwEvent
	}

	// 使用辅助函数构建完整的嵌套数据
	testData := protocol.BuildNestedEvents(gatewayEvent, subDeviceEvents, subGatewayEvents)

	startTime := time.Now()

	logger.Info("Publishing event data",
		zap.Any("test_data", testData))

	// 生成 message_id
	messageID := "event_" + time.Now().Format("150405")

	// 发送事件数据（使用 PublishEvent，method 从数据中提取）
	err = dev.PublishEvent("", testData, messageID)
	require.NoError(t, err)

	// 等待数据同步
	time.Sleep(time.Duration(cfg.Test.WaitDBSyncSeconds) * time.Second)

	// 验证网关自身事件数据
	t.Run("Verify_Gateway_Event", func(t *testing.T) {
		logger.Info("Verifying gateway event data",
			zap.String("gateway_device_id", cfg.Device.DeviceID))

		method := gatewayEvent["method"].(string)
		records, err := dbClient.QueryEventData(cfg.Device.DeviceID, method, startTime)
		require.NoError(t, err)
		assert.NotEmpty(t, records, "No event data found for gateway")

		if len(records) > 0 {
			record := records[0]
			assert.Equal(t, cfg.Device.DeviceID, record.DeviceID)
			assert.Equal(t, method, record.Identify)

			logger.Info("Gateway event data verified",
				zap.String("method", method),
				zap.String("device_id", cfg.Device.DeviceID),
				zap.Time("event_time", record.TS))
		}
	})

	// 验证子设备事件数据
	if len(cfg.Gateway.SubDevices) > 0 {
		t.Run("Verify_SubDevice_Event", func(t *testing.T) {
			subDev := cfg.Gateway.SubDevices[0]
			logger.Info("Verifying sub-device event data",
				zap.String("sub_device_id", subDev.DeviceID),
				zap.String("sub_device_number", subDev.SubDeviceNumber))

			subDevEvent := subDeviceEvents[subDev.SubDeviceNumber].(map[string]interface{})
			method := subDevEvent["method"].(string)

			records, err := dbClient.QueryEventData(subDev.DeviceID, method, startTime)
			require.NoError(t, err)
			assert.NotEmpty(t, records, "No event data found for sub-device")

			if len(records) > 0 {
				record := records[0]
				assert.Equal(t, subDev.DeviceID, record.DeviceID)
				assert.Equal(t, method, record.Identify)

				logger.Info("Sub-device event data verified",
					zap.String("method", method),
					zap.String("sub_device_number", subDev.SubDeviceNumber),
					zap.Time("event_time", record.TS))
			}
		})
	}

	// 验证子网关事件数据
	if len(cfg.Gateway.SubGateways) > 0 {
		t.Run("Verify_SubGateway_Event", func(t *testing.T) {
			subGw := cfg.Gateway.SubGateways[0]
			logger.Info("Verifying sub-gateway event data",
				zap.String("sub_gateway_id", subGw.DeviceID),
				zap.String("sub_gateway_number", subGw.SubGatewayNumber))

			subGwEvent := subGatewayEvents[subGw.SubGatewayNumber].(map[string]interface{})
			subGwGatewayData := subGwEvent["gateway_data"].(map[string]interface{})
			method := subGwGatewayData["method"].(string)

			records, err := dbClient.QueryEventData(subGw.DeviceID, method, startTime)
			require.NoError(t, err)
			assert.NotEmpty(t, records, "No event data found for sub-gateway")

			if len(records) > 0 {
				record := records[0]
				assert.Equal(t, subGw.DeviceID, record.DeviceID)
				assert.Equal(t, method, record.Identify)

				logger.Info("Sub-gateway event data verified",
					zap.String("method", method),
					zap.String("sub_gateway_number", subGw.SubGatewayNumber),
					zap.Time("event_time", record.TS))
			}
		})

		// 验证子网关下的子设备事件数据
		if len(cfg.Gateway.SubGateways[0].SubDevices) > 0 {
			t.Run("Verify_SubGateway_SubDevice_Event", func(t *testing.T) {
				subGw := cfg.Gateway.SubGateways[0]
				subGwSubDev := subGw.SubDevices[0]
				logger.Info("Verifying sub-gateway's sub-device event data",
					zap.String("device_id", subGwSubDev.DeviceID),
					zap.String("sub_device_number", subGwSubDev.SubDeviceNumber))

				subGwEvent := subGatewayEvents[subGw.SubGatewayNumber].(map[string]interface{})
				subGwSubDevEvents := subGwEvent["sub_device_data"].(map[string]interface{})
				subDevEvent := subGwSubDevEvents[subGwSubDev.SubDeviceNumber].(map[string]interface{})
				method := subDevEvent["method"].(string)

				records, err := dbClient.QueryEventData(subGwSubDev.DeviceID, method, startTime)
				require.NoError(t, err)
				assert.NotEmpty(t, records, "No event data found for sub-gateway's sub-device")

				if len(records) > 0 {
					record := records[0]
					assert.Equal(t, subGwSubDev.DeviceID, record.DeviceID)
					assert.Equal(t, method, record.Identify)

					logger.Info("Sub-gateway's sub-device event data verified",
						zap.String("method", method),
						zap.String("sub_device_number", subGwSubDev.SubDeviceNumber),
						zap.String("parent_sub_gateway", subGw.SubGatewayNumber),
						zap.Time("event_time", record.TS))
				}
			})
		}
	}

	logger.Info("Gateway event publish test completed successfully")
}
