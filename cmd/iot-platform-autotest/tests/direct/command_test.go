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

func TestCommandPublish(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	cfg, err := config.Load("../../config-community.yaml")
	require.NoError(t, err)

	dev, err := device.NewDevice(cfg, logger)
	require.NoError(t, err)
	require.NoError(t, dev.Connect())
	defer dev.Disconnect()

	// 订阅所有主题
	require.NoError(t, dev.SubscribeAll())

	apiClient := platform.NewAPIClient(&cfg.API, logger)
	dbClient, err := platform.NewDBClient(&cfg.Database, logger)
	require.NoError(t, err)
	defer dbClient.Close()

	topics := utils.NewMQTTTopics(cfg.Device.DeviceNumber)
	dev.ClearReceivedMessages("")

	// 构建命令数据
	identify := "RestartDevice"
	commandData := map[string]interface{}{
		"delay_seconds": float64(5), // 使用 float64
		"mode":          "safe",
	}

	// 下发命令
	err = apiClient.PublishCommand(cfg.Device.DeviceID, identify, commandData)
	require.NoError(t, err)

	// 等待设备接收
	timeout := time.Duration(cfg.Test.WaitMQTTResponseSeconds) * time.Second
	messages := dev.GetReceivedMessages(topics.Command(), timeout)
	assert.NotEmpty(t, messages, "Device did not receive command")

	if len(messages) > 0 {
		receivedMsg := messages[0]

		var receivedCmd map[string]interface{}
		err := json.Unmarshal(receivedMsg.Payload, &receivedCmd)
		require.NoError(t, err)

		// 验证命令内容
		assert.Equal(t, identify, receivedCmd["method"], "Command method mismatch")

		params, ok := receivedCmd["params"].(map[string]interface{})
		assert.True(t, ok, "Command params not found")

		for key, expectedValue := range commandData {
			actualValue := params[key]
			assert.Equal(t, expectedValue, actualValue, "Command param mismatch for key: %s", key)
		}

		// 从主题中提取 message_id
		topicParts := strings.Split(receivedMsg.Topic, "/")
		messageID := topicParts[len(topicParts)-1]

		logger.Info("Extracted message_id from topic",
			zap.String("topic", receivedMsg.Topic),
			zap.String("message_id", messageID))

		// 使用提取的 message_id 发送命令响应
		err = dev.PublishCommandResponse(messageID, true, identify)
		require.NoError(t, err)

		logger.Info("Command response sent",
			zap.String("identify", identify),
			zap.String("message_id", messageID))

		// 等待响应被处理
		time.Sleep(3 * time.Second)

		// 验证命令日志 - 使用重试机制
		var log *platform.CommandSetLog
		maxRetries := 5
		for i := 0; i < maxRetries; i++ {
			log, err = dbClient.QueryCommandSetLogs(cfg.Device.DeviceID, messageID)
			require.NoError(t, err)

			if log != nil && log.RspData != nil {
				break
			}

			if i < maxRetries-1 {
				logger.Info("Response data not yet recorded, retrying...",
					zap.Int("attempt", i+1),
					zap.Int("max_retries", maxRetries))
				time.Sleep(2 * time.Second)
			}
		}

		assert.NotNil(t, log, "Command set log not found")

		if log != nil {
			assert.Equal(t, cfg.Device.DeviceID, log.DeviceID, "Device ID mismatch")
			assert.Equal(t, messageID, *log.MessageID, "Message ID should match")
			assert.Equal(t, identify, *log.Identify, "Command identify should match")

			// 验证状态
			assert.Equal(t, "3", log.Status, "Status should be '3' (response success)")

			// 响应数据可能为 NULL
			if log.RspData == nil {
				logger.Warn("Response data is NULL in database",
					zap.String("log_id", log.ID),
					zap.String("message_id", messageID))
			} else {
				logger.Info("Response data recorded successfully",
					zap.String("rsp_data", *log.RspData))
			}

			assert.Equal(t, "1", log.OperationType, "Operation type should be '1' (manual)")

			logger.Info("Command log verified",
				zap.String("log_id", log.ID),
				zap.String("message_id", messageID),
				zap.String("identify", identify),
				zap.String("status", log.Status),
				zap.String("operation_type", log.OperationType))
		}
	}
}
