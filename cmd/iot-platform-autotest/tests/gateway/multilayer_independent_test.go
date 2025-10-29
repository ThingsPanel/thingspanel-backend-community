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

// TestMultiLayerGateway_OnlyTopGatewayData 测试场景：仅顶层网关上报数据
func TestMultiLayerGateway_OnlyTopGatewayData(t *testing.T) {
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

	// 创建数据库客户端
	dbClient, err := platform.NewDBClient(&cfg.Database, logger)
	require.NoError(t, err)
	defer dbClient.Close()

	// 构建测试数据：仅包含顶层网关数据
	gatewayData := map[string]interface{}{
		"temperature": 26.8,
		"humidity":    65.0,
		"status":      "online",
	}

	testData := protocol.BuildNestedTelemetry(gatewayData, nil, nil)

	startTime := time.Now()

	logger.Info("Testing: Only top gateway data",
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
				zap.Any("value", records[0]))
		}
	}

	logger.Info("Test completed: Only top gateway data")
}

// TestMultiLayerGateway_OnlySubGatewayData 测试场景：仅子网关上报数据（无 gateway_data）
func TestMultiLayerGateway_OnlySubGatewayData(t *testing.T) {
	// 初始化日志
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	defer logger.Sync()

	// 加载配置
	cfg, err := config.Load("../../config-gateway-community.yaml")
	require.NoError(t, err)

	// 验证是网关设备
	require.Equal(t, "gateway", cfg.DeviceType, "Test requires gateway device type")
	require.NotEmpty(t, cfg.Gateway.SubGateways, "Test requires sub-gateway configuration")

	// 创建网关设备
	dev, err := device.NewDevice(cfg, logger)
	require.NoError(t, err)
	require.NoError(t, dev.Connect())
	defer dev.Disconnect()

	// 创建数据库客户端
	dbClient, err := platform.NewDBClient(&cfg.Database, logger)
	require.NoError(t, err)
	defer dbClient.Close()

	// 构建测试数据：仅包含子网关数据，但子网关没有 gateway_data
	subGw := cfg.Gateway.SubGateways[0]

	subGatewayData := make(map[string]interface{})
	subGwData := map[string]interface{}{
		// 注意：这里故意不包含 gateway_data
		"sub_device_data": map[string]interface{}{
			subGw.SubDevices[0].SubDeviceNumber: map[string]interface{}{
				"temperature": 27.0,
				"humidity":    60.0,
			},
		},
	}
	subGatewayData[subGw.SubGatewayNumber] = subGwData

	testData := protocol.BuildNestedTelemetry(nil, nil, subGatewayData)

	startTime := time.Now()

	logger.Info("Testing: Only sub-gateway's sub-device data (no gateway_data for sub-gateway)",
		zap.Time("start_time", startTime),
		zap.String("sub_gateway_number", subGw.SubGatewayNumber),
		zap.Any("test_data", testData))

	// 发送遥测数据
	err = dev.PublishTelemetry(testData)
	require.NoError(t, err)

	// 等待数据同步
	time.Sleep(time.Duration(cfg.Test.WaitDBSyncSeconds) * time.Second)

	// 验证子网关下的子设备数据（这是关键测试点）
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
				zap.String("key", "temperature"),
				zap.Any("value", records[0]))
		}
	}

	logger.Info("Test completed: Only sub-gateway's sub-device data without gateway_data")
}

// TestMultiLayerGateway_OnlySubGatewayOwnData 测试场景：仅子网关自身上报数据（有 gateway_data，无 sub_device_data）
func TestMultiLayerGateway_OnlySubGatewayOwnData(t *testing.T) {
	// 初始化日志
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	defer logger.Sync()

	// 加载配置
	cfg, err := config.Load("../../config-gateway-community.yaml")
	require.NoError(t, err)

	// 验证是网关设备
	require.Equal(t, "gateway", cfg.DeviceType, "Test requires gateway device type")
	require.NotEmpty(t, cfg.Gateway.SubGateways, "Test requires sub-gateway configuration")

	// 创建网关设备
	dev, err := device.NewDevice(cfg, logger)
	require.NoError(t, err)
	require.NoError(t, dev.Connect())
	defer dev.Disconnect()

	// 创建数据库客户端
	dbClient, err := platform.NewDBClient(&cfg.Database, logger)
	require.NoError(t, err)
	defer dbClient.Close()

	// 构建测试数据：仅包含子网关自身数据
	subGw := cfg.Gateway.SubGateways[0]

	subGatewayData := make(map[string]interface{})
	subGwData := map[string]interface{}{
		"gateway_data": map[string]interface{}{
			"temperature": 28.5,
			"cpu_usage":   45.2,
			"memory":      "2048MB",
		},
		// 注意：这里故意不包含 sub_device_data
	}
	subGatewayData[subGw.SubGatewayNumber] = subGwData

	testData := protocol.BuildNestedTelemetry(nil, nil, subGatewayData)

	startTime := time.Now()

	logger.Info("Testing: Only sub-gateway own data (no sub_device_data)",
		zap.Time("start_time", startTime),
		zap.String("sub_gateway_number", subGw.SubGatewayNumber),
		zap.Any("test_data", testData))

	// 发送遥测数据
	err = dev.PublishTelemetry(testData)
	require.NoError(t, err)

	// 等待数据同步
	time.Sleep(time.Duration(cfg.Test.WaitDBSyncSeconds) * time.Second)

	// 验证子网关自身数据
	logger.Info("Verifying sub-gateway own data",
		zap.String("sub_gateway_id", subGw.DeviceID),
		zap.String("sub_gateway_number", subGw.SubGatewayNumber))

	records, err := dbClient.QueryTelemetryData(subGw.DeviceID, "temperature", startTime)
	require.NoError(t, err)
	assert.NotEmpty(t, records, "No telemetry data found for sub-gateway")

	if len(records) > 0 {
		logger.Info("Sub-gateway telemetry data verified",
			zap.String("sub_gateway_number", subGw.SubGatewayNumber),
			zap.String("key", "temperature"),
			zap.Any("value", records[0]))
	}

	logger.Info("Test completed: Only sub-gateway own data")
}

