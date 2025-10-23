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

func TestTelemetryPublish(t *testing.T) {
	// 初始化日志
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	defer logger.Sync()

	// 加载配置
	cfg, err := config.Load("../../config-community.yaml")
	require.NoError(t, err)

	// 创建设备
	dev, err := device.NewDevice(cfg, logger)
	require.NoError(t, err)
	require.NoError(t, dev.Connect())
	defer dev.Disconnect()

	// 创建数据库客户端
	dbClient, err := platform.NewDBClient(&cfg.Database, logger)
	require.NoError(t, err)
	defer dbClient.Close()

	// 构建测试数据
	testData := utils.BuildTelemetryData()
	startTime := time.Now()

	logger.Info("Starting telemetry test",
		zap.Time("start_time", startTime),
		zap.Any("test_data", testData))

	// 发送遥测数据
	err = dev.PublishTelemetry(testData)
	require.NoError(t, err)

	// 等待数据同步
	time.Sleep(time.Duration(cfg.Test.WaitDBSyncSeconds) * time.Second)

	// 验证数据入库
	for key, expectedValue := range testData {
		// 查询最新的遥测数据
		records, err := dbClient.QueryTelemetryData(cfg.Device.DeviceID, key, startTime)
		require.NoError(t, err)
		assert.NotEmpty(t, records, "No telemetry data found for key: %s", key)

		if len(records) > 0 {
			record := records[0]

			// 验证设备ID
			assert.Equal(t, cfg.Device.DeviceID, record.DeviceID)

			// 验证数据值
			err = utils.ValidateTelemetryData(testData, record.Key, record.BoolV, record.NumberV, record.StringV)
			assert.NoError(t, err, "Value validation failed for key: %s", key)

			logger.Info("Telemetry data verified from history table",
				zap.String("key", key),
				zap.Any("expected_value", expectedValue),
				zap.Time("data_time", time.Unix(record.TS, 0)))
		}

		// 验证当前数据表
		currentRecord, err := dbClient.QueryCurrentTelemetry(cfg.Device.DeviceID, key)
		require.NoError(t, err)
		assert.NotNil(t, currentRecord, "No current telemetry data found for key: %s", key)

		if currentRecord != nil {
			err = utils.ValidateTelemetryData(testData, currentRecord.Key, currentRecord.BoolV, currentRecord.NumberV, currentRecord.StringV)
			assert.NoError(t, err, "Current value validation failed for key: %s", key)

			logger.Info("Telemetry data verified from current table",
				zap.String("key", key),
				zap.Any("expected_value", expectedValue),
				zap.Time("data_time", time.Unix(currentRecord.TS, 0)))
		}
	}

	logger.Info("Telemetry test completed successfully")
}

func TestTelemetryControl(t *testing.T) {
	// 初始化日志
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	defer logger.Sync()

	// 加载配置
	cfg, err := config.Load("../../config-community.yaml")
	require.NoError(t, err)

	// 创建设备
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

	// 清空接收消息
	topics := utils.NewMQTTTopics(cfg.Device.DeviceNumber)
	dev.ClearReceivedMessages(topics.TelemetryControl())

	// 构建控制数据
	controlData := map[string]interface{}{
		"switch":      true,
		"temperature": 25.5,
	}

	startTime := time.Now()

	// 下发控制指令
	err = apiClient.PublishTelemetryControl(cfg.Device.DeviceID, controlData)
	require.NoError(t, err)

	// 等待设备接收
	timeout := time.Duration(cfg.Test.WaitMQTTResponseSeconds) * time.Second
	messages := dev.GetReceivedMessages(topics.TelemetryControl(), timeout)
	assert.NotEmpty(t, messages, "Device did not receive control message")

	if len(messages) > 0 {
		// 验证接收到的消息
		var receivedData map[string]interface{}
		err := json.Unmarshal(messages[0].Payload, &receivedData)
		require.NoError(t, err)

		for key, expectedValue := range controlData {
			actualValue, ok := receivedData[key]
			assert.True(t, ok, "Control data key not found: %s", key)
			assert.Equal(t, expectedValue, actualValue, "Control data value mismatch for key: %s", key)
		}

		logger.Info("Control message received and verified",
			zap.Any("data", receivedData))
	}

	// 验证下发日志
	time.Sleep(1 * time.Second)
	logs, err := dbClient.QueryTelemetrySetLogs(cfg.Device.DeviceID, startTime)
	require.NoError(t, err)
	assert.NotEmpty(t, logs, "No telemetry set log found")

	if len(logs) > 0 {
		log := logs[0]
		assert.Equal(t, cfg.Device.DeviceID, log.DeviceID)
		assert.Equal(t, "1", log.Status, "Telemetry control should be successful")

		logger.Info("Telemetry set log verified",
			zap.String("log_id", log.ID),
			zap.String("status", log.Status))
	}
}
