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

func TestGatewayTelemetryPublish(t *testing.T) {
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

	// 构建嵌套遥测数据
	// 包含: 网关自身数据 + 子设备数据 + 子网关数据
	gatewayData := map[string]interface{}{
		"temperature": 26.8,
		"humidity":    65.0,
	}

	subDeviceData := make(map[string]interface{})
	// 如果配置中有子设备，添加其数据
	if len(cfg.Gateway.SubDevices) > 0 {
		subDev := cfg.Gateway.SubDevices[0]
		subDeviceData[subDev.SubDeviceNumber] = map[string]interface{}{
			"temperature": 25.0,
			"switch":      true,
		}
	}

	subGatewayData := make(map[string]interface{})
	// 如果配置中有子网关，添加其数据
	if len(cfg.Gateway.SubGateways) > 0 {
		subGw := cfg.Gateway.SubGateways[0]
		subGwData := map[string]interface{}{
			"gateway_data": map[string]interface{}{
				"temperature": 28.5,
				"version":     "v1.0",
			},
		}

		// 如果子网关有子设备，添加子设备数据
		if len(subGw.SubDevices) > 0 {
			subGwSubDevData := make(map[string]interface{})
			for _, subDev := range subGw.SubDevices {
				subGwSubDevData[subDev.SubDeviceNumber] = map[string]interface{}{
					"temperature": 27.0,
					"humidity":    60.0,
				}
			}
			subGwData["sub_device_data"] = subGwSubDevData
		}

		subGatewayData[subGw.SubGatewayNumber] = subGwData
	}

	// 使用辅助函数构建完整的嵌套数据
	testData := protocol.BuildNestedTelemetry(gatewayData, subDeviceData, subGatewayData)

	startTime := time.Now()

	logger.Info("Starting gateway telemetry test",
		zap.Time("start_time", startTime),
		zap.Any("test_data", testData))

	// 发送遥测数据
	err = dev.PublishTelemetry(testData)
	require.NoError(t, err)

	// 等待数据同步
	time.Sleep(time.Duration(cfg.Test.WaitDBSyncSeconds) * time.Second)

	// 验证网关自身数据
	logger.Info("Verifying gateway data",
		zap.String("gateway_device_id", cfg.Device.DeviceID))

	for key := range gatewayData {
		records, err := dbClient.QueryTelemetryData(cfg.Device.DeviceID, key, startTime)
		require.NoError(t, err)
		assert.NotEmpty(t, records, "No telemetry data found for gateway key: %s", key)

		if len(records) > 0 {
			logger.Info("Gateway telemetry data verified",
				zap.String("key", key),
				zap.String("device_id", cfg.Device.DeviceID))
		}
	}

	// 验证子设备数据
	if len(cfg.Gateway.SubDevices) > 0 {
		subDev := cfg.Gateway.SubDevices[0]
		logger.Info("Verifying sub-device data",
			zap.String("sub_device_id", subDev.DeviceID),
			zap.String("sub_device_number", subDev.SubDeviceNumber))

		records, err := dbClient.QueryTelemetryData(subDev.DeviceID, "temperature", startTime)
		require.NoError(t, err)
		assert.NotEmpty(t, records, "No telemetry data found for sub-device")

		if len(records) > 0 {
			logger.Info("Sub-device telemetry data verified",
				zap.String("sub_device_number", subDev.SubDeviceNumber),
				zap.String("key", "temperature"))
		}
	}

	// 验证子网关数据
	if len(cfg.Gateway.SubGateways) > 0 {
		subGw := cfg.Gateway.SubGateways[0]
		logger.Info("Verifying sub-gateway data",
			zap.String("sub_gateway_id", subGw.DeviceID),
			zap.String("sub_gateway_number", subGw.SubGatewayNumber))

		records, err := dbClient.QueryTelemetryData(subGw.DeviceID, "temperature", startTime)
		require.NoError(t, err)
		assert.NotEmpty(t, records, "No telemetry data found for sub-gateway")

		if len(records) > 0 {
			logger.Info("Sub-gateway telemetry data verified",
				zap.String("sub_gateway_number", subGw.SubGatewayNumber),
				zap.String("key", "temperature"))
		}

		// 验证子网关下的子设备数据
		if len(subGw.SubDevices) > 0 {
			subGwSubDev := subGw.SubDevices[0]
			logger.Info("Verifying sub-gateway's sub-device data",
				zap.String("device_id", subGwSubDev.DeviceID),
				zap.String("sub_device_number", subGwSubDev.SubDeviceNumber))

			records, err := dbClient.QueryTelemetryData(subGwSubDev.DeviceID, "temperature", startTime)
			require.NoError(t, err)
			assert.NotEmpty(t, records, "No telemetry data found for sub-gateway's sub-device")

			if len(records) > 0 {
				logger.Info("Sub-gateway's sub-device telemetry data verified",
					zap.String("sub_device_number", subGwSubDev.SubDeviceNumber),
					zap.String("key", "temperature"))
			}
		}
	}

	logger.Info("Gateway telemetry test completed successfully")
}