// TestMultiLayerGateway_OnlyTopSubDeviceData 测试场景：仅顶层网关的直连子设备上报数据
func TestMultiLayerGateway_OnlyTopSubDeviceData(t *testing.T) {
	// 初始化日志
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	defer logger.Sync()

	// 加载配置
	cfg, err := config.Load("../../config-gateway-community.yaml")
	require.NoError(t, err)

	// 验证是网关设备
	require.Equal(t, "gateway", cfg.DeviceType, "Test requires gateway device type")
	require.NotEmpty(t, cfg.Gateway.SubDevices, "Test requires sub-device configuration")

	// 创建网关设备
	dev, err := device.NewDevice(cfg, logger)
	require.NoError(t, err)
	require.NoError(t, dev.Connect())
	defer dev.Disconnect()

	// 创建数据库客户端
	dbClient, err := platform.NewDBClient(&cfg.Database, logger)
	require.NoError(t, err)
	defer dbClient.Close()

	// 构建测试数据：仅包含顶层网关的直连子设备数据
	subDev := cfg.Gateway.SubDevices[0]
	subDeviceData := map[string]interface{}{
		subDev.SubDeviceNumber: map[string]interface{}{
			"temperature": 25.0,
			"switch":      true,
			"voltage":     220.5,
		},
	}

	testData := protocol.BuildNestedTelemetry(nil, subDeviceData, nil)

	startTime := time.Now()

	logger.Info("Testing: Only top gateway's sub-device data",
		zap.Time("start_time", startTime),
		zap.String("sub_device_number", subDev.SubDeviceNumber),
		zap.Any("test_data", testData))

	// 发送遥测数据
	err = dev.PublishTelemetry(testData)
	require.NoError(t, err)

	// 等待数据同步
	time.Sleep(time.Duration(cfg.Test.WaitDBSyncSeconds) * time.Second)

	// 验证子设备数据
	logger.Info("Verifying sub-device data",
		zap.String("sub_device_id", subDev.DeviceID),
		zap.String("sub_device_number", subDev.SubDeviceNumber))

	records, err := dbClient.QueryTelemetryData(subDev.DeviceID, "temperature", startTime)
	require.NoError(t, err)
	assert.NotEmpty(t, records, "No telemetry data found for sub-device")

	if len(records) > 0 {
		logger.Info("Sub-device telemetry data verified",
			zap.String("sub_device_number", subDev.SubDeviceNumber),
			zap.String("key", "temperature"),
			zap.Any("value", records[0]))
	}

	logger.Info("Test completed: Only top gateway's sub-device data")
}

