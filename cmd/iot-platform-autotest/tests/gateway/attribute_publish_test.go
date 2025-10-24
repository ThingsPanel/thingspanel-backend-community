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

func TestGatewayAttributePublish(t *testing.T) {
	// 初始化日志
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	defer logger.Sync()

	// 加载配置
	cfg, err := config.Load("../../config-gateway.yaml")
	require.NoError(t, err)

	// 验证是网关设备
	require.Equal(t, "gateway", cfg.DeviceType, "Test requires gateway device type")

	// 创建网关设备
	dev, err := device.NewDevice(cfg, logger)
	require.NoError(t, err)
	require.NoError(t, dev.Connect())
	defer dev.Disconnect()

	// 创建数据库客户端
	dbClient, err := platform.NewDBClient(&cfg.Database, logger)
	require.NoError(t, err)
	defer dbClient.Close()

	logger.Info("Starting gateway attribute publish test",
		zap.String("gateway_number", cfg.Device.DeviceNumber))

	// 构建嵌套属性数据
	// 包含: 网关自身数据 + 子设备数据 + 子网关数据
	gatewayData := map[string]interface{}{
		"device_name": "Gateway-Main",
		"location":    "Building A",
		"firmware":    "v2.0.1",
	}

	subDeviceData := make(map[string]interface{})
	// 如果配置中有子设备，添加其数据
	if len(cfg.Gateway.SubDevices) > 0 {
		subDev := cfg.Gateway.SubDevices[0]
		subDeviceData[subDev.SubDeviceNumber] = map[string]interface{}{
			"sensor_type": "temperature",
			"calibrated":  true,
			"range":       float64(100),
		}
	}

	subGatewayData := make(map[string]interface{})
	// 如果配置中有子网关，添加其数据
	if len(cfg.Gateway.SubGateways) > 0 {
		subGw := cfg.Gateway.SubGateways[0]
		subGwData := map[string]interface{}{
			"gateway_data": map[string]interface{}{
				"gateway_type": "edge",
				"protocol":     "mqtt",
				"max_devices":  float64(50),
			},
		}

		// 如果子网关有子设备，添加子设备数据
		if len(subGw.SubDevices) > 0 {
			subGwSubDevData := make(map[string]interface{})
			for _, subDev := range subGw.SubDevices {
				subGwSubDevData[subDev.SubDeviceNumber] = map[string]interface{}{
					"sensor_model": "TH-100",
					"calibrated":   true,
					"interval":     float64(60),
				}
			}
			subGwData["sub_device_data"] = subGwSubDevData
		}

		subGatewayData[subGw.SubGatewayNumber] = subGwData
	}

	// 使用辅助函数构建完整的嵌套数据
	testData := protocol.BuildNestedAttributes(gatewayData, subDeviceData, subGatewayData)

	logger.Info("Publishing attribute data",
		zap.Any("test_data", testData))

	// 生成 message_id
	messageID := "attr_" + time.Now().Format("150405")

	// 发送属性数据
	err = dev.PublishAttribute(testData, messageID)
	require.NoError(t, err)

	// 等待数据同步
	time.Sleep(time.Duration(cfg.Test.WaitDBSyncSeconds) * time.Second)

	// 验证网关自身属性数据
	t.Run("Verify_Gateway_Attributes", func(t *testing.T) {
		logger.Info("Verifying gateway attribute data",
			zap.String("gateway_device_id", cfg.Device.DeviceID))

		for key := range gatewayData {
			record, err := dbClient.QueryAttributeData(cfg.Device.DeviceID, key)
			require.NoError(t, err)
			assert.NotNil(t, record, "No attribute data found for gateway key: %s", key)

			if record != nil {
				logger.Info("Gateway attribute data verified",
					zap.String("key", key),
					zap.String("device_id", cfg.Device.DeviceID))
			}
		}
	})

	// 验证子设备属性数据
	if len(cfg.Gateway.SubDevices) > 0 {
		t.Run("Verify_SubDevice_Attributes", func(t *testing.T) {
			subDev := cfg.Gateway.SubDevices[0]
			logger.Info("Verifying sub-device attribute data",
				zap.String("sub_device_id", subDev.DeviceID),
				zap.String("sub_device_number", subDev.SubDeviceNumber))

			// 获取该子设备的属性数据
			subDevAttrs := subDeviceData[subDev.SubDeviceNumber].(map[string]interface{})
			for key := range subDevAttrs {
				record, err := dbClient.QueryAttributeData(subDev.DeviceID, key)
				require.NoError(t, err)
				assert.NotNil(t, record, "No attribute data found for sub-device key: %s", key)

				if record != nil {
					logger.Info("Sub-device attribute data verified",
						zap.String("key", key),
						zap.String("sub_device_number", subDev.SubDeviceNumber))
				}
			}
		})
	}

	// 验证子网关属性数据
	if len(cfg.Gateway.SubGateways) > 0 {
		t.Run("Verify_SubGateway_Attributes", func(t *testing.T) {
			subGw := cfg.Gateway.SubGateways[0]
			logger.Info("Verifying sub-gateway attribute data",
				zap.String("sub_gateway_id", subGw.DeviceID),
				zap.String("sub_gateway_number", subGw.SubGatewayNumber))

			// 获取子网关的属性数据
			subGwData := subGatewayData[subGw.SubGatewayNumber].(map[string]interface{})
			subGwAttrs := subGwData["gateway_data"].(map[string]interface{})

			for key := range subGwAttrs {
				record, err := dbClient.QueryAttributeData(subGw.DeviceID, key)
				require.NoError(t, err)
				assert.NotNil(t, record, "No attribute data found for sub-gateway key: %s", key)

				if record != nil {
					logger.Info("Sub-gateway attribute data verified",
						zap.String("key", key),
						zap.String("sub_gateway_number", subGw.SubGatewayNumber))
				}
			}
		})

		// 验证子网关下的子设备属性数据
		if len(cfg.Gateway.SubGateways[0].SubDevices) > 0 {
			t.Run("Verify_SubGateway_SubDevice_Attributes", func(t *testing.T) {
				subGw := cfg.Gateway.SubGateways[0]
				subGwSubDev := subGw.SubDevices[0]
				logger.Info("Verifying sub-gateway's sub-device attribute data",
					zap.String("device_id", subGwSubDev.DeviceID),
					zap.String("sub_device_number", subGwSubDev.SubDeviceNumber))

				// 获取子网关的子设备属性数据
				subGwData := subGatewayData[subGw.SubGatewayNumber].(map[string]interface{})
				subGwSubDevData := subGwData["sub_device_data"].(map[string]interface{})
				subDevAttrs := subGwSubDevData[subGwSubDev.SubDeviceNumber].(map[string]interface{})

				for key := range subDevAttrs {
					record, err := dbClient.QueryAttributeData(subGwSubDev.DeviceID, key)
					require.NoError(t, err)
					assert.NotNil(t, record, "No attribute data found for sub-gateway's sub-device key: %s", key)

					if record != nil {
						logger.Info("Sub-gateway's sub-device attribute data verified",
							zap.String("key", key),
							zap.String("sub_device_number", subGwSubDev.SubDeviceNumber),
							zap.String("parent_sub_gateway", subGw.SubGatewayNumber))
					}
				}
			})
		}
	}

	logger.Info("Gateway attribute publish test completed successfully")
}
