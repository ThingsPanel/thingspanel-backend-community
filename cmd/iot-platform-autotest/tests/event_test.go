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

func TestEventPublish(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	cfg, err := config.Load("../config.yaml")
	require.NoError(t, err)

	mqttDevice := device.NewMQTTDevice(cfg, logger)
	require.NoError(t, mqttDevice.Connect())
	defer mqttDevice.Disconnect()

	// 订阅事件响应主题
	require.NoError(t, mqttDevice.SubscribeAll())

	dbClient, err := platform.NewDBClient(&cfg.Database, logger)
	require.NoError(t, err)
	defer dbClient.Close()

	topics := utils.NewMQTTTopics(cfg.Device.DeviceNumber)
	mqttDevice.ClearReceivedMessages("")

	// 构建事件数据
	messageID := utils.GenerateMessageID()
	method := "AlarmTriggered"
	params := map[string]interface{}{
		"alarm_type": "temperature_high",
		"level":      "critical",
		"value":      85.5,
	}

	startTime := time.Now()

	logger.Info("Publishing event",
		zap.String("message_id", messageID),
		zap.String("method", method),
		zap.Any("params", params))

	// 发送事件
	err = mqttDevice.PublishEvent(method, params, messageID)
	require.NoError(t, err)

	// 等待数据同步
	time.Sleep(time.Duration(cfg.Test.WaitDBSyncSeconds) * time.Second)

	// 验证事件数据入库
	records, err := dbClient.QueryEventData(cfg.Device.DeviceID, method, startTime)
	require.NoError(t, err)
	assert.NotEmpty(t, records, "No event data found")

	if len(records) > 0 {
		record := records[0]

		assert.Equal(t, cfg.Device.DeviceID, record.DeviceID)
		assert.Equal(t, method, record.Identify)

		// 验证事件数据
		err = utils.ValidateEventData(method, params, record.Data)
		assert.NoError(t, err, "Event data validation failed")

		logger.Info("Event data verified in database",
			zap.String("method", method),
			zap.String("event_id", record.ID),
			zap.Time("event_time", record.TS))
	}

	// 等待并验证平台响应
	timeout := time.Duration(cfg.Test.WaitMQTTResponseSeconds) * time.Second
	responseMessages := mqttDevice.GetReceivedMessages(topics.EventResponse(), timeout)

	if len(responseMessages) > 0 {
		logger.Info("Received event response from platform",
			zap.Int("response_count", len(responseMessages)))

		for _, msg := range responseMessages {
			var response map[string]interface{}
			err := json.Unmarshal(msg.Payload, &response)
			require.NoError(t, err, "Failed to parse response")

			// 验证响应格式
			err = utils.ValidateResponse(response)
			assert.NoError(t, err, "Response validation failed")

			logger.Info("Event response validated",
				zap.String("topic", msg.Topic),
				zap.Any("response", response))
		}
	} else {
		logger.Warn("No event response received from platform",
			zap.String("expected_topic_pattern", topics.EventResponse()))
	}
}