// TestMultiLayerGateway_MixedDataCombinations 测试场景：各种数据组合
func TestMultiLayerGateway_MixedDataCombinations(t *testing.T) {
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

	// 创建数据库客户端
	dbClient, err := platform.NewDBClient(&cfg.Database, logger)
	require.NoError(t, err)
	defer dbClient.Close()

	// 测试用例1：顶层网关 + 子网关（无gateway_data）+ 子网关的子设备
	t.Run("TopGateway_And_SubGatewaySubDevice_NoGatewayData", func(t *testing.T) {
		if len(cfg.Gateway.SubGateways) == 0 {
			t.Skip("No sub-gateway configured")
		}

		subGw := cfg.Gateway.SubGateways[0]

		gatewayData := map[string]interface{}{
			"temperature": 26.0,
		}

		subGatewayData := make(map[string]interface{})
		subGwData := map[string]interface{}{
			// 子网关没有自身数据，只有子设备数据
			"sub_device_data": map[string]interface{}{
				subGw.SubDevices[0].SubDeviceNumber: map[string]interface{}{
					"temperature": 27.5,
					"humidity":    58.0,
				},
			},
		}
		subGatewayData[subGw.SubGatewayNumber] = subGwData

		testData := protocol.BuildNestedTelemetry(gatewayData, nil, subGatewayData)

		startTime := time.Now()

		logger.Info("Testing combination: Top gateway + Sub-gateway's sub-device (no gateway_data)",
			zap.Any("test_data", testData))

		err := dev.PublishTelemetry(testData)
		require.NoError(t, err)

		time.Sleep(time.Duration(cfg.Test.WaitDBSyncSeconds) * time.Second)

		// 验证顶层网关数据
		records, err := dbClient.QueryTelemetryData(cfg.Device.DeviceID, "temperature", startTime)
		require.NoError(t, err)
		assert.NotEmpty(t, records, "No data for top gateway")

		// 验证子网关的子设备数据
		if len(subGw.SubDevices) > 0 {
			subGwSubDev := subGw.SubDevices[0]
			records, err := dbClient.QueryTelemetryData(subGwSubDev.DeviceID, "temperature", startTime)
			require.NoError(t, err)
			assert.NotEmpty(t, records, "No data for sub-gateway's sub-device")

			logger.Info("Combination test passed: Data verified for both top gateway and sub-gateway's sub-device")
		}
	})

	// 测试用例2：仅子网关（无gateway_data）+ 子网关的子设备
	t.Run("OnlySubGatewaySubDevice_NoGatewayData", func(t *testing.T) {
		if len(cfg.Gateway.SubGateways) == 0 {
			t.Skip("No sub-gateway configured")
		}

		subGw := cfg.Gateway.SubGateways[0]

		subGatewayData := make(map[string]interface{})
		subGwData := map[string]interface{}{
			// 只有子设备数据，没有 gateway_data
			"sub_device_data": map[string]interface{}{
				subGw.SubDevices[0].SubDeviceNumber: map[string]interface{}{
					"temperature": 29.5,
					"humidity":    62.0,
				},
			},
		}
		subGatewayData[subGw.SubGatewayNumber] = subGwData

		testData := protocol.BuildNestedTelemetry(nil, nil, subGatewayData)

		startTime := time.Now()

		logger.Info("Testing combination: Only sub-gateway's sub-device (no gateway_data)",
			zap.Any("test_data", testData))

		err := dev.PublishTelemetry(testData)
		require.NoError(t, err)

		time.Sleep(time.Duration(cfg.Test.WaitDBSyncSeconds) * time.Second)

		// 验证子网关的子设备数据
		if len(subGw.SubDevices) > 0 {
			subGwSubDev := subGw.SubDevices[0]
			records, err := dbClient.QueryTelemetryData(subGwSubDev.DeviceID, "temperature", startTime)
			require.NoError(t, err)
			assert.NotEmpty(t, records, "No data for sub-gateway's sub-device")

			logger.Info("Combination test passed: Sub-gateway's sub-device data verified without gateway_data")
		}
	})

	logger.Info("All combination tests completed")
}

// TestMultiLayerGateway_AttributeWithoutGatewayData 测试场景：属性上报 - 子网关无 gateway_data
func TestMultiLayerGateway_AttributeWithoutGatewayData(t *testing.T) {
	// 初始化日志
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	defer logger.Sync()

	// 加载配置
	cfg, err := config.Load("../../config-gateway-community.yaml")
	require.NoError(t, err)

	// 验证是网关设备
	require.Equal(t, "gateway", cfg.DeviceType, "Test requires gateway device type")
	require.NotEmpty(t, cfg.Gateway.SubGateways, "Test requires sub-gateway configuration")

	// 创建网关设备
	dev, err := device.NewDevice(cfg, logger)
	require.NoError(t, err)
	require.NoError(t, dev.Connect())
	defer dev.Disconnect()

	// 创建数据库客户端
	dbClient, err := platform.NewDBClient(&cfg.Database, logger)
	require.NoError(t, err)
	defer dbClient.Close()

	// 构建测试数据：子网关的子设备属性，但子网关无 gateway_data
	subGw := cfg.Gateway.SubGateways[0]

	subGatewayData := make(map[string]interface{})
	subGwData := map[string]interface{}{
		// 注意：没有 gateway_data
		"sub_device_data": map[string]interface{}{
			subGw.SubDevices[0].SubDeviceNumber: map[string]interface{}{
				"sensor_type": "temperature",
				"calibrated":  true,
				"interval":    float64(60),
			},
		},
	}
	subGatewayData[subGw.SubGatewayNumber] = subGwData

	testData := protocol.BuildNestedAttributes(nil, nil, subGatewayData)

	logger.Info("Testing attribute: Sub-gateway's sub-device without gateway_data",
		zap.String("sub_gateway_number", subGw.SubGatewayNumber),
		zap.Any("test_data", testData))

	// 生成 message_id
	messageID := "attr_test_" + time.Now().Format("150405")

	// 发送属性数据
	err = dev.PublishAttribute(testData, messageID)
	require.NoError(t, err)

	// 等待数据同步
	time.Sleep(time.Duration(cfg.Test.WaitDBSyncSeconds) * time.Second)

	// 验证子网关的子设备属性数据
	if len(subGw.SubDevices) > 0 {
		subGwSubDev := subGw.SubDevices[0]
		logger.Info("Verifying sub-gateway's sub-device attribute data",
			zap.String("device_id", subGwSubDev.DeviceID),
			zap.String("sub_device_number", subGwSubDev.SubDeviceNumber))

		record, err := dbClient.QueryAttributeData(subGwSubDev.DeviceID, "sensor_type")
		require.NoError(t, err)
		assert.NotNil(t, record, "No attribute data found for sub-gateway's sub-device")

		if record != nil {
			logger.Info("Sub-gateway's sub-device attribute data verified",
				zap.String("key", "sensor_type"),
				zap.Any("record", record))
		}
	}

	logger.Info("Attribute test completed: Sub-gateway's sub-device without gateway_data")
}

// TestMultiLayerGateway_EventWithoutGatewayData 测试场景：事件上报 - 子网关无 gateway_data
func TestMultiLayerGateway_EventWithoutGatewayData(t *testing.T) {
	// 初始化日志
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	defer logger.Sync()

	// 加载配置
	cfg, err := config.Load("../../config-gateway-community.yaml")
	require.NoError(t, err)

	// 验证是网关设备
	require.Equal(t, "gateway", cfg.DeviceType, "Test requires gateway device type")
	require.NotEmpty(t, cfg.Gateway.SubGateways, "Test requires sub-gateway configuration")

	// 创建网关设备
	dev, err := device.NewDevice(cfg, logger)
	require.NoError(t, err)
	require.NoError(t, dev.Connect())
	defer dev.Disconnect()

	// 创建数据库客户端
	dbClient, err := platform.NewDBClient(&cfg.Database, logger)
	require.NoError(t, err)
	defer dbClient.Close()

	// 构建测试数据：子网关的子设备事件，但子网关无 gateway_data
	subGw := cfg.Gateway.SubGateways[0]

	// 为网关事件构建嵌套数据
	subGatewayData := make(map[string]interface{})
	subGwData := map[string]interface{}{
		// 注意：没有 gateway_data
		"sub_device_data": map[string]interface{}{
			subGw.SubDevices[0].SubDeviceNumber: map[string]interface{}{
				"method": "TemperatureAlert",
				"params": map[string]interface{}{
					"temperature": 35.5,
					"threshold":   30.0,
					"timestamp":   time.Now().Unix(),
				},
			},
		},
	}
	subGatewayData[subGw.SubGatewayNumber] = subGwData

	testData := protocol.BuildNestedEvents(nil, nil, subGatewayData)

	startTime := time.Now()

	logger.Info("Testing event: Sub-gateway's sub-device without gateway_data",
		zap.String("sub_gateway_number", subGw.SubGatewayNumber),
		zap.Any("test_data", testData))

	// 生成 message_id
	messageID := "event_test_" + time.Now().Format("150405")

	// 发送事件数据 (method 为空字符串表示网关事件，params 是完整的嵌套数据)
	err = dev.PublishEvent("", testData, messageID)
	require.NoError(t, err)

	// 等待数据同步
	time.Sleep(time.Duration(cfg.Test.WaitDBSyncSeconds) * time.Second)

	// 验证子网关的子设备事件数据
	if len(subGw.SubDevices) > 0 {
		subGwSubDev := subGw.SubDevices[0]
		logger.Info("Verifying sub-gateway's sub-device event data",
			zap.String("device_id", subGwSubDev.DeviceID),
			zap.String("sub_device_number", subGwSubDev.SubDeviceNumber))

		records, err := dbClient.QueryEventData(subGwSubDev.DeviceID, "TemperatureAlert", startTime)
		require.NoError(t, err)
		assert.NotEmpty(t, records, "No event data found for sub-gateway's sub-device")

		if len(records) > 0 {
			logger.Info("Sub-gateway's sub-device event data verified",
				zap.String("method", "TemperatureAlert"),
				zap.Any("record", records[0]))
		}
	}

	logger.Info("Event test completed: Sub-gateway's sub-device without gateway_data")
}
